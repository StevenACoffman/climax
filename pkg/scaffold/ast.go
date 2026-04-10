// Package scaffold generates Climax application and command files.
// This file contains all AST-based analysis used to locate and modify the
// dispatcher file without relying solely on embedded text markers.
package scaffold

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dispatcherInfo holds everything AddCommand needs about the dispatcher file.
// When markerBased is true registerInCmdGo uses text-marker replacement.
// When false it uses byte-offset insertion derived from AST analysis.
type dispatcherInfo struct {
	path    string // absolute path to the dispatcher file
	rootPkg string // local package name of the root Config, e.g. "root"
	rootVar string // variable holding root Config in Run, e.g. "r"
	cliName string // ff.Command.Name for root; "" means derive from import path

	markerBased bool // true when both text markers are present

	// Populated only when !markerBased:
	importEndOffset  int // byte offset of the import group's closing ')'
	callInsertOffset int // byte offset of the r.Command.Parse IfStmt — insert before
}

// parentCallResult describes a <pkg>.New(...) statement found in func Run.
type parentCallResult struct {
	isCaptured bool   // true when already an AssignStmt (captured return value)
	cfgVar     string // LHS variable name: existing name or generated "<pkg>Cfg"
	callStart  int    // byte offset of the call expression (used for promotion)
	lineEnd    int    // byte offset of the first byte of the next line
}

// findDispatcherFile locates the Go source file in cmd/ that acts as the
// climax dispatcher. It checks canonical names first, then falls back to a
// full directory scan. A file is accepted if it has both text markers or
// matches the AST structural pattern (func Run with the climax shape).
func findDispatcherFile(dir string) (string, []byte, error) {
	cmdDir := filepath.Join(dir, "cmd")

	// Fast path: canonical names.
	for _, name := range []string{"cmd.go", "command.go"} {
		p := filepath.Join(cmdDir, name)
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if looksLikeDispatcher(p, src) {
			return p, src, nil
		}
	}

	// Fallback: scan all .go files directly in cmd/.
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", cmdDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		p := filepath.Join(cmdDir, e.Name())
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if looksLikeDispatcher(p, src) {
			return p, src, nil
		}
	}

	return "", nil, errors.New(
		"no climax dispatcher found in cmd/ (checked for text markers and func Run pattern)",
	)
}

// looksLikeDispatcher returns true if src contains both text markers or
// passes the AST structural check for a climax dispatcher.
func looksLikeDispatcher(path string, src []byte) bool {
	content := string(src)
	if strings.Contains(content, ImportsMarker) && strings.Contains(content, CommandsMarker) {
		return true
	}
	_, err := probeDispatcherAST(path, src)
	return err == nil
}

// analyzeDispatcher locates the dispatcher file in dir and returns its fully
// analysed dispatcherInfo together with the raw source bytes.
//
// Marker path (fast): both markers present → text-replacement mode.
// AST path (fallback): one or both markers missing → byte-offset mode.
//
// Regardless of which path is taken, the AST is always probed so that rootVar
// is available to both modes.
func analyzeDispatcher(dir string) (*dispatcherInfo, []byte, error) {
	path, src, err := findDispatcherFile(dir)
	if err != nil {
		return nil, nil, err
	}

	content := string(src)
	info := &dispatcherInfo{
		path:    path,
		rootPkg: readMarker(content, "// climax:root-pkg"),
		cliName: readMarker(content, "// climax:name"),
	}
	if info.rootPkg == "" {
		info.rootPkg = "root"
	}

	// Always probe the AST — it's a fast parse with no type-checking.
	// This gives us rootVar and insertion offsets regardless of marker state.
	astInfo, astErr := probeDispatcherAST(path, src)

	hasImports := strings.Contains(content, ImportsMarker)
	hasCommands := strings.Contains(content, CommandsMarker)

	if hasImports && hasCommands {
		info.markerBased = true
		if astErr == nil {
			// AST is authoritative: it sees the actual variable names in the source.
			info.rootPkg = astInfo.rootPkg
			info.rootVar = astInfo.rootVar
		} else {
			// AST probe failed (e.g. parse error after manual edits): use the
			// template default. Generated files always use "r".
			info.rootVar = "r"
		}
		return info, src, nil
	}

	// One or both markers are missing: require a successful AST probe.
	if astErr != nil {
		return nil, nil, fmt.Errorf("analyzing %s: %w", path, astErr)
	}
	info.rootPkg = astInfo.rootPkg
	info.rootVar = astInfo.rootVar
	info.importEndOffset = astInfo.importEndOffset
	info.callInsertOffset = astInfo.callInsertOffset
	info.markerBased = false
	return info, src, nil
}

