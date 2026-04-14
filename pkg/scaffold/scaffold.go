// Package scaffold generates Climax application and command files.
package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Markers embedded in generated files so "climax add" can locate insertion points.
const (
	ImportsMarker  = "// climax:imports"
	CommandsMarker = "// register new commands here"
)

// AddOptions controls what AddCommand generates for the new command file.
type AddOptions struct {
	Name   string // ff.Command.Name; defaults to the Go package name (positional arg)
	Short  string // ff.Command.ShortHelp; defaults to "<name> command"
	Long   string // ff.Command.LongHelp; defaults to "<Name> is a new command."
	Parent string // Go package name of the parent command; defaults to the root package
}

// InitOptions controls what InitApp generates.
type InitOptions struct {
	ImportPrefix string // Go import path for the application root (required)
	Name         string // ff.Command.Name for the root; defaults to last segment of ImportPrefix
	Short        string // ff.Command.ShortHelp for the root; defaults to "TODO: describe <name> here"
	Long         string // ff.Command.LongHelp for the root; omitted if empty
	RootPkg      string // Go package name (and file basename) for the root config; defaults to "root"
	EnvPrefix    string // env var prefix for ff.WithEnvVarPrefix; defaults to NormalizeEnvPrefix(Name)
	// NoEnvPrefix selects ff.WithEnvVars() (no prefix) over ff.WithEnvVarPrefix.
	// When true, EnvPrefix is still normalized and used to rewrite the generated
	// content, but the resulting file omits the prefix from both the Parse call
	// and the climax:env-prefix marker. Mutually exclusive with a non-empty EnvPrefix
	// at the CLI layer; both may be set here (EnvPrefix is ignored in the output).
	NoEnvPrefix bool
	NoVersion   bool // when true, skip generating cmd/version/version.go
}

// InitApp writes a complete Climax application scaffold to dir and returns
// the relative paths of every file it creates, in the order they were written.
//
//nolint:gocritic // hugeParam: InitOptions is an options struct; passing by value is idiomatic
func InitApp(dir string, opts InitOptions) ([]string, error) {
	// Fill in defaults.
	parts := strings.Split(opts.ImportPrefix, "/")
	appName := parts[len(parts)-1]
	if opts.Name == "" {
		opts.Name = appName
	}
	if opts.RootPkg == "" {
		opts.RootPkg = "root"
	}
	if opts.Short == "" {
		opts.Short = "TODO: describe " + opts.Name + " here"
	}
	if opts.EnvPrefix == "" {
		opts.EnvPrefix = opts.Name
	}
	opts.EnvPrefix = NormalizeEnvPrefix(opts.EnvPrefix)
	if opts.EnvPrefix == "" {
		opts.EnvPrefix = "APP"
	}

	// Build conditional template fragments.
	longHelpLine := ""
	if opts.Long != "" {
		longHelpLine = "\t\tLongHelp:  " + strconv.Quote(opts.Long) + ",\n"
	}
	versionImport := ""
	versionCall := ""
	if !opts.NoVersion {
		versionImport = "\t\"" + opts.ImportPrefix + "/cmd/version\"\n"
		versionCall = "\tversion.New(r)\n"
	}

	vars := map[string]string{
		"APP_IMPORT":     opts.ImportPrefix,
		"APP_NAME":       opts.Name,
		"APP_ENV_PREFIX": opts.EnvPrefix,
		"ROOT_PKG":       opts.RootPkg,
		"APP_SHORT":      goStringContent(opts.Short),
		"LONG_HELP_LINE": longHelpLine,
		"VERSION_IMPORT": versionImport,
		"VERSION_CALL":   versionCall,
	}

	type fileEntry struct {
		rel  string
		tmpl string
	}
	files := []fileEntry{
		{"main.go", mainTemplate},
		{filepath.Join("cmd", "cmd.go"), cmdTemplate},
		{filepath.Join("cmd", opts.RootPkg, opts.RootPkg+".go"), rootTemplate},
	}
	if !opts.NoVersion {
		files = append(files, fileEntry{
			filepath.Join("cmd", "version", "version.go"),
			versionTemplate,
		})
	}

	cmdRel := filepath.Join("cmd", "cmd.go")
	var written []string
	for _, f := range files {
		content := applyVars(f.tmpl, vars)
		if opts.NoEnvPrefix && f.rel == cmdRel {
			content = applyNoEnvPrefix(content, opts.EnvPrefix)
		}
		if err := writeFile(filepath.Join(dir, f.rel), content); err != nil {
			return nil, err
		}
		written = append(written, f.rel)
	}
	return written, nil
}

