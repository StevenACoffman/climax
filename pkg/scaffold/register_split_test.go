package scaffold_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestAddCommand_RegisterSplit verifies that AddCommand keeps registrations
// inline in Run up to the threshold, then extracts them into a register()
// function, and that later adds continue registering inside register().
func TestAddCommand_RegisterSplit(t *testing.T) {
	dir := t.TempDir()
	const imp = "github.com/example/big"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+imp+"\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: imp,
		Name:         "big",
		Short:        "a big app",
	}); err != nil {
		t.Fatalf("InitApp: %v", err)
	}

	cmdPath := filepath.Join(dir, "cmd", "cmd.go")
	add := func(name string) {
		t.Helper()
		if _, _, err := scaffold.AddCommand(dir, name, imp, scaffold.AddOptions{}); err != nil {
			t.Fatalf("AddCommand(%s): %v", name, err)
		}
	}

	// init registers version (1). Seven adds → eight inline registrations, still
	// at (not past) the threshold, so no split yet.
	for i := 1; i <= 7; i++ {
		add(fmt.Sprintf("cmd%d", i))
	}
	if got := readFile(t, cmdPath); strings.Contains(got, "func register(") {
		t.Fatalf(
			"dispatcher split at 8 inline registrations; expected split only past the threshold",
		)
	}

	// The eighth add pushes past the threshold and triggers the split.
	add("cmd8")
	got := readFile(t, cmdPath)
	assertParses(t, cmdPath)
	if !strings.Contains(got, "func register(r *root.Config) {") {
		t.Error("expected a register(r *root.Config) function after the split")
	}
	if !strings.Contains(got, "\tregister(r)\n") {
		t.Error("Run should call register(r) after the split")
	}
	if !strings.Contains(got, "cmd8.New(r)") {
		t.Error("cmd8 not registered after the split")
	}
	// The marker moved into register(), so version and the earlier commands live
	// there too.
	regStart := strings.Index(got, "func register(")
	if regStart < 0 || !strings.Contains(got[regStart:], "version.New(r)") {
		t.Error("register() should contain the moved version.New(r) call")
	}
	if !strings.Contains(got[regStart:], scaffold.CommandsMarker) {
		t.Error("the // register new commands here marker should move into register()")
	}

	// A further add stays split (no second register func) and registers inside it.
	add("cmd9")
	got = readFile(t, cmdPath)
	assertParses(t, cmdPath)
	if n := strings.Count(got, "func register("); n != 1 {
		t.Errorf("expected exactly one register() function, got %d", n)
	}
	if !strings.Contains(got, "cmd9.New(r)") {
		t.Error("cmd9 not registered after the split")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func assertParses(t *testing.T, path string) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), path, nil, 0); err != nil {
		t.Errorf("%s does not parse: %v", path, err)
	}
}
