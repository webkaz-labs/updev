package cmd

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type detailBrowserRow struct {
	Title    string
	Status   string
	Summary  string
	Detail   string
	Metadata []string
}

type detailBrowserState = reviewui.State

type browserMouseMode string

const (
	browserMouseOff   browserMouseMode = "off"
	browserMouseWheel browserMouseMode = "wheel"
	browserMouseClick browserMouseMode = "click"
)

type detailBrowserModel struct {
	Title               string
	Rows                []detailBrowserRow
	FilteredRows        []detailBrowserRow
	FilteredQuery       string
	State               detailBrowserState
	Color               bool
	Height              int
	Width               int
	Filtering           bool
	FilterInput         string
	Help                bool
	MouseMode           browserMouseMode
	pendingMouseRelease int
}

type detailBrowserMouseMsg struct {
	Index   int
	Release bool
}

type detailBrowserWheelMsg struct {
	Delta int
}

func runDetailBrowser(title string, rows []detailBrowserRow, color bool) (detailBrowserState, error) {
	return runDetailBrowserWithState(title, rows, detailBrowserState{}, color)
}

func runDetailBrowserWithState(title string, rows []detailBrowserRow, state detailBrowserState, color bool) (detailBrowserState, error) {
	model := newDetailBrowserModel(title, rows, state, color)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return model.State, err
	}
	if result, ok := final.(detailBrowserModel); ok {
		return result.State, nil
	}
	return model.State, nil
}

func newDetailBrowserModel(title string, rows []detailBrowserRow, state detailBrowserState, color bool) detailBrowserModel {
	if state.Expanded == nil {
		state.Expanded = map[int]bool{}
	}
	state.Action = ""
	if len(rows) == 0 {
		state.Selected = 0
	} else if state.Selected < 0 {
		state.Selected = 0
	} else if state.Selected >= len(rows) {
		state.Selected = len(rows) - 1
	}
	model := detailBrowserModel{Title: title, Rows: rows, State: state, Color: color, FilterInput: state.Query, MouseMode: browserMouseOff, pendingMouseRelease: -1}
	model.refreshFilteredRows()
	model.clampSelection()
	return model
}

func (m detailBrowserModel) Init() tea.Cmd {
	return nil
}

func nextBrowserMouseMode(mode browserMouseMode) browserMouseMode {
	switch mode {
	case browserMouseOff:
		return browserMouseWheel
	case browserMouseWheel:
		return browserMouseClick
	default:
		return browserMouseOff
	}
}

func (m detailBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.Help {
			switch msg.String() {
			case "?", "esc", "q", "b", "backspace", "enter", " ":
				m.Help = false
			}
			return m, nil
		}
		if m.Filtering {
			m.updateFilterInput(msg)
			if m.State.Action == updevActionExit {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.State.Action = updevActionExit
			return m, tea.Quit
		case "b", "left", "backspace":
			if m.clearFilter() {
				return m, nil
			}
			m.State.Action = updevActionBack
			return m, tea.Quit
		case "h":
			m.State.Action = updevActionHome
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			m.move(-10)
		case "pgdown":
			m.move(m.visibleRowCapacity())
		case "home":
			m.State.Selected = 0
			m.ensureSelectedVisible()
		case "end":
			if count := m.filteredRowCount(); count > 0 {
				m.State.Selected = count - 1
				m.ensureSelectedVisible()
			}
		case "enter", " ":
			m.toggleSelected()
		case "/":
			m.Filtering = true
			m.FilterInput = m.State.Query
		case "?":
			m.Help = true
		case "m":
			m.MouseMode = nextBrowserMouseMode(m.MouseMode)
			m.pendingMouseRelease = -1
		case "x":
			m.clearFilter()
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height
		m.Width = msg.Width
		m.ensureSelectedVisible()
	case detailBrowserMouseMsg:
		if msg.Index >= 0 && msg.Index < m.filteredRowCount() {
			if msg.Release {
				if m.pendingMouseRelease == msg.Index {
					m.pendingMouseRelease = -1
					m.handleMouseToggle(msg.Index)
					return m, nil
				}
				m.pendingMouseRelease = -1
				return m, nil
			}
			m.State.Selected = msg.Index
			m.ensureSelectedVisible()
			m.pendingMouseRelease = msg.Index
		}
	case detailBrowserWheelMsg:
		m.scroll(msg.Delta)
	}
	return m, nil
}

func (m *detailBrowserModel) clearFilter() bool {
	if m.State.Query == "" && m.FilterInput == "" {
		return false
	}
	m.State.Query = ""
	m.FilterInput = ""
	m.State.Selected = 0
	m.State.Offset = 0
	m.refreshFilteredRows()
	m.clampSelection()
	return true
}

func (m *detailBrowserModel) updateFilterInput(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "enter":
		m.Filtering = false
	case "esc":
		m.Filtering = false
		m.clearFilter()
	case "ctrl+c":
		m.State.Action = updevActionExit
		m.Filtering = false
	case "x":
		m.Filtering = false
		m.clearFilter()
	case "backspace":
		runes := []rune(m.FilterInput)
		if len(runes) > 0 {
			m.FilterInput = string(runes[:len(runes)-1])
		}
		m.applyFilterInput()
	default:
		if text := msg.Key().Text; text != "" {
			m.FilterInput += text
			m.applyFilterInput()
		}
	}
}

