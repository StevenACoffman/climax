# Go CLI Command Pattern Spec

## Overview

This project implements a CLI dispatcher using `github.com/peterbourgon/ff/v4` (`ff`).
Commands are represented as `ff.Command` values. Do not use other CLI frameworks
(cobra, urfave/cli, etc.). Do not use interfaces for command polymorphism.
Use the `ff.Command` struct with the Config struct pattern described below.

Note: Replace `<org>/<repo>` below with the Go module path from `go.mod`.

**Flags-first configuration is a core requirement.** Every knob that affects
behaviour must be a registered flag on an `ff.FlagSet`. `ff` then knows how to
parse that value from CLI args, environment variables, and config files, and how
to describe it in `-h` output. The invariant to preserve: running any command
with `-h` (at any depth) must reveal the complete configuration surface area of
that command. Never hide behaviour behind hard-coded values, package-level
variables, or any mechanism that bypasses the flag set.

______________________________________________________________________

## Directory Structure

```
/
├── main.go                        # Entry point only — no logic
├── go.mod
├── cmd/
│   ├── cmd.go                     # Dispatcher and command registration (package cmd)
│   ├── root/                      # Default name; configurable via climax init --root-pkg
│   │   └── root.go                # Config: shared I/O, flags, root ff.Command, ExitError
│   ├── version/
│   │   └── version.go             # Version command (omit with climax init --no-version)
│   └── <name>/
│       └── <name>.go              # One package per command
```

The dispatcher file may also be named `command.go`; climax's AST analysis accepts either.

______________________________________________________________________

## Command Type

All commands use `ff.Command` from `github.com/peterbourgon/ff/v4`. The relevant
exported fields are:

```go
type Command struct {
    Name        string                               // dispatch key; case-insensitive match
    Usage       string                               // one-line syntax, e.g. "<cli> <cmd> [FLAGS] <ARG>"
    ShortHelp   string                               // shown next to name in parent help
    LongHelp    string                               // shown in command's own help (optional)
    Flags       ff.Flags                             // nil → empty flag set; --help always works
    Subcommands []*Command                           // populated by each subcommand's New()
    Exec        func(context.Context, []string) error // nil → returns ff.ErrNoExec
}
```

Do not call `Parse`, `Run`, or any other method on `ff.Command` from command
packages. Those are called exclusively by the dispatcher in `cmd/cmd.go`.

______________________________________________________________________

## Root Config

`cmd/root/root.go` defines `Config`. It holds `stdin`/`stdout`/`stderr`, any flags
shared across all commands, and the root `ff.Command`. It also declares `ExitError`
(see below). Subcommand configs embed `*root.Config` to inherit these.

```go
// cmd/root/root.go
package root

import (
    "fmt"
    "io"

    "github.com/peterbourgon/ff/v4"
)

// ExitError lets a command exit with a specific code without printing "error: ...".
// Return it from exec; main.go handles it via errors.As.
type ExitError int

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

type Config struct {
    Stdin   io.Reader
    Stdout  io.Writer
    Stderr  io.Writer
    Flags   *ff.FlagSet
    Command *ff.Command
}

func New(stdin io.Reader, stdout, stderr io.Writer) *Config {
    var cfg Config
    cfg.Stdin = stdin
    cfg.Stdout = stdout
    cfg.Stderr = stderr
    // No shared flags by default — cfg.Flags is nil; ff provides --help automatically.
    // To add shared flags (e.g. --verbose), uncomment and bind before constructing the command:
    // cfg.Flags = ff.NewFlagSet("<cli-name>")
    // cfg.Flags.BoolVar(&cfg.Verbose, 'v', "verbose", "log verbose output")
    cfg.Command = &ff.Command{
        Name:      "<cli-name>",
        Usage:     "<cli-name> <SUBCOMMAND> ...",
        ShortHelp: "<one-line description>",
        Flags:     cfg.Flags,
    }
    return &cfg
}
```

______________________________________________________________________

## ExitError

`root.ExitError` lets a command exit with a specific non-zero code without
printing an `error: ...` line. The dispatcher suppresses help output for it,
and `run()` in `main.go` calls `os.Exit` directly:

```go
// In any command's exec function:
if !ok {
    return root.ExitError(1) // exits 1; no "error:" printed
}
return nil // exits 0
```

______________________________________________________________________

## Adding a New Command

Each command lives in its own package under `cmd/`. The package contains a
`Config` struct, an exported `New` factory, and an unexported `exec` method.

### Command Template

```go
// cmd/<name>/<name>.go
package <name>

import (
    "context"
    "fmt"

    "github.com/peterbourgon/ff/v4"
    "<org>/<repo>/cmd/root"
)

type Config struct {
    *root.Config
    // command-local flag values declared here
    Flags   *ff.FlagSet
    Command *ff.Command
}

func New(parent *root.Config) *Config {
    var cfg Config
    cfg.Config = parent
    cfg.Flags = ff.NewFlagSet("<name>").SetParent(parent.Flags)
    // bind flags: cfg.Flags.StringVar(&cfg.SomeFlag, 0, "some-flag", "", "description")
    cfg.Command = &ff.Command{
        Name:      "<name>",
        Usage:     "<cli-name> <name> [FLAGS]",
        ShortHelp: "<one-line description>",
        LongHelp:  "<paragraph description>",
        Flags:     cfg.Flags,
        Exec:      cfg.exec,
    }
    parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
    return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
    // cfg.Stdin, cfg.Stdout, cfg.Stderr available via embedded root.Config
    // flag values available as cfg fields — already parsed before exec is called
    _, _ = fmt.Fprintln(cfg.Stdout, "<name>: not yet implemented")
    return nil
}
```

### Rules

- `New` and `Config` are the only exported identifiers in the package (commands that need a user-visible package-level variable, such as `Version string`, may export it too).
- `New` appends to `parent.Command.Subcommands` — no other registration needed.
- Flag values are bound to `Config` fields in `New()`, not inside `exec`.
- Every behavioral knob must be a registered flag on `cfg.Flags`. Never use hard-coded values, package-level variables, or `os.Getenv` calls outside the flag set — they make settings invisible to `-h`.
- `SetParent(parent.Flags)` must be called on every subcommand flag set so that parent flags are accepted at any depth.
- Write to `cfg.Stdout` / `cfg.Stderr`. Never use `os.Stdout` / `os.Stderr` directly.
- Return `error`. Do not call `os.Exit` inside a command; use `root.ExitError` instead.
- Error strings are lowercase, no trailing punctuation: `<command>: <reason>`.

### Exec stub generation

`climax add` inspects the root `Config` struct before generating the exec stub:

- If `Stdout` and/or `Stderr` fields are present → stub uses `fmt.Fprintln(cfg.Stdout, ...)` and imports `"fmt"`
- If a logger field is detected (field name contains `"log"`) → stub uses `cfg.<LoggerField>.Info(...)`
- Otherwise → stub returns `nil` with no imports

### Nested Commands

A command nested under another non-root command embeds its parent's `Config`
instead of `*root.Config`, giving it access to both shared I/O and any flags
the parent defines:

```go
// cmd/create/create.go — nested under "config"
package create

import (
    "context"

    "github.com/peterbourgon/ff/v4"
    "<org>/<repo>/cmd/config"
)

type Config struct {
    *config.Config // embeds parent; root.Config accessible transitively
    Flags   *ff.FlagSet
    Command *ff.Command
}

func New(parent *config.Config) *Config {
    var cfg Config
    cfg.Config = parent
    cfg.Flags = ff.NewFlagSet("create").SetParent(parent.Flags)
    cfg.Command = &ff.Command{
        Name:  "create",
        Usage: "<cli-name> config create [FLAGS]",
        // ...
        Exec: cfg.exec,
    }
    parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
    return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error { return nil }
```

### Concrete Example — version command

The generated version command prints rich build info (VCS commit, build date,
Go version, platform) with an optional `--json` flag for machine-readable output.

