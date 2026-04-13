// Package scaffold – lint.go detects structural drift between a user's
// climax-based application and the current scaffold templates, reporting each
// issue in unified-diff format so the caller can display actionable warnings.
package scaffold

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// LintIssue describes a single structural deviation found in a user's climax
// application, relative to the current scaffold templates.
type LintIssue struct {
	// File is the path relative to the application root, e.g. "main.go".
	File string
	// Property is a short human-readable name for the structural property.
	Property string
	// Diff is a unified-diff-style snippet showing found vs expected.
	Diff string
}

// parsedSource holds a parsed Go file and its raw bytes for text extraction.
type parsedSource struct {
	file *ast.File
	fset *token.FileSet
	src  []byte
}

// LintApp analyses the climax application at appDir and returns one LintIssue
// per structural group that deviates from the current scaffold templates.
// Issues are grouped by file/concern so each produces exactly one diff block.
func LintApp(appDir string) ([]LintIssue, error) {
	// analyzeDispatcher uses findDispatcherFile which checks cmd.go, command.go,
	// and falls back to a full directory scan — so dispInfo.path is always the
	// actual file regardless of what the user named it.
	dispInfo, _, err := analyzeDispatcher(appDir)
	if err != nil {
		return nil, fmt.Errorf("reading dispatcher: %w", err)
	}

	// Derive a display-friendly relative path for the dispatcher (e.g.
	// "cmd/cmd.go" or "cmd/command.go") for use in issue labels and diffs.
	cmdRel, err := filepath.Rel(appDir, dispInfo.path)
	if err != nil {
		cmdRel = dispInfo.path // fallback: use absolute if Rel fails
	}

	mainPath := filepath.Join(appDir, "main.go")
	rootPath := filepath.Join(appDir, "cmd", dispInfo.rootPkg, dispInfo.rootPkg+".go")

	mainPS, err := parseSource(mainPath)
	if err != nil {
		return nil, fmt.Errorf("parsing main.go: %w", err)
	}
	cmdPS, err := parseSource(dispInfo.path)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", cmdRel, err)
	}
	rootPS, err := parseSource(rootPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", rootPath, err)
	}

	var issues []LintIssue

	// ── Group 1: main.go shutdown pattern ────────────────────────────────────
	// All three properties (signal, run-separation, stdin) are part of the same
	// pattern; report once if any of them are absent.
	mainHasSignal := astHasCall(mainPS.file, "main", "NotifyContext")
	mainHasRun := astHasFuncDecl(mainPS.file, "run")
	mainHasStdin := astCallPassesIdent(mainPS.file, "run", "cmd", "Run", "Stdin")

	if !mainHasSignal || !mainHasRun || !mainHasStdin {
		found := mainPS.extractFuncs("main", "run")
		expected, err := lintExpectedMain()
		if err != nil {
			return nil, fmt.Errorf("building expected main pattern: %w", err)
		}
		issues = append(issues, LintIssue{
			File:     "main.go",
			Property: "signal-safe shutdown, run() separation, os.Stdin",
			Diff:     buildDiff("main.go", found, expected),
		})
	}

	// ── Group 2: dispatcher Run signature ────────────────────────────────────
	// Only the first two lines (signature + root.New call) are relevant here;
	// extracting the full Run body would make the diff misleadingly large.
	cmdHasStdinParam := astFuncHasIOReaderParam(cmdPS.file, "Run")
	cmdPassesStdin := astCallPassesIdent(cmdPS.file, "Run", dispInfo.rootPkg, "New", "stdin")

	if !cmdHasStdinParam || !cmdPassesStdin {
		found := cmdPS.extractFuncHead("Run", 2)
		expected, err := lintExpectedCmd(dispInfo.rootPkg)
		if err != nil {
			return nil, fmt.Errorf("building expected cmd pattern: %w", err)
		}
		issues = append(issues, LintIssue{
			File:     cmdRel,
			Property: "stdin io.Reader parameter in Run, stdin forwarded to root.New",
			Diff:     buildDiff(cmdRel, found, expected),
		})
	}

	// ── Group 3: cmd/<rootPkg>/<rootPkg>.go stdin support ────────────────────
	rootHasStdinField := astStructHasField(rootPS.file, "Config", "Stdin")
	rootHasStdinParam := astFuncHasIOReaderParam(rootPS.file, "New")
	rootAssignsStdin := astFuncAssignsField(rootPS.file, "New", "Stdin")

	if !rootHasStdinField || !rootHasStdinParam || !rootAssignsStdin {
		found := rootPS.extractStructAndFunc("Config", "New")
		expected, err := lintExpectedRoot(dispInfo.rootPkg)
		if err != nil {
			return nil, fmt.Errorf("building expected root pattern: %w", err)
		}
		rootRel := filepath.Join("cmd", dispInfo.rootPkg, dispInfo.rootPkg+".go")
		issues = append(issues, LintIssue{
			File:     rootRel,
			Property: "Stdin io.Reader field, stdin parameter in New, cfg.Stdin assignment",
			Diff:     buildDiff(rootRel, found, expected),
		})
	}

	return issues, nil
}

