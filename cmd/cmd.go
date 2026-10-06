// Package cmd is the dispatcher for the climax CLI.
// It registers all commands and routes incoming arguments
// to the matching command implementation.
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
	"github.com/StevenACoffman/climax/cmd/initialize"
	"github.com/StevenACoffman/climax/cmd/lint"
	"github.com/StevenACoffman/climax/cmd/mango"
	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/cmd/update"
	"github.com/StevenACoffman/climax/cmd/version"
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
//
// Every flag can be set via a CLIMAX_-prefixed environment variable.
// The mapping rule is: prepend CLIMAX_, uppercase, replace dashes with
// underscores. All flags are subcommand-specific; see each subcommand's --help
//
// Flags supplied on the command line always take precedence over env vars.
//
// Run prints the selected command's help to stderr only for --help and for
// usage errors (anything matching ErrUsage in the root package: parse
// failures, an unknown subcommand, or a command returning a UsageError). Other
// errors are returned without help, for main to print as "error: ...".
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	r := root.New(stdin, stdout, stderr)
	version.New(r)
	initialize.New(r)
	add.New(r)
	update.New(r)
	lint.New(r)
	mango.New(r)
	// register new commands here

	if err := r.Command.Parse(args, ff.WithEnvVarPrefix("CLIMAX")); err != nil {
		// --help asked for help, and any other parse failure (an unknown flag,
		// a bad flag value) is the command line's fault, so both show it.
		_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		if errors.Is(err, ff.ErrHelp) {
			return fmt.Errorf("parse: %w", err)
		}
		return &root.UsageError{Err: fmt.Errorf("parse: %w", err)}
	}

	// An unmatched token leaves the selected command a group parent (Exec == nil)
	// with a leftover positional. Without this guard it falls through to Run,
	// returns ff.ErrNoExec, and exits 0 — indistinguishable from a bare invocation.
	if sel := r.Command.GetSelected(); sel.Exec == nil {
		if rest := sel.Flags.GetArgs(); len(rest) > 0 {
			_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(sel))
			return &root.UsageError{Err: fmt.Errorf("%s: unknown subcommand %q", sel.Name, rest[0])}
		}
	}

	if err := r.Command.Run(ctx); err != nil {
		// Help is for usage mistakes only. A runtime failure's message stands on
		// its own, as do ff.ErrNoExec (no subcommand given) and ExitError (the
		// command already reported its outcome): none of them match ErrUsage.
		if errors.Is(err, root.ErrUsage) {
			_, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