`var Version = "dev"` is a **deliberate exception** to the no-global-variables
rule: the Go linker's `-ldflags "-X <pkg>.Version=<val>"` mechanism can only
override a package-level `var`, not a constant or a local variable. This is the
only package-level mutable variable permitted in a climax command.

Do not use `init()` to populate it. `init()` runs unconditionally at program
startup before any flag parsing, its side effects cannot be suppressed in tests,
and the version string is only needed when the `version` subcommand actually
runs. Read build info inside `exec` instead.

The canonical implementation lives in `pkg/scaffold/templates/version.go.tmpl`. Key structural
elements that the scaffold enforces (and `climax update` monitors):

**`type Option func(i *Info)`** — functional-options type for post-gather customization.

**`type Info struct`** — exported struct with JSON tags. Fields: `GitVersion`, `ModuleSum`,
`GitCommit`, `GitTreeState`, `BuildDate`, `BuiltBy`, `GoVersion`, `Compiler`, `Platform`
(all JSON-serialized), plus `ASCIIName`, `Name`, `Description`, `URL` (JSON `-`, display only).

**`type Config struct`** — embeds `*root.Config`, adds `JSON bool`, `Flags`, `Command`.

**`With*` constructors** — `WithAppDetails(name, description, url string) Option`,
`WithASCIIName(name string) Option`, `WithBuiltBy(name string) Option`.

**`GetVersionInfoFrom(bi *debug.BuildInfo, _ string, options ...Option) *Info`** — builds an
`Info` from an explicit `BuildInfo`, applying options. Accepts `nil` (all VCS fields become
`"unknown"`). Use this in tests to avoid touching global state.

**Pointer-receiver output methods on `*Info`**: `String() string` (tabwriter-aligned key:value
output) and `JSONString() (string, error)` (indented JSON).

```go
// cmd/version/version.go  (abbreviated — see version.go.tmpl for full source)
package version

var Version = "dev"

type Option func(i *Info)

type Info struct {
    GitVersion   string `json:"gitVersion"`
    ModuleSum    string `json:"moduleChecksum"`
    GitCommit    string `json:"gitCommit"`
    GitTreeState string `json:"gitTreeState"`
    BuildDate    string `json:"buildDate"`
    BuiltBy      string `json:"builtBy"`
    GoVersion    string `json:"goVersion"`
    Compiler     string `json:"compiler"`
    Platform     string `json:"platform"`

    ASCIIName   string `json:"-"`
    Name        string `json:"-"`
    Description string `json:"-"`
    URL         string `json:"-"`
}

type Config struct {
    *root.Config
    JSON    bool
    Flags   *ff.FlagSet
    Command *ff.Command
}

func WithAppDetails(name, description, url string) Option { … }
func WithASCIIName(name string) Option                    { … }
func WithBuiltBy(name string) Option                      { … }

func New(parent *root.Config) *Config { … }

func GetVersionInfoFrom(bi *debug.BuildInfo, _ string, options ...Option) *Info { … }

func (i *Info) String() string           { … } // tabwriter key:value
func (i *Info) JSONString() (string, error) { … } // json.MarshalIndent

func gatherVersionInfo(bi *debug.BuildInfo) Info { … }
func (cfg *Config) exec(_ context.Context, _ []string) error { … }
```

______________________________________________________________________

## Registering Commands

`cmd/cmd.go` is the **only place `New()` is called**. It constructs the root
config, calls each subcommand's `New()` in order (which controls help output
order), and exposes `Run` for `main`.

```go
// cmd/cmd.go
package cmd

// climax:name <cli-name>
// climax:root-pkg root

import (
    "context"
    "errors"
    "fmt"
    "io"

    "github.com/peterbourgon/ff/v4"
    "github.com/peterbourgon/ff/v4/ffhelp"
    "<org>/<repo>/cmd/root"
    "<org>/<repo>/cmd/version"
    // climax:imports
)

// Run parses args and dispatches to the matching command.
// args must not include the executable name (pass os.Args[1:]).
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
    r := root.New(stdin, stdout, stderr)
    version.New(r)
    // register new commands here

    if err := r.Command.Parse(args); err != nil {
        _, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
        return fmt.Errorf("parse: %w", err)
    }

    if err := r.Command.Run(ctx); err != nil {
        // Suppress help output for ErrNoExec and ExitError — both are intentional.
        var exitErr root.ExitError
        if !errors.Is(err, ff.ErrNoExec) && !errors.As(err, &exitErr) {
            _, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
        }
        return err
    }

    return nil
}
```

