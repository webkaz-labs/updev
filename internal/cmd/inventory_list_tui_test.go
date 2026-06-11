package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
)

func TestJoinCommandQuotesShellFragments(t *testing.T) {
	got := joinCommand([]string{"bash", "-lc", "brew update && brew upgrade"})
	want := `bash -lc "brew update && brew upgrade"`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestListUsesGoImplementationByDefault(t *testing.T) {
	if shouldDelegate([]string{"list"}) {
		t.Fatal("expected updev list to use Go rich list")
	}
	if shouldDelegate([]string{"list", "--fast"}) {
		t.Fatal("expected updev list --fast to use Go inventory")
	}
}

func TestBuildListReportFiltersItems(t *testing.T) {
	result := inventoryResult{
		Cached:    true,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Report: plan.Report{
			Status: plan.StatusDrift,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "brew", Desired: 1, Live: 2, Extra: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Status: plan.StatusOK, Desired: true, Live: true},
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: profileMismatchDetail("personal")},
			},
		},
	}
	report := buildListReport(result, listOptions{provider: "brew", status: "extra", query: "jq"})
	if !report.Cached || report.CacheAge == "" {
		t.Fatalf("expected cache metadata, got %#v", report)
	}
	if report.Limit != 0 {
		t.Fatalf("expected default limit to stay unlimited, got %d", report.Limit)
	}
	if len(report.Items) != 1 || report.Items[0].Name != "jq" {
		t.Fatalf("unexpected filtered items: %#v", report.Items)
	}
	attention := buildListReport(result, listOptions{status: "attention"})
	if len(attention.Items) != 2 || !listReportHasItem(attention, "jq") || !listReportHasItem(attention, "warp") {
		t.Fatalf("expected attention filter to include drift items, got %#v", attention.Items)
	}
	profile := buildListReport(result, listOptions{status: "profile-mismatch"})
	if len(profile.Items) != 1 || profile.Items[0].Name != "warp" || profile.Filters["status"] != "profile-mismatch" {
		t.Fatalf("expected profile-mismatch filter to include scoped drift, got %#v", profile)
	}
	category := buildListReport(result, listOptions{category: "personal"})
	if len(category.Items) != 2 || !listReportHasItem(category, "visual-studio-code") || !listReportHasItem(category, "warp") || category.Filters["category"] != "personal" {
		t.Fatalf("expected category filter to include personal cask, got %#v", category)
	}
}

