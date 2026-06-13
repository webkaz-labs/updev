package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

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

func TestSyncEntriesFromInventoryUsesProfileMismatchGuidance(t *testing.T) {
	report := plan.Report{
		Status: plan.StatusDrift,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: "profile-mismatch: entry is defined in personal scope"},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one sync entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Reason != "profile-mismatch" || entry.Action != "switch-profile-or-remove" || entry.Category != "personal" {
		t.Fatalf("expected profile mismatch guidance, got %#v", entry)
	}
}

func TestMiseManifestFixDryRunResolvesLatest(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(miseDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[tools]
"github:owner/tool" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	results := addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
		strings.Join([]string{"mise", "latest", "github:owner/tool"}, "\x00"): {Stdout: "1.2.3\n"},
	})
	results[strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00")] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	fake := &fakeCommandRunner{results: results}
	report := buildMiseManifestFixReport(context.Background(), miseManifestFixOptions{root: root}, fake)
	if report.Status != plan.StatusDrift || !report.DryRun || len(report.Actions) != 1 {
		t.Fatalf("expected dry-run drift action, got %#v", report)
	}
	if !mise.BoolValue(report.MiseMinimumReleaseAge.Active) || report.MiseMinimumReleaseAge.Value != "3d" {
		t.Fatalf("expected active mise minimum_release_age evidence, got %#v", report.MiseMinimumReleaseAge)
	}
	if action := report.Actions[0]; action.Status != plan.StatusDrift || action.Resolved != "1.2.3" || action.Current != "latest" || !action.AgePolicyActive || action.AgePolicyValue != "3d" {
		t.Fatalf("unexpected fix action: %#v", action)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "1.2.3") {
		t.Fatalf("dry-run should not rewrite config: %s", data)
	}
}

func TestMiseManifestFixApplyRewritesLatest(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(miseDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[tools]
"github:owner/tool" = "latest" # keep comment
"npm:demo" = { version = "latest", os = ["macos"] }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(projectDir, "mise.toml")
	if err := os.WriteFile(projectPath, []byte(`[tools]
"aqua:owner/project" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "latest", "github:owner/tool"}, "\x00"):  {Stdout: "1.2.3\n"},
		strings.Join([]string{"mise", "latest", "npm:demo"}, "\x00"):           {Stdout: "4.5.6\n"},
		strings.Join([]string{"mise", "latest", "aqua:owner/project"}, "\x00"): {Stdout: "7.8.9\n"},
	}}
	report := buildMiseManifestFixReport(context.Background(), miseManifestFixOptions{root: root, apply: true}, fake)
	if report.Status != plan.StatusOK || report.DryRun || len(report.Actions) != 3 {
		t.Fatalf("expected applied ok report, got %#v", report)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"github:owner/tool" = "1.2.3" # keep comment`, `version = "4.5.6"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected rewritten config to contain %q:\n%s", want, text)
		}
	}
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectData), `"aqua:owner/project" = "7.8.9"`) {
		t.Fatalf("expected rewritten project config:\n%s", projectData)
	}
}
