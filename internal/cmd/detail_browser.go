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
	Actions  []detailBrowserAction
}

type detailBrowserAction struct {
	Value       string
	Label       string
	Description string
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
	PrimaryEnterAction  bool
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
	return runDetailBrowserModel(model)
}

func runPrimaryActionBrowserWithState(title string, rows []detailBrowserRow, state detailBrowserState, color bool) (detailBrowserState, error) {
	model := newDetailBrowserModel(title, rows, state, color)
	model.PrimaryEnterAction = true
	return runDetailBrowserModel(model)
}

func runDetailBrowserModel(model detailBrowserModel) (detailBrowserState, error) {
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
		case "enter":
			if m.PrimaryEnterAction && m.selectRowAction(0) {
				return m, tea.Quit
			}
			m.toggleSelected()
		case " ":
			m.toggleSelected()
		case "a":
			if m.selectRowAction(0) {
				return m, tea.Quit
			}
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
		default:
			if index, ok := detailBrowserActionKeyIndex(msg.String()); ok {
				if m.selectRowAction(index) {
					return m, tea.Quit
				}
			}
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
	m.ensureSelectedVisible()
}

func (m *detailBrowserModel) selectRowAction(index int) bool {
	rows := m.filteredRows()
	if m.State.Selected < 0 || m.State.Selected >= len(rows) {
		return false
	}
	row := rows[m.State.Selected]
	if index < 0 || index >= len(row.Actions) || row.Actions[index].Value == "" {
		return false
	}
	m.State.Action = row.Actions[index].Value
	return true
}

