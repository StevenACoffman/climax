// Package scaffold – features.go implements the opt-in template features
// selected by `climax init` flags (--jsonl, --global-flags, --logger,
// --getenv). Each feature is applied as a pure string transform over the
// already-expanded base template output, so the default (no-feature) scaffold
// stays byte-identical and the base templates remain the single source of
// truth for `climax lint` and `climax update`.
package scaffold

import (
	"fmt"
	"go/format"
	"path/filepath"
	"strings"
)

// Features selects the opt-in template additions InitApp applies on top of the
// base scaffold. The zero value adds nothing (the default lean scaffold).
type Features struct {
	// JSONL generates a generic Outcome envelope with Emit* helpers.
	JSONL bool
	// GlobalFlags binds --verbose/-v, --quiet/-q, --no-color, --jsonl on the
	// root Config and routes --quiet through the shared Stdout.
	GlobalFlags bool
	// Logger adds a slog diagnostic logger (writing to stderr) plus NewLogger
	// and LogLevel helpers.
	Logger bool
	// Getenv threads an injected getenv func through New/Run so configuration
	// precedence is testable without touching os.Getenv.
	Getenv bool
	// PosixGuard generates the MisplacedFlag helper, which detects a flag
	// written after a positional argument (ff/v4 would silently swallow it).
	PosixGuard bool
}

// fileEntry pairs a scaffold file's destination path (relative to the app root)
// with the template it is generated from.
type fileEntry struct {
	rel  string
	tmpl string
}

// featureCtx carries the already-substituted names that feature transforms
// need to build anchors and generated code.
type featureCtx struct {
	appName string // ff.Command.Name / flag-set name
	rootPkg string // root config package name (and file basename)
}

// Any reports whether at least one feature is enabled.
func (f Features) Any() bool {
	return f.JSONL || f.GlobalFlags || f.Logger || f.Getenv || f.PosixGuard
}

// featureFiles returns the additional standalone files generated for the
// enabled features (each a whole new file in the root package rootPkg), or nil
// when no such feature is enabled.
func featureFiles(f Features, rootPkg string) []fileEntry {
	var files []fileEntry
	if f.JSONL {
		files = append(files, fileEntry{
			filepath.Join("cmd", rootPkg, "outcome.go"), outcomeTemplate,
		})
	}
	if f.Logger {
		files = append(files, fileEntry{
			filepath.Join("cmd", rootPkg, "logger.go"), loggerTemplate,
		})
	}
	if f.PosixGuard {
		files = append(files, fileEntry{
			filepath.Join("cmd", rootPkg, "misplaced.go"), misplacedTemplate,
		})
	}
	return files
}

// applyFeatures applies the enabled opt-in feature transforms to the expanded
// content of a scaffold file (identified by its app-relative path) and returns
// the possibly-rewritten, gofmt-formatted source. When no enabled feature
// touches the file, content is returned unchanged and unformatted, preserving
// the byte-for-byte default scaffold.
func applyFeatures(content, rel string, f Features, ctx featureCtx) (string, error) {
	if !f.Any() {
		return content, nil
	}
	orig := content
	switch rel {
	case "main.go":
		content = applyMainFeatures(content, f)
	case filepath.Join("cmd", "cmd.go"):
		content = applyCmdFeatures(content, f, ctx)
	case rootFileRel(ctx.rootPkg):
		content = applyRootFeatures(content, f, ctx)
	}
	if content == orig {
		return content, nil
	}
	return gofmtSource(content, rel)
}

// applyMainFeatures rewrites main.go for the enabled features.
func applyMainFeatures(content string, f Features) string {
	if f.Getenv {
		// Thread os.Getenv into the dispatcher call so New/Run can inject it.
		content = strings.Replace(content,
			"cmd.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)",
			"cmd.Run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)", 1)
	}
	return content
}

