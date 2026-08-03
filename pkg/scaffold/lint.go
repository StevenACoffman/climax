// Package scaffold – lint.go detects structural drift between a user's
// climax-based application and the current scaffold templates, reporting each
// issue in unified-diff format so the caller can display actionable warnings.
package scaffold

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// SeverityError marks structural drift from the scaffold templates. Any
	// error-severity issue makes `climax lint` exit non-zero.
	SeverityError Severity = iota
	// SeverityWarning marks an advisory issue (e.g. an un-filled placeholder)
	// that is reported but does not change the exit code.
	SeverityWarning
)

// Severity classifies a LintIssue as a hard error (structural drift that breaks
// the scaffold contract) or an advisory warning.
type Severity int

// LintIssue describes a single structural deviation found in a user's climax
// application, relative to the current scaffold templates.
type LintIssue struct {
	// File is the path relative to the application root, e.g. "main.go".
	File string
	// Property is a short human-readable name for the structural property.
	Property string
	// Severity is Error for scaffold-contract drift, Warning for advisories.
	Severity Severity
	// Detail is a unified-diff snippet (for drift) or a human-readable message
	// (for warnings) explaining the issue.
	Detail string
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
			Severity: SeverityError,
			Detail:   buildDiff("main.go", found, expected),
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
			Severity: SeverityError,
			Detail:   buildDiff(cmdRel, found, expected),
		})
	}

	// ── Group 2b: dispatcher unmatched-subcommand guard ──────────────────────
	// The surrounding error handling already calls GetSelected(), so the guard's
	// GetArgs() call is the reliable signal that the block is present. Without
	// the guard, a typo'd subcommand selects the root group parent and exits 0.
	if !astHasCall(cmdPS.file, "Run", "GetArgs") {
		expected, err := lintExpectedGuard(dispInfo.rootPkg)
		if err != nil {
			return nil, fmt.Errorf("building expected guard pattern: %w", err)
		}
		issues = append(issues, LintIssue{
			File:     cmdRel,
			Property: "unmatched-subcommand guard (a mistyped subcommand otherwise exits 0)",
			Severity: SeverityError,
			Detail:   buildDiff(cmdRel, "", expected),
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
			Severity: SeverityError,
			Detail:   buildDiff(rootRel, found, expected),
		})
	}

	// ── Placeholder help text (warnings) ─────────────────────────────────────
	placeholderIssues, err := findPlaceholderHelp(appDir)
	if err != nil {
		return nil, err
	}
	issues = append(issues, placeholderIssues...)

	// ── Opted-in feature surface (warnings) ──────────────────────────────────
	features := parseFeatureMarker(dispInfo.features)
	surfaceIssues, err := lintFeatureSurface(appDir, dispInfo.rootPkg, features)
	if err != nil {
		return nil, err
	}
	issues = append(issues, surfaceIssues...)

	// When the app opts into --posix-guard, flag positional-taking commands that
	// never call the MisplacedFlag helper they generated.
	if features["posix-guard"] {
		guardIssues, err := lintMisplacedFlagUsage(appDir)
		if err != nil {
			return nil, err
		}
		issues = append(issues, guardIssues...)
	}

	return issues, nil
}

// lintMisplacedFlagUsage warns about command files whose exec reads positional
// arguments but never calls MisplacedFlag — a flag written after a positional
// (e.g. "cmd path --flag") would then be silently swallowed by ff/v4.
func lintMisplacedFlagUsage(appDir string) ([]LintIssue, error) {
	cmdDir := filepath.Join(appDir, "cmd")
	var issues []LintIssue
	err := filepath.WalkDir(cmdDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !commandNeedsMisplacedGuard(path) {
			return nil
		}
		rel, err := filepath.Rel(appDir, path)
		if err != nil {
			rel = path // fallback: use absolute if Rel fails
		}
		issues = append(issues, LintIssue{
			File:     rel,
			Property: "positional-taking command does not call MisplacedFlag",
			Severity: SeverityWarning,
			Detail: "exec reads positional args but never calls MisplacedFlag; a flag written " +
				"after a positional (e.g. `cmd path --flag`) would be silently ignored",
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning for MisplacedFlag usage: %w", err)
	}
	return issues, nil
}

// commandNeedsMisplacedGuard reports whether the Go file at path is a command
// whose exec method reads named positional args but never calls MisplacedFlag.
// Unparseable and non-command files return false.
func commandNeedsMisplacedGuard(path string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false
	}
	execFn := findExecMethod(file)
	if execFn == nil || !execReadsPositionals(execFn) {
		return false
	}
	return !nodeContainsCall(file, "MisplacedFlag")
}

// findExecMethod returns the file's exec method (the command's Exec function),
// or nil if there is none.
func findExecMethod(file *ast.File) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Recv != nil && fd.Name.Name == "exec" && fd.Body != nil {
			return fd
		}
	}
	return nil
}

