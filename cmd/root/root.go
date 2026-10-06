// Package root defines the root configuration for the CLI.
package root

import (
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
)

// ErrUsage matches any usage error through [errors.Is]: a missing or
// malformed argument, an unknown flag or subcommand, conflicting flags. The
// dispatcher prints the selected command's help only for errors that match
// ErrUsage (and for --help), so a runtime failure is reported without a
// screenful of help above it, and main exits 2 for them where other failures
// exit 1. For the details, recover [*UsageError] with [errors.AsType].
var ErrUsage = errors.New("invalid usage")

// ExitError is returned by commands that want a specific non-zero exit code
// without printing an additional error message. run() in main.go checks for
// ExitError with errors.As and calls os.Exit(int(e)) directly, bypassing the
// default "error: ..." printer.
type ExitError int

// UsageError reports that the command line, not the work it asked for, was
// wrong. Err says what was wrong, e.g. "add: command name required"; its text
// is the whole message. A command's exec returns one as
//
//	return &UsageError{Err: fmt.Errorf("add: --name: %w", err)}
//
// building Err with errors.New or fmt.Errorf, whose %w keeps a cause
// inspectable.
type UsageError struct {
	Err error
}

// Config holds shared I/O writers and the root ff.Command.
// All subcommand configs embed *Config to inherit these.
type Config struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Flags   *ff.FlagSet
	Command *ff.Command
}

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e *UsageError) Error() string {
	if e.Err == nil {
		return ErrUsage.Error()
	}
	return e.Err.Error()
}

// Unwrap returns the cause, so errors.Is and errors.As see through to it.
func (e *UsageError) Unwrap() error { return e.Err }

// Is reports whether target is [ErrUsage].
func (e *UsageError) Is(target error) bool { return target == ErrUsage }

// New returns a new root Config with the given I/O writers.
func New(stdin io.Reader, stdout, stderr io.Writer) *Config {
	var cfg Config
	cfg.Stdin = stdin
	cfg.Stdout = stdout
	cfg.Stderr = stderr
	// No shared flags — cfg.Flags is nil; ff provides --help automatically.
	// Subcommands call SetParent(parent.Flags)
	// which is a no-op here; add shared flags (e.g. BoolVar) to activate.
	// To add shared flags, uncomment and bind before constructing the command:
	// cfg.Flags = ff.NewFlagSet("climax")
	// cfg.Flags.BoolVar(&cfg.MyFlag, 0, "my-flag", "", "description")
	cfg.Command = &ff.Command{
		Name:      "climax",
		Usage:     "climax <SUBCOMMAND> ...",
		ShortHelp: "a tool for scaffolding Climax-based CLI applications",
	}
	return &cfg
}
