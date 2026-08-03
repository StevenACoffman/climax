package scaffold

import (
	"strings"
	"testing"
)

func TestParsedSourceFromTemplate_main(t *testing.T) {
	ps, err := parsedSourceFromTemplate("main.go", mainTemplate, nil)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate(mainTemplate): %v", err)
	}
	if ps.file == nil {
		t.Fatal("expected non-nil AST file")
	}
	// Must be able to find main and run functions.
	if findFuncDecl(ps.file, "main") == nil {
		t.Error("func main not found in parsed mainTemplate")
	}
	if findFuncDecl(ps.file, "run") == nil {
		t.Error("func run not found in parsed mainTemplate")
	}
}

func TestParsedSourceFromTemplate_cmd(t *testing.T) {
	ps, err := parsedSourceFromTemplate(
		"cmd.go",
		cmdTemplate,
		map[string]string{"ROOT_PKG": "root"},
	)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate(cmdTemplate): %v", err)
	}
	if findFuncDecl(ps.file, "Run") == nil {
		t.Error("func Run not found in parsed cmdTemplate")
	}
}

func TestParsedSourceFromTemplate_root(t *testing.T) {
	ps, err := parsedSourceFromTemplate(
		"root.go",
		rootTemplate,
		map[string]string{"ROOT_PKG": "root"},
	)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate(rootTemplate): %v", err)
	}
	if !astStructHasField(ps.file, "Config", "Stdin") {
		t.Error("Config.Stdin field not found in parsed rootTemplate")
	}
	if findFuncDecl(ps.file, "New") == nil {
		t.Error("func New not found in parsed rootTemplate")
	}
}

func TestLintExpectedMain_containsKeyPatterns(t *testing.T) {
	got, err := lintExpectedMain()
	if err != nil {
		t.Fatalf("lintExpectedMain: %v", err)
	}
	for _, want := range []string{
		"signal.NotifyContext",
		"func run(",
		"os.Stdin",
		"os.Exit(code)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lintExpectedMain: missing %q", want)
		}
	}
}

func TestLintExpectedCmd_substitutesPkg(t *testing.T) {
	got, err := lintExpectedCmd("myroot")
	if err != nil {
		t.Fatalf("lintExpectedCmd: %v", err)
	}
	if !strings.Contains(got, "stdin io.Reader") {
		t.Error("lintExpectedCmd: missing stdin io.Reader in signature")
	}
	if !strings.Contains(got, "myroot.New(stdin,") {
		t.Errorf("lintExpectedCmd: expected myroot.New(stdin, ...), got:\n%s", got)
	}
}

func TestLintExpectedGuard_containsGuard(t *testing.T) {
	got, err := lintExpectedGuard("root")
	if err != nil {
		t.Fatalf("lintExpectedGuard: %v", err)
	}
	for _, want := range []string{
		"// An unmatched token", // the explanatory comment is included
		"r.Command.GetSelected()",
		"sel.Flags.GetArgs()",
		`unknown subcommand %q`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lintExpectedGuard: missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestLintExpectedRoot_stdinPresent(t *testing.T) {
	got, err := lintExpectedRoot("root")
	if err != nil {
		t.Fatalf("lintExpectedRoot: %v", err)
	}
	for _, want := range []string{
		"Stdin   io.Reader",
		"func New(stdin io.Reader,",
		"cfg.Stdin = stdin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lintExpectedRoot: missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestParsedSourceFromTemplate_version(t *testing.T) {
	ps, err := parsedSourceFromTemplate(
		"version.go",
		versionTemplate,
		map[string]string{"ROOT_PKG": "root"},
	)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate(versionTemplate): %v", err)
	}
	// exec is a method, not a top-level func; check top-level functions.
	for _, want := range []string{"New", "gatherVersionInfo", "GetVersionInfoFrom", "WithBuiltBy"} {
		if findFuncDecl(ps.file, want) == nil {
			t.Errorf("func %s not found in parsed versionTemplate", want)
		}
	}
	if !astStructHasField(ps.file, "Config", "JSON") {
		t.Error("Config.JSON field not found in parsed versionTemplate")
	}
}

func TestParsedSourceFromTemplate_man(t *testing.T) {
	ps, err := parsedSourceFromTemplate(
		"man.go",
		manCmdTemplate,
		map[string]string{"ROOT_PKG": "root"},
	)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate(manCmdTemplate): %v", err)
	}
	if !astStructHasField(ps.file, "Config", "Section") {
		t.Error("Config.Section field not found in parsed manCmdTemplate")
	}
	if findFuncDecl(ps.file, "New") == nil {
		t.Error("func New not found in parsed manCmdTemplate")
	}
}

func TestLintExpectedRoot_usesRootPkg(t *testing.T) {
	// The Config struct and New function names are fixed, but the package
	// clause should reflect the supplied rootPkg. extractStructAndFunc does
	// not include the package clause, so verify via the parsedSource directly.
	ps, err := parsedSourceFromTemplate(
		"root.go",
		rootTemplate,
		map[string]string{"ROOT_PKG": "mypkg"},
	)
	if err != nil {
		t.Fatalf("parsedSourceFromTemplate: %v", err)
	}
	if ps.file.Name.Name != "mypkg" {
		t.Errorf("package name: got %q, want %q", ps.file.Name.Name, "mypkg")
	}
}
