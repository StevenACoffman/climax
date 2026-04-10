package scaffold

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticDispatcher is the canonical climax dispatcher shape, WITH markers.
const syntheticDispatcher = `package cmd
// climax:name myapp
// climax:root-pkg root

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"example.com/myapp/cmd/root"
	// climax:imports
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)
	// register new commands here

	if err := r.Command.Parse(args); err != nil {
		fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	if err := r.Command.Run(ctx); err != nil {
		if !errors.Is(err, ff.ErrNoExec) {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
`

// syntheticDispatcherNoMarkers is the same file with both markers removed.
const syntheticDispatcherNoMarkers = `package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"example.com/myapp/cmd/root"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)

	if err := r.Command.Parse(args); err != nil {
		fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	if err := r.Command.Run(ctx); err != nil {
		if !errors.Is(err, ff.ErrNoExec) {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
`

// syntheticDispatcherWithCommands has some commands registered (no markers).
const syntheticDispatcherWithCommands = `package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"example.com/myapp/cmd/root"
	"example.com/myapp/cmd/serve"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)
	serve.New(r)

	if err := r.Command.Parse(args); err != nil {
		fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command))
		return fmt.Errorf("parse: %w", err)
	}

	if err := r.Command.Run(ctx); err != nil {
		if !errors.Is(err, ff.ErrNoExec) {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(r.Command.GetSelected()))
		}
		return err
	}

	return nil
}
`

func TestProbeDispatcherAST_withMarkers(t *testing.T) {
	src := []byte(syntheticDispatcher)
	info, err := probeDispatcherAST("cmd.go", src)
	if err != nil {
		t.Fatalf("probeDispatcherAST: %v", err)
	}
	if info.rootPkg != "root" {
		t.Errorf("rootPkg: got %q, want %q", info.rootPkg, "root")
	}
	if info.rootVar != "r" {
		t.Errorf("rootVar: got %q, want %q", info.rootVar, "r")
	}
	if info.importEndOffset == 0 {
		t.Error("importEndOffset must be non-zero")
	}
	if info.callInsertOffset == 0 {
		t.Error("callInsertOffset must be non-zero")
	}
	// The closing ')' of the import group must be at importEndOffset.
	if src[info.importEndOffset] != ')' {
		t.Errorf("importEndOffset %d: got byte %q, want ')'",
			info.importEndOffset, src[info.importEndOffset])
	}
	// The first byte at callInsertOffset must start the 'if' keyword.
	if string(src[info.callInsertOffset:info.callInsertOffset+2]) != "if" {
		t.Errorf("callInsertOffset %d: got %q, want start of 'if'",
			info.callInsertOffset, src[info.callInsertOffset:info.callInsertOffset+4])
	}
}

func TestProbeDispatcherAST_noMarkers(t *testing.T) {
	src := []byte(syntheticDispatcherNoMarkers)
	info, err := probeDispatcherAST("cmd.go", src)
	if err != nil {
		t.Fatalf("probeDispatcherAST (no markers): %v", err)
	}
	if info.rootPkg != "root" {
		t.Errorf("rootPkg: got %q, want %q", info.rootPkg, "root")
	}
	if info.rootVar != "r" {
		t.Errorf("rootVar: got %q, want %q", info.rootVar, "r")
	}
	if src[info.importEndOffset] != ')' {
		t.Errorf("importEndOffset %d: byte %q, want ')'",
			info.importEndOffset, src[info.importEndOffset])
	}
	if string(src[info.callInsertOffset:info.callInsertOffset+2]) != "if" {
		t.Errorf("callInsertOffset %d: got %q, want 'if'",
			info.callInsertOffset, src[info.callInsertOffset:info.callInsertOffset+4])
	}
}

