// Package cmd is the dispatcher for the climax CLI. It registers all commands
// and routes incoming arguments to the matching command implementation.
package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/StevenACoffman/climax/cmd/add"
	initialize "github.com/StevenACoffman/climax/cmd/initialize"
	"github.com/StevenACoffman/climax/cmd/version"
	"github.com/StevenACoffman/climax/pkg/pattern/command"
)

// Run dispatches args to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(args []string, stdout, stderr io.Writer) error {
	commands := []*command.Command{
		version.VersionCommand(),
		initialize.InitCommand(),
		add.AddCommand(),
	}

	m := make(map[string]*command.Command)
	for i := range commands {
		m[commands[i].UsageLine] = commands[i]
	}

	if len(args) == 0 || args[0] == "help" {
		// "help <command>" shows the Long description for that command.
		if len(args) == 2 {
			cmd := m[args[1]]
			if cmd == nil {
				return errors.New(args[1] + ": unknown command")
			}
			_, _ = fmt.Fprintln(stdout, cmd.Long)
			return nil
		}
		// "help" with no argument lists all commands.
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
