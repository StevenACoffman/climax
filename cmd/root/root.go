// Package root defines the root configuration for the climax CLI.
package root

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
	// No shared flags — Flags is nil. Subcommands call SetParent(parent.Flags)
	// which is a no-op here; add shared flags (e.g. BoolVar) to activate.
	cfg.Command = &ff.Command{
		Name:      "climax",
		Usage:     "climax <SUBCOMMAND> ...",
		ShortHelp: "a tool for scaffolding Climax-based CLI applications",
	}
	return &cfg
}
