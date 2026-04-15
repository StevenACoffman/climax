# climax

A scaffold generator for Go CLI applications built with [`peterbourgon/ff/v4`](https://github.com/peterbourgon/ff).

Climax generates the boilerplate for a structured, idiomatic CLI: one package per command, flags-first configuration (every setting is a registered `ff` flag, discoverable via `-h`), a shared root `Config` that threads `stdin`/`stdout`/`stderr` through the whole tree, signal-safe shutdown, and a dispatcher that routes arguments to the matching command. New commands can be added at any time — climax uses AST analysis to register them correctly even if you've edited the generated files or renamed the dispatcher.

## Install

```sh
go install github.com/StevenACoffman/climax@latest
```

Or, if you have [uv](https://docs.astral.sh/uv/) installed, run climax without a separate Go toolchain:

```sh
uvx --from climaxgo climax
```

To install it persistently as a uv tool:

```sh
uv tool install climaxgo
climax init
```

## Quick start

```sh
# Create a new module
mkdir myapp && cd myapp
go mod init github.com/yourname/myapp

# Scaffold the application
climax init

# Run it immediately
go run . --help

# Add commands
climax add serve
climax add config
climax add create --parent config # nested under config

# Add a man page command
climax mango
go get github.com/StevenACoffman/mango-ff github.com/muesli/roff

# Build
go build -o myapp .
```

## Commands

### `climax init [FLAGS] [path]`

Generates a complete CLI application skeleton at `path` (default: current directory). The path must be inside an existing Go module.

**Generated files:**

```
myapp/
  main.go               # entry point with signal-safe shutdown
  cmd/
    cmd.go              # dispatcher: routes args to commands
    root/
      root.go           # shared Config (Stdin, Stdout, Stderr, ff.Command)
    version/
      version.go        # version command (skip with --no-version)
```

**Example output:**

```
initialized climax app at /home/user/myapp (import: github.com/yourname/myapp)
  created main.go
  created cmd/cmd.go
  created cmd/root/root.go
  created cmd/version/version.go
```

**Flags:**

| Flag              | Default                                        | Description                                             |
| ----------------- | ---------------------------------------------- | ------------------------------------------------------- |
| `--name`          | last import path segment                       | CLI name used in usage strings (allows hyphens)         |
| `--short`         | `"TODO: describe <name> here"`                 | `ShortHelp` for the root command                        |
| `--long`          | *(omitted)*                                    | `LongHelp` for the root command                         |
| `--root-pkg`      | `root`                                         | Go package name for the root config package             |
| `--env-prefix`    | name uppercased, `-` and `.` replaced with `_` | Env var prefix for the generated app's flags            |
| `--no-env-prefix` | false                                          | Use `ff.WithEnvVars()` (no prefix) instead of a prefix |
| `--no-version`    | false                                          | Skip generating `cmd/version/version.go`                |

`--env-prefix` and `--no-env-prefix` are mutually exclusive. When neither is set, the generated app defaults to the app name uppercased (e.g. a flag `--log-level` in an app named `myapp` is readable as `MYAPP_LOG_LEVEL`).

### `climax add [FLAGS] <name> [path]`

Adds a new command package at `cmd/<name>/<name>.go` and registers it in the dispatcher. The path must be the root of an application created by `climax init`.

Registration uses AST analysis to locate the correct insertion point, so it works reliably even if you've removed the generated marker comments, restructured the dispatcher, or renamed it from `cmd.go` to `command.go`.

**Example output:**

```
added command "serve"
  created  cmd/serve/serve.go
  modified cmd/cmd.go
```

**Flags:**

| Flag             | Default                      | Description                                              |
| ---------------- | ---------------------------- | -------------------------------------------------------- |
| `--name`         | same as `<name>`             | `ff.Command.Name` in the generated file (allows hyphens) |
| `--short`        | `"<name> command"`           | `ShortHelp` for the generated command                    |
| `--long`         | `"<Name> is a new command."` | `LongHelp` for the generated command                     |
| `-p`, `--parent` | root package                 | Go package name of the parent command                    |

**What gets generated:**

`climax add serve` produces this file, ready to edit:

```go
// Package serve implements the "serve" CLI command.
package serve

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"

	"github.com/yourname/myapp/cmd/root"
)

// Config holds the configuration for the serve command.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the serve command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("serve").SetParent(parent.Flags)
	// bind flags: cfg.Flags.StringVar(&cfg.SomeFlag, 0, "some-flag", "", "description")
	cfg.Command = &ff.Command{
		Name:      "serve",
		Usage:     "myapp serve [FLAGS]",
		ShortHelp: "serve command",
		LongHelp:  "Serve is a new command.",
		Flags:     cfg.Flags,
		Exec:      cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	// TODO: implement serve.
	// Rename the second parameter from _ to args to access positional arguments.
	_, _ = fmt.Fprintln(cfg.Stdout, "serve: not yet implemented")
	return nil
}
```

The exec stub is adapted to the root `Config` shape: if `Stdout`/`Stderr` fields are present, the stub uses `fmt.Fprintln(cfg.Stdout, ...)` and imports `"fmt"`. If a logger field is detected, it uses `cfg.<Logger>.Info(...)`. Otherwise it returns `nil` with no imports.

**Nesting commands:**

Use the Go package name (not a variable name) as the `--parent` value:

```sh
climax add config
climax add create --parent config # creates cmd/create/create.go under config
```

### `climax mango [FLAGS] [path]`

Adds a `man` subcommand to the climax application at `path` (default: current directory). The generated command prints the application's man page in roff format to stdout using [`github.com/StevenACoffman/mango-ff`](https://github.com/StevenACoffman/mango-ff), which derives flag entries, subcommand sections, and help text directly from the `ff.Command` tree — no additional configuration required.

After running `climax mango`, add the required dependencies to the target application:

```sh
go get github.com/StevenACoffman/mango-ff github.com/muesli/roff
```

Then build and use the man page:

```sh
myapp man            # print roff to stdout
myapp man | man -l - # view in the man pager
```

**Example output:**

```
added man page command
  created  cmd/man/man.go
  modified cmd/cmd.go

Add dependencies:
  go get github.com/StevenACoffman/mango-ff github.com/muesli/roff

View the man page:
  go run . man | man -l -
```

**Flags:**

| Flag          | Default  | Description                                                             |
| ------------- | -------- | ----------------------------------------------------------------------- |
| `--section`   | `1`      | Man page section number (1–8)                                           |
| `--authors`   | *(none)* | Bake a `WithSection("Authors", ...)` call into the generated command    |
| `--copyright` | *(none)* | Bake a `WithSection("Copyright", ...)` call into the generated command  |

### `climax lint [path]`

Checks a climax-based application for structural drift from the current scaffold templates. The path defaults to the current directory, which must be the root of an application created by `climax init`.

Each issue is shown as a focused unified diff:

```
⚠  1 structural issue(s) found in /home/user/myapp

── cmd/cmd.go: stdin io.Reader parameter in Run, stdin forwarded to root.New

   --- a/cmd/cmd.go
   +++ b/cmd/cmd.go	(expected per climax template)
   @@ structural pattern @@
   -func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
   -	r := root.New(stdout, stderr)
   +func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
   +	r := root.New(stdin, stdout, stderr)
```

When no issues are found:

```
✓  No structural drift found.
```

**Structural groups checked:**

| File | Properties |
| ---- | ---------- |
| `main.go` | `signal.NotifyContext` for graceful shutdown; separate `run(ctx)` function for testability; `os.Stdin` passed explicitly to `cmd.Run` |
| `cmd/cmd.go` | `stdin io.Reader` parameter in `Run`; `stdin` forwarded to `root.New` |
| `cmd/<root>/<root>.go` | `Stdin io.Reader` field in `Config`; `stdin io.Reader` parameter in `New`; `cfg.Stdin = stdin` assignment |

Expected patterns are derived directly from the embedded template files in `pkg/scaffold/templates/`, so the lint checks automatically stay in sync whenever templates are updated.

Exits with status 1 when any issues are found, making it suitable for CI checks on generated apps.

### `climax version [--json]`

Prints build and version information for the climax binary, read from the module's embedded build info:

```
GitVersion:    v0.4.0
GitCommit:     a1b2c3d4e5f6...
GitTreeState:  clean
BuildDate:     2025-11-01T12:00:00
BuiltBy:       unknown
GoVersion:     go1.24.0
Compiler:      gc
ModuleSum:     h1:...
Platform:      darwin/arm64
```

Use `--json` for machine-readable output:

```sh
climax version --json
```

```json
{
  "gitVersion": "v0.4.0",
  "gitCommit": "a1b2c3d4e5f6...",
  "gitTreeState": "clean",
  "buildDate": "2025-11-01T12:00:00",
  "builtBy": "unknown",
  "goVersion": "go1.24.0",
  "compiler": "gc",
  "moduleChecksum": "h1:...",
  "platform": "darwin/arm64"
}
```

### `climax update [--apply] [path]`

> **Climax development tool only.** This command detects drift in climax's own source and templates. It refuses to run against any other module. Do not use it on applications generated by climax.

Detects structural drift between climax's own source files and the scaffold template files in `pkg/scaffold/templates/`. Run it after changing a structural pattern in `main.go`, `cmd/cmd.go`, `cmd/root/root.go`, `cmd/version/version.go`, or `cmd/mango/mango.go` to check whether the templates need updating.

**File mapping (source → template):**

| Source | Template |
| ------ | -------- |
| `main.go` | `main.go.tmpl` |
| `cmd/cmd.go` | `cmd.go.tmpl` |
| `cmd/root/root.go` | `root.go.tmpl` |
| `cmd/version/version.go` | `version.go.tmpl` |
| `cmd/mango/mango.go` | `man.go.tmpl` |

```
Drift detected: 3 item(s) (3 auto-fixable with --apply)

  ✗  main    signal.NotifyContext
  ✗  main    run() separation
  ✗  cmd     stdin io.Reader parameter in Run
```

Without `--apply` it reports drift and exits non-zero (useful in CI). With `--apply` it patches the template files in place for each auto-fixable item. Items where the template has a property the source does not are flagged for manual review and not auto-patched.

## Generated application structure

After `climax init` followed by `climax add serve`, `climax add config`, `climax add create --parent config`, and `climax mango`:

```
myapp/
  main.go
  cmd/
    cmd.go
    root/
      root.go
    version/
      version.go
    serve/
      serve.go
    config/
      config.go
    create/
      create.go
    man/
      man.go
```

Run any command:

```sh
go run . serve
go run . config
go run . config create
go run . help config create
go run . man | man -l -
```

## Generated patterns

### Flags-first configuration

Every knob that affects behaviour is a registered flag on an `ff.FlagSet`. This means running any command (or subcommand) with `-h` reveals its complete configuration surface area — nothing is hidden behind hard-coded values or environment variables that aren't also flags.

`ff` parses each flag from three sources, highest precedence first:

1. CLI args (`--port 8080`)
2. Environment variables — `ff` uppercases the prefix and replaces hyphens and dots with underscores, so `--log-level` with prefix `MYAPP` becomes `MYAPP_LOG_LEVEL`
3. Config files (TOML, JSON, INI, or `.env` format)

Because the flag is the single source of truth for each setting, you get all three sources for free without extra code:

```go
// cmd/serve/serve.go — in New()
cfg.Flags.IntVar(&cfg.Port, 0, "port", 8080, "port to listen on")
```

```sh
# All three lines set the same flag:
myapp serve --port 9090
MYAPP_PORT=9090 myapp serve
# serve.toml: port = 9090
```

The generated `cmd/cmd.go` includes a doc comment listing the env var prefix rule so it's always discoverable:

```go
// Every flag can be set via a MYAPP_-prefixed environment variable.
// The mapping rule: prepend MYAPP_, uppercase, replace dashes with underscores.
```

See the [peterbourgon/ff](https://github.com/peterbourgon/ff) documentation for details on config file formats and full precedence rules.

### Signal-safe shutdown

`main.go` uses `signal.NotifyContext` so `Ctrl-C` and `SIGTERM` cancel the context cleanly:

```go
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
```

`run` is intentionally separated from `main` so test harnesses can call it directly with a controlled context.

### Dispatcher error handling

The generated dispatcher in `cmd/cmd.go` distinguishes three error paths:

| Returned from exec | What happens |
| ------------------ | ------------ |
| `nil`, `ff.ErrHelp`, `ff.ErrNoExec` | Exit 0. `ff.ErrNoExec` fires when a parent command is invoked without a subcommand — help is shown but the process exits cleanly. |
| `root.ExitError(N)` | Exit N. No `"error: ..."` line is printed. Use this when the command has already reported the outcome (e.g. lint found issues). |
| Any other `error` | The selected command's help is printed to stderr, then `"error: <message>"`, then exit 1. |

Parse errors (bad flags) follow the same path as other errors: help is shown before the error message.

### Shared I/O

`stdin`, `stdout`, and `stderr` are passed explicitly from `main` through the dispatcher down to every command's `Config`. Nothing writes to `os.Stdout` directly after `main.go`. This makes commands testable without capturing global state:

```go
// cmd/cmd.go
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
    r := root.New(stdin, stdout, stderr)
    ...
}

// cmd/root/root.go
type Config struct {
    Stdin   io.Reader
    Stdout  io.Writer
    Stderr  io.Writer
    Flags   *ff.FlagSet
    Command *ff.Command
}
```

### Testing

Because all I/O goes through injected writers, commands are testable by calling `cmd.Run` with `bytes.Buffer` values:

```go
func TestServeCommand(t *testing.T) {
    var stdout, stderr bytes.Buffer
    err := cmd.Run(
        context.Background(),
        []string{"serve", "--port", "9090"},
        strings.NewReader(""),
        &stdout,
        &stderr,
    )
    if err != nil {
        t.Fatalf("serve: %v\nstderr: %s", err, &stderr)
    }
    // assert stdout.String() contains expected output
}
```

No global state is captured or restored. The test binary itself is not invoked.

### One package per command

Each command lives in its own package and embeds `*root.Config` to inherit the shared I/O:

```go
// cmd/serve/serve.go
type Config struct {
    *root.Config
    Port    int
    Flags   *ff.FlagSet
    Command *ff.Command
}

func New(parent *root.Config) *Config { ... }
func (cfg *Config) exec(ctx context.Context, args []string) error { ... }
```

`New` and `Config` are the only exported identifiers in the package. Flag values are bound to `Config` fields inside `New`, not inside `exec` — by the time `exec` runs, flags are already parsed.

### Nested commands

A child command embeds its parent's `Config` instead of `*root.Config`, giving it access to both the shared I/O and any flags the parent defines:

```go
// cmd/create/create.go
type Config struct {
	*config.Config // embeds the config command's Config
	Flags          *ff.FlagSet
	Command        *ff.Command
}
```

The root `Config` is accessible transitively via the embedded chain.

### Exit codes without error messages

`root.ExitError` lets a command exit with a specific non-zero code without printing an `error: ...` line:

```go
// In any command's exec function:
if noIssues {
    return nil           // exit 0
}
return root.ExitError(1) // exit 1, no "error:" printed
```

Use this when the command has already communicated its outcome through its own output — for example, `climax lint` prints the diff before returning `ExitError(1)`, so a redundant error line would be noise.

## Version embedding

The generated `cmd/version/version.go` reads the module version automatically from the Go toolchain's embedded build info. When a binary is installed via `go install` or built from a tagged release, the version is set without any extra build flags:

```sh
go install github.com/yourname/myapp@v1.2.3
myapp version        # prints v1.2.3
myapp version --json # machine-readable output
```

For local or untagged builds, the version defaults to `"dev"`. Override it at link time if needed:

```sh
go build -ldflags "-X 'github.com/yourname/myapp/cmd/version.Version=v1.2.3'" -o myapp .
```

`var Version = "dev"` is a deliberate exception to the no-globals rule: the Go linker's `-ldflags "-X <pkg>.Version=<val>"` mechanism requires a package-level `var`, not a constant or local variable.