func (m *detailBrowserModel) applyFilterInput() {
	m.State.Query = strings.TrimSpace(m.FilterInput)
	m.State.Selected = 0
	m.State.Offset = 0
	m.refreshFilteredRows()
	m.clampSelection()
}

func (m *detailBrowserModel) handleMouseToggle(index int) {
	m.State.Selected = index
	m.toggleSelected()
}

func (m *detailBrowserModel) move(delta int) {
	count := m.filteredRowCount()
	if count == 0 {
		m.State.Selected = 0
		return
	}
	m.State.Selected += delta
	if m.State.Selected < 0 {
		m.State.Selected = 0
	}
	if m.State.Selected >= count {
		m.State.Selected = count - 1
	}
	m.ensureSelectedVisible()
}

func (m *detailBrowserModel) scroll(delta int) {
	count := m.filteredRowCount()
	if count == 0 {
		m.State.Offset = 0
		return
	}
	m.State.Offset += delta
	if m.State.Offset < 0 {
		m.State.Offset = 0
	}
	maxOffset := count - 1
	if m.State.Offset > maxOffset {
		m.State.Offset = maxOffset
	}
}

func (m *detailBrowserModel) toggleSelected() {
	if m.filteredRowCount() == 0 {
		return
	}
	if m.State.Expanded == nil {
		m.State.Expanded = map[int]bool{}
	}
	m.State.Expanded[m.State.Selected] = !m.State.Expanded[m.State.Selected]
}

