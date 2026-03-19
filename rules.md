# Go Application Rules

Unified rules derived from:
- [Ben Johnson](./benbjohnson_rules.md)'s gobeyond.dev series on Go application design
- [Mat Ryer](./matryer_rules)'s "https://grafana.com/blog/how-i-write-http-services-in-go-after-13-years/"
- [Mitchell Hashimoto](./mitchelh_rules.md)'s "https://www.youtube.com/watch?v=8hQG7QlcLBk" (GopherCon 2017)

---

## Do Not

These are the highest-priority rules. They represent the most common mistakes.

**Architecture**
- Do not put domain types in subpackages — they belong in the root package.
- Do not group packages by type (`models/`, `controllers/`, `handlers/`) — group by dependency instead.
- Do not allow the root package to import any other package in the application.
- Do not use global variables for application state (DB connections, config, etc.).
- Do not put business logic in `main` — it only wires dependencies.
- Do not put the `main` package in the project root — put binaries under `cmd/`.
- Do not enforce authorization in HTTP handlers or middleware — enforce it in service implementations, embedded in SQL where possible.
- Do not call `os.Exit` anywhere except `main`. Return errors up the stack.

**HTTP**
- Do not make handlers methods on a server struct. Use maker funcs that take dependencies as arguments.
- Do not use the global `flag` package. Use `flag.NewFlagSet` inside `run`.
- Do not use `t.SetEnv` in tests. Use the `getenv` parameter instead.
- Do not store durable state in handler closures. Use a database.
- Do not put fallible setup in `addRoutes`. Resolve errors in `run` before calling it.
- Do not use a named `middleware` type alias. Return `func(http.Handler) http.Handler` directly.
- Do not call `json.NewEncoder` or `json.NewDecoder` inline in handlers. Use centralized helpers.
- Do not repeat middleware dependency arguments on every `mux.Handle` call. Use a constructor that closes over dependencies once.

**SQL**
- Do not use ORMs — use `database/sql` directly.
- Do not expose transactions to callers of a service — transactions are an implementation detail.
- Do not return `(nil, nil)` from a function that looks up a single entity by ID.
- Do not allow arbitrary sort columns from callers — map a fixed set of named values to SQL.
- Do not interpolate caller-supplied strings into SQL queries — use parameterized queries.

**Testing**
- Do not use third-party testing frameworks — use the stdlib `testing` package only.
- Do not return errors from test helpers — call `t.Fatalf` inside the helper.
- Do not omit `t.Helper()` from test helpers.
- Do not use `time.Sleep` in tests — use `select` + `time.After`.
- Do not hardcode port numbers in tests — use `"127.0.0.1:0"` and let the OS assign one.
- Do not mock `net.Conn` — make real network connections.
- Do not test unexported functions as the primary testing strategy.
- Do not write unit tests that duplicate assertions already covered by an end-to-end test.

---

## Project Layout

```
myapp/                    — root package: domain types, interfaces, Error type
    user.go               — User struct, UserService interface
    dial.go               — Dial struct, DialService interface
    error.go              — Error type, error codes, ErrorCode(), ErrorMessage()
    context.go            — NewContextWithUser(), UserIDFromContext()
    cmd/
        myapp/
            main.go       — calls run(), handles exit code
            run.go        — wires dependencies, starts server
        myappctl/
            main.go       — optional CLI binary
    sqlite/               — SQLite implementation of domain interfaces
        sqlite.go         — DB type, Open(), Close()
        user.go           — sqlite.UserService
        dial.go           — sqlite.DialService
    http/                 — HTTP adapter (subpackage, not a binary)
        server.go         — NewServer() constructor
        routes.go         — addRoutes(); every mux.Handle call lives here
        middleware.go     — middleware constructor functions
        encode.go         — encode, decode, decodeValid generic helpers
        user.go           — handler maker funcs for users
        dial.go           — handler maker funcs for dials
    mock/                 — shared mock implementations for testing
        user_service.go   — mock.UserService
        dial_service.go   — mock.DialService
```

**Rules:**
- The root package name matches the application name (e.g., `package myapp`).
- Each subpackage is named after the dependency it wraps: `sqlite`, `http`, `mock`.
- It is acceptable to name a package the same as its wrapped stdlib package (e.g., `http`) because the two are never used in the same file.
- Multiple binaries: one subdirectory per binary under `cmd/`.
- Group related types together in one file. One major concept per file.
- Target 200–500 SLOC per file. 1000 SLOC is the hard limit.
- Put the most important type at the top of the file; lesser types below.
- If a package exceeds ~10,000 SLOC total, evaluate whether it should be split into separate projects.

---

## Root Package: Domain Types and Interfaces

The root package defines the application's domain language. It contains only:
- Plain data structs with no external dependencies
- Service interfaces
- The `Error` type and error helpers
- Context helpers for passing the authenticated user

```go
// myapp/user.go
package myapp

import "context"

// User represents an application user.
type User struct {
    ID    int
    Name  string
    Email string
}

// UserService defines operations on users.
// Implementations live in subpackages (sqlite, postgres, mock).
type UserService interface {
    // FindUserByID retrieves a user by ID.
    // Returns ENOTFOUND if the user does not exist.
    FindUserByID(ctx context.Context, id int) (*User, error)

    // FindUsers retrieves a list of users matching filter.
    // Also returns the total count of matching users regardless of Limit/Offset.
    FindUsers(ctx context.Context, filter UserFilter) ([]*User, int, error)

    // CreateUser creates a new user.
    // On success, user.ID and timestamps are populated on the input struct.
    CreateUser(ctx context.Context, user *User) error

    // UpdateUser updates an existing user by ID.
    // Returns the updated user even if an error occurs.
    // Returns ENOTFOUND if the user does not exist.
    // Returns EUNAUTHORIZED if the caller does not own the user.
    UpdateUser(ctx context.Context, id int, upd UserUpdate) (*User, error)

    // DeleteUser permanently removes a user by ID.
    // Returns ENOTFOUND if the user does not exist.
    // Returns EUNAUTHORIZED if the caller does not own the user.
    DeleteUser(ctx context.Context, id int) error
}

// UserFilter filters results from FindUsers.
type UserFilter struct {
    ID    *int    // optional
    Email *string // optional

    Offset int
    Limit  int
}

// UserUpdate holds the fields that can be updated on a user.
// Pointer fields are optional — nil means do not change.
type UserUpdate struct {
    Name  *string
    Email *string
}
```