// IsClimaxApp reports whether dir is the root of a Climax application.
// It accepts applications whether or not the text markers are present,
// using AST analysis as a fallback when markers have been removed.
func IsClimaxApp(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		return errors.New("not a climax app root: missing main.go")
	}
	info, _, err := analyzeDispatcher(dir)
	if err != nil {
		return fmt.Errorf("not a climax app root: %w", err)
	}
	rootFile := filepath.Join("cmd", info.rootPkg, info.rootPkg+".go")
	if _, err := os.Stat(filepath.Join(dir, rootFile)); err != nil {
		return fmt.Errorf("not a climax app root: missing %s", rootFile)
	}
	return nil
}

// AddCommand creates cmd/<name>/<name>.go and registers it in the dispatcher.
// It works whether or not the dispatcher file retains its text markers.
// It returns the relative paths of the created command file and the modified
// dispatcher file, both relative to dir.
func AddCommand(
	dir, name, importPrefix string,
	opts AddOptions,
) (created, modified string, err error) {
	if err := ValidateIdent(name); err != nil {
		return "", "", err
	}

	info, src, err := analyzeDispatcher(dir)
	if err != nil {
		return "", "", fmt.Errorf("reading dispatcher: %w", err)
	}

	// Resolve the CLI name used in generated Usage strings.
	// Priority: climax:name marker → ff.Command.Name in root pkg file → import path.
	cliName := info.cliName
	if cliName == "" {
		cliName = extractCLIName(dir, info.rootPkg)
	}
	if cliName == "" {
		parts := strings.Split(importPrefix, "/")
		cliName = parts[len(parts)-1]
	}

	// Apply AddOptions defaults.
	ffName := name
	if opts.Name != "" {
		ffName = opts.Name
	}
	short := name + " command"
	if opts.Short != "" {
		short = opts.Short
	}
	long := titleCase(name) + " is a new command."
	if opts.Long != "" {
		long = opts.Long
	}

	parentPkg := opts.Parent
	if parentPkg == "" || parentPkg == info.rootPkg {
		parentPkg = info.rootPkg
	}

	rc := probeRootConfig(dir, info.rootPkg)
	execImports, execBody := execStubVars(rc, ffName)

	vars := map[string]string{
		"APP_IMPORT":   importPrefix,
		"APP_NAME":     cliName,
		"ROOT_PKG":     info.rootPkg,
		"PARENT_PKG":   parentPkg,
		"CMD_NAME":     name,
		"CMD_FF_NAME":  ffName,
		"CMD_SHORT":    goStringContent(short),
		"CMD_LONG":     goStringContent(long),
		"EXEC_IMPORTS": execImports,
		"EXEC_BODY":    execBody,
	}

	// Write cmd/<name>/<name>.go.
	createdRel := filepath.Join("cmd", name, name+".go")
	if err := writeFile(
		filepath.Join(dir, createdRel),
		applyVars(newCmdTemplate, vars),
	); err != nil {
		return "", "", err
	}

	// Register in the dispatcher file.
	if err := registerInCmdGo(info, src, name, importPrefix, parentPkg); err != nil {
		return "", "", err
	}

	modifiedRel, relErr := filepath.Rel(dir, info.path)
	if relErr != nil {
		modifiedRel = info.path // fallback to absolute path
	}
	return createdRel, modifiedRel, nil
}

// ManOptions controls what AddManCommand generates for the man subcommand.
type ManOptions struct {
	// Section is the man page section number (1–8). Defaults to 1.
	Section int
	// Authors is baked into a WithSection("Authors", ...) call when non-empty.
	Authors string
	// Copyright is baked into a WithSection("Copyright", ...) call when non-empty.
	Copyright string
}