func listReportHasItem(report listReport, name string) bool {
	for _, item := range report.Items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func TestBuildListReportEnrichesMiseAndShowsMiseVersionRows(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	rows := `{
  "mise": {
    "rows": [
      ["node", "24.15.0", "-", "inactive", "-", "Node runtime"],
      ["node", "24.16.0", "lts", "active", "/tmp/config.toml", "Node runtime"]
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "rows_cache.json"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	tsv := "mise:node\tNode runtime\tNode.js runtime\n"
	if err := os.WriteFile(filepath.Join(cacheDir, "desc_ja.tsv"), []byte(tsv), 0o600); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	report := buildListReport(result, listOptions{provider: "mise", query: "node"})
	if len(report.Items) != 1 || report.Items[0].Version != "24.16.0" || report.Items[0].Detail != "Node.js runtime" {
		t.Fatalf("expected active mise row to enrich item, got %#v", report.Items)
	}
	if len(report.Sections) != 1 || report.Sections[0].Title != "mise / runtime" {
		t.Fatalf("expected mise runtime section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 2 {
		t.Fatalf("expected active and inactive mise rows, got %#v", report.Sections[0].Rows)
	}
	active := report.Sections[0].Rows[0]
	if active.Name != "node" || active.Version != "24.16.0" || active.State != "active" || active.Wanted != "lts" {
		t.Fatalf("unexpected active row: %#v", active)
	}
	inactive := report.Sections[0].Rows[1]
	if inactive.Name != "node" || inactive.Version != "24.15.0" || inactive.State != "inactive" || inactive.Wanted != "-" {
		t.Fatalf("unexpected inactive row: %#v", inactive)
	}
	pending := pendingTranslations(report, loadLegacyCache(), true)
	if pending["mise:node"] != "Node runtime" {
		t.Fatalf("expected mise row cache description to feed translation, got %#v", pending)
	}
}

func TestBuildListReportFiltersMiseSectionsByToolStatus(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	rows := `{
  "mise": {
    "rows": [
      ["node", "24.15.0", "-", "inactive", "-", "Node runtime"],
      ["node", "24.16.0", "lts", "active", "/tmp/config.toml", "Node runtime"]
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "rows_cache.json"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	active := buildListReport(result, listOptions{provider: "mise", status: "active"})
	if len(active.Sections) != 1 || len(active.Sections[0].Rows) != 1 || active.Sections[0].Rows[0].State != "active" {
		t.Fatalf("expected only active mise row, got %#v", active.Sections)
	}
	inactive := buildListReport(result, listOptions{provider: "mise", status: "inactive"})
	if len(inactive.Sections) != 1 || len(inactive.Sections[0].Rows) != 1 || inactive.Sections[0].Rows[0].State != "inactive" {
		t.Fatalf("expected only inactive mise row, got %#v", inactive.Sections)
	}
	installed := buildListReport(result, listOptions{provider: "mise", status: "installed"})
	if len(installed.Sections) != 1 || len(installed.Sections[0].Rows) != 2 {
		t.Fatalf("expected both installed mise rows, got %#v", installed.Sections)
	}
}

func TestBuildListReportAddsManualAppsOnlyWhenRequested(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## Adobe Creative Cloud で管理

- Adobe Photoshop 2025

## Mac App Store で管理

| アプリ | 用途 |
|--------|------|
| Final Cut Pro, iMovie | Apple 純正 動画/音楽 |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}

	defaultReport := buildListReport(result, listOptions{root: root})
	if len(defaultReport.Sections) != 0 || len(defaultReport.Providers) != 0 {
		t.Fatalf("expected manual apps to stay out of the default report, got sections=%#v providers=%#v", defaultReport.Sections, defaultReport.Providers)
	}

	report := buildListReport(result, listOptions{root: root, provider: "manual", query: "final"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/app-store" {
		t.Fatalf("expected filtered manual app-store section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].Name != "Final Cut Pro" || report.Sections[0].Rows[0].State != "manual" {
		t.Fatalf("unexpected manual rows: %#v", report.Sections[0].Rows)
	}
	if len(report.Providers) != 1 || report.Providers[0].Name != "manual" || report.Providers[0].Desired != 1 || report.Providers[0].Live != 0 {
		t.Fatalf("expected synthetic manual provider summary, got %#v", report.Providers)
	}
	if counts := listKindCounts(report); counts["manual"] != 1 {
		t.Fatalf("expected manual kind count, got %#v", counts)
	}
	if counts := listCategoryCounts(report); counts["manual"] != 1 {
		t.Fatalf("expected manual category count, got %#v", counts)
	}
	if !listManualOnly(listOptions{provider: "manual"}) {
		t.Fatal("expected manual provider filter to avoid live provider collection")
	}
	if listManualOnly(listOptions{provider: "manual", includeVSCode: true}) {
		t.Fatal("did not expect mixed manual/vscode list to skip provider collection")
	}
}

func TestManualMarkdownInventoryRequiresOptIn(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte("## Manual\n\n- Demo App\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual"})
	if len(report.Sections) != 0 || len(report.Providers) != 0 {
		t.Fatalf("expected unconfigured Markdown inventory to be ignored, got sections=%#v providers=%#v", report.Sections, report.Providers)
	}
}

func TestManualStructuredInventorySourceAddsAcceptedRows(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "manual-apps.toml")
	if err := os.WriteFile(source, []byte(`[[manual.apps]]
name = "Motion"
aliases = ["Motion.app", "com.apple.motionapp"]
managed_by = "mas"
category = "creative"
description = "Apple motion graphics app."
confidence = "high"
review_status = "accepted"

[manual.apps.identifiers]
bundle_id = "com.apple.motionapp"

[manual.apps.provenance]
source = "human"
source_url = "https://apps.apple.com/app/motion/id434290957"
owner = "Apple"
update_owner = "mas"
provider_metadata = "mac app store receipt"
evidence = ["mac_app_store_receipt", "app_bundle"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual"})
	if len(report.Sections) != 1 || len(report.Sections[0].Rows) != 1 {
		t.Fatalf("expected one structured manual row, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	for _, want := range []string{"bundle_id: com.apple.motionapp", "source: human", "source_url: https://apps.apple.com/app/motion/id434290957", "owner: Apple", "update_owner: mas", "provider_metadata: mac app store receipt"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected structured manual row detail to contain %q: %#v", want, row)
		}
	}
	if row.Name != "Motion" || row.State != "mas" {
		t.Fatalf("unexpected structured manual row: %#v", row)
	}
	if _, ok := manualAppMatch(manualAppIndex(root), "com.apple.motionapp"); !ok {
		t.Fatal("expected accepted structured app alias to be indexed")
	}
}

func TestManualStructuredDraftDoesNotSuppressLiveReviewCandidate(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Draft Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Draft Demo",
		"CFBundleIdentifier":         "com.example.draft-demo",
		"CFBundleShortVersionString": "1.0",
	})
	source := filepath.Join(root, "manual-apps.toml")
	if err := os.WriteFile(source, []byte(`[[manual.apps]]
name = "Draft Demo"
aliases = ["com.example.draft-demo"]
managed_by = "vendor"
category = "drafts"
description = "Agent draft description."
review_status = "draft"

[manual.apps.identifiers]
bundle_id = "com.example.draft-demo"

[manual.apps.provenance]
source = "agent"
evidence = ["app_bundle"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: "manual", format: "json"})
	if report.Status != plan.StatusDrift || len(report.Items) != 1 {
		t.Fatalf("expected draft to keep live app in review plan, got %#v", report)
	}
	item := report.Items[0]
	if item.Name != "Draft Demo" || item.Action != "needs-review" || !strings.Contains(item.Detail, "Agent draft description") {
		t.Fatalf("expected live candidate enriched by draft details, got %#v", item)
	}
	if _, ok := manualAppMatch(manualAppIndex(root), "com.example.draft-demo"); ok {
		t.Fatal("did not expect draft structured app to be indexed as desired state")
	}
}

func TestInventoryReviewAgentEnrichWritesDraftSource(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Agent Demo.app"), map[string]string{
		"CFBundleDisplayName": "Agent Demo",
		"CFBundleIdentifier":  "com.example.agent-demo",
	})
	source := filepath.Join(root, "manual-apps.toml")
	if err := os.WriteFile(source, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeManualAgent(t, root, `[[manual.apps]]
name = "Agent Demo"
aliases = ["com.example.agent-demo"]
managed_by = "vendor"
description = "Agent generated draft."
review_status = "accepted"

[manual.apps.identifiers]
bundle_id = "com.example.agent-demo"
`)
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n\n[inventory.agent]\nenabled = true\ncommand = [%q]\n", agent)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	opts := inventoryReviewOptions{root: root, provider: "manual", action: "enrich", query: "agent demo", format: "json"}
	report := buildInventoryReviewReport(opts)
	if _, _, drafts, err := applyInventoryReviewAction(opts, report); err != nil || len(drafts) != 1 {
		t.Fatalf("agent enrich failed: drafts=%#v err=%v", drafts, err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`review_status = "draft"`, `source = "agent"`, `command = "manual-agent"`, `bundle_id = "com.example.agent-demo"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected draft source to contain %q:\n%s", want, string(data))
		}
	}
}

func TestInventoryReviewAgentEnrichBatchRequiresBatchOptIn(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "manual-apps.toml")
	if err := os.WriteFile(source, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeManualAgent(t, root, "")
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n\n[inventory.agent]\nenabled = true\ncommand = [%q]\nbatch = false\n", agent)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	candidates := []manualReviewCandidate{{Name: "One"}, {Name: "Two"}}
	opts := inventoryReviewOptions{root: root, provider: "manual", action: "enrich-batch", format: "json"}
	if _, _, _, err := applyInventoryReviewAction(opts, inventoryReviewReport{Candidates: candidates}); err == nil {
		t.Fatal("expected enrich-batch to require [inventory.agent].batch = true")
	}
}

func TestManualStructuredDraftActionsAcceptEditAndIgnore(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "manual-apps.toml")
	if err := os.WriteFile(source, []byte(`[[manual.apps]]
name = "Draft Demo"
aliases = ["com.example.draft-demo"]
managed_by = "vendor"
description = "Agent generated draft."
review_status = "draft"

[manual.apps.identifiers]
bundle_id = "com.example.draft-demo"

[manual.apps.provenance]
source = "agent"
evidence = ["app_bundle"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	rows := manualAppSectionsForInventoryCommand(root)
	if len(rows) != 1 || len(rows[0].Rows) != 1 || len(rows[0].Rows[0].Actions) != 3 {
		t.Fatalf("expected draft row with accept/edit/ignore actions, got %#v", rows)
	}
	if err := applyManualStructuredDraftAction(root, "accept-draft", "draft demo"); err != nil {
		t.Fatalf("accept draft failed: %v", err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `review_status = "accepted"`) {
		t.Fatalf("expected accepted draft:\n%s", string(data))
	}
	if _, ok := manualAppMatch(manualAppIndex(root), "com.example.draft-demo"); !ok {
		t.Fatal("expected accepted draft to become manual desired index")
	}
	if err := applyManualStructuredDraftAction(root, "ignore-draft", "draft demo"); err == nil {
		t.Fatal("expected accepted draft to be unavailable for ignore-draft")
	}
}

func TestManualPlanDetailRowsAddsBatchEnrichmentAction(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manual-apps.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeManualAgent(t, root, "")
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("[inventory.manual]\nsources = [\"manual-apps.toml\"]\n\n[inventory.agent]\nenabled = true\ncommand = [%q]\nbatch = true\n", agent)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	rows := manualPlanDetailRows(inventoryPlanReport{
		Root: root,
		ReviewCandidates: []manualReviewCandidate{
			{Name: "One"},
			{Name: "Two"},
		},
		Items: []manualPlanItem{{Name: "One", State: "installed", Action: "needs-review"}},
	})
	if len(rows) < 2 || rows[0].Title != "batch draft enrichment" || len(rows[0].Actions) != 1 || !strings.Contains(rows[0].Actions[0].Value, "\tenrich-batch\t") {
		t.Fatalf("expected batch enrichment row before item rows, got %#v", rows)
	}
	if !detailRowHasManualAction(rows[1], "enrich") {
		t.Fatalf("expected item row to expose single enrich action, got %#v", rows[1].Actions)
	}
}

func detailRowHasManualAction(row detailBrowserRow, action string) bool {
	for _, candidate := range row.Actions {
		parsed, _, ok := parseManualPlanDetailAction(candidate.Value)
		if ok && parsed == action {
			return true
		}
	}
	return false
}

func writeFakeManualAgent(t *testing.T, root string, output string) string {
	t.Helper()
	path := filepath.Join(root, "manual-agent")
	script := "#!/bin/sh\ncat >/dev/null\ncat <<'EOF'\n" + output + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManualInventoryUsesConfiguredOverridesAndAliases(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory]\noverrides = \"inventory-overrides.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	overrides := `[[manual.apps]]
name = "Example App"
aliases = ["example-cask", "Example.app"]
category = "Vendor"
detail = "vendor updater"
managed_by = "vendor"
`
	if err := os.WriteFile(filepath.Join(root, "inventory-overrides.toml"), []byte(overrides), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", query: "example"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/vendor" {
		t.Fatalf("expected manual override section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].State != "vendor" || report.Sections[0].Rows[0].Detail != "vendor updater" {
		t.Fatalf("unexpected override row: %#v", report.Sections[0].Rows)
	}
	if _, ok := manualAppMatch(manualAppIndex(root), "example-cask"); !ok {
		t.Fatalf("expected alias to reconcile manual/cask identity")
	}
}

func TestManualInventoryScansApplicationBundles(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo Display",
		"CFBundleName":               "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "1.2.3",
	})

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", status: "installed", query: "demo"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/installed-apps" {
		t.Fatalf("expected installed app section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Demo Display" || row.Version != "1.2.3" || row.State != "installed" {
		t.Fatalf("unexpected scanned app row: %#v", row)
	}
	for _, want := range []string{"source: app bundle", "path: ", "bundle_id: com.example.demo", "version: 1.2.3", "managed_by: manual", "update_owner: unknown", "ownership_confidence: low", "provider_metadata: Info.plist"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected scanned app detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Name != "manual" || report.Providers[0].Desired != 0 || report.Providers[0].Live != 1 {
		t.Fatalf("expected scanned app to count as live-only manual inventory, got %#v", report.Providers)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only installed app review candidate, got %#v", report.ReviewCandidates)
	}
	candidate := report.ReviewCandidates[0]
	if candidate.Evidence[0].Scanner != "macos_app_bundle" || candidate.Evidence[0].BundleID != "com.example.demo" || candidate.Evidence[0].ManagedBy != "manual" || candidate.Evidence[0].UpdateOwner != "unknown" || candidate.Evidence[0].OwnershipConfidence != "low" || candidate.Evidence[0].ProviderMetadata != "Info.plist" || candidate.SuggestedOverride.Name != "Demo Display" || candidate.Confidence != "medium" || candidate.RemediationCode != "manual_inventory_override" {
		t.Fatalf("unexpected review candidate evidence: %#v", candidate)
	}
}

func TestManualInventoryMarksMacAppStoreReceipts(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "Applications", "StoreDemo.app")
	writeFakeAppBundle(t, appPath, map[string]string{
		"CFBundleDisplayName":        "Store Demo",
		"CFBundleIdentifier":         "com.example.store-demo",
		"CFBundleShortVersionString": "5.0.0",
	})
	writeFakeMASReceipt(t, appPath)

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", status: "installed", query: "store"})
	if len(report.Sections) != 1 || len(report.Sections[0].Rows) != 1 {
		t.Fatalf("expected one MAS receipt app row, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	for _, want := range []string{"source: mac app store receipt", "managed_by: mas", "update_owner: mas", "ownership_confidence: high", "provider_metadata: mac app store receipt"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected MAS receipt detail to contain %q, got %q", want, row.Detail)
		}
	}
	if !strings.Contains(row.Detail, "source: mac app store receipt") {
		t.Fatalf("expected MAS receipt source in detail, got %q", row.Detail)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].SuggestedOverride.ManagedBy != "mas" || report.ReviewCandidates[0].Confidence != "high" {
		t.Fatalf("expected MAS receipt candidate to suggest managed_by=mas, got %#v", report.ReviewCandidates)
	}
}

func TestManualInventoryParsesMASListEvidence(t *testing.T) {
	apps := parseManualMASList("803453959 Slack (4.45.69)\n409201541 Pages (14.4)\n")
	if len(apps) != 2 || apps[0].Name != "Pages" || apps[0].ID != "409201541" || apps[0].Version != "14.4" || apps[1].Name != "Slack" {
		t.Fatalf("unexpected mas list parse: %#v", apps)
	}

	sections := reconcileManualAppSections(manualMASListSectionsFromOutput("409201541 Pages (14.4)\n"))
	candidates := manualReviewCandidates(sections)
	if len(candidates) != 1 {
		t.Fatalf("expected MAS-only app review candidate, got %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.SuggestedOverride.ManagedBy != "mas" || candidate.Confidence != "high" || candidate.Evidence[0].Scanner != "mas_list" || candidate.Evidence[0].MASID != "409201541" {
		t.Fatalf("unexpected MAS-only candidate: %#v", candidate)
	}
}

func TestManualInventoryMergesMASListWithAppBundleEvidence(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Pages.app"), map[string]string{
		"CFBundleDisplayName":        "Pages",
		"CFBundleIdentifier":         "com.apple.iWork.Pages",
		"CFBundleShortVersionString": "14.4",
	})

	sections := append(manualScannedAppSections(root), manualMASListSectionsFromOutput("409201541 Pages (14.4)\n")...)
	sections = reconcileManualAppSections(sections)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected merged MAS/app-bundle evidence, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if row.State != "installed" || !strings.Contains(row.Detail, "source: app bundle") || !strings.Contains(row.Detail, "source: mas list") || !strings.Contains(row.Detail, "mas_id: 409201541") {
		t.Fatalf("unexpected merged MAS/app-bundle row: %#v", row)
	}
	candidates := manualReviewCandidates(sections)
	if len(candidates) != 1 || candidates[0].SuggestedOverride.ManagedBy != "mas" || candidates[0].Evidence[0].MASID != "409201541" {
		t.Fatalf("expected merged MAS candidate to preserve MAS evidence, got %#v", candidates)
	}
}

func TestManualInventoryReconcilesDocumentedAndInstalledApps(t *testing.T) {
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte("## 業務・専門アプリ（Cask なし）\n\n- Pencil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Pencil.app"), map[string]string{
		"CFBundleDisplayName":        "Pencil",
		"CFBundleIdentifier":         "dev.pencil.desktop",
		"CFBundleShortVersionString": "1.1.17",
	})

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", query: "Pencil"})
	if len(report.Sections) != 1 {
		t.Fatalf("expected documented and installed app to reconcile into one section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Pencil" || row.Version != "1.1.17" || row.State != "managed" {
		t.Fatalf("expected reconciled managed row, got %#v", row)
	}
	for _, want := range []string{"業務・専門アプリ", "source: app bundle", "bundle_id: dev.pencil.desktop"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected reconciled detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Desired != 1 || report.Providers[0].Live != 1 {
		t.Fatalf("expected reconciled app to count as desired and live, got %#v", report.Providers)
	}
	if len(report.ReviewCandidates) != 0 {
		t.Fatalf("did not expect review candidate for reconciled app, got %#v", report.ReviewCandidates)
	}
}

func TestManualInventoryReconcilesDocumentedAppsWithHomebrewCasks(t *testing.T) {
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte("## ベンダー独自更新\n\n- Evernote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusOK, Desired: true, Live: true, Category: "personal"},
		},
	}}

	report := buildListReport(result, listOptions{root: root, provider: "manual", query: "Evernote"})
	if len(report.Sections) != 1 {
		t.Fatalf("expected documented and cask app to reconcile into one section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Evernote" || row.State != "brew" {
		t.Fatalf("expected reconciled brew row, got %#v", row)
	}
	for _, want := range []string{"ベンダー独自更新", "source: homebrew cask", "cask: evernote", "category: personal"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected cask reconciliation detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Desired != 1 || report.Providers[0].Live != 1 {
		t.Fatalf("expected reconciled cask to count as desired and live, got %#v", report.Providers)
	}
}

func TestManualInventoryHidesHomebrewOnlyCasksByDefault(t *testing.T) {
	root := t.TempDir()
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "firefox", Status: plan.StatusOK, Desired: true, Live: true, Category: "personal"},
		},
	}}

	defaultReport := buildListReport(result, listOptions{root: root, provider: "manual"})
	if len(defaultReport.Sections) != 0 {
		t.Fatalf("expected Homebrew-only cask evidence to be hidden by default, got %#v", defaultReport.Sections)
	}
	statusReport := buildListReport(result, listOptions{root: root, provider: "manual", status: "brew"})
	if len(statusReport.Sections) != 1 || statusReport.Sections[0].Rows[0].Name != "Firefox" {
		t.Fatalf("expected explicit brew status filter to show cask evidence, got %#v", statusReport.Sections)
	}
	queryReport := buildListReport(result, listOptions{root: root, provider: "manual", query: "firefox"})
	if len(queryReport.Sections) != 1 || queryReport.Sections[0].Rows[0].State != "brew" {
		t.Fatalf("expected query filter to show cask evidence, got %#v", queryReport.Sections)
	}
}

func TestHomebrewTapDocsStayBrewEvidenceNotManualDesired(t *testing.T) {
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## Intel Mac 向け自前ビルド（Homebrew Tap で自動配布）

| アプリ | リポジトリ | Cask 名 | ビルド方式 |
|--------|-----------|---------|-----------|
| Codex Monitor | [webkaz/codexmonitor-intel-builds](https://github.com/webkaz/codexmonitor-intel-builds) | ` + "`codexmonitor-intel`" + ` | Tauri クロスコンパイル |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Codex Monitor.app"), map[string]string{
		"CFBundleDisplayName":        "Codex Monitor",
		"CFBundleIdentifier":         "com.example.codexmonitor",
		"CFBundleShortVersionString": "0.7.67",
	})
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "codexmonitor-intel",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
	}}

	normal := buildListReport(result, listOptions{root: root, query: "codexmonitor"})
	if len(normal.Items) != 1 || !strings.Contains(normal.Items[0].Detail, "source: homebrew tap docs") || !strings.Contains(normal.Items[0].Detail, "cask: codexmonitor-intel") {
		t.Fatalf("expected normal brew inventory row to carry Homebrew Tap docs evidence, got %#v", normal.Items)
	}

	manual := buildListReport(result, listOptions{root: root, provider: "manual", query: "codexmonitor"})
	if len(manual.Sections) != 1 || manual.Sections[0].Name != "manual/installed-apps" {
		t.Fatalf("expected Homebrew Tap docs to reconcile into installed evidence, got %#v", manual.Sections)
	}
	row := manual.Sections[0].Rows[0]
	if row.Name != "Codex Monitor" || row.State != "brew" || !strings.Contains(row.Detail, "source: homebrew tap docs") || !strings.Contains(row.Detail, "source: homebrew cask") {
		t.Fatalf("expected reconciled brew evidence row, got %#v", row)
	}
	if strings.Count(row.Detail, "cask: codexmonitor-intel") != 1 {
		t.Fatalf("expected cask evidence to be deduplicated, got %q", row.Detail)
	}
	if len(row.Actions) == 0 || !toolRowHasRouteAction(row, listHubActionManual) {
		t.Fatalf("expected reconciled row to preserve manual review routing action, got %#v", row.Actions)
	}
	if len(manual.Providers) != 1 || manual.Providers[0].Desired != 0 || manual.Providers[0].Live != 1 {
		t.Fatalf("expected Homebrew Tap evidence not to count as manual desired state, got %#v", manual.Providers)
	}
	if len(manual.ReviewCandidates) != 0 {
		t.Fatalf("did not expect manual review candidate for Homebrew-managed cask evidence, got %#v", manual.ReviewCandidates)
	}
}

func TestManualLiveCaskInventoryItemsUsesBoundedBrewProbe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Homebrew cask probe is macOS-only")
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "firefox\nvisual-studio-code\n"}}
	items := manualLiveCaskInventoryItems(defaultRoot(), fake)
	if len(items) != 2 || items[0].Provider != "brew" || items[0].Kind != "cask" || items[0].Name != "firefox" || items[0].Status != plan.StatusExtra || !items[0].Live {
		t.Fatalf("expected live Homebrew cask evidence, got %#v", items)
	}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], " ") != "brew list --cask -1" {
		t.Fatalf("expected bounded brew cask list probe, got %#v", fake.calls)
	}
	if got := manualLiveCaskInventoryItems(t.TempDir(), fake); got != nil {
		t.Fatalf("expected non-default root to skip live brew probe, got %#v", got)
	}
}

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

func writeFakeAppBundle(t *testing.T, path string, values map[string]string) {
	t.Helper()
	contents := filepath.Join(path, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	var plist strings.Builder
	plist.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
`)
	for key, value := range values {
		plist.WriteString("<key>")
		plist.WriteString(key)
		plist.WriteString("</key><string>")
		plist.WriteString(value)
		plist.WriteString("</string>\n")
	}
	plist.WriteString("</dict></plist>\n")
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFakeMASReceipt(t *testing.T, appPath string) {
	t.Helper()
	receiptDir := filepath.Join(appPath, "Contents", "_MASReceipt")
	if err := os.MkdirAll(receiptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(receiptDir, "receipt"), []byte("fake receipt"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildListReportUsesManualInventoryForCaskDriftGuidance(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## ベンダー独自更新

| アプリ | 入手先 | 用途 |
|--------|--------|------|
| Evernote | 公式 | ノート |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusDrift,
			Root:   root,
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusExtra, Live: true},
			},
		},
	}
	report := buildListReport(result, listOptions{root: root})
	if len(report.Items) != 1 || !strings.Contains(report.Items[0].Detail, "manual-local-only") {
		t.Fatalf("expected manual inventory guidance for extra cask, got %#v", report.Items)
	}
}

func TestPrintListTextUsesMiseSectionsInsteadOfDuplicateInventoryItems(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "mise", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Version: "24.16.0", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Sections: []toolSection{
			{
				Name:  "mise/runtime",
				Title: "mise / runtime",
				Rows: []toolRow{
					{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				},
			},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if strings.Contains(text, "mise / tool / runtime") {
		t.Fatalf("expected mise inventory item table to be suppressed when rich section exists:\n%s", text)
	}
	if !strings.Contains(text, "summary:") || !strings.Contains(text, "wanted") || !strings.Contains(text, "24.16.0") || !strings.Contains(text, "lts") {
		t.Fatalf("expected rich mise section with version and requested version:\n%s", text)
	}
}

func TestPrintListTextKeepsMiseDriftItemsWithRichSections(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "mise", Desired: 2, Live: 1, Missing: 1},
		},
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Version: "24.16.0", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Category: "github", Name: "github:openai/tunnel-client", Status: plan.StatusMissing, Desired: true, Detail: "defined in mise but not installed"},
		},
		Sections: []toolSection{
			{
				Name:  "mise/runtime",
				Title: "mise / runtime",
				Rows: []toolRow{
					{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				},
			},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if strings.Contains(text, "mise / tool / runtime") {
		t.Fatalf("expected ok mise inventory rows to stay suppressed when rich section exists:\n%s", text)
	}
	for _, want := range []string{"missing=1", "mise / tool / github", "github:openai/tunnel-client", "defined in mise but not installed", "mise / runtime", "24.16.0", "lts"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list text to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextShowsAttentionSummary(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 2, Extra: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
			{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: profileMismatchDetail("personal")},
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"summary:", "1 provider attention", "extra=1", "profile-mismatch=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list summary to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextShowsCategoryMeaning(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 1},
			{Name: "mise", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node", State: "active"}},
		}},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"categories", "personal=1", "runtime=1", "meaning:", "personal-only additions on top of work", "other categories are provider/backend groups"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected category summary to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextDetailsExpandsDescriptions(t *testing.T) {
	report := listReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Details: true,
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "demo", Status: plan.StatusOK, Desired: true, Live: true, Detail: "A deliberately long package description that should be available in the detail section."},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"details", "brew/brew demo", "A deliberately long package description"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list detail output to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextSurfacesReviewEvidence(t *testing.T) {
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "cask", "demo-app"), "brew held: release age gate")
	listEvidenceAdd(evidence.Security, listEvidenceExactKey("brew", "cask", "demo-app"), "brew/cask demo-app: hold")
	listEvidenceAdd(evidence.Backends, listEvidenceExactKey("mise", "tool", "cargo:fd-find"), "aqua prebuilt CLI is preferred")
	report := listReport{
		Status:  plan.StatusHeld,
		Root:    "/repo",
		Details: true,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo-app",
			Status:   plan.StatusHeld,
			Desired:  true,
			Live:     true,
		}},
		Sections: []toolSection{{
			Name:  "mise/cargo",
			Title: "mise / cargo",
			Rows: []toolRow{{
				Name:   "cargo:fd-find",
				State:  "active",
				Detail: "fd via cargo",
			}},
		}},
		Evidence: evidence,
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"review: upd=1 sec=1 bak=1", "update evidence:", "security evidence:", "backend evidence:", "brew held: release age gate", "aqua prebuilt CLI is preferred"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list text to include review evidence %q:\n%s", want, text)
		}
	}
}

func TestListEvidenceSummaryShowsZeroCounts(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo", Evidence: listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if !strings.Contains(text, "review: upd=0 sec=0 bak=0") {
		t.Fatalf("expected zero evidence counts to be visible:\n%s", text)
	}
	if got := listTitleWithEvidenceSummary("updev installed inventory", report); !strings.Contains(got, "upd=0 sec=0 bak=0") {
		t.Fatalf("expected TUI title evidence counts, got %q", got)
	}
}

func TestListTitleEvidenceSummaryCountsOnlyVisibleRowActions(t *testing.T) {
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "jq"), "brew updated: jq 1.7 -> 1.8.1")
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "hidden"), "brew updated: hidden 1.0 -> 1.1")
	listEvidenceAdd(evidence.Security, listEvidenceExactKey("brew", "cask", "ghost"), "brew/cask ghost: hold")
	listEvidenceAdd(evidence.Backends, listEvidenceExactKey("brew", "brew", "jq"), "backend evidence: jq")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "jq",
			Version:  "1.8.1",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	title := listTitleWithEvidenceSummary("updev installed inventory", report)
	if !strings.Contains(title, "upd=1 sec=0 bak=1") {
		t.Fatalf("expected title to count only visible row actions, got %q", title)
	}
}