**Rules:**
- Domain structs reference only primitive types and other domain types.
- No `database/sql`, `net/http`, or any other external import in the root package.
- Service interfaces live alongside the types they operate on, in the same file.
- Every interface method has a godoc comment documenting which error codes it can return.
- Filter structs use pointer fields so each field is independently optional.
- Update structs use pointer fields so partial updates are expressible without a separate endpoint.

---

## Error Type

Define one `Error` type in the root package.

```go
// myapp/error.go
package myapp

import (
    "bytes"
    "fmt"
)

// Application error codes.
const (
    ECONFLICT     = "conflict"     // action cannot be performed
    EINTERNAL     = "internal"     // internal error
    EINVALID      = "invalid"      // validation failed
    ENOTFOUND     = "not_found"    // entity does not exist
    EUNAUTHORIZED = "unauthorized" // caller lacks permission
)

// Error defines a standard application error.
type Error struct {
    Code    string // machine-readable error code
    Message string // human-readable message for end users
    Op      string // logical operation, e.g. "sqlite.UserService.FindUserByID"
    Err     error  // nested error
}

func (e *Error) Error() string {
    var buf bytes.Buffer
    if e.Op != "" {
        fmt.Fprintf(&buf, "%s: ", e.Op)
    }
    if e.Err != nil {
        buf.WriteString(e.Err.Error())
    } else {
        if e.Code != "" {
            fmt.Fprintf(&buf, "<%s> ", e.Code)
        }
        buf.WriteString(e.Message)
    }
    return buf.String()
}

// ErrorCode returns the code of the root error, or EINTERNAL if none is set.
func ErrorCode(err error) string {
    if err == nil {
        return ""
    }
    if e, ok := err.(*Error); ok && e.Code != "" {
        return e.Code
    } else if ok && e.Err != nil {
        return ErrorCode(e.Err)
    }
    return EINTERNAL
}

// ErrorMessage returns the human-readable message, or a generic fallback.
func ErrorMessage(err error) string {
    if err == nil {
        return ""
    }
    if e, ok := err.(*Error); ok && e.Message != "" {
        return e.Message
    } else if ok && e.Err != nil {
        return ErrorMessage(e.Err)
    }
    return "An internal error has occurred. Please contact technical support."
}
```

**Rules:**
- Start with five codes. Add more only as needed.
- A wrapping error carries `Op` + `Err`. A leaf error carries `Code` + `Message`. Never both.
- Translate all external errors (e.g., `sql.ErrNoRows`) to domain codes at the implementation boundary.
- Call `ErrorCode(err)` and `ErrorMessage(err)` in calling code — never type-assert `*Error` directly.

### Op for logical stack traces

Every significant function wraps errors with its `Op` name using the format `"package.Type.Method"`:

```go
func (s *UserService) CreateUser(ctx context.Context, user *myapp.User) error {
    const op = "sqlite.UserService.CreateUser"
    if err := s.insertUser(ctx, user); err != nil {
        return &myapp.Error{Op: op, Err: err}
    }
    return nil
}
```

The resulting `Error()` string is a single-line logical stack trace:
```
sqlite.UserService.CreateUser: sqlite.insertUser: near "INSERT": syntax error
```

---

## Authentication via Context

```go
// myapp/context.go
package myapp

import "context"

type contextKey int

const userContextKey contextKey = iota

func NewContextWithUser(ctx context.Context, user *User) context.Context {
    return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) *User {
    u, _ := ctx.Value(userContextKey).(*User)
    return u
}

func UserIDFromContext(ctx context.Context) int {
    if u := UserFromContext(ctx); u != nil {
        return u.ID
    }
    return 0
}
```

**Rules:**
- The HTTP layer sets the user on the context after authentication.
- Service implementations extract the user from context to apply authorization.
- Authorization is enforced at the lowest level — embedded in SQL `WHERE` clauses so the database enforces it.

```go
// sqlite/dial.go — authorization embedded in the query, not in the HTTP handler
func findDials(ctx context.Context, tx *Tx, filter myapp.DialFilter) ([]*myapp.Dial, int, error) {
    userID := myapp.UserIDFromContext(ctx)
    where := []string{"1 = 1"}
    args := []interface{}{}
    where = append(where, `id IN (SELECT dial_id FROM dial_memberships WHERE user_id = ?)`)
    args = append(args, userID)
    // ...
}
```

---

## Subpackages: Dependency Adapters

Each subpackage wraps one external dependency and implements one or more domain interfaces.

```go
// sqlite/user.go
package sqlite

import (
    "context"
    "myapp"
)

type UserService struct {
    db *DB
}

func (s *UserService) FindUserByID(ctx context.Context, id int) (*myapp.User, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()

    user, err := findUserByID(ctx, tx, id)
    if err != nil {
        return nil, err
    }
    return user, nil
}
```

**Rules:**
- Subpackages never import each other.
- All service methods follow the same shape: begin transaction → call helpers → commit.
- Because all implementations satisfy the same root-package interface, they can be stacked with caching or decorator wrappers.

```go
// myapp/user_cache.go — caching layer in the root package
type UserCache struct {
    cache   map[int]*User
    service UserService
}

func NewUserCache(service UserService) *UserCache {
    return &UserCache{cache: make(map[int]*User), service: service}
}

// cmd/myapp/main.go
userService := myapp.NewUserCache(&sqlite.UserService{DB: db})
```

---

## Program Entry Point

`main` does one thing: call `run` and handle the exit code.

```go
func main() {
    ctx := context.Background()
    if err := run(ctx, os.Args, os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
        os.Exit(1)
    }
}
```

`run` accepts OS primitives as arguments so tests can substitute them without touching global state:

```go
func run(
    ctx    context.Context,
    args   []string,
    getenv func(string) string,
    stdin  io.Reader,
    stdout, stderr io.Writer,
) error {
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
    defer cancel()
    // parse flags, build dependencies, call NewServer, start httpServer
}
```

| Parameter | `main` passes | Test passes |
|---|---|---|
| `ctx` | `context.Background()` | `context.WithCancel(...)` |
| `args` | `os.Args` | custom `[]string` |
| `getenv` | `os.Getenv` | custom func |
| `stdin` | `os.Stdin` | `strings.NewReader(...)` |
| `stdout` | `os.Stdout` | `&bytes.Buffer{}` |
| `stderr` | `os.Stderr` | `io.Discard` |

