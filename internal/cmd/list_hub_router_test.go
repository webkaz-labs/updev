package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
)

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

func TestListHubRouterOpensSupportCatalogWithoutSubprogram(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo"}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionSupport, nil, false)
	if model.screen != listHubRouterDetail || model.stateKey != listHubActionSupport {
		t.Fatalf("expected support catalog detail view, screen=%q state=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	for _, want := range []string{"updev support catalog", "provider/homebrew", "experimental"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected support catalog view to include %q:\n%s", want, view)
		}
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if view := model.View().Content; !strings.Contains(view, "support_label") {
		t.Fatalf("expected expanded support catalog row to include support_label:\n%s", view)
	}
	updated, _ = model.handleAction(listSupportFilterActionValue("label", "experimental"))
	model = updated.(listHubRouterModel)
	if model.stateKey != "support-filter:label:experimental" {
		t.Fatalf("expected support label filter state, got %q", model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "experimental") || strings.Contains(view, "supported_preview command") {
		t.Fatalf("expected support label filter to show only experimental rows:\n%s", view)
	}
	updated, _ = model.handleAction(listSupportFilterActionValue("label", "all"))
	model = updated.(listHubRouterModel)
	if view := model.View().Content; !strings.Contains(view, "supported_preview") || !strings.Contains(view, "experimental") {
		t.Fatalf("expected support label all filter to restore catalog rows:\n%s", view)
	}
}

func TestListHubRouterProviderFilterShowsNonDefaultSupportLabel(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{{
			Name:    "vscode",
			Desired: 1,
			Live:    1,
		}},
	}
	rows := listProviderFilterRows(report)
	if len(rows) != 1 {
		t.Fatalf("expected provider row, got %#v", rows)
	}
	if !strings.Contains(rows[0].Summary, "support=experimental") {
		t.Fatalf("expected non-default support label in provider summary, got %#v", rows[0])
	}
	if !strings.Contains(strings.Join(rows[0].Metadata, "\n"), "support_label: experimental") {
		t.Fatalf("expected support label metadata, got %#v", rows[0])
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
		Evidence: addBackendListEvidence(plan.EvidenceIndex{}, backendPlan),
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
		Evidence: addBackendListEvidence(plan.EvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
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
		Evidence: addBackendListEvidence(plan.EvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
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
	evidence := plan.EvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	for _, key := range plan.EvidenceUpdateItemKeys("brew", "cask demo-app", miseBumpProvider) {
		plan.AddEvidence(evidence.Updates, key, "brew held: release age gate")
	}
	for _, key := range listEvidenceFindingKeys(safetyGate{Provider: "brew"}, safetyFinding{Provider: "brew", Kind: "cask", Name: "demo-app", Decision: "hold", Reason: "release too new"}) {
		plan.AddEvidence(evidence.Security, key, "brew/cask demo-app: hold")
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

func toolRowHasRouteAction(row toolRow, domain string) bool {
	for _, action := range row.Actions {
		if route, ok := parseListRouteAction(action.Value); ok && route.Domain == domain {
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

func testBackendCompatibleAssetName(prefix string) string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "darwin"
	}
	archName := runtime.GOARCH
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	return prefix + "-" + osName + "-" + archName + ".tar.gz"
}

func TestBackendPlanReportsReadOnlyConvergenceFindings(t *testing.T) {
	compatibleAsset := testBackendCompatibleAssetName("demo")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`brew "git"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "ripgrep"
brew "rtk"
brew "git"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
ripgrep = "15.1.0"
"cargo:fd-find" = { version = "10.4.2", os = ["macos/x64"] }
"aqua:sharkdp/fd" = { version = "10.4.2", os = ["macos/arm64", "linux"] }
"cargo:git-delta" = { version = "0.19.2", os = ["macos/x64"] }
"aqua:dandavison/delta" = { version = "0.19.2", os = ["macos/x64", "linux"] }
"cargo:broot" = "1.56.4"
"cargo:demo-tool" = "0.1.0"
"cargo:local-build" = "0.2.0"
"npm:@scope/demo-cli" = "2.0.0"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"rg":          nil,
			"rtk":         nil,
			"fd":          fmt.Errorf("missing"),
			"delta":       nil,
			"demo-tool":   nil,
			"local-build": nil,
			"demo-cli":    nil,
		},
		results: map[string]runner.Result{
			"brew\x00info\x00--json=v2\x00git\x00rtk":                                          {Stdout: `{"formulae":[{"name":"rtk","urls":{"stable":{"url":"https://github.com/rtk-ai/rtk/archive/refs/tags/v0.42.1.tar.gz"},"head":{"url":"https://github.com/rtk-ai/rtk.git"}}}],"casks":[]}`},
			"cargo\x00info\x00demo-tool":                                                       {Stdout: "demo-tool # CLI\nrepository: https://github.com/example/demo-tool\n"},
			"cargo\x00info\x00local-build":                                                     {Stdout: "local-build # CLI\nrepository: https://github.com/example/local-build\n"},
			"npm\x00view\x00@scope/demo-cli\x00repository\x00homepage\x00--json":               {Stdout: `{"repository":{"url":"git+https://github.com/example/demo-cli.git"},"homepage":"https://example.com"}`},
			"gh\x00api\x00repos/Canop/broot/releases/latest\x00--jq\x00.assets[].name":         {Stdout: compatibleAsset},
			"gh\x00api\x00repos/rtk-ai/rtk/releases/latest\x00--jq\x00.assets[].name":          {Stdout: compatibleAsset},
			"gh\x00api\x00repos/example/demo-tool/releases/latest\x00--jq\x00.assets[].name":   {Stdout: compatibleAsset},
			"gh\x00api\x00repos/example/local-build/releases/latest\x00--jq\x00.assets[].name": {},
			"gh\x00api\x00repos/example/demo-cli/releases/latest\x00--jq\x00.assets[].name":    {Stdout: compatibleAsset},
		},
	}
	report := buildBackendPlanReportWithRunner(context.Background(), backendOptions{command: "plan", root: root}, fake)
	if report.SchemaVersion != backendPlanReportSchemaVersion || report.Status != plan.StatusDrift {
		t.Fatalf("expected drift backend plan report, got %#v", report)
	}
	types := map[string]bool{}
	for _, finding := range report.Findings {
		types[finding.Type] = true
		switch finding.Name {
		case "ripgrep":
			if finding.CommandStatus != "on-path" || !containsString(finding.CommandNames, "rg") || finding.RecommendedTier != "mise/core" || finding.PreferenceRank != 1 {
				t.Fatalf("expected ripgrep command verification, got %#v", finding)
			}
		case "rtk":
			if finding.Type != "homebrew-to-mise-candidate" || finding.RecommendedName != "github:rtk-ai/rtk" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || finding.CommandStatus != "on-path" || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" {
				t.Fatalf("expected rtk GitHub backend candidate from Homebrew metadata with platform evidence, got %#v", finding)
			}
		case "cargo:fd-find":
			if finding.CommandStatus != "missing" || !containsString(finding.CurrentOS, "macos/x64") || !containsString(finding.RecommendedOS, "macos/arm64") || finding.RecommendedTier != "mise/aqua" || finding.PreferenceRank != 2 {
				t.Fatalf("expected fd command and OS-condition evidence, got %#v", finding)
			}
		case "cargo:git-delta":
			if finding.RecommendedName != "aqua:dandavison/delta" || !finding.RewriteAllowed || !containsString(finding.CurrentOS, "macos/x64") || !containsString(finding.RecommendedOS, "macos/x64") {
				t.Fatalf("expected delta recommendation to be safely removable with covered OS evidence, got %#v", finding)
			}
		case "cargo:broot":
			if finding.RecommendedName != "github:Canop/broot" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 {
				t.Fatalf("expected broot GitHub backend recommendation, got %#v", finding)
			}
		case "cargo:demo-tool":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/demo-tool" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" || !containsString(finding.ReleaseAssetMatches, compatibleAsset) {
				t.Fatalf("expected cargo metadata GitHub backend candidate with platform evidence, got %#v", finding)
			}
		case "cargo:local-build":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/local-build" || finding.ReleaseAssetStatus != "no-assets" || finding.Confidence != "low" || !strings.Contains(finding.Action, "local cargo build") {
				t.Fatalf("expected cargo metadata candidate without assets to preserve local build, got %#v", finding)
			}
		case "npm:@scope/demo-cli":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/demo-cli" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || !containsString(finding.CommandNames, "demo-cli") || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" {
				t.Fatalf("expected npm metadata GitHub backend candidate with platform evidence, got %#v", finding)
			}
		}
	}
	if !types["homebrew-to-mise"] || !types["homebrew-to-mise-candidate"] || !types["mise-backend-rewrite"] || !types["mise-backend-candidate"] {
		t.Fatalf("expected brew and mise convergence findings, got %#v", report.Findings)
	}
	rows := backendDetailRows(report)
	if len(rows) != len(report.Findings) || !strings.Contains(strings.Join(rows[0].Metadata, " "), "command status:") || !strings.Contains(strings.Join(rows[0].Metadata, " "), "preference:") || !strings.Contains(strings.Join(rows[0].Metadata, " "), "recommendation kind:") {
		t.Fatalf("expected backend detail rows to expose command evidence, got %#v", rows)
	}
	sections := backendToolSections(report)
	sectionTitles := []string{}
	for _, section := range sections {
		sectionTitles = append(sectionTitles, section.Title)
	}
	for _, want := range []string{"backend / homebrew-to-mise", "backend / mise-backend-rewrite", "backend / mise-backend-candidate"} {
		if !containsString(sectionTitles, want) {
			t.Fatalf("expected backend grouped table sections to include %q, got %#v", want, sectionTitles)
		}
	}
	actionRows := map[string]detailBrowserRow{}
	for _, row := range rows {
		actionRows[row.Title] = row
	}
	if len(actionRows["cargo:broot -> github:Canop/broot"].Actions) != 1 {
		t.Fatalf("expected simple mise rewrite to expose an action, got %#v", actionRows["cargo:broot -> github:Canop/broot"].Actions)
	}
	if len(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions) != 1 || !strings.Contains(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions[0].Value, "remove-mise") {
		t.Fatalf("expected covered duplicate backend to expose remove action, got %#v", actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions)
	}
	if len(actionRows["ripgrep -> ripgrep"].Actions) != 1 || !strings.Contains(actionRows["ripgrep -> ripgrep"].Actions[0].Value, "remove-brew") {
		t.Fatalf("expected duplicated Homebrew/mise ownership to expose Brewfile removal action, got %#v", actionRows["ripgrep -> ripgrep"].Actions)
	}
	if len(actionRows["cargo:demo-tool -> github:example/demo-tool"].Actions) != 0 {
		t.Fatalf("expected metadata-inferred rewrite to remain review-only, got %#v", actionRows["cargo:demo-tool -> github:example/demo-tool"].Actions)
	}
	if len(actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Actions) != 0 {
		t.Fatalf("expected already-desired rewrite to remain review-only, got %#v", actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Actions)
	}
	if !strings.Contains(strings.Join(actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Metadata, " "), "review-only") {
		t.Fatalf("expected uncovered OS conditions to explain review-only applyability, got %#v", actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Metadata)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["cargo:broot -> github:Canop/broot"].Actions[0].Value); !ok || action != "rewrite-mise" || current != "cargo:broot" || recommended != "github:Canop/broot" {
		t.Fatalf("unexpected backend action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions[0].Value); !ok || action != "remove-mise" || current != "cargo:git-delta" || recommended != "aqua:dandavison/delta" {
		t.Fatalf("unexpected backend remove action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["ripgrep -> ripgrep"].Actions[0].Value); !ok || action != "remove-brew" || current != "brew:ripgrep" || recommended != "ripgrep" {
		t.Fatalf("unexpected backend Brewfile removal action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
}

func TestBackendPreferenceTiersHonorConfiguredOrder(t *testing.T) {
	config := updevConfig{Backends: updevBackendsConfig{PreferenceOrder: []string{"store/native", "mise/github", "linux/apt"}}}
	tiers := backendPreferenceTiersWithConfig(config)
	if len(tiers) < 5 {
		t.Fatalf("expected configured and default tiers, got %#v", tiers)
	}
	for index, tier := range tiers {
		if tier.Rank != index+1 {
			t.Fatalf("expected ranks to be recomputed, got %#v", tiers)
		}
	}
	if tiers[0].Label != "store/native" || tiers[0].Provider != "mas" {
		t.Fatalf("expected store/native first with default provider mapping, got %#v", tiers[0])
	}
	if tiers[1].Label != "mise/github" || tiers[1].Provider != "mise" || tiers[1].Backend != "github" {
		t.Fatalf("expected mise/github second, got %#v", tiers[1])
	}
	if tiers[2].Label != "linux/apt" || tiers[2].Provider != "linux" || tiers[2].Backend != "apt" {
		t.Fatalf("expected unknown provider/backend label to be preserved, got %#v", tiers[2])
	}
	if tiers[3].Label != "mise/core" {
		t.Fatalf("expected unspecified defaults to remain after configured tiers, got %#v", tiers[:5])
	}
}

func TestBackendPreferenceTiersExcludeDeprecatedMiseBackendsByDefault(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	tiers := backendPreferenceTiersWithConfig(updevConfig{})
	for _, tier := range tiers {
		switch tier.Label {
		case "mise/ubi", "mise/vfox", "mise/asdf":
			t.Fatalf("expected deprecated or legacy backend to stay out of defaults, got %#v", tier)
		}
	}
	ubi := backendPreferenceTierFor("mise", "ubi:owner/repo")
	if ubi.Label != "mise/ubi" || ubi.Rank != 90 {
		t.Fatalf("expected existing ubi backend to remain recognized as deprecated, got %#v", ubi)
	}
	configured := backendPreferenceTiersWithConfig(updevConfig{Backends: updevBackendsConfig{PreferenceOrder: []string{"mise/asdf"}}})
	if configured[0].Label != "mise/asdf" || configured[0].Rank != 1 {
		t.Fatalf("expected explicit deprecated backend override to be honored, got %#v", configured[0])
	}
}

func TestBackendDoctorOnlyReportsMiseBackendFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "ripgrep"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
"cargo:fd-find" = "10.4.2"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildBackendPlanReportWithRunner(context.Background(), backendOptions{command: "doctor", root: root}, &fakeCommandRunner{paths: map[string]error{"fd": nil}})
	if len(report.Findings) != 1 || report.Findings[0].Type != "mise-backend-rewrite" {
		t.Fatalf("expected doctor to report only mise backend rewrites, got %#v", report.Findings)
	}
	if report.Findings[0].CommandStatus != "on-path" {
		t.Fatalf("expected doctor to verify candidate command, got %#v", report.Findings[0])
	}
}