// ─── Source extraction ────────────────────────────────────────────────────────

func parseSource(path string) (*parsedSource, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &parsedSource{file: file, fset: fset, src: src}, nil
}

// extractFuncs returns the source text of the named top-level functions,
// separated by a blank line. Functions not found are silently skipped.
func (ps *parsedSource) extractFuncs(names ...string) string {
	var parts []string
	for _, name := range names {
		fn := findFuncDecl(ps.file, name)
		if fn == nil {
			continue
		}
		start := ps.fset.Position(fn.Pos()).Offset
		end := ps.fset.Position(fn.End()).Offset
		if end <= start || end > len(ps.src) {
			continue
		}
		// Capture the full line that starts the function (may start mid-line
		// if there's a comment on the same line — back up to line start).
		lineStart := start
		for lineStart > 0 && ps.src[lineStart-1] != '\n' {
			lineStart--
		}
		parts = append(parts, strings.TrimRight(string(ps.src[lineStart:end]), " \t"))
	}
	return strings.Join(parts, "\n\n")
}

// extractFuncHead returns the first n lines of the named function's source,
// starting from the func declaration line. If the function has fewer than n
// lines (or n ≤ 0), the full function is returned. Useful for diff snippets
// that only need to show the signature and first body statement.
func (ps *parsedSource) extractFuncHead(name string, n int) string {
	fn := findFuncDecl(ps.file, name)
	if fn == nil {
		return ""
	}
	start := ps.fset.Position(fn.Pos()).Offset
	end := min(ps.fset.Position(fn.End()).Offset, len(ps.src))
	lineStart := start
	for lineStart > 0 && ps.src[lineStart-1] != '\n' {
		lineStart--
	}
	text := strings.TrimRight(string(ps.src[lineStart:end]), " \t")
	if n <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// extractStructAndFunc returns the source text of a named struct type
// declaration followed by a named function, separated by a blank line.
func (ps *parsedSource) extractStructAndFunc(structName, funcName string) string {
	var parts []string

	// Find the struct declaration.
	for _, decl := range ps.file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != structName {
				continue
			}
			start := ps.fset.Position(gd.Pos()).Offset
			end := min(ps.fset.Position(gd.End()).Offset, len(ps.src))
			lineStart := start
			for lineStart > 0 && ps.src[lineStart-1] != '\n' {
				lineStart--
			}
			parts = append(parts, strings.TrimRight(string(ps.src[lineStart:end]), " \t"))
		}
	}

	fn := findFuncDecl(ps.file, funcName)
	if fn != nil {
		start := ps.fset.Position(fn.Pos()).Offset
		end := ps.fset.Position(fn.End()).Offset
		if end <= len(ps.src) {
			lineStart := start
			for lineStart > 0 && ps.src[lineStart-1] != '\n' {
				lineStart--
			}
			parts = append(parts, strings.TrimRight(string(ps.src[lineStart:end]), " \t"))
		}
	}

	return strings.Join(parts, "\n\n")
}

// ─── Expected snippets ────────────────────────────────────────────────────────
// These derive the canonical expected text directly from the embedded template
// files by applying dummy substitutions, parsing the result as Go, and
// extracting the relevant declarations. This guarantees lint checks stay in
// sync with the templates automatically.

