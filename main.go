// Package main is the entry point for the climax CLI tool.
package main

import (
	"fmt"
	"os"

	"github.com/StevenACoffman/climax/cmd"
)

const (
	exitFail    = 1
	exitSuccess = 0
)

func main() {
	if err := cmd.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(exitFail)
	}
	os.Exit(exitSuccess)
}
