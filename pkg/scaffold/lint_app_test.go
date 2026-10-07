package scaffold_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// lintOrFatal runs LintApp and fails the test on error.
func lintOrFatal(t *testing.T, dir string) []scaffold.LintIssue {
	t.Helper()
	issues, err := scaffold.LintApp(dir)
	if err != nil {
		t.Fatalf("LintApp: %v", err)
	}
	return issues
}

// findIssue returns the first issue whose Property contains substr.
func findIssue(issues []scaffold.LintIssue, substr string) (scaffold.LintIssue, bool) {
	for _, iss := range issues {
		if strings.Contains(iss.Property, substr) {
			return iss, true
		}
	}
	return scaffold.LintIssue{}, false
}

// TestLintApp_FeatureSurfaceDrift verifies that a fresh opted-in feature file is
// clean, and that removing a climax-owned symbol surfaces a drift warning.
func TestLintApp_FeatureSurfaceDrift(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{JSONL: true})
	if iss, ok := findIssue(lintOrFatal(t, dir), "feature surface"); ok {
		t.Errorf("unexpected feature-surface issue on a fresh app: %+v", iss)
	}

	// Drop the (final) CodeForError declaration from outcome.go.
	outcomePath := filepath.Join(dir, "cmd", "root", "outcome.go")
	src, err := os.ReadFile(outcomePath)
	if err != nil {
		t.Fatal(err)
	}
	before, _, found := bytes.Cut(src, []byte("// CodeForError"))
	if !found {
		t.Fatal("CodeForError not found in generated outcome.go")
	}
	if err := os.WriteFile(outcomePath, before, 0o644); err != nil {
		t.Fatal(err)
	}

	iss, ok := findIssue(lintOrFatal(t, dir), "feature surface drifted")
	if !ok {
		t.Fatal("expected a feature-surface drift warning")
	}
	if iss.Severity != scaffold.SeverityWarning {
		t.Errorf("surface-drift severity = %v, want Warning", iss.Severity)
	}
	if !strings.Contains(iss.Detail, "CodeForError") {
		t.Errorf("drift detail should name the missing symbol: %q", iss.Detail)
	}
}

// TestLintApp_FeatureSurfaceMissing verifies that deleting an opted-in feature
// file reports it as missing.
func TestLintApp_FeatureSurfaceMissing(t *testing.T) {
	dir := initFeatured(t, scaffold.Features{JSONL: true})
	if err := os.Remove(filepath.Join(dir, "cmd", "root", "outcome.go")); err != nil {
		t.Fatal(err)
	}
	if _, ok := findIssue(lintOrFatal(t, dir), "feature surface missing"); !ok {
		t.Error("expected a missing-surface warning after deleting outcome.go")
	}
}

// TestLintApp_MisplacedFlagUsage verifies that, in a --posix-guard app, a
// command whose exec reads positional args but never calls MisplacedFlag is
// warned about — while the default blank-args stub is not.
func TestLintApp_MisplacedFlagUsage(t *testing.T) {
	const imp = "github.com/example/feat"
	dir := initFeatured(t, scaffold.Features{PosixGuard: true})
	if _, _, err := scaffold.AddCommand(dir, "serve", imp, scaffold.AddOptions{}); err != nil {
		t.Fatalf("AddCommand: %v", err)
	}

	// The default stub takes a blank "_ []string" — no warning.
	if _, ok := findIssue(lintOrFatal(t, dir), "MisplacedFlag"); ok {
		t.Error("blank-args command should not warn about MisplacedFlag")
	}

	// Renaming the positional parameter to args signals it reads positionals.
	servePath := filepath.Join(dir, "cmd", "serve", "serve.go")
	src, err := os.ReadFile(servePath)
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.Replace(string(src),
		"exec(_ context.Context, _ []string)",
		"exec(_ context.Context, args []string)", 1)
	if renamed == string(src) {
		t.Fatal("could not rename the exec positional parameter in the generated stub")
	}
	if err := os.WriteFile(servePath, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}

	iss, ok := findIssue(lintOrFatal(t, dir), "MisplacedFlag")
	if !ok {
		t.Fatal("expected a MisplacedFlag warning for a positional-taking command")
	}
	if iss.Severity != scaffold.SeverityWarning {
		t.Errorf("MisplacedFlag warning severity = %v, want Warning", iss.Severity)
	}
}

// scaffoldApp writes a go.mod and a fresh climax scaffold into a temp dir and
// returns the dir. LintApp only parses source (no compilation), so this is
// fast and needs no module cache.
func scaffoldApp(t *testing.T, short string) string {
	t.Helper()
	dir := t.TempDir()
	const importPrefix = "github.com/example/lintapp"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+importPrefix+"\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.InitApp(dir, scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         "lintapp",
		Short:        short,
		RootPkg:      "root",
	}); err != nil {
		t.Fatalf("InitApp: %v", err)
	}
	return dir
}

// TestLintApp_freshScaffoldIsClean verifies that a freshly generated scaffold
// with real help text reports no issues — the guard is present and no
// placeholder survives.
func TestLintApp_freshScaffoldIsClean(t *testing.T) {
	dir := scaffoldApp(t, "a real description")
	issues, err := scaffold.LintApp(dir)
	if err != nil {
		t.Fatalf("LintApp: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues on a fresh scaffold, got %d: %+v", len(issues), issues)
	}
}

