// Package cmd is the dispatcher for the climax CLI. It registers all commands
// and routes incoming arguments to the matching command implementation.
package cmd
// climax:name climax
// climax:root-pkg root

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/StevenACoffman/climax/cmd/add"
	initialize "github.com/StevenACoffman/climax/cmd/initialize"
	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/cmd/version"
	// climax:imports
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)
	version.New(r)
	initialize.New(r)
	add.New(r)
	// register new commands here

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
