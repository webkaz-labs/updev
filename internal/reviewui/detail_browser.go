package reviewui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type (
	detailBrowserRow    = DetailRow
	detailBrowserAction = DetailAction
	detailBrowserState  = State
)

const (
	browserMouseOff   MouseMode = MouseOff
	browserMouseWheel MouseMode = MouseWheel
	browserMouseClick MouseMode = MouseClick
)

type DetailBrowserModel struct {
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
	MouseMode           MouseMode
	pendingMouseRelease int
	PrimaryEnterAction  bool
	actionFocus         int
	actions             BrowserActions
	labels              DetailBrowserLabels
	format              DetailBrowserFormatters
}

type detailBrowserMouseMsg struct {
	Index   int
	Release bool
}

type detailBrowserWheelMsg struct {
	Delta int
}

type DetailBrowserLabels struct {
	Keyboard             string
	PrimaryKeyboard      string
	Filter               string
	NoRows               string
	FocusedActionsPrefix string
	HelpLines            []string
	PrimaryEnterHelp     string
	PrimarySpaceHelp     string
	DetailsHeading       string
	EvidenceHeading      string
	ActionsHeading       string
	NoAdditionalDetail   string
	ActionsBadge         string
	PositionZeroFilter   func(total int, query string) string
	PositionZero         string
	PositionFilter       func(selected int, count int, offset int, query string) string
	Position             func(selected int, count int, offset int) string
}

type DetailBrowserFormatters struct {
	Truncate         func(string, int) string
	OneLine          func(string) string
	SectionHeading   func(string, bool) string
	LocalizeEvidence func(string) string
}

func RunDetailBrowserModel(model DetailBrowserModel) (detailBrowserState, error) {
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return model.State, err
	}
	if result, ok := final.(DetailBrowserModel); ok {
		return result.State, nil
	}
	return model.State, nil
}

type DetailBrowserOptions struct {
	Title   string
	Rows    []DetailRow
	State   State
	Labels  DetailBrowserLabels
	Format  DetailBrowserFormatters
	Actions BrowserActions
	Color   bool
}

func NewDetailBrowserModel(options DetailBrowserOptions) DetailBrowserModel {
	state := options.State
	if state.Expanded == nil {
		state.Expanded = map[int]bool{}
	}
	state.Action = ""
	if len(options.Rows) == 0 {
		state.Selected = 0
	} else if state.Selected < 0 {
		state.Selected = 0
	} else if state.Selected >= len(options.Rows) {
		state.Selected = len(options.Rows) - 1
	}
	model := DetailBrowserModel{
		Title:               options.Title,
		Rows:                options.Rows,
		State:               state,
		Color:               options.Color,
		FilterInput:         state.Query,
		MouseMode:           browserMouseOff,
		pendingMouseRelease: -1,
		actionFocus:         -1,
		actions:             fillActions(options.Actions),
		labels:              fillDetailBrowserLabels(options.Labels),
		format:              fillDetailBrowserFormatters(options.Format),
	}
	model.refreshFilteredRows()
	model.clampSelection()
	return model
}