// execReadsPositionals reports whether fn has a []string parameter that is
// named (not the blank identifier) — the scaffold's convention that the command
// reads its positional arguments.
func execReadsPositionals(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		arr, ok := field.Type.(*ast.ArrayType)
		if !ok || arr.Len != nil {
			continue
		}
		if elt, ok := arr.Elt.(*ast.Ident); !ok || elt.Name != "string" {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				return true
			}
		}
	}
	return false
}

// lintFeatureSurface checks each opted-in file-generating feature (per the
// // climax:features marker) against its current template: the app's file must
// declare every top-level symbol the template does. It reports warnings — the
// files are user-owned and customizable, so drift is advisory, not fatal.
// Splice features (getenv, global-flags) have no standalone file and are not
// surface-checked here.
func lintFeatureSurface(appDir, rootPkg string, features map[string]bool) ([]LintIssue, error) {
	surfaces := []struct {
		feature  string
		filename string
		tmpl     string
	}{
		{"jsonl", "outcome.go", outcomeTemplate},
		{"logger", "logger.go", loggerTemplate},
		{"posix-guard", "misplaced.go", misplacedTemplate},
	}

	var issues []LintIssue
	for _, s := range surfaces {
		if !features[s.feature] {
			continue
		}
		rel := filepath.Join("cmd", rootPkg, s.filename)
		userPS, err := parseSource(filepath.Join(appDir, rel))
		if err != nil {
			issues = append(issues, LintIssue{
				File:     rel,
				Property: "opted-in feature surface missing",
				Severity: SeverityWarning,
				Detail: fmt.Sprintf(
					"app opted into %q (per // climax:features) but %s is missing or unparseable",
					s.feature, rel),
			})
			continue
		}
		tmplPS, err := parsedSourceFromTemplate(
			s.filename,
			s.tmpl,
			map[string]string{"ROOT_PKG": rootPkg},
		)
		if err != nil {
			return nil, fmt.Errorf("parsing %s template: %w", s.feature, err)
		}
		missing := missingNames(declaredNames(tmplPS.file), declaredNames(userPS.file))
		if len(missing) > 0 {
			issues = append(issues, LintIssue{
				File:     rel,
				Property: "opted-in feature surface drifted from template",
				Severity: SeverityWarning,
				Detail: fmt.Sprintf(
					"%s is missing symbols the current %q template generates: %s",
					rel, s.feature, strings.Join(missing, ", ")),
			})
		}
	}
	return issues, nil
}

// declaredNames returns the set of top-level declaration names in file:
// functions and methods (by name), types, and const/var identifiers.
func declaredNames(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names[n.Name] = true
					}
				}
			}
		}
	}
	return names
}