func TestListTableShowsUpdatedBadgeWithVersionDelta(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "jq"), "brew updated: jq 1.7 -> 1.8.1")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "jq",
			Version:  "1.8.1",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶up 1.7→1.8.1" {
		t.Fatalf("expected updated badge with version delta, got %#v", row)
	}
}

func TestListTableOmitsSymbolicUpdateDelta(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "wezterm@nightly"), "brew updated: wezterm@nightly latest -> latest")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "wezterm@nightly",
			Version:  "latest",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶up" {
		t.Fatalf("expected symbolic update badge without version delta, got %#v", row)
	}
}

func TestListTableShowsHeldBadgeForDeferredUpdateEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "cask", "demo-app"), "brew held: release age gate")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo-app",
			Status:   plan.StatusHeld,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶upd" {
		t.Fatalf("expected held update badge, got %#v", row)
	}
}

func TestListTableShowsHeldBadgeForBrewSecurityHoldEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	for _, key := range listEvidenceFindingKeys(safetyGate{Provider: "brew"}, safetyFinding{
		Provider: "brew",
		Kind:     "brew",
		Name:     "jq",
		Decision: "hold",
		Reason:   "candidate release is too new",
	}) {
		listEvidenceAdd(evidence.Security, key, "brew/brew jq: hold")
	}
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "jq",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶sec" {
		t.Fatalf("expected security hold badge, got %#v", row)
	}
	if !toolRowHasRouteAction(sections[0].Rows[0], listHubActionSecurity) {
		t.Fatalf("expected security route action for held brew finding, got %#v", sections[0].Rows[0].Actions)
	}
}

func TestListTableShowsUpdateAndSecurityBadgesTogether(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("mise", "tool", "go"), "mise-bump deferred: candidate available")
	listEvidenceAdd(evidence.Security, listEvidenceExactKey("mise", "tool", "go"), "mise/tool go: hold")
	report := listReport{
		Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "go",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶upd ▶sec" {
		t.Fatalf("expected update and security badges to be visible together, got %#v", row)
	}
}

func TestListEvidenceMetadataLocalizesMiseMessages(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	metadata := (listItemEvidence{
		Updates:  []string{"mise-bump held: mise bump applied 1 safe candidates; 18 candidates require review"},
		Security: []string{"mise/tool node: held (decision: review): mise backend is unsupported or opaque for updev-owned release-age evidence"},
	}).Metadata()
	text := strings.Join(metadata, "\n")
	for _, want := range []string{"更新根拠:", "セキュリティ根拠:", "安全な更新候補 1 件を適用済みです", "リリース経過日数の根拠を十分に確認できない"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected localized evidence text to include %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"update evidence:", "security evidence:", "mise bump applied", "unsupported or opaque"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("expected localized evidence text to avoid %q:\n%s", unwanted, text)
		}
	}
}

func TestListSecurityEvidenceShowsReleaseAgeAvailability(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	releasedAt := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	detail := listSecurityEvidenceDetail(updateReport{Security: "strict"}, safetyGate{Provider: "mise-bump", Status: plan.StatusHeld}, safetyFinding{
		Provider:          "mise",
		Kind:              "npm",
		Name:              "npm:demo",
		Decision:          "hold",
		Reason:            "mise minimum_release_age held candidate before it appeared in normal outdated output",
		ReleaseDate:       releasedAt,
		MinReleaseAgeDays: 3,
	})
	for _, want := range []string{"リリース日:", "経過", "最小 3日", "解除目安", "あと約"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("expected release-age availability detail to include %q:\n%s", want, detail)
		}
	}
	if !strings.Contains(detail, "hold: 経過") {
		t.Fatalf("expected native release-age hold detail to show days before the long reason:\n%s", detail)
	}
}

func TestListDetailRowsIncludeItemsAndToolSections(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Limit:  1,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true, Detail: "JSON processor"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows: []toolRow{
				{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				{Name: "python", Version: "3.13.0", Wanted: "latest", State: "inactive", Detail: "Python runtime"},
			},
		}},
	}
	rows := listDetailRows(report)
	if len(rows) != 2 {
		t.Fatalf("expected item plus limited tool detail rows, got %#v", rows)
	}
	if rows[0].Title != "brew/brew jq" || rows[1].Title != "mise / runtime node" {
		t.Fatalf("unexpected detail row titles: %#v", rows)
	}
	if !strings.Contains(strings.Join(rows[1].Metadata, " "), "wanted: lts") {
		t.Fatalf("expected mise wanted version metadata, got %#v", rows[1])
	}
	if !strings.Contains(rows[0].Detail, "description: JSON processor") || !strings.Contains(rows[0].Detail, "identity: brew / brew / jq") || !strings.Contains(rows[0].Detail, "status: ok - desired and installed") {
		t.Fatalf("expected inventory item detail to explain status and management state, got %#v", rows[0])
	}
	if !strings.Contains(strings.Join(rows[0].Metadata, " "), "name: jq") {
		t.Fatalf("expected inventory item metadata to include item identity, got %#v", rows[0])
	}
	renderedInventoryDetail := strings.Join(detailBrowserExpandedLinesStyled(rows[0], 80, true), "\n")
	for _, want := range []string{"\033[1m\033[35mdetails", "\033[36mdescription:", "\033[36midentity:", "\033[36mstatus:", "\033[1m\033[35mevidence", "\033[36mprovider:"} {
		if !strings.Contains(renderedInventoryDetail, want) {
			t.Fatalf("expected rendered inventory detail to contain %q:\n%q", want, renderedInventoryDetail)
		}
	}
	if len(rows[0].Actions) != 0 {
		t.Fatalf("did not expect backend route without backend evidence, got %#v", rows[0].Actions)
	}
}

func TestListTableSectionsConvertBrewItemsToExpandableRows(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Category: "work", Name: "jq", Version: "1.8.1", Status: plan.StatusOK, Desired: true, Live: true, Detail: "JSON processor"},
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Version: "1.100.0", Status: plan.StatusExtra, Live: true, Detail: "Editor"},
		},
	}
	sections := listTableSections(report)
	if len(sections) != 2 || sections[0].Title != "brew / brew / work" || sections[1].Title != "brew / cask / personal" {
		t.Fatalf("expected brew item sections, got %#v", sections)
	}
	if sections[0].Rows[0].Name != "jq" || sections[0].Rows[0].State != "ok" || !strings.Contains(sections[0].Rows[0].Detail, "description: JSON processor") || !strings.Contains(sections[0].Rows[0].Detail, "identity: brew / brew / jq") {
		t.Fatalf("expected brew item detail to include rich inventory context, got %#v", sections[0].Rows[0])
	}
	if len(sections[0].Rows[0].Actions) != 0 {
		t.Fatalf("did not expect backend route without backend evidence, got %#v", sections[0].Rows[0].Actions)
	}
	model := newToolTableBrowserModel("updev list brew", sections, detailBrowserState{}, false)
	view := model.View().Content
	for _, want := range []string{"brew / brew / work", "jq", "JSON processor", "brew / cask / personal", "visual-studio-code"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected brew table browser to include %q:\n%s", want, view)
		}
	}
	model.ToggleSelected()
	view = model.View().Content
	for _, want := range []string{"detail", "description: JSON processor", "identity: brew / brew / jq", "status: ok - desired and installed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded installed inventory row to include %q:\n%s", want, view)
		}
	}
	for _, duplicate := range []string{"metadata", "version: 1.8.1", "state: ok"} {
		if strings.Contains(view, duplicate) {
			t.Fatalf("did not expect expanded installed inventory row to repeat table metadata %q:\n%s", duplicate, view)
		}
	}
}

func TestListTableSectionsGroupManualInstalledAppsForDisplay(t *testing.T) {
	report := listReport{
		Filters: map[string]string{"provider": manualProviderName},
		Sections: []toolSection{{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows: []toolRow{{
				Name:   "Store App",
				State:  "installed",
				Detail: "source: mac app store receipt; path: /Applications/Store App.app",
			}, {
				Name:   "Cask App",
				State:  "brew",
				Detail: "source: homebrew cask; cask: cask-app",
			}, {
				Name:   "Vendor App",
				State:  "installed",
				Detail: "source: app bundle; path: /Applications/Vendor App.app",
			}, {
				Name:   "Ignored App",
				State:  "ignored",
				Detail: "source: app bundle; path: /Applications/Ignored App.app",
			}},
		}},
	}
	sections := listTableSections(report)
	if len(sections) != 4 {
		t.Fatalf("expected manual installed app display groups, got %#v", sections)
	}
	wants := []struct {
		name string
		row  string
	}{
		{"manual/installed-app-store", "Store App"},
		{"manual/installed-homebrew-casks", "Cask App"},
		{"manual/installed-overrides", "Ignored App"},
		{"manual/installed-vendor-apps", "Vendor App"},
	}
	for index, want := range wants {
		if sections[index].Name != want.name || len(sections[index].Rows) != 1 || sections[index].Rows[0].Name != want.row {
			t.Fatalf("unexpected manual group at %d: got %#v want name=%q row=%q", index, sections[index], want.name, want.row)
		}
	}
}

