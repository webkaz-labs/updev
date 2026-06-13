package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/legacycache"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
)

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
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: inventoryannotate.ProfileMismatchDetail("personal")},
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
	cache := legacycache.Load()
	pending := cache.PendingTranslations(displayListItems(report.Items, report.Sections), report.Sections, true)
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

func TestPrintReadOnlyTextShowsCacheAndNoChanges(t *testing.T) {
	result := inventoryResult{
		Cached:    true,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "brew", Supported: true, Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "app", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	var out bytes.Buffer
	printReadOnlyText(&out, "status", result)
	text := out.String()
	for _, want := range []string{"updev status ok", "cache:", "categories", "personal=1", "changes", "no changes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected read-only text to include %q:\n%s", want, text)
		}
	}
}

func TestListUsesStaleInventoryCacheForFastInitialDisplay(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
	root := t.TempDir()
	entry := inventoryCacheEntry{
		Version:   inventoryCacheVersion,
		Root:      root,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   root,
			Providers: []plan.ProviderSummary{
				{Name: "mise", Supported: true, Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	saveInventoryCache(entry)
	result := collectInventoryCachedWithOptions(context.Background(), root, false, 0, inventoryOptions{})
	if !result.Cached {
		t.Fatal("expected stale inventory cache to be reused for list initial display")
	}
	if got := result.Report.Root; got != root {
		t.Fatalf("unexpected cached root: %q", got)
	}
}

func TestPrintReadOnlyTextUsesRichListCategoryCounts(t *testing.T) {
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
	var out bytes.Buffer
	printReadOnlyText(&out, "status", result)
	text := out.String()
	for _, want := range []string{"categories", "runtime=2", "other categories are provider/backend groups", "changes", "no changes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected read-only text to include %q:\n%s", want, text)
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
