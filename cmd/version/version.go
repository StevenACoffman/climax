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
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the version command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("version").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "output version information as JSON")
	cfg.Command = &ff.Command{
		Name:      "version",
		Usage:     "climax version [--json]",
		ShortHelp: "print version information",
		LongHelp: `Print build and version information for this climax binary.

Fields shown:

  GitVersion    module version tag (e.g. v0.3.1) or "devel" for local builds
  GitCommit     VCS commit hash
  GitTreeState  "clean" or "dirty" (whether the working tree had uncommitted changes)
  BuildDate     timestamp of the VCS commit used for the build
  BuiltBy       build system name when set via WithBuiltBy (e.g. goreleaser)
  GoVersion     Go toolchain version (e.g. go1.23.0)
  Compiler      Go compiler name (usually "gc")
  ModuleSum     go.sum checksum of the main module
  Platform      GOOS/GOARCH pair (e.g. darwin/arm64)

Use --json to get machine-readable output suitable for scripting.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	info := pkgversion.GetVersionInfo()
	if cfg.JSON {
		s, err := info.JSONString()
		if err != nil {
			return fmt.Errorf("version: %w", err)
		}
		_, _ = fmt.Fprintln(cfg.Stdout, s)
		return nil
	}
	_, _ = fmt.Fprint(cfg.Stdout, info.String())
	return nil
}
