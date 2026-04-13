// Package scaffold – drift.go detects and repairs structural drift between
// climax's own source files and the scaffold template files.
//
// The AST-based approach parses the actual Go source with go/parser to make
// structural inferences (e.g. "does func main call signal.NotifyContext?")
// rather than fragile string matching. Template patching works by reading the
// relevant template file from pkg/scaffold/templates/, applying text
// replacements, and writing it back.
package scaffold

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// DriftItem describes a single structural difference between a climax source
// file and the corresponding scaffold template.
type DriftItem struct {
	Template   string // "main", "cmd", "root", "version", or "man"
	Property   string // human-readable property name
	InSource   string // "present" or "absent"
	InTemplate string // "present" or "absent"
	patch      *templatePatch
}

// templatePatch carries the information needed to repair a single template
// file by applying string replacements to it.
type templatePatch struct {
	templateFile string // filename within pkg/scaffold/templates/, e.g. "main.go.tmpl"
	replacements []replacePair
}

type replacePair struct{ old, new string }

// templateSet holds the template strings used for drift comparisons.
// Separating this from the embedded variables allows tests to inject
// modified templates without touching the filesystem.
type templateSet struct {
	main    string
	cmd     string
	root    string
	version string
	man     string
}

// sourceInfo caches all structural properties extracted from the real source.
type sourceInfo struct {
	// main.go properties
	mainHasSignal   bool
	mainHasRunFunc  bool
	mainPassesStdin bool
	// cmd/cmd.go properties
	cmdHasStdinParam bool
	cmdPassesStdin   bool
	// cmd/root/root.go properties
	rootHasStdinField bool
	rootHasStdinParam bool
	rootAssignsStdin  bool
	// cmd/version/version.go properties
	versionHasJSONFlag                   bool
	versionHasTabwriter                  bool
	versionHasGetVersionInfoFrom         bool
	versionInfoMethodsUsePointerReceiver bool
	versionHasOptionType                 bool
	versionHasOptionConstructors         bool
	// cmd/mango/mango.go → man.go.tmpl properties
	mangoHasSectionField bool
}

// defaultTemplates returns the embedded scaffold templates.
func defaultTemplates() templateSet {
	return templateSet{
		main:    mainTemplate,
		cmd:     cmdTemplate,
		root:    rootTemplate,
		version: versionTemplate,
		man:     manCmdTemplate,
	}
}

// IsFixable reports whether this drift item can be automatically repaired by
// ApplyFixes. It returns true only when the check defined a string-replacement
// patch — not all source-has/template-lacks items are auto-patchable (e.g.
// tabwriter output or GetVersionInfoFrom require a broader template rewrite).
func (d DriftItem) IsFixable() bool {
	return d.patch != nil
}

// ─── Public API ──────────────────────────────────────────────────────────────

// DetectDrift analyses climaxDir (the root of the climax module) and returns
// the list of structural properties that are present in the real source but
// absent from the embedded templates, or vice-versa.
func DetectDrift(climaxDir string) ([]DriftItem, error) {
	main, err := parseFile(filepath.Join(climaxDir, "main.go"))
	if err != nil {
		return nil, fmt.Errorf("parsing main.go: %w", err)
	}
	cmdPath, _, err := findDispatcherFile(climaxDir)
	if err != nil {
		return nil, fmt.Errorf("finding dispatcher: %w", err)
	}
	cmdFile, err := parseFile(cmdPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", cmdPath, err)
	}
	rootFile, err := parseFile(filepath.Join(climaxDir, "cmd", "root", "root.go"))
	if err != nil {
		return nil, fmt.Errorf("parsing cmd/root/root.go: %w", err)
	}

	versionPath := filepath.Join(climaxDir, "cmd", "version", "version.go")
	versionFile, err := parseFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("parsing cmd/version/version.go: %w", err)
	}
	versionSrc, err := os.ReadFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("reading cmd/version/version.go: %w", err)
	}

	mangoFile, err := parseFile(filepath.Join(climaxDir, "cmd", "mango", "mango.go"))
	if err != nil {
		return nil, fmt.Errorf("parsing cmd/mango/mango.go: %w", err)
	}

	src := sourceInfo{
		mainHasSignal:  astHasCall(main, "main", "NotifyContext"),
		mainHasRunFunc: astHasFuncDecl(main, "run"),
		mainPassesStdin: astCallPassesIdent(
			main,
			"run",
			"cmd",
			"Run",
			"Stdin",
		),
		cmdHasStdinParam: astFuncHasIOReaderParam(cmdFile, "Run"),
		cmdPassesStdin: astCallPassesIdent(
			cmdFile,
			"Run",
			"root",
			"New",
			"stdin",
		),
		rootHasStdinField:  astStructHasField(rootFile, "Config", "Stdin"),
		rootHasStdinParam:  astFuncHasIOReaderParam(rootFile, "New"),
		rootAssignsStdin:   astFuncAssignsField(rootFile, "New", "Stdin"),
		versionHasJSONFlag: astStructHasField(versionFile, "Config", "JSON"),
		versionHasTabwriter: strings.Contains(
			string(versionSrc),
			"tabwriter.NewWriter",
		),
		versionHasGetVersionInfoFrom: astHasFuncDecl(versionFile, "GetVersionInfoFrom"),
		versionInfoMethodsUsePointerReceiver: astMethodHasPointerReceiver(
			versionFile,
			"Info",
			"String",
		) &&
			astMethodHasPointerReceiver(versionFile, "Info", "JSONString"),
		versionHasOptionType: astHasTypeDecl(versionFile, "Option"),
		versionHasOptionConstructors: astHasFuncDecl(versionFile, "WithAppDetails") &&
			astHasFuncDecl(versionFile, "WithASCIIName") &&
			astHasFuncDecl(versionFile, "WithBuiltBy"),
		mangoHasSectionField: astStructHasField(mangoFile, "Config", "Section"),
	}

	tmpl := defaultTemplates()
	return runChecks(src, &tmpl), nil
}

