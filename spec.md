# Go CLI Command Pattern Spec

## Overview

This project implements a CLI dispatcher using the idiomatic Go command pattern,
modeled after the Go toolchain's own `cmd/go` implementation. Do not use
third-party CLI frameworks (cobra, urfave/cli, etc.). Do not use interfaces for
command polymorphism. Use the `Command` struct with function pointers.

Note: Replace <org>/<repo> below with the Go module path from go.mod.

---

## Directory Structure

```
/
├── main.go                        # Entry point only — no logic
├── go.mod
├── cmd/
│   ├── cmd.go                     # Dispatcher and command registration (package cmd)
│   ├── version/
│   │   └── version.go             # Version command
│   └── <name>/
│       └── <name>.go              # One package per command
└── pkg/
    └── pattern/
        └── command/
            └── base.go            # Command struct definition
```

---

## Base Type

Do not modify this struct. All commands use it directly.

```go
// pkg/pattern/command/base.go
package command

type Command struct {
    // Run holds the command implementation. args are the arguments after the
    // command name — the executable and command name are already stripped.
    Run func(cmd *Command, args []string) error

    // UsageLine is the one-line usage message and the dispatch key.
    UsageLine string

    // Short is shown in 'help' output.
    Short string

    // Long is shown in 'help <command>' output.
    Long string
}
```

---

## Adding a New Command

Each command lives in its own package under `cmd/`. The package contains exactly
two things: an exported factory function and an unexported implementation function.

### Command Template

```go
// cmd/<name>/<name>.go
package <name>

import (
    "github.com/<org>/<repo>/pkg/pattern/command"
)

// <Name>Command is the only exported symbol in this package.
func <Name>Command() *command.Command {
    return &command.Command{
        UsageLine: "<cli-name>",
        Short:     "<one-line description>",
        Long:      "<paragraph description>",
        Run:       <name>Cmd,
    }
}

// <name>Cmd holds the implementation. It is unexported.
func <name>Cmd(cmd *command.Command, args []string) error {
    // implementation
    return nil
}
```

### Rules

- The factory function is the only exported symbol in the package.
- Command logic goes in the unexported `<name>Cmd` function, not in the factory.
- `UsageLine` is the exact string the user types on the CLI. It is also the
  dispatch key — it must be unique across all commands.
- Return `error`. Do not call `os.Exit` inside a command.
- Error strings are lowercase, no trailing punctuation, format: `<command>: <reason>`.

### Concrete Example — command with no arguments

The `greet` command below is a concrete application of the template above.

```go
// cmd/greet/greet.go
package greet

import (
    "fmt"

    "github.com/<org>/<repo>/pkg/pattern/command"
)

func GreetCommand() *command.Command {
    return &command.Command{
        UsageLine: "greet",
        Short:     "prints a greeting",
        Long:      "Prints a greeting to the user.",
        Run:       greetCmd,
    }
}

func greetCmd(cmd *command.Command, args []string) error {
    fmt.Println("Hello!")
    return nil
}
```

### Concrete Example — command that reads positional arguments

```go
// cmd/echo/echo.go
package echo

import (
    "fmt"
    "strings"

    "github.com/<org>/<repo>/pkg/pattern/command"
)

func EchoCommand() *command.Command {
    return &command.Command{
        UsageLine: "echo",
        Short:     "echoes arguments",
        Long:      "Prints all provided arguments joined by spaces.",
        Run:       echoCmd,
    }
}

func echoCmd(cmd *command.Command, args []string) error {
    if len(args) == 0 {
        return fmt.Errorf("echo: no arguments provided")
    }
    fmt.Println(strings.Join(args, " "))
    return nil
}
```

### Concrete Example — command with flags

Use `flag.NewFlagSet` scoped to the command function. Do not use the global
`flag` package or cobra-style flag binding.

