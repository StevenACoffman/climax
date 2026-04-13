// Package mango implements the "mango" CLI command.
package mango

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// Config holds the configuration for the mango command.
type Config struct {
	*root.Config
	Section   int
	Authors   string
	Copyright string
	Flags     *ff.FlagSet
	Command   *ff.Command
}

// New creates and registers the mango command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("mango").SetParent(parent.Flags)
	cfg.Flags.IntVar(&cfg.Section, 0, "section", 1, "man page section number (1–8)")
	cfg.Flags.StringVar(
		&cfg.Authors,
		0,
		"authors",
		"",
		`bake a WithSection("Authors", ...) call into the generated command`,
	)
	cfg.Flags.StringVar(
		&cfg.Copyright,
		0,
		"copyright",
		"",
		`bake a WithSection("Copyright", ...) call into the generated command`,
	)
	cfg.Command = &ff.Command{
		Name:      "mango",
		Usage:     "climax mango [--section N] [--authors TEXT] [--copyright TEXT] [path]",
		ShortHelp: "add a man page command to a climax application",
		LongHelp: `Mango adds a "man" subcommand to the climax application rooted at path
(default: current directory). The generated command prints the application's
man page in roff format to stdout using github.com/StevenACoffman/mango-ff,
which derives flag entries, subcommand sections, and help text directly from
the ff.Command tree — no additional configuration required.

Optional metadata flags are baked directly into the generated source:

  --authors "Jane Doe <https://github.com/you/myapp>"
  --copyright "Released under the MIT licence."

After running climax mango, add the required dependencies to the target app:

  go get github.com/StevenACoffman/mango-ff github.com/muesli/roff

Then build and use the man page:

  myapp man              # print roff to stdout
  myapp man | man -l -   # view in the man pager`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	if cfg.Section < 1 || cfg.Section > 8 {
		return fmt.Errorf("mango: --section must be between 1 and 8, got %d", cfg.Section)
	}

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("mango: %w", err)
	}

	info, err := gomod.Find(absPath)
	if err != nil {
		return fmt.Errorf("mango: path is not inside a Go module: %w", err)
	}

	if err := scaffold.IsClimaxApp(absPath); err != nil {
		return fmt.Errorf("mango: %w", err)
	}

	importPrefix, err := info.ImportPath(absPath)
	if err != nil {
		return fmt.Errorf("mango: %w", err)
	}

	opts := scaffold.ManOptions{
		Section:   cfg.Section,
		Authors:   cfg.Authors,
		Copyright: cfg.Copyright,
	}

	created, modified, err := scaffold.AddManCommand(absPath, importPrefix, opts)
	if err != nil {
		return fmt.Errorf("mango: %w", err)
	}

	_, _ = fmt.Fprintf(cfg.Stdout, "added man page command\n")
	_, _ = fmt.Fprintf(cfg.Stdout, "  created  %s\n", created)
	_, _ = fmt.Fprintf(cfg.Stdout, "  modified %s\n", modified)
	_, _ = fmt.Fprintf(cfg.Stdout, "\nAdd dependencies:\n")
	_, _ = fmt.Fprintf(
		cfg.Stdout,
		"  go get github.com/StevenACoffman/mango-ff github.com/muesli/roff\n",
	)
	_, _ = fmt.Fprintf(cfg.Stdout, "\nView the man page:\n")
	_, _ = fmt.Fprintf(cfg.Stdout, "  go run . man | man -l -\n")
	return nil
}