// ApplyFixes patches the template files inside climaxDir for every DriftItem
// that has a patch attached. Items without patches (e.g. template-has-extra)
// are skipped. All patches targeting the same file are applied in a single
// read/write cycle. Returns an error if any patch fails.
func ApplyFixes(climaxDir string, items []DriftItem) error {
	templatesDir := filepath.Join(climaxDir, "pkg", "scaffold", "templates")

	// Collect all replacement pairs keyed by template filename, preserving
	// order so that multiple patches to the same file are applied in sequence.
	type fileEdit struct {
		path  string
		pairs []replacePair
	}
	seen := map[string]*fileEdit{}
	var order []string
	for _, item := range items {
		if item.patch == nil {
			continue
		}
		f := item.patch.templateFile
		if _, ok := seen[f]; !ok {
			seen[f] = &fileEdit{path: filepath.Join(templatesDir, f)}
			order = append(order, f)
		}
		seen[f].pairs = append(seen[f].pairs, item.patch.replacements...)
	}

	for _, f := range order {
		edit := seen[f]
		content, err := os.ReadFile(edit.path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}
		body := string(content)
		for _, rp := range edit.pairs {
			body = strings.Replace(body, rp.old, rp.new, 1)
		}
		if err := os.WriteFile(edit.path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", f, err)
		}
	}
	return nil
}

// ─── Checks ──────────────────────────────────────────────────────────────────

