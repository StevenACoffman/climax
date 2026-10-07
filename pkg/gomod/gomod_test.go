package gomod_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/climax/pkg/gomod"
)

// TestFind_moduleDirectiveForms covers the module-line spellings the go
// command accepts. The path ends up in every generated import, so a trailing
// comment or quote leaking through produces a scaffold that does not build.
func TestFind_moduleDirectiveForms(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"plain":            "module example.com/x\n",
		"tab":              "module\texample.com/x\n",
		"trailing comment": "module example.com/x // the x module\n",
		"double-quoted":    "module \"example.com/x\"\n",
		"back-quoted":      "module `example.com/x`\n",
		"after preamble":   "// header\n\nmodule example.com/x\n\ngo 1.26\n",
	}
	for name, gomodText := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(
				filepath.Join(dir, "go.mod"),
				[]byte(gomodText),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
			info, err := gomod.Find(dir)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if info.Module != "example.com/x" {
				t.Errorf("Module = %q, want %q", info.Module, "example.com/x")
			}
		})
	}
}

// TestFind_noDirective reports an error rather than an empty module path.
func TestFind_noDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gomod.Find(dir); err == nil {
		t.Fatal("Find succeeded on a go.mod with no module directive")
	}
}
