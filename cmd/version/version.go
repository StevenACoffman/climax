// Package version implements the "version" CLI command.
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/climax/cmd/root"
)

const unknown = "unknown"

// Source values: where the binary's source came from, as far as the build
// recorded it. Reported in JSON as "source"; the text form shows SourceDetail.
const (
	// SourceRelease is a build with link-time release values (e.g. goreleaser).
	SourceRelease = "release"
	// SourceModule is a build from the module cache: go install/run pkg@version.
	SourceModule = "module"
	// SourceVCS is a build in a VCS checkout, with the toolchain's VCS stamp.
	SourceVCS = "vcs"
	// SourceLocal is a build from local files with no VCS stamp.
	SourceLocal = "local"
	// SourceUnknown is a binary with no module build info at all.
	SourceUnknown = "unknown"
)

// Version is the application version string. When built from a tagged release
// or installed via "go install", the Go toolchain embeds the module version
// automatically, and it is read from build info at startup. Override at link
// time only if the auto-detected value is incorrect:
//
//	go build -ldflags "-X 'github.com/StevenACoffman/climax/cmd/version.Version=v1.2.3'"
var Version = "dev"

// Release metadata injected at link time, for example by goreleaser:
//
//	-X github.com/StevenACoffman/climax/cmd/version.Commit={{ .FullCommit }}
//	-X github.com/StevenACoffman/climax/cmd/version.CommitDate={{ .CommitDate }}
//	-X github.com/StevenACoffman/climax/cmd/version.TreeState={{ .GitTreeState }}
//	-X github.com/StevenACoffman/climax/cmd/version.BuiltBy=goreleaser
//
// A build through the module proxy (goreleaser's gomod.proxy, or go install
// pkg@version) carries no VCS stamps in its build info, so these are the only
// way such a binary can report its commit. A non-empty value wins over build
// info; empty defers to it. The linker silently ignores -X for a variable that
// does not exist, so renaming one of these breaks the release without an error.
var (
	Commit     string
	CommitDate string
	TreeState  string
	BuiltBy    string
)

// pseudoVersion matches a Go module pseudo-version; the groups are the commit
// time and the revision. See golang.org/x/mod/module.IsPseudoVersion.
var pseudoVersion = regexp.MustCompile(
	`^v[0-9]+\.(?:0\.0-|[0-9]+\.[0-9]+-(?:[^+]*\.)?0\.)([0-9]{14})-([A-Za-z0-9]+)(?:\+[0-9A-Za-z.-]+)?$`,
)

// Option can be used to customize the Info after it is gathered from the
// environment.
type Option func(i *Info)