func (m detailBrowserModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(tr(
		"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, / filter, m mouse, x clear, ? help, b/Backspace Back, q Exit",
		"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、/ filter、m mouse、x 解除、? help、b/Backspace 戻る、q 終了",
	), m.Color))
	line := 4
	if m.Filtering {
		fmt.Fprintf(&out, "%s %s\n", textui.StyleLabel(tr("filter:", "フィルター:"), m.Color), m.FilterInput)
		line = 5
	}
	fmt.Fprintf(&out, "%s %s\n\n", textui.StyleDim(m.positionText(), m.Color), textui.StyleDim("mouse="+string(m.MouseMode), m.Color))
	if m.Help {
		for _, line := range interactiveBrowserHelpLines() {
			fmt.Fprintf(&out, "  %s\n", line)
		}
		view := tea.NewView(out.String())
		view.AltScreen = true
		return view
	}
	filteredRows := m.filteredRows()
	if len(filteredRows) == 0 {
		fmt.Fprintf(&out, "  %s\n", textui.StyleDim(tr("no detail rows", "該当する詳細行はありません"), m.Color))
	}
	rowLines := map[int]int{}
	renderedLines := 0
	maxBodyLines := m.visibleBodyLines()
	for index, row := range filteredRows {
		if index < m.State.Offset {
			continue
		}
		if renderedLines >= maxBodyLines {
			break
		}
		rowLines[index] = line
		prefix := "  "
		if index == m.State.Selected {
			prefix = "> "
		}
		marker := "+"
		if m.State.Expanded[index] {
			marker = "-"
		}
		status := row.Status
		if status == "" {
			status = "ok"
		}
		fmt.Fprintf(&out, "%s%s %s %s %s\n", prefix, marker, textui.StyleStatus(status, m.Color), textui.StyleName(row.Title, m.Color), textTruncate(oneLine(row.Summary), 82))
		line++
		renderedLines++
		if m.State.Expanded[index] {
			expandedLines := detailBrowserExpandedLinesWithWidth(row, m.expandedDetailWidth())
			for _, expandedLine := range expandedLines {
				if renderedLines >= maxBodyLines {
					break
				}
				fmt.Fprintf(&out, "    %s\n", expandedLine)
				line++
				renderedLines++
			}
		}
	}
	view := tea.NewView(out.String())
	view.AltScreen = true
	if m.MouseMode == browserMouseOff {
		view.MouseMode = tea.MouseModeNone
		return view
	}
	view.MouseMode = tea.MouseModeCellMotion
	view.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		release := false
		switch msg.(type) {
		case tea.MouseClickMsg:
			if m.MouseMode != browserMouseClick {
				return nil
			}
		case tea.MouseReleaseMsg:
			if m.MouseMode != browserMouseClick {
				return nil
			}
			release = true
		case tea.MouseWheelMsg:
			mouse := msg.Mouse()
			switch mouse.Button {
			case tea.MouseWheelUp:
				return func() tea.Msg { return detailBrowserWheelMsg{Delta: -3} }
			case tea.MouseWheelDown:
				return func() tea.Msg { return detailBrowserWheelMsg{Delta: 3} }
			default:
				return nil
			}
		default:
			return nil
		}
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		for index, y := range rowLines {
			if mouse.Y == y {
				return func() tea.Msg { return detailBrowserMouseMsg{Index: index, Release: release} }
			}
		}
		return nil
	}
	return view
}

func interactiveBrowserHelpLines() []string {
	return []string{
		tr("Up/Down or j/k: move the focused row", "↑↓ または j/k: 行を移動"),
		tr("PgUp/PgDn or mouse wheel: scroll by a page", "PgUp/PgDn または mouse wheel: ページ単位でスクロール"),
		tr("Enter or Space: expand/collapse details", "Enter / Space: 詳細を展開/折りたたみ"),
		tr("/: filter within the current view", "/: 現在の view 内を filter"),
		tr("m: cycle mouse off/wheel/click; off is best for selecting terminal text", "m: mouse off/wheel/click を切替。端末で文字選択したい時は off"),
		tr("x: clear the current in-view filter", "x: view 内 filter を解除"),
		tr("b, Left, or Backspace: go back; clears an in-view filter first", "b / ← / Backspace: 戻る。view 内 filter があれば先に解除"),
		tr("h: return to the top hub", "h: top hub に戻る"),
		tr("q, Esc, or Ctrl-C: exit the selector", "q / Esc / Ctrl-C: selector を終了"),
		tr("?, Esc, q, b, Enter, or Space: close this help", "? / Esc / q / b / Enter / Space: この help を閉じる"),
	}
}

func (m *detailBrowserModel) clampSelection() {
	count := m.filteredRowCount()
	if count == 0 {
		m.State.Selected = 0
		m.State.Offset = 0
		return
	}
	if m.State.Selected < 0 {
		m.State.Selected = 0
	}
	if m.State.Selected >= count {
		m.State.Selected = count - 1
	}
	m.ensureSelectedVisible()
}

