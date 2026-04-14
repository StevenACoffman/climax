package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyFileForTest copies a file from src to dst, creating parent directories.
// The original file is never modified.
func copyFileForTest(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyFileForTest: reading %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("copyFileForTest: mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("copyFileForTest: writing %s: %v", dst, err)
	}
}

// setupClimaxSourceFiles copies all source files that DetectDrift reads into
// tmpDir, mirroring the structure expected by DetectDrift. Templates are NOT
// copied because DetectDrift uses embedded template strings for comparison;
// only ApplyFixes reads template files from disk (use setupTemplateFiles for that).
//
// The real climax root is derived from the test package location: tests in
// pkg/scaffold run with the working directory set to that package, so "../.."
// resolves to the module root.
func setupClimaxSourceFiles(t *testing.T, tmpDir string) {
	t.Helper()
	const realRoot = "../.."
	for _, rel := range []string{
		"main.go",
		"cmd/cmd.go",
		"cmd/root/root.go",
		"cmd/version/version.go",
		"cmd/mango/mango.go",
	} {
		copyFileForTest(t, filepath.Join(realRoot, rel), filepath.Join(tmpDir, rel))
	}
}

// setupTemplateFiles copies all scaffold template files into
// tmpDir/pkg/scaffold/templates/ so that ApplyFixes can write to them.
func setupTemplateFiles(t *testing.T, tmpDir string) {
	t.Helper()
	const realRoot = "../.."
	for _, name := range []string{
		"main.go.tmpl",
		"cmd.go.tmpl",
		"root.go.tmpl",
		"version.go.tmpl",
		"man.go.tmpl",
		"newcmd.go.tmpl",
	} {
		src := filepath.Join(realRoot, "pkg", "scaffold", "templates", name)
		dst := filepath.Join(tmpDir, "pkg", "scaffold", "templates", name)
		copyFileForTest(t, src, dst)
	}
}

// syntheticSourceInfoAllPresent returns a sourceInfo with all boolean fields
// set to true, corresponding to the current state of the real climax source
// where every structural property is present in both source and templates.
// Update this function whenever new fields are added to sourceInfo.
func syntheticSourceInfoAllPresent() sourceInfo {
	return sourceInfo{
		mainHasSignal:                        true,
		mainHasRunFunc:                       true,
		mainPassesStdin:                      true,
		cmdHasStdinParam:                     true,
		cmdPassesStdin:                       true,
		cmdHasEnvPrefix:                      true,
		rootHasStdinField:                    true,
		rootHasStdinParam:                    true,
		rootAssignsStdin:                     true,
		versionHasJSONFlag:                   true,
		versionHasTabwriter:                  true,
		versionHasGetVersionInfoFrom:         true,
		versionInfoMethodsUsePointerReceiver: true,
		versionHasOptionType:                 true,
		versionHasOptionConstructors:         true,
		mangoHasSectionField:                 true,
	}
}

// ─── runChecks unit tests ─────────────────────────────────────────────────────

// TestRunChecks_noSourceNoDrift verifies that runChecks returns no items when
// all source properties are absent and templates also lack them (both false).
func TestRunChecks_noSourceNoDrift(t *testing.T) {
	items := runChecks(sourceInfo{}, &templateSet{})
	if len(items) != 0 {
		t.Errorf("expected no drift items for empty source and templates, got %d", len(items))
	}
}

// TestRunChecks_allInSync_noDrift verifies that when source and templates are
// fully in sync (all properties present in both), no drift is reported.
func TestRunChecks_allInSync_noDrift(t *testing.T) {
	tmpl := defaultTemplates()
	items := runChecks(syntheticSourceInfoAllPresent(), &tmpl)
	if len(items) != 0 {
		for _, item := range items {
			t.Logf("unexpected drift: template=%s property=%q inSource=%s inTemplate=%s",
				item.Template, item.Property, item.InSource, item.InTemplate)
		}
		t.Errorf("expected no drift, got %d item(s)", len(items))
	}
}

