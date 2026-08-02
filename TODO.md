# climax TODO

Template-enrichment backlog derived from drift analysis of three real climax apps —
**exegesis**, **agentic-dev-harness (adh)**, and **skillsaw** — against
`pkg/scaffold/templates/{root,cmd,version,main}.go.tmpl`.

Method note: `climax lint` reported **no drift** for all three, because its checks
cover only main.go signal handling, `run()` separation, and stdin threading. Every
item below was found by hand-diffing the apps' `root.go` / `cmd.go` / `version.go`
against the templates. `version.go` had **zero** drift across all three (it is
byte-identical to the template) — no version work is needed. All the drift lives in
`root.go` and the dispatcher.

Design stance: climax delivers by **codegen, not import**, and its stated scope bars
"generic outcome envelopes." Most items below are therefore proposed as **opt-in**
template variants selected by `climax init` / `climax add` flags, so the default
scaffold stays minimal. Two exceptions are P0 correctness fixes to the base template.

---

## P0 — base-template correctness (do first)

### 1. Add the unmatched-subcommand guard to `cmd.go.tmpl`
**Evidence:** exegesis *and* skillsaw independently added byte-identical code for this.
Two independent re-derivations means the stock template has a real bug.

**Problem:** With the stock dispatcher, `myapp bogus` selects the root (a group parent
whose `Exec == nil`), falls through to `r.Command.Run`, returns `ff.ErrNoExec`, and
exits **0** — indistinguishable from a bare `myapp` invocation. A typo silently
"succeeds."

**Change:** after `Parse` and before `r.Command.Run(ctx)`, insert:
```go
// An unmatched token leaves the selected command a group parent (Exec == nil)
// with a leftover positional. Without this it falls through to Run, returns
// ff.ErrNoExec, and exits 0 — indistinguishable from a bare invocation.
if sel := r.Command.GetSelected(); sel.Exec == nil {
    if rest := sel.Flags.GetArgs(); len(rest) > 0 {
        _, _ = fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(sel))
        return fmt.Errorf("%s: unknown subcommand %q", sel.Name, rest[0])
    }
}
```
Then teach `climax lint` (and `climax update`) to require this block, so existing
apps are told to add it. This is the highest-value single change.

### 2. Warn on leftover placeholder help text (`climax lint`)
**Evidence:** skillsaw shipped with `ShortHelp: "TODO: describe skillsaw here"` — the
untouched scaffold default — and lint passed it clean.

**Change:** add a lint check that flags a root/command whose `ShortHelp`/`LongHelp`
still contains the generated placeholder (`TODO: describe`, `APP_SHORT`, etc.). Warn,
don't error. Cheap guard against un-filled scaffolds reaching users.

---

## P1 — opt-in template features (generalized from adh)

adh is the only app that outgrew the base scaffold; its additions are broadly useful
CLI ergonomics, not adh-specific. Offer each behind an `init` flag so the default stays
lean. Recommend a single umbrella flag `climax init --machine` that turns on 3–6
together, plus individual flags for surgical use.

### 3. `--jsonl` outcome envelope  (flag: `climax init --jsonl`, or `climax add outcome`)
Generate, in the root package, a GENERIC envelope (no domain vocabulary):
```go
type Outcome struct {
    Status  string `json:"status"`            // ok | blocked | error
    Code    int    `json:"code"`              // process exit code (0 for ok)
    Reason  string `json:"reason,omitempty"`  // stable machine token (app-defined)
    Message string `json:"message,omitempty"` // human detail
    Data    any    `json:"data,omitempty"`    // payload on success
}
const (StatusOK = "ok"; StatusBlocked = "blocked"; StatusError = "error")
func (c *Config) EmitJSONL(v any) error
func (c *Config) EmitOK(data any) error
func (c *Config) EmitBlocked(code int, reason, message string) error
func (c *Config) EmitError(code int, reason, message string) error
```
Explicitly OUT of the template (belongs to the app): the concrete `Reason*` token set
and any `CodeForError`/`ReasonForError` that reads a domain error taxonomy. Generate at
most a stub `CodeForError(err error) int` returning 1, with a comment pointing the app
to specialize it. This is the piece climax's charter says it "doesn't do" — so it must
be opt-in and vocabulary-free; the transport shell only.

