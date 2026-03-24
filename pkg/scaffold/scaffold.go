// Package scaffold generates Climax application and command files.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
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
	Name  string // ff.Command.Name; defaults to the Go package name (positional arg)
	Short string // ff.Command.ShortHelp; defaults to "<name> command"
	Long  string // ff.Command.LongHelp; defaults to "<Name> is a new command."
}

// InitOptions controls what InitApp generates.
type InitOptions struct {
	ImportPrefix string // Go import path for the application root (required)
	Name         string // ff.Command.Name for the root; defaults to last segment of ImportPrefix
	Short        string // ff.Command.ShortHelp for the root; defaults to "TODO: describe <name> here"
	Long         string // ff.Command.LongHelp for the root; omitted if empty
	RootPkg      string // Go package name (and file basename) for the root config; defaults to "root"
	NoVersion    bool   // when true, skip generating cmd/version/version.go
}

// InitApp writes a complete Climax application scaffold to dir.
func InitApp(dir string, opts InitOptions) error {
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

	// Build conditional template fragments.
	longHelpLine := ""
	if opts.Long != "" {
		longHelpLine = "\t\tLongHelp:  \"" + strings.ReplaceAll(opts.Long, `"`, `\"`) + "\",\n"
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
		"ROOT_PKG":       opts.RootPkg,
		"APP_SHORT":      opts.Short,
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

	for _, f := range files {
		content := applyVars(f.tmpl, vars)
		if err := writeFile(filepath.Join(dir, f.rel), content); err != nil {
			return err
		}
	}
	return nil
}

// IsClimaxApp reports whether dir is the root of a Climax application
// created by InitApp. It returns a descriptive error if not.
func IsClimaxApp(dir string) error {
	for _, rel := range []string{"main.go", filepath.Join("cmd", "cmd.go")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			return fmt.Errorf("not a climax app root: missing %s", rel)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "cmd", "cmd.go"))
	if err != nil {
		return fmt.Errorf("not a climax app root: cannot read cmd/cmd.go: %w", err)
	}
	content := string(data)

	for _, marker := range []string{ImportsMarker, CommandsMarker} {
		if !strings.Contains(content, marker) {
			return fmt.Errorf("not a climax app root: cmd/cmd.go missing %q marker", marker)
		}
	}

	// Determine root package from marker (default "root" for backwards compat).
	rootPkg := readMarker(content, "// climax:root-pkg")
	if rootPkg == "" {
		rootPkg = "root"
	}

	rootFile := filepath.Join("cmd", rootPkg, rootPkg+".go")
	if _, err := os.Stat(filepath.Join(dir, rootFile)); err != nil {
		return fmt.Errorf("not a climax app root: missing %s", rootFile)
	}

	return nil
}