// AddManCommand creates cmd/man/man.go in the climax application rooted at dir
// and registers it in the dispatcher. importPrefix is the Go module import path
// for the application root.
//
// It returns the relative paths of the created file and the modified dispatcher.
// After running AddManCommand, the caller must ensure the target application
// adds the required dependencies:
//
//	go get github.com/StevenACoffman/mango-ff github.com/muesli/roff
func AddManCommand(
	dir, importPrefix string,
	opts ManOptions,
) (created, modified string, err error) {
	if opts.Section < 1 || opts.Section > 8 {
		opts.Section = 1
	}

	info, src, err := analyzeDispatcher(dir)
	if err != nil {
		return "", "", fmt.Errorf("reading dispatcher: %w", err)
	}

	cliName := info.cliName
	if cliName == "" {
		cliName = extractCLIName(dir, info.rootPkg)
	}
	if cliName == "" {
		parts := strings.Split(importPrefix, "/")
		cliName = parts[len(parts)-1]
	}

	vars := map[string]string{
		"APP_IMPORT":        importPrefix,
		"APP_NAME":          cliName,
		"ROOT_PKG":          info.rootPkg,
		"MAN_SECTION":       strconv.Itoa(opts.Section),
		"MAN_WITH_SECTIONS": manWithSectionsBlock(opts.Authors, opts.Copyright),
	}

	createdRel := filepath.Join("cmd", "man", "man.go")
	if err := writeFile(
		filepath.Join(dir, createdRel),
		applyVars(manCmdTemplate, vars),
	); err != nil {
		return "", "", err
	}

	if err := registerInCmdGo(info, src, "man", importPrefix, info.rootPkg); err != nil {
		return "", "", err
	}

	modifiedRel, relErr := filepath.Rel(dir, info.path)
	if relErr != nil {
		modifiedRel = info.path
	}
	return createdRel, modifiedRel, nil
}

// manWithSectionsBlock returns the source lines that chain WithSection calls
// onto the manPage variable, or an empty string when neither authors nor
// copyright is set. The returned string is ready to be spliced directly into
// the template — it ends with a newline when non-empty.
func manWithSectionsBlock(authors, copyright string) string {
	if authors == "" && copyright == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\tmanPage = manPage")
	if authors != "" {
		b.WriteString(".\n\t\tWithSection(\"Authors\", " + strconv.Quote(authors) + ")")
	}
	if copyright != "" {
		b.WriteString(".\n\t\tWithSection(\"Copyright\", " + strconv.Quote(copyright) + ")")
	}
	b.WriteString("\n")
	return b.String()
}

// registerInCmdGo injects the new command's import and New() call into the
// dispatcher file. It dispatches to the marker path when both text markers are
// present, and to the AST path otherwise.
func registerInCmdGo(info *dispatcherInfo, src []byte, name, importPrefix, parentPkg string) error {
	if info.markerBased {
		return registerViaMarkers(info, string(src), name, importPrefix, parentPkg)
	}
	return registerViaAST(info, src, name, importPrefix, parentPkg)
}

