package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// TestAddCommand_secondChildOfSameParent pins the case proptest found: the
// first child promotes `a.New(r)` to `aCfg := a.New(r)`, and the second child
// must still find its parent in that captured form.
func TestAddCommand_secondChildOfSameParent(t *testing.T) {
	dir := scaffoldApp(t, "nested")
	const importPrefix = "github.com/example/lintapp"
	if _, _, err := scaffold.AddCommand(dir, "a", importPrefix, scaffold.AddOptions{}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	for _, child := range []string{"b", "c"} {
		if _, _, err := scaffold.AddCommand(dir, child, importPrefix,
			scaffold.AddOptions{Parent: "a"}); err != nil {
			t.Fatalf("add %s under a: %v", child, err)
		}
	}

	src, err := os.ReadFile(filepath.Join(dir, "cmd", "cmd.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aCfg := a.New(r)", "b.New(aCfg)", "c.New(aCfg)"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("cmd.go missing %q:\n%s", want, src)
		}
	}
	if n := strings.Count(string(src), "aCfg :="); n != 1 {
		t.Errorf("aCfg declared %d times, want 1:\n%s", n, src)
	}
}
