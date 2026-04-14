package scaffold_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestInitApp_NoEnvPrefix verifies that InitApp with NoEnvPrefix=true generates
// cmd/cmd.go that uses ff.WithEnvVars() and omits the climax:env-prefix marker
// without requiring network access or compilation.
func TestInitApp_NoEnvPrefix(t *testing.T) {
	dir := t.TempDir()
	const importPrefix = "github.com/example/nopfx"

	// Write a minimal go.mod so IsClimaxApp checks won't be needed here.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+importPrefix+"\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "nopfx",
		Short:        "a no-prefix test app",
		NoEnvPrefix:  true,
	})
	if err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	t.Logf("InitApp wrote: %v", written)

	cmdGo, err := os.ReadFile(filepath.Join(dir, "cmd", "cmd.go"))
	if err != nil {
		t.Fatalf("reading cmd/cmd.go: %v", err)
	}
	content := string(cmdGo)

	if !strings.Contains(content, "ff.WithEnvVars()") {
		t.Errorf("cmd/cmd.go: expected ff.WithEnvVars(), not found\n%s", content)
	}
	if strings.Contains(content, "ff.WithEnvVarPrefix(") {
		t.Errorf("cmd/cmd.go: unexpected ff.WithEnvVarPrefix found\n%s", content)
	}
	if strings.Contains(content, "// climax:env-prefix") {
		t.Errorf(
			"cmd/cmd.go: climax:env-prefix marker should be absent with --no-env-prefix\n%s",
			content,
		)
	}
}

// TestGoVet_InitWithNoEnvPrefix verifies that InitApp with NoEnvPrefix=true
// produces Go source that passes "go vet ./...".
func TestGoVet_InitWithNoEnvPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}

	dir := t.TempDir()
	const importPrefix = "github.com/example/nopfx"

	run(t, dir, goTool, "mod", "init", importPrefix)

	written, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "nopfx",
		Short:        "a no-prefix test app",
		NoEnvPrefix:  true,
	})
	if err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	t.Logf("InitApp wrote: %v", written)

	for _, rel := range written {
		data, readErr := os.ReadFile(filepath.Join(dir, rel))
		if readErr == nil {
			t.Logf("=== %s ===\n%s", rel, data)
		}
	}

	run(t, dir, goTool, "mod", "tidy")
	run(t, dir, goTool, "vet", "./...")
}

// run executes cmd in dir and fails the test on any error.
func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	//nolint:gosec // G204: subprocess with variable is intentional — this helper calls "go" tool
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("%s %v:\n%s", name, args, out)
	}
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
}