**Rules:**
- `signal.NotifyContext` goes inside `run`, not `main`, so `cancel` is deferred.
- Use `flag.NewFlagSet(args[0], flag.ContinueOnError)` and pass `args[1:]` to it. Never use the global `flag` package.
- Use the `getenv` parameter instead of calling `os.Getenv` directly.

---

## HTTP Layer

### Server Constructor

`NewServer` takes all dependencies as explicit arguments and returns `http.Handler`.

```go
func NewServer(
    logger *Logger,
    config *Config,
    userService myapp.UserService,
    dialService myapp.DialService,
) http.Handler {
    mux := http.NewServeMux()
    addRoutes(mux, logger, config, userService, dialService)
    var handler http.Handler = mux
    handler = someMiddleware(handler)
    handler = someMiddleware2(handler)
    return handler
}
```

**Rules:**
- Return type is `http.Handler`, not a named struct, unless the situation genuinely requires more.
- Pass `nil` for dependencies a particular test does not exercise.
- Global middleware (CORS, auth, logging) is applied here, not in `addRoutes`.
- Prefer explicit argument lists over config structs — the compiler enforces completeness.
- The HTTP layer translates domain errors to HTTP status codes using `myapp.ErrorCode(err)`:

```go
func errorStatusCode(err error) int {
    switch myapp.ErrorCode(err) {
    case myapp.ENOTFOUND:
        return http.StatusNotFound
    case myapp.EINVALID:
        return http.StatusBadRequest
    case myapp.EUNAUTHORIZED:
        return http.StatusUnauthorized
    case myapp.ECONFLICT:
        return http.StatusConflict
    default:
        return http.StatusInternalServerError
    }
}
```

### Route Registration

All routes live in `routes.go`. This is the single place to see the full API surface.

```go
func addRoutes(
    mux          *http.ServeMux,
    logger       *Logger,
    config       Config,
    userService  myapp.UserService,
    dialService  myapp.DialService,
) {
    mux.Handle("/api/v1/users", handleUsersGet(logger, userService))
    mux.Handle("/api/v1/users/", handleUserGet(logger, userService))
    mux.Handle("/admin", adminOnly(handleAdminIndex(logger)))
    mux.HandleFunc("/healthz", handleHealthz(logger))
    mux.Handle("/", http.NotFoundHandler())
}
```

**Rules:**
- `addRoutes` does not return an error. Anything fallible is resolved in `run` before this is called.
- Always register an explicit `http.NotFoundHandler()` for `/`.
- Always include a `/healthz` or `/readyz` endpoint.
- Per-route middleware is applied inline here.

### Handler Maker Funcs

Handlers are maker funcs: functions that take dependencies and return `http.Handler`.

```go
func handleSomething(logger *Logger, store *Store) http.Handler {
    // one-time setup runs here at registration time, not per request
    thing := prepareThing()
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // per-request logic
        logger.Info(r.Context(), "handleSomething")
    })
}
```

**Rules:**
- The return type is always `http.Handler`, not `http.HandlerFunc`.
- The outer function's scope is the closure environment. Use it for one-time setup.
- Only read shared closure data from concurrent handlers. Protect any writes with a mutex.

Defer expensive setup with `sync.Once`:

```go
func handleTemplate(files ...string) http.Handler {
    var (
        init   sync.Once
        tpl    *template.Template
        tplerr error
    )
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        init.Do(func() {
            tpl, tplerr = template.ParseFiles(files...)
        })
        if tplerr != nil {
            http.Error(w, tplerr.Error(), http.StatusInternalServerError)
            return
        }
        // use tpl
    })
}
```

Declare request/response types inside the maker func if they are handler-specific:

```go
func handleSomething() http.Handler {
    type request struct {
        Name string
    }
    type response struct {
        Greeting string `json:"greeting"`
    }
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ...
    })
}
```

### Encoding and Decoding

Centralize JSON encode/decode in `encode.go`. All handlers call these helpers.

`encode` takes `r *http.Request` even though it only writes a response. This is intentional: it keeps the signature symmetric with `decode` and allows future content negotiation (reading the `Accept` header) without changing every call site. Do not remove the `r` parameter.

```go
func encode[T any](w http.ResponseWriter, r *http.Request, status int, v T) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    if err := json.NewEncoder(w).Encode(v); err != nil {
        return fmt.Errorf("encode json: %w", err)
    }
    return nil
}

func decode[T any](r *http.Request) (T, error) {
    var v T
    if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
        return v, fmt.Errorf("decode json: %w", err)
    }
    return v, nil
}
```

Usage:
```go
// encode — type inferred from argument
err := encode(w, r, http.StatusOK, obj)

// decode — type must be specified (T only in return position)
decoded, err := decode[CreateSomethingRequest](r)
```

### Validation

Use a single-method `Validator` interface. Request types implement it.

```go
type Validator interface {
    Valid(ctx context.Context) (problems map[string]string)
}
```

```go
func decodeValid[T Validator](r *http.Request) (T, map[string]string, error) {
    var v T
    if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
        return v, nil, fmt.Errorf("decode json: %w", err)
    }
    if problems := v.Valid(r.Context()); len(problems) > 0 {
        return v, problems, fmt.Errorf("invalid %T: %d problems", v, len(problems))
    }
    return v, nil, nil
}
```

**Rules:**
- `Valid` returns `nil` (not an empty map) when valid.
- Keep `Valid` to field-level checks. Database checks belong outside this method.
- Use `decodeValid` for types that implement `Validator`; use `decode` for others.
- Never call `v.Valid()` inline in handler code — that belongs in `decodeValid`.

### Middleware

Middleware signature: `func(http.Handler) http.Handler`. No named type alias.

```go
func adminOnly(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !currentUser(r).IsAdmin {
            http.NotFound(w, r)
            return
        }
        h(w, r)
    })
}
```

When middleware needs dependencies, use a constructor that closes over them:

```go
// middleware.go
func newAuthMiddleware(logger Logger, db *DB) func(http.Handler) http.Handler {
    return func(h http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // use logger, db
            h.ServeHTTP(w, r)
        })
    }
}

// routes.go
auth := newAuthMiddleware(logger, db)
mux.Handle("/route1", auth(handleSomething(deps)))
mux.Handle("/route2", auth(handleSomething2(deps)))
```