func TestManualSectionRowsRouteToManualReview(t *testing.T) {
	sections := manualCaskSections([]plan.Item{{Provider: "brew", Kind: "cask", Name: "demo-app", Status: plan.StatusOK}})
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one manual cask row, got %#v", sections)
	}
	if len(sections[0].Rows[0].Actions) != 1 || !toolRowHasRouteAction(sections[0].Rows[0], listHubActionManual) {
		t.Fatalf("expected manual cask row to route to manual review, got %#v", sections[0].Rows[0].Actions)
	}
	detail := toolDetailRow(sections[0], sections[0].Rows[0])
	if len(detail.Actions) != 1 || !detailRowHasRouteAction(detail, listHubActionManual) || !strings.Contains(strings.Join(detail.Metadata, " "), "next action:") {
		t.Fatalf("expected manual detail row to preserve review routing action and evidence, got %#v", detail)
	}
}

func TestListFilteredActionRoutesDomainActionsBackToHub(t *testing.T) {
	defaultAction := listHubActionProvider
	pendingAction := ""
	handled, exit := handleListFilteredAction(listHubActionBackends, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionBackends || pendingAction != listHubActionBackends {
		t.Fatalf("expected backend row action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}

	handled, exit = handleListFilteredAction(updevActionHome, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionFull {
		t.Fatalf("expected home action to reset default action, handled=%v exit=%v default=%q", handled, exit, defaultAction)
	}

	handled, exit = handleListFilteredAction(updevActionExit, true, &defaultAction, &pendingAction)
	if !handled || !exit {
		t.Fatalf("expected exit action to exit, handled=%v exit=%v", handled, exit)
	}
}

func TestListFilteredActionRoutesInventoryToggleActions(t *testing.T) {
	defaultAction := listHubActionFull
	pendingAction := ""
	handled, exit := handleListFilteredAction(listHubActionManual, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionManual || pendingAction != listHubActionManual {
		t.Fatalf("expected manual toggle action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}

	handled, exit = handleListFilteredAction(listHubActionFull, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionFull || pendingAction != listHubActionFull {
		t.Fatalf("expected installed toggle action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}
}

func TestListTableBrowserViewToggleLabelsAreOptIn(t *testing.T) {
	defaultLabels := tableBrowserLabels()
	if strings.Contains(defaultLabels.ControlsHelp, "Tab") {
		t.Fatalf("default table browser labels should not mention view toggle: %q", defaultLabels.ControlsHelp)
	}

	toggleLabels := tableBrowserLabelsWithViewToggle()
	if !strings.Contains(toggleLabels.ControlsHelp, "Tab") {
		t.Fatalf("expected list toggle controls to mention Tab: %q", toggleLabels.ControlsHelp)
	}
	if !strings.Contains(strings.Join(toggleLabels.HelpLines, "\n"), "installed") {
		t.Fatalf("expected list toggle help to mention installed/manual switch: %#v", toggleLabels.HelpLines)
	}
}

func TestListRowsRouteToCachedUpdateAndSecurityEvidence(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	saveLastUpdateReport(updateReport{
		Root: root,
		Steps: []updateStep{{
			Name:         "brew",
			Status:       plan.StatusHeld,
			Reason:       "security review required",
			Updated:      []string{"jq"},
			SkippedItems: []string{"ripgrep"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "ripgrep",
				Decision: "hold",
				Reason:   "new release is inside minimum age",
			}},
		}},
	})
	report := buildListReport(inventoryResult{Report: plan.Report{
		Root: root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}}, listOptions{root: root})
	sections := listTableSections(report)
	rowsByName := map[string]toolRow{}
	for _, section := range sections {
		for _, row := range section.Rows {
			rowsByName[section.Name+"/"+row.Name] = row
		}
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/jq"], listHubActionUpdates) || strings.Contains(rowsByName["brew/brew/jq"].Detail, "security evidence:") {
		t.Fatalf("expected jq row to route only to update evidence, got %#v", rowsByName["brew/brew/jq"])
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionUpdates) || !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionSecurity) || !strings.Contains(rowsByName["brew/brew/ripgrep"].Detail, "security evidence:") {
		t.Fatalf("expected ripgrep row to route to update and security evidence, got %#v", rowsByName["brew/brew/ripgrep"])
	}
	if toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionUpdates) || toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionSecurity) {
		t.Fatalf("did not expect brew evidence to attach to same-name mise row, got %#v", rowsByName["mise/tool/ripgrep"])
	}
	details := listDetailRows(report)
	byTitle := map[string]detailBrowserRow{}
	for _, row := range details {
		byTitle[row.Title] = row
	}
	rg := byTitle["brew/brew ripgrep"]
	if !detailRowHasRouteAction(rg, listHubActionSecurity) || !strings.Contains(strings.Join(rg.Metadata, " "), "security evidence:") {
		t.Fatalf("expected ripgrep detail row to expose security route evidence, got %#v", rg)
	}
}

func TestListRowsRouteMiseBumpEvidenceToMiseTool(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	lastReport := updateReport{
		Root: root,
		Steps: []updateStep{{
			Name:    miseBumpProvider,
			Status:  plan.StatusOK,
			Updated: []string{"github:openai/codex 0.60.0 -> 0.60.1"},
		}},
		Safety: []safetyGate{{
			Provider: miseBumpProvider,
			Status:   plan.StatusOK,
			Findings: []safetyFinding{{
				Provider:          "mise",
				Kind:              "tool",
				Name:              "github:openai/codex",
				InstalledVersions: []string{"0.60.0"},
				CurrentVersion:    "0.60.1",
				Decision:          "allow",
				Reason:            "mise pinned-version bump candidate passed release-age and provenance checks",
				Source:            miseBumpSource,
			}},
		}},
	}
	saveLastUpdateReport(lastReport)
	report := buildListReport(inventoryResult{Report: plan.Report{
		Root: root,
		Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "github:openai/codex",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
	}}, listOptions{root: root})
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one mise row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if !toolRowHasRouteAction(row, listHubActionUpdates) || !toolRowHasRouteAction(row, listHubActionSecurity) {
		t.Fatalf("expected mise bump row to route to update and security evidence, got %#v", row.Actions)
	}
	if !strings.Contains(row.Detail, "update evidence:") || !strings.Contains(row.Detail, "security evidence:") {
		t.Fatalf("expected mise bump evidence in row detail, got %q", row.Detail)
	}
	detailRows := updateSecurityDetailRowsForFilter(lastReport, lastReportOptions{section: "security", query: "github:openai/codex"})
	if len(detailRows) != 1 || !detailRowHasActionPrefix(detailRows[0], miseBumpDetailActionPrefix+"\t") {
		t.Fatalf("expected query-filtered safe bump detail row with apply action, got %#v", detailRows)
	}
}

func detailRowHasActionPrefix(row detailBrowserRow, prefix string) bool {
	for _, action := range row.Actions {
		if strings.HasPrefix(action.Value, prefix) {
			return true
		}
	}
	return false
}

func TestListRowsShowHoldBadgeForStrictSecurityReviewEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	saveLastUpdateReport(updateReport{
		Status:   plan.StatusHeld,
		Root:     root,
		Security: "strict",
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "cask",
				Name:     "firefox",
				Decision: "review",
				Reason:   "Homebrew cask needs vendor provenance review before update",
			}},
		}},
	})
	report := buildListReport(inventoryResult{Report: plan.Report{
		Root: root,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "firefox",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
	}}, listOptions{root: root})
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	rendered := reviewui.StyledRow(row, false, false)
	if len(rendered) != 5 || rendered[3] != "▶sec" {
		t.Fatalf("expected strict review evidence to render as held, got %#v", rendered)
	}
	if !strings.Contains(row.Detail, "security evidence:") || !strings.Contains(row.Detail, "held (decision: review)") {
		t.Fatalf("expected detail to preserve strict-held review decision, got %q", row.Detail)
	}
}

func TestListRowsExposeBackendRouteOnlyWithMatchingEvidence(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
			Type:            "homebrew-to-mise",
			Provider:        "brew",
			Kind:            "brew",
			Name:            "ripgrep",
			RecommendedName: "ripgrep",
			RecommendedSpec: "15.1.0",
			Reason:          "ripgrep is already a stable mise-managed CLI",
		}}}),
	}
	sections := listTableSections(report)
	rowsByName := map[string]toolRow{}
	for _, section := range sections {
		for _, row := range section.Rows {
			rowsByName[section.Name+"/"+row.Name] = row
		}
	}
	if toolRowHasRouteAction(rowsByName["brew/brew/jq"], listHubActionBackends) {
		t.Fatalf("did not expect unrelated jq row to expose backend route: %#v", rowsByName["brew/brew/jq"])
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionBackends) || !strings.Contains(rowsByName["brew/brew/ripgrep"].Detail, "backend evidence:") {
		t.Fatalf("expected ripgrep row to expose focused backend route and evidence: %#v", rowsByName["brew/brew/ripgrep"])
	}
	if !toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionBackends) || !strings.Contains(rowsByName["mise/tool/ripgrep"].Detail, "backend evidence:") {
		t.Fatalf("expected recommended mise row to expose backend route and evidence: %#v", rowsByName["mise/tool/ripgrep"])
	}
}

func TestListSectionsExposeBackendRoutesForRichToolRows(t *testing.T) {
	report := listReport{
		Sections: []toolSection{{
			Name:  "mise/cargo",
			Title: "mise / cargo",
			Rows: []toolRow{{
				Name:    "cargo:fd-find",
				Version: "10.4.2",
				State:   "active",
				Detail:  "fd via cargo",
			}},
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
			Type:                "mise-backend-rewrite",
			Provider:            "mise",
			Kind:                "tool",
			Name:                "cargo:fd-find",
			Current:             "cargo:fd-find",
			RecommendedProvider: "mise",
			RecommendedName:     "aqua:sharkdp/fd",
			CommandNames:        []string{"fd"},
			Reason:              "aqua prebuilt CLI is preferred over a cargo global build",
		}}}),
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one enriched section row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if !toolRowHasRouteAction(row, listHubActionBackends) || !strings.Contains(row.Detail, "backend evidence:") {
		t.Fatalf("expected rich mise section row to expose backend route and evidence, got %#v", row)
	}
}

