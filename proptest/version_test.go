package proptest_test

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/climax/cmd/version"
)

// buildInfo draws the parts of a debug.BuildInfo that GetVersionInfoFrom
// reads — a module-cache build (sum, tag or pseudo-version), a checkout
// (vcs.* settings), or neither — including values it has to reject (a
// malformed vcs.time, an unknown vcs.modified) as well as ones it keeps.
var buildInfo = rapid.Custom(func(t *rapid.T) *debug.BuildInfo {
	if rapid.IntRange(0, 9).Draw(t, "nilInfo") == 0 {
		return nil
	}
	bi := &debug.BuildInfo{Main: debug.Module{
		Path: "github.com/Example/App",
		Version: rapid.SampledFrom([]string{
			"", "(devel)", "v1.2.3", "v1.2.3+dirty",
			"v0.0.0-20260101120000-abcdefabcdef", "v1.2.4-0.20260101120000-abcdefabcdef",
		}).Draw(t, "mainVersion"),
		Sum: rapid.SampledFrom([]string{"", "h1:abc="}).Draw(t, "mainSum"),
	}}
	settings := map[string]*rapid.Generator[string]{
		"vcs":          rapid.Just("git"),
		"vcs.revision": rapid.StringMatching(`[0-9a-f]{0,12}`),
		"vcs.modified": rapid.SampledFrom([]string{"true", "false", "", "maybe"}),
		"vcs.time":     rapid.SampledFrom([]string{"2026-10-06T12:00:00Z", "yesterday", ""}),
	}
	for _, key := range []string{"vcs", "vcs.revision", "vcs.modified", "vcs.time"} {
		if rapid.Bool().Draw(t, "has "+key) {
			bi.Settings = append(bi.Settings,
				debug.BuildSetting{Key: key, Value: settings[key].Draw(t, key)})
		}
	}
	return bi
})

// stamp draws the link-time values a release build injects, each possibly
// unset, and a CommitDate that may not parse.
var stamp = rapid.Custom(func(t *rapid.T) version.Stamp {
	return version.Stamp{
		Version: rapid.SampledFrom([]string{"", "dev", "v9.9.9"}).Draw(t, "Version"),
		Commit:  rapid.SampledFrom([]string{"", "deadbeef"}).Draw(t, "Commit"),
		CommitDate: rapid.SampledFrom([]string{"", "2026-01-02T03:04:05Z", "soon"}).
			Draw(t, "CommitDate"),
		TreeState: rapid.SampledFrom([]string{"", "dirty"}).Draw(t, "TreeState"),
		BuiltBy:   rapid.SampledFrom([]string{"", "goreleaser"}).Draw(t, "BuiltBy"),
	}
})

// TestVersion_info checks what `version` prints for any build: every reported
// field is filled (never blank — "unknown" stands in for missing data), Source
// is one of the documented values, every link-time value that is set wins, the
// JSON form decodes back to the same values, and the text form shows each.
func TestVersion_info(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bi := buildInfo.Draw(t, "buildInfo")
		link := stamp.Draw(t, "stamp")
		info := version.GetVersionInfoFrom(bi, link)

		assertFieldsShown(t, info)
		switch info.Source {
		case version.SourceRelease, version.SourceModule, version.SourceVCS,
			version.SourceLocal, version.SourceUnknown:
		default:
			t.Errorf("Source = %q, not a documented value", info.Source)
		}
		assertStampWins(t, &link, info)

		js, err := info.JSONString()
		if err != nil {
			t.Fatalf("JSONString: %v", err)
		}
		var back version.Info
		if err := json.Unmarshal([]byte(js), &back); err != nil {
			t.Fatalf("JSONString output does not decode: %v\n%s", err, js)
		}
		if back != *info {
			t.Fatalf("JSON round trip changed the info:\n got %+v\nwant %+v", back, *info)
		}
	})
}

// assertStampWins fails if a link-time value that is set did not override
// whatever the build info said.
func assertStampWins(t *rapid.T, link *version.Stamp, info *version.Info) {
	t.Helper()
	if link.Version == "v9.9.9" && info.GitVersion != link.Version {
		t.Errorf("GitVersion = %q, want the link value %q", info.GitVersion, link.Version)
	}
	if link.Commit != "" && info.GitCommit != link.Commit {
		t.Errorf("GitCommit = %q, want the link value %q", info.GitCommit, link.Commit)
	}
	if link.TreeState != "" && info.GitTreeState != link.TreeState {
		t.Errorf("GitTreeState = %q, want the link value %q", info.GitTreeState, link.TreeState)
	}
	if link.CommitDate == "2026-01-02T03:04:05Z" && info.BuildDate != "2026-01-02T03:04:05" {
		t.Errorf("BuildDate = %q, want the link value", info.BuildDate)
	}
	if link.BuiltBy != "" && info.BuiltBy != link.BuiltBy {
		t.Errorf("BuiltBy = %q, want the link value %q", info.BuiltBy, link.BuiltBy)
	}
}

// assertFieldsShown fails if any reported field is blank, or missing from the
// text form `version` prints.
func assertFieldsShown(t *rapid.T, info *version.Info) {
	t.Helper()
	fields := map[string]string{
		"GitVersion": info.GitVersion, "Source": info.SourceDetail,
		"GitCommit": info.GitCommit, "GitTreeState": info.GitTreeState,
		"BuildDate": info.BuildDate, "BuiltBy": info.BuiltBy,
		"GoVersion": info.GoVersion, "Compiler": info.Compiler,
		"ModuleSum": info.ModuleSum, "Platform": info.Platform,
	}
	text := info.String()
	for name, v := range fields {
		if v == "" {
			t.Errorf("%s is empty", name)
		}
		if !strings.Contains(text, name+":") || !strings.Contains(text, v) {
			t.Errorf("String() does not show %s=%q:\n%s", name, v, text)
		}
	}
}