// parsedSourceFromTemplate applies overrides on top of the default template
// placeholder values, expands tmpl, and parses it as Go source under synName.
func parsedSourceFromTemplate(
	synName, tmpl string,
	overrides map[string]string,
) (*parsedSource, error) {
	// defaults maps every placeholder to a syntactically valid Go value so
	// the template can be parsed with go/parser. Callers supply overrides
	// (e.g. ROOT_PKG) for accurate per-app output.
	defaults := map[string]string{
		"APP_IMPORT":     "example.com/app",
		"APP_NAME":       "app",
		"APP_SHORT":      "an application",
		"ROOT_PKG":       "root",
		"LONG_HELP_LINE": "",
		"VERSION_IMPORT": "",
		"VERSION_CALL":   "",
		"EXEC_IMPORTS":   "",
		"EXEC_BODY":      "\treturn nil\n",
		"PARENT_PKG":     "root",
		"CMD_NAME":       "serve",
		"CMD_FF_NAME":    "serve",
		"CMD_SHORT":      "a command",
		"CMD_LONG":       "A command.",
	}
	merged := make(map[string]string, len(defaults)+len(overrides))
	maps.Copy(merged, defaults)
	maps.Copy(merged, overrides)
	src := []byte(applyVars(tmpl, merged))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, synName, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", synName, err)
	}
	return &parsedSource{file: file, fset: fset, src: src}, nil
}

func lintExpectedMain() (string, error) {
	ps, err := parsedSourceFromTemplate("main.go", mainTemplate, nil)
	if err != nil {
		return "", err
	}
	return ps.extractFuncs("main", "run"), nil
}

func lintExpectedCmd(rootPkg string) (string, error) {
	ps, err := parsedSourceFromTemplate("cmd.go", cmdTemplate, map[string]string{
		"ROOT_PKG": rootPkg,
	})
	if err != nil {
		return "", err
	}
	return ps.extractFuncHead("Run", 2), nil
}

func lintExpectedRoot(rootPkg string) (string, error) {
	ps, err := parsedSourceFromTemplate("root.go", rootTemplate, map[string]string{
		"ROOT_PKG": rootPkg,
	})
	if err != nil {
		return "", err
	}
	return ps.extractStructAndFunc("Config", "New"), nil
}

// ─── Diff builder ─────────────────────────────────────────────────────────────

// buildDiff produces a unified-diff-style block showing found vs expected.
// Lines only in found are prefixed with '-'; lines only in expected with '+'.
// Lines common to both are prefixed with ' '.
func buildDiff(relPath, found, expected string) string {
	foundLines := splitLines(found)
	wantLines := splitLines(expected)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n", relPath)
	fmt.Fprintf(&sb, "+++ b/%s\t(expected per climax template)\n", relPath)
	sb.WriteString("@@ structural pattern @@\n")

	// Compute LCS and emit the diff.
	lcs := longestCommonSubsequence(foundLines, wantLines)
	i, j, k := 0, 0, 0
	for k < len(lcs) {
		for i < len(foundLines) && foundLines[i] != lcs[k] {
			fmt.Fprintf(&sb, "-%s\n", foundLines[i])
			i++
		}
		for j < len(wantLines) && wantLines[j] != lcs[k] {
			fmt.Fprintf(&sb, "+%s\n", wantLines[j])
			j++
		}
		fmt.Fprintf(&sb, " %s\n", lcs[k])
		i++
		j++
		k++
	}
	for i < len(foundLines) {
		fmt.Fprintf(&sb, "-%s\n", foundLines[i])
		i++
	}
	for j < len(wantLines) {
		fmt.Fprintf(&sb, "+%s\n", wantLines[j])
		j++
	}

	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// longestCommonSubsequence returns the LCS of two string slices. Used by
// buildDiff to produce a minimal diff rather than showing all lines as changed.
func longestCommonSubsequence(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			switch {
			case a[i-1] == b[j-1]:
				dp[i][j] = dp[i-1][j-1] + 1
			case dp[i-1][j] >= dp[i][j-1]:
				dp[i][j] = dp[i-1][j]
			default:
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack.
	result := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		switch {
		case a[i-1] == b[j-1]:
			result = append(result, a[i-1])
			i--
			j--
		case dp[i-1][j] >= dp[i][j-1]:
			i--
		default:
			j--
		}
	}
	// Reverse.
	for lo, hi := 0, len(result)-1; lo < hi; lo, hi = lo+1, hi-1 {
		result[lo], result[hi] = result[hi], result[lo]
	}
	return result
}