func TestManualRowsExposeUpdateAndSecurityRoutesFromCaskEvidence(t *testing.T) {
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	for _, key := range listEvidenceUpdateItemKeys("brew", "cask demo-app") {
		listEvidenceAdd(evidence.Updates, key, "brew held: release age gate")
	}
	for _, key := range listEvidenceFindingKeys(safetyGate{Provider: "brew"}, safetyFinding{Provider: "brew", Kind: "cask", Name: "demo-app", Decision: "hold", Reason: "release too new"}) {
		listEvidenceAdd(evidence.Security, key, "brew/cask demo-app: hold")
	}
	report := listReport{
		Sections: []toolSection{{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows: []toolRow{{
				Name:   "Demo App",
				State:  "brew",
				Detail: "source: app bundle; cask: demo-app",
			}},
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one manual section row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if !toolRowHasRouteAction(row, listHubActionUpdates) || !toolRowHasRouteAction(row, listHubActionSecurity) {
		t.Fatalf("expected manual cask evidence row to expose update and security routes, got %#v", row.Actions)
	}
	if !strings.Contains(row.Detail, "update evidence:") || !strings.Contains(row.Detail, "security evidence:") {
		t.Fatalf("expected manual cask evidence row detail to include update and security evidence, got %q", row.Detail)
	}
}

func TestBackendDetailRowsForListRouteFocusesMatchingItem(t *testing.T) {
	report := backendPlanReport{Findings: []backendFinding{
		{Type: "homebrew-to-mise", Provider: "brew", Kind: "brew", Name: "ripgrep", RecommendedName: "ripgrep", RecommendedSpec: "15.1.0", Reason: "ripgrep can move"},
		{Type: "homebrew-to-mise", Provider: "brew", Kind: "brew", Name: "jq", RecommendedName: "jq", RecommendedSpec: "1.8.1", Reason: "jq can move"},
	}}
	rows := backendDetailRowsForListRoute(report, listRouteAction{Domain: listHubActionBackends, Provider: "brew", Kind: "brew", Name: "ripgrep"})
	if len(rows) != 1 || !strings.Contains(rows[0].Title, "ripgrep") {
		t.Fatalf("expected focused ripgrep backend row, got %#v", rows)
	}
}

func toolRowHasAction(row toolRow, value string) bool {
	for _, action := range row.Actions {
		if action.Value == value {
			return true
		}
	}
	return false
}

func toolRowHasRouteAction(row toolRow, domain string) bool {
	for _, action := range row.Actions {
		if route, ok := parseListRouteAction(action.Value); ok && route.Domain == domain {
			return true
		}
	}
	return false
}

func detailRowHasAction(row detailBrowserRow, value string) bool {
	for _, action := range row.Actions {
		if action.Value == value {
			return true
		}
	}
	return false
}

func detailRowHasRouteAction(row detailBrowserRow, domain string) bool {
	for _, action := range row.Actions {
		if route, ok := parseListRouteAction(action.Value); ok && route.Domain == domain {
			return true
		}
	}
	return false
}

func TestStyledToolRowColorsProviderStatuses(t *testing.T) {
	ok := styledToolRow(toolRow{Name: "jq", Version: "1.8.1", State: "ok", Detail: "JSON processor"}, false, true)
	if !strings.Contains(ok[1], "\033[32m") || !strings.Contains(ok[2], "\033[32m") || !strings.Contains(ok[4], "\033[36m") {
		t.Fatalf("expected ok row to color version/status/detail, got %#v", ok)
	}
	if strings.Contains(strings.Join(ok, " "), "\033[2m") {
		t.Fatalf("did not expect ok row to be dimmed, got %#v", ok)
	}

	extra := styledToolRow(toolRow{Name: "visual-studio-code", Version: "1.100.0", State: "extra", Detail: "installed but not desired"}, false, true)
	if !strings.Contains(extra[2], "\033[33m") || !strings.Contains(extra[4], "\033[33m") {
		t.Fatalf("expected extra status/detail to be warning-colored, got %#v", extra)
	}

	inactive := styledToolRow(toolRow{Name: "python", Version: "3.13.0", Wanted: "latest", State: "inactive", Detail: "Python runtime"}, true, true)
	if !strings.Contains(inactive[1], "\033[2m") || !strings.Contains(inactive[2], "\033[2m") || !strings.Contains(inactive[5], "\033[2m") {
		t.Fatalf("expected inactive version/wanted/detail to stay dimmed, got %#v", inactive)
	}
}

func TestInventoryItemHelpersColorAttentionRows(t *testing.T) {
	extraVersion := styleInventoryItemVersion("2026-05-14", plan.StatusExtra, true)
	extraDetail := styleInventoryItemDetail("installed but not desired", plan.StatusExtra, true)
	if !strings.Contains(extraVersion, "\033[33m") || !strings.Contains(extraDetail, "\033[33m") {
		t.Fatalf("expected extra inventory row fields to be warning-colored, got version=%q detail=%q", extraVersion, extraDetail)
	}
	okDetail := styleInventoryItemDetail("JSON processor", plan.StatusOK, true)
	if !strings.Contains(okDetail, "\033[36m") {
		t.Fatalf("expected ok detail to be label-colored, got %q", okDetail)
	}
}

func TestDetailBrowserModelTogglesAndRendersExpandedDetail(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Status: "ok", Summary: "short", Detail: "full detail"},
		{Title: "two", Status: "held", Summary: "summary", Detail: "second detail", Metadata: []string{"updated: jq; git", "applyability: review-only"}, Actions: []detailBrowserAction{{Value: "demo", Label: "review", Description: "inspect evidence"}}},
	}, detailBrowserState{}, false)
	model.move(1)
	if model.State.Selected != 1 {
		t.Fatalf("expected selected row 1, got %d", model.State.Selected)
	}
	model.toggleSelected()
	if !model.State.Expanded[1] {
		t.Fatalf("expected selected row to be expanded: %#v", model.State)
	}
	view := model.View()
	if !view.AltScreen {
		t.Fatal("expected detail browser to use alt screen for stable mouse coordinates")
	}
	for _, want := range []string{"Enter/Space expand", "> - held two", "[actions:1]", "[updated:2]", "[review-only]", "focused actions: a/1=review", "details", "evidence", "actions", "action 1 [press a or 1]: review", "detail: second detail", "mouse=off"} {
		if !strings.Contains(view.Content, want) {
			t.Fatalf("expected detail browser view to contain %q:\n%s", want, view.Content)
		}
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}))
	model = updated.(detailBrowserModel)
	if !strings.Contains(model.View().Content, "> action 1 [press a or 1]: review") {
		t.Fatalf("expected down key to focus expanded action:\n%s", model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if model.State.Action != "demo" {
		t.Fatalf("expected Enter on focused action to select action, got %#v", model.State)
	}
	coloredLines := strings.Join(detailBrowserExpandedLinesStyled(detailBrowserRow{
		Detail:   "second detail",
		Metadata: []string{"updated: jq; git"},
		Actions:  []detailBrowserAction{{Value: "demo", Label: "review", Description: "inspect evidence"}},
	}, 80, true), "\n")
	for _, want := range []string{"\033[1m\033[35mdetails", "\033[36mdetail:", "\033[1m\033[35mevidence", "\033[36mupdated:", "\033[1m\033[35mactions", "\033[36maction 1 [press a or 1]:", "\033[32mreview"} {
		if !strings.Contains(coloredLines, want) {
			t.Fatalf("expected colored detail lines to contain %q:\n%q", want, coloredLines)
		}
	}
}

func TestDetailBrowserKeepsExpandedDetailVisibleNearBottom(t *testing.T) {
	rows := []detailBrowserRow{}
	for i := 0; i < 12; i++ {
		detail := "detail"
		if i == 10 {
			detail = "description: expanded row\nidentity: manual / app / bottom\nstatus: needs-review\nnext action: open manual review"
		}
		rows = append(rows, detailBrowserRow{Title: fmt.Sprintf("row-%02d", i), Status: "ok", Detail: detail})
	}
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	model.Height = 14
	model.move(10)
	model.toggleSelected()
	view := model.View().Content
	for _, want := range []string{"row-10", "description: expanded row", "identity: manual / app / bottom", "next action: open manual review"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded bottom detail row to stay visible with %q:\n%s", want, view)
		}
	}
}

func TestDetailBrowserActionKeysReturnFocusedRowAction(t *testing.T) {
	rows := []detailBrowserRow{{
		Title:   "action row",
		Status:  "held",
		Summary: "needs action",
		Actions: []detailBrowserAction{
			{Value: "first", Label: "first action"},
			{Value: "second", Label: "second action"},
		},
	}}
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(detailBrowserModel)
	if model.State.Action != "first" {
		t.Fatalf("expected a to select first row action, got %#v", model.State)
	}

	model = newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))
	model = updated.(detailBrowserModel)
	if model.State.Action != "second" {
		t.Fatalf("expected 2 to select second row action, got %#v", model.State)
	}
}

func TestDetailBrowserFocusedActionHintFitsTerminalWidth(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{{
		Title:  "long actions",
		Status: "held",
		Actions: []detailBrowserAction{
			{Value: "one", Label: "a deliberately long action label that should not overflow the header"},
		},
	}}, detailBrowserState{}, false)
	model.Width = 36
	view := model.View().Content
	if !strings.Contains(view, "focused actions:") || !strings.Contains(view, "…") {
		t.Fatalf("expected focused action hint to be truncated to terminal width:\n%s", view)
	}
}

func TestDetailBrowserReservesFocusedActionHintLine(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{{
		Title:   "backend review",
		Status:  "drift",
		Summary: "needs action",
		Actions: []detailBrowserAction{{
			Value: "backend",
			Label: "open backend review",
		}},
	}, {
		Title:   "already aligned",
		Status:  "ok",
		Summary: "no action",
	}}, detailBrowserState{}, false)
	firstView := model.View().Content
	model.move(1)
	secondView := model.View().Content
	if !strings.Contains(firstView, "focused actions: a/1=open backend review") {
		t.Fatalf("expected first focused row to show action hint:\n%s", firstView)
	}
	if strings.Contains(secondView, "focused actions:") {
		t.Fatalf("expected second focused row to have no action hint:\n%s", secondView)
	}
	if firstIndex, secondIndex := detailViewLineIndex(firstView, "drift backend review"), detailViewLineIndex(secondView, "drift backend review"); firstIndex != secondIndex {
		t.Fatalf("expected detail rows to stay stable, got first=%d second=%d\nfirst:\n%s\nsecond:\n%s", firstIndex, secondIndex, firstView, secondView)
	}
	if firstLines, secondLines := strings.Count(firstView, "\n"), strings.Count(secondView, "\n"); firstLines != secondLines {
		t.Fatalf("expected view line count to stay stable, got first=%d second=%d\nfirst:\n%s\nsecond:\n%s", firstLines, secondLines, firstView, secondView)
	}
}

func detailViewLineIndex(text string, needle string) int {
	for index, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return index
		}
	}
	return -1
}

func TestDogfoodDetailRowsSelectManualBackendSecurityAndDashboardActions(t *testing.T) {
	manualRows := []detailBrowserRow{manualPlanDetailRow(manualPlanItem{
		Name:       "Google Sheets",
		State:      "installed",
		Action:     "needs-review",
		Confidence: "medium",
		NextStep:   "accept, edit, or ignore one explicit override after ownership review",
	}, "")}
	if action := selectedDetailActionForKey(manualRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'})); !strings.HasPrefix(action, manualPlanDetailActionPrefix+"\taccept\tGoogle Sheets") {
		t.Fatalf("expected manual detail row to select accept action, got %q", action)
	}

	backendRows := backendDetailRows(backendPlanReport{Findings: []backendFinding{{
		Type:            "mise-backend-rewrite",
		Name:            "cargo:broot",
		RecommendedName: "github:Canop/broot",
		RewriteAllowed:  true,
	}}})
	if action, current, recommended, ok := parseBackendDetailAction(selectedDetailActionForKey(backendRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))); !ok || action != "rewrite-mise" || current != "cargo:broot" || recommended != "github:Canop/broot" {
		t.Fatalf("expected backend detail row to select rewrite action, action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}

	securityRows := updateSecurityDetailRows(updateReport{Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{Provider: "brew", Kind: "cask", Name: "demo", Decision: "hold"}},
	}}})
	if action, provider, kind, name, ok := parseSecurityDetailAction(selectedDetailActionForKey(securityRows, tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))); !ok || action != "allow-custom-rerun" || provider != "brew" || kind != "cask" || name != "demo" {
		t.Fatalf("expected security detail row to select custom allow rerun action, action=%q provider=%q kind=%q name=%q ok=%v", action, provider, kind, name, ok)
	}

	dashboardRows := updateDashboardDetailRows(updateReport{Status: plan.StatusOK}, inventoryPlanReport{}, backendPlanReport{})
	if action := selectedDetailActionForKey(dashboardRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'})); action != updateHubActionUpdatesFilter {
		t.Fatalf("expected dashboard focused row to select update filter action, got %q", action)
	}
	model := newDetailBrowserModel(updateHubTitle(updateReport{Status: plan.StatusOK}), dashboardRows, detailBrowserState{}, false)
	model.PrimaryEnterAction = true
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if model.State.Action != updateHubActionUpdatesFilter {
		t.Fatalf("expected dashboard Enter to select focused primary action, got %q", model.State.Action)
	}
}

func TestUpdateHubRouterBackReturnsFromDetailToDashboard(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionLogs, updateHubActionLogs, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected router to start in logs detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back to return to dashboard, screen=%q\n%s", model.screen, model.View().Content)
	}
	if !strings.Contains(model.View().Content, "updev update ok") {
		t.Fatalf("expected dashboard view after Back:\n%s", model.View().Content)
	}
}