// AddCommand creates cmd/<name>/<name>.go and registers it in cmd/cmd.go.
func AddCommand(dir, name, importPrefix string, opts AddOptions) error {
	if err := ValidateIdent(name); err != nil {
		return err
	}

	// Read persisted values from cmd/cmd.go markers.
	cmdGoPath := filepath.Join(dir, "cmd", "cmd.go")
	data, err := os.ReadFile(cmdGoPath)
	if err != nil {
		return fmt.Errorf("reading cmd/cmd.go: %w", err)
	}
	content := string(data)

	cliName := readMarker(content, "// climax:name")
	if cliName == "" {
		// Fallback: derive from import path (pre-marker apps).
		parts := strings.Split(importPrefix, "/")
		cliName = parts[len(parts)-1]
	}

	rootPkg := readMarker(content, "// climax:root-pkg")
	if rootPkg == "" {
		rootPkg = "root"
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

	vars := map[string]string{
		"APP_IMPORT":  importPrefix,
		"APP_NAME":    cliName,
		"ROOT_PKG":    rootPkg,
		"CMD_NAME":    name,
		"CMD_FF_NAME": ffName,
		"CMD_SHORT":   short,
		"CMD_LONG":    long,
	}

	// Write cmd/<name>/<name>.go.
	cmdFilePath := filepath.Join(dir, "cmd", name, name+".go")
	if err := writeFile(cmdFilePath, applyVars(newCmdTemplate, vars)); err != nil {
		return err
	}

	// Register in cmd/cmd.go.
	return registerInCmdGo(dir, name, importPrefix)
}

func registerInCmdGo(dir, name, importPrefix string) error {
	cmdGoPath := filepath.Join(dir, "cmd", "cmd.go")
	data, err := os.ReadFile(cmdGoPath)
	if err != nil {
		return fmt.Errorf("reading cmd/cmd.go: %w", err)
	}
	content := string(data)

	if !strings.Contains(content, ImportsMarker) {
		return fmt.Errorf("cmd/cmd.go is missing the %s marker", ImportsMarker)
	}
	if !strings.Contains(content, CommandsMarker) {
		return fmt.Errorf("cmd/cmd.go is missing the %s marker", CommandsMarker)
	}

	// Insert import before the climax:imports marker.
	// The marker line in the file is "\t// climax:imports", so we replace just
	// the marker text; the existing leading tab remains, giving correct indentation.
	importLine := fmt.Sprintf("\"%s/cmd/%s\"\n\t", importPrefix, name)
	content = strings.Replace(content, ImportsMarker, importLine+ImportsMarker, 1)

	// Insert New(r) call before the commands marker.
	// The marker line is "\t// register new commands here".
	callLine := fmt.Sprintf("%s.New(r)\n\t", name)
	content = strings.Replace(content, CommandsMarker, callLine+CommandsMarker, 1)

	if err := os.WriteFile(cmdGoPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing cmd/cmd.go: %w", err)
	}

	return nil
}

// readMarker scans content line by line for "// <prefix> <value>" and returns
// the trimmed value after the prefix. Returns "" if not found.
func readMarker(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
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
		return fmt.Errorf("name cannot be empty")
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
		return fmt.Errorf("command name cannot be empty")
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

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// ── templates ────────────────────────────────────────────────────────────────

const mainTemplate = `// Package main is the entry point for the CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/peterbourgon/ff/v4"
	"APP_IMPORT/cmd"
)

const (
	exitFail    = 1
	exitSuccess = 0
)

func main() {
	ctx := context.Background()
	err := cmd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		os.Exit(exitSuccess)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(exitFail)
	}
}
`

const cmdTemplate = `// Package cmd is the dispatcher; it routes CLI arguments to the matching command.
package cmd
// climax:name APP_NAME
// climax:root-pkg ROOT_PKG

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"APP_IMPORT/cmd/ROOT_PKG"
VERSION_IMPORT	// climax:imports
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := ROOT_PKG.New(stdout, stderr)
VERSION_CALL	// register new commands here

	if err := r.Command.Parse(args); err != nil {
		fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	if err := r.Command.Run(ctx); err != nil {
		if !errors.Is(err, ff.ErrNoExec) {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
`

const rootTemplate = `// Package ROOT_PKG defines the root configuration for the CLI.
package ROOT_PKG

import (
	"io"

	"github.com/peterbourgon/ff/v4"
)

// Config holds shared I/O writers and the root ff.Command.
// All subcommand configs embed *Config to inherit these.
type Config struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New returns a new root Config with the given I/O writers.
func New(stdout, stderr io.Writer) *Config {
	var cfg Config
	cfg.Stdout = stdout
	cfg.Stderr = stderr
	// No shared flags — cfg.Flags is nil; ff provides --help automatically.
	// To add shared flags, uncomment and bind before constructing the command:
	// cfg.Flags = ff.NewFlagSet("APP_NAME")
	// cfg.Flags.BoolVar(&cfg.MyFlag, 0, "my-flag", "", "description")
	cfg.Command = &ff.Command{
		Name:      "APP_NAME",
		Usage:     "APP_NAME <SUBCOMMAND> ...",
		ShortHelp: "APP_SHORT",
LONG_HELP_LINE	}
	return &cfg
}
`

const versionTemplate = `// Package version implements the "version" CLI command.
package version

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"
	"APP_IMPORT/cmd/ROOT_PKG"
)

// Version is the application version string.
// Override at build time: go build -ldflags "-X 'APP_IMPORT/cmd/version.Version=1.2.3'"
var Version = "dev"

// Config holds the configuration for the version command.
type Config struct {
	*ROOT_PKG.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the version command with the given parent config.
func New(parent *ROOT_PKG.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("version").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "version",
		Usage:     "APP_NAME version",
		ShortHelp: "print version information",
		LongHelp:  "Prints version information for the application.",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	_, _ = fmt.Fprintln(cfg.Stdout, "version "+Version)
	return nil
}
`

const newCmdTemplate = `// Package CMD_NAME implements the "CMD_FF_NAME" CLI command.
package CMD_NAME

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"
	"APP_IMPORT/cmd/ROOT_PKG"
)

// Config holds the configuration for the CMD_FF_NAME command.
type Config struct {
	*ROOT_PKG.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the CMD_FF_NAME command with the given parent config.
func New(parent *ROOT_PKG.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("CMD_FF_NAME").SetParent(parent.Flags)
	// bind flags: cfg.Flags.StringVar(&cfg.SomeFlag, 0, "some-flag", "", "description")
	cfg.Command = &ff.Command{
		Name:      "CMD_FF_NAME",
		Usage:     "APP_NAME CMD_FF_NAME [FLAGS]",
		ShortHelp: "CMD_SHORT",
		LongHelp:  "CMD_LONG",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	// TODO: implement CMD_FF_NAME.
	// Rename the second parameter from _ to args to access positional arguments.
	_, _ = fmt.Fprintln(cfg.Stdout, "CMD_FF_NAME: not yet implemented")
	return nil
}
`