func TestProbeDispatcherAST_notADispatcher(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"no func Run", `package cmd
import "fmt"
func Other() { fmt.Println("hi") }
`},
		{"Run has no Parse call", `package cmd
import (
	"context"
	"io"
	"example.com/app/cmd/root"
)
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)
	return nil
}
`},
		{"no import group", `package cmd
import "context"
func Run(ctx context.Context) error { return nil }
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := probeDispatcherAST("cmd.go", []byte(tc.src))
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestExtractRootFromBody_firstDefineWins(t *testing.T) {
	// If there are multiple := assignments, the first one is used.
	src := `package cmd
import (
	"context"
	"io"
	"example.com/app/cmd/myroot"
	"example.com/app/cmd/other"
)
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg := myroot.New(stdout, stderr)
	_ = other.New(cfg)
	if err := cfg.Command.Parse(args); err != nil { return err }
	if err := cfg.Command.Run(ctx); err != nil { return err }
	return nil
}
`
	info, err := probeDispatcherAST("cmd.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.rootPkg != "myroot" {
		t.Errorf("rootPkg: got %q, want %q", info.rootPkg, "myroot")
	}
	if info.rootVar != "cfg" {
		t.Errorf("rootVar: got %q, want %q", info.rootVar, "cfg")
	}
}

func TestFindParentCallInAST_bareCall(t *testing.T) {
	src := []byte(syntheticDispatcherWithCommands)
	pcr, err := findParentCallInAST("cmd.go", src, "serve")
	if err != nil {
		t.Fatalf("findParentCallInAST: %v", err)
	}
	if pcr.isCaptured {
		t.Error("expected isCaptured=false for bare ExprStmt")
	}
	if pcr.cfgVar != "serveCfg" {
		t.Errorf("cfgVar: got %q, want %q", pcr.cfgVar, "serveCfg")
	}
	if pcr.callStart == 0 {
		t.Error("callStart must be non-zero")
	}
	if pcr.lineEnd <= pcr.callStart {
		t.Errorf("lineEnd %d must be > callStart %d", pcr.lineEnd, pcr.callStart)
	}
	// The byte at callStart should be 's' (start of "serve.New(r)").
	if src[pcr.callStart] != 's' {
		t.Errorf("callStart %d: got byte %q, want 's'", pcr.callStart, src[pcr.callStart])
	}
}

func TestFindParentCallInAST_capturedCall(t *testing.T) {
	src := []byte(`package cmd
import (
	"context"
	"io"
	"example.com/app/cmd/root"
	"example.com/app/cmd/config"
)
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	r := root.New(stdout, stderr)
	configCfg := config.New(r)
	_ = configCfg
	if err := r.Command.Parse(args); err != nil { return err }
	if err := r.Command.Run(ctx); err != nil { return err }
	return nil
}
`)
	pcr, err := findParentCallInAST("cmd.go", src, "config")
	if err != nil {
		t.Fatalf("findParentCallInAST: %v", err)
	}
	if !pcr.isCaptured {
		t.Error("expected isCaptured=true for AssignStmt")
	}
	if pcr.cfgVar != "configCfg" {
		t.Errorf("cfgVar: got %q, want %q", pcr.cfgVar, "configCfg")
	}
}

func TestFindParentCallInAST_notFound(t *testing.T) {
	src := []byte(syntheticDispatcherNoMarkers)
	_, err := findParentCallInAST("cmd.go", src, "nonexistent")
	if err == nil {
		t.Error("expected error for missing parent command, got nil")
	}
}

func TestInsertAt(t *testing.T) {
	src := []byte("hello world")
	got := insertAt(src, 5, ", dear")
	if string(got) != "hello, dear world" {
		t.Errorf("got %q, want %q", got, "hello, dear world")
	}
	// Original must not be mutated.
	if string(src) != "hello world" {
		t.Errorf("src was mutated: %q", src)
	}
}

func TestInsertAt_atStart(t *testing.T) {
	got := insertAt([]byte("world"), 0, "hello ")
	if string(got) != "hello world" {
		t.Errorf("got %q", got)
	}
}

func TestInsertAt_atEnd(t *testing.T) {
	got := insertAt([]byte("hello"), 5, " world")
	if string(got) != "hello world" {
		t.Errorf("got %q", got)
	}
}

func TestIsCommandMethodCall(t *testing.T) {
	// Build a synthetic call AST: r.Command.Parse(args)
	// Use probeDispatcherAST on a known-good source to exercise isCommandMethodCall indirectly.
	src := []byte(syntheticDispatcher)
	info, err := probeDispatcherAST("cmd.go", src)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// callInsertOffset must point into the 'if' statement that uses r.Command.Parse.
	// This means isCommandMethodCall returned true during probing.
	if string(src[info.callInsertOffset:info.callInsertOffset+2]) != "if" {
		t.Errorf("callInsertOffset does not point at 'if': %q",
			src[info.callInsertOffset:info.callInsertOffset+4])
	}
}

func TestStmtLineEnd(t *testing.T) {
	// Verify that stmtLineEnd returns the byte offset AFTER the '\n'.
	src := []byte(syntheticDispatcherWithCommands)
	fset := token.NewFileSet()
	// We rely on findParentCallInAST which calls stmtLineEnd internally.
	pcr, err := findParentCallInAST("cmd.go", src, "serve")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// The byte just before lineEnd should be '\n'.
	if pcr.lineEnd == 0 || src[pcr.lineEnd-1] != '\n' {
		t.Errorf("lineEnd %d: byte before it is %q, want '\\n'",
			pcr.lineEnd, src[pcr.lineEnd-1])
	}
	_ = fset
}

func TestLooksLikeDispatcher_commandGo(t *testing.T) {
	// The filename "command.go" must be accepted just like "cmd.go".
	if !looksLikeDispatcher("command.go", []byte(syntheticDispatcher)) {
		t.Error("expected true for dispatcher with markers in command.go")
	}
	if !looksLikeDispatcher("command.go", []byte(syntheticDispatcherNoMarkers)) {
		t.Error("expected true for marker-free dispatcher in command.go")
	}
}

func TestFindDispatcherFile_commandGo(t *testing.T) {
	// Write a dispatcher to cmd/command.go (not cmd/cmd.go) and verify discovery.
	appDir := t.TempDir()
	cmdDir := filepath.Join(appDir, "cmd")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(cmdDir, "command.go"),
		[]byte(syntheticDispatcher),
		0o644,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, src, err := findDispatcherFile(appDir)
	if err != nil {
		t.Fatalf("findDispatcherFile: %v", err)
	}
	if filepath.Base(path) != "command.go" {
		t.Errorf("path base: got %q, want %q", filepath.Base(path), "command.go")
	}
	if len(src) == 0 {
		t.Error("expected non-empty src")
	}
}

func TestFindDispatcherFile_noMarkers_commandGo(t *testing.T) {
	// Verify that a marker-free dispatcher in command.go is also discovered.
	appDir := t.TempDir()
	cmdDir := filepath.Join(appDir, "cmd")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(cmdDir, "command.go"),
		[]byte(syntheticDispatcherNoMarkers),
		0o644,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, _, err := findDispatcherFile(appDir)
	if err != nil {
		t.Fatalf("findDispatcherFile: %v", err)
	}
	if filepath.Base(path) != "command.go" {
		t.Errorf("path base: got %q, want %q", filepath.Base(path), "command.go")
	}
}

func TestLooksLikeDispatcher_markers(t *testing.T) {
	if !looksLikeDispatcher("cmd.go", []byte(syntheticDispatcher)) {
		t.Error("expected true for dispatcher with markers")
	}
}

func TestLooksLikeDispatcher_noMarkers(t *testing.T) {
	if !looksLikeDispatcher("cmd.go", []byte(syntheticDispatcherNoMarkers)) {
		t.Error("expected true for marker-free dispatcher matching AST pattern")
	}
}

func TestLooksLikeDispatcher_notADispatcher(t *testing.T) {
	src := []byte(`package other
