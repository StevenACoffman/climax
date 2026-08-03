# climax TODO

Template-enrichment backlog derived from drift analysis of three real climax apps —
**exegesis**, **agentic-dev-harness (adh)**, and **skillsaw** — against
`pkg/scaffold/templates/{root,cmd,version,main}.go.tmpl`.

Method note: `climax lint` originally reported **no drift** for all three, because
its checks covered only main.go signal handling, `run()` separation, and stdin
threading. Every item below was found by hand-diffing the apps' `root.go` / `cmd.go`
/ `version.go` against the templates. `version.go` had **zero** drift across all
three (it is byte-identical to the template) — no version work is needed. All the
drift lives in `root.go` and the dispatcher.

**Status (2026-08-02): the entire P0–P3 backlog is complete.** No numbered items remain
outstanding; only the explicit non-goals below stay out of scope.
- **P0:** the unmatched-subcommand guard ships in `cmd.go.tmpl` (required by `climax
  lint`, tracked by `climax update`); `climax lint` warns on leftover placeholder help.
- **P1:** `climax init` takes `--jsonl`, `--global-flags`, `--logger`, `--getenv`, and
  the umbrella `--machine`, applied as pure post-expansion string transforms so the
  default scaffold stays byte-identical (drift self-check still clean).
- **P2:** `--posix-guard` generates the `MisplacedFlag` helper (folded into `--machine`);
  `climax add` auto-extracts registrations into a `register()` function once a dispatcher
  passes 8 commands.
- **P3:** `climax init` emits a `// climax:features …` marker, and `climax lint` uses it
  to drift-check the opted-in feature surface and warn on positional-taking commands that
  never call `MisplacedFlag` (both warning-severity).

Design stance: climax delivers by **codegen, not import**, and its stated scope bars
"generic outcome envelopes." Most items below are therefore proposed as **opt-in**
template variants selected by `climax init` / `climax add` flags, so the default
scaffold stays minimal. Two exceptions were P0 correctness fixes to the base
template — both now shipped.

---

## P0 — base-template correctness (DONE 2026-08-02)

### 1. Add the unmatched-subcommand guard to `cmd.go.tmpl` ✅ DONE
**Shipped:** guard added to `cmd.go.tmpl` and climax's own `cmd/cmd.go`;
`climax lint` requires it (error severity, detected via the guard's unique
`GetArgs()` call, with a suggested block derived from the template); `climax
update` tracks it as a drift-checked property. Verified end-to-end: a generated
app now exits non-zero with `<app>: unknown subcommand "…"` on a typo'd
subcommand, while bare and valid invocations are unchanged.

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

### 2. Warn on leftover placeholder help text (`climax lint`) ✅ DONE
**Shipped:** `climax lint` scans every `ff.Command`'s `ShortHelp`/`LongHelp` under
`cmd/` for un-filled scaffold defaults (`TODO: describe`, `is a new command`, and
raw `APP_SHORT`/`APP_LONG`/`CMD_SHORT`/`CMD_LONG` placeholders) and reports them at
**warning** severity — printed but never failing the exit code. Implemented via a
new `Severity` field on `LintIssue`; `climax lint` exits non-zero only on
error-severity issues.

**Evidence:** skillsaw shipped with `ShortHelp: "TODO: describe skillsaw here"` — the
untouched scaffold default — and lint passed it clean.

**Change:** add a lint check that flags a root/command whose `ShortHelp`/`LongHelp`
still contains the generated placeholder (`TODO: describe`, `APP_SHORT`, etc.). Warn,
don't error. Cheap guard against un-filled scaffolds reaching users.

---

## P1 — opt-in template features (generalized from adh) — ✅ DONE 2026-08-02

adh is the only app that outgrew the base scaffold; its additions are broadly useful
CLI ergonomics, not adh-specific. Offered behind `init` flags so the default stays
lean, plus the umbrella `climax init --machine` that turns all four on.

**Implementation note:** all four features are applied as pure `string→string`
transforms over the already-expanded base templates (`pkg/scaffold/features.go`),
formatted with `go/format`. The base templates are therefore untouched — `climax
lint` and the `climax update` drift self-check are unaffected, and the default
scaffold is byte-identical. getenv needed **no** `climax add` change: child commands
keep `New(parent *root.Config)` and are registered unchanged. Only cross-feature
coupling: the logger's post-parse rebuild requires `--global-flags` (for the
verbose/quiet/jsonl fields); logger alone emits a static seed.