// probeDispatcherAST parses path and validates the climax dispatcher pattern
// without relying on comment markers.
// It returns a partially-filled dispatcherInfo (no path, cliName, or markerBased set).
func probeDispatcherAST(path string, src []byte) (*dispatcherInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	info := &dispatcherInfo{}

	// (a) Parenthesized import group — needed for byte-offset import insertion.
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT || gd.Rparen == token.NoPos {
			continue
		}
		info.importEndOffset = fset.Position(gd.Rparen).Offset
		break
	}
	if info.importEndOffset == 0 {
		return nil, fmt.Errorf("%s: no parenthesized import group", path)
	}

	// (b) Top-level func Run — the climax dispatcher signature.
	runFunc := findRunFunc(file)
	if runFunc == nil {
		return nil, fmt.Errorf("%s: no top-level func Run", path)
	}

	// (c) First short variable declaration of the form <var> := <pkg>.New(...)
	// identifies rootPkg and rootVar.
	info.rootPkg, info.rootVar = extractRootFromBody(runFunc.Body.List)
	if info.rootPkg == "" {
		return nil, fmt.Errorf("%s: no root config assignment found in func Run", path)
	}

	// (d) First IfStmt whose Init assigns from <rootVar>.Command.Parse(...)
	// is the fence before which new command registrations are inserted.
	info.callInsertOffset = findParseCallOffset(fset, runFunc.Body.List, info.rootVar)
	if info.callInsertOffset == 0 {
		return nil, fmt.Errorf(
			"%s: %s.Command.Parse call not found in func Run",
			path,
			info.rootVar,
		)
	}

	return info, nil
}

// findRunFunc returns the first top-level func Run declaration in file, or nil.
func findRunFunc(file *ast.File) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "Run" && fd.Recv == nil && fd.Body != nil {
			return fd
		}
	}
	return nil
}

// extractRootFromBody scans stmts for the first short variable declaration of
// the form <var> := <pkg>.New(...) and returns (pkg, var).
// This matches the generated pattern: r := root.New(stdout, stderr).
func extractRootFromBody(stmts []ast.Stmt) (rootPkg, rootVar string) {
	for _, stmt := range stmts {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE ||
			len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" {
			continue
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		varIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			continue
		}
		return pkgIdent.Name, varIdent.Name
	}
	return "", ""
}

// findParseCallOffset returns the byte offset of the first IfStmt in stmts
// whose Init is an assignment from <rootVar>.Command.Parse(...).
// New command registrations are inserted before this statement.
func findParseCallOffset(fset *token.FileSet, stmts []ast.Stmt, rootVar string) int {
	for _, stmt := range stmts {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || ifStmt.Init == nil {
			continue
		}
		assign, ok := ifStmt.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			continue
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		if isCommandMethodCall(call, rootVar, "Parse") {
			return fset.Position(stmt.Pos()).Offset
		}
	}
	return 0
}

// isCommandMethodCall returns true if call matches <rootVar>.Command.<method>(...).
func isCommandMethodCall(call *ast.CallExpr, rootVar, method string) bool {
	outer, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || outer.Sel.Name != method {
		return false
	}
	inner, ok := outer.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "Command" {
		return false
	}
	ident, ok := inner.X.(*ast.Ident)
	return ok && ident.Name == rootVar
}

