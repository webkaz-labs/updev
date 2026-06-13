package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestInventoryRenderBuildsManualAppsPreview(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryRenderReport(inventoryRenderOptions{root: root, report: "manual-apps", format: "json"})
	if report.SchemaVersion != 1 || report.Report != "manual-apps" || report.Path != filepath.Join(root, "docs", "apps.md") {
		t.Fatalf("unexpected render report metadata: %#v", report)
	}
	for _, want := range []string{"Generated preview", "## Installed apps", "| Demo | installed | 2.0.0 |"} {
		if !strings.Contains(report.Content, want) {
			t.Fatalf("expected rendered markdown to contain %q:\n%s", want, report.Content)
		}
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only review candidate in render report, got %#v", report.ReviewCandidates)
	}
}

func TestInventoryRenderIncludesStructuredManualSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manual-apps.toml"), []byte(`[[manual.apps]]
name = "Structured Demo"
managed_by = "vendor"
description = "Structured source row."
review_status = "accepted"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	report := buildInventoryRenderReport(inventoryRenderOptions{root: root, report: "manual-apps", format: "json"})
	if !strings.Contains(report.Content, "| Structured Demo | vendor |") || !strings.Contains(report.Content, "Structured source row.") {
		t.Fatalf("expected structured manual source in rendered Markdown, got:\n%s", report.Content)
	}
}

func TestInventoryScanBuildsManualAppEvidence(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryScanReport(inventoryScanOptions{root: root, provider: "manual", format: "json"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected scan report metadata: %#v", report)
	}
	if report.Summary.Live != 1 || report.Summary.Desired != 0 {
		t.Fatalf("expected live-only manual summary, got %#v", report.Summary)
	}
	if len(report.Sections) != 1 || len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].Name != "Demo" {
		t.Fatalf("expected scanned app section, got %#v", report.Sections)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].Evidence[0].BundleID != "com.example.demo" {
		t.Fatalf("expected review candidate with bundle evidence, got %#v", report.ReviewCandidates)
	}
	if code := inventoryScanExitCode(report); code != 2 {
		t.Fatalf("expected review-needed scan exit code, got %d", code)
	}
}

func TestInventoryScanRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryScanOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestInventoryRenderRejectsUnsupportedReport(t *testing.T) {
	if _, err := parseInventoryRenderOptions([]string{"--report", "unknown"}); err == nil {
		t.Fatal("expected unsupported report error")
	}
}

func TestInventoryReviewBuildsManualOverridePreview(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected review report metadata: %#v", report)
	}
	if report.OverridesPath != filepath.Join(root, ".config", "updev", "inventory-overrides.toml") {
		t.Fatalf("unexpected default overrides path: %q", report.OverridesPath)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only manual app candidate, got %#v", report.Candidates)
	}
	for _, want := range []string{
		"[[manual.apps]]",
		`name = "Demo"`,
		`aliases = ["Demo.app", "com.example.demo"]`,
		`managed_by = "manual"`,
		`# reason_code = "manual_app_live_only"`,
		`# remediation_code = "manual_inventory_override"`,
		`# confidence = "medium"`,
		`managed_by="manual"`,
		`update_owner="unknown"`,
		`ownership_confidence="low"`,
		`provider_metadata="Info.plist"`,
		`bundle_id="com.example.demo"`,
		`version="2.0.0"`,
	} {
		if !strings.Contains(report.OverridePreview, want) {
			t.Fatalf("expected override preview to contain %q:\n%s", want, report.OverridePreview)
		}
	}
	if code := inventoryReviewExitCode(report); code != 2 {
		t.Fatalf("expected review-needed exit code, got %d", code)
	}
}

func TestInventoryReviewUsesConfiguredOverridePath(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory]\noverrides = \"state/manual-overrides.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.OverridesPath != filepath.Join(root, "state", "manual-overrides.toml") {
		t.Fatalf("expected configured overrides path, got %q", report.OverridesPath)
	}
	if report.Status != plan.StatusOK || len(report.Candidates) != 0 || report.OverridePreview != "" {
		t.Fatalf("expected empty review to stay ok, got %#v", report)
	}
}