// missingNames returns the sorted names present in want but absent from have.
func missingNames(want, have map[string]bool) []string {
	var missing []string
	for name := range want {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// findPlaceholderHelp scans every non-test .go file under cmd/ for ff.Command
// literals whose ShortHelp/LongHelp still contain generated placeholder text,
// returning one warning-severity LintIssue per offending file. Un-filled help
// reaches users verbatim, so this is a cheap guard against shipping the
// scaffold defaults; it never fails the build.
func findPlaceholderHelp(appDir string) ([]LintIssue, error) {
	cmdDir := filepath.Join(appDir, "cmd")
	var issues []LintIssue
	err := filepath.WalkDir(cmdDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		hits, err := placeholderHelpInFile(path)
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			return nil
		}
		rel, err := filepath.Rel(appDir, path)
		if err != nil {
			rel = path // fallback: use absolute if Rel fails
		}
		issues = append(issues, LintIssue{
			File:     rel,
			Property: "un-filled scaffold help text",
			Severity: SeverityWarning,
			Detail:   strings.Join(hits, "\n"),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning for placeholder help: %w", err)
	}
	return issues, nil
}

// placeholderHelpInFile parses one Go file and returns a message for each
// ShortHelp/LongHelp value in an ff.Command literal that still contains a
// scaffold placeholder.
func placeholderHelpInFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	// placeholders lists substrings that mark un-filled scaffold help text: a
	// ShortHelp/LongHelp still holding one of these was generated but never
	// described. Extend this list as scaffold defaults change.
	placeholders := []string{
		"TODO: describe",   // `climax init` default ShortHelp
		"is a new command", // `climax add` default LongHelp
		"APP_SHORT",        // unsubstituted root ShortHelp placeholder
		"APP_LONG",         // unsubstituted root LongHelp placeholder
		"CMD_SHORT",        // unsubstituted command ShortHelp placeholder
		"CMD_LONG",         // unsubstituted command LongHelp placeholder
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isCommandLit(lit) {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || (key.Name != "ShortHelp" && key.Name != "LongHelp") {
				continue
			}
			bl, ok := kv.Value.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				continue
			}
			val, err := strconv.Unquote(bl.Value)
			if err != nil {
				continue
			}
			for _, ph := range placeholders {
				if strings.Contains(val, ph) {
					hits = append(
						hits,
						fmt.Sprintf("%s still contains placeholder %q", key.Name, ph),
					)
				}
			}
		}
		return true
	})
	return hits, nil
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

// extractIfStmtWithCall returns the source text of the first top-level if
// statement inside the named function whose subtree contains a call to callName,
// including the contiguous //-comment block immediately above it. Returns ""
// when no such statement is found.
func (ps *parsedSource) extractIfStmtWithCall(funcName, callName string) string {
	fn := findFuncDecl(ps.file, funcName)
	if fn == nil {
		return ""
	}
	for _, stmt := range fn.Body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || !nodeContainsCall(ifStmt, callName) {
			continue
		}
		start := ps.fset.Position(ifStmt.Pos()).Offset
		end := min(ps.fset.Position(ifStmt.End()).Offset, len(ps.src))
		// Back up to the guard's own line start, then over the explanatory
		// comment block above it so the diff shows the comment too.
		lineStart := start
		for lineStart > 0 && ps.src[lineStart-1] != '\n' {
			lineStart--
		}
		lineStart = absorbLeadingComments(ps.src, lineStart)
		return strings.TrimRight(string(ps.src[lineStart:end]), " \t")
	}
	return ""
}

// nodeContainsCall reports whether n's subtree contains a call whose selector
// matches callName, e.g. "GetArgs" matches sel.Flags.GetArgs().
func nodeContainsCall(n ast.Node, callName string) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == callName {
			found = true
			return false
		}
		return true
	})
	return found
}

// absorbLeadingComments returns the offset of the first byte of the contiguous
// block of //-comment lines (ignoring indentation) immediately preceding the
// line that starts at off. If there are none, off is returned unchanged.
func absorbLeadingComments(src []byte, off int) int {
	for off > 0 {
		prevEnd := off - 1 // the '\n' terminating the previous line
		prevStart := prevEnd
		for prevStart > 0 && src[prevStart-1] != '\n' {
			prevStart--
		}
		if !strings.HasPrefix(strings.TrimSpace(string(src[prevStart:prevEnd])), "//") {
			break
		}
		off = prevStart
	}
	return off
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
		"APP_IMPORT":        "example.com/app",
		"APP_NAME":          "app",
		"APP_ENV_PREFIX":    "APP",
		"APP_SHORT":         "an application",
		"ROOT_PKG":          "root",
		"LONG_HELP_LINE":    "",
		"VERSION_IMPORT":    "",
		"VERSION_CALL":      "",
		"EXEC_IMPORTS":      "",
		"EXEC_BODY":         "\treturn nil\n",
		"PARENT_PKG":        "root",
		"CMD_NAME":          "serve",
		"CMD_FF_NAME":       "serve",
		"CMD_SHORT":         "a command",
		"CMD_LONG":          "A command.",
		"MAN_SECTION":       "1",
		"MAN_WITH_SECTIONS": "",
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

// lintExpectedGuard derives the canonical unmatched-subcommand guard block
// (with its explanatory comment) from the dispatcher template, so the lint
// suggestion always matches what `climax init` would generate today.
func lintExpectedGuard(rootPkg string) (string, error) {
	ps, err := parsedSourceFromTemplate("cmd.go", cmdTemplate, map[string]string{
		"ROOT_PKG": rootPkg,
	})
	if err != nil {
		return "", err
	}
	return ps.extractIfStmtWithCall("Run", "GetArgs"), nil
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