// Info holds build and VCS metadata for structured output.
type Info struct {
	GitVersion   string `json:"gitVersion"`
	Source       string `json:"source"`
	SourceDetail string `json:"sourceDetail"`
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

// Stamp holds the values injected at link time with -ldflags -X. An empty field,
// or a Version still at "dev", defers to the build info.
type Stamp struct {
	Version    string
	Commit     string
	CommitDate string
	TreeState  string
	BuiltBy    string
}

// Config holds the configuration for the version command.
type Config struct {
	*root.Config
	JSON    bool
	Flags   *ff.FlagSet
	Command *ff.Command
}

// LinkStamp returns the values this binary was linked with.
func LinkStamp() Stamp {
	return Stamp{
		Version:    Version,
		Commit:     Commit,
		CommitDate: CommitDate,
		TreeState:  TreeState,
		BuiltBy:    BuiltBy,
	}
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
  Source        where the source came from: a release, a module zip, a VCS
                checkout, or local files (JSON "source": release, module,
                vcs, local, unknown)
  GitCommit     VCS commit hash
  GitTreeState  "clean" or "dirty" (whether the working tree had uncommitted changes);
                "clean" for a module-proxy build, whose source is the tagged module
  BuildDate     timestamp of the VCS commit used for the build
  BuiltBy       build system that produced the binary (e.g. goreleaser), if it said
  GoVersion     Go toolchain version (e.g. go1.23.0)
  Compiler      Go compiler name (usually "gc")
  ModuleSum     go.sum checksum of the main module
  Platform      GOOS/GOARCH pair (e.g. darwin/arm64)

GitCommit, GitTreeState, and BuildDate come from the release's link-time
values when present, otherwise from the Go toolchain's VCS stamp, otherwise
from a pseudo-version. A "go install <module>@<tag>" build has none of these,
so GitCommit and BuildDate read "unknown"; Source says where to look them up.

Use --json to get machine-readable output suitable for scripting.

Environment variable overrides (CLIMAX_ prefix):

  CLIMAX_JSON  --json`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
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
	_, _ = fmt.Fprintf(w, "Source:\t%s\n", i.SourceDetail)
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

// GetVersionInfoFrom builds an Info from an explicit BuildInfo value and link
// stamp. Each field takes the stamp's value when set, then the build info's,
// then what a pseudo-version encodes, then "unknown". Passing nil and a zero
// Stamp returns an Info with all VCS fields set to "unknown" and current
// runtime values for GoVersion, Compiler, and Platform. This is useful for
// testing without touching global state.
//
//nolint:gocritic // hugeParam: Stamp is a small value set once at link time; by value keeps callers simple
func GetVersionInfoFrom(bi *debug.BuildInfo, link Stamp, options ...Option) *Info {
	i := gatherVersionInfo(bi, link)
	for _, opt := range options {
		opt(&i)
	}
	return &i
}

//nolint:gocritic // hugeParam: see GetVersionInfoFrom
func gatherVersionInfo(bi *debug.BuildInfo, link Stamp) Info {
	info := Info{
		GitVersion:   "devel",
		ModuleSum:    unknown,
		GitCommit:    unknown,
		GitTreeState: unknown,
		BuildDate:    unknown,
		BuiltBy:      unknown,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	if bi != nil {
		applyBuildInfo(&info, bi)
	}
	// Link-time values were set deliberately for this binary, so they win.
	if link.Version != "" && link.Version != "dev" {
		info.GitVersion = link.Version
	}
	if link.Commit != "" {
		info.GitCommit = link.Commit
	}
	if link.TreeState != "" {
		info.GitTreeState = link.TreeState
	}
	if d := formatCommitDate(link.CommitDate); d != "" {
		info.BuildDate = d
	}
	if link.BuiltBy != "" {
		info.BuiltBy = link.BuiltBy
	}
	info.Source, info.SourceDetail = describeSource(bi, link)
	return info
}

// applyBuildInfo fills info from the Go toolchain's embedded build info.
func applyBuildInfo(info *Info, bi *debug.BuildInfo) {
	if bi.Main.Sum != "" {
		info.ModuleSum = bi.Main.Sum
		// A main module built from the module cache (go install/run pkg@version,
		// goreleaser's gomod.proxy) has no working tree, but it is the module zip
		// for that version, checked against ModuleSum, and pkg@version refuses
		// replace directives: the source is exactly the tree at that version.
		info.GitTreeState = "clean"
	}
	// A build with no VCS stamp (-buildvcs=false, or no .git) reports "(devel)".
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.GitVersion = bi.Main.Version
	}
	applyVCSStamp(info, bi.Settings)
	// `go install pkg@<commit>` has no VCS stamp, but its pseudo-version names
	// the commit (a 12-character prefix) and the commit's time.
	if rev, at, ok := parsePseudoVersion(bi.Main.Version); ok {
		if info.GitCommit == unknown {
			info.GitCommit = rev
		}
		if info.BuildDate == unknown {
			info.BuildDate = at.Format("2006-01-02T15:04:05")
		}
	}
}

// applyVCSStamp fills info from the vcs.* settings `go build` records in a
// checkout.
func applyVCSStamp(info *Info, settings []debug.BuildSetting) {
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			if s.Value != "" {
				info.GitCommit = s.Value
			}
		case "vcs.modified":
			switch s.Value {
			case "true":
				info.GitTreeState = "dirty"
			case "false":
				info.GitTreeState = "clean"
			}
		case "vcs.time":
			if d := formatCommitDate(s.Value); d != "" {
				info.BuildDate = d
			}
		}
	}
}

// parsePseudoVersion extracts the revision and commit time from a Go module
// pseudo-version, in any of its three forms:
//
//	vX.0.0-yyyymmddhhmmss-abcdefabcdef
//	vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef
//	vX.Y.(Z+1)-0.yyyymmddhhmmss-abcdefabcdef
//
// with an optional build suffix such as +dirty or +incompatible.
func parsePseudoVersion(v string) (rev string, at time.Time, ok bool) {
	m := pseudoVersion.FindStringSubmatch(v)
	if m == nil {
		return "", time.Time{}, false
	}
	at, err := time.Parse("20060102150405", m[1])
	if err != nil {
		return "", time.Time{}, false
	}
	return m[2], at, true
}

// formatCommitDate normalizes an RFC 3339 timestamp — vcs.time, or goreleaser's
// {{ .CommitDate }} — to UTC without a zone suffix. It returns "" for anything
// that does not parse, so a malformed value reads as unknown.
func formatCommitDate(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05")
}

// describeSource classifies where the binary's source came from, from what the
// build embedded: a module sum means the module cache, vcs.* settings mean a
// checkout, neither means local files with no VCS stamp. A link-time stamp
// makes it a release build, though nothing verifies that claim.
//
//nolint:gocritic // hugeParam: see GetVersionInfoFrom
func describeSource(bi *debug.BuildInfo, link Stamp) (source, detail string) {
	var from string
	switch {
	case bi == nil:
		return SourceUnknown, "no module build info embedded"
	case bi.Main.Sum != "":
		source = SourceModule
		from = "module zip " + bi.Main.Version
		if _, _, ok := parsePseudoVersion(bi.Main.Version); ok {
			detail = from + "; commit and date from its pseudo-version"
		} else {
			detail = from + "; commit not embedded, see " +
				proxyInfoURL(bi.Main.Path, bi.Main.Version)
		}
	case buildSetting(bi, "vcs") != "":
		source = SourceVCS
		from = buildSetting(bi, "vcs") + " checkout"
		detail = from
	default:
		source = SourceLocal
		from = "local source"
		detail = "local source without a VCS stamp (-buildvcs=false, an exported tarball, or no VCS)"
	}
	if link.Commit == "" && link.CommitDate == "" && link.TreeState == "" && link.BuiltBy == "" {
		return source, detail
	}
	by := "link-time stamp"
	if link.BuiltBy != "" {
		by = link.BuiltBy
	}
	return SourceRelease, "release by " + by + ", from " + from
}

// buildSetting returns the value of the build setting key, or "".
func buildSetting(bi *debug.BuildInfo, key string) string {
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// proxyInfoURL is where proxy.golang.org records a version's commit hash and
// time, for a published module. Upper-case letters are escaped as "!" plus the
// lower-case letter, as the module proxy protocol requires.
func proxyInfoURL(path, version string) string {
	escape := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if 'A' <= r && r <= 'Z' {
				b.WriteByte('!')
				r += 'a' - 'A'
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	return "https://proxy.golang.org/" + escape(path) + "/@v/" + escape(version) + ".info"
}

func (cfg *Config) exec(_ context.Context, _ []string) error {
	bi, _ := debug.ReadBuildInfo()
	info := GetVersionInfoFrom(bi, LinkStamp())
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