func (m detailBrowserModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	line := 1
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(m.keyboardSummary(), m.Color))
	line++
	if m.Filtering {
		fmt.Fprintf(&out, "%s %s\n", textui.StyleLabel(tr("filter:", "フィルター:"), m.Color), m.FilterInput)
		line++
	}
	fmt.Fprintf(&out, "%s %s\n", textui.StyleDim(m.positionText(), m.Color), textui.StyleDim("mouse="+string(m.MouseMode), m.Color))
	line++
	if hint := m.selectedActionHint(); hint != "" {
		if m.Width > 0 {
			hint = textTruncate(hint, m.Width)
		}
		fmt.Fprintf(&out, "%s\n", textui.StyleDim(hint, m.Color))
		line++
	}
	fmt.Fprintln(&out)
	line++
	if m.Help {
		for _, line := range m.helpLines() {
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
	maxBodyLines := m.visibleBodyLines(line)
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
		summary := detailBrowserCollapsedSummary(row)
		fmt.Fprintf(&out, "%s%s %s %s %s\n", prefix, marker, textui.StyleStatus(status, m.Color), textui.StyleName(row.Title, m.Color), detailBrowserStyleSummary(textTruncate(summary, 82), status, m.Color))
		line++
		renderedLines++
		if m.State.Expanded[index] {
			expandedLines := detailBrowserExpandedLinesStyled(row, m.expandedDetailWidth(), m.Color)
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

func (m detailBrowserModel) keyboardSummary() string {
	if m.PrimaryEnterAction {
		return tr(
			"Up/Down/j/k move, PgUp/PgDn scroll, Enter action, Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
			"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter 操作、Space 展開、a/1-9 action、/ filter、m mouse、x 解除、? help、b 戻る、q 終了",
		)
	}
	return tr(
		"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
		"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、a/1-9 action、/ filter、m mouse、x 解除、? help、b 戻る、q 終了",
	)
}

func (m detailBrowserModel) selectedActionHint() string {
	rows := m.filteredRows()
	if m.State.Selected < 0 || m.State.Selected >= len(rows) {
		return ""
	}
	actions := rows[m.State.Selected].Actions
	if len(actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for index, action := range actions {
		if strings.TrimSpace(action.Label) == "" {
			continue
		}
		key := fmt.Sprintf("%d", index+1)
		if index == 0 {
			key = "a/1"
		}
		parts = append(parts, key+"="+action.Label)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(actions) > len(parts) {
		parts = append(parts, fmt.Sprintf("+%d", len(actions)-len(parts)))
	}
	return tr("focused actions: ", "選択中の操作: ") + strings.Join(parts, ", ")
}

func interactiveBrowserHelpLines() []string {
	return []string{
		tr("Up/Down or j/k: move the focused row", "↑↓ または j/k: 行を移動"),
		tr("PgUp/PgDn or mouse wheel: scroll by a page", "PgUp/PgDn または mouse wheel: ページ単位でスクロール"),
		tr("Enter or Space: expand/collapse details", "Enter / Space: 詳細を展開/折りたたみ"),
		tr("a: run the first action on the focused row", "a: focus 行の最初の action を実行"),
		tr("1-9: run the numbered action shown in expanded details", "1-9: 展開詳細に表示された番号の action を実行"),
		tr("/: filter within the current view", "/: 現在の view 内を filter"),
		tr("m: cycle mouse off/wheel/click; off is best for selecting terminal text", "m: mouse off/wheel/click を切替。端末で文字選択したい時は off"),
		tr("x: clear the current in-view filter", "x: view 内 filter を解除"),
		tr("b, Left, or Backspace: go back; clears an in-view filter first", "b / ← / Backspace: 戻る。view 内 filter があれば先に解除"),
		tr("h: return to the top hub", "h: top hub に戻る"),
		tr("q, Esc, or Ctrl-C: exit the selector", "q / Esc / Ctrl-C: selector を終了"),
		tr("?, Esc, q, b, Enter, or Space: close this help", "? / Esc / q / b / Enter / Space: この help を閉じる"),
	}
}

func (m detailBrowserModel) helpLines() []string {
	lines := interactiveBrowserHelpLines()
	if !m.PrimaryEnterAction {
		return lines
	}
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.Contains(line, "Enter or Space:") || strings.Contains(line, "Enter / Space:") {
			out = append(out,
				tr("Enter: run the first action on the focused row", "Enter: focus 行の最初の action を実行"),
				tr("Space: expand/collapse details", "Space: 詳細を展開/折りたたみ"),
			)
			continue
		}
		out = append(out, line)
	}
	return out
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
	rows := m.filteredRows()
	for attempts := 0; attempts <= len(rows); attempts++ {
		if detailBrowserRowBlockVisible(rows, m.State.Offset, m.visibleBodyLines(5), m.State.Expanded, m.expandedDetailWidth(), m.State.Selected) {
			return
		}
		if m.State.Selected < m.State.Offset {
			m.State.Offset = m.State.Selected
			continue
		}
		if m.State.Offset < m.State.Selected {
			m.State.Offset++
			if m.State.Offset > maxOffset {
				m.State.Offset = maxOffset
				return
			}
			continue
		}
		return
	}
}

func detailBrowserRowBlockVisible(rows []detailBrowserRow, offset int, maxBodyLines int, expanded map[int]bool, width int, selected int) bool {
	if selected < 0 || selected >= len(rows) || maxBodyLines <= 0 {
		return false
	}
	renderedLines := 0
	for index := offset; index < len(rows); index++ {
		if renderedLines >= maxBodyLines {
			return false
		}
		rowLines := 1
		if expanded[index] {
			rowLines += len(detailBrowserExpandedLinesWithWidth(rows[index], width))
		}
		if index == selected {
			return renderedLines+rowLines <= maxBodyLines
		}
		renderedLines += rowLines
	}
	return false
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

func (m detailBrowserModel) visibleBodyLines(usedLines int) int {
	if m.Height <= 0 {
		return 22
	}
	lines := m.Height - usedLines - 1
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
	parts := append([]string{row.Title, row.Status, row.Summary, row.Detail}, row.Metadata...)
	for _, action := range row.Actions {
		parts = append(parts, action.Label, action.Description)
	}
	haystack := strings.ToLower(strings.Join(parts, " "))
	return strings.Contains(haystack, query)
}

func detailBrowserExpandedLines(row detailBrowserRow) []string {
	return detailBrowserExpandedLinesWithWidth(row, 0)
}

func detailBrowserExpandedLinesWithWidth(row detailBrowserRow, width int) []string {
	return detailBrowserExpandedLinesStyled(row, width, false)
}

func detailBrowserExpandedLinesStyled(row detailBrowserRow, width int, color bool) []string {
	lines := []string{}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, browserSectionHeadingText(tr("details", "詳細"), color))
		lines = append(lines, detailBrowserDetailLines(row.Detail, width, color)...)
	}
	metadataStarted := false
	for _, meta := range row.Metadata {
		if strings.TrimSpace(meta) == "" {
			continue
		}
		if !metadataStarted {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, browserSectionHeadingText(tr("evidence", "根拠"), color))
			metadataStarted = true
		}
		lines = append(lines, wrapDetail(detailBrowserMetadataLine(strings.TrimSpace(meta), color), width)...)
	}
	actionsStarted := false
	for index, action := range row.Actions {
		if strings.TrimSpace(action.Label) == "" {
			continue
		}
		if !actionsStarted {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, browserSectionHeadingText(tr("actions", "操作"), color))
			actionsStarted = true
		}
		lines = append(lines, wrapDetail(detailBrowserActionLine(index, action, color), width)...)
	}
	if len(lines) == 0 {
		lines = append(lines, textui.StyleDim(tr("no additional detail", "追加の詳細はありません"), color))
	}
	return lines
}

func detailBrowserDetailLines(detail string, width int, color bool) []string {
	lines := []string{}
	rawLines := splitNonEmpty(strings.ReplaceAll(detail, "\r\n", "\n"), "\n")
	for index, rawLine := range rawLines {
		line := strings.TrimSpace(rawLine)
		if isDetailBrowserKeyValueLine(line) {
			lines = append(lines, wrapDetail(detailBrowserMetadataLine(line, color), width)...)
			continue
		}
		key := "detail"
		if index > 0 {
			key = "note"
		}
		lines = append(lines, wrapDetail(detailBrowserKeyValueLine(key, line, color), width)...)
	}
	return lines
}

func detailBrowserKeyValueLine(key string, value string, color bool) string {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return value
	}
	return textui.StyleKey(key+":", color) + " " + value
}