### Graceful Shutdown

```go
httpServer := &http.Server{
    Addr:    net.JoinHostPort(config.Host, config.Port),
    Handler: srv,
}
go func() {
    if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        fmt.Fprintf(stderr, "error listening and serving: %s\n", err)
    }
}()
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := httpServer.Shutdown(shutdownCtx); err != nil {
        fmt.Fprintf(stderr, "error shutting down: %s\n", err)
    }
}()
wg.Wait()
```

---

## SQL Layer

### DB and Tx Types

Wrap `*sql.DB` and `*sql.Tx` in application-specific types:

```go
// sqlite/sqlite.go
type DB struct {
    db  *sql.DB
    DSN string
}

func (db *DB) Open() error  { ... }
func (db *DB) Close() error { ... }

type Tx struct {
    *sql.Tx
}
```

### Service Methods: Transaction Boundary Only

Service methods are thin. They own the transaction; helper functions own the SQL.

```go
func (s *DialService) CreateDial(ctx context.Context, dial *myapp.Dial) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    if err := createDial(ctx, tx, dial); err != nil {
        return err
    }
    return tx.Commit()
}
```

### Helper Functions: Unexported, Accept `*Tx`, Reusable

Helper functions are package-level (not attached to a service type) so multiple service methods can call them within the same transaction.

```go
func createDial(ctx context.Context, tx *Tx, dial *myapp.Dial) error {
    const op = "sqlite.createDial"
    result, err := tx.ExecContext(ctx, `
        INSERT INTO dials (user_id, name, created_at, updated_at)
        VALUES (?, ?, ?, ?)`,
        dial.UserID, dial.Name, dial.CreatedAt, dial.UpdatedAt,
    )
    if err != nil {
        return &myapp.Error{Op: op, Err: err}
    }
    dial.ID, err = result.LastInsertId()
    return err
}
```

### Row Iteration

```go
rows, err := tx.QueryContext(ctx, query, args...)
if err != nil {
    return nil, 0, err
}
defer rows.Close()

dials := make([]*myapp.Dial, 0)  // make, not var — encodes as [] not null in JSON
var n int
for rows.Next() {
    var dial myapp.Dial
    if err := rows.Scan(&dial.ID, &dial.Name, &n); err != nil {
        return nil, 0, err
    }
    dials = append(dials, &dial)
}
return dials, n, rows.Err()
```

Three rules:
1. `defer rows.Close()` immediately after a successful `QueryContext`.
2. Initialize slices with `make([]*T, 0)` — nil slices encode as JSON `null`; empty slices as `[]`.
3. Return `rows.Err()` after the loop — it captures errors that occurred mid-iteration.

### Building WHERE Clauses Dynamically

```go
where := []string{"1 = 1"}
args := []interface{}{}

if v := filter.ID; v != nil {
    where = append(where, "id = ?")
    args = append(args, *v)
}
if v := filter.Email; v != nil {
    where = append(where, "email = ?")
    args = append(args, *v)
}

query := `SELECT id, name, email, COUNT(*) OVER() FROM users WHERE ` +
    strings.Join(where, " AND ") +
    ` ORDER BY ` + orderBy +
    ` LIMIT ? OFFSET ?`
args = append(args, filter.Limit, filter.Offset)
```

Start with `"1 = 1"` so the slice is never empty and `strings.Join` always produces valid SQL.

### Pagination with Total Count in One Query

```sql
SELECT id, name, COUNT(*) OVER()
FROM dials
WHERE user_id = ?
ORDER BY id ASC
LIMIT ? OFFSET ?
```

`COUNT(*) OVER()` is a window function that returns the total matching row count on every row, ignoring `LIMIT`/`OFFSET`. Scan it on every iteration — the value is the same each time.

### Sort Order: Fixed Named Values Only

```go
var orderBy string
switch filter.SortBy {
case "name_asc":
    orderBy = "name ASC"
case "updated_at_desc":
    orderBy = "updated_at DESC"
default:
    orderBy = "id ASC"
}
```

Never interpolate caller-supplied strings into SQL. Always provide a safe default.

### Loading Associated Data

Use an `attachXAssociations` helper to load related objects after the primary query:

The service method calls the helper in a loop after the primary query:

```go
func (s *DialService) FindDials(ctx context.Context, filter myapp.DialFilter) ([]*myapp.Dial, int, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, 0, err
    }
    defer tx.Rollback()

    dials, n, err := findDials(ctx, tx, filter)
    if err != nil {
        return dials, n, err
    }
    for _, dial := range dials {
        if err := attachDialAssociations(ctx, tx, dial); err != nil {
            return dials, n, err
        }
    }
    return dials, n, nil
}

func attachDialAssociations(ctx context.Context, tx *Tx, dial *myapp.Dial) error {
    var err error
    if dial.User, err = findUserByID(ctx, tx, dial.UserID); err != nil {
        return fmt.Errorf("attach dial user: %w", err)
    }
    return nil
}
```

Always return parent associations. Include child collections only when small and almost always needed.

---

## CRUD Conventions

### FindByID

```go
FindUserByID(ctx context.Context, id int) (*User, error)
```

- Never return `(nil, nil)`. Return `ENOTFOUND` if the entity does not exist.
- Implement by calling the list helper and changing the empty-result semantics:

```go
func findUserByID(ctx context.Context, tx *Tx, id int) (*myapp.User, error) {
    users, _, err := findUsers(ctx, tx, myapp.UserFilter{ID: &id})
    if err != nil {
        return nil, err
    } else if len(users) == 0 {
        return nil, &myapp.Error{Code: myapp.ENOTFOUND, Message: "User not found."}
    }
    return users[0], nil
}
```

### FindMany

```go
FindUsers(ctx context.Context, filter UserFilter) ([]*User, int, error)
```

- `(nil, 0, nil)` is a valid return — an empty result set is not an error.
- The `int` return is the total count for pagination (ignores `Limit`/`Offset`).

### Create

```go
CreateUser(ctx context.Context, user *User) error
```

- Accept a pointer to the domain struct.
- Mutate the input pointer in place with generated values (ID, CreatedAt, UpdatedAt).
- Nest related object creation in the same transaction by accepting populated child slices on the input struct.

### Update