func TestUpdateHubRouterClearsDashboardActionAfterReturning(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected router to start on dashboard, screen=%q", model.screen)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected dashboard action to open logs detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back from logs to return to dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Down after Back to stay on dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterBackPreservesDashboardFocus(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{Name: "brew", Status: plan.StatusOK}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(updateHubRouterModel)
	selected := model.dashboard.State.Selected
	lineIndex := model.dashboard.selectedLineIndex()
	if lineIndex < 0 || model.dashboard.Lines[lineIndex].Action == "" || selected == 0 {
		t.Fatalf("expected dashboard focus to move before opening route, selected=%d line=%d", selected, lineIndex)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen == updateHubRouterDashboard {
		t.Fatalf("expected Enter to open a routed view")
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard || model.dashboard.State.Selected != selected {
		t.Fatalf("expected Back to preserve dashboard focus selected=%d, got screen=%q selected=%d", selected, model.screen, model.dashboard.State.Selected)
	}
}

func TestUpdateHubRouterBackKeepsDashboardTopAnchored(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Security: "strict",
		Steps: []updateStep{{
			Name:   "mise-bump",
			Status: plan.StatusHeld,
			SkippedItems: []string{
				"aqua:modem-dev/hunk 0.14.0 -> 0.14.1",
				"cloudflared 2026.5.0 -> 2026.5.2",
				"copilot-cli 1.0.48 -> 1.0.61",
				"fzf 0.72.0 -> 0.73.1",
				"go 1.26.3 -> 1.26.4",
				"lazygit 0.61.1 -> 0.62.2",
				"node 24.16.0 -> 26.3.0",
				"rust 1.95.0 -> 1.96.0",
				"uv 0.11.14 -> 0.11.19",
			},
		}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.height = 10
	model.applyDashboardSize(&model.dashboard)
	for range 8 {
		updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
		model = updated.(updateHubRouterModel)
	}
	selected := model.dashboard.State.Selected
	if model.dashboard.State.Offset == 0 {
		t.Fatalf("expected low dashboard focus to scroll before opening a route")
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen == updateHubRouterDashboard {
		t.Fatalf("expected Enter to open a routed view")
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	view := model.View().Content
	if model.screen != updateHubRouterDashboard || model.dashboard.State.Selected != selected {
		t.Fatalf("expected Back to preserve dashboard focus selected=%d, got screen=%q selected=%d", selected, model.screen, model.dashboard.State.Selected)
	}
	if model.dashboard.State.Offset != 0 || !strings.Contains(view, "root: /repo") {
		t.Fatalf("expected Back to keep dashboard top anchored, offset=%d\n%s", model.dashboard.State.Offset, view)
	}
}

func TestUpdateHubRouterOpensFullReportWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionFull, updateHubActionFull, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "full" {
		t.Fatalf("expected router to open full report detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev full report") || !strings.Contains(view, "cached update report") {
		t.Fatalf("expected full report detail view:\n%s", view)
	}
}

func TestUpdateHubRouterOpensBackendTableWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "bat",
		RecommendedProvider: "mise",
		RecommendedName:     "bat",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	if model.screen != updateHubRouterTable || model.stateKey != "backends" {
		t.Fatalf("expected router to open backend table, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev backend convergence") || !strings.Contains(view, "bat") {
		t.Fatalf("expected backend table view:\n%s", view)
	}
}

func TestUpdateHubRouterRefreshesReviewPlansAsynchronously(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	initialView := model.View().Content
	if !strings.Contains(initialView, "loading - preparing manual") || !strings.Contains(initialView, "loading - preparing backend") {
		t.Fatalf("expected dashboard to show review plan loading rows:\n%s", initialView)
	}
	for _, want := range []string{"review actions", "action", "description"} {
		if !strings.Contains(initialView, want) {
			t.Fatalf("expected loading review action table to contain %q:\n%s", want, initialView)
		}
	}
	if model.dashboard.selectedLineIndex() < 0 || model.dashboard.Lines[model.dashboard.selectedLineIndex()].Action != updateHubActionLogs || model.dashboard.State.Offset != 0 {
		t.Fatalf("expected initial loading dashboard to stay top-anchored on update details, selected=%d offset=%d lines=%#v", model.dashboard.selectedLineIndex(), model.dashboard.State.Offset, model.dashboard.Lines)
	}
	manualPlan := inventoryPlanReport{
		Status:         plan.StatusDrift,
		AttentionCount: 1,
		Items:          []manualPlanItem{{Name: "Vendor App", Action: "needs-review"}},
		ActionCounts:   map[string]int{"needs-review": 1},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	updated, _ := model.Update(updateHubManualPlanMsg{Report: manualPlan})
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(updateHubBackendPlanMsg{Report: backendPlan})
	model = updated.(updateHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "loading - preparing") || !strings.Contains(readyView, "needs-review=1") || !strings.Contains(readyView, "homebrew-to-mise=1") {
		t.Fatalf("expected dashboard to refresh review plan rows:\n%s", readyView)
	}
}

func TestUpdateHubRouterKeepsReviewRoutesLoadingUntilAsyncPlanArrives(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, updateHubActionManualPlan, updateHubActionManualPlan, false)
	if model.screen != updateHubRouterTable || model.stateKey != "manual-plan" || !model.manualLoading {
		t.Fatalf("expected manual review route to remain loading, screen=%q state=%q manualLoading=%v", model.screen, model.stateKey, model.manualLoading)
	}
	if view := model.View().Content; !strings.Contains(view, "manual review loading") {
		t.Fatalf("expected manual review loading title:\n%s", view)
	}

	model = newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, updateHubActionBackends, updateHubActionBackends, false)
	if model.screen != updateHubRouterTable || model.stateKey != "backends" || !model.backendLoading {
		t.Fatalf("expected backend route to remain loading, screen=%q state=%q backendLoading=%v", model.screen, model.stateKey, model.backendLoading)
	}
	if view := model.View().Content; !strings.Contains(view, "backend evidence loading") {
		t.Fatalf("expected backend loading title:\n%s", view)
	}
}

func TestUpdateHubRouterShowsJapaneseReviewActionColumnsWhileLoading(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	view := model.View().Content
	for _, want := range []string{"確認アクション", "操作", "説明", "loading - 手動/vendor app 候補を準備中", "loading - backend evidence を準備中"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected Japanese loading review action table to contain %q:\n%s", want, view)
		}
	}
}

func TestUpdateHubRouterAsyncPlanBuildersUseContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var manualContextCanceled bool
	var backendContextCanceled bool
	builders := updateHubPlanBuilders{
		Manual: func(ctx context.Context, root string) inventoryPlanReport {
			manualContextCanceled = ctx.Err() != nil
			return canceledUpdateHubManualPlan(root)
		},
		Backend: func(ctx context.Context, root string) backendPlanReport {
			backendContextCanceled = ctx.Err() != nil
			return canceledUpdateHubBackendPlan(root)
		},
	}
	model := newUpdateHubRouterModelWithContext(ctx, builders, updateReport{Status: plan.StatusOK, Root: "/repo"}, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected async plan commands")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected two async plan commands, got %#v", msg)
	}
	var manualMsg updateHubManualPlanMsg
	var backendMsg updateHubBackendPlanMsg
	for _, batchCmd := range batch {
		switch got := batchCmd().(type) {
		case updateHubManualPlanMsg:
			manualMsg = got
		case updateHubBackendPlanMsg:
			backendMsg = got
		default:
			t.Fatalf("unexpected async plan message: %#v", got)
		}
	}
	if !manualContextCanceled || !backendContextCanceled {
		t.Fatalf("expected canceled context in async builders, manual=%v backend=%v", manualContextCanceled, backendContextCanceled)
	}
	if manualMsg.Report.Status != plan.StatusHeld || backendMsg.Report.Status != plan.StatusHeld {
		t.Fatalf("expected canceled partial reports, manual=%#v backend=%#v", manualMsg.Report, backendMsg.Report)
	}
}

func TestUpdateHubCanceledPlanReportsExplainPartialResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manualPlan := buildUpdateHubManualPlanWithContext(ctx, "/repo")
	backendPlan := buildUpdateHubBackendPlanWithContext(ctx, "/repo")
	if manualPlan.Status != plan.StatusHeld || !strings.Contains(strings.Join(manualPlan.NextSteps, " "), "canceled") {
		t.Fatalf("expected held manual cancel report, got %#v", manualPlan)
	}
	if backendPlan.Status != plan.StatusHeld || !strings.Contains(strings.Join(backendPlan.Warnings, " "), "canceled") {
		t.Fatalf("expected held backend cancel report, got %#v", backendPlan)
	}
}

func TestUpdateHubRouterUpdateFilterStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" || !model.detail.PrimaryEnterAction {
		t.Fatalf("expected update filter menu inside router, screen=%q state=%q primary=%v", model.screen, model.stateKey, model.detail.PrimaryEnterAction)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:provider:brew" {
		t.Fatalf("expected Enter to open filtered update evidence, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") {
		t.Fatalf("expected filtered update detail to include provider evidence:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from filtered evidence to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}, {
		Name:   "mise",
		Status: plan.StatusOK,
		Stdout: "mise output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("updates"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:updates" {
		t.Fatalf("expected update query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "brew", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:query:brew" {
		t.Fatalf("expected query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") || strings.Contains(view, "mise output") {
		t.Fatalf("expected query-filtered update detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from query result to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterSecurityQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo", Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "danger-app",
			Decision: "hold",
			Reason:   "unique-risk",
		}, {
			Provider: "brew",
			Kind:     "cask",
			Name:     "other-app",
			Decision: "hold",
			Reason:   "other reason",
		}},
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionSecurityFilter, updateHubActionSecurityFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("security"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:security" {
		t.Fatalf("expected security query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "unique-risk", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:security:query:unique-risk" {
		t.Fatalf("expected security query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "danger-app") || strings.Contains(view, "other-app") {
		t.Fatalf("expected query-filtered security detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:security" {
		t.Fatalf("expected Back from security query result to return to security filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterWriteConfirmationStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:            "mise-backend-rewrite",
		Name:            "cargo:broot",
		RecommendedName: "github:Canop/broot",
		RewriteAllowed:  true,
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	action := backendDetailActionValue("rewrite-mise", "cargo:broot", "github:Canop/broot")
	updated, _ := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterConfirm || !strings.HasPrefix(model.stateKey, "write-confirm:") {
		t.Fatalf("expected backend write confirmation inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "Rewrite mise backend") || !strings.Contains(view, "cargo:broot") {
		t.Fatalf("expected backend confirmation view:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != updateHubActionBackends {
		t.Fatalf("expected Back from confirmation to return to backend table, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterManualEditRemainsExternal(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionManualPlan, updateHubActionManualPlan, false)
	action := manualPlanDetailActionValue("edit", "Vendor App")
	updated, cmd := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.finalAction != action || cmd == nil {
		t.Fatalf("expected manual edit to remain an external action, final=%q cmdNil=%v", model.finalAction, cmd == nil)
	}
}

func TestListHubRouterTogglesInstalledAndManualWithoutSubprogram(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "ripgrep",
			Version:  "14.1.1",
			Status:   plan.StatusOK,
		}, {
			Provider: manualProviderName,
			Kind:     "app",
			Name:     "Vendor App",
			Status:   plan.StatusDrift,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionFull, nil, false)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected installed inventory table, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionManual {
		t.Fatalf("expected Tab to switch to manual table inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev list manual") || !strings.Contains(view, "Vendor App") {
		t.Fatalf("expected manual view after Tab:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "shift+tab", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Shift+Tab to switch back to installed inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterInventoryTabSwitchesToManualInventory(t *testing.T) {
	report := updateReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "ripgrep",
			Version:  "14.1.1",
			Status:   plan.StatusOK,
		}, {
			Provider: manualProviderName,
			Kind:     "app",
			Name:     "Vendor App",
			Status:   plan.StatusDrift,
		}}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionInventoryAll, updateHubActionInventoryAll, false)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected installed inventory table, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != listHubActionManual {
		t.Fatalf("expected Tab to switch to manual inventory inside update router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev list manual") || !strings.Contains(view, "Vendor App") {
		t.Fatalf("expected manual inventory view after Tab:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "shift+tab", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected Shift+Tab to switch back to installed inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterRefreshesSummaryInventoryBackendEvidenceAsynchronously(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}}},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, true, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: "brew"})
	if model.screen != updateHubRouterTable || !strings.HasPrefix(model.stateKey, "summary:") {
		t.Fatalf("expected summary inventory route, screen=%q state=%q", model.screen, model.stateKey)
	}
	initialView := model.View().Content
	if !strings.Contains(initialView, "backend evidence loading") || strings.Contains(initialView, "▶bak") || strings.Contains(initialView, "node") {
		t.Fatalf("expected summary inventory to show filtered loading view before backend evidence:\n%s", initialView)
	}
	updated, _ := model.Update(updateHubBackendPlanMsg{Report: backendPlan})
	model = updated.(updateHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "backend evidence loading") {
		t.Fatalf("expected summary inventory backend loading state to clear:\n%s", readyView)
	}
	if !strings.Contains(readyView, "▶bak") || !strings.Contains(readyView, "open backend review") || strings.Contains(readyView, "node") {
		t.Fatalf("expected summary inventory refresh to keep filter and add backend badge:\n%s", readyView)
	}
}

func TestUpdateHubRouterRouteBackReturnsToSummaryInventory(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}}},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: "brew"})
	updated, _ := model.handleAction(listRouteActionValueForTarget(listHubActionBackends, "brew", "brew", "ripgrep"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected backend route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || !strings.HasPrefix(model.stateKey, "summary:") {
		t.Fatalf("expected Back from backend route to return to summary inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "ripgrep") || !strings.Contains(view, "▶bak") || strings.Contains(view, "node") {
		t.Fatalf("expected summary inventory return to keep provider filter and backend badge:\n%s", view)
	}
}

func TestUpdateHubRouterSecurityRouteShowsAllowedFindingForQuery(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Security: "strict",
		Root:     "/repo",
		Safety: []safetyGate{{
			Provider: "mise-bump",
			Status:   plan.StatusHeld,
			Summary:  &safetySummary{Allow: 1, Hold: 12},
			Findings: []safetyFinding{{
				Provider: "mise",
				Kind:     "tool",
				Name:     "github:ogulcancelik/herdr",
				Version:  "0.6.8 -> 0.6.9",
				Decision: "allow",
				Reason:   "accepted from updev detail browser after local review",
			}, {
				Provider: "mise",
				Kind:     "tool",
				Name:     "cloudflared",
				Version:  "2026.5.2 -> 2026.6.0",
				Decision: "hold",
				Reason:   "candidate is too new",
			}},
		}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionSecurity, Provider: "mise-bump", Query: "tool/github:ogulcancelik/herdr"})
	view := model.View().Content
	if !strings.Contains(view, "github:ogulcancelik/herdr") || !strings.Contains(view, "allow") {
		t.Fatalf("expected queried allowed security finding in routed detail view:\n%s", view)
	}
	if strings.Contains(view, "mise-bump security 12 hold") || strings.Contains(view, "cloudflared") {
		t.Fatalf("expected route to avoid fallback gate summary and unrelated findings:\n%s", view)
	}
}

func TestListHubRouterBackReturnsToSelectorHub(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo", Items: []plan.Item{{
		Provider: "mise",
		Kind:     "tool",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionFull, nil, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.finalAction != updevActionBack {
		t.Fatalf("expected Back to quit router for selector hub, got %q", model.finalAction)
	}
}

func TestListHubRouterOpensBackendTableWithoutSubprogram(t *testing.T) {
	report := listReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "bat",
		RecommendedProvider: "mise",
		RecommendedName:     "bat",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionBackends, nil, false)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionBackends {
		t.Fatalf("expected backend table, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev backend convergence") || !strings.Contains(view, "bat") {
		t.Fatalf("expected backend table view:\n%s", view)
	}
}

func TestListHubRouterRefreshesBackendEvidenceAsynchronously(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := listReport{Status: plan.StatusOK, Root: "/repo", Items: []plan.Item{{
		Provider: "brew",
		Kind:     "brew",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, true, updateReport{}, false, listHubActionFull, nil, false)
	initialView := model.View().Content
	if !strings.Contains(initialView, "backend evidence loading") || strings.Contains(initialView, "open backend review") {
		t.Fatalf("expected initial list view to render before backend evidence is ready:\n%s", initialView)
	}
	updated, _ := model.Update(listHubBackendPlanMsg{Report: backendPlan})
	model = updated.(listHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "backend evidence loading") {
		t.Fatalf("expected backend loading state to clear:\n%s", readyView)
	}
	if !strings.Contains(readyView, "▶bak") || !strings.Contains(readyView, "open backend review") {
		t.Fatalf("expected backend evidence refresh to add visible backend badge and row action:\n%s", readyView)
	}
}

func TestListHubRouterRefreshesFilteredBackendEvidenceAsynchronously(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{{
			Name:    "brew",
			Desired: 1,
			Live:    1,
		}},
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, true, updateReport{}, false, listHubActionProvider, nil, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != "filter-result:provider:brew" {
		t.Fatalf("expected provider-filtered inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	initialView := model.View().Content
	if !strings.Contains(initialView, "backend evidence loading") || strings.Contains(initialView, "▶bak") {
		t.Fatalf("expected filtered list to show loading before backend evidence:\n%s", initialView)
	}
	updated, _ = model.Update(listHubBackendPlanMsg{Report: backendPlan})
	model = updated.(listHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "backend evidence loading") {
		t.Fatalf("expected filtered backend loading state to clear:\n%s", readyView)
	}
	if !strings.Contains(readyView, "▶bak") || !strings.Contains(readyView, "open backend review") {
		t.Fatalf("expected filtered backend refresh to add visible backend badge and row action:\n%s", readyView)
	}
}

func TestListHubRouterProviderFilterStaysInsideRouter(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{{
			Name:    "brew",
			Desired: 1,
			Live:    1,
		}},
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionProvider, nil, false)
	if model.screen != listHubRouterDetail || model.stateKey != "filter-menu:"+listHubActionProvider || !model.detail.PrimaryEnterAction {
		t.Fatalf("expected provider filter menu inside router, screen=%q state=%q primary=%v", model.screen, model.stateKey, model.detail.PrimaryEnterAction)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != "filter-result:provider:brew" {
		t.Fatalf("expected Enter to open provider-filtered inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "ripgrep") || strings.Contains(view, "node") {
		t.Fatalf("expected provider-filtered view to show brew rows only:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || model.stateKey != "filter-menu:"+listHubActionProvider {
		t.Fatalf("expected Back from filtered rows to return to provider menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterQueryInputStaysInsideRouter(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "unique-rip-tool",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionQuery, nil, false)
	if model.screen != listHubRouterInput || model.stateKey != listHubActionQuery {
		t.Fatalf("expected list query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "unique-rip", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != "filter-result:query:unique-rip" {
		t.Fatalf("expected query-filtered list inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "unique-rip-tool") || strings.Contains(view, "node") {
		t.Fatalf("expected query-filtered list view:\n%s", view)
	}
}

func TestListHubRouterSecurityCustomAllowInputsStayInsideRouter(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo"}
	lastUpdate := updateReport{Status: plan.StatusHeld, Root: "/repo", Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo",
			Decision: "hold",
			Reason:   "review required",
		}},
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, false, lastUpdate, true, listHubActionSecurity, nil, false)
	action := securityDetailActionValue("allow-custom", "brew", "cask", "demo")
	updated, _ := model.handleAction(action)
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterInput || !strings.HasPrefix(model.stateKey, "write-reason:") {
		t.Fatalf("expected custom allow reason input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "reviewed provenance", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterInput || !strings.HasPrefix(model.stateKey, "write-expiry:") {
		t.Fatalf("expected custom allow expiry input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterConfirm || !strings.HasPrefix(model.stateKey, "write-confirm:") {
		t.Fatalf("expected custom allow confirmation inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "reviewed provenance") || !strings.Contains(view, "expires:") {
		t.Fatalf("expected custom allow confirmation detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || model.stateKey != listHubActionSecurity {
		t.Fatalf("expected Back from confirmation to return to security detail, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterSecurityShowsProviderSummaryRows(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo"}
	lastUpdate := updateReport{Status: plan.StatusHeld, Root: "/repo", Security: "strict", Safety: []safetyGate{
		{Provider: "mise", Status: plan.StatusOK},
		{Provider: "brew", Status: plan.StatusHeld, Findings: []safetyFinding{{Provider: "brew", Kind: "cask", Name: "wezterm@nightly", Decision: "review", Reason: "host mismatch"}}},
	}}
	model := newListHubRouterModel(report, backendPlanReport{}, false, lastUpdate, true, listHubActionSecurity, nil, false)
	view := model.View().Content
	for _, want := range []string{"updev security review", "mise security", "brew"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected list security view to include %q:\n%s", want, view)
		}
	}
	route := listRouteAction{Domain: listHubActionSecurity, Provider: "brew", Kind: "cask", Name: "wezterm@nightly"}
	if rows := model.routeRows(route); len(rows) == 0 {
		t.Fatalf("expected list security item route to return rows for %+v", route)
	}
}

func TestListHubRouterKeepsBackendRouteLoadingUntilAsyncPlanArrives(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo", Items: []plan.Item{{
		Provider: "brew",
		Kind:     "brew",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, true, updateReport{}, false, listHubActionBackends, nil, false)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionBackends || !model.backendLoading {
		t.Fatalf("expected backend route to remain loading, screen=%q state=%q backendLoading=%v", model.screen, model.stateKey, model.backendLoading)
	}
	if view := model.View().Content; !strings.Contains(view, "backend evidence loading") {
		t.Fatalf("expected backend loading title:\n%s", view)
	}
}

func TestListHubRouterRouteBackReturnsToOriginView(t *testing.T) {
	report := listReport{Status: plan.StatusDrift, Root: "/repo", Items: []plan.Item{{
		Provider: "brew",
		Kind:     "brew",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionFull, nil, false)
	model.showRouteDetail(listRouteAction{Domain: listHubActionBackends, Provider: "brew", Kind: "brew", Name: "ripgrep"})
	if model.screen != listHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Back from route detail to return to origin inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterClearsRowActionAfterReturningToInventory(t *testing.T) {
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlan),
	}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionFull, nil, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected row action to open route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	if !model.detail.State.Expanded[0] {
		t.Fatalf("expected route detail to open expanded on the focused row, state=%#v", model.detail.State)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Back from route detail to return to inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Down after Back to stay in inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestInventoryItemDetailLocalizesJapaneseEvidenceAndActions(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise-candidate",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "git",
		RecommendedProvider: "mise",
		RecommendedName:     "github:git/git",
		Reason:              "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew",
		Action:              "review github:git/git as a candidate only; verify release assets, version mapping, and ownership before changing Homebrew ownership",
	}}}
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "git",
			Category: "work",
			Detail:   "keep macOS/system git available",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlan),
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one inventory row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	for _, want := range []string{
		"説明: macOS/system git を使える状態に保つ",
		"関連 evidence: 1 件の backend evidence",
		"backend 根拠: github:git/git は候補としてのみ確認します",
		"次の操作: backend 整理を開く",
	} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected localized detail to contain %q:\n%s", want, row.Detail)
		}
	}
	expanded := strings.Join(reviewui.ExpandedLines(row, reviewLabels()), "\n")
	if strings.Contains(expanded, "note:") {
		t.Fatalf("did not expect localized key-value detail to fall back to note lines:\n%s", expanded)
	}
}

func TestDetailBrowserDoesNotTreatURLsAsKeyValueLines(t *testing.T) {
	lines := strings.Join(detailBrowserDetailLines("go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html", 120, false), "\n")
	if strings.Contains(lines, "https: //") {
		t.Fatalf("did not expect URL scheme to be split as a key-value line:\n%s", lines)
	}
	if !strings.Contains(lines, "detail: go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html") {
		t.Fatalf("expected URL to remain inside the detail text:\n%s", lines)
	}
}

func selectedDetailActionForKey(rows []detailBrowserRow, key tea.KeyPressMsg) string {
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ := model.Update(key)
	model = updated.(detailBrowserModel)
	return model.State.Action
}

func TestDetailBrowserCollapsedBadgesSummarizeActionsAndEvidence(t *testing.T) {
	row := detailBrowserRow{
		Status:  "held",
		Summary: "security review required",
		Metadata: []string{
			"updated: jq; git",
			"deferred: demo held",
			"decision: hold",
			"release assets: compatible",
			"applyability: applyable: rewrite current mise key",
		},
		Actions: []detailBrowserAction{{Value: "one", Label: "allow"}, {Value: "two", Label: "hold"}},
	}
	got := detailBrowserCollapsedSummary(row)
	for _, want := range []string{"[actions:2]", "[updated:2]", "[deferred:1]", "[decision:hold]", "[assets:compatible]", "[applyable]", "security review required"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected collapsed summary to include %q, got %q", want, got)
		}
	}
}

func TestDetailBrowserMouseClickTogglesOncePerClick(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Status: "ok", Summary: "short", Detail: "full detail"},
	}, detailBrowserState{}, false)
	model.MouseMode = browserMouseClick
	click := model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to detail row")
	}
	updated, _ := model.Update(click())
	model = updated.(detailBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected click press to select without expanding row: %#v", model.State)
	}
	release := model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if release == nil {
		t.Fatal("expected row release to map to detail row")
	}
	updated, _ = model.Update(release())
	model = updated.(detailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected matching release to expand row: %#v", model.State)
	}
	click = model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(click())
	model = updated.(detailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected second click press not to collapse row: %#v", model.State)
	}
	release = model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(release())
	model = updated.(detailBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected second matching release to collapse row: %#v", model.State)
	}
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to detail browser movement")
	}
}

func TestDetailBrowserFiltersRowsInPlace(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "node", Status: "ok", Summary: "runtime", Detail: "javascript"},
		{Title: "jq", Status: "held", Summary: "json", Detail: "processor"},
	}, detailBrowserState{Query: "json"}, false)
	view := model.View().Content
	if strings.Contains(view, "node") || !strings.Contains(view, "jq") || !strings.Contains(view, `filter="json"`) {
		t.Fatalf("expected filtered detail browser rows:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected detail filter to update while typing: %#v\n%s", model.State, model.View().Content)
	}
	if view := model.View().Content; strings.Index(view, "filter: jq") < 0 || strings.Index(view, "filter: jq") > strings.Index(view, "jq") {
		t.Fatalf("expected active detail filter input near the top:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected esc to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected slash filter input to apply detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.State.Action != "" {
		t.Fatalf("expected back to clear detail filter before leaving: %#v", model.State)
	}
}

func TestToolTableBrowserPreservesGroupedTableAndExpandsDetail(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "A deliberately long Node.js runtime description that should expand below the grouped table row.",
		}},
	}}, detailBrowserState{}, false)
	model.MouseMode = reviewui.MouseClick
	view := model.View().Content
	for _, want := range []string{"mise / runtime", "name", "version", "wanted", "state", "detail", "node", "24.16.0", "lts"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected table browser to preserve grouped table output with %q:\n%s", want, view)
		}
	}
	rendered := model.View()
	if !rendered.AltScreen {
		t.Fatal("expected table browser to use alt screen for stable mouse coordinates")
	}
	click := rendered.OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to table row")
	}
	msg, ok := click().(toolTableMouseMsg)
	if !ok || msg.Index != 0 {
		t.Fatalf("expected row click to target index 0, got %#v", msg)
	}
	model.ToggleSelected()
	view = model.View().Content
	if !strings.Contains(view, "detail: A deliberately long Node.js runtime description") {
		t.Fatalf("expected expanded detail under table row:\n%s", view)
	}
}

func TestToolTableBrowserExpandedActionsAreSelectable(t *testing.T) {
	actions := []reviewui.Action{{
		Value:       "open-backend",
		Label:       "open backend review",
		Description: "inspect backend recommendation",
	}, {
		Value:       "open-update",
		Label:       "open update evidence",
		Description: "inspect update evidence",
	}, {
		Value:       "open-security",
		Label:       "open security review",
		Description: "inspect security evidence",
	}, {
		Value:       "open-manual",
		Label:       "open manual review",
		Description: "inspect manual evidence",
	}, {
		Value:       "open-extra",
		Label:       "open extra review",
		Description: "inspect extra evidence",
	}}
	model := newToolTableBrowserModel("updev list brew", []toolSection{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows: []toolRow{{
			Name:    "ripgrep",
			State:   "ok",
			Detail:  "description: ripgrep",
			Actions: actions,
		}},
	}}, detailBrowserState{}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	view := model.View().Content
	if !strings.Contains(view, "> action 1 [press a or 1]: open backend review") || !strings.Contains(view, "expanded actions:") {
		t.Fatalf("expected expanded action focus in table browser:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(toolTableBrowserModel)
	view = model.View().Content
	if !strings.Contains(view, "> action 2 [press 2]: open update evidence") {
		t.Fatalf("expected cursor to move to second expanded action:\n%s", view)
	}
	for _, want := range []string{"action 3 [press 3]: open security review", "action 4 [press 4]: open manual review", "action 5 [press 5]: open extra review"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded row to preserve all actions with %q:\n%s", want, view)
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	if model.State.Action != "open-update" {
		t.Fatalf("expected Enter to run focused expanded action, got %#v", model.State)
	}
}

func TestToolTableBrowserFiltersRowsInPlace(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "javascript runtime",
		}, {
			Name:    "go",
			Version: "1.26.3",
			State:   "active",
			Detail:  "language runtime",
		}},
	}}, detailBrowserState{Query: "javascript"}, false)
	view := model.View().Content
	if !strings.Contains(view, "node") || strings.Contains(view, "go") || !strings.Contains(view, `filter="javascript"`) {
		t.Fatalf("expected filtered table browser rows:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || !strings.Contains(model.View().Content, "go") {
		t.Fatalf("expected x to clear table filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "go" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected table filter to update while typing: %#v\n%s", model.State, model.View().Content)
	}
	if view := model.View().Content; strings.Index(view, "filter: go") < 0 || strings.Index(view, "filter: go") > strings.Index(view, "mise / runtime") {
		t.Fatalf("expected active table filter input near the top:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected esc to clear active table filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear active table filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "go" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected slash filter input to apply table filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.State.Action != "" {
		t.Fatalf("expected back to clear table filter before leaving: %#v", model.State)
	}
}

func TestBrowserModelsExposeNavigationActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{name: "back", key: tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}), want: updevActionBack},
		{name: "home", key: tea.KeyPressMsg(tea.Key{Text: "h", Code: 'h'}), want: updevActionHome},
		{name: "exit", key: tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}), want: updevActionExit},
	} {
		t.Run("detail/"+tc.name, func(t *testing.T) {
			model := newDetailBrowserModel("details", []detailBrowserRow{{Title: "one"}}, detailBrowserState{}, false)
			updated, _ := model.Update(tc.key)
			model = updated.(detailBrowserModel)
			if model.State.Action != tc.want {
				t.Fatalf("expected detail action %q, got %#v", tc.want, model.State)
			}
		})
		t.Run("table/"+tc.name, func(t *testing.T) {
			model := newToolTableBrowserModel("tools", []toolSection{{
				Name:  "mise/core",
				Title: "mise / core",
				Rows:  []toolRow{{Name: "node"}},
			}}, detailBrowserState{}, false)
			updated, _ := model.Update(tc.key)
			model = updated.(toolTableBrowserModel)
			if model.State.Action != tc.want {
				t.Fatalf("expected table action %q, got %#v", tc.want, model.State)
			}
		})
	}
}

func TestToolTableBrowserMouseClickTogglesOncePerClick(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "detail",
		}},
	}}, detailBrowserState{}, false)
	model.MouseMode = reviewui.MouseClick
	click := model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to table row")
	}
	updated, _ := model.Update(click())
	model = updated.(toolTableBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected click press to select without expanding row: %#v", model.State)
	}
	release := model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if release == nil {
		t.Fatal("expected row release to map to table row")
	}
	updated, _ = model.Update(release())
	model = updated.(toolTableBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected matching release to expand row: %#v", model.State)
	}
	click = model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	updated, _ = model.Update(click())
	model = updated.(toolTableBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected second click press not to collapse row: %#v", model.State)
	}
	release = model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	updated, _ = model.Update(release())
	model = updated.(toolTableBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected second matching release to collapse row: %#v", model.State)
	}
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to table browser movement")
	}
}