func detailBrowserMetadataLine(line string, color bool) string {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return line
	}
	return detailBrowserKeyValueLine(key, value, color)
}

func isDetailBrowserKeyValueLine(line string) bool {
	key, _, ok := strings.Cut(strings.TrimSpace(line), ":")
	key = strings.TrimSpace(key)
	return ok && key != "" && !strings.ContainsAny(key, " \t/") && len([]rune(key)) <= 32
}

func detailBrowserActionLine(index int, action detailBrowserAction, color bool) string {
	key := fmt.Sprintf("action %d [press %d]:", index+1, index+1)
	if index == 0 {
		key = fmt.Sprintf("action %d [press a or 1]:", index+1)
	}
	label := textui.StyleAction(strings.TrimSpace(action.Label), color)
	line := textui.StyleKey(key, color) + " " + label
	if strings.TrimSpace(action.Description) != "" {
		line += textui.StyleDim(" - "+strings.TrimSpace(action.Description), color)
	}
	return line
}

func detailBrowserCollapsedSummary(row detailBrowserRow) string {
	parts := detailBrowserRowBadges(row)
	if summary := strings.TrimSpace(oneLine(row.Summary)); summary != "" {
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func detailBrowserRowBadges(row detailBrowserRow) []string {
	badges := []string{}
	if len(row.Actions) > 0 {
		badges = append(badges, fmt.Sprintf("[%s:%d]", tr("actions", "操作"), len(row.Actions)))
	}
	for _, meta := range row.Metadata {
		key, value, ok := strings.Cut(strings.TrimSpace(meta), ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(oneLine(value))
		switch key {
		case "updated":
			badges = append(badges, "[updated:"+detailBrowserListCount(value)+"]")
		case "deferred":
			badges = append(badges, "[deferred:"+detailBrowserListCount(value)+"]")
		case "applyability", "適用可否":
			lower := strings.ToLower(value)
			switch {
			case strings.Contains(lower, "applyable") || strings.Contains(value, "適用可能"):
				badges = append(badges, "[applyable]")
			case strings.Contains(lower, "review-only"):
				badges = append(badges, "[review-only]")
			}
		case "decision":
			if value != "" {
				badges = append(badges, "[decision:"+strings.Fields(value)[0]+"]")
			}
		case "release assets", "release asset":
			if value != "" {
				badges = append(badges, "[assets:"+strings.Fields(value)[0]+"]")
			}
		}
	}
	return compactUniqueStrings(badges)
}

func detailBrowserListCount(value string) string {
	if strings.TrimSpace(value) == "" {
		return "0"
	}
	count := 1
	for _, sep := range []string{";", ","} {
		if strings.Contains(value, sep) {
			count = len(splitNonEmpty(value, sep))
			break
		}
	}
	return fmt.Sprintf("%d", count)
}

func splitNonEmpty(value string, sep string) []string {
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

func compactUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func detailBrowserStyleSummary(value string, status string, color bool) string {
	if value == "" {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "allow", "active", "updated":
		return textui.StyleLabel(value, color)
	case "missing", "extra", "drift", "held", "hold", "review", "unavailable", "attention", "skipped", "deferred", "needs-review", "ignore-local", "adopt-brew", "adopt-mas", "open-vendor":
		return textui.StyleWarning(value, color)
	case "error", "blocked":
		return textui.StyleError(value, color)
	default:
		return textui.StyleDim(value, color)
	}
}

func detailBrowserActionKeyIndex(key string) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	return int(key[0] - '1'), true
}

func wrapDetail(value string, width int) []string {
	if width <= 0 {
		width = 96
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := []string{}
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, textui.WrapText(line, width)...)
	}
	return lines
}

func (m detailBrowserModel) expandedDetailWidth() int {
	if m.Width <= 0 {
		return 0
	}
	return m.Width - 4
}
