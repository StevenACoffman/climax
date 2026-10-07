package scaffold_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// benchImportPrefix is the module path every benchmark app is generated under.
const benchImportPrefix = "github.com/example/benchapp"

// allFeatures is the `init --machine` feature set: the largest scaffold, so the
// benchmarks see every template and every feature transform.
var allFeatures = scaffold.Features{
	JSONL: true, GlobalFlags: true, Logger: true, Getenv: true, PosixGuard: true,
}

// benchApp writes a go.mod and a fully-featured scaffold into a new temp dir.
func benchApp(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+benchImportPrefix+"\n\ngo 1.26\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: benchImportPrefix,
		Short:        "a benchmark application",
		Features:     allFeatures,
	}); err != nil {
		b.Fatalf("InitApp: %v", err)
	}
	return dir
}

// BenchmarkInitApp measures `climax init --machine`: template expansion, the
// feature transforms and their gofmt pass, and the file writes.
func BenchmarkInitApp(b *testing.B) {
	base := b.TempDir()
	for b.Loop() {
		dir, err := os.MkdirTemp(base, "app")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
			ImportPrefix: benchImportPrefix,
			Features:     allFeatures,
		}); err != nil {
			b.Fatalf("InitApp: %v", err)
		}
	}
}

// BenchmarkAddCommand measures `climax add` against a dispatcher that already
// holds a handful of commands, including the parse and rewrite of cmd.go.
func BenchmarkAddCommand(b *testing.B) {
	dir := benchApp(b)
	for i := range 6 {
		if _, _, err := scaffold.AddCommand(dir, "pre"+strconv.Itoa(i),
			benchImportPrefix, scaffold.AddOptions{}); err != nil {
			b.Fatalf("AddCommand: %v", err)
		}
	}
	cmdGo := filepath.Join(dir, "cmd", "cmd.go")
	dispatcher, err := os.ReadFile(cmdGo)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		// Undo the previous iteration so each one adds a seventh command to the
		// same six-command dispatcher, rather than to an ever-growing one.
		b.StopTimer()
		if err := os.WriteFile(cmdGo, dispatcher, 0o644); err != nil {
			b.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(dir, "cmd", "added")); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, _, err := scaffold.AddCommand(dir, "added", benchImportPrefix,
			scaffold.AddOptions{}); err != nil {
			b.Fatalf("AddCommand: %v", err)
		}
	}
}

// BenchmarkLintApp measures `climax lint` on a fully-featured app with a few
// commands: dispatcher analysis, per-file parsing, and the template diffing.
func BenchmarkLintApp(b *testing.B) {
	dir := benchApp(b)
	for _, name := range []string{"serve", "migrate", "status"} {
		if _, _, err := scaffold.AddCommand(dir, name, benchImportPrefix,
			scaffold.AddOptions{}); err != nil {
			b.Fatalf("AddCommand: %v", err)
		}
	}
	for b.Loop() {
		if _, err := scaffold.LintApp(dir); err != nil {
			b.Fatalf("LintApp: %v", err)
		}
	}
}

// BenchmarkNormalizeEnvPrefix measures the per-name normalization init runs on
// every app name.
func BenchmarkNormalizeEnvPrefix(b *testing.B) {
	for b.Loop() {
		_ = scaffold.NormalizeEnvPrefix("my-cli.tool_v2")
	}
}