### Dispatch Rules

- `Run` receives `os.Args[1:]` — executable name already removed by `main`.
- Subcommand selection is **case-insensitive** match on `Name`. No prefix matching.
- `-h` / `--help` at any level causes `Parse` to return `ff.ErrHelp`; treated as success.
- A command with no `Exec` returns `ff.ErrNoExec`; treated as success.
- Unknown subcommand returns an error; `main` controls the exit code.

______________________________________________________________________

## Entry Point

`main.go` is intentionally thin. It sets up signal-safe shutdown via
`signal.NotifyContext` and delegates to a separate `run()` function
(which improves testability — test harnesses can call `run` directly).

`run()` returns an `int` exit code. `main()` calls `stop()` first, then
`os.Exit`. If `run()` called `os.Exit` directly, `stop()` would never execute,
leaving the signal-handler goroutine running until the process terminated anyway
— a subtle goroutine leak and a violation of the no-`os.Exit`-outside-main rule.

`ff.ErrHelp` and `ff.ErrNoExec` are not failures. `root.ExitError` bypasses
the `"error: ..."` printer.

```go
// main.go
package main

import (
    "context"
    "errors"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/peterbourgon/ff/v4"
    "<org>/<repo>/cmd"
    "<org>/<repo>/cmd/root"
)

const (
    exitFail    = 1
    exitSuccess = 0
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(),
        os.Interrupt,    // SIGINT = Ctrl+C
        syscall.SIGQUIT, // Ctrl-\
        syscall.SIGTERM, // polite termination request
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
```

______________________________________________________________________

## Post-Parse Initialization

Because `ff` separates `Parse()` from `Run()`, dependencies that require parsed
flag values (API clients, DB connections, loggers) can be initialized in `cmd.go`
between the two calls and assigned to fields on `root.Config` that all subcommand
configs inherit.

```go
// in cmd.go Run(), between Parse and Run:
if err := r.Command.Parse(args); err != nil { ... }

client, err := api.NewClient(r.Token) // r.Token was set during Parse
if err != nil {
    return fmt.Errorf("construct client: %w", err)
}
r.Client = client // now available to all exec functions via embedded root.Config

if err := r.Command.Run(ctx); err != nil { ... }
```

______________________________________________________________________

## Code Generation

The `climax` tool scaffolds and extends applications that follow this spec.

### `climax init [FLAGS] [path]`

Creates a new application skeleton at `path` (default: `.`). The path must be
inside an existing Go module. Generated files, in order:

1. `main.go`
2. `cmd/cmd.go`
3. `cmd/<root-pkg>/<root-pkg>.go`
4. `cmd/version/version.go` (unless `--no-version`)

| Flag           | Default                        | Description                                                   |
| -------------- | ------------------------------ | ------------------------------------------------------------- |
| `--name`       | last import path segment       | `ff.Command.Name` for the root command (allows hyphens)       |
| `--short`      | `"TODO: describe <name> here"` | `ff.Command.ShortHelp` for the root command                   |
| `--long`       | _(omitted)_                    | `ff.Command.LongHelp` for the root command                    |
| `--root-pkg`   | `root`                         | Go package name and file basename for the root config package |
| `--no-version` | false                          | Skip generating `cmd/version/version.go`                      |

Output:

```
initialized climax app at /path/to/app (import: github.com/yourname/app)
  created main.go
  created cmd/cmd.go
  created cmd/root/root.go
  created cmd/version/version.go
```

### `climax add [FLAGS] <name> [path]`

Creates `cmd/<name>/<name>.go` and registers it in the dispatcher. `<name>`
must be a valid Go identifier (used as the package name). `[path]` must be the
root of an existing climax application.