func DefaultDetailBrowserLabels() DetailBrowserLabels {
	return DetailBrowserLabels{
		Keyboard:             "Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
		PrimaryKeyboard:      "Up/Down/j/k move, PgUp/PgDn scroll, Enter action, Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
		Filter:               "filter:",
		NoRows:               "no detail rows",
		FocusedActionsPrefix: "focused actions: ",
		HelpLines: []string{
			"Up/Down or j/k: move the focused row",
			"PgUp/PgDn or mouse wheel: scroll by a page",
			"Enter or Space: expand/collapse details",
			"a: run the first action on the focused row",
			"1-9: run the numbered action shown in expanded details",
			"/: filter within the current view",
			"m: cycle mouse off/wheel/click; off is best for selecting terminal text",
			"x: clear the current in-view filter",
			"b, Left, or Backspace: go back; clears an in-view filter first",
			"h: return to the top hub",
			"q, Esc, or Ctrl-C: exit the selector",
			"?, Esc, q, b, Enter, or Space: close this help",
		},
		PrimaryEnterHelp:   "Enter: run the first action on the focused row",
		PrimarySpaceHelp:   "Space: expand/collapse details",
		DetailsHeading:     "details",
		EvidenceHeading:    "evidence",
		ActionsHeading:     "actions",
		NoAdditionalDetail: "no additional detail",
		ActionsBadge:       "actions",
		PositionZeroFilter: func(total int, query string) string {
			return fmt.Sprintf("0/%d rows, filter=%q", total, query)
		},
		PositionZero: "0 rows",
		PositionFilter: func(selected int, count int, offset int, query string) string {
			return fmt.Sprintf("row %d/%d, offset %d, filter=%q", selected, count, offset, query)
		},
		Position: func(selected int, count int, offset int) string {
			return fmt.Sprintf("row %d/%d, offset %d", selected, count, offset)
		},
	}
}

func DefaultDetailBrowserFormatters() DetailBrowserFormatters {
	return DetailBrowserFormatters{
		Truncate:         textui.Truncate,
		OneLine:          func(text string) string { return strings.Join(strings.Fields(text), " ") },
		SectionHeading:   func(text string, color bool) string { return textui.StyleSection(strings.TrimSpace(text), color) },
		LocalizeEvidence: func(text string) string { return strings.TrimSpace(text) },
	}
}

func fillDetailBrowserLabels(labels DetailBrowserLabels) DetailBrowserLabels {
	defaults := DefaultDetailBrowserLabels()
	if labels.Keyboard == "" {
		labels.Keyboard = defaults.Keyboard
	}
	if labels.PrimaryKeyboard == "" {
		labels.PrimaryKeyboard = defaults.PrimaryKeyboard
	}
	if labels.Filter == "" {
		labels.Filter = defaults.Filter
	}
	if labels.NoRows == "" {
		labels.NoRows = defaults.NoRows
	}
	if labels.FocusedActionsPrefix == "" {
		labels.FocusedActionsPrefix = defaults.FocusedActionsPrefix
	}
	if len(labels.HelpLines) == 0 {
		labels.HelpLines = defaults.HelpLines
	}
	if labels.PrimaryEnterHelp == "" {
		labels.PrimaryEnterHelp = defaults.PrimaryEnterHelp
	}
	if labels.PrimarySpaceHelp == "" {
		labels.PrimarySpaceHelp = defaults.PrimarySpaceHelp
	}
	if labels.DetailsHeading == "" {
		labels.DetailsHeading = defaults.DetailsHeading
	}
	if labels.EvidenceHeading == "" {
		labels.EvidenceHeading = defaults.EvidenceHeading
	}
	if labels.ActionsHeading == "" {
		labels.ActionsHeading = defaults.ActionsHeading
	}
	if labels.NoAdditionalDetail == "" {
		labels.NoAdditionalDetail = defaults.NoAdditionalDetail
	}
	if labels.ActionsBadge == "" {
		labels.ActionsBadge = defaults.ActionsBadge
	}
	if labels.PositionZeroFilter == nil {
		labels.PositionZeroFilter = defaults.PositionZeroFilter
	}
	if labels.PositionZero == "" {
		labels.PositionZero = defaults.PositionZero
	}
	if labels.PositionFilter == nil {
		labels.PositionFilter = defaults.PositionFilter
	}
	if labels.Position == nil {
		labels.Position = defaults.Position
	}
	return labels
}

func fillDetailBrowserFormatters(format DetailBrowserFormatters) DetailBrowserFormatters {
	defaults := DefaultDetailBrowserFormatters()
	if format.Truncate == nil {
		format.Truncate = defaults.Truncate
	}
	if format.OneLine == nil {
		format.OneLine = defaults.OneLine
	}
	if format.SectionHeading == nil {
		format.SectionHeading = defaults.SectionHeading
	}
	if format.LocalizeEvidence == nil {
		format.LocalizeEvidence = defaults.LocalizeEvidence
	}
	return format
}