// TestLintApp_missingGuardIsError removes the unmatched-subcommand guard from a
// generated dispatcher and confirms LintApp reports it as an error-severity
// issue (the case that motivated the P0 fix: a typo'd subcommand exits 0).
func TestLintApp_missingGuardIsError(t *testing.T) {
	dir := scaffoldApp(t, "a real description")
	cmdPath := filepath.Join(dir, "cmd", "cmd.go")
	src, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)
	start := strings.Index(content, "\t// An unmatched token")
	end := strings.Index(content, "\tif err := r.Command.Run(ctx); err != nil {")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("could not locate guard block to remove in:\n%s", content)
	}
	if err := os.WriteFile(cmdPath, []byte(content[:start]+content[end:]), 0o644); err != nil {
		t.Fatal(err)
	}

	issues, err := scaffold.LintApp(dir)
	if err != nil {
		t.Fatalf("LintApp: %v", err)
	}
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Property, "unmatched-subcommand guard") {
			found = true
			if iss.Severity != scaffold.SeverityError {
				t.Errorf("guard issue severity = %v, want SeverityError", iss.Severity)
			}
			if !strings.Contains(iss.Detail, "unknown subcommand") {
				t.Errorf("guard issue detail missing suggested block:\n%s", iss.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected an unmatched-subcommand guard issue, got %+v", issues)
	}
}

// TestLintApp_placeholderHelpIsWarning confirms that an un-filled ShortHelp is
// reported as a warning (not an error) so it never fails the build.
func TestLintApp_placeholderHelpIsWarning(t *testing.T) {
	dir := scaffoldApp(t, "") // empty Short defaults to "TODO: describe lintapp here"
	issues, err := scaffold.LintApp(dir)
	if err != nil {
		t.Fatalf("LintApp: %v", err)
	}

	var warn *scaffold.LintIssue
	for i := range issues {
		if issues[i].Severity == scaffold.SeverityError {
			t.Errorf("unexpected error-severity issue alongside a placeholder: %+v", issues[i])
		}
		if issues[i].Severity == scaffold.SeverityWarning {
			warn = &issues[i]
		}
	}
	if warn == nil {
		t.Fatalf("expected a placeholder warning, got %+v", issues)
	}
	if !strings.Contains(warn.Detail, "TODO: describe") {
		t.Errorf("warning detail = %q, want mention of the TODO placeholder", warn.Detail)
	}
}

// TestLintApp_driftDiff checks the diff `climax lint` prints is a faithful
// unified diff of real content: read without its "+" lines it is the user's
// drifted code, read without its "-" lines it is the template's, and the
// unchanged lines between are shown once as context.
func TestLintApp_driftDiff(t *testing.T) {
	t.Parallel()
	pristine := scaffoldApp(t, "a drift-diff app")
	drifted := scaffoldApp(t, "a drift-diff app")
	mainPath := filepath.Join(drifted, "main.go")
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(src, []byte("signal.NotifyContext("), []byte("withoutSignals("), 1)
	if bytes.Equal(changed, src) {
		t.Fatal("main.go template no longer calls signal.NotifyContext")
	}
	if err := os.WriteFile(mainPath, changed, 0o644); err != nil {
		t.Fatal(err)
	}

	iss, ok := findIssue(lintOrFatal(t, drifted), "signal-safe shutdown")
	if !ok {
		t.Fatal("expected a main.go drift issue")
	}
	found, expected, context := diffSides(t, iss.Detail)

	if !strings.Contains(strings.Join(found, "\n"), "withoutSignals(") ||
		strings.Contains(strings.Join(found, "\n"), "signal.NotifyContext(") {
		t.Errorf("found side should show the drifted call only:\n%s", iss.Detail)
	}
	if !strings.Contains(strings.Join(expected, "\n"), "signal.NotifyContext(") ||
		strings.Contains(strings.Join(expected, "\n"), "withoutSignals(") {
		t.Errorf("expected side should show the template's call only:\n%s", iss.Detail)
	}
	if context < 2 {
		t.Errorf("got %d context lines, want the shared lines shown once:\n%s", context, iss.Detail)
	}
	pristineSrc, err := os.ReadFile(filepath.Join(pristine, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	assertSubsequence(t, "found side of the diff", found, string(changed))
	assertSubsequence(t, "expected side of the diff", expected, string(pristineSrc))
}

// diffSides splits a buildDiff body into the lines of each side (context lines
// belong to both) and counts the context lines. The three header lines are
// skipped.
func diffSides(t *testing.T, detail string) (found, expected []string, context int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(detail, "\n"), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "--- ") ||
		!strings.HasPrefix(lines[1], "+++ ") {
		t.Fatalf("not a unified diff:\n%s", detail)
	}
	for _, l := range lines[3:] {
		switch {
		case strings.HasPrefix(l, " "):
			found = append(found, l[1:])
			expected = append(expected, l[1:])
			context++
		case strings.HasPrefix(l, "-"):
			found = append(found, l[1:])
		case strings.HasPrefix(l, "+"):
			expected = append(expected, l[1:])
		default:
			t.Fatalf("diff line %q has no -, + or space prefix:\n%s", l, detail)
		}
	}
	return found, expected, context
}

// assertSubsequence fails unless lines appear in text as whole lines, in order.
func assertSubsequence(t *testing.T, what string, lines []string, text string) {
	t.Helper()
	have := strings.Split(text, "\n")
	i := 0
	for _, l := range lines {
		for i < len(have) && have[i] != l {
			i++
		}
		if i == len(have) {
			t.Errorf("%s: line %q is missing or out of order", what, l)
			return
		}
		i++
	}
}