```go
UpdateUser(ctx context.Context, id int, upd UserUpdate) (*User, error)
```

- Accept an `Update` struct with pointer fields; nil means unchanged.
- Return the updated object even when an error occurs — web UIs need it to replay the form.
- The `id` is separate from `UserUpdate` so the same update struct can target multiple IDs.

### Delete

```go
DeleteUser(ctx context.Context, id int) error
```

- Delete by primary key.
- Enforce authorization inside the implementation using `UserIDFromContext`.

---

## Mock Package

The mock package provides hand-written mocks for testing. No third-party mock libraries.

```go
// mock/user_service.go
package mock

import (
    "context"
    "myapp"
)

type UserService struct {
    FindUserByIDFn      func(ctx context.Context, id int) (*myapp.User, error)
    FindUserByIDInvoked bool

    CreateUserFn      func(ctx context.Context, user *myapp.User) error
    CreateUserInvoked bool

    // one Fn + Invoked pair per interface method
}

func (s *UserService) FindUserByID(ctx context.Context, id int) (*myapp.User, error) {
    s.FindUserByIDInvoked = true
    return s.FindUserByIDFn(ctx, id)
}
```

Usage in tests:

```go
var svc mock.UserService
svc.FindUserByIDFn = func(ctx context.Context, id int) (*myapp.User, error) {
    if id != 100 {
        t.Fatalf("unexpected id: %d", id)
    }
    return &myapp.User{ID: 100, Name: "Susy"}, nil
}
```

**Rules:**
- One mock per domain interface, in the `mock` package.
- Write them by hand — no mock generation tools.
- The `Invoked` booleans let tests assert that a method was or was not called.

---

## Wiring Dependencies

`main` (or `run`) wires concrete implementations to interfaces and starts the server. No business logic.

```go
// cmd/myapp/main.go
func main() {
    ctx := context.Background()
    if err := run(ctx, os.Args, os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
        os.Exit(1)
    }
}

// cmd/myapp/run.go
func run(ctx context.Context, args []string, getenv func(string) string,
    stdin io.Reader, stdout, stderr io.Writer) error {

    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
    defer cancel()

    db := &sqlite.DB{DSN: getenv("DATABASE_URL")}
    if err := db.Open(); err != nil {
        return err
    }
    defer db.Close()

    userService := myapp.NewUserCache(&sqlite.UserService{DB: db})
    dialService := &sqlite.DialService{DB: db}

    srv := http.NewServer(logger, config, userService, dialService)
    // start httpServer with graceful shutdown
}
```

**Rules:**
- `main` is the only place where concrete implementation packages (`sqlite`, `http`) are imported together.
- Dependency injection is manual — no framework.
- `main` is also an adapter: it connects OS environment (env vars, args) to the domain.
- `main` only calls `run`; `run` does the actual wiring.

---

## Test Methodology

### Table-Driven Tests

Always use table-driven tests. Set up the table structure even for a single case.

```go
cases := map[string]struct{ A, B, Expected int }{
    "positive":  {1, 1, 2},
    "negative":  {-1, -2, -3},
    "mixed":     {1, -1, 0},
    "both zero": {0, 0, 0},
}
for name, tc := range cases {
    tc := tc // capture loop variable (required before Go 1.22)
    t.Run(name, func(t *testing.T) {
        actual := tc.A + tc.B
        if actual != tc.Expected {
            t.Errorf("expected %d, got %d", tc.Expected, actual)
        }
    })
}
```

**Rules:**
- Name every case. Never rely on array indices in failure output.
- Use `t.Run` to wrap each case. `defer` works per-subtest and cases are individually targetable.
- Capture the loop variable inside the subtest: `tc := tc`.

### Test Fixtures

Store test data in a `test-fixtures/` directory alongside the test file.

```go
func TestParseConfig(t *testing.T) {
    data := filepath.Join("test-fixtures", "valid_config.hcl")
    f, err := os.Open(data)
    if err != nil {
        t.Fatalf("failed to open fixture: %s", err)
    }
    defer f.Close()
}
```

`go test` always sets the working directory to the package directory being tested, so relative paths work regardless of where `go test` is invoked.

### Golden Files

Use golden files to test complex output (formatted text, serialized structs, generated code).

```go
var update = flag.Bool("update", false, "update golden files")

func TestFormat(t *testing.T) {
    cases := []struct{ Name, Input string }{
        {"basic", "input.hcl"},
        {"empty", "empty.hcl"},
    }
    for _, tc := range cases {
        t.Run(tc.Name, func(t *testing.T) {
            input, _ := os.ReadFile(filepath.Join("test-fixtures", tc.Input))
            actual := Format(input)

            golden := filepath.Join("test-fixtures", tc.Name+".golden")
            if *update {
                os.WriteFile(golden, actual, 0644)
            }

            expected, _ := os.ReadFile(golden)
            if !bytes.Equal(actual, expected) {
                t.Errorf("output mismatch for %s\ngot:\n%s\nwant:\n%s",
                    tc.Name, actual, expected)
            }
        })
    }
}
```

Workflow: run `go test -update` to generate golden files, inspect them by eye, commit when correct.

### Test Helpers

Never return an error from a test helper. Accept `*testing.T` and call `t.Fatalf` internally.

```go
// Wrong
func testTempFile(t *testing.T) (string, error) { … }

// Right
func testTempFile(t *testing.T) (string, func()) {
    t.Helper()
    tf, err := os.CreateTemp("", "test")
    if err != nil {
        t.Fatalf("testTempFile: %s", err)
    }
    tf.Close()
    return tf.Name(), func() { os.Remove(tf.Name()) }
}

func TestSomething(t *testing.T) {
    path, cleanup := testTempFile(t)
    defer cleanup()
}
```

When a helper has no meaningful return value, one-line the defer:

```go
func testChdir(t *testing.T, dir string) func() {
    t.Helper()
    old, err := os.Getwd()
    if err != nil {
        t.Fatalf("testChdir: %s", err)
    }
    if err := os.Chdir(dir); err != nil {
        t.Fatalf("testChdir: %s", err)
    }
    return func() { os.Chdir(old) }
}

func TestThing(t *testing.T) {
    defer testChdir(t, "/tmp/testdir")()
}
```

**Rules:**
- Always call `t.Helper()` at the top of every test helper.
- Return a `func()` for cleanup; defer it at the call site.
- Use `t.Fatal` (not `t.Error`) in helpers when execution cannot continue.

