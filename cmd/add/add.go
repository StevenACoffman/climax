// Package add implements the "add" CLI command.
package add

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/pattern/command"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// AddCommand is the only exported symbol in this package.
func AddCommand() *command.Command {
	return &command.Command{
		UsageLine: "add",
		Short:     "add a new command to a climax application",
		Long: "Add (climax add <name> [path]) creates a new command package at " +
			"cmd/<name>/<name>.go inside the Climax application rooted at path " +
			"(default: current directory) and registers it in cmd/cmd.go. " +
			"The path must be inside an existing Go module and must be the root " +
			"of an application previously created by 'climax init'.",
		Run: addCmd,
	}
}

func addCmd(cmd *command.Command, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("add: command name required (usage: climax add <name> [path])")
	}
	name := fs.Arg(0)

	path := "."
	if fs.NArg() > 1 {
		path = fs.Arg(1)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	info, err := gomod.Find(absPath)
	if err != nil {
		return fmt.Errorf("add: path is not inside a Go module: %w", err)
	}

	if err := scaffold.IsClimaxApp(absPath); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	importPrefix, err := info.ImportPath(absPath)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	if err := scaffold.AddCommand(absPath, name, importPrefix); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.Stdout, "added command %q to %s\n", name, absPath)
	return nil
}