// registerViaMarkers is the original text-replacement path, used when both
// climax marker comments are present in the dispatcher file.
func registerViaMarkers(info *dispatcherInfo, content, name, importPrefix, parentPkg string) error {
	// Insert import before the climax:imports marker.
	// The marker appears in the file as "\t// climax:imports"; we replace only
	// the bare marker text so the existing leading tab stays in place.
	importLine := fmt.Sprintf("\"%s/cmd/%s\"\n\t", importPrefix, name)
	content = strings.Replace(content, ImportsMarker, importLine+ImportsMarker, 1)

	if parentPkg == info.rootPkg {
		// Direct child of root: insert before the commands marker.
		callLine := fmt.Sprintf("%s.New(%s)\n\t", name, info.rootVar)
		content = strings.Replace(content, CommandsMarker, callLine+CommandsMarker, 1)
	} else {
		// Non-root parent: locate the parent's New( call, promote it from a bare
		// expression statement to a captured assignment if needed, then insert
		// the child call on the line immediately following.
		parentCallPattern := "\t" + parentPkg + ".New("
		if !strings.Contains(content, parentCallPattern) {
			return fmt.Errorf(
				"parent command %q not found in dispatcher; add it with 'climax add %s' first",
				parentPkg, parentPkg)
		}

		parentCfgVar := parentPkg + "Cfg"
		capturedPattern := "\t" + parentCfgVar + " := " + parentPkg + ".New("
		if !strings.Contains(content, capturedPattern) {
			content = strings.Replace(content, parentCallPattern,
				"\t"+parentCfgVar+" := "+parentPkg+".New(", 1)
		}

		searchStr := "\t" + parentCfgVar + " := " + parentPkg + ".New("
		idx := strings.Index(content, searchStr)
		if idx == -1 {
			return fmt.Errorf(
				"unexpected: %q not found in %s after promotion",
				searchStr,
				info.path,
			)
		}
		lineEnd := strings.Index(content[idx:], "\n")
		if lineEnd == -1 {
			return fmt.Errorf("unexpected: no newline after parent registration in %s", info.path)
		}
		insertPos := idx + lineEnd + 1
		callLine := fmt.Sprintf("\t%s.New(%s)\n", name, parentCfgVar)
		content = content[:insertPos] + callLine + content[insertPos:]
	}

	if err := os.WriteFile(info.path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", info.path, err)
	}
	return nil
}

// registerViaAST is the marker-free path. It uses the byte offsets derived
// from the AST probe to splice the import and command registration directly
// into the source without needing any embedded marker comments.
func registerViaAST(info *dispatcherInfo, src []byte, name, importPrefix, parentPkg string) error {
	// Step 1: insert import before the closing ')' of the import group.
	// %q produces a properly quoted Go string literal including the double quotes.
	importLine := fmt.Sprintf("\t%q\n", importPrefix+"/cmd/"+name)
	src = insertAt(src, info.importEndOffset, importLine)

	if parentPkg == info.rootPkg {
		// Step 2a: insert <name>.New(<rootVar>) before r.Command.Parse.
		// Shift the call offset to account for the bytes inserted in step 1.
		callOffset := info.callInsertOffset + len(importLine)
		callLine := fmt.Sprintf("\t%s.New(%s)\n", name, info.rootVar)
		src = insertAt(src, callOffset, callLine)
	} else {
		// Step 2b: find the parent's New() call in the updated source (re-parse
		// because step 1 shifted all subsequent byte offsets) and insert the
		// child call immediately after it.
		pcr, err := findParentCallInAST(info.path, src, parentPkg)
		if err != nil {
			return fmt.Errorf(
				"parent command %q not found; add it with 'climax add %s' first",
				parentPkg, parentPkg)
		}

		// Promote a bare ExprStmt to a captured AssignStmt when the parent call's
		// return value is not yet being saved. Insert "<pkg>Cfg := " before the
		// call expression; adjust lineEnd for the inserted bytes.
		if !pcr.isCaptured {
			promotion := pcr.cfgVar + " := "
			src = insertAt(src, pcr.callStart, promotion)
			pcr.lineEnd += len(promotion)
		}

		// Insert the child command call on the line immediately after the parent.
		callLine := fmt.Sprintf("\t%s.New(%s)\n", name, pcr.cfgVar)
		src = insertAt(src, pcr.lineEnd, callLine)
	}

	if err := os.WriteFile(info.path, src, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", info.path, err)
	}
	return nil
}