func runChecks(src sourceInfo, tmpl *templateSet) []DriftItem {
	type check struct {
		tmpl   string
		prop   string
		inSrc  bool
		inTmpl bool
		patch  *templatePatch
	}

	checks := []check{
		{
			tmpl:   "main",
			prop:   "signal.NotifyContext",
			inSrc:  src.mainHasSignal,
			inTmpl: strings.Contains(tmpl.main, "signal.NotifyContext"),
			patch: &templatePatch{
				templateFile: "main.go.tmpl",
				replacements: []replacePair{
					{
						`"context"
	"errors"
	"fmt"
	"os"`,
						`"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"`,
					},
					{
						`func main() {
	ctx := context.Background()
	err := cmd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		os.Exit(exitSuccess)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(exitFail)
	}
}`,
						`func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	code := run(ctx)
	stop()
	os.Exit(code)
}

// run is intentionally separated from main to improve testability. Please preserve this comment.
func run(ctx context.Context) int {
	err := cmd.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		return exitSuccess
	default:
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		return exitFail
	}
}`,
					},
				},
			},
		},
		{
			tmpl:   "main",
			prop:   "run() separation",
			inSrc:  src.mainHasRunFunc,
			inTmpl: strings.Contains(tmpl.main, "func run("),
		},
		{
			tmpl:   "main",
			prop:   "os.Stdin passed to cmd.Run",
			inSrc:  src.mainPassesStdin,
			inTmpl: strings.Contains(tmpl.main, "os.Stdin"),
		},
		{
			tmpl:   "cmd",
			prop:   "stdin io.Reader parameter in Run",
			inSrc:  src.cmdHasStdinParam,
			inTmpl: strings.Contains(tmpl.cmd, "stdin io.Reader"),
			patch: &templatePatch{
				templateFile: "cmd.go.tmpl",
				replacements: []replacePair{
					{
						"func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {",
						"func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {",
					},
				},
			},
		},
		{
			tmpl:   "cmd",
			prop:   "stdin forwarded to root.New",
			inSrc:  src.cmdPassesStdin,
			inTmpl: strings.Contains(tmpl.cmd, "ROOT_PKG.New(stdin,"),
			patch: &templatePatch{
				templateFile: "cmd.go.tmpl",
				replacements: []replacePair{
					{
						"r := ROOT_PKG.New(stdout, stderr)",
						"r := ROOT_PKG.New(stdin, stdout, stderr)",
					},
				},
			},
		},
		{
			tmpl:   "root",
			prop:   "Stdin io.Reader field in Config",
			inSrc:  src.rootHasStdinField,
			inTmpl: strings.Contains(tmpl.root, "Stdin   io.Reader"),
			patch: &templatePatch{
				templateFile: "root.go.tmpl",
				replacements: []replacePair{
					{
						"type Config struct {\n\tStdout  io.Writer",
						"type Config struct {\n\tStdin   io.Reader\n\tStdout  io.Writer",
					},
				},
			},
		},
		{
			tmpl:   "root",
			prop:   "stdin io.Reader parameter in New",
			inSrc:  src.rootHasStdinParam,
			inTmpl: strings.Contains(tmpl.root, "func New(stdin io.Reader,"),
			patch: &templatePatch{
				templateFile: "root.go.tmpl",
				replacements: []replacePair{
					{
						"func New(stdout, stderr io.Writer) *Config {",
						"func New(stdin io.Reader, stdout, stderr io.Writer) *Config {",
					},
				},
			},
		},
		{
			tmpl:   "root",
			prop:   "cfg.Stdin = stdin assignment",
			inSrc:  src.rootAssignsStdin,
			inTmpl: strings.Contains(tmpl.root, "cfg.Stdin = stdin"),
			patch: &templatePatch{
				templateFile: "root.go.tmpl",
				replacements: []replacePair{
					{
						"cfg.Stdout = stdout",
						"cfg.Stdin = stdin\n\tcfg.Stdout = stdout",
					},
				},
			},
		},
		{
			tmpl:   "version",
			prop:   "JSON flag in Config",
			inSrc:  src.versionHasJSONFlag,
			inTmpl: strings.Contains(tmpl.version, "\tJSON    bool\n"),
			patch: &templatePatch{
				templateFile: "version.go.tmpl",
				replacements: []replacePair{
					{
						"\t*ROOT_PKG.Config\n\tFlags   *ff.FlagSet",
						"\t*ROOT_PKG.Config\n\tJSON    bool\n\tFlags   *ff.FlagSet",
					},
				},
			},
		},
		{
			tmpl:   "version",
			prop:   "tabwriter output",
			inSrc:  src.versionHasTabwriter,
			inTmpl: strings.Contains(tmpl.version, "tabwriter.NewWriter"),
		},
		{
			tmpl:   "version",
			prop:   "GetVersionInfoFrom function",
			inSrc:  src.versionHasGetVersionInfoFrom,
			inTmpl: strings.Contains(tmpl.version, "func GetVersionInfoFrom("),
		},
		{
			tmpl:  "version",
			prop:  "Info methods use pointer receivers",
			inSrc: src.versionInfoMethodsUsePointerReceiver,
			inTmpl: strings.Contains(tmpl.version, "func (i *Info) String()") &&
				strings.Contains(tmpl.version, "func (i *Info) JSONString()"),
			patch: &templatePatch{
				templateFile: "version.go.tmpl",
				replacements: []replacePair{
					{"func (i Info) String()", "func (i *Info) String()"},
					{"func (i Info) JSONString()", "func (i *Info) JSONString()"},
				},
			},
		},
		{
			tmpl:   "version",
			prop:   "Option type",
			inSrc:  src.versionHasOptionType,
			inTmpl: strings.Contains(tmpl.version, "type Option func("),
		},
		{
			tmpl:  "version",
			prop:  "WithAppDetails, WithASCIIName, WithBuiltBy constructors",
			inSrc: src.versionHasOptionConstructors,
			inTmpl: strings.Contains(tmpl.version, "func WithAppDetails(") &&
				strings.Contains(tmpl.version, "func WithASCIIName(") &&
				strings.Contains(tmpl.version, "func WithBuiltBy("),
		},
		{
			tmpl:   "man",
			prop:   "Section int field in Config",
			inSrc:  src.mangoHasSectionField,
			inTmpl: strings.Contains(tmpl.man, "\tSection int\n"),
			patch: &templatePatch{
				templateFile: "man.go.tmpl",
				replacements: []replacePair{
					{
						"\t*ROOT_PKG.Config\n\tFlags   *ff.FlagSet",
						"\t*ROOT_PKG.Config\n\tSection int\n\tFlags   *ff.FlagSet",
					},
				},
			},
		},
	}

	var items []DriftItem
	for _, c := range checks {
		if c.inSrc == c.inTmpl {
			continue // in sync
		}
		item := DriftItem{
			Template:   c.tmpl,
			Property:   c.prop,
			InSource:   presence(c.inSrc),
			InTemplate: presence(c.inTmpl),
		}
		// Only attach patch when source has the property but template doesn't;
		// removing things from templates is a manual decision.
		if c.inSrc && !c.inTmpl {
			item.patch = c.patch
		}
		items = append(items, item)
	}
	return items
}

