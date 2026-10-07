package version_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/cmd/version"
)

// fixtureDir holds the build info of each way of producing a climax binary,
// recorded by bin/capture-buildinfo.sh.
const fixtureDir = "testdata/buildinfo"

const (
	tagCommit = "3efdf089bc50439f68a59e7eddd9f631c782bba1"
	tagDate   = "2026-08-03T02:57:43"
	tagSum    = "h1:/V/mx5lBYxrejteLSTNUxZ1+D1wwTg019N1H75qkvgc="
)

// stampFlag matches one -X assignment to a cmd/version variable in a recorded
// -ldflags build setting.
var stampFlag = regexp.MustCompile(`-X \S+/cmd/version\.(\w+)=(\S+)`)

// ldflagTarget matches a .goreleaser.yaml ldflag that sets a cmd/version variable.
var ldflagTarget = regexp.MustCompile(`-X \{\{ \.ModulePath \}\}/cmd/version\.(\w+)=`)

// fixtureWant is what `version` must report for each recorded build. The
// runtime fields (GoVersion, Compiler, Platform) come from the test binary and
// are not compared.
var fixtureWant = map[string]version.Info{
	"release-proxy": {
		GitVersion: "v0.8.0", Source: version.SourceRelease,
		SourceDetail: "release by goreleaser, from module zip v0.8.0",
		GitCommit:    tagCommit, GitTreeState: "clean", BuildDate: tagDate,
		BuiltBy: "goreleaser", ModuleSum: tagSum,
	},
	"module-tag": {
		GitVersion: "v0.8.0", Source: version.SourceModule,
		SourceDetail: "module zip v0.8.0; commit not embedded, see " +
			"https://proxy.golang.org/github.com/!steven!a!coffman/climax/@v/v0.8.0.info",
		GitCommit: "unknown", GitTreeState: "clean", BuildDate: "unknown",
		BuiltBy: "unknown", ModuleSum: tagSum,
	},
	"module-pseudo": {
		GitVersion: "v0.6.1-0.20260803024314-ef9a082b5468", Source: version.SourceModule,
		SourceDetail: "module zip v0.6.1-0.20260803024314-ef9a082b5468; " +
			"commit and date from its pseudo-version",
		GitCommit: "ef9a082b5468", GitTreeState: "clean", BuildDate: "2026-08-03T02:43:14",
		BuiltBy: "unknown", ModuleSum: "h1:U5AudfOGkfPq/sqVtbW5m8b2to2KTv6+0d3B1alWaR4=",
	},
	"vcs-tag-clean": {
		GitVersion: "v0.8.0", Source: version.SourceVCS, SourceDetail: "git checkout",
		GitCommit: tagCommit, GitTreeState: "clean", BuildDate: tagDate,
		BuiltBy: "unknown", ModuleSum: "unknown",
	},
	"vcs-tag-dirty": {
		GitVersion: "v0.8.0+dirty", Source: version.SourceVCS, SourceDetail: "git checkout",
		GitCommit: tagCommit, GitTreeState: "dirty", BuildDate: tagDate,
		BuiltBy: "unknown", ModuleSum: "unknown",
	},
	"vcs-pseudo": {
		GitVersion: "v0.8.1-0.20260901120000-ef09c4d19254", Source: version.SourceVCS,
		SourceDetail: "git checkout",
		GitCommit:    "ef09c4d192542c5704c5a7707a7aca43b711852d", GitTreeState: "clean",
		BuildDate: "2026-09-01T12:00:00", BuiltBy: "unknown", ModuleSum: "unknown",
	},
	"local-novcs":   localWant,
	"local-tarball": localWant,
}

// localWant is a build from files with no VCS stamp: -buildvcs=false and an
// exported tarball embed the same thing, so they report the same thing.
var localWant = version.Info{
	GitVersion:   "devel",
	Source:       version.SourceLocal,
	SourceDetail: "local source without a VCS stamp (-buildvcs=false, an exported tarball, or no VCS)",
	GitCommit:    "unknown",
	GitTreeState: "unknown",
	BuildDate:    "unknown",
	BuiltBy:      "unknown",
	ModuleSum:    "unknown",
}

