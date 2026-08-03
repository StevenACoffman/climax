package scaffold_test

import (
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// initFeatured scaffolds an app with the given features into a temp dir and
// returns the dir. InitApp only writes source (no compilation), so this needs
// no network or module cache.
func initFeatured(t *testing.T, f scaffold.Features) string {
	t.Helper()
	dir := t.TempDir()
	const imp = "github.com/example/feat"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+imp+"\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: imp,
		Name:         "feat",
		Short:        "a featured app",
		Features:     f,
	}); err != nil {
		t.Fatalf("InitApp(%+v): %v", f, err)
	}
	return dir
}

// assertAllParse fails the test if any generated .go file is not syntactically
// valid Go — the key correctness property of the string-splice transforms.
func assertAllParse(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// ParseFile reads the file itself, keeping filesystem access out of the
		// WalkDir callback (gosec G122).
		if _, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0); perr != nil {
			t.Errorf("%s: generated source does not parse: %v", path, perr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
}

// read returns the contents of a generated file relative to dir.
func read(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// assertGofmt fails if the generated file is not already gofmt-formatted.
func assertGofmt(t *testing.T, dir, rel string) {
	t.Helper()
	src := read(t, dir, rel)
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("%s: format.Source: %v", rel, err)
	}
	if string(formatted) != src {
		t.Errorf("%s is not gofmt-clean", rel)
	}
}

func TestInitApp_Getenv(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{Getenv: true})
	assertAllParse(t, dir)

	root := read(t, dir, filepath.Join("cmd", "root", "root.go"))
	if !strings.Contains(root, "func New(getenv func(string) string, stdin io.Reader") {
		t.Error("root New signature missing getenv parameter")
	}
	if !strings.Contains(root, "cfg.Getenv = getenv") {
		t.Error("root New body missing cfg.Getenv assignment")
	}
	cmd := read(t, dir, filepath.Join("cmd", "cmd.go"))
	if !strings.Contains(cmd, "getenv func(string) string") {
		t.Error("dispatcher Run missing getenv parameter")
	}
	if !strings.Contains(cmd, "root.New(getenv, stdin, stdout, stderr)") {
		t.Error("dispatcher does not forward getenv to root.New")
	}
	if m := read(t, dir, "main.go"); !strings.Contains(m, "os.Getenv") {
		t.Error("main.go does not pass os.Getenv to cmd.Run")
	}
}

func TestInitApp_GlobalFlags(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{GlobalFlags: true})
	assertAllParse(t, dir)

	root := read(t, dir, filepath.Join("cmd", "root", "root.go"))
	for _, want := range []string{
		"Verbose bool", "Quiet",
		`cfg.Flags.BoolVar(&cfg.Verbose, 'v', "verbose"`,
		`cfg.Flags.BoolVar(&cfg.Quiet, 'q', "quiet"`,
		"cfg.Flags,", // flag set must be attached to the root ff.Command
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root.go missing %q", want)
		}
	}
	if cmd := read(
		t,
		dir,
		filepath.Join("cmd", "cmd.go"),
	); !strings.Contains(
		cmd,
		"r.Stdout = io.Discard",
	) {
		t.Error("dispatcher missing --quiet stdout redirect")
	}
}

func TestInitApp_Logger(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{Logger: true})
	assertAllParse(t, dir)
	assertGofmt(t, dir, filepath.Join("cmd", "root", "logger.go"))

	root := read(t, dir, filepath.Join("cmd", "root", "root.go"))
	// gofmt aligns the Config struct, so match the field type rather than exact spacing.
	for _, want := range []string{`"log/slog"`, "*slog.Logger", "cfg.Log = NewLogger(stderr, false, slog.LevelWarn)"} {
		if !strings.Contains(root, want) {
			t.Errorf("root.go missing %q", want)
		}
	}
	logger := read(t, dir, filepath.Join("cmd", "root", "logger.go"))
	for _, want := range []string{"func NewLogger(", "func LogLevel("} {
		if !strings.Contains(logger, want) {
			t.Errorf("logger.go missing %q", want)
		}
	}
	// Without --global-flags there is nothing to rebuild the logger from.
	if cmd := read(t, dir, filepath.Join("cmd", "cmd.go")); strings.Contains(cmd, "NewLogger") {
		t.Error("dispatcher should not rebuild the logger without --global-flags")
	}
}