| Flag             | Default                      | Description                                              |
| ---------------- | ---------------------------- | -------------------------------------------------------- |
| `--name`         | same as `<name>`             | `ff.Command.Name` for the new command (allows hyphens)   |
| `--short`        | `"<name> command"`           | `ff.Command.ShortHelp` for the new command               |
| `--long`         | `"<Name> is a new command."` | `ff.Command.LongHelp` for the new command                |
| `-p`, `--parent` | root package                 | Go package name of the parent command (for nesting)      |

The CLI name used in `Usage` strings is resolved in this order:
1. `// climax:name` marker in the dispatcher
2. `Name` field of the root `ff.Command`, extracted via AST from `cmd/<root-pkg>/<root-pkg>.go`
3. Last segment of the import path

Registration uses AST analysis to locate the correct insertion point, so it
works even when marker comments have been removed or the file is named `command.go`.

Output:

```
added command "serve"
  created  cmd/serve/serve.go
  modified cmd/cmd.go
```

### Persistence markers

`climax init` writes two marker comments to the dispatcher that carry values
needed by subsequent `climax add` runs:

```go
// climax:name <cli-name>   // ff.Command.Name used for the root command
// climax:root-pkg <pkg>    // root config package name (default: root)
```

When both markers are present, `climax add` uses text insertion at the marker
positions. When one or both are absent, it falls back to full AST analysis of
the dispatcher to determine insertion points — which requires that the file still
has a parenthesized import block, a top-level `func Run`, a root assignment
(`r := root.New(...)`), and a `r.Command.Parse(...)` call.

### `climax lint [path]`

Checks a climax application for structural drift from the current scaffold
templates. Issues are grouped by file; at most three can be reported — one per
structural group:

| Group | File | Properties checked |
|---|---|---|
| 1 | `main.go` | `signal.NotifyContext`, separate `run()` function, `os.Stdin` passed to `cmd.Run` |
| 2 | `cmd/cmd.go` | `stdin io.Reader` parameter in `Run`, `stdin` forwarded to `root.New` |
| 3 | `cmd/<root>/<root>.go` | `Stdin io.Reader` field in `Config`, `stdin io.Reader` parameter in `New`, `cfg.Stdin = stdin` assignment |

All three properties in a group must be present to suppress that group's issue.
Each issue is shown as a focused unified diff. Exits with status 1 when any issues
are found.

Success output:

```
✓  No structural drift found.
```

Failure output:

```
⚠  2 structural issue(s) found in /path/to/app

── main.go: signal-safe shutdown, run() separation, os.Stdin

   --- a/main.go
   +++ b/main.go	(expected per climax template)
   @@ structural pattern @@
   ...
```

### `climax update [--apply] [path]`

A **climax development tool** — detects drift between climax's own source files
and the scaffold template files in `pkg/scaffold/templates/`. Only runs against
the climax module itself (`github.com/StevenACoffman/climax`). Run after changing
a structural pattern in `main.go`, `cmd/cmd.go`, or `cmd/root/root.go`.

Fifteen structural properties are checked independently (vs. three groups in `lint`):

| File | Properties |
|---|---|
| `main.go` | `signal.NotifyContext`, `run()` separation, `os.Stdin` passed to `cmd.Run` |
| `cmd/cmd.go` | `stdin io.Reader` parameter in `Run`, `stdin` forwarded to `root.New` |
| `cmd/root/root.go` | `Stdin io.Reader` field, `stdin io.Reader` parameter in `New`, `cfg.Stdin = stdin` assignment |
| `cmd/version/version.go` | `JSON` flag in `Config`, tabwriter output, `GetVersionInfoFrom` function, `Info` methods use pointer receivers, `Option` type, `With*` constructors |
| `cmd/mango/mango.go` | `Section int` field in `Config` |

Each drift item falls into one of three categories:

- **`✗` auto-fixable** — source has the property, template lacks it, and a string-replacement patch is defined. Fixed by `--apply`.
- **`✗` manual-needed** — source has the property, template lacks it, but no patch exists (e.g. `tabwriter` output format or `GetVersionInfoFrom` require a broader template rewrite). Reported but not auto-patched.
- **`⚠` manual review** — template has the property, source does not. Removing things from templates is a deliberate decision; never auto-patched.