// findParentCallInAST parses src and locates the statement in func Run that
// calls <parentPkg>.New(...). It returns the information needed to optionally
// promote a bare expression statement to a captured assignment and to insert
// a child command call immediately after it.
func findParentCallInAST(path string, src []byte, parentPkg string) (*parentCallResult, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	runFunc := findRunFunc(file)
	if runFunc == nil {
		return nil, fmt.Errorf("%s: no func Run", path)
	}

	for _, stmt := range runFunc.Body.List {
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			// Bare call: parentPkg.New(r) — needs promotion to a captured assignment.
			call, ok := s.X.(*ast.CallExpr)
			if !ok || !isNewCall(call, parentPkg) {
				continue
			}
			return &parentCallResult{
				isCaptured: false,
				cfgVar:     parentPkg + "Cfg",
				callStart:  fset.Position(s.X.Pos()).Offset,
				lineEnd:    stmtLineEnd(fset, s, src),
			}, nil

		case *ast.AssignStmt:
			// Captured: <cfgVar> := parentPkg.New(r) — already promoted.
			if len(s.Rhs) != 1 {
				continue
			}
			call, ok := s.Rhs[0].(*ast.CallExpr)
			if !ok || !isNewCall(call, parentPkg) {
				continue
			}
			if len(s.Lhs) != 1 {
				continue
			}
			lhsIdent, ok := s.Lhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			return &parentCallResult{
				isCaptured: true,
				cfgVar:     lhsIdent.Name,
				lineEnd:    stmtLineEnd(fset, s, src),
			}, nil
		}
	}

	return nil, fmt.Errorf("parent command %q not found in func Run", parentPkg)
}

// isNewCall returns true if call is <pkg>.New(...).
func isNewCall(call *ast.CallExpr, pkg string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

// stmtLineEnd returns the byte offset of the first byte AFTER the newline
// that terminates stmt in src. If src ends without a newline it returns len(src).
func stmtLineEnd(fset *token.FileSet, stmt ast.Stmt, src []byte) int {
	off := fset.Position(stmt.End()).Offset
	for off < len(src) && src[off] != '\n' {
		off++
	}
	if off < len(src) {
		off++ // step past the '\n'
	}
	return off
}

// extractCLIName parses cmd/<rootPkg>/<rootPkg>.go and returns the Name field
// from the first ff.Command composite literal. Returns "" on any failure.
func extractCLIName(dir, rootPkg string) string {
	p := filepath.Join(dir, "cmd", rootPkg, rootPkg+".go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, nil, 0)
	if err != nil {
		return ""
	}
	var name string
	ast.Inspect(file, func(n ast.Node) bool {
		if name != "" {
			return false
		}
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
			if !ok || key.Name != "Name" {
				continue
			}
			bl, ok := kv.Value.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				continue
			}
			name, _ = strconv.Unquote(bl.Value)
		}
		return true
	})
	return name
}

// isCommandLit returns true if lit's type is <pkg>.Command or *<pkg>.Command,
// matching ff.Command composite literals in root config files.
func isCommandLit(lit *ast.CompositeLit) bool {
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "Command"
	case *ast.StarExpr:
		if sel, ok := t.X.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "Command"
		}
	}
	return false
}

// insertAt returns a new byte slice with text inserted at byte offset.
func insertAt(src []byte, offset int, text string) []byte {
	out := make([]byte, 0, len(src)+len(text))
	out = append(out, src[:offset]...)
	out = append(out, text...)
	out = append(out, src[offset:]...)
	return out
}

// rootConfigInfo describes the observable I/O-related fields of the root
// Config struct. Used to adapt the exec stub in generated command files
// and to detect drift between the actual source and generated templates.
type rootConfigInfo struct {
	hasStdin    bool   // Config has a field named Stdin (io.Reader or similar)
	hasStdout   bool   // Config has a field named Stdout (io.Writer or similar)
	hasStderr   bool   // Config has a field named Stderr
	loggerField string // name of a logger field, e.g. "Logger"; "" if none found
}

// probeRootConfig parses cmd/<rootPkg>/<rootPkg>.go and reports which
// output-capable fields the Config struct declares. Returns a zero value
// (all false/"") on any parse failure so callers can fall back gracefully.
func probeRootConfig(dir, rootPkg string) rootConfigInfo {
	p := filepath.Join(dir, "cmd", rootPkg, rootPkg+".go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, nil, 0)
	if err != nil {
		return rootConfigInfo{}
	}

	var info rootConfigInfo
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Config" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					switch name.Name {
					case "Stdin":
						info.hasStdin = true
					case "Stdout":
						info.hasStdout = true
					case "Stderr":
						info.hasStderr = true
					default:
						if strings.Contains(strings.ToLower(name.Name), "log") &&
							info.loggerField == "" {
							info.loggerField = name.Name
						}
					}
				}
			}
		}
	}
	return info
}