// loadFixture parses a recorded build and the link-time stamp its -ldflags set.
func loadFixture(t *testing.T, name string) (*debug.BuildInfo, version.Stamp) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	bi, err := debug.ParseBuildInfo(string(data))
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	stamp := version.Stamp{Version: "dev"}
	for _, s := range bi.Settings {
		if s.Key != "-ldflags" {
			continue
		}
		for _, m := range stampFlag.FindAllStringSubmatch(s.Value, -1) {
			switch m[1] {
			case "Version":
				stamp.Version = m[2]
			case "Commit":
				stamp.Commit = m[2]
			case "CommitDate":
				stamp.CommitDate = m[2]
			case "TreeState":
				stamp.TreeState = m[2]
			case "BuiltBy":
				stamp.BuiltBy = m[2]
			default:
				t.Fatalf("%s: -ldflags sets unknown cmd/version.%s", name, m[1])
			}
		}
	}
	return bi, stamp
}

// withoutRuntime zeroes the fields that describe the running test binary.
func withoutRuntime(i *version.Info) version.Info {
	out := *i
	out.GoVersion, out.Compiler, out.Platform = "", "", ""
	return out
}

// TestGetVersionInfoFrom_recordedBuilds replays every recorded way of building
// climax through GetVersionInfoFrom.
func TestGetVersionInfoFrom_recordedBuilds(t *testing.T) {
	t.Parallel()
	for name, want := range fixtureWant {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bi, stamp := loadFixture(t, name)
			if got := withoutRuntime(version.GetVersionInfoFrom(bi, stamp)); got != want {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

// TestRecordedBuilds_allExpected fails when bin/capture-buildinfo.sh records a
// build that fixtureWant has no expectation for, so a new kind of binary cannot
// arrive untested.
func TestRecordedBuilds_allExpected(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join(fixtureDir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no recorded builds; run bin/capture-buildinfo.sh")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".txt")
		if _, ok := fixtureWant[name]; !ok {
			t.Errorf("%s is recorded but has no entry in fixtureWant", name)
		}
	}
}

// TestGetVersionInfoFrom_stampPrecedence covers what recorded builds do not: a
// link-time value set over a VCS stamp, a malformed one, and no build info.
func TestGetVersionInfoFrom_stampPrecedence(t *testing.T) {
	t.Parallel()
	vcs, _ := loadFixture(t, "vcs-tag-clean")

	got := version.GetVersionInfoFrom(vcs, version.Stamp{
		Version: "v9.9.9", Commit: "abc", CommitDate: "2026-01-02T03:04:05-05:00",
		TreeState: "dirty", BuiltBy: "ci",
	})
	want := version.Info{
		GitVersion: "v9.9.9", Source: version.SourceRelease,
		SourceDetail: "release by ci, from git checkout",
		GitCommit:    "abc", GitTreeState: "dirty", BuildDate: "2026-01-02T08:04:05",
		BuiltBy: "ci", ModuleSum: "unknown",
	}
	if g := withoutRuntime(got); g != want {
		t.Errorf("stamp over VCS:\ngot  %+v\nwant %+v", g, want)
	}

	got = version.GetVersionInfoFrom(vcs, version.Stamp{CommitDate: "yesterday"})
	if got.BuildDate != tagDate {
		t.Errorf(
			"malformed CommitDate: BuildDate = %q, want the VCS stamp %q",
			got.BuildDate,
			tagDate,
		)
	}

	// Found by proptest: a present-but-empty setting must not blank the field.
	got = version.GetVersionInfoFrom(&debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: ""},
	}}, version.Stamp{})
	if got.GitCommit != "unknown" {
		t.Errorf("empty vcs.revision: GitCommit = %q, want %q", got.GitCommit, "unknown")
	}

	got = version.GetVersionInfoFrom(nil, version.Stamp{Version: "dev"})
	if got.Source != version.SourceUnknown || got.GitVersion != "devel" ||
		got.GitCommit != "unknown" {
		t.Errorf("no build info: got %+v", *got)
	}
}

// TestGetVersionInfoFrom_withBuiltByOverridesStamp keeps the Option able to
// override a link-time value, since options are applied last.
func TestGetVersionInfoFrom_withBuiltByOverridesStamp(t *testing.T) {
	t.Parallel()
	got := version.GetVersionInfoFrom(nil, version.Stamp{BuiltBy: "goreleaser"},
		version.WithBuiltBy("make"))
	if got.BuiltBy != "make" {
		t.Errorf("BuiltBy = %q, want %q", got.BuiltBy, "make")
	}
}

