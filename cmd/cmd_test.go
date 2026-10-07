package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd"
	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// helpHeader starts every ffhelp.Command rendering, so its presence on stderr
// means the dispatcher printed a command's help.
const helpHeader = "COMMAND\n"

// TestRun_helpOnlyForUsageErrors checks the dispatcher's one rule about help:
// it is printed for --help and for usage errors, and never for a runtime
// failure, a bare invocation, or a command that already reported its outcome.
func TestRun_helpOnlyForUsageErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args      func(t *testing.T) []string
		wantHelp  bool
		wantUsage bool   // errors.Is(err, root.ErrUsage)
		wantErr   error  // a sentinel the error must also match, or nil
		wantMsg   string // text the error message must contain, or ""
	}{
		"unknown subcommand": {
			args:     func(*testing.T) []string { return []string{"nosuch"} },
			wantHelp: true, wantUsage: true,
		},
		"unknown flag": {
			args:     func(*testing.T) []string { return []string{"add", "--bogus"} },
			wantHelp: true, wantUsage: true,
		},
		"missing argument": {
			args:     func(*testing.T) []string { return []string{"add"} },
			wantHelp: true, wantUsage: true,
		},
		"invalid argument": {
			args:     func(*testing.T) []string { return []string{"add", "func"} },
			wantHelp: true, wantUsage: true,
		},
		"invalid flag value": {
			args:      func(*testing.T) []string { return []string{"add", "--parent", "a-b", "serve"} },
			wantHelp:  true,
			wantUsage: true,
		},
		"conflicting flags": {
			args: func(*testing.T) []string {
				return []string{"init", "--env-prefix", "X", "--no-env-prefix"}
			},
			wantHelp: true, wantUsage: true,
		},
		"flag out of range": {
			args:     func(*testing.T) []string { return []string{"mango", "--section", "9"} },
			wantHelp: true, wantUsage: true,
		},
		"flag after an argument": {
			args: func(*testing.T) []string {
				return []string{"add", "serve", ".", "--short", "x"}
			},
			wantHelp: true, wantUsage: true,
			wantMsg: `flag "--short" must come before the arguments`,
		},
		"flag after --": {
			// "--" makes everything after it literal, so the guard stays out of
			// the way and add sees "--" as its path argument.
			args:    func(*testing.T) []string { return []string{"add", "serve", "--", "--x"} },
			wantMsg: "not a climax app root",
		},
		"leading -- passes a dash through": {
			// ff consumes the leading "--", so add receives "-x" as its name;
			// add rejects that itself, not the misplaced-flag guard.
			args:     func(*testing.T) []string { return []string{"add", "--", "-x"} },
			wantHelp: true, wantUsage: true,
			wantMsg: `command name "-x" must start with a letter`,
		},
		"--help": {
			args:     func(*testing.T) []string { return []string{"add", "--help"} },
			wantHelp: true, wantErr: ff.ErrHelp,
		},
		"bare invocation": {
			args:    func(*testing.T) []string { return nil },
			wantErr: ff.ErrNoExec,
		},
		"runtime failure": {
			// An empty directory is not inside a Go module: the command line
			// was fine, the environment was not.
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"add", "serve", t.TempDir()}
			},
		},
		"command reported its own outcome": {
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"lint", driftedApp(t)}
			},
			wantErr: root.ExitError(1),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			err := cmd.Run(
				context.Background(),
				tc.args(t),
				strings.NewReader(""),
				&stdout,
				&stderr,
			)

			if err == nil {
				t.Fatal("Run returned nil; every case here is a failure or a non-exec outcome")
			}
			if got := strings.Contains(stderr.String(), helpHeader); got != tc.wantHelp {
				t.Errorf("help printed = %v, want %v\nerror: %v\nstderr:\n%s",
					got, tc.wantHelp, err, stderr.String())
			}
			if got := errors.Is(err, root.ErrUsage); got != tc.wantUsage {
				t.Errorf("errors.Is(err, ErrUsage) = %v, want %v (err: %v)", got, tc.wantUsage, err)
			}
			assertMatches(t, err, tc.wantErr, tc.wantMsg)
		})
	}
}

// assertMatches fails unless err matches want through errors.Is (when want is
// set) and its message contains msg (when msg is set).
func assertMatches(t *testing.T, err, want error, msg string) {
	t.Helper()
	if want != nil && !errors.Is(err, want) {
		t.Errorf("errors.Is(%v, %v) = false", err, want)
	}
	if msg != "" && !strings.Contains(err.Error(), msg) {
		t.Errorf("error %q does not contain %q", err, msg)
	}
}

// driftedApp scaffolds a climax app whose main.go has lost its signal
// handling, so `climax lint` reports the drift and exits through ExitError.
func driftedApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const importPrefix = "example.com/drifted"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+importPrefix+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.InitApp(
		dir,
		scaffold.InitOptions{ImportPrefix: importPrefix},
	); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	mainPath := filepath.Join(dir, "main.go")
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(src), "signal.NotifyContext(", "withoutSignals(", 1)
	if drifted == string(src) {
		t.Fatal("main.go template no longer calls signal.NotifyContext")
	}
	if err := os.WriteFile(mainPath, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
