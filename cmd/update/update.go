// Package update implements the "update" command.
// It parses climax's own source files into an AST, detects structural drift
// between those files and the embedded scaffold templates, and optionally
// applies fixes to bring the templates back into alignment.
package update

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// climaxModule is the canonical module path for the climax tool itself.
// DetectDrift is only meaningful when run against climax's own source.
const climaxModule = "github.com/StevenACoffman/climax"

// Config holds the configuration for the update command.
type Config struct {
	*root.Config
	Apply   bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the update command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("update").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.Apply, 0, "apply", "apply detected fixes to scaffold templates")
	cfg.Command = &ff.Command{
		Name:      "update",
		Usage:     "climax update [--apply] [path]",
		ShortHelp: "detect (and optionally fix) drift between climax source and scaffold templates",
		LongHelp: `Update parses climax's own source files using the Go AST and compares their
structure against the scaffold template files in pkg/scaffold/templates/.

This command is for climax's own development workflow — run it whenever you
change a structural pattern in main.go, cmd/cmd.go, cmd/root/root.go,
cmd/version/version.go, or cmd/mango/mango.go to detect whether the
corresponding scaffold templates need updating.

File mapping (source → template):

  main.go                   →  main.go.tmpl
  cmd/cmd.go                →  cmd.go.tmpl
  cmd/root/root.go          →  root.go.tmpl
  cmd/version/version.go    →  version.go.tmpl
  cmd/mango/mango.go        →  man.go.tmpl

Structural properties checked:

  main      signal.NotifyContext, run() separation, os.Stdin passed to cmd.Run
  cmd       stdin io.Reader parameter in Run, stdin forwarded to root.New
  root      Stdin io.Reader field, stdin parameter in New, cfg.Stdin assignment
  version   JSON flag in Config, tabwriter output
  man       Section int field in Config

Without --apply the command prints a drift report and exits non-zero when drift
is found (suitable for CI). With --apply it patches the template files in-place
for each fixable item (source has property, template does not).

Items where the template has a property but the source does not are reported
but not auto-fixed, as removing things from templates is a manual decision.

The path argument defaults to the current directory, which must be the climax
module root (directory containing go.mod with module github.com/StevenACoffman/climax).`,
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

	// Verify this is the climax module.
	info, err := gomod.Find(absDir)
	if err != nil {
		return fmt.Errorf("finding go.mod: %w", err)
	}
	if info.Module != climaxModule {
		_, _ = fmt.Fprintf(cfg.Stderr, "  found module: %s\n", info.Module)
		_, _ = fmt.Fprintf(cfg.Stderr, "  expected:     %s\n", climaxModule)
		_, _ = fmt.Fprintf(cfg.Stderr, "  directory:    %s\n", absDir)
		return fmt.Errorf("update: this command can only run against the climax module itself")
	}

	// Detect drift via AST analysis.
	items, err := scaffold.DetectDrift(info.Root)
	if err != nil {
		return fmt.Errorf("detecting drift: %w", err)
	}

	if len(items) == 0 {
		fmt.Fprintln(cfg.Stdout, "✓  No drift detected — templates are in sync with source.")
		return nil
	}

	// Separate fixable from informational items.
	var fixable, manual []scaffold.DriftItem
	for _, item := range items {
		if item.InSource == "present" && item.InTemplate == "absent" {
			fixable = append(fixable, item)
		} else {
			manual = append(manual, item)
		}
	}

	// Print drift report.
	printReport(cfg.Stdout, len(items), fixable, manual)

	if !cfg.Apply {
		fmt.Fprintln(cfg.Stdout, "Run with --apply to patch the template files automatically.")
		return root.ExitError(1)
	}

	// Apply fixes (fixable items only; ApplyFixes ignores items without patches).
	if err := scaffold.ApplyFixes(info.Root, items); err != nil {
		return fmt.Errorf("applying fixes: %w", err)
	}
	_, _ = fmt.Fprintf(cfg.Stdout, "✓  Patched %d item(s) in template files.\n", len(fixable))
	return nil
}

// printReport writes the drift summary to w.
func printReport(w io.Writer, total int, fixable, manual []scaffold.DriftItem) {
	_, _ = fmt.Fprintf(w, "Drift detected: %d item(s)", total)
	if len(fixable) > 0 {
		_, _ = fmt.Fprintf(w, " (%d fixable with --apply)", len(fixable))
	}
	_, _ = fmt.Fprintf(w, "\n\n")

	for _, item := range fixable {
		_, _ = fmt.Fprintf(w, "  ✗  %-6s  %s\n", item.Template, item.Property)
	}
	if len(manual) > 0 {
		if len(fixable) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "  Requires manual review (template has property, source does not):")
		for _, item := range manual {
			_, _ = fmt.Fprintf(w, "  ⚠   %-6s  %s\n", item.Template, item.Property)
		}
	}
	fmt.Fprintln(w)
}
