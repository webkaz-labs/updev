package cmd

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
)

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

func selectedDetailActionForKey(rows []detailBrowserRow, key tea.KeyPressMsg) string {
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ := model.Update(key)
	model = updated.(detailBrowserModel)
	return model.State.Action
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
