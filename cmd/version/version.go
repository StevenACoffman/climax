// Package version implements the "version" CLI command.
package version

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
)

const unknown = "unknown"

// Version is the application version string. When built from a tagged release
// or installed via "go install", the Go toolchain embeds the module version
// automatically, and it is read from build info at startup. Override at link
// time only if the auto-detected value is incorrect:
//
//	go build -ldflags "-X 'APP_IMPORT/cmd/version.Version=v1.2.3'"
var Version = "dev"

// Option can be used to customize the Info after it is gathered from the
// environment.
type Option func(i *Info)

// Info provides the version info.
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

// Config holds the configuration for the version command.
type Config struct {
	*root.Config
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

func getBuildInfo() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
}

func getGitVersion(bi *debug.BuildInfo, linkFlagOverride string) string {
	if linkFlagOverride != "dev" && linkFlagOverride != "" {
		return linkFlagOverride
	}
	if bi == nil {
		return ""
	}
	// TODO: remove this when the issue https://github.com/golang/go/issues/29228 is fixed
	if bi.Main.Version == "(devel)" || bi.Main.Version == "" {
		return ""
	}
	return bi.Main.Version
}

func getCommit(bi *debug.BuildInfo) string {
	if revision := getKey(bi, "vcs.revision"); revision != "" {
		return revision
	}
	if bi != nil {
		return bi.Main.Version
	}
	return ""
}

func getDirty(bi *debug.BuildInfo) string {
	modified := getKey(bi, "vcs.modified")
	if modified == "true" {
		return "dirty"
	}
	if modified == "false" {
		return "clean"
	}
	return ""
}

func getBuildDate(bi *debug.BuildInfo) string {
	buildTime := getKey(bi, "vcs.time")
	t, err := time.Parse("2006-01-02T15:04:05Z", buildTime)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02T15:04:05")
}

func getKey(bi *debug.BuildInfo, key string) string {
	if bi == nil {
		return ""
	}
	for _, iter := range bi.Settings {
		if iter.Key == key {
			return iter.Value
		}
	}
	return ""
}

// WithAppDetails allows setting the app name and description.
func WithAppDetails(name, description, url string) Option {
	return func(i *Info) {
		i.Name = name
		i.Description = description
		i.URL = url
	}
}

// WithASCIIName allows you to add an ASCII art of the name.
func WithASCIIName(name string) Option {
	return func(i *Info) {
		i.ASCIIName = name
	}
}

// WithBuiltBy allows to set the builder name/builder system name.
func WithBuiltBy(name string) Option {
	return func(i *Info) {
		i.BuiltBy = name
	}
}

// GetVersionInfo returns build information for the running binary.
func GetVersionInfo(options ...Option) *Info {
	return GetVersionInfoFrom(getBuildInfo(), "", options...)
}

// GetVersionInfoFrom builds an Info from an explicit BuildInfo value.
// Passing nil returns an Info with all VCS fields set to "unknown" and
// current runtime values for GoVersion, Compiler, and Platform.
// This is useful for testing without touching global state.
func GetVersionInfoFrom(bi *debug.BuildInfo, linkOverride string, options ...Option) *Info {
	moduleSum := unknown
	if bi != nil {
		moduleSum = cmp.Or(bi.Main.Sum, unknown)
	}

	i := Info{
		GitVersion:   cmp.Or(getGitVersion(bi, linkOverride), "devel"),
		ModuleSum:    moduleSum,
		GitCommit:    cmp.Or(getCommit(bi), unknown),
		GitTreeState: cmp.Or(getDirty(bi), unknown),
		BuildDate:    cmp.Or(getBuildDate(bi), unknown),
		BuiltBy:      unknown,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	for _, opt := range options {
		opt(&i)
	}
	return &i
}

// String returns the string representation of the version info.
func (i *Info) String() string {
	b := strings.Builder{}
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	if i.Name != "" {
		if i.ASCIIName != "" {
			_, _ = fmt.Fprint(w, i.ASCIIName)
		}
		_, _ = fmt.Fprint(w, i.Name)
		if i.Description != "" {
			_, _ = fmt.Fprintf(w, ": %s", i.Description)
		}
		if i.URL != "" {
			_, _ = fmt.Fprintf(w, "\n%s", i.URL)
		}
		_, _ = fmt.Fprint(w, "\n\n")
	}

	_, _ = fmt.Fprintf(w, "GitVersion:\t%s\n", i.GitVersion)
	_, _ = fmt.Fprintf(w, "GitCommit:\t%s\n", i.GitCommit)
	_, _ = fmt.Fprintf(w, "GitTreeState:\t%s\n", i.GitTreeState)
	_, _ = fmt.Fprintf(w, "BuildDate:\t%s\n", i.BuildDate)
	_, _ = fmt.Fprintf(w, "BuiltBy:\t%s\n", i.BuiltBy)
	_, _ = fmt.Fprintf(w, "GoVersion:\t%s\n", i.GoVersion)
	_, _ = fmt.Fprintf(w, "Compiler:\t%s\n", i.Compiler)
	_, _ = fmt.Fprintf(w, "ModuleSum:\t%s\n", i.ModuleSum)
	_, _ = fmt.Fprintf(w, "Platform:\t%s\n", i.Platform)

	_ = w.Flush()
	return b.String()
}

// JSONString returns the JSON representation of the version info.
func (i *Info) JSONString() (string, error) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling version info: %w", err)
	}
	return string(b), nil
}

// New creates and registers the version command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("version").SetParent(parent.Flags)
	cfg.Flags.BoolVar(&cfg.JSON, 0, "json", "output version information as JSON")
	cfg.Command = &ff.Command{
		Name:      "version",
		Usage:     "climax version [--json]",
		ShortHelp: "print version information",
		LongHelp: `Print build and version information for this climax binary.

Fields shown:

  GitVersion    module version tag (e.g. v0.3.1) or "devel" for local builds
  GitCommit     VCS commit hash
  GitTreeState  "clean" or "dirty" (whether the working tree had uncommitted changes)
  BuildDate     timestamp of the VCS commit used for the build
  BuiltBy       build system name when set via WithBuiltBy (e.g. goreleaser)
  GoVersion     Go toolchain version (e.g. go1.23.0)
  Compiler      Go compiler name (usually "gc")
  ModuleSum     go.sum checksum of the main module
  Platform      GOOS/GOARCH pair (e.g. darwin/arm64)

Use --json to get machine-readable output suitable for scripting.

Environment variable overrides (CLIMAX_ prefix):

  CLIMAX_JSON  --json`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	bi, _ := debug.ReadBuildInfo()
	info := GetVersionInfoFrom(bi, Version)
	if cfg.JSON {
		s, err := info.JSONString()
		if err != nil {
			return fmt.Errorf("version: %w", err)
		}
		_, _ = fmt.Fprintln(cfg.Stdout, s)
		return nil
	}
	_, _ = fmt.Fprint(cfg.Stdout, info.String())
	return nil
}
