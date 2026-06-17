package reviewui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newDetailBrowserModel(title string, rows []detailBrowserRow, state detailBrowserState, color bool) DetailBrowserModel {
	return NewDetailBrowserModel(DetailBrowserOptions{
		Title:   title,
		Rows:    rows,
		State:   state,
		Labels:  DetailBrowserLabels{},
		Format:  DetailBrowserFormatters{},
		Actions: BrowserActions{Back: "back", Home: "home", Exit: "exit"},
		Color:   color,
	})
}

func TestDetailBrowserModelTogglesAndRendersExpandedDetail(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Status: "ok", Summary: "short", Detail: "full detail"},
		{Title: "two", Status: "held", Summary: "summary", Detail: "second detail", Metadata: []string{"updated: jq; git", "applyability: review-only"}, Actions: []detailBrowserAction{{Value: "demo", Label: "review", Description: "inspect evidence"}}},
	}, detailBrowserState{}, false)
	model.Move(1)
	if model.State.Selected != 1 {
		t.Fatalf("expected selected row 1, got %d", model.State.Selected)
	}
	model.ToggleSelected()
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
	model = updated.(DetailBrowserModel)
	if !strings.Contains(model.View().Content, "> action 1 [press a or 1]: review") {
		t.Fatalf("expected down key to focus expanded action:\n%s", model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(DetailBrowserModel)
	if model.State.Action != "demo" {
		t.Fatalf("expected Enter on focused action to select action, got %#v", model.State)
	}
	coloredLines := strings.Join(DetailBrowserExpandedLinesStyled(detailBrowserRow{
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

func TestDetailBrowserActionLineCompactsLongEvidenceDescriptions(t *testing.T) {
	line := detailBrowserActionLine(0, detailBrowserAction{
		Value:       "security",
		Label:       "security review",
		Description: "brew/brew mise: hold: release too new; cache: brew 3h; source: /Users/example/Brewfile; download: https://example.com/archive.tar.gz",
	}, false, true)
	for _, want := range []string{"> action 1 [press a or 1]: security review", "release too new"} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected compact action line to contain %q:\n%s", want, line)
		}
	}
	for _, unwanted := range []string{"source:", "download:"} {
		if strings.Contains(line, unwanted) {
			t.Fatalf("expected compact action line to avoid %q:\n%s", unwanted, line)
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
	model.Move(10)
	model.ToggleSelected()
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
	model = updated.(DetailBrowserModel)
	if model.State.Action != "first" {
		t.Fatalf("expected a to select first row action, got %#v", model.State)
	}

	model = newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))
	model = updated.(DetailBrowserModel)
	if model.State.Action != "second" {
		t.Fatalf("expected 2 to select second row action, got %#v", model.State)
	}
}

func TestDetailBrowserStateRestoresFocusedAction(t *testing.T) {
	rows := []detailBrowserRow{{
		Title:   "action row",
		Status:  "held",
		Summary: "needs action",
		Actions: []detailBrowserAction{
			{Value: "first", Label: "first action"},
			{Value: "second", Label: "second action"},
		},
	}}
	model := newDetailBrowserModel("details", rows, detailBrowserState{Expanded: map[int]bool{0: true}}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(DetailBrowserModel)
	if model.State.ActionFocus != 2 {
		t.Fatalf("expected second focused action to be persisted as one-based state, got %#v", model.State)
	}

	restored := newDetailBrowserModel("details", rows, model.State, false)
	updated, _ = restored.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	restored = updated.(DetailBrowserModel)
	if restored.State.Action != "second" {
		t.Fatalf("expected restored focused action to run second action, got %#v", restored.State)
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
	model.Move(1)
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

func TestDetailBrowserDoesNotTreatURLsAsKeyValueLines(t *testing.T) {
	lines := strings.Join(DetailBrowserDetailLines("go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html", 120, false), "\n")
	if strings.Contains(lines, "https: //") {
		t.Fatalf("did not expect URL scheme to be split as a key-value line:\n%s", lines)
	}
	if !strings.Contains(lines, "detail: go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html") {
		t.Fatalf("expected URL to remain inside the detail text:\n%s", lines)
	}
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
	got := DetailBrowserCollapsedSummary(row)
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
	model = updated.(DetailBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected click press to select without expanding row: %#v", model.State)
	}
	release := model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if release == nil {
		t.Fatal("expected row release to map to detail row")
	}
	updated, _ = model.Update(release())
	model = updated.(DetailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected matching release to expand row: %#v", model.State)
	}
	click = model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(click())
	model = updated.(DetailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected second click press not to collapse row: %#v", model.State)
	}
	release = model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(release())
	model = updated.(DetailBrowserModel)
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
	model = updated.(DetailBrowserModel)
	if model.State.Query != "" || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(DetailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected detail filter to update while typing: %#v\n%s", model.State, model.View().Content)
	}
	if view := model.View().Content; strings.Index(view, "filter: jq") < 0 || strings.Index(view, "filter: jq") > strings.Index(view, "jq") {
		t.Fatalf("expected active detail filter input near the top:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	model = updated.(DetailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected esc to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(DetailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(DetailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(DetailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected slash filter input to apply detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(DetailBrowserModel)
	if model.State.Query != "" || model.State.Action != "" {
		t.Fatalf("expected back to clear detail filter before leaving: %#v", model.State)
	}
}
