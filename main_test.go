package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestRun_exitCodes drives the whole CLI through run with injected I/O and
// checks how each kind of outcome reaches the shell: 0 for success, --help and a
// bare invocation; 2 for a usage error, with help above the message; 1 for a
// runtime failure, with the message alone; and a command's own ExitError code,
// with no "error:" line because the command already reported.
func TestRun_exitCodes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args      func(t *testing.T) []string
		wantCode  int
		wantError string // the "error: ..." line run prints, or "" for none
		wantHelp  bool
	}{
		"success": {
			args:     func(*testing.T) []string { return []string{"climax", "version"} },
			wantCode: exitSuccess,
		},
		"bare invocation": {
			args:     func(*testing.T) []string { return []string{"climax"} },
			wantCode: exitSuccess,
		},
		"--help": {
			args:     func(*testing.T) []string { return []string{"climax", "add", "--help"} },
			wantCode: exitSuccess, wantHelp: true,
		},
		"usage error": {
			args:      func(*testing.T) []string { return []string{"climax", "add"} },
			wantCode:  exitUsage,
			wantError: "error: add: command name required",
			wantHelp:  true,
		},
		"unknown flag": {
			args:      func(*testing.T) []string { return []string{"climax", "add", "--bogus"} },
			wantCode:  exitUsage,
			wantError: `"--bogus": unknown flag`,
			wantHelp:  true,
		},
		"runtime failure": {
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"climax", "add", "serve", t.TempDir()}
			},
			wantCode:  exitFail,
			wantError: "error: add: path is not inside a Go module",
		},
		"command reported its own outcome": {
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"climax", "lint", driftedApp(t)}
			},
			wantCode: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args(t), strings.NewReader(""), &stdout, &stderr)

			if code != tc.wantCode {
				t.Errorf("exit %d, want %d\nstderr:\n%s", code, tc.wantCode, stderr.String())
			}
			assertStderr(t, stderr.String(), tc.wantError, tc.wantHelp)
		})
	}
}

// assertStderr checks run's stderr carries the wanted "error: ..." line (none
// when wantError is empty) and the command's help exactly when wantHelp is set.
func assertStderr(t *testing.T, stderr, wantError string, wantHelp bool) {
	t.Helper()
	switch {
	case wantError == "" && strings.Contains(stderr, "error: "):
		t.Errorf("printed an error line, want none:\n%s", stderr)
	case wantError != "" && !strings.Contains(stderr, wantError):
		t.Errorf("stderr does not contain %q:\n%s", wantError, stderr)
	}
	if got := strings.Contains(stderr, "COMMAND\n"); got != wantHelp {
		t.Errorf("help printed = %v, want %v\nstderr:\n%s", got, wantHelp, stderr)
	}
}

// driftedApp scaffolds a climax app whose main.go has lost its signal
// handling, so `climax lint` reports the drift and returns ExitError(1).
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