### 3. `--jsonl` outcome envelope  ✅ DONE (flag: `climax init --jsonl`)
**Shipped:** generates `cmd/<root>/outcome.go` with the generic `Outcome` struct,
`Status*` consts, `EmitJSONL/EmitOK/EmitBlocked/EmitError` methods on `*Config`, and a
package-level stub `CodeForError(err error) int` (returns 1). Vocabulary-free; the app
owns the `Reason` tokens and specializes `CodeForError`. Original spec below.

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

### 4. Global flags block  ✅ DONE (flag: `climax init --global-flags`)
**Shipped:** binds `Verbose/-v`, `Quiet/-q`, `NoColor`, `JSONL` on the root `Config`,
attaches the flag set to the root `ff.Command` (so they parse and show in `--help`),
and adds the `if r.Quiet { r.Stdout = io.Discard }` redirect after `Parse`. Original
spec below.

### 4. Global flags block  (flag: `climax init --global-flags`)
Bind on the root `Config` and `New`:
`--verbose (-v)`, `--quiet (-q)`, `--no-color`, and `--jsonl` (implied when #3 is on).
In the dispatcher, after `Parse`, add the quiet redirect:
```go
if r.Quiet { r.Stdout = io.Discard } // one place suppresses non-error output for all commands
```
Rationale: adh proves these four are wanted by any non-trivial CLI, and routing
`--quiet` through the shared `*Config.Stdout` is the clean way to make it global.

### 5. slog diagnostic logger  ✅ DONE (flag: `climax init --logger`)
**Shipped:** adds `Log *slog.Logger` to `Config` (seeded in `New`), generates
`cmd/<root>/logger.go` with `NewLogger(w, jsonl, level)` and `LogLevel(verbose, quiet)`,
and (with `--global-flags`) rebuilds the logger in `Run` after parse. Writes to stderr.
Original spec below.

### 5. slog diagnostic logger  (flag: `climax init --logger`)
Add `Log *slog.Logger` to `Config` (seeded in `New`, rebuilt in `Run` after parse) plus:
```go
func NewLogger(w io.Writer, jsonl bool, level slog.Level) *slog.Logger // JSON handler under --jsonl
func LogLevel(verbose, quiet bool) slog.Level                          // quiet=Error, verbose=Debug, else Warn
```
Logger writes to **stderr**, keeping the stdout data plane clean. Note: `climax lint`
*already name-detects* a logger field to choose the exec-stub print style — so the
templates should be able to *generate* the field it looks for. Closes that gap.

### 6. Injected `Getenv`  ✅ DONE (flag: `climax init --getenv`)
**Shipped:** adds `Getenv func(string) string` to `Config`, changes `New`/`Run`
signatures to thread it, and passes `os.Getenv` from `main`. Applied as a splice (the
base templates stay intact); `climax add` needed no change because child command
signatures are unchanged. Original spec below.

### 6. Injected `Getenv`  (flag: `climax init --getenv`)
Add `Getenv func(string) string` to `Config` and change `New` to
`New(getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer)`, threading
`getenv` from the dispatcher's `Run(ctx, args, getenv, stdin, stdout, stderr)`.
Because this alters the `New`/`Run` signatures, it MUST be a template variant, not an
add-on splice — `climax add` needs to detect which variant an app uses (AST: does
`New` take a `getenv` param?) and generate matching command stubs. Rationale: makes
config precedence testable without touching `os.Getenv` (matryer getenv-injection).

---

## P2 — helpers & ergonomics — ✅ DONE 2026-08-02

### 7. `MisplacedFlag` root helper  ✅ DONE (flag: `climax init --posix-guard`)
**Shipped:** `--posix-guard` (folded into `--machine`) generates
`cmd/<root>/misplaced.go` with the pure `MisplacedFlag(args []string) string` helper —
returns the first flag-looking token, skipping the conventional positionals `-` (stdin)
and `--` (end-of-flags). Same opt-in mechanism as the P1 feature files. The companion
`climax lint` note for positional-taking commands that never call it stays **P3** (#9).
Original spec below.

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

### 8. `register()` split in the dispatcher  ✅ DONE (auto past N commands)
**Shipped:** `climax add` runs `maybeSplitRegister` (a pure `[]byte→[]byte` transform,
`registerSplitThreshold = 8`). Once the dispatcher holds ≥8 inline registrations, the
next add extracts the whole block — plus the `// register new commands here` marker —
into `func register(r *root.Config)`, replacing it in `Run` with a single `register(r)`
call. Idempotent (splits once); later adds insert at the marker inside `register()` with
no change to the existing insertion logic. Marker-based dispatchers only (AST-only ones
keep inlining). Verified with a `go vet` integration test. Original spec below.

### 8. `register()` split in the dispatcher  (auto past N commands)
**Evidence:** adh has 30+ commands and moved registration into a dedicated
`register(r *root.Config)` func, leaving `Run` focused on parse-and-dispatch.
**Change:** when `climax add` pushes a dispatcher past a threshold (say 8 commands),
generate/maintain a `register()` function instead of inlining `New(r)` calls in `Run`.
Keep the `// register new commands here` marker inside `register()`.

---

## P3 — lint/update coverage expansion — ✅ DONE 2026-08-02

### 9. Make `climax lint` aware of opted-in features ✅ DONE
**Shipped, all three sub-items:**
- **Feature marker.** `climax init` now splices a `// climax:features
  jsonl,global-flags,logger,getenv,posix-guard` marker into cmd.go (after the
  root-pkg marker; base template untouched, so drift self-check stays clean). Read
  via the existing marker machinery (`dispatcherInfo.features` + `readMarker`);
  `parseFeatureMarker` turns it into a set.
- **Opted-in surface drift** (`lintFeatureSurface`, warning severity). For each
  opted-in **file-generating** feature (jsonl→outcome.go, logger→logger.go,
  posix-guard→misplaced.go), the app's file must declare every top-level symbol its
  current template does (expected symbols derived from the template via
  `parsedSourceFromTemplate` → DRY; presence-checked so customization/additions are
  tolerated). Missing file or missing symbols → one warning each. Splice features
  (getenv, global-flags) are recorded in the marker but not file-surface-checked.
- **MisplacedFlag usage** (`lintMisplacedFlagUsage`, warning, gated on `posix-guard`).
  Warns on a command whose `exec` reads positionals (its `[]string` param is named,
  not `_` — the scaffold's own convention) but never calls `MisplacedFlag`.

Both new checks are **warnings** (feature files are user-owned; advisory, non-fatal —
exit code unchanged). Verified with unit tests plus manual smoke across all cases.
Original spec below.

### 9. Make `climax lint` aware of opted-in features (original spec)
All P1/P2 feature surfaces now ship as templates (`outcome.go.tmpl`, `logger.go.tmpl`,
`misplaced.go.tmpl`, plus the root/dispatcher/main splices), but lint/update are still
blind to them — no feature marker is emitted (deliberately deferred). Once `climax init`
starts emitting a `// climax:features jsonl,global-flags,logger,getenv,posix-guard`
marker:
- verify the generated `Outcome`/`Emit*`/`NewLogger`/`Getenv`/`MisplacedFlag` shapes
  still match the current templates (drift detection for the opted-in surface);
- ~~require the P0 unmatched-subcommand guard unconditionally~~ ✅ done (P0 #1);
- ~~warn on placeholder help (#2)~~ ✅ done (P0 #2);
- warn on positional-taking commands that never call `MisplacedFlag` — now actionable
  since the helper ships (P2 #7); needs an AST check for commands whose `exec` reads
  `args` positionals but never calls `MisplacedFlag`.
Record the chosen feature set in a marker so `climax update --apply` can re-sync only
the surface the app actually uses.

---

## Non-goals (explicitly keep OUT of climax)

- Domain reason-token vocabularies (adh's `at_ops`/`ungrounded`/`proof`/…).
- App config systems (adh's `--config`/`--profile`/`--repo` + `ConfigGetenv` bridge).
- Any `CodeForError`/`ReasonForError` wired to a specific error taxonomy — generate a
  stub only. climax owns the envelope's *shape*; the app owns its *words*.
