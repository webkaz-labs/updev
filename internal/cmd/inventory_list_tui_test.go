package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/plan"
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
			{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: inventoryannotate.ProfileMismatchDetail("personal")},
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