// applyCmdFeatures rewrites the dispatcher (cmd/cmd.go) for the enabled features.
func applyCmdFeatures(content string, f Features, ctx featureCtx) string {
	if f.Getenv {
		content = strings.Replace(
			content,
			"func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {",
			"func Run(ctx context.Context, args []string, getenv func(string) string, "+
				"stdin io.Reader, stdout, stderr io.Writer) error {",
			1,
		)
		content = strings.Replace(content,
			"r := "+ctx.rootPkg+".New(stdin, stdout, stderr)",
			"r := "+ctx.rootPkg+".New(getenv, stdin, stdout, stderr)", 1)
	}
	// Post-parse setup is inserted before the unmatched-subcommand guard, which
	// the base template always emits.
	if block := cmdPostParseBlock(f, ctx.rootPkg); block != "" {
		content = strings.Replace(content,
			"\t// An unmatched token leaves",
			block+"\t// An unmatched token leaves", 1)
	}
	// Record the opted-in feature set so climax lint can drift-check the surface.
	if list := featureMarkerList(f); list != "" {
		rootPkgMarker := "// climax:root-pkg " + ctx.rootPkg + "\n"
		content = strings.Replace(content,
			rootPkgMarker,
			rootPkgMarker+"// climax:features "+list+"\n", 1)
	}
	return content
}

// featureMarkerList returns the comma-separated list of enabled feature names
// (in a fixed order) recorded in the // climax:features marker, or "" when no
// feature is enabled.
func featureMarkerList(f Features) string {
	var names []string
	if f.JSONL {
		names = append(names, "jsonl")
	}
	if f.GlobalFlags {
		names = append(names, "global-flags")
	}
	if f.Logger {
		names = append(names, "logger")
	}
	if f.Getenv {
		names = append(names, "getenv")
	}
	if f.PosixGuard {
		names = append(names, "posix-guard")
	}
	return strings.Join(names, ",")
}

// parseFeatureMarker parses the value of a // climax:features marker into a set
// of feature names. An empty or blank list yields an empty (non-nil) set.
func parseFeatureMarker(list string) map[string]bool {
	set := make(map[string]bool)
	for name := range strings.SplitSeq(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = true
		}
	}
	return set
}

// cmdPostParseBlock builds the statements inserted into Run after Parse: the
// --quiet stdout redirect and the logger rebuild. Returns "" when no enabled
// feature contributes.
func cmdPostParseBlock(f Features, rootPkg string) string {
	var b strings.Builder
	if f.GlobalFlags {
		b.WriteString("\t// --quiet routes every command's non-error output to io.Discard.\n" +
			"\tif r.Quiet {\n" +
			"\t\tr.Stdout = io.Discard\n" +
			"\t}\n\n")
	}
	if f.Logger && f.GlobalFlags {
		// Rebuild now that --verbose/--quiet/--jsonl are parsed.
		b.WriteString("\tr.Log = " + rootPkg + ".NewLogger(stderr, r.JSONL, " +
			rootPkg + ".LogLevel(r.Verbose, r.Quiet))\n\n")
	}
	return b.String()
}

// applyRootFeatures rewrites the root config file for the enabled features.
func applyRootFeatures(content string, f Features, ctx featureCtx) string {
	content = spliceRootConfigFields(content, f)
	content = spliceRootNewBody(content, f)
	if f.Logger {
		// gofmt sorts the stdlib import group, so insertion position is nominal.
		content = strings.Replace(content,
			"\t\"io\"\n",
			"\t\"io\"\n\t\"log/slog\"\n", 1)
	}
	if f.Getenv {
		content = strings.Replace(
			content,
			"func New(stdin io.Reader, stdout, stderr io.Writer) *Config {",
			"func New(getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) *Config {",
			1,
		)
	}
	if f.GlobalFlags {
		content = spliceGlobalFlagBinding(content, ctx.appName)
	}
	return content
}