func TestManualPlanTextDetailsExposeActionableHints(t *testing.T) {
	items := []manualPlanItem{{
		Name:           "Vendor App",
		Action:         "open-vendor",
		ReviewURL:      "https://example.com/vendor",
		InstallHint:    "open the vendor URL for review only",
		CommandPreview: []string{`open "https://example.com/vendor"`},
	}}
	var out bytes.Buffer
	printManualPlanTextDetails(&out, items, false)
	text := out.String()
	for _, want := range []string{"review details", "https://example.com/vendor", "open the vendor URL", `open "https://example.com/vendor"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected manual plan details to contain %q:\n%s", want, text)
		}
	}
}

func TestInventoryPlanBuildsManualReviewActions(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: "manual", format: "json"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected plan metadata: %#v", report)
	}
	if len(report.Items) != 1 || report.Items[0].Action != "needs-review" || report.Items[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only manual review action, got %#v", report.Items)
	}
	if report.Items[0].SuggestedProvider != "manual" || len(report.Items[0].CommandPreview) != 1 || !strings.Contains(report.Items[0].InstallHint, "accept, edit, or ignore") {
		t.Fatalf("expected review guidance fields, got %#v", report.Items[0])
	}
	if report.ActionCounts["needs-review"] != 1 || report.AttentionCount != 1 || len(report.NextSteps) == 0 {
		t.Fatalf("expected action counts, attention count, and next steps, got %#v %d %#v", report.ActionCounts, report.AttentionCount, report.NextSteps)
	}
	if code := inventoryPlanExitCode(report); code != 2 {
		t.Fatalf("expected review-needed plan exit code, got %d", code)
	}
}

func TestInventoryPlanSuggestsMASAdoption(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Store.app"), map[string]string{
		"CFBundleDisplayName": "Store",
		"CFBundleIdentifier":  "com.example.store",
	})
	receipt := filepath.Join(root, "Applications", "Store.app", "Contents", "_MASReceipt")
	if err := os.MkdirAll(receipt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(receipt, "receipt"), []byte("receipt"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: "manual", format: "json"})
	if len(report.Items) != 1 || report.Items[0].Action != "adopt-mas" || report.Items[0].Confidence != "high" {
		t.Fatalf("expected MAS adoption suggestion, got %#v", report.Items)
	}
	if report.Items[0].SuggestedOverride == nil || report.Items[0].SuggestedOverride.ManagedBy != "mas" {
		t.Fatalf("expected suggested MAS override, got %#v", report.Items[0].SuggestedOverride)
	}
	if report.Items[0].SuggestedProvider != "mas" || !strings.Contains(report.Items[0].InstallHint, "Mac App Store") {
		t.Fatalf("expected MAS provider guidance, got %#v", report.Items[0])
	}
	if len(report.Items[0].CommandPreview) != 1 || !strings.Contains(report.Items[0].CommandPreview[0], `mas search "Store"`) {
		t.Fatalf("expected MAS search preview for receipt-only evidence, got %#v", report.Items[0].CommandPreview)
	}
}

func TestManualInventoryReconcilesCaskByApplicationPathBasename(t *testing.T) {
	sections := reconcileManualAppSections([]toolSection{
		{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows: []toolRow{{
				Name:    "Code",
				Version: "1.123.0",
				State:   "installed",
				Detail:  "source: app bundle; path: /Applications/Visual Studio Code.app",
			}},
		},
		{
			Name:  "manual/homebrew-casks",
			Title: "manual / Homebrew casks",
			Rows: []toolRow{{
				Name:   "Visual Studio Code",
				State:  "brew",
				Detail: "source: homebrew cask; cask: visual-studio-code; status: ok",
			}},
		},
	})
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one reconciled installed section, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if row.Name != "Code" || row.State != "brew" || !strings.Contains(row.Detail, "cask: visual-studio-code") {
		t.Fatalf("expected app bundle row to merge Homebrew cask evidence, got %#v", row)
	}
	if !manualRowHiddenByDefault(sections[0], row, listOptions{provider: "manual"}) {
		t.Fatalf("expected default manual view to hide brew-owned installed app row, got %#v", row)
	}
}

func TestInventoryPlanAddsBrewAndVendorGuidance(t *testing.T) {
	sections := []toolSection{
		{
			Name:  "manual/homebrew-casks",
			Title: "manual / Homebrew casks",
			Rows: []toolRow{{
				Name:   "Firefox",
				State:  "brew",
				Detail: "source: homebrew cask; cask: firefox; status: ok; category: personal",
			}},
		},
		{
			Name:  "manual/vendor",
			Title: "manual / Vendor",
			Rows: []toolRow{{
				Name:   "Vendor Tool",
				State:  "manual",
				Detail: "source: vendor; 入手先: https://vendor.example.com/tool",
			}},
		},
	}
	items := manualPlanItems(sections)
	if len(items) != 2 {
		t.Fatalf("expected two manual plan items, got %#v", items)
	}
	byName := map[string]manualPlanItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	brew := byName["Firefox"]
	if brew.Action != "adopt-brew" || brew.SuggestedProvider != "brew" || len(brew.CommandPreview) != 1 || !strings.Contains(brew.CommandPreview[0], "brew info --cask") {
		t.Fatalf("expected brew adoption guidance, got %#v", brew)
	}
	if len(brew.Evidence) != 1 || brew.Evidence[0].ManagedBy != "brew" || brew.Evidence[0].UpdateOwner != "brew" || brew.Evidence[0].OwnershipConfidence != "high" || brew.Evidence[0].ProviderMetadata != "Homebrew cask inventory" {
		t.Fatalf("expected brew ownership evidence quality fields, got %#v", brew.Evidence)
	}
	if brew.SuggestedOverride == nil || brew.SuggestedOverride.ManagedBy != "brew" || !containsString(brew.SuggestedOverride.Aliases, "firefox") {
		t.Fatalf("expected brew suggested override, got %#v", brew.SuggestedOverride)
	}
	masItems := manualPlanItems(manualMASListSectionsFromOutput("123456789 Store Tool (1.2.3)\n"))
	if len(masItems) != 1 || masItems[0].Action != "adopt-mas" || len(masItems[0].CommandPreview) != 1 || masItems[0].CommandPreview[0] != `mas lookup "123456789"` {
		t.Fatalf("expected MAS id adoption guidance, got %#v", masItems)
	}
	vendor := byName["Vendor Tool"]
	if vendor.Action != "open-vendor" || vendor.SuggestedProvider != "vendor" || vendor.ReviewURL != "https://vendor.example.com/tool" || len(vendor.CommandPreview) != 1 || !strings.HasPrefix(vendor.CommandPreview[0], "open ") {
		t.Fatalf("expected gated vendor guidance, got %#v", vendor)
	}
	if vendor.SuggestedOverride == nil || vendor.SuggestedOverride.ManagedBy != "vendor" {
		t.Fatalf("expected vendor suggested override, got %#v", vendor.SuggestedOverride)
	}
	brewRow := manualPlanDetailRow(brew, "")
	if len(brewRow.Actions) != 1 || !strings.Contains(brewRow.Actions[0].Value, "review-cask") {
		t.Fatalf("expected brew plan row to expose review-cask action, got %#v", brewRow.Actions)
	}
	if !containsSubstring(brewRow.Metadata, "managed_by=brew") || !containsSubstring(brewRow.Metadata, "ownership_confidence=high") || !containsSubstring(brewRow.Metadata, "provider_metadata=Homebrew cask inventory") {
		t.Fatalf("expected brew detail metadata to expose ownership evidence, got %#v", brewRow.Metadata)
	}
	if action, target, ok := parseManualPlanDetailAction(brewRow.Actions[0].Value); !ok || action != "review-cask" || target != "firefox" {
		t.Fatalf("unexpected parsed manual plan action: action=%q target=%q ok=%v", action, target, ok)
	}
	planSections := manualPlanToolSections(inventoryPlanReport{Items: []manualPlanItem{brew, vendor}})
	if len(planSections) != 2 || planSections[0].Title != "manual / adopt-brew" || planSections[1].Title != "manual / open-vendor" {
		t.Fatalf("expected manual plan grouped table sections by action, got %#v", planSections)
	}
	if len(planSections[0].Rows[0].Actions) != len(brewRow.Actions) || !strings.Contains(planSections[0].Rows[0].Detail, "summary:") {
		t.Fatalf("expected manual grouped row to preserve actions and rich detail, got %#v", planSections[0].Rows[0])
	}
}

func TestInventoryPlanSuggestsIgnoreLocalForUserApplications(t *testing.T) {
	section := toolSection{
		Name:  "manual/installed-apps",
		Title: "manual / Installed apps",
		Rows: []toolRow{{
			Name:   "Local Helper",
			State:  "installed",
			Detail: "source: app bundle; path: /Users/demo/Applications/Local Helper.app",
		}},
	}
	items := manualPlanItems([]toolSection{section})
	if len(items) != 1 || items[0].Action != "ignore-local" || items[0].ReasonCode != "manual_app_user_local" {
		t.Fatalf("expected user-local app ignore candidate, got %#v", items)
	}
	row := manualPlanDetailRow(items[0], "")
	if len(row.Actions) != 3 {
		t.Fatalf("expected installed manual row to expose accept/ignore/edit actions, got %#v", row.Actions)
	}
}

func TestInventoryPlanFiltersActionAndQuery(t *testing.T) {
	items := []manualPlanItem{
		{Name: "Demo", Action: "needs-review", Detail: "one"},
		{Name: "Pencil", Action: "open-vendor", Detail: "pencil.dev"},
	}
	got := filterManualPlanItems(items, inventoryPlanOptions{action: "open-vendor", query: "pencil"})
	if len(got) != 1 || got[0].Name != "Pencil" {
		t.Fatalf("expected filtered plan item, got %#v", got)
	}
	candidates := filterManualReviewCandidatesForPlan([]manualReviewCandidate{{Name: "Demo"}, {Name: "Pencil"}}, got)
	if len(candidates) != 1 || candidates[0].Name != "Pencil" {
		t.Fatalf("expected review candidates to follow filtered plan items, got %#v", candidates)
	}
}

func TestInventoryPlanRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryPlanOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestInventoryReviewAcceptsManualOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName": "Demo",
		"CFBundleIdentifier":  "com.example.demo",
	})

	opts := inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "demo", format: "json"}
	report := buildInventoryReviewReport(opts)
	if len(report.Candidates) != 1 {
		t.Fatalf("expected one candidate before accept, got %#v", report.Candidates)
	}
	if _, _, _, err := applyInventoryReviewAction(opts, report); err != nil {
		t.Fatalf("accept action failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".config", "updev", "inventory-overrides.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name = "Demo"`, `aliases = ["Demo.app", "com.example.demo"]`, `managed_by = "manual"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected accepted override to contain %q:\n%s", want, string(data))
		}
	}
	after := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if after.Status != plan.StatusOK || len(after.Candidates) != 0 {
		t.Fatalf("expected accepted override to suppress review candidate, got %#v", after)
	}
}

func TestInventoryReviewIgnoreAddsLocalOnlyOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Local Helper.app"), map[string]string{
		"CFBundleDisplayName": "Local Helper",
		"CFBundleIdentifier":  "com.example.local-helper",
	})

	opts := inventoryReviewOptions{root: root, provider: "manual", action: "ignore", query: "helper", format: "json"}
	report := buildInventoryReviewReport(opts)
	if _, _, _, err := applyInventoryReviewAction(opts, report); err != nil {
		t.Fatalf("ignore action failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".config", "updev", "inventory-overrides.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`category = "Ignored"`, `lifecycle = "local-only"`, `local-only app ignored by manual inventory review`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected ignored override to contain %q:\n%s", want, string(data))
		}
	}
	sections := manualAppSectionsForInventoryCommand(root)
	if len(sections) != 1 || len(sections[0].Rows) != 1 || sections[0].Rows[0].State != "local-only" {
		t.Fatalf("expected local-only override row, got %#v", sections)
	}
	summary := manualProviderSummary(sections)
	if summary.Desired != 0 || summary.Live != 1 {
		t.Fatalf("expected local-only app to count as live-only, got %#v", summary)
	}
	localOnlyWithoutLive := manualProviderSummary(manualOverrideSections([]manualAppOverride{{Name: "Ghost", Lifecycle: "local-only"}}))
	if localOnlyWithoutLive.Desired != 0 || localOnlyWithoutLive.Live != 0 {
		t.Fatalf("expected local-only override without live evidence to stay out of desired/live counts, got %#v", localOnlyWithoutLive)
	}
	after := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if after.Status != plan.StatusOK || len(after.Candidates) != 0 {
		t.Fatalf("expected ignored override to suppress review candidate, got %#v", after)
	}
}

func TestInventoryReviewDetectsExistingOverrideByAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	path := filepath.Join(root, ".config", "updev", "inventory-overrides.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[[manual.apps]]
name = "Existing Demo"
aliases = ["com.example.demo"]
managed_by = "manual"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := manualReviewCandidate{
		Name: "Demo",
		SuggestedOverride: manualReviewOverrideFields{
			Name:    "Demo",
			Aliases: []string{"Demo.app", "com.example.demo"},
		},
	}
	existing := matchingManualOverride(root, candidate)
	if existing.Name != "Existing Demo" {
		t.Fatalf("expected existing override alias match, got %#v", existing)
	}
	report := inventoryReviewReport{Candidates: []manualReviewCandidate{candidate}}
	if _, _, _, err := applyInventoryReviewAction(inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "demo", format: "json"}, report); err == nil {
		t.Fatal("expected duplicate override to block accept append")
	}
}

func TestInventoryReviewListsUpdatesAndRemovesOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	path := filepath.Join(root, ".config", "updev", "inventory-overrides.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`# keep header
[[manual.apps]]
name = "Demo"
aliases = ["com.example.demo"]
managed_by = "manual"
detail = "before"

# keep neighbor comment
[[manual.apps]]
name = "Keep"
managed_by = "manual"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	list := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "list", query: "demo", format: "json"})
	if len(list.Overrides) != 1 || list.Overrides[0].Name != "Demo" {
		t.Fatalf("expected override list to filter by query, got %#v", list.Overrides)
	}
	t.Setenv("EDITOR", "true")
	updateOpts := inventoryReviewOptions{root: root, provider: "manual", action: "update", query: "demo", format: "json"}
	updateReport := buildInventoryReviewReport(updateOpts)
	changed, err := applyManualOverrideManagementAction(updateOpts, updateReport.Overrides)
	if err != nil || changed.Name != "Demo" {
		t.Fatalf("update action failed: changed=%#v err=%v", changed, err)
	}
	removeOpts := inventoryReviewOptions{root: root, provider: "manual", action: "remove", query: "demo", format: "json"}
	removeReport := buildInventoryReviewReport(removeOpts)
	changed, err = applyManualOverrideManagementAction(removeOpts, removeReport.Overrides)
	if err != nil || changed.Name != "Demo" {
		t.Fatalf("remove action failed: changed=%#v err=%v", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep header", "# keep neighbor comment", `name = "Keep"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected remove to preserve %q in untouched content:\n%s", want, string(data))
		}
	}
	after := loadManualAppOverrides(root)
	if len(after) != 1 || after[0].Name != "Keep" {
		t.Fatalf("expected remove action to leave untouched override, got %#v", after)
	}
}

