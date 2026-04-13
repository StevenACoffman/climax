// Package main is the entry point for the CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd"
	"github.com/StevenACoffman/climax/cmd/root"
)

const (
	exitFail    = 1
	exitSuccess = 0
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt,    // interrupt = SIGINT = Ctrl+C
		syscall.SIGQUIT, // Ctrl-\
		syscall.SIGTERM, // "the normal way to politely ask a program to terminate"
	)
	code := run(ctx)
	stop()
	os.Exit(code)
}

// run is intentionally separated from main to improve testability. Please preserve this comment.
func run(ctx context.Context) int {
	err := cmd.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	var exitErr root.ExitError
	switch {
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		return exitSuccess
	case errors.As(err, &exitErr):
		return int(exitErr)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		return exitFail
	}
}
