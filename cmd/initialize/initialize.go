// Package initialize implements the "init" CLI command.
package initialize

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// Config holds the configuration for the init command.
type Config struct {
	*root.Config
	Name      string
	Short     string
	Long      string
	RootPkg   string
	NoVersion bool
	Flags     *ff.FlagSet
	Command   *ff.Command
}

// New creates and registers the init command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("init").SetParent(parent.Flags)
	cfg.Flags.StringVar(
		&cfg.Name,
		0,
		"name",
		"",
		"CLI name for the root ff.Command (default: last import path segment; allows hyphens)",
	)
	cfg.Flags.StringVar(
		&cfg.Short,
		0,
		"short",
		"",
		`ShortHelp for the root command (default: "TODO: describe <name> here")`,
	)
	cfg.Flags.StringVar(
		&cfg.Long,
		0,
		"long",
		"",
		"LongHelp for the root command (omitted if not set)",
	)
	cfg.Flags.StringVar(
		&cfg.RootPkg,
		0,
		"root-pkg",
		"",
		`Go package name for the root config package (default: "root")`,
	)
	cfg.Flags.BoolVar(&cfg.NoVersion, 0, "no-version", "skip generating cmd/version/version.go")
	cfg.Command = &ff.Command{
		Name:      "init",
		Usage:     "climax init [FLAGS] [path]",
		ShortHelp: "initialize a new climax application",
		LongHelp: "Init (climax init [FLAGS] [path]) creates a new Climax-based CLI application " +
			"at path (default: current directory). The path must be inside an existing " +
			"Go module. The generated application follows the Climax command pattern.",
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	if cfg.Name != "" {
		if err := scaffold.ValidateCliName(cfg.Name); err != nil {
			return fmt.Errorf("init: --name: %w", err)
		}
	}
	if cfg.RootPkg != "" {
		if err := scaffold.ValidateIdent(cfg.RootPkg); err != nil {
			return fmt.Errorf("init: --root-pkg: %w", err)
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	info, err := gomod.Find(absPath)
	if err != nil {
		return fmt.Errorf("init: path is not inside a Go module: %w", err)
	}

	importPrefix, err := info.ImportPath(absPath)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	opts := scaffold.InitOptions{
		ImportPrefix: importPrefix,
		Name:         cfg.Name,
		Short:        cfg.Short,
		Long:         cfg.Long,
		RootPkg:      cfg.RootPkg,
		NoVersion:    cfg.NoVersion,
	}

	written, err := scaffold.InitApp(absPath, opts)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	_, _ = fmt.Fprintf(cfg.Stdout, "initialized climax app at %s (import: %s)\n",
		absPath, importPrefix)
	for _, f := range written {
		_, _ = fmt.Fprintf(cfg.Stdout, "  created %s\n", f)
	}
	return nil
}
