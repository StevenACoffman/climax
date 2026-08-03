// Package lint implements the "lint" command.
// It identifies a climax-based application, parses its source files into an
// AST, and reports structural drift from the current scaffold templates in
// unified-diff format.
package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// Config holds the configuration for the lint command.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the lint command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("lint").SetParent(parent.Flags)
	cfg.Command = &ff.Command{
		Name:      "lint",
		Usage:     "climax lint [path]",
		ShortHelp: "check a climax application for structural drift from the scaffold templates",
		LongHelp: `Lint parses a climax-based application's source files using the Go AST and
reports structural deviations from the current scaffold templates.

Each issue is shown in unified-diff format:
  Lines prefixed with '-' are what was found in the application.
  Lines prefixed with '+' are what the template expects instead.
  Lines prefixed with ' ' are shared context.

Structural groups checked:

  main.go
    • signal.NotifyContext for graceful shutdown
    • separate run(ctx) function for testability
    • os.Stdin passed explicitly to cmd.Run

  cmd/cmd.go
    • stdin io.Reader parameter in Run
    • stdin forwarded to root.New
    • unmatched-subcommand guard (a mistyped subcommand otherwise exits 0)

  cmd/<root>/<root>.go
    • Stdin io.Reader field in Config
    • stdin io.Reader parameter in New
    • cfg.Stdin = stdin assignment

Warnings (reported but not fatal):

  any command file
    • un-filled scaffold help text (ShortHelp/LongHelp still holds a
      generated placeholder — the un-edited scaffold default)

  opted-in feature surface (per the // climax:features marker)
    • a generated feature file (outcome.go / logger.go / misplaced.go) is
      missing or has drifted from its current template
    • a --posix-guard app has a positional-taking command that never calls
      MisplacedFlag

The path argument defaults to the current directory, which must be the root
of a climax application (created by 'climax init').

Exits with status 1 when any errors are found; warnings do not affect the
exit code.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	if err := scaffold.IsClimaxApp(absDir); err != nil {
		return fmt.Errorf("lint: %w", err)
	}

	issues, err := scaffold.LintApp(absDir)
	if err != nil {
		return fmt.Errorf("lint: %w", err)
	}

	if len(issues) == 0 {
		_, _ = fmt.Fprintln(cfg.Stdout, "✓  No structural drift found.")
		return nil
	}

	var errCount, warnCount int
	for _, issue := range issues {
		if issue.Severity == scaffold.SeverityWarning {
			warnCount++
		} else {
			errCount++
		}
	}

	_, _ = fmt.Fprintf(cfg.Stdout, "%d issue(s) found in %s (%d error, %d warning)\n\n",
		len(issues), absDir, errCount, warnCount)
	for _, issue := range issues {
		marker := "✗"
		if issue.Severity == scaffold.SeverityWarning {
			marker = "⚠"
		}
		_, _ = fmt.Fprintf(cfg.Stdout, "%s  %s: %s\n\n", marker, issue.File, issue.Property)
		for line := range strings.SplitSeq(issue.Detail, "\n") {
			_, _ = fmt.Fprintf(cfg.Stdout, "   %s\n", line)
		}
		_, _ = fmt.Fprintln(cfg.Stdout)
	}

	// Warnings alone don't fail the build; only structural errors set a
	// non-zero exit code.
	if errCount > 0 {
		return root.ExitError(1)
	}
	return nil
}
