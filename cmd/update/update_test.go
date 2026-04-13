// Package update_test provides black-box tests for the update command.
package update_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/cmd/update"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// ─── printReport tests ────────────────────────────────────────────────────────

// TestPrintReport_fixableOnly verifies the output when only source-has items
// are present, with a non-zero auto-fix count.
func TestPrintReport_fixableOnly(t *testing.T) {
	t.Parallel()
	fixable := []scaffold.DriftItem{
		{
			Template:   "version",
			Property:   "JSON flag in Config",
			InSource:   "present",
			InTemplate: "absent",
		},
		{
			Template:   "main",
			Property:   "signal.NotifyContext",
			InSource:   "present",
			InTemplate: "absent",
		},
	}
	var out strings.Builder
	update.PrintReport(&out, 2, 1, fixable, nil)
	got := out.String()

	if !strings.Contains(got, "Drift detected: 2 item(s)") {
		t.Errorf("missing total count: %q", got)
	}
	if !strings.Contains(got, "(1 auto-fixable with --apply)") {
		t.Errorf("missing auto-fixable count: %q", got)
	}
	if !strings.Contains(got, "JSON flag in Config") {
		t.Errorf("missing fixable property: %q", got)
	}
	if strings.Contains(got, "Requires manual review") {
		t.Errorf("unexpected manual review section: %q", got)
	}
}

// TestPrintReport_manualOnly verifies the output when only template-has items
// are present (auto-fix count 0).
func TestPrintReport_manualOnly(t *testing.T) {
	t.Parallel()
	manual := []scaffold.DriftItem{
		{
			Template:   "version",
			Property:   "tabwriter output",
			InSource:   "absent",
			InTemplate: "present",
		},
	}
	var out strings.Builder
	update.PrintReport(&out, 1, 0, nil, manual)
	got := out.String()

	if !strings.Contains(got, "Drift detected: 1 item(s)") {
		t.Errorf("missing total count: %q", got)
	}
	if strings.Contains(got, "auto-fixable") {
		t.Errorf("unexpected auto-fixable mention when count is 0: %q", got)
	}
	if !strings.Contains(got, "Requires manual review") {
		t.Errorf("missing manual review section: %q", got)
	}
	if !strings.Contains(got, "tabwriter output") {
		t.Errorf("missing property name: %q", got)
	}
}

// TestPrintReport_mixed verifies that fixable items appear with ✗ and manual
// review items appear in the separate ⚠ section.
func TestPrintReport_mixed(t *testing.T) {
	t.Parallel()
	fixable := []scaffold.DriftItem{
		{
			Template:   "version",
			Property:   "JSON flag in Config",
			InSource:   "present",
			InTemplate: "absent",
		},
	}
	manual := []scaffold.DriftItem{
		{
			Template:   "root",
			Property:   "Stdin io.Reader field in Config",
			InSource:   "absent",
			InTemplate: "present",
		},
	}
	var out strings.Builder
	update.PrintReport(&out, 2, 1, fixable, manual)
	got := out.String()

	if !strings.Contains(got, "Drift detected: 2 item(s)") {
		t.Errorf("missing total count: %q", got)
	}
	if !strings.Contains(got, "(1 auto-fixable with --apply)") {
		t.Errorf("missing auto-fixable count: %q", got)
	}
	if !strings.Contains(got, "JSON flag in Config") {
		t.Errorf("missing fixable property: %q", got)
	}
	if !strings.Contains(got, "Requires manual review") {
		t.Errorf("missing manual review section: %q", got)
	}
	if !strings.Contains(got, "Stdin io.Reader field in Config") {
		t.Errorf("missing manual property: %q", got)
	}
}

// TestPrintReport_noAutoFix_omitsLabel verifies that the "(N auto-fixable)"
// label is omitted when autoFixCount is 0, even if fixable items are present.
// This covers the case where source-has/template-lacks items exist but none
// have an auto-fix patch defined (e.g. tabwriter, GetVersionInfoFrom).
func TestPrintReport_noAutoFix_omitsLabel(t *testing.T) {
	t.Parallel()
	fixable := []scaffold.DriftItem{
		{
			Template:   "version",
			Property:   "tabwriter output",
			InSource:   "present",
			InTemplate: "absent",
		},
		{
			Template:   "version",
			Property:   "GetVersionInfoFrom function",
			InSource:   "present",
			InTemplate: "absent",
		},
	}
	var out strings.Builder
	update.PrintReport(&out, 2, 0, fixable, nil)
	got := out.String()

	if strings.Contains(got, "auto-fixable") {
		t.Errorf("unexpected auto-fixable label when count is 0: %q", got)
	}
	if !strings.Contains(got, "tabwriter output") ||
		!strings.Contains(got, "GetVersionInfoFrom function") {
		t.Errorf("missing property names: %q", got)
	}
}

// ─── Command execution tests ──────────────────────────────────────────────────

// runUpdateCommand constructs a root config with captured I/O, registers the
// update command, and runs it with the given path argument.
func runUpdateCommand(t *testing.T, path string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf strings.Builder
	parent := root.New(strings.NewReader(""), &outBuf, &errBuf)
	update.New(parent)
	if parseErr := parent.Command.Parse([]string{"update", path}); parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}
	err = parent.Command.Run(context.Background())
	return outBuf.String(), errBuf.String(), err
}

// TestUpdateCommand_wrongModule verifies that the update command rejects a
// directory whose go.mod declares a module other than the climax module.
func TestUpdateCommand_wrongModule(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(tmpDir, "go.mod"),
		[]byte("module example.com/myapp\n\ngo 1.23\n"),
		0o644,
	); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	_, errOut, err := runUpdateCommand(t, tmpDir)

	if err == nil {
		t.Fatal("expected error for wrong module, got nil")
	}
	if !strings.Contains(err.Error(), "climax module") {
		t.Errorf("error should mention 'climax module', got: %v", err)
	}
	if !strings.Contains(errOut, "example.com/myapp") {
		t.Errorf("stderr should echo found module name, got: %q", errOut)
	}
}

// TestUpdateCommand_cleanSource_noDrift verifies that the update command
// reports no drift when run against the real climax source (the module root
// two directories up from this test file).
func TestUpdateCommand_cleanSource_noDrift(t *testing.T) {
	t.Parallel()
	climaxRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	out, _, runErr := runUpdateCommand(t, climaxRoot)

	if runErr != nil {
		t.Fatalf("expected no error for clean climax source, got: %v", runErr)
	}
	if !strings.Contains(out, "No drift detected") {
		t.Errorf("expected 'No drift detected', got: %q", out)
	}
}
