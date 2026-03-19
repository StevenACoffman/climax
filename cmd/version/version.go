// Package version implements the "version" CLI command.
package version

import (
	"fmt"

	"github.com/StevenACoffman/climax/pkg/pattern/command"
	pkgversion "github.com/StevenACoffman/climax/pkg/version"
)

// VersionCommand is the only exported symbol in this package.
func VersionCommand() *command.Command {
	return &command.Command{
		UsageLine: "version",
		Short:     "Shows version",
		Long:      "Shows the version of this binary",
		Run:       versionCmd,
	}
}

func versionCmd(cmd *command.Command, _ []string) error {
	_, _ = fmt.Fprint(cmd.Stdout, pkgversion.GetVersionInfo().String())
	return nil
}
