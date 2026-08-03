# ff vs kong: Gap Analysis

This document compares [`peterbourgon/ff`](https://github.com/peterbourgon/ff) (v4)
and [`alecthomas/kong`](https://github.com/alecthomas/kong) as Go CLI libraries,
with emphasis on what ff lacks relative to kong. Both libraries were read
directly from source for this analysis.

---

## Overview

| | ff | kong |
|---|---|---|
| Declaration style | Programmatic (`FlagSet` methods) or limited struct tags | Reflection-driven struct tags |
| Subcommand model | `Command` tree with `Subcommands []*Command` | Recursive struct with `cmd:""` fields |
| Config sources | CLI → env vars → config file (priority order) | CLI → resolvers (env, JSON, …) |
| Dispatch | `func(ctx context.Context, args []string) error` | `Run(...) error` method with dependency injection |
| Extensibility | `Flags` and `Flag` interfaces; `ConfigFileParseFunc` | `Resolver`, `Mapper`, `Plugins`, `DynamicCommand` |
| CGo | no | no |

ff is smaller, simpler, and stdlib-compatible. kong is a full CLI framework.
The gaps below are features kong provides that ff does not.

---

## 1. Struct Tag Coverage

ff's `AddStruct` recognises an `ff:` struct tag with six keys:

```
short/s/shortname   long/l/longname   usage/u
default/d/def       placeholder/p     noplaceholder   nodefault
```

kong's `kong:` tag has roughly 25 keys covering the entire CLI model:

```
cmd         arg         env         name        help
type        placeholder default     short       aliases
required    optional    hidden      negatable   format
sep         mapsep      enum        group       xor
and         prefix      envprefix   set         embed
passthrough -
```

Everything ff can express in a tag (names, usage, default, placeholder) is a
subset of what kong expresses. The absent keys in ff — `env`, `required`,
`enum`, `xor`, `and`, `hidden`, `negatable`, `aliases`, `cmd`, `arg`,
`embed`, `passthrough`, `prefix` — are the root cause of most gaps listed
below.

---

## 2. Positional Arguments

kong's `arg:""` tag defines typed, named positional arguments with full model
support: required vs optional, scalar vs variadic, help text, defaults, and
enum constraints. Positionals are distinct from flags in kong's grammar.

ff has no positional concept. Unconsumed strings after flag parsing stops are
returned by `GetArgs() []string`. All structure imposed on those bytes is the
caller's responsibility.

---

## 3. Named Mapper Types

kong maps specific strings passed to `type:""` to built-in or registered value
decoders:

| `type:` value | Behaviour |
|---|---|
| `counter` | `int` incremented once per flag occurrence; `-v -v -v` → `3` |
| `filecontent` | reads the named file and stores its content in a `string` field |
| `path` | platform-normalised filesystem path |
| `existingfile` | path validated to exist as a file at parse time |
| `existingdir` | path validated to exist as a directory at parse time |

Additional mappers can be registered with `kong.NamedMapper(name, mapper)`.

ff has no named-mapper concept. Custom types require implementing `flag.Value`
or supplying a `ParseFunc` to `ffval.Value[T]`. Existence validation cannot be
expressed declaratively.

---

## 4. Required Flags

kong's `required:""` tag makes a flag mandatory. kong errors at parse time if a
required flag is not set, with a clear message.

ff has no required-flag mechanism. Callers must check `flag.IsSet()` after
parsing and construct their own errors.

---

## 5. Constraint Groups: XOR and AND

kong's `xor:"groupname"` enforces mutual exclusivity across a named set of
flags: at most one may be set. An error is produced if more than one is
provided.

kong's `and:"groupname"` enforces co-occurrence: every flag in the group must
be set together, or none may be. An error is produced if only a subset is
provided.

ff has no inter-flag constraint system.

---

## 6. Negatable Boolean Flags

kong's `negatable:""` tag automatically registers a `--no-<flag>` counterpart
for any boolean flag. Setting `--no-verbose` sets the field to `false`
regardless of default.

ff has no equivalent. Two separate flags must be defined and reconciled
manually.

---

## 7. Flag and Command Aliases

kong's `aliases:"x,y"` tag registers additional names for a flag or command.
Both forms are accepted at parse time and shown in help.

ff has no alias support for flags or commands.

---

## 8. Lifecycle Hooks

kong invokes method hooks on any command struct that implements them, in parse
order:

```go
BeforeReset(args ...any) error    // before defaults are applied
BeforeResolve(args ...any) error  // before resolvers (env, config) run
BeforeApply(args ...any) error    // before CLI args are applied
AfterApply(args ...any) error     // after all values are set and validated
AfterRun(args ...any) error       // after Run() returns
```

Hook parameters are resolved by type from the binding registry, so hooks can
receive typed dependencies just as `Run` methods can.

ff has no hook system. The caller owns the entire sequence between `Parse` and
execution.

---

## 9. Validation

kong calls `Validate() error` (or `Validate(ctx *kong.Context) error`) on any
command struct that implements either form, automatically after all values are
set, before `Run` is called.

ff has no post-parse validation pass. `FlagSet.Func(short, long, fn, usage)`
can run a callback each time an individual flag is set during parsing, but
there is no whole-command validation hook.

---

## 10. Dependency Injection

kong's `ctx.Run(binds...)` resolves `Run(...) error` method parameters by type
from a registry of bound values. Each command struct's `Run` method declares
only the dependencies it needs; kong satisfies them automatically. The binding
API is:

```go
kong.Bind(values...)              // at parse time
ctx.Bind(values...)               // at run time
ctx.BindTo(impl, iface)           // bind an interface
ctx.BindSingletonProvider(fn)     // lazy construction
ctx.BindToProvider(fn)            // per-call construction
```

Automatic bindings include `*kong.Kong`, `*kong.Context`, `*kong.Path`, and
all parent command structs traversed during parse.

ff's `Exec func(ctx context.Context, args []string) error` is a plain function
with a fixed signature. Any dependency beyond `context.Context` must be closed
over at construction time.

---

## 11. Resolver Interface

kong's `Resolver` is an interface for supplying flag values from external
sources:

```go
type Resolver interface {
    Validate(app *Application) error
    Resolve(context *Context, parent *Path, flag *Flag) (any, error)
}
```

Multiple resolvers are stacked via `kong.Resolver(...)` and consulted in
order. The built-in `kong.JSON` resolver handles dotted-path lookup and
tries both snake_case and camelCase key variants. `ResolverFunc` is a
one-method convenience adapter.

ff's extension point for external configuration is the function type
`ConfigFileParseFunc func(r io.Reader, set func(name, value string) error) error`,
which covers new file formats well but cannot resolve individual flag values
from arbitrary external sources per-flag.

---

## 12. Multiple Env Var Names Per Flag

kong's `env:"VAR1,VAR2,VAR3"` lists fallback environment variable names for a
single flag. The first one found in the environment wins.

ff derives exactly one env var name per flag from its long name (uppercased,
separators replaced with underscores) plus a single optional prefix. There is
no per-flag override or fallback list.

---

## 13. Variable Interpolation

kong evaluates `${varname}` and `${varname=default}` in help strings, default
values, and enum lists at parse time. Variables are supplied at the call site:

```go
kong.Parse(&cli, kong.Vars{"version": version, "config": defaultConfig})
```

They can also be set per-field with the `set:"k=v"` tag.

ff has no interpolation. All strings are used verbatim.

---

## 14. Flag and Command Grouping

kong's `group:"name"` tag assigns flags and commands to named display groups.
`kong.Groups([]Group{...})` maps group names to header text and visual
separators. Help output organises flags and commands by group.

ff has no intra-FlagSet grouping. `ffhelp` renders one `FLAGS` section per
`FlagSet` level in a command tree (distinguishing parent flags from child
flags), but all flags within a set are listed flat.

---

## 15. Embedded Structs and Prefix Namespacing

kong's `embed:""` tag folds a struct's children into the parent command,
enabling shared flag groups across commands without repetition. The `prefix:""`
and `envprefix:""` tags namespace embedded fields automatically.

ff has no embedding or prefix concept. Shared flags must be reproduced in each
`FlagSet` or made available via `SetParent`, which only adds visibility, not a
structural hierarchy.

---

## 16. Passthrough Mode

kong's `passthrough:""` tag on a command stops all flag parsing at that point
and delivers all remaining args verbatim. This is intended for commands that
exec a subprocess (e.g. `mytool run -- subcmd --its-flag`). `passthrough:"partial"`
is a softer variant that parses known flags and passes the rest through.

ff stops parsing at the first non-flag argument and leaves the rest in
`GetArgs()`. This is structurally similar but is not a declared model concept;
it cannot be surfaced in help text or constrained to specific commands.

---

## 17. Error Suggestions

kong includes Levenshtein edit-distance comparison and generates "did you
mean X?" suggestions for unknown flag and command names.

ff returns a plain `unknownFlagError` with no suggestion.

---

## 18. Help System

| Capability | ff | kong |
|---|---|---|
| Auto-registered `--help` / `-h` | no — returns `ErrHelp`, caller handles display | yes — built-in, calls `Exit(0)` |
| Per-command extended help | `Command.LongHelp string` (plain string) | `HelpProvider` interface: `Help() string`, formatted via `go/doc` |
| Layout options | none; callers compose `ffhelp.Section` values | `HelpOptions`: Compact, Tree, FlagsLast, Summary, NoAppSummary, NoExpandSubcommands, WrapUpperBound, Indenter |
| Flag grouping in help | one flat `FLAGS` section per FlagSet level | yes, via `group:""` tag |
| Custom value formatting | `NoDefault`, `NoPlaceholder` on `FlagConfig` | `HelpValueFormatter` interface; `PlaceHolderProvider` on mappers |
| Context-sensitive subcommand help | yes — `ffhelp.Command` walks to selected | yes |
| Tree / compact display modes | no | yes |

---

## 19. Config File Formats

Both libraries accept a custom parse function; the built-in parsers differ at
the margins.

| Format | ff | kong |
|---|---|---|
| JSON | `ffjson.Parse` (nested, dotted keys) | `kong.JSON` resolver (nested, snake/camel variants) |
| TOML | `fftoml.Parse` | `kong-toml` plugin |
| YAML | `ffyaml.Parse` | `kong-yaml` plugin |
| `.env` | `ffenv.Parse` | no |
| Plain `key value` | `ff.PlainParser` | no |
| HCL | no | `kong-hcl` plugin |

---

## 20. Plugin and Dynamic Command System

kong provides two runtime extensibility mechanisms:

- `kong.Plugins` — embed a `[]interface{}` of command structs; subcommands are
  discovered at parse time, enabling third-party packages to contribute commands.
- `DynamicCommand(name, help, group, cmd, tags...)` — add a command after the
  grammar has been built from the initial struct.

ff's `Command.Subcommands []*Command` is a plain slice populated at
construction. There is no registration API or runtime discovery mechanism.

---

## Summary

### Gaps by implementation effort

**Low effort — additive, self-contained:**

- Named mapper types (`counter`, `filecontent`, `existingfile`, `existingdir`)
  — implement `flag.Value` wrappers and register them
- Aliases — extend `coreFlag` and flag lookup to check an `aliases []string`
  field
- Negatable boolean flags — at definition time, register a `--no-<name>` flag
  that sets the same pointer to `false`
- Enum in `AddStruct` tag — add an `enum` key to the `ff:` tag parser, backed
  by the existing `ffval.Enum[T]`
- Required flag declaration — add a `required` key to `FlagConfig` and
  `AddStruct`; check in a post-parse pass
- HCL config format — implement `ConfigFileParseFunc`
- Error suggestions — add Levenshtein lookup to `findShortFlag` / `findLongFlag`

**Moderate effort — requires new structures or parse-phase changes:**

- Post-parse validation hook — call a `Validate() error` method on the command
  struct in `Command.Run`
- XOR / AND constraint groups — add group membership metadata to `FlagConfig`;
  enforce in a post-parse constraint pass
- Flag grouping in help — add a `Group string` field to `FlagConfig` and
  `coreFlag`; sort sections in `ffhelp`
- Variable interpolation — string preprocessing pass at parse time using a
  supplied `Vars` map
- Env var fallback names per flag — extend `FlagConfig` with `EnvVarNames []string`
- Multiple config file paths (try in order) — extend `ParseContext`
- Embed + prefix namespacing in `AddStruct` — extend the reflection pass to
  recurse into embedded structs
- Plugin / dynamic command registration — add a registration API over the
  existing `Subcommands` slice

**High effort — architectural changes to the parse or dispatch pipeline:**

- Positional arguments — the parse loop treats all non-flags as remainders;
  supporting named, typed positionals requires a distinct parse phase
- Lifecycle hooks (`BeforeReset`, `BeforeResolve`, `BeforeApply`, `AfterApply`,
  `AfterRun`) — requires a multi-phase parse pipeline and a hook-dispatch
  mechanism
- Dependency injection into `Exec` — requires reflection-based parameter
  resolution, a binding registry, and a changed `Exec` calling convention
- Passthrough mode as a declared command property — requires parse-loop changes
  and model support
