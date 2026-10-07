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

// TestAddCommand_nestedWithAndWithoutMarkers adds a root command, two children
// of it, and a grandchild under the first child. With the climax marker comments in cmd/cmd.go,
// AddCommand
// edits by text; with them removed (the README promises add still works), it
// edits by AST offsets. Either way the result must register every command,
// promote the parent once (`aCfg := a.New(r)`, which the second child must
// still find — the case proptest found), list registrations in tree order so
// --help shows siblings in the order they were added, and stay a gofmt-clean,
// lint-clean climax app.
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

			addCommandTree(t, dir, importPrefix)
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

// addCommandTree adds a under the root, then b and c under a, then d under b.
func addCommandTree(t *testing.T, dir, importPrefix string) {
	t.Helper()
	for _, c := range []struct{ name, parent string }{
		{"a", ""}, {"b", "a"}, {"c", "a"}, {"d", "b"},
	} {
		if _, _, err := scaffold.AddCommand(dir, c.name, importPrefix,
			scaffold.AddOptions{Parent: c.parent}); err != nil {
			t.Fatalf("add %s under %q: %v", c.name, c.parent, err)
		}
	}
}

// assertNestedDispatcher checks cmd.go imports a, b, c and d, promotes a and b
// to captured configs exactly once each, registers the tree depth-first in the
// order it was added (a, b, b's child d, then c), and is gofmt-clean.
func assertNestedDispatcher(t *testing.T, cmdGo, importPrefix string) {
	t.Helper()
	src, err := os.ReadFile(cmdGo)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		if imp := `"` + importPrefix + `/cmd/` + name + `"`; !bytes.Contains(src, []byte(imp)) {
			t.Errorf("cmd.go missing import %s:\n%s", imp, src)
		}
	}
	for _, cfg := range []string{"aCfg :=", "bCfg :="} {
		if n := strings.Count(string(src), cfg); n != 1 {
			t.Errorf("%q appears %d times, want 1:\n%s", cfg, n, src)
		}
	}
	last := -1
	for _, reg := range []string{"aCfg := a.New(r)", "bCfg := b.New(aCfg)", "d.New(bCfg)", "c.New(aCfg)"} {
		at := bytes.Index(src, []byte(reg))
		if at < 0 {
			t.Errorf("cmd.go missing %q:\n%s", reg, src)
			continue
		}
		if at < last {
			t.Errorf("%q is out of order; want a, b, d, c:\n%s", reg, src)
		}
		last = at
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