func TestInventoryReviewActionRequiresSingleCandidate(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "One.app"), map[string]string{"CFBundleDisplayName": "One"})
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Two.app"), map[string]string{"CFBundleDisplayName": "Two"})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "accept", format: "json"})
	if _, _, _, err := applyInventoryReviewAction(inventoryReviewOptions{root: root, provider: "manual", action: "accept", format: "json"}, report); err == nil {
		t.Fatal("expected write action to require a single matching candidate")
	}
	filtered := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "one", format: "json"})
	if len(filtered.Candidates) != 1 || filtered.Candidates[0].Name != "One" {
		t.Fatalf("expected query to select one candidate, got %#v", filtered.Candidates)
	}
}

func TestInventoryReviewReconcilesCachedHomebrewCasks(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Evernote.app"), map[string]string{
		"CFBundleDisplayName": "Evernote",
		"CFBundleIdentifier":  "com.evernote.Evernote",
	})
	saveInventoryCache(inventoryCacheEntry{
		Version:       inventoryCacheVersion,
		Root:          root,
		IncludeVSCode: false,
		CreatedAt:     time.Now(),
		Report: plan.Report{
			Root: root,
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.Status != plan.StatusOK || len(report.Candidates) != 0 {
		t.Fatalf("expected cached cask evidence to suppress live-only manual candidate, got %#v", report)
	}
}

func TestInventoryReviewRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryReviewOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