func (m DetailBrowserModel) Init() tea.Cmd {
	return nil
}

func nextBrowserMouseMode(mode MouseMode) MouseMode {
	switch mode {
	case browserMouseOff:
		return browserMouseWheel
	case browserMouseWheel:
		return browserMouseClick
	default:
		return browserMouseOff
	}
}

func (m DetailBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.State.Action == m.actions.Exit {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.State.Action = m.actions.Exit
			return m, tea.Quit
		case "b", "left", "backspace":
			if m.clearFilter() {
				return m, nil
			}
			m.State.Action = m.actions.Back
			return m, tea.Quit
		case "h":
			m.State.Action = m.actions.Home
			return m, tea.Quit
		case "up", "k":
			m.moveFocus(-1)
		case "down", "j":
			m.moveFocus(1)
		case "pgup":
			m.Move(-10)
		case "pgdown":
			m.Move(m.visibleRowCapacity())
		case "home":
			m.State.Selected = 0
			m.actionFocus = -1
			m.EnsureSelectedVisible()
		case "end":
			if count := m.filteredRowCount(); count > 0 {
				m.State.Selected = count - 1
				m.actionFocus = -1
				m.EnsureSelectedVisible()
			}
		case "enter":
			if m.selectFocusedAction() {
				return m, tea.Quit
			}
			if m.PrimaryEnterAction && m.selectRowAction(0) {
				return m, tea.Quit
			}
			m.ToggleSelected()
		case " ":
			m.ToggleSelected()
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
			if index, ok := DetailBrowserActionKeyIndex(msg.String()); ok {
				if m.selectRowAction(index) {
					return m, tea.Quit
				}
			}
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height
		m.Width = msg.Width
		m.EnsureSelectedVisible()
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
			m.actionFocus = -1
			m.EnsureSelectedVisible()
			m.pendingMouseRelease = msg.Index
		}
	case detailBrowserWheelMsg:
		m.scroll(msg.Delta)
	}
	return m, nil
}

