package proptest_test

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// dispatcherImports are the package names cmd/cmd.go imports itself. A command
// or root package of the same name passes ValidateIdent but redeclares the
// import, so the dispatcher does not compile — a known gap the generator steps
// around, since LintApp parses without type-checking and could not see it.
var dispatcherImports = map[string]bool{
	"context": true, "errors": true, "fmt": true, "io": true,
	"os": true, "ff": true, "ffhelp": true, "slog": true,
}

// helpText draws ShortHelp/LongHelp values, including the quotes, backslashes
// and newlines that have to survive being spliced into a Go string literal.
var helpText = rapid.OneOf(
	rapid.Just(""),
	rapid.StringMatching(`[A-Za-z ]{1,20}`),
	rapid.StringOfN(rapid.SampledFrom([]rune("a Z\"\\\n\t`é$%{}")), 1, 16, -1),
)

// pkgName draws a lower-case Go identifier that is safe as a command or root
// package name: not a keyword, not one of the dispatcher's own imports, and
// not a directory the scaffold already owns.
func pkgName(taken ...string) *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_]{0,8}`).Filter(func(s string) bool {
		if scaffold.ValidateIdent(s) != nil || dispatcherImports[s] {
			return false
		}
		switch s {
		case "cmd", "main", "version", "man":
			return false
		}
		for _, t := range taken {
			if s == t {
				return false
			}
		}
		return true
	})
}

// TestScaffold_initThenAddStaysClean is the scaffold's central promise stated
// as a property: whatever combination of init options and features is chosen,
// and however many commands are added afterwards (nested or not, crossing the
// point where registrations move into register()), the result is a climax app
// that `climax lint` reports no structural drift on, and every Go file in it
// is gofmt-clean — imports sorted included, whatever the module path sorts
// next to.
func TestScaffold_initThenAddStaysClean(t *testing.T) {
	base := t.TempDir()
	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp(base, "app")
		if err != nil {
			t.Fatal(err)
		}
		// Paths that sort before, inside, and after github.com/peterbourgon/ff, so
		// import order cannot come out right by accident of the module name.
		importPrefix := rapid.SampledFrom([]string{
			"example.com/prop/app", "github.com/acme/app", "github.com/zed/app", "zz.io/app",
		}).Draw(t, "importPrefix")
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module "+importPrefix+"\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		opts := drawInitOptions(t, importPrefix)
		if _, err := scaffold.InitApp(dir, opts); err != nil {
			t.Fatalf("InitApp(%+v): %v", opts, err)
		}
		addCommands(t, dir, importPrefix, opts.RootPkg)

		if err := scaffold.IsClimaxApp(dir); err != nil {
			t.Fatalf("IsClimaxApp: %v", err)
		}
		assertLintClean(t, dir)
		assertGofmtClean(t, dir)
	})
}

// drawInitOptions draws every option `climax init` exposes.
func drawInitOptions(t *rapid.T, importPrefix string) scaffold.InitOptions {
	return scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         rapid.StringMatching(`([a-z][a-z0-9\-]{0,10})?`).Draw(t, "name"),
		Short:        helpText.Draw(t, "short"),
		Long:         helpText.Draw(t, "long"),
		RootPkg:      rapid.OneOf(rapid.Just("root"), pkgName()).Draw(t, "rootPkg"),
		EnvPrefix: rapid.StringMatching(`([A-Za-z][A-Za-z0-9_\-.]{0,8})?`).
			Draw(t, "envPrefix"),
		NoEnvPrefix: rapid.Bool().Draw(t, "noEnvPrefix"),
		NoVersion:   rapid.Bool().Draw(t, "noVersion"),
		Features: scaffold.Features{
			JSONL:       rapid.Bool().Draw(t, "jsonl"),
			GlobalFlags: rapid.Bool().Draw(t, "globalFlags"),
			Logger:      rapid.Bool().Draw(t, "logger"),
			Getenv:      rapid.Bool().Draw(t, "getenv"),
			PosixGuard:  rapid.Bool().Draw(t, "posixGuard"),
		},
	}
}

// addCommands adds up to 10 distinct commands — enough for some runs to cross
// the 8-registration split into register() — each either under the root or
// under a command added before it.
func addCommands(t *rapid.T, dir, importPrefix, rootPkg string) {
	t.Helper()
	cmds := rapid.SliceOfNDistinct(pkgName(rootPkg), 0, 10, rapid.ID[string]).
		Draw(t, "commands")
	var added []string
	for _, name := range cmds {
		add := scaffold.AddOptions{
			Short: helpText.Draw(t, "cmdShort"),
			Long:  helpText.Draw(t, "cmdLong"),
		}
		if len(added) > 0 && rapid.Bool().Draw(t, "nested") {
			add.Parent = rapid.SampledFrom(added).Draw(t, "parent")
		}
		if _, _, err := scaffold.AddCommand(dir, name, importPrefix, add); err != nil {
			t.Fatalf("AddCommand(%q, %+v): %v", name, add, err)
		}
		added = append(added, name)
	}
}

// assertLintClean fails on any error-severity LintApp issue. Warnings are
// advisory (an un-filled "TODO: describe" ShortHelp is one) and expected.
func assertLintClean(t *rapid.T, dir string) {
	t.Helper()
	issues, err := scaffold.LintApp(dir)
	if err != nil {
		t.Fatalf("LintApp: %v", err)
	}
	for _, iss := range issues {
		if iss.Severity == scaffold.SeverityError {
			t.Errorf("%s: %s\n%s", iss.File, iss.Property, iss.Detail)
		}
	}
}

// assertGofmtClean fails if any .go file under dir does not parse, or would
// be changed by gofmt.
func assertGofmtClean(t *rapid.T, dir string) {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(rel, ".go") {
			return err
		}
		src, err := root.ReadFile(rel)
		if err != nil {
			return err
		}
		got, err := format.Source(src)
		switch {
		case err != nil:
			t.Errorf("%s does not parse: %v", rel, err)
		case !bytes.Equal(got, src):
			t.Errorf("%s is not gofmt-clean:\n%s", rel, src)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