// TestRunChecks_versionJSON_sourceHasTemplateLacks verifies that when the
// source has the JSON flag but the template does not, a fixable drift item is
// produced with a patch attached.
func TestRunChecks_versionJSON_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	// Remove JSON flag from the version template.
	tmpl := defaultTemplates()
	tmpl.version = strings.Replace(versionTemplate, "\tJSON    bool\n", "", 1)
	if tmpl.version == versionTemplate {
		t.Fatal("strings.Replace had no effect – check exact tab/spacing in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "JSON flag in Config" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'JSON flag in Config' drift item, not found")
	}
	if found.InSource != "present" {
		t.Errorf("InSource: got %q, want %q", found.InSource, "present")
	}
	if found.InTemplate != "absent" {
		t.Errorf("InTemplate: got %q, want %q", found.InTemplate, "absent")
	}
	if found.patch == nil {
		t.Error("expected patch to be set for auto-fixable drift (source has, template lacks)")
	}
}

// TestRunChecks_versionJSON_templateHasSourceLacks verifies that when the
// template has the JSON flag but the source does not, a manual-review drift
// item is produced without a patch.
func TestRunChecks_versionJSON_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionHasJSONFlag = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "JSON flag in Config" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'JSON flag in Config' drift item, not found")
	}
	if found.InSource != "absent" {
		t.Errorf("InSource: got %q, want %q", found.InSource, "absent")
	}
	if found.InTemplate != "present" {
		t.Errorf("InTemplate: got %q, want %q", found.InTemplate, "present")
	}
	// No patch: removing things from templates is a manual decision.
	if found.patch != nil {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_versionTabwriter_sourceHasTemplateLacks verifies tabwriter
// drift detection between version source and template.
func TestRunChecks_versionTabwriter_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = strings.Replace(versionTemplate, "tabwriter.NewWriter", "/* removed */", 1)
	if tmpl.version == versionTemplate {
		t.Fatal("strings.Replace had no effect – check tabwriter.NewWriter in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "tabwriter output" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'tabwriter output' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong presence: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	// tabwriter drift has no auto-fix patch.
	if found.patch != nil {
		t.Error("expected no patch for tabwriter drift (no auto-fix defined)")
	}
}

// TestRunChecks_versionGetVersionInfoFrom_sourceHasTemplateLacks verifies that
// when the source has GetVersionInfoFrom but the template does not, a drift
// item is produced (no auto-fix patch because the change requires restructuring
// the whole info pattern).
func TestRunChecks_versionGetVersionInfoFrom_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = strings.Replace(
		versionTemplate,
		"func GetVersionInfoFrom(",
		"func _removed_(",
		1,
	)
	if tmpl.version == versionTemplate {
		t.Fatal("strings.Replace had no effect – GetVersionInfoFrom not found in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "GetVersionInfoFrom function" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'GetVersionInfoFrom function' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
}

// TestRunChecks_versionGetVersionInfoFrom_templateHasSourceLacks verifies that
// when the template has GetVersionInfoFrom but the source does not, a
// manual-review drift item (no patch) is produced.
func TestRunChecks_versionGetVersionInfoFrom_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionHasGetVersionInfoFrom = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "GetVersionInfoFrom function" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'GetVersionInfoFrom function' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.patch != nil {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_versionPointerReceiver_sourceHasTemplateLacks verifies that
// when the source uses pointer receivers for Info methods but the template uses
// value receivers, a fixable drift item with a patch is produced.
func TestRunChecks_versionPointerReceiver_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	// Simulate an outdated template with value receivers.
	tmpl.version = strings.NewReplacer(
		"func (i *Info) String()", "func (i Info) String()",
		"func (i *Info) JSONString()", "func (i Info) JSONString()",
	).Replace(versionTemplate)
	if tmpl.version == versionTemplate {
		t.Fatal("replacement had no effect – check pointer receiver strings in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" &&
			items[i].Property == "Info methods use pointer receivers" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'Info methods use pointer receivers' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.patch == nil {
		t.Error("expected patch for pointer-receiver drift (auto-fixable)")
	}
}

// TestApplyFixes_versionPointerReceiver verifies that ApplyFixes restores
// pointer receivers in version.go.tmpl when the file has value receivers.
func TestApplyFixes_versionPointerReceiver(t *testing.T) {
	tmpDir := t.TempDir()
	tmplDir := filepath.Join(tmpDir, "pkg", "scaffold", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Simulate an outdated template with value receivers.
	withValueReceivers := strings.NewReplacer(
		"func (i *Info) String()", "func (i Info) String()",
		"func (i *Info) JSONString()", "func (i Info) JSONString()",
	).Replace(versionTemplate)
	if withValueReceivers == versionTemplate {
		t.Fatal("replacement had no effect – check pointer receiver strings in versionTemplate")
	}
	if err := os.WriteFile(
		filepath.Join(tmplDir, "version.go.tmpl"),
		[]byte(withValueReceivers),
		0o644,
	); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	// Derive drift items from runChecks.
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = withValueReceivers
	items := runChecks(src, &tmpl)

	var found bool
	for _, item := range items {
		if item.Template == "version" && item.Property == "Info methods use pointer receivers" {
			found = true
		}
	}
	if !found {
		t.Fatal("runChecks did not produce pointer-receiver drift item – cannot test ApplyFixes")
	}

	if err := ApplyFixes(tmpDir, items); err != nil {
		t.Fatalf("ApplyFixes: %v", err)
	}

	result, err := os.ReadFile(filepath.Join(tmplDir, "version.go.tmpl"))
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	body := string(result)
	if !strings.Contains(body, "func (i *Info) String()") {
		t.Error("ApplyFixes did not restore pointer receiver for String()")
	}
	if !strings.Contains(body, "func (i *Info) JSONString()") {
		t.Error("ApplyFixes did not restore pointer receiver for JSONString()")
	}
}

// TestRunChecks_versionOptionType_sourceHasTemplateLacks verifies that when
// the source declares "type Option func(i *Info)" but the template does not,
// a drift item is produced (no auto-fix patch; requires structural change).
func TestRunChecks_versionOptionType_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = strings.Replace(
		versionTemplate,
		"type Option func(",
		"type _removed_option func(",
		1,
	)
	if tmpl.version == versionTemplate {
		t.Fatal("strings.Replace had no effect – type Option not found in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "Option type" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'Option type' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
}

// TestRunChecks_versionOptionConstructors_sourceHasTemplateLacks verifies that
// when the source has WithAppDetails/WithASCIIName/WithBuiltBy but the template
// lacks them, a drift item is produced (no auto-fix patch).
func TestRunChecks_versionOptionConstructors_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = strings.NewReplacer(
		"func WithAppDetails(", "func _WithAppDetails(",
		"func WithASCIIName(", "func _WithASCIIName(",
		"func WithBuiltBy(", "func _WithBuiltBy(",
	).Replace(versionTemplate)
	if tmpl.version == versionTemplate {
		t.Fatal("replacement had no effect – With* constructors not found in version.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" &&
			items[i].Property == "WithAppDetails, WithASCIIName, WithBuiltBy constructors" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal(
			"expected version 'WithAppDetails, WithASCIIName, WithBuiltBy constructors' drift item, not found",
		)
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
}

// TestRunChecks_mangoSection_sourceHasTemplateLacks verifies that when the
// mango source has Section but the man template does not, a fixable drift
// item with a patch is produced.
func TestRunChecks_mangoSection_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.man = strings.Replace(manCmdTemplate, "\tSection int\n", "", 1)
	if tmpl.man == manCmdTemplate {
		t.Fatal("strings.Replace had no effect – check exact string in man.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "man" && items[i].Property == "Section int field in Config" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected man 'Section int field in Config' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong presence: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.patch == nil {
		t.Error("expected patch for auto-fixable Section drift")
	}
}

// TestRunChecks_mangoSection_inSyncNoDrift verifies that when both source and
// template have Section, no drift is reported.
func TestRunChecks_mangoSection_inSyncNoDrift(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	for _, item := range items {
		if item.Template == "man" && item.Property == "Section int field in Config" {
			t.Errorf("unexpected drift for man Section: inSource=%s inTemplate=%s",
				item.InSource, item.InTemplate)
		}
	}
}

// ─── ApplyFixes tests ─────────────────────────────────────────────────────────

// TestApplyFixes_versionJSON verifies that ApplyFixes restores the JSON flag in
// the version template when the template file is missing it. Drift items are
// derived from runChecks to keep the patch logic DRY.
func TestApplyFixes_versionJSON(t *testing.T) {
	tmpDir := t.TempDir()
	tmplDir := filepath.Join(tmpDir, "pkg", "scaffold", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Remove JSON flag from the version template, simulating an outdated file.
	withoutJSON := strings.Replace(versionTemplate, "\tJSON    bool\n", "", 1)
	if withoutJSON == versionTemplate {
		t.Fatal("strings.Replace had no effect – check versionTemplate content")
	}
	if err := os.WriteFile(
		filepath.Join(tmplDir, "version.go.tmpl"),
		[]byte(withoutJSON),
		0o644,
	); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	// Derive drift items from runChecks (source has JSON, modified template lacks it).
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = withoutJSON
	items := runChecks(src, &tmpl)

	// Sanity-check: confirm the expected drift item was produced.
	var found bool
	for _, item := range items {
		if item.Template == "version" && item.Property == "JSON flag in Config" {
			found = true
		}
	}
	if !found {
		t.Fatal("runChecks did not produce version JSON drift item – cannot test ApplyFixes")
	}

	if err := ApplyFixes(tmpDir, items); err != nil {
		t.Fatalf("ApplyFixes: %v", err)
	}

	result, err := os.ReadFile(filepath.Join(tmplDir, "version.go.tmpl"))
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !strings.Contains(string(result), "\tJSON    bool\n") {
		t.Error("ApplyFixes did not restore JSON flag in version.go.tmpl")
	}
}

// TestApplyFixes_mangoSection verifies that ApplyFixes restores the Section int
// field in the man template when the template file is missing it. Drift items
// are derived from runChecks to keep the patch logic DRY.
func TestApplyFixes_mangoSection(t *testing.T) {
	tmpDir := t.TempDir()
	tmplDir := filepath.Join(tmpDir, "pkg", "scaffold", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Remove Section field from the man template, simulating an outdated file.
	withoutSection := strings.Replace(manCmdTemplate, "\tSection int\n", "", 1)
	if withoutSection == manCmdTemplate {
		t.Fatal("strings.Replace had no effect – check manCmdTemplate content")
	}
	if err := os.WriteFile(
		filepath.Join(tmplDir, "man.go.tmpl"),
		[]byte(withoutSection),
		0o644,
	); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	// Derive drift items from runChecks (source has Section, modified template lacks it).
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.man = withoutSection
	items := runChecks(src, &tmpl)

	// Sanity-check: confirm the expected drift item was produced.
	var found bool
	for _, item := range items {
		if item.Template == "man" && item.Property == "Section int field in Config" {
			found = true
		}
	}
	if !found {
		t.Fatal("runChecks did not produce man Section drift item – cannot test ApplyFixes")
	}

	if err := ApplyFixes(tmpDir, items); err != nil {
		t.Fatalf("ApplyFixes: %v", err)
	}

	result, err := os.ReadFile(filepath.Join(tmplDir, "man.go.tmpl"))
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !strings.Contains(string(result), "\tSection int\n") {
		t.Error("ApplyFixes did not restore Section int in man.go.tmpl")
	}
}

// TestApplyFixes_skipsItemsWithoutPatch verifies that items without a patch
// are silently skipped and do not cause an error.
func TestApplyFixes_skipsItemsWithoutPatch(t *testing.T) {
	tmpDir := t.TempDir()
	items := []DriftItem{
		{
			Template:   "version",
			Property:   "tabwriter output",
			InSource:   "present",
			InTemplate: "absent",
			patch:      nil, // no patch — manual review required
		},
	}
	if err := ApplyFixes(tmpDir, items); err != nil {
		t.Errorf("ApplyFixes should not error for patchless items, got: %v", err)
	}
}

// TestDriftItem_IsFixable verifies that IsFixable returns true only for items
// that were produced by a check with an auto-fix patch, and false for items
// produced by checks without patches (e.g. tabwriter drift).
func TestDriftItem_IsFixable(t *testing.T) {
	src := syntheticSourceInfoAllPresent()

	// JSON flag check has a patch — its drift item should be fixable.
	tmplWithPatch := defaultTemplates()
	tmplWithPatch.version = strings.Replace(versionTemplate, "\tJSON    bool\n", "", 1)
	if tmplWithPatch.version == versionTemplate {
		t.Fatal("setup: JSON flag not found in versionTemplate")
	}
	for _, item := range runChecks(src, &tmplWithPatch) {
		if item.Property == "JSON flag in Config" {
			if !item.IsFixable() {
				t.Error("JSON flag drift item should be fixable (has patch)")
			}
			goto nextCheck
		}
	}
	t.Fatal("JSON flag drift item not found")

nextCheck:
	// tabwriter check has no patch — its drift item should not be fixable.
	tmplNoPatch := defaultTemplates()
	tmplNoPatch.version = strings.Replace(
		versionTemplate,
		"tabwriter.NewWriter",
		"/* removed */",
		1,
	)
	if tmplNoPatch.version == versionTemplate {
		t.Fatal("setup: tabwriter.NewWriter not found in versionTemplate")
	}
	for _, item := range runChecks(src, &tmplNoPatch) {
		if item.Property == "tabwriter output" {
			if item.IsFixable() {
				t.Error("tabwriter drift item should not be fixable (no patch defined)")
			}
			return
		}
	}
	t.Fatal("tabwriter drift item not found")
}

// TestRunChecks_mainSignal_sourceHasTemplateLacks verifies that when the source
// has signal.NotifyContext but the template does not, a fixable drift item with
// a patch is produced.
func TestRunChecks_mainSignal_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.main = strings.Replace(mainTemplate, "signal.NotifyContext", "REMOVED", 1)
	if tmpl.main == mainTemplate {
		t.Fatal("strings.Replace had no effect – signal.NotifyContext not in main.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "main" && items[i].Property == "signal.NotifyContext" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected main 'signal.NotifyContext' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if !found.IsFixable() {
		t.Error("expected patch for signal.NotifyContext drift (auto-fixable)")
	}
}

// TestRunChecks_mainSignal_templateHasSourceLacks verifies that when the
// template has signal.NotifyContext but the source does not, a manual-review
// drift item without a patch is produced.
func TestRunChecks_mainSignal_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.mainHasSignal = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "main" && items[i].Property == "signal.NotifyContext" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected main 'signal.NotifyContext' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_cmdStdinParam_sourceHasTemplateLacks verifies that when the
// source has the stdin io.Reader parameter but the template does not, a fixable
// drift item with a patch is produced.
func TestRunChecks_cmdStdinParam_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.cmd = strings.Replace(cmdTemplate, "stdin io.Reader", "REMOVED", 1)
	if tmpl.cmd == cmdTemplate {
		t.Fatal("strings.Replace had no effect – stdin io.Reader not in cmd.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "cmd" && items[i].Property == "stdin io.Reader parameter in Run" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected cmd 'stdin io.Reader parameter in Run' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if !found.IsFixable() {
		t.Error("expected patch for stdin parameter drift (auto-fixable)")
	}
}

// TestRunChecks_cmdStdinParam_templateHasSourceLacks verifies that when the
// template has the stdin parameter but the source does not, a manual-review
// item without a patch is produced.
func TestRunChecks_cmdStdinParam_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.cmdHasStdinParam = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "cmd" && items[i].Property == "stdin io.Reader parameter in Run" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected cmd 'stdin io.Reader parameter in Run' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_rootStdinField_sourceHasTemplateLacks verifies that when the
// source has the Stdin field but the template does not, a fixable drift item
// with a patch is produced.
func TestRunChecks_rootStdinField_sourceHasTemplateLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.root = strings.Replace(rootTemplate, "Stdin   io.Reader", "REMOVED   io.Reader", 1)
	if tmpl.root == rootTemplate {
		t.Fatal("strings.Replace had no effect – Stdin field not in root.go.tmpl")
	}

	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "root" && items[i].Property == "Stdin io.Reader field in Config" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected root 'Stdin io.Reader field in Config' drift item, not found")
	}
	if found.InSource != "present" || found.InTemplate != "absent" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if !found.IsFixable() {
		t.Error("expected patch for Stdin field drift (auto-fixable)")
	}
}

// TestRunChecks_versionTabwriter_templateHasSourceLacks verifies the reverse
// drift direction: template has tabwriter, source does not.
func TestRunChecks_versionTabwriter_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionHasTabwriter = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "tabwriter output" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'tabwriter output' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_versionPointerReceiver_templateHasSourceLacks verifies the
// reverse drift direction for pointer receivers: template uses them, source
// does not.
func TestRunChecks_versionPointerReceiver_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionInfoMethodsUsePointerReceiver = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" &&
			items[i].Property == "Info methods use pointer receivers" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'Info methods use pointer receivers' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch (template has pointer receivers, source lacks — manual review)")
	}
}

// TestRunChecks_versionOptionType_templateHasSourceLacks verifies the reverse
// drift direction for the Option type: template has it, source does not.
func TestRunChecks_versionOptionType_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionHasOptionType = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" && items[i].Property == "Option type" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version 'Option type' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_versionOptionConstructors_templateHasSourceLacks verifies the
// reverse drift direction for Option constructors: template has them, source
// does not.
func TestRunChecks_versionOptionConstructors_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.versionHasOptionConstructors = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "version" &&
			items[i].Property == "WithAppDetails, WithASCIIName, WithBuiltBy constructors" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected version constructors drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// TestRunChecks_mangoSection_templateHasSourceLacks verifies the reverse drift
// direction for the man Section field: template has it, source does not.
func TestRunChecks_mangoSection_templateHasSourceLacks(t *testing.T) {
	src := syntheticSourceInfoAllPresent()
	src.mangoHasSectionField = false

	tmpl := defaultTemplates()
	items := runChecks(src, &tmpl)

	var found *DriftItem
	for i := range items {
		if items[i].Template == "man" && items[i].Property == "Section int field in Config" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected man 'Section int field in Config' drift item, not found")
	}
	if found.InSource != "absent" || found.InTemplate != "present" {
		t.Errorf("wrong direction: InSource=%q InTemplate=%q", found.InSource, found.InTemplate)
	}
	if found.IsFixable() {
		t.Error("expected no patch for manual-review drift (template has, source lacks)")
	}
}

// ─── DetectDrift integration tests ───────────────────────────────────────────

// TestDetectDrift_realDirectory verifies that the live climax source tree
// reports zero drift when DetectDrift is run directly against it — exactly
// replicating what "climax update" does when invoked from the module root.
// This test is read-only: it never writes to "../..".
func TestDetectDrift_realDirectory(t *testing.T) {
	items, err := DetectDrift("../..")
	if err != nil {
		t.Fatalf("DetectDrift(realDir): %v", err)
	}
	for _, item := range items {
		t.Errorf(
			"unexpected drift in real directory: template=%s property=%q inSource=%s inTemplate=%s",
			item.Template,
			item.Property,
			item.InSource,
			item.InTemplate,
		)
	}
}

// TestDetectDrift_cleanState verifies that the current climax source has zero
// drift against the embedded templates in all directions. Source files are
// copied to a temp dir; the originals are never modified.
func TestDetectDrift_cleanState(t *testing.T) {
	tmpDir := t.TempDir()
	setupClimaxSourceFiles(t, tmpDir)

	items, err := DetectDrift(tmpDir)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}

	for _, item := range items {
		t.Errorf("unexpected drift: template=%s property=%q inSource=%s inTemplate=%s",
			item.Template, item.Property, item.InSource, item.InTemplate)
	}
}

// TestDetectDrift_versionSource_missingJSON_detected verifies that DetectDrift
// reports drift when the version source file lacks the JSON flag (template ahead
// of source — manual-review category). Source copies are written to a temp dir.
func TestDetectDrift_versionSource_missingJSON_detected(t *testing.T) {
	tmpDir := t.TempDir()
	setupClimaxSourceFiles(t, tmpDir)

	// Remove the JSON flag from the copied version.go.
	vPath := filepath.Join(tmpDir, "cmd", "version", "version.go")
	data, err := os.ReadFile(vPath)
	if err != nil {
		t.Fatalf("reading version.go copy: %v", err)
	}
	modified := strings.Replace(string(data), "\tJSON    bool\n", "", 1)
	if modified == string(data) {
		t.Fatal("strings.Replace had no effect – JSON flag not found in version.go")
	}
	if err := os.WriteFile(vPath, []byte(modified), 0o644); err != nil {
		t.Fatalf("writing modified version.go: %v", err)
	}

	items, err := DetectDrift(tmpDir)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}

	var found bool
	for _, item := range items {
		if item.Template == "version" && item.Property == "JSON flag in Config" {
			found = true
			if item.InSource != "absent" || item.InTemplate != "present" {
				t.Errorf(
					"wrong direction: InSource=%q InTemplate=%q",
					item.InSource,
					item.InTemplate,
				)
			}
		}
	}
	if !found {
		t.Error("expected version JSON drift item when source lacks JSON flag, not found")
	}
}

// TestDetectDrift_mangoSource_missingSection_detected verifies that DetectDrift
// reports drift when the mango source file lacks the Section field.
func TestDetectDrift_mangoSource_missingSection_detected(t *testing.T) {
	tmpDir := t.TempDir()
	setupClimaxSourceFiles(t, tmpDir)

	// Remove the Section field from the copied mango.go.
	mPath := filepath.Join(tmpDir, "cmd", "mango", "mango.go")
	data, err := os.ReadFile(mPath)
	if err != nil {
		t.Fatalf("reading mango.go copy: %v", err)
	}
	// mango.go aligns struct fields; Section is followed by 3 spaces before "int".
	modified := strings.Replace(string(data), "\tSection   int\n", "", 1)
	if modified == string(data) {
		t.Fatal("strings.Replace had no effect – check Section field spacing in mango.go")
	}
	if err := os.WriteFile(mPath, []byte(modified), 0o644); err != nil {
		t.Fatalf("writing modified mango.go: %v", err)
	}

	items, err := DetectDrift(tmpDir)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}

	var found bool
	for _, item := range items {
		if item.Template == "man" && item.Property == "Section int field in Config" {
			found = true
		}
	}
	if !found {
		t.Error("expected man Section drift item when mango source lacks Section, not found")
	}
}

// TestDetectDrift_roundTrip verifies the full detect-then-fix cycle for the
// version JSON flag: copy templates to temp dir, remove JSON from the template
// file, run DetectDrift (which will see source-has/template-lacks), apply fixes,
// then confirm the template file was updated.
func TestDetectDrift_roundTrip_versionJSON(t *testing.T) {
	tmpDir := t.TempDir()
	setupClimaxSourceFiles(t, tmpDir)
	setupTemplateFiles(t, tmpDir)

	// Remove JSON flag from the version template FILE (not the embedded string).
	vTmplPath := filepath.Join(tmpDir, "pkg", "scaffold", "templates", "version.go.tmpl")
	data, err := os.ReadFile(vTmplPath)
	if err != nil {
		t.Fatalf("reading version.go.tmpl copy: %v", err)
	}
	withoutJSON := strings.Replace(string(data), "\tJSON    bool\n", "", 1)
	if withoutJSON == string(data) {
		t.Fatal("strings.Replace had no effect – JSON flag not found in version.go.tmpl")
	}
	if err := os.WriteFile(vTmplPath, []byte(withoutJSON), 0o644); err != nil {
		t.Fatalf("writing modified template: %v", err)
	}

	// Build a sourceInfo where version source has JSON but template (embedded) does too;
	// we craft the DriftItems directly to simulate source-ahead-of-template scenario.
	src := syntheticSourceInfoAllPresent()
	tmpl := defaultTemplates()
	tmpl.version = withoutJSON // simulate template lacking JSON
	items := runChecks(src, &tmpl)

	// Confirm the expected drift item is present.
	var found bool
	for _, item := range items {
		if item.Template == "version" && item.Property == "JSON flag in Config" &&
			item.InSource == "present" && item.InTemplate == "absent" {
			found = true
		}
	}
	if !found {
		t.Fatal("drift item for version JSON not found – cannot test round-trip fix")
	}

	// Apply fixes to the temp dir.
	if err := ApplyFixes(tmpDir, items); err != nil {
		t.Fatalf("ApplyFixes: %v", err)
	}

	// Verify the template file now contains the JSON flag.
	result, err := os.ReadFile(vTmplPath)
	if err != nil {
		t.Fatalf("reading patched template: %v", err)
	}
	if !strings.Contains(string(result), "\tJSON    bool\n") {
		t.Error("JSON flag not restored in version.go.tmpl after ApplyFixes")
	}
}
