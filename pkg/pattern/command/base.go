// Package command defines the base Command type used by all climax commands.
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
