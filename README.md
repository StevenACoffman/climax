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

| Flag           | Default                        | Description                                     |
| -------------- | ------------------------------ | ----------------------------------------------- |
| `--name`       | last import path segment       | CLI name used in usage strings (allows hyphens) |
| `--short`      | `"TODO: describe <name> here"` | `ShortHelp` for the root command                |
| `--long`       | *(omitted)*                    | `LongHelp` for the root command                 |
| `--root-pkg`   | `root`                         | Go package name for the root config package     |
| `--no-version` | false                          | Skip generating `cmd/version/version.go`        |

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

**Nesting commands:**

Use the Go package name (not a variable name) as the `--parent` value:

```sh
climax add config
climax add create --parent config # creates cmd/create/create.go under config
```

### `climax version [--json]`

Prints build and version information for the climax binary, read from the module's embedded build info:

```
GitVersion:    v0.3.1
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
  "gitVersion": "v0.3.1",
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

### `climax lint [path]`

Checks a climax-based application for structural drift from the current scaffold templates. The path defaults to the current directory, which must be the root of an application created by `climax init`.

Each issue is shown as a focused unified diff:

```
── cmd/cmd.go: stdin io.Reader parameter in Run, stdin forwarded to root.New

   --- a/cmd/cmd.go
   +++ b/cmd/cmd.go	(expected per climax template)
   @@ structural pattern @@
   -func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
   -	r := root.New(stdout, stderr)
   +func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
   +	r := root.New(stdin, stdout, stderr)
```

Expected patterns are derived directly from the embedded template files in `pkg/scaffold/templates/`, so the lint checks automatically stay in sync whenever templates are updated.

Exits with status 1 when any issues are found, making it suitable for CI checks on generated apps.

### `climax update [--apply] [path]`

Detects structural drift between climax's own source files and the scaffold template files in `pkg/scaffold/templates/`. This is a **climax development tool** — run it after changing a structural pattern in `main.go`, `cmd/cmd.go`, or `cmd/root/root.go` to check whether the templates need updating.

```
Drift detected: 3 item(s) (3 fixable with --apply)

  ✗  main   signal.NotifyContext
  ✗  main   run() separation
  ✗  cmd    stdin io.Reader parameter in Run
```

Without `--apply` it reports drift and exits non-zero (useful in CI). With `--apply` it patches the template files in place for each fixable item. Items where the template has a property the source doesn't are flagged for manual review.

## Generated application structure

After `climax init` followed by `climax add serve`, `climax add config`, and `climax add create --parent config`:

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
```

Run any command:

```sh
go run . serve
go run . config
go run . config create
go run . help config create
```

## Generated patterns

### Flags-first configuration

Every knob that affects behaviour is a registered flag on an `ff.FlagSet`. This means running any command (or subcommand) with `-h` reveals its complete configuration surface area — nothing is hidden behind hard-coded values or environment variables that aren't also flags.

`ff` knows how to parse each flag from three sources, highest precedence first:

1. CLI args (`--port 8080`)
2. Environment variables — `ff` uppercases the prefix and replaces hyphens and dots in flag names with underscores, so `--log-level` with prefix `MYAPP` becomes `MYAPP_LOG_LEVEL`
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

See the [peterbourgon/ff](https://github.com/peterbourgon/ff) documentation for details on config file formats, env var prefix configuration, and full precedence rules.

### Signal-safe shutdown

`main.go` uses `signal.NotifyContext` so `Ctrl-C` cancels the context cleanly:

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx)
}
```

`run` is intentionally separated from `main` to allow test harnesses to call it directly.

### Shared I/O

`stdin`, `stdout`, and `stderr` are passed explicitly from `main` through the dispatcher down to every command's `Config`. Nothing uses `os.Stdout` directly after `main.go`. This makes commands testable without capturing global state:

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

### Exit codes without error messages

`root.ExitError` lets a command exit with a specific non-zero code without printing an `error: ...` line. The dispatcher skips help-text printing for `ExitError`, and `run()` in `main.go` calls `os.Exit` directly:

```go
// In any command's exec function:
if noIssues {
	return nil // exit 0
}
return root.ExitError(1) // exit 1, no "error:" printed
```

This is used by `climax lint` and `climax update` to signal failure to CI without redundant error output.

## Version embedding

The generated `cmd/version/version.go` reads the module version automatically from the Go toolchain's embedded build info. When a binary is installed via `go install` or built from a tagged release, the version is set without any extra build flags:

```sh
go install github.com/yourname/myapp@v1.2.3
myapp version
# v1.2.3
```

For local or untagged builds, the version defaults to `"dev"`. Override it at link time if needed:

```sh
go build -ldflags "-X 'github.com/yourname/myapp/cmd/version.Version=v1.2.3'" -o myapp .
```