// spliceRootConfigFields inserts feature-specific fields into the Config struct
// after Stderr. gofmt realigns the struct, so the field spacing here is nominal.
func spliceRootConfigFields(content string, f Features) string {
	var b strings.Builder
	if f.GlobalFlags {
		b.WriteString("\tVerbose bool\n\tQuiet bool\n\tNoColor bool\n\tJSONL bool\n")
	}
	if f.Logger {
		b.WriteString("\tLog *slog.Logger\n")
	}
	if f.Getenv {
		b.WriteString("\tGetenv func(string) string\n")
	}
	if b.Len() == 0 {
		return content
	}
	return strings.Replace(content,
		"\tStderr  io.Writer\n",
		"\tStderr  io.Writer\n"+b.String(), 1)
}

// spliceRootNewBody inserts feature-specific assignments into New after the
// cfg.Stderr assignment.
func spliceRootNewBody(content string, f Features) string {
	var b strings.Builder
	if f.Getenv {
		b.WriteString("\tcfg.Getenv = getenv\n")
	}
	if f.Logger {
		// Seed a default logger; --global-flags apps rebuild it after Parse
		// with the actual --verbose/--quiet/--jsonl values.
		b.WriteString("\tcfg.Log = NewLogger(stderr, false, slog.LevelWarn)\n")
	}
	if b.Len() == 0 {
		return content
	}
	return strings.Replace(content,
		"\tcfg.Stderr = stderr\n",
		"\tcfg.Stderr = stderr\n"+b.String(), 1)
}

// spliceGlobalFlagBinding replaces the base "no shared flags" comment block in
// New with a bound flag set (--verbose/-v, --quiet/-q, --no-color, --jsonl).
func spliceGlobalFlagBinding(content, appName string) string {
	oldBlock := "\t// No shared flags — cfg.Flags is nil; ff provides --help automatically.\n" +
		"\t// Subcommands call SetParent(parent.Flags)\n" +
		"\t// which is a no-op here; add shared flags (e.g. BoolVar) to activate.\n" +
		"\t// To add shared flags, uncomment and bind before constructing the command:\n" +
		"\t// cfg.Flags = ff.NewFlagSet(\"" + appName + "\")\n" +
		"\t// cfg.Flags.BoolVar(&cfg.MyFlag, 0, \"my-flag\", \"\", \"description\")\n"
	newBlock := "\t// Shared flags, inherited by every subcommand via SetParent(parent.Flags).\n" +
		"\tcfg.Flags = ff.NewFlagSet(\"" + appName + "\")\n" +
		"\tcfg.Flags.BoolVar(&cfg.Verbose, 'v', \"verbose\", \"enable verbose (debug-level) output\")\n" +
		"\tcfg.Flags.BoolVar(&cfg.Quiet, 'q', \"quiet\", \"suppress non-error output\")\n" +
		"\tcfg.Flags.BoolVar(&cfg.NoColor, 0, \"no-color\", \"disable colored output\")\n" +
		"\tcfg.Flags.BoolVar(&cfg.JSONL, 0, \"jsonl\", \"emit machine-readable JSONL on stdout\")\n"
	content = strings.Replace(content, oldBlock, newBlock, 1)
	// Attach the flag set to the root command so the flags parse and appear in
	// --help; the base template leaves ff.Command.Flags unset.
	return strings.Replace(content,
		"\tcfg.Command = &ff.Command{\n",
		"\tcfg.Command = &ff.Command{\n\t\tFlags: cfg.Flags,\n", 1)
}

// rootFileRel returns the app-relative path of the root config file whose
// package (and file basename) is rootPkg.
func rootFileRel(rootPkg string) string {
	return filepath.Join("cmd", rootPkg, rootPkg+".go")
}

// gofmtSource formats Go source, wrapping errors with the file it came from.
func gofmtSource(content, rel string) (string, error) {
	formatted, err := format.Source([]byte(content))
	if err != nil {
		return "", fmt.Errorf("formatting %s after feature splice: %w", rel, err)
	}
	return string(formatted), nil
}
