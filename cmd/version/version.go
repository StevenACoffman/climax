// Package version implements the "version" CLI command.
package version

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"
	"github.com/StevenACoffman/climax/cmd/root"
	pkgversion "github.com/StevenACoffman/climax/pkg/version"
)

// Config holds the configuration for the version command.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the version command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("version").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "version",
		Usage:     "climax version",
		ShortHelp: "print version information",
		LongHelp:  "Shows the version of this binary.",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	_, _ = fmt.Fprint(cfg.Stdout, pkgversion.GetVersionInfo().String())
	return nil
}