func Unrelated() {}
`)
	if looksLikeDispatcher("other.go", src) {
		t.Error("expected false for non-dispatcher file")
	}
}

func TestIsCommandLit(t *testing.T) {
	// extractCLIName opens a file path; with a nonexistent path it must return "".
	name := extractCLIName("/nonexistent", "root")
	if name != "" {
		t.Errorf("expected empty for missing file, got %q", name)
	}
}

func TestProbeRootConfig_stdoutStderr(t *testing.T) {
	src := `package root
import "io"
type Config struct {
	Stdout io.Writer
	Stderr io.Writer
}
`
	dir := writeRootFile(t, "root", src)
	rc := probeRootConfig(dir, "root")
	if !rc.hasStdout {
		t.Error("expected hasStdout=true")
	}
	if !rc.hasStderr {
		t.Error("expected hasStderr=true")
	}
	if rc.loggerField != "" {
		t.Errorf("expected loggerField empty, got %q", rc.loggerField)
	}
}

func TestProbeRootConfig_logger(t *testing.T) {
	src := `package root
import "go.uber.org/zap"
type Config struct {
	Logger *zap.Logger
}
`
	dir := writeRootFile(t, "root", src)
	rc := probeRootConfig(dir, "root")
	if rc.hasStdout {
		t.Error("expected hasStdout=false")
	}
	if rc.loggerField != "Logger" {
		t.Errorf("loggerField: got %q, want %q", rc.loggerField, "Logger")
	}
}

func TestProbeRootConfig_unknown(t *testing.T) {
	src := `package root