func (m *DetailBrowserModel) clearFilter() bool {
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

func (m *DetailBrowserModel) updateFilterInput(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "enter":
		m.Filtering = false
	case "esc":
		m.Filtering = false
		m.clearFilter()
	case "ctrl+c":
		m.State.Action = m.actions.Exit
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

func (m *DetailBrowserModel) applyFilterInput() {
	m.State.Query = strings.TrimSpace(m.FilterInput)
	m.State.Selected = 0
	m.State.Offset = 0
	m.actionFocus = -1
	m.refreshFilteredRows()
	m.clampSelection()
}

func (m *DetailBrowserModel) handleMouseToggle(index int) {
	m.State.Selected = index
	m.actionFocus = -1
	m.ToggleSelected()
}

func (m *DetailBrowserModel) Move(delta int) {
	count := m.filteredRowCount()
	if count == 0 {
		m.State.Selected = 0
		return
	}
	m.State.Selected += delta
	m.actionFocus = -1
	if m.State.Selected < 0 {
		m.State.Selected = 0
	}
	if m.State.Selected >= count {
		m.State.Selected = count - 1
	}
	m.EnsureSelectedVisible()
}

func (m *DetailBrowserModel) moveFocus(delta int) {
	if delta == 0 {
		return
	}
	rows := m.filteredRows()
	if m.State.Selected >= 0 && m.State.Selected < len(rows) && m.State.Expanded[m.State.Selected] {
		actionCount := len(rows[m.State.Selected].Actions)
		if actionCount > 0 {
			if m.actionFocus < 0 && delta < 0 {
				m.Move(delta)
				return
			}
			next := m.actionFocus + delta
			if m.actionFocus < 0 && delta > 0 {
				next = 0
			}
			if next >= 0 && next < actionCount {
				m.actionFocus = next
				m.EnsureSelectedVisible()
				return
			}
			if next < 0 {
				m.actionFocus = -1
				m.EnsureSelectedVisible()
				return
			}
			m.actionFocus = -1
		}
	}
	m.Move(delta)
}

func (m *DetailBrowserModel) scroll(delta int) {
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

func (m *DetailBrowserModel) ToggleSelected() {
	if m.filteredRowCount() == 0 {
		return
	}
	if m.State.Expanded == nil {
		m.State.Expanded = map[int]bool{}
	}
	m.State.Expanded[m.State.Selected] = !m.State.Expanded[m.State.Selected]
	m.actionFocus = -1
	m.EnsureSelectedVisible()
}

func (m *DetailBrowserModel) selectRowAction(index int) bool {
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

func (m *DetailBrowserModel) selectFocusedAction() bool {
	rows := m.filteredRows()
	if m.State.Selected < 0 || m.State.Selected >= len(rows) || !m.State.Expanded[m.State.Selected] || m.actionFocus < 0 {
		return false
	}
	return m.selectRowAction(m.actionFocus)
}

func (m DetailBrowserModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	line := 1
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(m.keyboardSummary(), m.Color))
	line++
	if m.Filtering {
		fmt.Fprintf(&out, "%s %s\n", textui.StyleLabel(m.labels.Filter, m.Color), m.FilterInput)
		line++
	}
	fmt.Fprintf(&out, "%s %s\n", textui.StyleDim(m.positionText(), m.Color), textui.StyleDim("mouse="+string(m.MouseMode), m.Color))
	line++
	hint := m.selectedActionHint()
	if m.Width > 0 {
		hint = m.format.Truncate(hint, m.Width)
	}
	if hint != "" {
		fmt.Fprintf(&out, "%s\n", textui.StyleDim(hint, m.Color))
	} else {
		fmt.Fprintln(&out)
	}
	line++
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
		fmt.Fprintf(&out, "  %s\n", textui.StyleDim(m.labels.NoRows, m.Color))
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
		summary := m.collapsedSummary(row)
		fmt.Fprintf(&out, "%s%s %s %s %s\n", prefix, marker, textui.StyleStatus(status, m.Color), textui.StyleName(row.Title, m.Color), detailBrowserStyleSummary(m.format.Truncate(summary, 82), status, m.Color))
		line++
		renderedLines++
		if m.State.Expanded[index] {
			actionFocus := -1
			if index == m.State.Selected {
				actionFocus = m.actionFocus
			}
			expandedLines := m.expandedLinesStyledFocus(row, m.expandedDetailWidth(), m.Color, actionFocus)
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

func (m DetailBrowserModel) keyboardSummary() string {
	if m.PrimaryEnterAction {
		return m.labels.PrimaryKeyboard
	}
	return m.labels.Keyboard
}

func (m DetailBrowserModel) selectedActionHint() string {
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
	return m.labels.FocusedActionsPrefix + strings.Join(parts, ", ")
}

func interactiveBrowserHelpLines() []string {
	return DefaultDetailBrowserLabels().HelpLines
}

func (m DetailBrowserModel) helpLines() []string {
	lines := m.labels.HelpLines
	if !m.PrimaryEnterAction {
		return lines
	}
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.Contains(line, "Enter or Space:") || strings.Contains(line, "Enter / Space:") {
			out = append(
				out,
				m.labels.PrimaryEnterHelp,
				m.labels.PrimarySpaceHelp,
			)
			continue
		}
		out = append(out, line)
	}
	return out
}

func (m *DetailBrowserModel) clampSelection() {
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
	m.EnsureSelectedVisible()
}

func (m *DetailBrowserModel) EnsureSelectedVisible() {
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
		if detailBrowserRowBlockVisible(rows, m.State.Offset, m.visibleBodyLines(m.headerLineCount()), m.State.Expanded, m.expandedDetailWidth(), m.State.Selected) {
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

func (m DetailBrowserModel) headerLineCount() int {
	lines := 5
	if m.Filtering {
		lines++
	}
	return lines
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
			rowLines += len(DetailBrowserExpandedLinesWithWidth(rows[index], width))
		}
		if index == selected {
			return renderedLines+rowLines <= maxBodyLines
		}
		renderedLines += rowLines
	}
	return false
}

func (m DetailBrowserModel) visibleRowCapacity() int {
	if m.Height <= 0 {
		return 18
	}
	capacity := m.Height - 8
	if capacity < 4 {
		return 4
	}
	return capacity
}

func (m DetailBrowserModel) visibleBodyLines(usedLines int) int {
	if m.Height <= 0 {
		return 22
	}
	lines := m.Height - usedLines - 1
	if lines < 6 {
		return 6
	}
	return lines
}

func (m DetailBrowserModel) positionText() string {
	count := m.filteredRowCount()
	if count == 0 {
		if m.State.Query != "" {
			return m.labels.PositionZeroFilter(len(m.Rows), m.State.Query)
		}
		return m.labels.PositionZero
	}
	if m.State.Query != "" {
		return m.labels.PositionFilter(m.State.Selected+1, count, m.State.Offset+1, m.State.Query)
	}
	return m.labels.Position(m.State.Selected+1, count, m.State.Offset+1)
}

func (m DetailBrowserModel) filteredRowCount() int {
	return len(m.filteredRows())
}

func (m DetailBrowserModel) filteredRows() []detailBrowserRow {
	if m.FilteredQuery == strings.TrimSpace(strings.ToLower(m.State.Query)) {
		return m.FilteredRows
	}
	return filteredDetailRows(m.Rows, m.State.Query)
}

func (m *DetailBrowserModel) refreshFilteredRows() {
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

func DetailBrowserExpandedLines(row detailBrowserRow) []string {
	return DetailBrowserExpandedLinesWithWidth(row, 0)
}

func DetailBrowserExpandedLinesWithWidth(row detailBrowserRow, width int) []string {
	return DetailBrowserExpandedLinesStyled(row, width, false)
}

func DetailBrowserExpandedLinesStyled(row detailBrowserRow, width int, color bool) []string {
	return DetailBrowserExpandedLinesStyledFocus(row, width, color, -1)
}

func DetailBrowserExpandedLinesStyledFocus(row detailBrowserRow, width int, color bool, actionFocus int) []string {
	return detailBrowserExpandedLinesStyledFocusWithLabels(row, width, color, actionFocus, DefaultDetailBrowserLabels(), DefaultDetailBrowserFormatters())
}

func (m DetailBrowserModel) expandedLinesStyledFocus(row detailBrowserRow, width int, color bool, actionFocus int) []string {
	return detailBrowserExpandedLinesStyledFocusWithLabels(row, width, color, actionFocus, m.labels, m.format)
}

func detailBrowserExpandedLinesStyledFocusWithLabels(row detailBrowserRow, width int, color bool, actionFocus int, labels DetailBrowserLabels, format DetailBrowserFormatters) []string {
	if format.SectionHeading == nil {
		format.SectionHeading = DefaultDetailBrowserFormatters().SectionHeading
	}
	lines := []string{}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, format.SectionHeading(labels.DetailsHeading, color))
		lines = append(lines, DetailBrowserDetailLines(row.Detail, width, color)...)
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
			lines = append(lines, format.SectionHeading(labels.EvidenceHeading, color))
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
			lines = append(lines, format.SectionHeading(labels.ActionsHeading, color))
			actionsStarted = true
		}
		lines = append(lines, wrapDetail(detailBrowserActionLineWithFormat(index, action, color, index == actionFocus, format), width)...)
	}
	if len(lines) == 0 {
		lines = append(lines, textui.StyleDim(labels.NoAdditionalDetail, color))
	}
	return lines
}

func DetailBrowserDetailLines(detail string, width int, color bool) []string {
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
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	key = strings.TrimSpace(key)
	if !ok || key == "" || len([]rune(key)) > 32 || strings.Contains(key, "/") || strings.Contains(key, "\t") {
		return false
	}
	if keyLooksLikeURLSchemePrefix(key, value) {
		return false
	}
	if !strings.Contains(key, " ") {
		return true
	}
	switch strings.ToLower(key) {
	case "linked evidence", "update evidence", "security evidence", "backend evidence", "next action", "managed by":
		return true
	case "関連 evidence", "次の操作":
		return true
	default:
		return false
	}
}

func detailBrowserActionLine(index int, action detailBrowserAction, color bool, focused bool) string {
	return detailBrowserActionLineWithFormat(index, action, color, focused, DefaultDetailBrowserFormatters())
}

func detailBrowserActionLineWithFormat(index int, action detailBrowserAction, color bool, focused bool, format DetailBrowserFormatters) string {
	key := fmt.Sprintf("action %d [press %d]:", index+1, index+1)
	if index == 0 {
		key = fmt.Sprintf("action %d [press a or 1]:", index+1)
	}
	if format.LocalizeEvidence == nil {
		format.LocalizeEvidence = DefaultDetailBrowserFormatters().LocalizeEvidence
	}
	label := textui.StyleAction(strings.TrimSpace(action.Label), color)
	prefix := "  "
	if focused {
		prefix = textui.StyleRequested("> ", color)
	}
	line := prefix + textui.StyleKey(key, color) + " " + label
	if strings.TrimSpace(action.Description) != "" {
		line += textui.StyleDim(" - "+compactDetailBrowserActionDescription(format.LocalizeEvidence(action.Description)), color)
	}
	return line
}

func compactDetailBrowserActionDescription(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	for _, delimiter := range []string{"; source:", "; tap:", "; homepage:", "; download:", "; homepage host:", "; download host:", "; キャッシュ:", "; リリース経過:"} {
		if before, _, ok := strings.Cut(value, delimiter); ok {
			value = strings.TrimSpace(before)
		}
	}
	return textui.Truncate(value, 72)
}

func DetailBrowserCollapsedSummary(row detailBrowserRow) string {
	return detailBrowserCollapsedSummaryWithFormat(row, DefaultDetailBrowserFormatters())
}

func (m DetailBrowserModel) collapsedSummary(row detailBrowserRow) string {
	return detailBrowserCollapsedSummaryWithLabels(row, m.labels, m.format)
}

func detailBrowserCollapsedSummaryWithFormat(row detailBrowserRow, format DetailBrowserFormatters) string {
	return detailBrowserCollapsedSummaryWithLabels(row, DefaultDetailBrowserLabels(), format)
}

func detailBrowserCollapsedSummaryWithLabels(row detailBrowserRow, labels DetailBrowserLabels, format DetailBrowserFormatters) string {
	parts := detailBrowserRowBadgesWithLabels(row, labels, format)
	if format.OneLine == nil {
		format.OneLine = DefaultDetailBrowserFormatters().OneLine
	}
	if summary := strings.TrimSpace(format.OneLine(row.Summary)); summary != "" {
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func detailBrowserRowBadges(row detailBrowserRow) []string {
	return detailBrowserRowBadgesWithLabels(row, DefaultDetailBrowserLabels(), DefaultDetailBrowserFormatters())
}

func detailBrowserRowBadgesWithLabels(row detailBrowserRow, labels DetailBrowserLabels, format DetailBrowserFormatters) []string {
	if format.OneLine == nil {
		format.OneLine = DefaultDetailBrowserFormatters().OneLine
	}
	badges := []string{}
	if len(row.Actions) > 0 {
		badges = append(badges, fmt.Sprintf("[%s:%d]", labels.ActionsBadge, len(row.Actions)))
	}
	for _, meta := range row.Metadata {
		key, value, ok := strings.Cut(strings.TrimSpace(meta), ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(format.OneLine(value))
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

func DetailBrowserActionKeyIndex(key string) (int, bool) {
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

func (m DetailBrowserModel) expandedDetailWidth() int {
	if m.Width <= 0 {
		return 0
	}
	return m.Width - 4
}
