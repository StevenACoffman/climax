package version_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/StevenACoffman/climax/pkg/version"
)

// TestGetVersionInfo_string verifies that GetVersionInfo returns a non-empty
// Info and that String() includes all expected field names.
func TestGetVersionInfo_string(t *testing.T) {
	info := version.GetVersionInfo()
	got := info.String()
	if got == "" {
		t.Fatal("String() must not be empty")
	}
	for _, field := range []string{
		"GitVersion:", "GitCommit:", "GitTreeState:",
		"BuildDate:", "GoVersion:", "Platform:",
	} {
		if !strings.Contains(got, field) {
			t.Errorf("String() missing field %q\nfull output:\n%s", field, got)
		}
	}
}

// TestGetVersionInfo_json verifies that JSONString() produces valid, non-empty JSON.
func TestGetVersionInfo_json(t *testing.T) {
	info := version.GetVersionInfo()
	got, err := info.JSONString()
	if err != nil {
		t.Fatalf("JSONString() returned error: %v", err)
	}
	if got == "" {
		t.Fatal("JSONString() must not be empty")
	}
}

// TestGetVersionInfoFrom_nilBuildInfo verifies that a nil BuildInfo produces
// a valid Info with default field values.
func TestGetVersionInfoFrom_nilBuildInfo(t *testing.T) {
	info := version.GetVersionInfoFrom(nil)
	if info == nil {
		t.Fatal("expected non-nil Info for nil BuildInfo")
	}
	if info.GitVersion != "devel" {
		t.Errorf("GitVersion: got %q, want %q", info.GitVersion, "devel")
	}
	if info.GitCommit != "unknown" {
		t.Errorf("GitCommit: got %q, want %q", info.GitCommit, "unknown")
	}
}

// TestGetVersionInfoFrom_gitVersion tests version extraction from BuildInfo.
func TestGetVersionInfoFrom_gitVersion(t *testing.T) {
	cases := []struct {
		rawVersion string
		want       string
	}{
		{"(devel)", "devel"},
		{"", "devel"},
		{"v1.2.3", "v1.2.3"},
	}
	for _, tc := range cases {
		info := version.GetVersionInfoFrom(&debug.BuildInfo{
			Main: debug.Module{Version: tc.rawVersion},
		})
		if info.GitVersion != tc.want {
			t.Errorf("rawVersion=%q: got GitVersion %q, want %q",
				tc.rawVersion, info.GitVersion, tc.want)
		}
	}
}

// TestGetVersionInfoFrom_gitTreeState tests dirty/clean detection.
func TestGetVersionInfoFrom_gitTreeState(t *testing.T) {
	cases := []struct {
		modified string
		want     string
	}{
		{"true", "dirty"},
		{"false", "clean"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		settings := []debug.BuildSetting{}
		if tc.modified != "" {
			settings = append(settings, debug.BuildSetting{Key: "vcs.modified", Value: tc.modified})
		}
		info := version.GetVersionInfoFrom(&debug.BuildInfo{Settings: settings})
		if info.GitTreeState != tc.want {
			t.Errorf("vcs.modified=%q: got GitTreeState %q, want %q",
				tc.modified, info.GitTreeState, tc.want)
		}
	}
}

// TestGetVersionInfoFrom_buildDate tests build date parsing.
func TestGetVersionInfoFrom_buildDate(t *testing.T) {
	cases := []struct {
		vcsTime string
		want    string
	}{
		{"", "unknown"},
		{"not a date", "unknown"},
		{"2024-01-15T10:30:00Z", "2024-01-15T10:30:00"},
	}
	for _, tc := range cases {
		settings := []debug.BuildSetting{}
		if tc.vcsTime != "" {
			settings = append(settings, debug.BuildSetting{Key: "vcs.time", Value: tc.vcsTime})
		}
		info := version.GetVersionInfoFrom(&debug.BuildInfo{Settings: settings})
		if info.BuildDate != tc.want {
			t.Errorf("vcs.time=%q: got BuildDate %q, want %q", tc.vcsTime, info.BuildDate, tc.want)
		}
	}
}

// TestGetVersionInfoFrom_gitCommit tests commit hash extraction.
func TestGetVersionInfoFrom_gitCommit(t *testing.T) {
	const sha = "abc1234def5678"
	info := version.GetVersionInfoFrom(&debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: sha},
		},
	})
	if info.GitCommit != sha {
		t.Errorf("got GitCommit %q, want %q", info.GitCommit, sha)
	}
}

// TestGetVersionInfoFrom_options verifies that functional options are applied.
func TestGetVersionInfoFrom_options(t *testing.T) {
	const art = " _\n|_|\n"
	info := version.GetVersionInfoFrom(nil,
		version.WithBuiltBy("goreleaser"),
		version.WithAppDetails("myapp", "a useful tool", "https://example.com"),
		version.WithASCIIName(art),
	)
	if info.BuiltBy != "goreleaser" {
		t.Errorf("BuiltBy: got %q, want %q", info.BuiltBy, "goreleaser")
	}
	if info.Name != "myapp" {
		t.Errorf("Name: got %q, want %q", info.Name, "myapp")
	}
	if info.ASCIIName != art {
		t.Errorf("ASCIIName: got %q, want %q", info.ASCIIName, art)
	}
	// String() output should include the ASCII art and app name.
	got := info.String()
	if !strings.Contains(got, "myapp") {
		t.Errorf("String() missing app name\noutput:\n%s", got)
	}
}
