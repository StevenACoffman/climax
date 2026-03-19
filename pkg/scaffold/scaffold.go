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

// InitApp writes a complete Climax application scaffold to dir.
// importPrefix is the Go import path for the application root
// (e.g. "github.com/org/repo/tools/myapp").
func InitApp(dir, importPrefix string) error {
	files := []struct {
		rel     string
		content string
	}{
		{"main.go", applyImport(mainTemplate, importPrefix)},
		{filepath.Join("cmd", "cmd.go"), applyImport(cmdTemplate, importPrefix)},
		{filepath.Join("cmd", "version", "version.go"), applyImport(versionTemplate, importPrefix)},
		{filepath.Join("pkg", "pattern", "command", "base.go"), baseTemplate},
	}

	for _, f := range files {
		if err := writeFile(filepath.Join(dir, f.rel), f.content); err != nil {
			return fmt.Errorf("init: %w", err)
		}
	}
	return nil
}

// IsClimaxApp reports whether dir is the root of a Climax application
// created by InitApp. It returns a descriptive error if not.
func IsClimaxApp(dir string) error {
	required := []string{
		"main.go",
		filepath.Join("cmd", "cmd.go"),
		filepath.Join("pkg", "pattern", "command", "base.go"),
	}
	for _, rel := range required {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			return fmt.Errorf("not a climax app root: missing %s", rel)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "cmd", "cmd.go"))
	if err != nil {
		return fmt.Errorf("not a climax app root: cannot read cmd/cmd.go: %w", err)
	}
	if !strings.Contains(string(data), CommandsMarker) {
		return fmt.Errorf("not a climax app root: cmd/cmd.go missing %q marker", CommandsMarker)
	}
	return nil
}

// AddCommand creates cmd/<name>/<name>.go and registers it in cmd/cmd.go.
func AddCommand(dir, name, importPrefix string) error {
	if err := validateIdent(name); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	titleName := titleCase(name)

	// Write cmd/<name>/<name>.go.
	cmdFilePath := filepath.Join(dir, "cmd", name, name+".go")
	content := applyImport(newCmdTemplate, importPrefix)
	content = strings.ReplaceAll(content, "CMD_TITLE", titleName)
	content = strings.ReplaceAll(content, "CMD_NAME", name)
	if err := writeFile(cmdFilePath, content); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	// Register in cmd/cmd.go.
	if err := registerInCmdGo(dir, name, titleName, importPrefix); err != nil {
		return fmt.Errorf("add: %w", err)
	}
	return nil
}

func registerInCmdGo(dir, name, titleName, importPrefix string) error {
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

	// Insert factory call before the commands marker.
	// The marker line is "\t\t// register new commands here".
	callLine := fmt.Sprintf("%s.%sCommand(),\n\t\t", name, titleName)
	content = strings.Replace(content, CommandsMarker, callLine+CommandsMarker, 1)

	if err := os.WriteFile(cmdGoPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing cmd/cmd.go: %w", err)
	}

	return nil
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

func applyImport(tmpl, importPrefix string) string {
	return strings.ReplaceAll(tmpl, "APP_IMPORT", importPrefix)
}

func validateIdent(name string) error {
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
	"fmt"
	"os"

	"APP_IMPORT/cmd"
)

const (
	exitFail    = 1
	exitSuccess = 0
)

func main() {
	if err := cmd.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(exitFail)
	}
	os.Exit(exitSuccess)
}
`

const cmdTemplate = `// Package cmd is the dispatcher; it routes CLI arguments to the matching command.
package cmd

import (
	"errors"
	"fmt"
	"io"

	"APP_IMPORT/cmd/version"
	// climax:imports
	"APP_IMPORT/pkg/pattern/command"
)

// Run dispatches args to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(args []string, stdout, stderr io.Writer) error {
	commands := []*command.Command{
		version.VersionCommand(),
		// register new commands here
	}

	m := make(map[string]*command.Command)
	for i := range commands {
		m[commands[i].UsageLine] = commands[i]
	}

	if len(args) == 0 || args[0] == "help" {
		if len(args) == 2 {
			cmd := m[args[1]]
			if cmd == nil {
				return errors.New(args[1] + ": unknown command")
			}
			_, _ = fmt.Fprintln(stdout, cmd.Long)
			return nil
		}
		_, _ = fmt.Fprintln(stdout, "Available Commands:")
		for i := range commands {
			_, _ = fmt.Fprintf(stdout, "%s - %s\n", commands[i].UsageLine, commands[i].Short)
		}
		return nil
	}

	cmd := m[args[0]]
	if cmd == nil {
		return errors.New(args[0] + ": invalid command")
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run(cmd, args[1:])
}
`

const versionTemplate = `// Package version implements the "version" CLI command.
package version

import (
	"fmt"

	"APP_IMPORT/pkg/pattern/command"
)

// VersionCommand is the only exported symbol in this package.
func VersionCommand() *command.Command {
	return &command.Command{
		UsageLine: "version",
		Short:     "prints version information",
		Long:      "Version prints version information for the application.",
		Run:       versionCmd,
	}
}

func versionCmd(cmd *command.Command, _ []string) error {
	_, _ = fmt.Fprintln(cmd.Stdout, "version 0.0.1")
	return nil
}
`

const baseTemplate = `// Package command defines the base Command type used by all CLI commands.
package command

import "io"

// Command is the base type for all CLI commands.
type Command struct {
	// Run holds the command implementation. args are the arguments after the
	// command name — the executable and command name are already stripped.
	Run func(cmd *Command, args []string) error

	// UsageLine is the one-line usage message and the dispatch key.
	UsageLine string

	// Short is shown in 'help' output.
	Short string

	// Long is shown in 'help <command>' output.
	Long string

	// Stdout and Stderr are set by the dispatcher before Run is called.
	// Commands must write to these instead of os.Stdout / os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
}
`

const newCmdTemplate = `// Package CMD_NAME implements the "CMD_NAME" CLI command.
package CMD_NAME

import (
	"fmt"

	"APP_IMPORT/pkg/pattern/command"
)

// CMD_TITLECommand is the only exported symbol in this package.
func CMD_TITLECommand() *command.Command {
	return &command.Command{
		UsageLine: "CMD_NAME",
		Short:     "CMD_NAME command",
		Long:      "CMD_TITLE is a new command.",
		Run:       CMD_NAMECmd,
	}
}

func CMD_NAMECmd(cmd *command.Command, _ []string) error {
	_, _ = fmt.Fprintln(cmd.Stdout, "CMD_NAME")
	return nil
}
`