func TestInitApp_LoggerWithGlobalFlags(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{Logger: true, GlobalFlags: true})
	assertAllParse(t, dir)
	if cmd := read(t, dir, filepath.Join("cmd", "cmd.go")); !strings.Contains(
		cmd, "r.Log = root.NewLogger(stderr, r.JSONL, root.LogLevel(r.Verbose, r.Quiet))") {
		t.Error("dispatcher missing logger rebuild for --logger --global-flags")
	}
}

func TestInitApp_JSONL(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{JSONL: true})
	assertAllParse(t, dir)
	assertGofmt(t, dir, filepath.Join("cmd", "root", "outcome.go"))

	outcome := read(t, dir, filepath.Join("cmd", "root", "outcome.go"))
	for _, want := range []string{
		"type Outcome struct", "StatusOK", "func (c *Config) EmitJSONL(", "func CodeForError(err error) int",
	} {
		if !strings.Contains(outcome, want) {
			t.Errorf("outcome.go missing %q", want)
		}
	}
}

func TestInitApp_PosixGuard(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{PosixGuard: true})
	assertAllParse(t, dir)
	assertGofmt(t, dir, filepath.Join("cmd", "root", "misplaced.go"))

	misplaced := read(t, dir, filepath.Join("cmd", "root", "misplaced.go"))
	if !strings.Contains(misplaced, "func MisplacedFlag(args []string) string") {
		t.Error("misplaced.go missing MisplacedFlag helper")
	}
}

func TestInitApp_Machine_AllFeaturesParse(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{
		JSONL: true, GlobalFlags: true, Logger: true, Getenv: true, PosixGuard: true,
	})
	assertAllParse(t, dir)
	assertGofmt(t, dir, filepath.Join("cmd", "root", "root.go"))
	assertGofmt(t, dir, filepath.Join("cmd", "cmd.go"))
	assertGofmt(t, dir, filepath.Join("cmd", "root", "outcome.go"))
	assertGofmt(t, dir, filepath.Join("cmd", "root", "logger.go"))
	assertGofmt(t, dir, filepath.Join("cmd", "root", "misplaced.go"))
	// getenv + global-flags together: New takes getenv and binds flags.
	root := read(t, dir, filepath.Join("cmd", "root", "root.go"))
	if !strings.Contains(root, "func New(getenv func(string) string") {
		t.Error("--machine root New missing getenv parameter")
	}
	if !strings.Contains(root, "cfg.Flags.BoolVar(&cfg.Verbose") {
		t.Error("--machine root New missing global-flag binding")
	}
}

func TestInitApp_FeaturesMarker(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{
		JSONL: true, GlobalFlags: true, Logger: true, Getenv: true, PosixGuard: true,
	})
	cmd := read(t, dir, filepath.Join("cmd", "cmd.go"))
	if !strings.Contains(cmd, "// climax:features jsonl,global-flags,logger,getenv,posix-guard") {
		t.Errorf("cmd.go missing the full climax:features marker:\n%s", cmd)
	}

	// A no-feature scaffold records no marker.
	base := initFeatured(t, scaffold.Features{})
	if strings.Contains(read(t, base, filepath.Join("cmd", "cmd.go")), "climax:features") {
		t.Error("base scaffold should not emit a climax:features marker")
	}
}

func TestInitApp_NoFeatures_NoArtifacts(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{})
	if _, err := os.Stat(filepath.Join(dir, "cmd", "root", "outcome.go")); err == nil {
		t.Error("outcome.go generated without --jsonl")
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "root", "logger.go")); err == nil {
		t.Error("logger.go generated without --logger")
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "root", "misplaced.go")); err == nil {
		t.Error("misplaced.go generated without --posix-guard")
	}
	root := read(t, dir, filepath.Join("cmd", "root", "root.go"))
	// Match feature tokens by name (gofmt alignment makes exact spacing unstable).
	for _, unwanted := range []string{"Verbose", "log/slog", "Getenv"} {
		if strings.Contains(root, unwanted) {
			t.Errorf("base root.go unexpectedly contains %q", unwanted)
		}
	}
}
