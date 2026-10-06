package proptest_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/climax/pkg/gomod"
)

// TestGomod_findAndImportPath checks that, from any directory nested below a
// module root, Find returns that root and its declared path however the module
// line is spelled, and ImportPath maps the directory to module + its relative
// path — the import path init and add write into generated code.
func TestGomod_findAndImportPath(t *testing.T) {
	base := t.TempDir()
	segment := rapid.StringMatching(`[a-z][a-z0-9_\-]{0,6}`)
	rapid.Check(t, func(t *rapid.T) {
		mod := strings.Join(rapid.SliceOfN(segment, 1, 4).Draw(t, "modSegments"), ".") +
			"/" + segment.Draw(t, "modLeaf")
		spelling := rapid.SampledFrom([]string{"plain", "tab", "comment", "quoted"}).
			Draw(t, "spelling")
		line := map[string]string{
			"plain":   "module " + mod,
			"tab":     "module\t" + mod,
			"comment": "module " + mod + " // trailing",
			"quoted":  "module " + strconv.Quote(mod),
		}[spelling]

		root, err := os.MkdirTemp(base, "mod")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"),
			[]byte("// preamble\n\n"+line+"\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rel := rapid.SliceOfN(segment, 0, 4).Draw(t, "subdirs")
		dir := filepath.Join(append([]string{root}, rel...)...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		info, err := gomod.Find(dir)
		if err != nil {
			t.Fatalf("Find(%s): %v", dir, err)
		}
		if info.Root != root || info.Module != mod {
			t.Fatalf("Find = {%s %q}, want {%s %q}", info.Root, info.Module, root, mod)
		}
		want := strings.Join(append([]string{mod}, rel...), "/")
		got, err := info.ImportPath(dir)
		if err != nil {
			t.Fatalf("ImportPath: %v", err)
		}
		if got != want {
			t.Fatalf("ImportPath(%s) = %q, want %q", dir, got, want)
		}
	})
}
