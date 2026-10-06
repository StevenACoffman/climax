package scaffold_test

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestAddCommand_nestedWithAndWithoutMarkers adds a root command and two
// children of it. With the climax marker comments in cmd/cmd.go, AddCommand
// edits by text; with them removed (the README promises add still works), it
// edits by AST offsets. Either way the result must register every command,
// promote the parent once (`aCfg := a.New(r)`, which the second child must
// still find — the case proptest found), and stay a gofmt-clean, lint-clean
// climax app.
func TestAddCommand_nestedWithAndWithoutMarkers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		stripMarkers bool
	}{
		"with markers":    {stripMarkers: false},
		"without markers": {stripMarkers: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := scaffoldApp(t, "nested")
			const importPrefix = "github.com/example/lintapp"
			cmdGo := filepath.Join(dir, "cmd", "cmd.go")
			if tc.stripMarkers {
				stripMarkerLines(t, cmdGo)
			}

			addParentWithTwoChildren(t, dir, importPrefix)
			assertNestedDispatcher(t, cmdGo, importPrefix)
			if err := scaffold.IsClimaxApp(dir); err != nil {
				t.Errorf("IsClimaxApp: %v", err)
			}
			for _, iss := range lintOrFatal(t, dir) {
				if iss.Severity == scaffold.SeverityError {
					t.Errorf("lint error %s: %s\n%s", iss.File, iss.Property, iss.Detail)
				}
			}
		})
	}
}

// addParentWithTwoChildren adds command a under the root, then b and c under a.
func addParentWithTwoChildren(t *testing.T, dir, importPrefix string) {
	t.Helper()
	if _, _, err := scaffold.AddCommand(dir, "a", importPrefix, scaffold.AddOptions{}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	for _, child := range []string{"b", "c"} {
		if _, _, err := scaffold.AddCommand(dir, child, importPrefix,
			scaffold.AddOptions{Parent: "a"}); err != nil {
			t.Fatalf("add %s under a: %v", child, err)
		}
	}
}

// assertNestedDispatcher checks cmd.go imports and registers a, b and c, with a
// promoted to a captured config exactly once, and is gofmt-clean.
func assertNestedDispatcher(t *testing.T, cmdGo, importPrefix string) {
	t.Helper()
	src, err := os.ReadFile(cmdGo)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"` + importPrefix + `/cmd/a"`, `"` + importPrefix + `/cmd/b"`, `"` + importPrefix + `/cmd/c"`,
		"aCfg := a.New(r)", "b.New(aCfg)", "c.New(aCfg)",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("cmd.go missing %q:\n%s", want, src)
		}
	}
	if n := strings.Count(string(src), "aCfg :="); n != 1 {
		t.Errorf("aCfg declared %d times, want 1:\n%s", n, src)
	}
	formatted, err := format.Source(src)
	if err != nil {
		t.Fatalf("cmd.go does not parse: %v\n%s", err, src)
	}
	if !bytes.Equal(formatted, src) {
		t.Errorf("cmd.go is not gofmt-clean:\n%s", src)
	}
}

// stripMarkerLines removes both climax marker comments from a dispatcher, the
// way a user tidying generated code might, so AddCommand must fall back to AST
// analysis.
func stripMarkerLines(t *testing.T, path string) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for line := range strings.SplitSeq(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == scaffold.ImportsMarker || trimmed == scaffold.CommandsMarker {
			continue
		}
		kept = append(kept, line)
	}
	stripped := strings.Join(kept, "\n")
	if stripped == string(src) {
		t.Fatal("no marker lines found to strip")
	}
	if err := os.WriteFile(path, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}
}
