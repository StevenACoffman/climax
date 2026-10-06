package gomod_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/climax/pkg/gomod"
)

// BenchmarkFind measures locating the module from a command directory a few
// levels below the root, as `climax add` does from inside an app.
func BenchmarkFind(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/bench // the bench module\n\ngo 1.26\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	dir := filepath.Join(root, "cmd", "serve", "internal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		info, err := gomod.Find(dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := info.ImportPath(dir); err != nil {
			b.Fatal(err)
		}
	}
}