Wrap real types with test-specific setup/teardown helpers:

```go
// sqlite/testing_test.go
package sqlite_test

type TestDB struct {
    *sqlite.DB
}

func MustOpenDB(t *testing.T) *TestDB {
    t.Helper()
    db := &sqlite.DB{DSN: ":memory:"}
    if err := db.Open(); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { db.Close() })
    return &TestDB{db}
}
```

Test assertion helpers (use instead of verbose `if err != nil` blocks):

```go
func assert(t *testing.T, condition bool, msg string) {
    t.Helper()
    if !condition {
        t.Fatal(msg)
    }
}

func ok(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %s", err)
    }
}

func equals(t *testing.T, exp, act interface{}) {
    t.Helper()
    if exp != act {
        t.Fatalf("expected %v, got %v", exp, act)
    }
}
```

### Timing-Dependent Tests

Use `select` + `time.After` + `timeMultiplier`. Never use `time.Sleep`.

```go
var timeMultiplier = time.Duration(1)

func TestAsyncThing(t *testing.T) {
    done := make(chan struct{})
    go doAsyncWork(done)

    select {
    case <-done:
    case <-time.After(5 * time.Second * timeMultiplier):
        t.Fatal("timed out waiting for async work")
    }
}
```

`timeMultiplier` is a package-level variable so CI environments can increase it without changing test logic.

### Parallelization

**Unit tests:** Do not use `t.Parallel()`. Parallel tests make failures ambiguous — you cannot tell whether a failure is a logic bug or a race condition.

**Run-based integration tests:** `t.Parallel()` is safe when calling `run()` end-to-end, because `run` has no global state. Each invocation has its own wired dependencies.

The test port is controlled via the `getenv` parameter; the server binds to the port from configuration and does not pick one at random. The same `getenv` function provides all environment values the test needs:

```go
func TestSomethingEndToEnd(t *testing.T) {
    t.Parallel() // safe because run has no global state
    ctx, cancel := context.WithCancel(context.Background())
    t.Cleanup(cancel)

    getenv := func(key string) string {
        switch key {
        case "PORT":
            return "18080"
        case "DATABASE_URL":
            return "file::memory:?cache=shared"
        default:
            return ""
        }
    }
    args := []string{"myapp"}

    go run(ctx, args, getenv, nil, io.Discard, io.Discard)
    waitForReady(ctx, 5*time.Second, "http://localhost:18080/healthz")
    // hit the API as a real client would
}
```

Use `t.Cleanup(cancel)` — the context cancels when the test ends, triggering graceful shutdown. Delete unit tests that assert the same thing as an end-to-end test. One authoritative set of tests is easier to maintain than duplicated assertions spread across layers. Write handler-level unit tests only when there is specific complex logic that warrants isolated coverage.

### Wait for readiness

Poll `/healthz` before hitting the API. The polling loop also provides a health endpoint that real users benefit from — designing for testability exposes what users need.

```go
func waitForReady(ctx context.Context, timeout time.Duration, endpoint string) error {
    client := http.Client{}
    start := time.Now()
    for {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
        if err != nil {
            return fmt.Errorf("failed to create request: %w", err)
        }
        resp, err := client.Do(req)
        if err == nil && resp.StatusCode == http.StatusOK {
            resp.Body.Close()
            return nil
        }
        if resp != nil {
            resp.Body.Close()
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if time.Since(start) >= timeout {
                return fmt.Errorf("timeout waiting for endpoint")
            }
            time.Sleep(250 * time.Millisecond)
        }
    }
}
```

### Inline test types for storytelling

When request/response types are scoped inside a maker func, declare minimal inline structs in test code that express only what the test cares about. This makes intent explicit.

```go
// Only Name matters for this endpoint — the struct says so
person := struct {
    Name string `json:"name"`
}{Name: "Mat Ryer"}
```

If you need to verify a response field, declare only that field:

```go
var got struct {
    Greeting string `json:"greeting"`
}
json.NewDecoder(resp.Body).Decode(&got)
```

If you need to validate concurrent behavior in unit tests, use goroutines and `-race` explicitly, run as separate processes rather than via `t.Parallel()`.

---

## Writing Testable Code

### Global State

Avoid global state. When it is unavoidable, use this hierarchy:

```go
// Worst — cannot be changed in tests
const port = 1000

// Better — can be overridden in tests
var port = 1000

// Best — constant default, configurable via struct
const defaultPort = 1000

type ServerOpts struct {
    Port int // initialize to defaultPort in constructor
}
```

Do not use `init()` to set global state that tests cannot override.

### Test the Exported API

Test the exported API, not internals. Unexported functions are implementation details. If the exported API is correct, the internals are correct by definition.

```go
// Test this:
func TestServer_CreateUser(t *testing.T) { … }

// Not this (unless the function is extremely complex):
func Test_hashPassword(t *testing.T) { … }
```

Use `internal/` packages to over-package safely. Internal packages cannot be imported by external consumers, giving good test boundaries without committing to a public API.

Test files use the external test package to test the exported API:

```go
// sqlite/user_test.go
package sqlite_test
```

### Networking

Always make real network connections in tests. Use OS-assigned ports.

```go
func testConn(t *testing.T) (client, server net.Conn) {
    t.Helper()
    ln, _ := net.Listen("tcp", "127.0.0.1:0")
    var srv net.Conn
    go func() {
        defer ln.Close()
        srv, _ = ln.Accept()
    }()
    cli, _ := net.Dial("tcp", ln.Addr().String())
    return cli, srv
}
```

### Configurability

Production code with hardcoded behavior (fixed ports, paths, timeouts) is hard to test. Overparameterize structs. Unexported fields are fine — only internal tests need access.

```go
type ServerOpts struct {
    CachePath string
    Port      int
    // unexported test-only field
    testSkipAuth bool
}

func (s *Server) authenticate(r *http.Request) (User, error) {
    if s.opts.testSkipAuth {
        return User{ID: "test-user"}, nil
    }
    return s.oauthProvider.Validate(r)
}
```

### Complex Struct Equality

Choose based on what gives the most useful failure output:

**Option 1 — `reflect.DeepEqual`**: works, but failure messages are often opaque.

**Option 2 — third-party diff library**: generates human-readable diff on mismatch.

