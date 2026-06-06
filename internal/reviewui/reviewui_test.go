package reviewui

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

func TestPrintSectionsUsesSharedColorAndLimit(t *testing.T) {
	sections := []Section{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []Row{
			{Name: "node", Version: "24.0.0", Wanted: "latest", State: "active", Detail: "JavaScript runtime"},
			{Name: "python", Version: "3.13.0", Wanted: "latest", State: "inactive", Detail: "Python runtime"},
		},
	}}
	var out bytes.Buffer
	PrintSections(&out, sections, 1, Labels{MoreRows: func(count int) string { return "more rows" }}, true)
	text := out.String()
	if !strings.Contains(text, "mise / runtime") || !strings.Contains(text, "node") {
		t.Fatalf("expected rendered section and first row:\n%s", text)
	}
	if strings.Contains(text, "python") {
		t.Fatalf("expected limit to hide second row:\n%s", text)
	}
	if !strings.Contains(text, "more rows") {
		t.Fatalf("expected omitted row hint:\n%s", text)
	}
	if !strings.Contains(text, "\x1b[") {
		t.Fatalf("expected colorized output:\n%s", text)
	}
}

func TestFilteredSectionsSearchesSectionAndRowFields(t *testing.T) {
	sections := []Section{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []Row{
			{Name: "node", Version: "24.0.0", State: "active", Detail: "JavaScript runtime"},
			{Name: "python", Version: "3.13.0", State: "inactive", Detail: "Python runtime"},
		},
	}}
	filtered := FilteredSections(sections, "PYTHON")
	if RowCount(filtered) != 1 || filtered[0].Rows[0].Name != "python" {
		t.Fatalf("expected only python row, got %#v", filtered)
	}
	filtered = FilteredSections(sections, "runtime")
	if RowCount(filtered) != 2 {
		t.Fatalf("expected section/title query to keep both rows, got %#v", filtered)
	}
}

func TestExpandedLinesUseLabelsAndFallback(t *testing.T) {
	lines := ExpandedLines(Row{}, Labels{NoExtraDetail: "none"})
	if len(lines) != 1 || lines[0] != "none" {
		t.Fatalf("expected fallback detail line, got %#v", lines)
	}
	lines = ExpandedLines(Row{Detail: "detail text", Version: "1.0.0", Wanted: "latest", State: "active"}, Labels{Detail: "詳細", Version: "版", Wanted: "要求", State: "状態"})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"詳細", "詳細: detail text"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %#v", want, lines)
		}
	}
	for _, duplicate := range []string{"版: 1.0.0", "要求: latest", "状態: active"} {
		if strings.Contains(joined, duplicate) {
			t.Fatalf("did not expect expanded lines to repeat table metadata %q in %#v", duplicate, lines)
		}
	}
}

func TestExpandedLinesExposeRowActions(t *testing.T) {
	lines := ExpandedLines(Row{
		Name:   "jq",
		State:  "extra",
		Detail: "installed but not desired",
		Actions: []Action{{
			Value:       "open-backend",
			Label:       "open backend review",
			Description: "inspect provider ownership",
		}},
	}, Labels{})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"actions", "action 1 [press a or 1]: open backend review", "inspect provider ownership"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected expanded action line %q in %#v", want, lines)
		}
	}
}

func TestTableBrowserActionKeysReturnFocusedRowAction(t *testing.T) {
	model := NewTableBrowserModel("inventory", []Section{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows: []Row{{
			Name:  "ripgrep",
			State: "ok",
			Actions: []Action{
				{Value: "backend", Label: "open backend review"},
				{Value: "logs", Label: "open logs"},
			},
		}},
	}}, State{}, TableBrowserLabels{}, BrowserActions{}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(TableBrowserModel)
	if model.State.Action != "backend" {
		t.Fatalf("expected a to select first row action, got %#v", model.State)
	}

	model = NewTableBrowserModel("inventory", []Section{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows: []Row{{
			Name:  "ripgrep",
			State: "ok",
			Actions: []Action{
				{Value: "backend", Label: "open backend review"},
				{Value: "logs", Label: "open logs"},
			},
		}},
	}}, State{}, TableBrowserLabels{}, BrowserActions{}, false)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))
	model = updated.(TableBrowserModel)
	if model.State.Action != "logs" {
		t.Fatalf("expected 2 to select second row action, got %#v", model.State)
	}
}

func TestTableBrowserShowsFocusedActionHint(t *testing.T) {
	model := NewTableBrowserModel("inventory", []Section{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows: []Row{{
			Name:  "ripgrep",
			State: "ok",
			Actions: []Action{{
				Value: "backend",
				Label: "open backend review",
			}},
		}},
	}}, State{}, TableBrowserLabels{}, BrowserActions{}, false)
	view := model.View().Content
	if !strings.Contains(view, "focused actions: a/1=open backend review") {
		t.Fatalf("expected focused action hint in table browser:\n%s", view)
	}
}

func TestStyledRowOmitsURLFromTableDetail(t *testing.T) {
	row := Row{
		Name:    "bat",
		Version: "0.26.1",
		State:   "active",
		Detail:  "A cat clone. https://github.com/sharkdp/bat",
	}
	styled := StyledRow(row, false, false)
	if len(styled) != 4 {
		t.Fatalf("expected four table columns, got %#v", styled)
	}
	if strings.Contains(styled[3], "https://") {
		t.Fatalf("expected table detail to omit URL, got %#v", styled)
	}
	if !strings.Contains(styled[3], "A cat clone.") {
		t.Fatalf("expected table detail to keep description, got %#v", styled)
	}
}

func TestExpandedLinesPreserveAndWrapURL(t *testing.T) {
	row := Row{
		Detail: "A cat clone. https://github.com/sharkdp/bat-with-a-deliberately-long-url-segment-for-wrapping",
	}
	lines := ExpandedLines(row, Labels{})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "https://github.com") {
		t.Fatalf("expected expanded detail to preserve URL, got %#v", lines)
	}
	for _, line := range lines {
		if textui.DisplayWidth(line) > defaultExpandedDetailWidth {
			t.Fatalf("expected expanded detail to wrap at %d, got %d for %q in %#v", defaultExpandedDetailWidth, textui.DisplayWidth(line), line, lines)
		}
	}
}

func TestExpandedLinesUseRequestedWidth(t *testing.T) {
	row := Row{
		Detail: "detail text with a deliberately-long-token-for-width-sensitive-wrapping",
	}
	narrowWidth := 48
	narrow := ExpandedLinesWithWidth(row, Labels{}, narrowWidth)
	wide := ExpandedLinesWithWidth(row, Labels{}, 96)
	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrow terminal width to produce more detail lines, narrow=%#v wide=%#v", narrow, wide)
	}
	for _, line := range narrow {
		if textui.DisplayWidth(line) > narrowWidth {
			t.Fatalf("expected narrow line width <= %d, got %d for %q in %#v", narrowWidth, textui.DisplayWidth(line), line, narrow)
		}
	}
}
