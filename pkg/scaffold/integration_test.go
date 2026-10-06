package scaffold_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/cmd/version"
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

// TestGoVet_InitMachineThenAdd verifies that a fully-featured scaffold
// (init --machine, i.e. all four opt-in features) followed by "climax add"
// produces Go source that compiles cleanly under "go vet ./...".
//
// Like the other integration tests it is skipped in short mode and when the go
// tool is unavailable, because it runs "go mod tidy" (which may hit the network)
// and "go vet" (which compiles the generated code).
func TestGoVet_InitMachineThenAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}

	dir := t.TempDir()
	const importPrefix = "github.com/example/machineapp"

	run(t, dir, goTool, "mod", "init", importPrefix)

	written, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "machineapp",
		Short:        "a fully-featured test application",
		Features: scaffold.Features{
			JSONL: true, GlobalFlags: true, Logger: true, Getenv: true, PosixGuard: true,
		},
	})
	if err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	t.Logf("InitApp wrote: %v", written)

	created, modified, err := scaffold.AddCommand(dir, "serve", importPrefix, scaffold.AddOptions{})
	if err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	t.Logf("AddCommand created %s, modified %s", created, modified)

	for _, rel := range append(written, created, modified) {
		if data, readErr := os.ReadFile(filepath.Join(dir, rel)); readErr == nil {
			t.Logf("=== %s ===\n%s", rel, data)
		}
	}

	run(t, dir, goTool, "mod", "tidy")
	run(t, dir, goTool, "vet", "./...")
}

// TestGoVet_RegisterSplit verifies that a dispatcher whose registrations have
// been extracted into register() (by adding enough commands to pass the split
// threshold) still compiles cleanly under "go vet ./...".
func TestGoVet_RegisterSplit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}

	dir := t.TempDir()
	const importPrefix = "github.com/example/bigapp"

	run(t, dir, goTool, "mod", "init", importPrefix)

	if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "bigapp",
		Short:        "a large test application",
	}); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	// init registers version (1); eight more adds cross the split threshold.
	for i := 1; i <= 8; i++ {
		name := fmt.Sprintf("cmd%d", i)
		if _, _, err := scaffold.AddCommand(
			dir,
			name,
			importPrefix,
			scaffold.AddOptions{},
		); err != nil {
			t.Fatalf("AddCommand(%s): %v", name, err)
		}
	}

	cmdGo, err := os.ReadFile(filepath.Join(dir, "cmd", "cmd.go"))
	if err != nil {
		t.Fatalf("reading cmd/cmd.go: %v", err)
	}
	if !strings.Contains(string(cmdGo), "func register(") {
		t.Fatalf("expected register() split after crossing the threshold; cmd.go:\n%s", cmdGo)
	}
	t.Logf("=== cmd/cmd.go ===\n%s", cmdGo)

	run(t, dir, goTool, "mod", "tidy")
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

// TestGoBuild_versionLdflags verifies that a generated app's cmd/version
// honors the same link-time variables climax's own .goreleaser.yaml injects.
// The linker silently ignores -X for a variable that does not exist, so a
// template that drops or renames one would build fine and report "unknown".
//
// Skipped in short mode and without the go tool: it runs "go mod tidy" (which
// may hit the network) and builds the generated app.
func TestGoBuild_versionLdflags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}

	dir := t.TempDir()
	const importPrefix = "github.com/example/stampapp"
	run(t, dir, goTool, "mod", "init", importPrefix)
	if _, err := scaffold.InitApp(
		dir,
		scaffold.InitOptions{ImportPrefix: importPrefix},
	); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	run(t, dir, goTool, "mod", "tidy")

	stamp := map[string]string{
		"Version":    "v9.9.9",
		"Commit":     "deadbeef",
		"CommitDate": "2026-01-02T03:04:05Z",
		"TreeState":  "dirty",
		"BuiltBy":    "probe",
	}
	var ldflags []string
	for name, value := range stamp {
		ldflags = append(ldflags, "-X "+importPrefix+"/cmd/version."+name+"="+value)
	}
	bin := filepath.Join(dir, "stampapp")
	run(t, dir, goTool, "build", "-ldflags", strings.Join(ldflags, " "), "-o", bin, ".")

	//nolint:gosec // G204: runs the binary this test just built
	out, err := exec.CommandContext(context.Background(), bin, "version", "--json").Output()
	if err != nil {
		t.Fatalf("stampapp version --json: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decoding %s: %v", out, err)
	}
	want := map[string]string{
		"gitVersion":   "v9.9.9",
		"gitCommit":    "deadbeef",
		"buildDate":    "2026-01-02T03:04:05",
		"gitTreeState": "dirty",
		"builtBy":      "probe",
	}
	for field, w := range want {
		if got[field] != w {
			t.Errorf("%s = %q, want %q", field, got[field], w)
		}
	}
}