Without `--apply`, prints a drift report and exits non-zero:

```
Drift detected: 3 item(s) (2 auto-fixable with --apply)

  ✗  main    signal.NotifyContext
  ✗  cmd     stdin io.Reader parameter in Run
  ✗  version tabwriter output

  Requires manual review (template has property, source does not):
  ⚠   root    Stdin io.Reader field

Run with --apply to patch the template files automatically.
```

The `(N auto-fixable with --apply)` label is omitted when `N` is zero. With `--apply`,
only the auto-fixable items are patched in-place in the template files; the others remain
in the report for manual action.

### `climax version [--json]`

Prints build and version information for the `climax` binary itself, read from
the embedded module build info:

```
GitVersion:    v0.3.1
GitCommit:     a1b2c3d4e5f6...
GitTreeState:  clean
BuildDate:     2025-11-01T12:00:00
BuiltBy:       goreleaser
GoVersion:     go1.24.0
Compiler:      gc
ModuleSum:     h1:...
Platform:      darwin/arm64
```

Use `--json` for machine-readable output.

______________________________________________________________________

## Constraints

| Rule | Rationale |
|---|---|
| Every configurable knob is a registered flag | Hard-coded values and out-of-band `os.Getenv` calls silently break `-h` discoverability and make the configuration surface invisible to operators |
| Use `ff`; no other CLI frameworks | `ff` provides flags, subcommand dispatch, and help with minimal surface area |
| No `Commander` interface | Go composition via `Exec` function pointer is sufficient |
| No `init()` functions | `init()` runs unconditionally at startup before flag parsing, its side effects cannot be suppressed in tests, and execution order across packages is implicit; use `New()` in `cmd/cmd.go` for registration and initialize resources inside `exec` when they are needed |
| No package-level mutable variables, **except** `var Version = "dev"` in `cmd/version/` | `go build -ldflags "-X <pkg>.Version=<val>"` can only override a `var` — `const` and local variables are link-time immutable; this is the one sanctioned global in a climax command |
| Config struct per command | Carries parsed flag values and inherited I/O; avoids global state |
| Flag values bound in `New()`, not in `exec` | Flags are parsed before `exec` is called; binding in `exec` is too late |
| `SetParent` on every subcommand flag set | Allows parent flags to be accepted at any subcommand depth |
| Never use `os.Stdout`/`os.Stderr` directly | Write to `cfg.Stdout`/`cfg.Stderr` for testability |
| Return `root.ExitError`, not `os.Exit` | Commands don't control the process; only `main()` calls `os.Exit` — after `stop()` releases the signal context |
| `run()` returns `int`; `main()` calls `stop()` then `os.Exit` | `os.Exit` inside `run()` would bypass `stop()`, leaving the signal-handler goroutine running until the process terminates |
| Errors bubble to `main` | `run()` is the single place that maps errors to exit codes |
| `ff.ErrHelp` and `ff.ErrNoExec` are success | Handle both in `run()`'s switch; do not propagate as failures |

______________________________________________________________________

## Checklist: Adding a New Command

- [ ] Create `cmd/<name>/` package
- [ ] Define `Config` struct embedding `*root.Config` (or `*<parent>.Config` for nesting), with command-local flag value fields
- [ ] Write `New(parent *root.Config) *Config` that:
  - creates `ff.NewFlagSet("<name>").SetParent(parent.Flags)`
  - binds flag values to `Config` fields
  - exposes every configurable behaviour as a flag (no `os.Getenv`, no hard-coded values)
  - constructs `ff.Command` with `Name`, `Usage`, `ShortHelp`, `Flags`, and `Exec`
  - appends to `parent.Command.Subcommands`
- [ ] Write `func (cfg *Config) exec(ctx context.Context, args []string) error`
- [ ] Call `<name>.New(r)` in the registration block in `cmd/cmd.go`
- [ ] Add the import for the new package in `cmd/cmd.go`