func presence(b bool) string {
	if b {
		return "present"
	}
	return "absent"
}

// ─── AST helpers ─────────────────────────────────────────────────────────────

func parseFile(path string) (*ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return f, nil
}

// astHasCall returns true if the body of function named funcName in file
// contains any call expression whose selector matches callName
// (e.g. callName="NotifyContext" matches signal.NotifyContext(...)).
func astHasCall(file *ast.File, funcName, callName string) bool {
	fn := findFuncDecl(file, funcName)
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == callName {
			found = true
			return false
		}
		return true
	})
	return found
}

// astHasFuncDecl returns true if file declares a top-level function named name.
func astHasFuncDecl(file *ast.File, name string) bool {
	return findFuncDecl(file, name) != nil
}

// astHasTypeDecl returns true if file declares a top-level type named name.
func astHasTypeDecl(file *ast.File, name string) bool {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// astCallPassesIdent returns true if, inside funcName's body, there is a call
// to pkg.method(...) that includes an argument whose name or selector ends with
// identName (e.g. "Stdin" matches os.Stdin).
func astCallPassesIdent(file *ast.File, funcName, pkg, method, identName string) bool {
	fn := findFuncDecl(file, funcName)
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != pkg {
			return true
		}
		// Check whether any argument references identName.
		for _, arg := range call.Args {
			if argMatchesIdent(arg, identName) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// argMatchesIdent returns true if expr is an identifier or selector whose last
// component matches name (e.g. os.Stdin matches "Stdin", stdin matches "stdin").
func argMatchesIdent(expr ast.Expr, name string) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == name
	case *ast.SelectorExpr:
		return e.Sel.Name == name
	}
	return false
}

// astFuncHasIOReaderParam returns true if function funcName in file has any
// parameter whose type is io.Reader.
func astFuncHasIOReaderParam(file *ast.File, funcName string) bool {
	fn := findFuncDecl(file, funcName)
	if fn == nil || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		if typeString(field.Type) == "io.Reader" {
			return true
		}
	}
	return false
}

// astStructHasField returns true if the struct type named structName in file
// declares a field named fieldName.
//
//nolint:unparam // structName is always "Config" today but the function is kept general
func astStructHasField(file *ast.File, structName, fieldName string) bool {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != structName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if name.Name == fieldName {
						return true
					}
				}
			}
		}
	}
	return false
}

// astFuncAssignsField returns true if function funcName in file has an
// assignment of the form cfg.fieldName = ... anywhere in its body.
func astFuncAssignsField(file *ast.File, funcName, fieldName string) bool {
	fn := findFuncDecl(file, funcName)
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == fieldName {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// astMethodHasPointerReceiver returns true if the named method on receiverType
// uses a pointer receiver (e.g. func (i *Info) String() satisfies
// astMethodHasPointerReceiver(f, "Info", "String")).
//
// Methods on other types that share the same name are skipped; the function
// only returns true when both the receiver type and the star match.
func astMethodHasPointerReceiver(file *ast.File, receiverType, methodName string) bool {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		if fd.Name.Name != methodName {
			continue
		}
		star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		ident, ok := star.X.(*ast.Ident)
		if ok && ident.Name == receiverType {
			return true
		}
	}
	return false
}

// findFuncDecl returns the first top-level function declaration named name, or nil.
func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Recv == nil && fd.Name.Name == name && fd.Body != nil {
			return fd
		}
	}
	return nil
}

// typeString returns a simplified string representation of a type expression
// sufficient for matching "io.Reader", "io.Writer", "*pkg.Type", etc.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if ok {
			return pkg.Name + "." + t.Sel.Name
		}
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	}
	return ""
}