**Option 3 — `testString()` pattern**: for large or deeply nested structures, add an unexported method that renders the fields that matter as a human-readable string, then compare strings.

```go
// In a _test.go file in the package under test:
func (g *Graph) testString() string {
    var buf bytes.Buffer
    for _, node := range g.nodes {
        fmt.Fprintf(&buf, "%s -> %v\n", node.Name, node.Deps)
    }
    return buf.String()
}

if got.testString() != want.testString() {
    t.Errorf("graph mismatch:\ngot:\n%s\nwant:\n%s",
        got.testString(), want.testString())
}
```

### Subprocessing

**Option 1 — Execute the real binary.** Guard with a boolean initialized in `init`:

```go
var testHasGit bool

func init() {
    if _, err := exec.LookPath("git"); err == nil {
        testHasGit = true
    }
}

func TestGitStatus(t *testing.T) {
    if !testHasGit {
        t.Log("git not found, skipping")
        t.Skip()
    }
    // use real git binary
}
```

**Option 2 — Mock the subprocess** using the `helperProcess` pattern:

```go
func helperProcess(s ...string) *exec.Cmd {
    cs := []string{"-test.run=TestHelperProcess", "--"}
    cs = append(cs, s...)
    env := []string{"GO_WANT_HELPER_PROCESS=1"}
    cmd := exec.Command(os.Args[0], cs...)
    cmd.Env = append(env, os.Environ()...)
    return cmd
}

func TestHelperProcess(*testing.T) {
    if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
        return
    }
    defer os.Exit(0)

    args := os.Args
    for len(args) > 0 {
        if args[0] == "--" {
            args = args[1:]
            break
        }
        args = args[1:]
    }

    cmd, args := args[0], args[1:]
    switch cmd {
    case "status":
        fmt.Println("nothing to commit")
    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
        os.Exit(1)
    }
}
```

`TestHelperProcess` is a no-op during normal `go test` runs. When called via `helperProcess`, it implements mock subprocess behavior.

### Interfaces

Define interfaces at the point of use. The caller — not the callee — defines what it needs. Keep interfaces as small as possible.

```go
// Too broad — tests must implement the full net.Conn interface
func ServeConn(c net.Conn) error { … }

// Better — tests only need to implement io.ReadWriteCloser
func ServeConn(c io.ReadWriteCloser) error { … }
```

Create interfaces where you expect alternate implementations or where the interface is the natural mocking seam. Do not create an interface for every type.

### Inline test interfaces

The `mock` package is for application-wide domain interfaces that multiple packages need. For a one-off dependency in a single test file, declare an inline struct with a function field instead:

```go
type fakeMailer struct {
    SendFunc func(to, subject, body string) error
}

func (f *fakeMailer) Send(to, subject, body string) error {
    return f.SendFunc(to, subject, body)
}
```

Use it in the test:

```go
mailer := &fakeMailer{
    SendFunc: func(to, subject, body string) error {
        if to != "user@example.com" {
            t.Errorf("unexpected recipient: %s", to)
        }
        return nil
    },
}
```

This keeps the interface narrow (the caller defines only what it needs), keeps the mock local to the test file, and avoids polluting the `mock` package with types that only one test uses.

### Testing as a Public API

For packages consumed by other packages, export test helpers in a `testing.go` file (not `_test.go`). Use `go-testing-interface` instead of `*testing.T` to avoid injecting `go test` flags into every program that imports your package.

```go
// testing.go
import testing "github.com/mitchellh/go-testing-interface"

func TestConfig(t testing.T) *Config {
    t.Helper()
    return &Config{
        Addr:    "127.0.0.1:0",
        Timeout: 100 * time.Millisecond,
    }
}

func TestConfigInvalid(t testing.T) *Config {
    t.Helper()
    return &Config{} // missing required fields
}

func TestServer(t testing.T) (net.Addr, io.Closer) {
    t.Helper()
    srv := newInMemoryServer()
    if err := srv.Start(); err != nil {
        t.Fatalf("TestServer: %s", err)
    }
    return srv.Addr(), srv
}
```

Also export mock structs for interfaces your package defines, so consumers can record and replay calls.

### Custom Frameworks

Always use `go test` as the test runner, even for acceptance and integration tests. Build custom frameworks inside `go test`, not alongside it.

Guard expensive test suites with a flag:

```go
var flagAcceptance = flag.Bool("acceptance", false, "run acceptance tests")

func TestProvider_basic(t *testing.T) {
    if !*flagAcceptance {
        t.Skip("skipping acceptance test; run with -acceptance")
    }
    // provision real resources, make real API calls
}
```

### Repeat Yourself in Tests

Prefer a long, flat, self-contained test over a short test that delegates to many helpers in many files. When a test fails months later, the reader should understand it by reading one function.

```go
// Prefer this (200 lines, everything in one place):
func TestContext_Apply_basicDestroy(t *testing.T) {
    m := testModule(t, "apply-destroy")
    p := testProvider("aws")
    p.ApplyFn = testApplyFn
    p.DiffFn = testDiffFn
    state := &State{ … }
    ctx := testContext(t, &ContextOpts{
        Module:    m,
        Providers: map[string]ResourceProviderFactory{"aws": testProviderFuncFixed(p)},
        State:     state,
    })
    // … 180 more explicit lines
}

// Over this (20 lines calling 7 helpers across 4 files):
func TestContext_Apply_basicDestroy(t *testing.T) {
    ctx := defaultTestContext(t)
    applyAndCheck(t, ctx, expectedDestroyState())
}
```

When writing a new test that resembles an existing one, copy and modify. It feels wasteful but keeps each test independently readable.

The rule applies to *test logic*, not to *test infrastructure*. Low-level setup helpers that are universally applicable (`testTempFile`, `testTempDB`, `testConn`, `MustOpenDB`) are good abstractions — they handle error-prone setup that every test in the package needs. What you should not abstract is the sequence of actions and assertions that constitutes the test itself.

---

## Unified Checklist

### Project structure
- [ ] Root package contains domain types, service interfaces, Error type — no external imports
- [ ] Subpackages named after wrapped dependency (`sqlite`, `http`, `mock`)
- [ ] Binaries live under `cmd/`; `main.go` and `run.go` in `cmd/myapp/`, not in the `http/` subpackage
- [ ] File organization: one major concept per file, 200–500 SLOC target
- [ ] Most important type at top of file; lesser types below

