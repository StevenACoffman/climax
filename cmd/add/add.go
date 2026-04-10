// Package add implements the "add" CLI command.
package add

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// Config holds the configuration for the add command.
type Config struct {
	*root.Config
	Name    string
	Short   string
	Long    string
	Parent  string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the add command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("add").SetParent(parent.Flags)
	cfg.Flags.StringVar(
		&cfg.Name,
		0,
		"name",
		"",
		"ff.Command.Name for the generated command (default: same as <name>; allows hyphens)",
	)
	cfg.Flags.StringVar(
		&cfg.Short,
		0,
		"short",
		"",
		`ShortHelp for the generated command (default: "<name> command")`,
	)
	cfg.Flags.StringVar(
		&cfg.Long,
		0,
		"long",
		"",
		`LongHelp for the generated command (default: "<Name> is a new command.")`,
	)
	cfg.Flags.StringVar(
		&cfg.Parent,
		'p',
		"parent",
		"",
		"Go package name of the parent command (default: root)",
	)
	cfg.Command = &ff.Command{
		Name:      "add",
		Usage:     "climax add [FLAGS] <name> [path]",
		ShortHelp: "add a new command to a climax application",
		LongHelp: "Add (climax add [FLAGS] <name> [path]) creates a new command package at " +
			"cmd/<name>/<name>.go inside the Climax application rooted at path " +
			"(default: current directory) and registers it in cmd/cmd.go. " +
			"The path must be inside an existing Go module and must be the root " +
			"of an application previously created by 'climax init'.",
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("add: command name required (usage: climax add [FLAGS] <name> [path])")
	}
	name := args[0]

	path := "."
	if len(args) > 1 {
		path = args[1]
	}

	if cfg.Name != "" {
		if err := scaffold.ValidateCliName(cfg.Name); err != nil {
			return fmt.Errorf("add: --name: %w", err)
		}
	}

	if cfg.Parent != "" {
		if err := scaffold.ValidateIdent(cfg.Parent); err != nil {
			return fmt.Errorf("add: --parent: %w", err)
		}
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

	opts := scaffold.AddOptions{
		Name:   cfg.Name,
		Short:  cfg.Short,
		Long:   cfg.Long,
		Parent: cfg.Parent,
	}

	created, modified, err := scaffold.AddCommand(absPath, name, importPrefix, opts)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	_, _ = fmt.Fprintf(cfg.Stdout, "added command %q\n", name)
	_, _ = fmt.Fprintf(cfg.Stdout, "  created  %s\n", created)
	_, _ = fmt.Fprintf(cfg.Stdout, "  modified %s\n", modified)
	return nil
}