func (m *detailBrowserModel) ensureSelectedVisible() {
	capacity := m.visibleRowCapacity()
	if capacity <= 0 {
		capacity = 1
	}
	if m.State.Selected < m.State.Offset {
		m.State.Offset = m.State.Selected
	}
	if m.State.Selected >= m.State.Offset+capacity {
		m.State.Offset = m.State.Selected - capacity + 1
	}
	if m.State.Offset < 0 {
		m.State.Offset = 0
	}
	maxOffset := m.filteredRowCount() - 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.State.Offset > maxOffset {
		m.State.Offset = maxOffset
	}
}

func (m detailBrowserModel) visibleRowCapacity() int {
	if m.Height <= 0 {
		return 18
	}
	capacity := m.Height - 8
	if capacity < 4 {
		return 4
	}
	return capacity
}

func (m detailBrowserModel) visibleBodyLines() int {
	if m.Height <= 0 {
		return 22
	}
	lines := m.Height - 5
	if lines < 6 {
		return 6
	}
	return lines
}

func (m detailBrowserModel) positionText() string {
	count := m.filteredRowCount()
	if count == 0 {
		if m.State.Query != "" {
			return fmt.Sprintf(tr("0/%d rows, filter=%q", "0/%d 行、filter=%q"), len(m.Rows), m.State.Query)
		}
		return tr("0 rows", "0 行")
	}
	if m.State.Query != "" {
		return fmt.Sprintf(tr("row %d/%d, offset %d, filter=%q", "%d/%d 行、offset %d、filter=%q"), m.State.Selected+1, count, m.State.Offset+1, m.State.Query)
	}
	return fmt.Sprintf(tr("row %d/%d, offset %d", "%d/%d 行、offset %d"), m.State.Selected+1, count, m.State.Offset+1)
}

func (m detailBrowserModel) filteredRowCount() int {
	return len(m.filteredRows())
}

func (m detailBrowserModel) filteredRows() []detailBrowserRow {
	if m.FilteredQuery == strings.TrimSpace(strings.ToLower(m.State.Query)) {
		return m.FilteredRows
	}
	return filteredDetailRows(m.Rows, m.State.Query)
}

func (m *detailBrowserModel) refreshFilteredRows() {
	m.FilteredQuery = strings.TrimSpace(strings.ToLower(m.State.Query))
	m.FilteredRows = filteredDetailRows(m.Rows, m.State.Query)
}

func filteredDetailRows(rows []detailBrowserRow, rawQuery string) []detailBrowserRow {
	query := strings.TrimSpace(strings.ToLower(rawQuery))
	if query == "" {
		return rows
	}
	out := make([]detailBrowserRow, 0, len(rows))
	for _, row := range rows {
		if detailBrowserRowMatches(row, query) {
			out = append(out, row)
		}
	}
	return out
}

func detailBrowserRowMatches(row detailBrowserRow, query string) bool {
	haystack := strings.ToLower(strings.Join(append([]string{row.Title, row.Status, row.Summary, row.Detail}, row.Metadata...), " "))
	return strings.Contains(haystack, query)
}

func detailBrowserExpandedLines(row detailBrowserRow) []string {
	return detailBrowserExpandedLinesWithWidth(row, 0)
}

func detailBrowserExpandedLinesWithWidth(row detailBrowserRow, width int) []string {
	lines := []string{}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, wrapDetail("detail: "+strings.TrimSpace(row.Detail), width)...)
	}
	for _, meta := range row.Metadata {
		if strings.TrimSpace(meta) == "" {
			continue
		}
		lines = append(lines, wrapDetail(strings.TrimSpace(meta), width)...)
	}
	if len(lines) == 0 {
		lines = append(lines, tr("no additional detail", "追加の詳細はありません"))
	}
	return lines
}

func wrapDetail(value string, width int) []string {
	if width <= 0 {
		width = 96
	}
	return textui.WrapText(value, width)
}

func (m detailBrowserModel) expandedDetailWidth() int {
	if m.Width <= 0 {
		return 0
	}
	return m.Width - 4
}