### Domain layer
- [ ] Domain structs reference only primitive types and other domain types
- [ ] Service interfaces live alongside the types they operate on
- [ ] Every interface method documents which error codes it can return
- [ ] Filter structs use pointer fields; Update structs use pointer fields

### Error handling
- [ ] Error type in root package with Code, Message, Op, Err fields
- [ ] Five base error codes: ECONFLICT, EINTERNAL, EINVALID, ENOTFOUND, EUNAUTHORIZED
- [ ] `ErrorCode()` and `ErrorMessage()` helpers in root package
- [ ] External errors translated to domain error codes at the implementation boundary
- [ ] `Op` set on every significant function using `"package.Type.Method"` format

### Authentication
- [ ] `NewContextWithUser()` and `UserIDFromContext()` in root package `context.go`
- [ ] Authorization enforced inside service implementations, embedded in SQL `WHERE` clauses

### Program entry point
- [ ] `main` only calls `run`; passes `os.Args`, `os.Getenv`, `os.Stdin`, `os.Stdout`, `os.Stderr`
- [ ] `run` accepts `(ctx, args, getenv, stdin, stdout, stderr)` — no OS globals
- [ ] `signal.NotifyContext` and `defer cancel()` inside `run`
- [ ] `flag.NewFlagSet` inside `run`; global `flag` package never used

### HTTP layer
- [ ] `NewServer` takes all dependencies as arguments, returns `http.Handler`
- [ ] Global middleware applied in `NewServer`; per-route middleware in `routes.go`
- [ ] All routes registered in `routes.go`; explicit `http.NotFoundHandler()` for `/`
- [ ] `/healthz` endpoint present
- [ ] Handlers are maker funcs: `func handleX(deps...) http.Handler` — not struct methods
- [ ] Maker func return type is `http.Handler`, not `http.HandlerFunc`
- [ ] Request/response types declared inside maker func if handler-specific
- [ ] Expensive one-time setup deferred with `sync.Once`
- [ ] All JSON encode/decode through `encode`/`decode`/`decodeValid` in `encode.go`
- [ ] `decodeValid` used for request types that implement `Validator`
- [ ] `Validator` interface: `Valid(ctx context.Context) map[string]string`
- [ ] Middleware signature: `func(http.Handler) http.Handler`; no named type alias
- [ ] Middleware with dependencies uses a constructor; deps not repeated per route
- [ ] Graceful shutdown: `<-ctx.Done()` → `httpServer.Shutdown` with timeout → `wg.Wait()`
- [ ] HTTP layer translates domain error codes to HTTP status codes via `ErrorCode(err)`
- [ ] `encode` keeps `r *http.Request` parameter for symmetry and future content negotiation
- [ ] Middleware dependencies closed over in a constructor; not repeated per `mux.Handle` call

### SQL layer
- [ ] Service methods are thin: begin tx → helpers → commit; `defer tx.Rollback()`
- [ ] SQL helpers are unexported package-level functions accepting `*Tx`
- [ ] `defer rows.Close()` after every successful `QueryContext`
- [ ] `rows.Err()` returned after every row iteration loop
- [ ] Result slices initialized with `make([]*T, 0)`, not `var`
- [ ] `WHERE` clauses built with `[]string{"1 = 1"}` + `strings.Join`
- [ ] Sort order mapped from fixed named values, never interpolated from caller
- [ ] `COUNT(*) OVER()` used for pagination total count in a single query
- [ ] `attachXAssociations` helper called in a loop inside the service method after the primary query

### CRUD conventions
- [ ] `FindByID` never returns `(nil, nil)`; returns `ENOTFOUND` if missing
- [ ] `FindMany` returns `([]*T, int, error)`; `int` is total count for pagination
- [ ] `Create` mutates the input pointer (sets ID, timestamps); nested related objects created in same transaction
- [ ] `UpdateX` returns the updated object even on error

### Mock package
- [ ] Hand-written mocks with `Fn` + `Invoked` field pairs per method
- [ ] No mock generation tools

### Dependency injection
- [ ] `main` only wires dependencies; no business logic
- [ ] Caching/layering via wrapper types that implement domain interfaces
- [ ] No global variables; no dependency injection framework

### Test methodology
- [ ] Table-driven test structure used, even for single cases
- [ ] Every table case has a name; no index-based names
- [ ] `t.Run` used to scope each case; loop variable captured
- [ ] Test fixtures stored in `test-fixtures/`, accessed with relative paths
- [ ] Complex output tested with golden files and an `-update` flag
- [ ] Test helpers accept `*testing.T`, never return errors, always call `t.Helper()`
- [ ] Cleanup returned as `func()` closure, deferred at call site
- [ ] `assert`, `ok`, `equals` helpers defined; no assertion libraries
- [ ] Async tests use `select` + `time.After` + `timeMultiplier`, not `time.Sleep`
- [ ] Unit tests do not call `t.Parallel()`; run-based integration tests may
- [ ] Run-based tests use `getenv` func to control port, DSN, and other config
- [ ] `waitForReady` polls `/healthz` before making test assertions
- [ ] Unit tests that duplicate end-to-end assertions are deleted

### Writing testable code
- [ ] No unoverridable global constants controlling runtime behavior
- [ ] Configurable defaults: `const defaultX` + `struct { X type }` pattern
- [ ] Only exported API tested; test files use `_test` package suffix
- [ ] Large packages split via `internal/` to create test boundaries
- [ ] Real network connections in tests; OS-assigned ports via `"127.0.0.1:0"`
- [ ] Structs overparameterized so tests can override ports, paths, timeouts
- [ ] Complex struct equality tested via diff library or `testString()` pattern
- [ ] Subprocess tests use `testHasX bool` guard or `helperProcess` mock pattern
- [ ] Interfaces defined at point of use, as narrow as the caller needs
- [ ] One-off test-local interfaces use inline struct + function field; `mock` package reserved for app-wide interfaces
- [ ] Public packages export test helpers in `testing.go` using `go-testing-interface`
- [ ] Acceptance/integration tests guarded by a flag, always run via `go test`
- [ ] Tests are flat and self-contained; repeat test logic rather than abstracting it
- [ ] Infrastructure helpers (`testTempFile`, `testTempDB`, `testConn`) are abstracted; test logic is not