// TestVersionTemplate_matchesClimaxOnRecordedBuilds runs a generated app's
// cmd/version over every build bin/capture-buildinfo.sh recorded and requires
// the same report climax's own cmd/version gives. The template is a copy, not
// an import, so this is what keeps the two from drifting apart.
//
// Skipped in short mode and without the go tool: it runs "go mod tidy" (which
// may hit the network) and "go test" in the generated app.
func TestVersionTemplate_matchesClimaxOnRecordedBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}
	fixtures, err := filepath.Glob(
		filepath.Join("..", "..", "cmd", "version", "testdata", "buildinfo", "*.txt"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no recorded builds (run bin/capture-buildinfo.sh): %v", err)
	}

	app := appVersionReport(t, goTool, fixtures)
	for _, f := range fixtures {
		name := strings.TrimSuffix(filepath.Base(f), ".txt")
		got, ok := app[name]
		if !ok {
			t.Errorf("%s: generated app produced no report", name)
			continue
		}
		for field, want := range climaxReport(t, f) {
			if got[field] != want {
				t.Errorf("%s: %s = %v in the generated app, %v in climax",
					name, field, got[field], want)
			}
		}
	}
}

// appVersionReport generates an app, copies the fixtures and
// testdata/version_dump_test.go.txt into its cmd/version, and returns what the
// app's copy of the version logic reports for each fixture.
func appVersionReport(t *testing.T, goTool string, fixtures []string) map[string]map[string]any {
	t.Helper()
	dir := t.TempDir()
	const importPrefix = "github.com/example/versionapp"
	run(t, dir, goTool, "mod", "init", importPrefix)
	if _, err := scaffold.InitApp(
		dir,
		scaffold.InitOptions{ImportPrefix: importPrefix},
	); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	versionDir := filepath.Join(dir, "cmd", "version")
	fixtureDir := filepath.Join(versionDir, "testdata", "buildinfo")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		copyFile(t, f, filepath.Join(fixtureDir, filepath.Base(f)))
	}
	dump, err := os.ReadFile(filepath.Join("testdata", "version_dump_test.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dump = []byte(strings.ReplaceAll(string(dump), "APP_IMPORT", importPrefix))
	if err := os.WriteFile(filepath.Join(versionDir, "dump_test.go"), dump, 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, dir, goTool, "mod", "tidy")
	reportPath := filepath.Join(t.TempDir(), "report.json")
	run(t, dir, goTool, "test", "./cmd/version", "-run", "TestDump", "-args", "-out="+reportPath)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

// copyFile copies src to dst.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// climaxReport is climax's own cmd/version reading of a recorded build, as the
// generic JSON map the generated app's report decodes to.
func climaxReport(t *testing.T, fixture string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := debug.ParseBuildInfo(string(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(version.GetVersionInfoFrom(bi, stampFromLdflags(bi)))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// stampFromLdflags recovers the link-time stamp from a recorded -ldflags
// setting, as testdata/version_dump_test.go.txt does inside the app.
func stampFromLdflags(bi *debug.BuildInfo) version.Stamp {
	stamp := version.Stamp{Version: "dev"}
	set := map[string]*string{
		"Version": &stamp.Version, "Commit": &stamp.Commit, "CommitDate": &stamp.CommitDate,
		"TreeState": &stamp.TreeState, "BuiltBy": &stamp.BuiltBy,
	}
	for _, s := range bi.Settings {
		if s.Key != "-ldflags" {
			continue
		}
		for _, field := range strings.Fields(s.Value) {
			target, value, ok := strings.Cut(field, "=")
			if !ok || !strings.Contains(target, "/cmd/version.") {
				continue
			}
			if p, ok := set[target[strings.LastIndex(target, ".")+1:]]; ok {
				*p = value
			}
		}
	}
	return stamp
}

// TestGoBuild_helpOnlyForUsageErrors builds a generated app and checks it keeps
// the rules climax's own does: a usage error prints the command's help and
// exits 2, a runtime failure prints only its message and exits 1, and a bare
// invocation exits 0 quietly.
//
// Skipped in short mode and without the go tool: it runs "go mod tidy" (which
// may hit the network) and builds the generated app.
func TestGoBuild_helpOnlyForUsageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}
	bin := buildUsageApp(t, goTool)

	cases := map[string]struct {
		args     []string
		wantExit int
		wantHelp bool
		wantMsg  string
	}{
		"usage error":        {[]string{"serve"}, 2, true, "serve: address required"},
		"runtime error":      {[]string{"serve", ":80"}, 1, false, "serve: connection refused"},
		"unknown subcommand": {[]string{"nosuch"}, 2, true, `unknown subcommand "nosuch"`},
		"bare invocation":    {nil, 0, false, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			exit, stderr := runBinary(t, bin, tc.args...)
			if exit != tc.wantExit {
				t.Errorf("exit %d, want %d\nstderr:\n%s", exit, tc.wantExit, stderr)
			}
			if got := strings.Contains(stderr, "COMMAND\n"); got != tc.wantHelp {
				t.Errorf("help printed = %v, want %v\nstderr:\n%s", got, tc.wantHelp, stderr)
			}
			if !strings.Contains(stderr, tc.wantMsg) {
				t.Errorf("stderr does not mention %q:\n%s", tc.wantMsg, stderr)
			}
		})
	}
}

// buildUsageApp generates an app with one command, serve, that fails as a
// usage error without an argument and as a runtime error with one, and
// returns the path of the built binary.
func buildUsageApp(t *testing.T, goTool string) string {
	t.Helper()
	dir := t.TempDir()
	const importPrefix = "github.com/example/usageapp"
	run(t, dir, goTool, "mod", "init", importPrefix)
	if _, err := scaffold.InitApp(
		dir,
		scaffold.InitOptions{ImportPrefix: importPrefix},
	); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	// AddCommand registers serve in the dispatcher; its body is then replaced.
	if _, _, err := scaffold.AddCommand(
		dir,
		"serve",
		importPrefix,
		scaffold.AddOptions{},
	); err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	serve := `package serve

import (
	"context"
	"errors"

	"github.com/peterbourgon/ff/v4"

	"` + importPrefix + `/cmd/root"
)

type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("serve").SetParent(parent.Flags)
	cfg.Command = &ff.Command{Name: "serve", Usage: "usageapp serve <addr>", Flags: cfg.Flags, Exec: cfg.exec}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if len(args) == 0 {
		return &root.UsageError{Err: errors.New("serve: address required")}
	}
	return errors.New("serve: connection refused")
}
`
	if err := os.WriteFile(
		filepath.Join(dir, "cmd", "serve", "serve.go"),
		[]byte(serve),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	run(t, dir, goTool, "mod", "tidy")
	bin := filepath.Join(dir, "usageapp")
	run(t, dir, goTool, "build", "-o", bin, ".")
	return bin
}

// runBinary runs bin with args and returns its exit code and stderr.
func runBinary(t *testing.T, bin string, args ...string) (exit int, stderr string) {
	t.Helper()
	//nolint:gosec // G204: runs a binary this test built
	c := exec.CommandContext(context.Background(), bin, args...)
	var buf strings.Builder
	c.Stderr = &buf
	err := c.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode(), buf.String()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, buf.String()
}