// readMarker scans content line by line for "// <prefix> <value>" and returns
// the trimmed value after the prefix. Returns "" if not found.
// The prefix must be followed by whitespace or end-of-line to avoid false
// matches against longer markers (e.g. "// climax:name" must not match
// "// climax:name-extra").
func readMarker(content, prefix string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := trimmed[len(prefix):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// writeFile creates path (and any needed parent directories), failing if the
// file already exists.
func writeFile(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

// applyVars replaces placeholder keys in tmpl with their values simultaneously.
func applyVars(tmpl string, vars map[string]string) string {
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, k, v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// ValidateCliName reports whether name is valid as a CLI command name.
// It allows letters, digits, hyphens, and underscores, starting with a letter
// or underscore.
func ValidateCliName(name string) error {
	if name == "" {
		return errors.New("name cannot be empty")
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return fmt.Errorf("name %q must start with a letter or underscore", name)
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
				return fmt.Errorf("name %q contains invalid character %q", name, r)
			}
		}
	}
	return nil
}

// ValidateIdent reports whether name is a valid Go identifier.
func ValidateIdent(name string) error {
	if name == "" {
		return errors.New("command name cannot be empty")
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return fmt.Errorf("command name %q must start with a letter or underscore", name)
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return fmt.Errorf("command name %q contains invalid character %q", name, r)
			}
		}
	}
	return nil
}

// NormalizeEnvPrefix derives a valid environment-variable prefix from name.
// It uppercases all ASCII letters, replaces hyphens and periods with
// underscores, and drops all other non-ASCII and non-alphanumeric characters.
//
// Examples:
//
//	"myapp"   → "MYAPP"
//	"my-app"  → "MY_APP"
//	"my.app"  → "MY_APP"
//	"café"    → "CAF"   (non-ASCII é dropped)
func NormalizeEnvPrefix(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '-' || r == '.':
			b.WriteRune('_')
		case r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// applyNoEnvPrefix rewrites generated cmd.go content to use ff.WithEnvVars()
// (no prefix) instead of ff.WithEnvVarPrefix("PREFIX"). It:
//   - removes the climax:env-prefix marker line
//   - replaces the three-line prefix-specific doc comment with a two-line
//     no-prefix equivalent
//   - replaces ff.WithEnvVarPrefix("PREFIX") with ff.WithEnvVars()
//
// normalizedPrefix must be the already-normalized value used during template
// expansion (opts.EnvPrefix after InitApp fills defaults and calls NormalizeEnvPrefix).
func applyNoEnvPrefix(content, normalizedPrefix string) string {
	// Remove the climax:env-prefix marker line.
	content = strings.Replace(content,
		"\n// climax:env-prefix "+normalizedPrefix, "", 1)
	// Replace the three-line prefix-specific doc comment.
	oldComment := "// Every flag can be set via a " + normalizedPrefix + "_-prefixed environment variable.\n" +
		"// The mapping rule is: prepend " + normalizedPrefix + "_, uppercase, replace dashes with\n" +
		"// underscores."
	newComment := "// Every flag can be set via an environment variable with the same name,\n" +
		"// uppercased with dashes and dots replaced by underscores (--log-level → LOG_LEVEL)."
	content = strings.Replace(content, oldComment, newComment, 1)
	// Replace the WithEnvVarPrefix Parse option with WithEnvVars.
	content = strings.Replace(content,
		`ff.WithEnvVarPrefix("`+normalizedPrefix+`")`,
		"ff.WithEnvVars()",
		1)
	return content
}

// goStringContent returns s escaped for use as the content of a Go interpreted
// string literal — i.e. what goes between the double quotes. It handles all
// characters that require escaping: backslashes, double quotes, control characters, etc.
func goStringContent(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}

// execStubVars returns the EXEC_IMPORTS and EXEC_BODY template variable values
// for the exec stub, adapted to the output fields available on the root Config.
func execStubVars(rc rootConfigInfo, cmdName string) (imports, body string) {
	switch {
	case rc.hasStdout:
		// Climax-style root: io.Writer fields present.
		return "\t\"fmt\"\n",
			fmt.Sprintf("\t_, _ = fmt.Fprintln(cfg.Stdout, %q)\n\treturn nil\n",
				cmdName+": not yet implemented")
	case rc.loggerField != "":
		// Logger-style root: structured logger field present.
		return "",
			fmt.Sprintf("\tcfg.%s.Info(%q)\n\treturn nil\n",
				rc.loggerField, cmdName+": not yet implemented")
	default:
		// Unknown root shape: safest stub that always compiles.
		return "", "\treturn nil\n"
	}
}