type Config struct {
	SomeField string
}
`
	dir := writeRootFile(t, "root", src)
	rc := probeRootConfig(dir, "root")
	if rc.hasStdout || rc.hasStderr || rc.loggerField != "" {
		t.Errorf("expected zero rootConfigInfo, got %+v", rc)
	}
}

func TestProbeRootConfig_missingFile(t *testing.T) {
	rc := probeRootConfig("/nonexistent", "root")
	if rc.hasStdout || rc.hasStderr || rc.loggerField != "" {
		t.Errorf("expected zero rootConfigInfo for missing file, got %+v", rc)
	}
}

// writeRootFile writes src to <tmpdir>/cmd/<pkg>/<pkg>.go and returns tmpdir.
func writeRootFile(t *testing.T, pkg, src string) string {
	t.Helper()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "cmd", pkg)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, pkg+".go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestExecStubVars_stdout(t *testing.T) {
	rc := rootConfigInfo{hasStdout: true}
	imports, body := execStubVars(rc, "serve")
	if !strings.Contains(imports, `"fmt"`) {
		t.Errorf("imports: expected fmt import, got %q", imports)
	}
	if !strings.Contains(body, "cfg.Stdout") {
		t.Errorf("body: expected cfg.Stdout, got %q", body)
	}
	if !strings.Contains(body, "serve: not yet implemented") {
		t.Errorf("body: expected cmd name, got %q", body)
	}
}

func TestExecStubVars_logger(t *testing.T) {
	rc := rootConfigInfo{loggerField: "Logger"}
	imports, body := execStubVars(rc, "publish")
	if imports != "" {
		t.Errorf("imports: expected empty, got %q", imports)
	}
	if !strings.Contains(body, "cfg.Logger.Info") {
		t.Errorf("body: expected cfg.Logger.Info, got %q", body)
	}
	if !strings.Contains(body, "publish: not yet implemented") {
		t.Errorf("body: expected cmd name, got %q", body)
	}
}

func TestExecStubVars_unknown(t *testing.T) {
	rc := rootConfigInfo{}
	imports, body := execStubVars(rc, "sync")
	if imports != "" {
		t.Errorf("imports: expected empty, got %q", imports)
	}
	if body != "\treturn nil\n" {
		t.Errorf("body: got %q, want bare return nil", body)
	}
}

// TestRegisterViaAST_rootParent verifies that the AST insertion path produces
// valid Go source when adding a command as a direct child of root.
func TestRegisterViaAST_rootParent(t *testing.T) {
	src := []byte(syntheticDispatcherNoMarkers)
	info, err := probeDispatcherAST("cmd.go", src)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	info.path = "cmd.go"

	importLine := "\t\"example.com/myapp/cmd/serve\"\n"
	callLine := "\tserve.New(r)\n"

	// Simulate the import insertion.
	after := insertAt(src, info.importEndOffset, importLine)

	// The closing ')' should now be at importEndOffset + len(importLine).
	newCloseParen := info.importEndOffset + len(importLine)
	if after[newCloseParen] != ')' {
		t.Errorf("after import insert: byte at %d is %q, want ')'",
			newCloseParen, after[newCloseParen])
	}

	// Simulate the call insertion (shifted by import length).
	callOffset := info.callInsertOffset + len(importLine)
	after = insertAt(after, callOffset, callLine)

	// The result must still parse as valid Go.
	if _, parseErr := probeDispatcherAST("cmd.go", after); parseErr != nil {
		t.Errorf("result does not parse as a valid dispatcher: %v", parseErr)
	}

	content := string(after)
	if !strings.Contains(content, "example.com/myapp/cmd/serve") {
		t.Error("import not found in result")
	}
	if !strings.Contains(content, "serve.New(r)") {
		t.Error("call line not found in result")
	}
}

// TestRegisterViaAST_commandGo mirrors TestRegisterViaAST_rootParent but uses
// "command.go" as the dispatcher filename to verify the filename is irrelevant
// to the AST insertion logic.
func TestRegisterViaAST_commandGo(t *testing.T) {
	src := []byte(syntheticDispatcherNoMarkers)
	info, err := probeDispatcherAST("command.go", src)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	info.path = "command.go"

	importLine := "\t\"example.com/myapp/cmd/deploy\"\n"
	callLine := "\tdeploy.New(r)\n"

	after := insertAt(src, info.importEndOffset, importLine)

	newCloseParen := info.importEndOffset + len(importLine)
	if after[newCloseParen] != ')' {
		t.Errorf("after import insert: byte at %d is %q, want ')'",
			newCloseParen, after[newCloseParen])
	}

	callOffset := info.callInsertOffset + len(importLine)
	after = insertAt(after, callOffset, callLine)

	// Result must parse as a valid dispatcher regardless of the filename.
	if _, parseErr := probeDispatcherAST("command.go", after); parseErr != nil {
		t.Errorf("result does not parse as a valid dispatcher: %v", parseErr)
	}

	content := string(after)
	if !strings.Contains(content, "example.com/myapp/cmd/deploy") {
		t.Error("import not found in result")
	}
	if !strings.Contains(content, "deploy.New(r)") {
		t.Error("call line not found in result")
	}
}