```go
// cmd/shout/shout.go
package shout

import (
    "flag"
    "fmt"
    "strings"

    "github.com/<org>/<repo>/pkg/pattern/command"
)

func ShoutCommand() *command.Command {
    return &command.Command{
        UsageLine: "shout",
        Short:     "prints arguments in uppercase",
        Long:      "Prints arguments in uppercase. Use -n to suppress newline.",
        Run:       shoutCmd,
    }
}

func shoutCmd(cmd *command.Command, args []string) error {
    fs := flag.NewFlagSet("shout", flag.ContinueOnError)
    noNewline := fs.Bool("n", false, "suppress trailing newline")
    if err := fs.Parse(args); err != nil {
        return fmt.Errorf("shout: %w", err)
    }
    out := strings.ToUpper(strings.Join(fs.Args(), " "))
    if *noNewline {
        fmt.Print(out)
    } else {
        fmt.Println(out)
    }
    return nil
}
```

---

## Registering Commands

`cmd/cmd.go` is the **only place commands are registered**. It declares
`package cmd` — this is a dispatcher package, not a binary. Add a factory call
to the `commands` slice. Do not register commands anywhere else (no `init()`,
no globals).

```go
// cmd/cmd.go
package cmd

import (
    "errors"
    "fmt"

    "github.com/<org>/<repo>/cmd/greet"
    "github.com/<org>/<repo>/cmd/version"
    "github.com/<org>/<repo>/pkg/pattern/command"
)

// Run dispatches args to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(args []string) error {
    commands := []*command.Command{
        version.VersionCommand(),
        greet.GreetCommand(),
        // register new commands here
    }

    m := make(map[string]*command.Command)
    for i := range commands {
        m[commands[i].UsageLine] = commands[i]
    }

    if len(args) == 0 || (len(args) == 1 && args[0] == "help") {
        fmt.Println("Available Commands:")
        for i := range commands {
            fmt.Printf("%s - %s\n", commands[i].UsageLine, commands[i].Short)
        }
        return nil
    }

    cmd := m[args[0]]
    if cmd == nil {
        return errors.New(args[0] + ": invalid command")
    }
    // Strip the command name before passing args to Run.
    return cmd.Run(cmd, args[1:])
}
```

### Dispatch Rules

- `Run` receives `os.Args[1:]` — executable name already removed by `main`.
- `cmd.Run(cmd, args[1:])` — command name is also removed before dispatch.
- Lookup is by exact `UsageLine` match via map. No prefix matching, no fuzzy matching.
- Unknown command returns an error; `main` owns the exit code.

---

## Entry Point

`main.go` is intentionally thin. It passes arguments, handles errors, and exits.
No logic belongs here.

```go
// main.go
package main

import (
    "fmt"
    "os"

    "github.com/<org>/<repo>/cmd"
)

const (
    exitFail    = 1
    exitSuccess = 0
)

func main() {
    if err := cmd.Run(os.Args[1:]); err != nil {
        fmt.Printf("error: %+v\n", err)
        os.Exit(exitFail)
    }
    os.Exit(exitSuccess)
}
```

---

## Constraints

| Rule | Rationale |
|---|---|
| No CLI frameworks | The pattern is self-contained; frameworks add indirection |
| No `Commander` interface | Go composition via function pointer is sufficient |
| No `init()` for registration | Explicit registration in `cmd.go` is easier to trace |
| No `switch`/`case` dispatch | Map dispatch is O(1) and requires no changes to the dispatcher |
| One command per package | Keeps packages focused; factory function is the only public API |
| Errors bubble to `main` | Commands don't call `os.Exit`; only `main` controls exit codes |
| `UsageLine` is the dispatch key | It must exactly match what the user types |
| `flag.NewFlagSet` for flags | Scoped flag sets prevent global state; never use `flag.Parse` directly |
| `package cmd` is not a binary | It is a dispatcher package imported by `main`, not a standalone binary |

---

## Checklist: Adding a New Command

- [ ] Create `cmd/<name>/` package
- [ ] Write exported `<Name>Command() *command.Command` factory function
- [ ] Write unexported `<name>Cmd(cmd *command.Command, args []string) error`
- [ ] Set a unique `UsageLine` (this is the CLI verb)
- [ ] Add `<name>.<Name>Command()` to the `commands` slice in `cmd/cmd.go`
- [ ] Add the import for the new package in `cmd/cmd.go`
