// Package initialize implements the "init" CLI command.
package initialize

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/StevenACoffman/climax/pkg/gomod"
	"github.com/StevenACoffman/climax/pkg/pattern/command"
	"github.com/StevenACoffman/climax/pkg/scaffold"
)

// InitCommand is the only exported symbol in this package.
func InitCommand() *command.Command {
	return &command.Command{
		UsageLine: "init",
		Short:     "initialize a new climax application",
		Long: "Init (climax init [path]) creates a new Climax-based CLI application " +
			"at path (default: current directory). The path must be inside an existing " +
			"Go module. The generated application follows the Climax command pattern.",
		Run: initCmd,
	}
}

func initCmd(cmd *command.Command, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
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

	if err := scaffold.InitApp(absPath, importPrefix); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.Stdout, "initialized climax app at %s (import: %s)\n",
		absPath, importPrefix)
	return nil
}
