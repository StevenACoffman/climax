package scaffold_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestGoVet_InitThenAdd verifies that running "climax init" followed by
// "climax add" produces Go source that passes "go vet ./...".
//
// The test is skipped when the "go" tool is not available or when -short is set,
// because it invokes "go mod tidy" (which may hit the network or module cache)
// and "go vet" (which compiles the generated code).
func TestGoVet_InitThenAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}

	dir := t.TempDir()
	const importPrefix = "github.com/example/testapp"

	// --- go mod init ---
	run(t, dir, goTool, "mod", "init", importPrefix)

	// --- climax init ---
	written, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "testapp",
		Short:        "a test application",
		RootPkg:      "root",
	})
	if err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	t.Logf("InitApp wrote: %v", written)

	// --- climax add ---
	created, modified, err := scaffold.AddCommand(dir, "serve", importPrefix, scaffold.AddOptions{})
	if err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	t.Logf("AddCommand created %s, modified %s", created, modified)

	// Dump generated files for easy debugging on failure.
	for _, rel := range append(written, created, modified) {
		data, readErr := os.ReadFile(filepath.Join(dir, rel))
		if readErr == nil {
			t.Logf("=== %s ===\n%s", rel, data)
		}
	}

	// --- go mod tidy (fetches ff/v4 into the module cache if needed) ---
	run(t, dir, goTool, "mod", "tidy")

	// --- go vet ./... ---
	run(t, dir, goTool, "vet", "./...")
}

// run executes cmd in dir and fails the test on any error.
func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("%s %v:\n%s", name, args, out)
	}
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
}