### 4. Global flags block  (flag: `climax init --global-flags`)
Bind on the root `Config` and `New`:
`--verbose (-v)`, `--quiet (-q)`, `--no-color`, and `--jsonl` (implied when #3 is on).
In the dispatcher, after `Parse`, add the quiet redirect:
```go
if r.Quiet { r.Stdout = io.Discard } // one place suppresses non-error output for all commands
```
Rationale: adh proves these four are wanted by any non-trivial CLI, and routing
`--quiet` through the shared `*Config.Stdout` is the clean way to make it global.

### 5. slog diagnostic logger  (flag: `climax init --logger`)
Add `Log *slog.Logger` to `Config` (seeded in `New`, rebuilt in `Run` after parse) plus:
```go
func NewLogger(w io.Writer, jsonl bool, level slog.Level) *slog.Logger // JSON handler under --jsonl
func LogLevel(verbose, quiet bool) slog.Level                          // quiet=Error, verbose=Debug, else Warn
```
Logger writes to **stderr**, keeping the stdout data plane clean. Note: `climax lint`
*already name-detects* a logger field to choose the exec-stub print style — so the
templates should be able to *generate* the field it looks for. Closes that gap.

### 6. Injected `Getenv`  (flag: `climax init --getenv`)
Add `Getenv func(string) string` to `Config` and change `New` to
`New(getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer)`, threading
`getenv` from the dispatcher's `Run(ctx, args, getenv, stdin, stdout, stderr)`.
Because this alters the `New`/`Run` signatures, it MUST be a template variant, not an
add-on splice — `climax add` needs to detect which variant an app uses (AST: does
`New` take a `getenv` param?) and generate matching command stubs. Rationale: makes
config precedence testable without touching `os.Getenv` (matryer getenv-injection).

---

## P2 — helpers & ergonomics

### 7. `MisplacedFlag` root helper  (flag: `climax init --posix-guard`)
**Evidence:** skillsaw added it; adh works around the same hazard elsewhere.
ff/v4 stops flag parsing at the first positional, so `cmd <path> --json` silently
swallows `--json` as a positional. Generate:
```go
// MisplacedFlag returns the first positional that looks like a flag ("-x"), or "".
// Commands taking positional paths call this to fail loudly instead of ignoring a flag.
func MisplacedFlag(args []string) string
```
Consider a companion `climax lint` note for commands that take positional args but
never call it.

### 8. `register()` split in the dispatcher  (auto past N commands)
**Evidence:** adh has 30+ commands and moved registration into a dedicated
`register(r *root.Config)` func, leaving `Run` focused on parse-and-dispatch.
**Change:** when `climax add` pushes a dispatcher past a threshold (say 8 commands),
generate/maintain a `register()` function instead of inlining `New(r)` calls in `Run`.
Keep the `// register new commands here` marker inside `register()`.

---

## P3 — lint/update coverage expansion

### 9. Make `climax lint` aware of opted-in features
Today lint is blind to everything in P0–P2. Once an app opts into a feature (detectable
via the `// climax:*` marker comments — extend them, e.g. `// climax:features jsonl,logger,getenv`):
- verify the generated `Outcome`/`Emit*`/`NewLogger`/`Getenv` shapes still match the
  current templates (drift detection for the opted-in surface);
- require the P0 unmatched-subcommand guard unconditionally;
- warn on placeholder help (#2) and on positional-taking commands missing `MisplacedFlag`.
Record the chosen feature set in a marker so `climax update --apply` can re-sync only
the surface the app actually uses.

---

## Non-goals (explicitly keep OUT of climax)

- Domain reason-token vocabularies (adh's `at_ops`/`ungrounded`/`proof`/…).
- App config systems (adh's `--config`/`--profile`/`--repo` + `ConfigGetenv` bridge).
- Any `CodeForError`/`ReasonForError` wired to a specific error taxonomy — generate a
  stub only. climax owns the envelope's *shape*; the app owns its *words*.