// TestReleaseLdflags_reachVersionOutput builds climax with every -X target
// .goreleaser.yaml names and checks each value comes out of `climax version`.
// The linker silently ignores -X for a variable that does not exist, so a
// renamed variable or a typo in the release config would otherwise ship a
// binary that reports "unknown" with nothing failing.
func TestReleaseLdflags_reachVersionOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the climax binary")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not available:", err)
	}
	cfg, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Each target gets a recognizable value and the JSON field it must reach.
	probes := map[string]struct{ value, field, want string }{
		"Version":    {"v9.9.9", "gitVersion", "v9.9.9"},
		"Commit":     {"deadbeef", "gitCommit", "deadbeef"},
		"CommitDate": {"2026-01-02T03:04:05Z", "buildDate", "2026-01-02T03:04:05"},
		"TreeState":  {"dirty", "gitTreeState", "dirty"},
		"BuiltBy":    {"probe", "builtBy", "probe"},
	}
	matches := ldflagTarget.FindAllStringSubmatch(string(cfg), -1)
	if len(matches) == 0 {
		t.Fatal("found no -X {{ .ModulePath }}/cmd/version.* ldflags in .goreleaser.yaml")
	}
	var ldflags []string
	for _, m := range matches {
		p, ok := probes[m[1]]
		if !ok {
			t.Fatalf(".goreleaser.yaml sets cmd/version.%s, which this test has no probe for", m[1])
		}
		ldflags = append(ldflags,
			"-X github.com/StevenACoffman/climax/cmd/version."+m[1]+"="+p.value)
	}

	bin := filepath.Join(t.TempDir(), "climax")
	//nolint:gosec // G204: runs the go tool with flags this test built
	build := exec.CommandContext(context.Background(), goTool, "build",
		"-ldflags", strings.Join(ldflags, " "), "-o", bin, "../..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	//nolint:gosec // G204: runs the binary this test just built
	out, err := exec.CommandContext(context.Background(), bin, "version", "--json").Output()
	if err != nil {
		t.Fatalf("climax version --json: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decoding %s: %v", out, err)
	}
	for _, m := range matches {
		p := probes[m[1]]
		if got[p.field] != p.want {
			t.Errorf("-X cmd/version.%s=%s: %s = %q, want %q",
				m[1], p.value, p.field, got[p.field], p.want)
		}
	}
}

// TestGetVersionInfoFrom_pseudoVersions checks every pseudo-version form yields
// its commit and time, and that versions merely shaped like one do not.
func TestGetVersionInfoFrom_pseudoVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version, commit, date string
	}{
		{"v0.0.0-20260803025743-3efdf089bc50", "3efdf089bc50", "2026-08-03T02:57:43"},
		{"v1.2.4-0.20260803025743-3efdf089bc50", "3efdf089bc50", "2026-08-03T02:57:43"},
		{"v1.2.3-rc.1.0.20260803025743-3efdf089bc50", "3efdf089bc50", "2026-08-03T02:57:43"},
		{"v2.0.0-20260803025743-3efdf089bc50+incompatible", "3efdf089bc50", "2026-08-03T02:57:43"},
		{"v0.8.1-0.20260803025743-3efdf089bc50+dirty", "3efdf089bc50", "2026-08-03T02:57:43"},
		// Not pseudo-versions: a plain pre-release, and a timestamp-shaped
		// pre-release on a version that is neither vX.0.0 nor "-0." based.
		{"v1.2.3-rc.1", "unknown", "unknown"},
		{"v1.2.3-20260803025743-3efdf089bc50", "unknown", "unknown"},
		// Fourteen digits that are not a valid time.
		{"v0.0.0-20261399999999-3efdf089bc50", "unknown", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			got := version.GetVersionInfoFrom(&debug.BuildInfo{
				Main: debug.Module{Path: "example.com/m", Version: tc.version, Sum: "h1:x="},
			}, version.Stamp{})
			if got.GitCommit != tc.commit || got.BuildDate != tc.date {
				t.Errorf("GitCommit, BuildDate = %q, %q; want %q, %q",
					got.GitCommit, got.BuildDate, tc.commit, tc.date)
			}
		})
	}
}