func TestBrowserMouseDragDoesNotToggleAndMouseCanBeDisabled(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Detail: "detail"},
		{Title: "two", Detail: "detail"},
	}, detailBrowserState{}, false)
	if detail.MouseMode != browserMouseOff || detail.View().MouseMode != tea.MouseModeNone || detail.View().OnMouse != nil {
		t.Fatalf("expected mouse support to be off by default")
	}
	updated, _ := detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseWheel || detail.View().OnMouse == nil {
		t.Fatalf("expected first m to enable wheel-only mouse mode")
	}
	if detail.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft})) != nil {
		t.Fatalf("expected wheel-only mode to ignore clicks")
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseClick {
		t.Fatalf("expected second m to enable click mouse mode")
	}
	click := detail.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = detail.Update(click())
	detail = updated.(detailBrowserModel)
	release := detail.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 6, Button: tea.MouseLeft}))
	updated, _ = detail.Update(release())
	detail = updated.(detailBrowserModel)
	if detail.State.Expanded[0] || detail.State.Expanded[1] {
		t.Fatalf("expected drag release on another row not to toggle expansion: %#v", detail.State)
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseOff || detail.View().MouseMode != tea.MouseModeNone || detail.View().OnMouse != nil {
		t.Fatalf("expected third m to disable detail browser mouse support")
	}

	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "one"}, {Name: "two"}},
	}}, detailBrowserState{}, false)
	table.MouseMode = reviewui.MouseClick
	click = table.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	tableUpdated, _ := table.Update(click())
	table = tableUpdated.(toolTableBrowserModel)
	release = table.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 8, Button: tea.MouseLeft}))
	tableUpdated, _ = table.Update(release())
	table = tableUpdated.(toolTableBrowserModel)
	if table.State.Expanded[0] || table.State.Expanded[1] {
		t.Fatalf("expected table drag release on another row not to toggle expansion: %#v", table.State)
	}
}

func TestToolTableBrowserScrollsBySelectedRow(t *testing.T) {
	rows := make([]toolRow, 0, 30)
	for i := 0; i < 30; i++ {
		rows = append(rows, toolRow{Name: "tool-" + string(rune('a'+i)), Version: "1.0.0", State: "active", Detail: "detail"})
	}
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  rows,
	}}, detailBrowserState{}, false)
	model.Height = 10
	model.Move(20)
	if model.State.Selected != 20 || model.State.Offset == 0 {
		t.Fatalf("expected scroll offset to follow selected row, got selected=%d offset=%d", model.State.Selected, model.State.Offset)
	}
	before := model.State.Selected
	beforeOffset := model.State.Offset
	model.MouseMode = reviewui.MouseWheel
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to table scroll")
	}
	updated, _ := model.Update(wheel())
	model = updated.(toolTableBrowserModel)
	if model.State.Selected != before || model.State.Offset <= beforeOffset {
		t.Fatalf("expected wheel down to scroll without moving selection, before=%d/%d after=%d/%d", before, beforeOffset, model.State.Selected, model.State.Offset)
	}
	view := model.View().Content
	if strings.Contains(view, "tool-a") || !strings.Contains(view, "tool-u") {
		t.Fatalf("expected scrolled view around selected row:\n%s", view)
	}
}

func TestToolTableBrowserKeepsSelectedRowVisibleAcrossManySections(t *testing.T) {
	sections := []toolSection{}
	for section := 0; section < 10; section++ {
		rows := []toolRow{}
		for row := 0; row < 3; row++ {
			rows = append(rows, toolRow{
				Name:    fmt.Sprintf("tool-%d-%d", section, row),
				Version: "1.0.0",
				State:   "active",
				Detail:  "detail",
			})
		}
		sections = append(sections, toolSection{
			Name:  fmt.Sprintf("mise/section-%d", section),
			Title: fmt.Sprintf("mise / section-%d", section),
			Rows:  rows,
		})
	}
	model := newToolTableBrowserModel("updev list mise", sections, detailBrowserState{}, false)
	model.Height = 10
	model.Move(20)
	if !toolTableVisibleRows(model.VisibleSections(), model.State.Offset, model.VisibleBodyLines(), model.State.Expanded)[model.State.Selected] {
		t.Fatalf("expected selected row to be visible, selected=%d offset=%d\n%s", model.State.Selected, model.State.Offset, model.View().Content)
	}
	if !strings.Contains(model.View().Content, "tool-6-2") {
		t.Fatalf("expected visible content around selected row:\n%s", model.View().Content)
	}
}

func TestToolTableBrowserKeepsExpandedDetailVisibleNearBottom(t *testing.T) {
	rows := []toolRow{}
	for i := 0; i < 12; i++ {
		detail := "detail"
		if i == 10 {
			detail = "description: expanded row\nidentity: brew / brew / bottom\nstatus: ok - desired and installed\nlinked evidence: cached update\nnext action: open update evidence"
		}
		rows = append(rows, toolRow{Name: fmt.Sprintf("tool-%02d", i), State: "ok", Detail: detail})
	}
	model := newToolTableBrowserModel("updev list brew", []toolSection{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows:  rows,
	}}, detailBrowserState{}, false)
	model.Height = 14
	model.Move(10)
	model.ToggleSelected()
	view := model.View().Content
	for _, want := range []string{"tool-10", "description: expanded row", "identity: brew / brew / bottom", "next action: open update evidence"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded bottom detail to stay visible with %q:\n%s", want, view)
		}
	}
}

func TestBrowserModelsClearStaleNavigationAction(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{{
		Title: "one",
	}}, detailBrowserState{Action: updevActionExit}, false)
	if detail.State.Action != "" {
		t.Fatalf("expected stale detail browser action to be cleared, got %#v", detail.State)
	}
	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "node"}},
	}}, detailBrowserState{Action: updevActionHome}, false)
	if table.State.Action != "" {
		t.Fatalf("expected stale table browser action to be cleared, got %#v", table.State)
	}
}

func TestBrowserHelpOverlayTogglesInPlace(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{{Title: "one"}}, detailBrowserState{}, false)
	updated, _ := detail.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	detail = updated.(detailBrowserModel)
	if !detail.Help || !strings.Contains(detail.View().Content, "expand/collapse details") {
		t.Fatalf("expected detail browser help overlay:\n%s", detail.View().Content)
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	detail = updated.(detailBrowserModel)
	if detail.Help {
		t.Fatalf("expected detail browser help to close")
	}
	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "node"}},
	}}, detailBrowserState{}, false)
	tableUpdated, _ := table.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	table = tableUpdated.(toolTableBrowserModel)
	if !table.Help || !strings.Contains(table.View().Content, "expand/collapse details") {
		t.Fatalf("expected table browser help overlay:\n%s", table.View().Content)
	}
}

func TestPrintListTextHonorsSectionLimit(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Limit:  1,
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 2, Live: 2},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "one", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "two", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if !strings.Contains(text, "one") || strings.Contains(text, "two") || !strings.Contains(text, "... 1 more rows") {
		t.Fatalf("expected list limit to show first row and omitted count:\n%s", text)
	}
}

func TestListHubChoicesExposeFiltersAndNavigation(t *testing.T) {
	backendPlan := backendPlanReport{Findings: []backendFinding{{Name: "ripgrep", RecommendedName: "ripgrep"}}}
	choices := listHubChoices(listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra},
		},
	}, backendPlan, false, updateReport{}, false)
	if choices[0].Value != listHubActionFull || !choices[0].Selected {
		t.Fatalf("expected installed inventory to be the default list hub choice, got %#v", choices[0])
	}
	values := map[string]bool{}
	for _, choice := range choices {
		values[choice.Value] = true
	}
	for _, want := range []string{listHubActionAttention, listHubActionProvider, listHubActionKind, listHubActionCategory, listHubActionStatus, listHubActionQuery, listHubActionManual, listHubActionBackends, listHubActionLimited, listHubActionDetails, listHubActionFull, updevActionExit} {
		if !values[want] {
			t.Fatalf("expected list hub choice %q in %#v", want, choices)
		}
	}
}

func TestListHubChoicesExposeBackendWhileEvidenceIsLoading(t *testing.T) {
	choices := listHubChoices(listReport{}, backendPlanReport{}, true, updateReport{}, false)
	values := map[string]bool{}
	descriptions := map[string]string{}
	for _, choice := range choices {
		values[choice.Value] = true
		descriptions[choice.Value] = choice.Description
	}
	if !values[listHubActionBackends] {
		t.Fatalf("expected backend convergence choice while evidence is loading: %#v", choices)
	}
	if !strings.Contains(descriptions[listHubActionBackends], "asynchronously") && !strings.Contains(descriptions[listHubActionBackends], "非同期") {
		t.Fatalf("expected backend loading choice to explain async preparation: %#v", choices)
	}
}

func TestShouldRunListHubRouterIncludesQuery(t *testing.T) {
	if !shouldRunListHubRouterAction(listHubActionQuery) {
		t.Fatal("expected list query to run inside the list hub router")
	}
}

func TestDerivedListReportCanOpenManualApps(t *testing.T) {
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "apps.md"), []byte("## Manual\n\n- Demo App\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := listReport{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK},
		},
	}
	manual := derivedListReport(report, listOptions{provider: "manual"})
	if len(manual.Items) != 0 || len(manual.Sections) != 1 || manual.Sections[0].Rows[0].Name != "Demo App" {
		t.Fatalf("expected derived manual report to use original root without live provider collection, got %#v", manual)
	}
}

func TestListInventoryReviewCountIncludesToolSections(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "app"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node"}, {Name: "python"}},
		}},
	}
	if got := listInventoryReviewCount(report); got != 3 {
		t.Fatalf("expected review count to include manifest rows and rich tool rows, got %d", got)
	}
}

func TestListFacetCountsIncludeToolSections(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "app"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node"}, {Name: "python"}},
		}},
	}
	if counts := listKindCounts(report); counts["cask"] != 1 || counts["tool"] != 2 {
		t.Fatalf("unexpected kind counts: %#v", counts)
	}
	if counts := listCategoryCounts(report); counts["personal"] != 1 || counts["runtime"] != 2 {
		t.Fatalf("unexpected category counts: %#v", counts)
	}
	if rows := listVisibleRowCount(report); rows != 3 {
		t.Fatalf("expected visible row count to include item and tool rows, got %d", rows)
	}
	if desc := categoryDescription("work"); !strings.Contains(desc, "included by personal") {
		t.Fatalf("expected work category description, got %q", desc)
	}
	if desc := categoryDescription("core"); !strings.Contains(desc, "core CLI") {
		t.Fatalf("expected core category description, got %q", desc)
	}
}

func TestListFilteredDetailBrowserRequiresRows(t *testing.T) {
	empty := listReport{}
	handled, exit := runListFilteredDetailBrowser("empty", empty, map[string]detailBrowserState{}, "empty", false)
	if handled || exit {
		t.Fatalf("expected empty report to fall back to text output")
	}
}

func TestDetailBrowserActionKeyIndex(t *testing.T) {
	if index, ok := detailBrowserActionKeyIndex("1"); !ok || index != 0 {
		t.Fatalf("expected key 1 to map to first action, got index=%d ok=%v", index, ok)
	}
	if _, ok := detailBrowserActionKeyIndex("0"); ok {
		t.Fatal("did not expect key 0 to map to an action")
	}
	if _, ok := detailBrowserActionKeyIndex("a"); ok {
		t.Fatal("did not expect key a to map through numeric action helper")
	}
}

func TestDetailBrowserPreservesPreformattedDetailLines(t *testing.T) {
	lines := detailBrowserExpandedLinesWithWidth(detailBrowserRow{
		Detail: "stdout line one\nstdout line two",
		Metadata: []string{
			"stderr: warning one\nwarning two",
		},
	}, 80)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"detail: stdout line one",
		"stdout line two",
		"stderr: warning one",
		"warning two",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected expanded detail to preserve %q in %#v", want, lines)
		}
	}
}

func TestListBrowserStateKeyUsesFilters(t *testing.T) {
	report := listReport{Filters: map[string]string{"provider": "mise", "query": "node"}}
	if got := listBrowserStateKey(report); got != "list:provider=mise query=node" {
		t.Fatalf("unexpected list browser state key: %q", got)
	}
}

func TestParseTranslatedTSV(t *testing.T) {
	pending := map[string]string{"brew:git": "Distributed revision control"}
	got := parseTranslatedTSV([]byte("noise\nBEGIN_TSV\nbrew:git\t分散バージョン管理\nEND_TSV\n"), pending)
	if got["brew:git"] != "分散バージョン管理" {
		t.Fatalf("unexpected translation parse result: %#v", got)
	}
}

func TestListTranslationDisabledByConfigSkipsExplicitRequest(t *testing.T) {
	t.Setenv("UPDEV_DESCRIPTION_TRANSLATION", "off")
	update := maybeUpdateListTranslations(listOptions{format: "text", translateNow: true}, listReport{})
	if !update.Attempted || update.Changed || !strings.Contains(update.Message, "disabled") {
		t.Fatalf("expected disabled translation message, got %#v", update)
	}
}
