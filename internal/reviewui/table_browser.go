package reviewui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type BrowserActions struct {
	Back     string
	Home     string
	Exit     string
	Next     string
	Previous string
}

type TableBrowserLabels struct {
	Labels
	ControlsHelp       string
	NoRows             string
	FilterLabel        string
	HelpLines          []string
	PositionZeroFilter func(total int, query string) string
	PositionZero       string
	PositionFilter     func(selected int, count int, offset int, query string) string
	Position           func(selected int, count int, offset int) string
}

type TableBrowserModel struct {
	Title               string
	Sections            []Section
	FilteredSections    []Section
	FilteredQuery       string
	State               State
	Color               bool
	Height              int
	Width               int
	Filtering           bool
	FilterInput         string
	Help                bool
	MouseMode           MouseMode
	pendingMouseRelease int
	actionFocus         int
	actions             BrowserActions
	labels              TableBrowserLabels
}

type MouseMode string

const (
	MouseOff   MouseMode = "off"
	MouseWheel MouseMode = "wheel"
	MouseClick MouseMode = "click"
)

type tableMouseMsg struct {
	Index   int
	Release bool
}

type tableWheelMsg struct {
	Delta int
}

func RunTableBrowserWithState(title string, sections []Section, state State, labels TableBrowserLabels, actions BrowserActions, color bool) (State, error) {
	model := NewTableBrowserModel(title, sections, state, labels, actions, color)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return model.State, err
	}
	if result, ok := final.(TableBrowserModel); ok {
		return result.State, nil
	}
	return model.State, nil
}

func NewTableBrowserModel(title string, sections []Section, state State, labels TableBrowserLabels, actions BrowserActions, color bool) TableBrowserModel {
	if state.Expanded == nil {
		state.Expanded = map[int]bool{}
	}
	state.Action = ""
	count := RowCount(sections)
	if count == 0 {
		state.Selected = 0
	} else if state.Selected < 0 {
		state.Selected = 0
	} else if state.Selected >= count {
		state.Selected = count - 1
	}
	model := TableBrowserModel{Title: title, Sections: sections, State: state, Color: color, FilterInput: state.Query, MouseMode: MouseOff, pendingMouseRelease: -1, actionFocus: stateActionFocusIndex(state.ActionFocus), actions: fillActions(actions), labels: fillTableBrowserLabels(labels)}
	model.refreshFilteredSections()
	model.clampSelection()
	model.clampActionFocus()
	return model
}

func (m TableBrowserModel) Init() tea.Cmd {
	return nil
}

func (m TableBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "tab":
			if m.actions.Next != "" {
				m.State.Action = m.actions.Next
				return m, tea.Quit
			}
		case "shift+tab", "backtab", "btab":
			if m.actions.Previous != "" {
				m.State.Action = m.actions.Previous
				return m, tea.Quit
			}
		case "up", "k":
			m.moveFocus(-1)
		case "down", "j":
			m.moveFocus(1)
		case "pgup":
			m.move(-m.visibleRowCapacity())
		case "pgdown":
			m.move(m.visibleRowCapacity())
		case "home":
			m.State.Selected = 0
			m.setActionFocus(-1)
			m.ensureSelectedVisible()
		case "end":
			if count := RowCount(m.Sections); count > 0 {
				m.State.Selected = count - 1
				m.setActionFocus(-1)
				m.ensureSelectedVisible()
			}
		case "enter":
			if m.selectFocusedAction() {
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
			m.MouseMode = nextMouseMode(m.MouseMode)
			m.pendingMouseRelease = -1
		case "x":
			m.clearFilter()
		default:
			if index, ok := tableBrowserActionKeyIndex(msg.String()); ok {
				if m.selectRowAction(index) {
					return m, tea.Quit
				}
			}
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height
		m.Width = msg.Width
		m.ensureSelectedVisible()
	case tableMouseMsg:
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
			m.setActionFocus(-1)
			m.ensureSelectedVisible()
			m.pendingMouseRelease = msg.Index
		}
	case tableWheelMsg:
		m.scroll(msg.Delta)
	}
	return m, nil
}

func (m *TableBrowserModel) clearFilter() bool {
	if m.State.Query == "" && m.FilterInput == "" {
		return false
	}
	m.State.Query = ""
	m.FilterInput = ""
	m.State.Selected = 0
	m.State.Offset = 0
	m.setActionFocus(-1)
	m.refreshFilteredSections()
	m.clampSelection()
	return true
}

func (m *TableBrowserModel) updateFilterInput(msg tea.KeyPressMsg) {
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

func (m *TableBrowserModel) applyFilterInput() {
	m.State.Query = strings.TrimSpace(m.FilterInput)
	m.State.Selected = 0
	m.State.Offset = 0
	m.setActionFocus(-1)
	m.refreshFilteredSections()
	m.clampSelection()
}

func (m *TableBrowserModel) handleMouseToggle(index int) {
	m.State.Selected = index
	m.ensureSelectedVisible()
	m.toggleSelected()
}

func (m *TableBrowserModel) Move(delta int) {
	m.move(delta)
}

func (m *TableBrowserModel) move(delta int) {
	count := m.filteredRowCount()
	if count == 0 {
		m.State.Selected = 0
		return
	}
	m.State.Selected += delta
	m.setActionFocus(-1)
	if m.State.Selected < 0 {
		m.State.Selected = 0
	}
	if m.State.Selected >= count {
		m.State.Selected = count - 1
	}
	m.ensureSelectedVisible()
}

func (m *TableBrowserModel) moveFocus(delta int) {
	if delta == 0 {
		return
	}
	row, ok := m.selectedRow()
	actionCount := len(row.Actions)
	if ok && m.State.Expanded[m.State.Selected] && actionCount > 0 {
		if m.actionFocus < 0 && delta < 0 {
			m.move(delta)
			return
		}
		next := m.actionFocus + delta
		if m.actionFocus < 0 && delta > 0 {
			next = 0
		}
		if next >= 0 && next < actionCount {
			m.setActionFocus(next)
			m.ensureSelectedVisible()
			return
		}
		if next < 0 {
			m.setActionFocus(-1)
			m.ensureSelectedVisible()
			return
		}
		m.setActionFocus(-1)
	}
	m.move(delta)
}

func (m *TableBrowserModel) scroll(delta int) {
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

func (m *TableBrowserModel) toggleSelected() {
	if m.filteredRowCount() == 0 {
		return
	}
	if m.State.Expanded == nil {
		m.State.Expanded = map[int]bool{}
	}
	m.State.Expanded[m.State.Selected] = !m.State.Expanded[m.State.Selected]
	if !m.State.Expanded[m.State.Selected] {
		m.setActionFocus(-1)
		m.ensureSelectedVisible()
		return
	}
	row, ok := m.selectedRow()
	if ok && len(row.Actions) > 0 {
		m.setActionFocus(0)
	} else {
		m.setActionFocus(-1)
	}
	m.ensureSelectedVisible()
}

func (m *TableBrowserModel) ToggleSelected() {
	m.toggleSelected()
}

func (m *TableBrowserModel) selectRowAction(index int) bool {
	row, ok := m.selectedRow()
	if !ok || index < 0 || index >= len(row.Actions) || row.Actions[index].Value == "" {
		return false
	}
	m.setActionFocus(index)
	m.State.Action = row.Actions[index].Value
	return true
}

func (m *TableBrowserModel) selectFocusedAction() bool {
	row, ok := m.selectedRow()
	if !ok || !m.State.Expanded[m.State.Selected] || m.actionFocus < 0 || m.actionFocus >= len(row.Actions) {
		return false
	}
	return m.selectRowAction(m.actionFocus)
}

func (m *TableBrowserModel) setActionFocus(index int) {
	m.actionFocus = index
	if index < 0 {
		m.State.ActionFocus = 0
		return
	}
	m.State.ActionFocus = index + 1
}

func (m *TableBrowserModel) clampActionFocus() {
	row, ok := m.selectedRow()
	if !ok || !m.State.Expanded[m.State.Selected] {
		m.setActionFocus(-1)
		return
	}
	if m.actionFocus >= len(row.Actions) {
		m.setActionFocus(-1)
	}
}

func (m TableBrowserModel) selectedRow() (Row, bool) {
	target := m.State.Selected
	if target < 0 {
		return Row{}, false
	}
	index := 0
	for _, section := range m.filteredSections() {
		for _, row := range section.Rows {
			if index == target {
				return row, true
			}
			index++
		}
	}
	return Row{}, false
}

func tableBrowserActionKeyIndex(value string) (int, bool) {
	if len(value) != 1 || value[0] < '1' || value[0] > '9' {
		return 0, false
	}
	return int(value[0] - '1'), true
}

func (m TableBrowserModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(m.labels.ControlsHelp, m.Color))
	line := 4
	if m.Filtering {
		fmt.Fprintf(&out, "%s %s\n", textui.StyleLabel(m.labels.FilterLabel, m.Color), m.FilterInput)
		line = 5
	}
	fmt.Fprintf(&out, "%s %s\n", textui.StyleDim(m.positionText(), m.Color), textui.StyleDim("mouse="+string(m.MouseMode), m.Color))
	if hint := m.selectedActionHint(); hint != "" {
		fmt.Fprintf(&out, "%s\n", textui.StyleDim(hint, m.Color))
	} else {
		fmt.Fprintln(&out)
	}
	line++
	fmt.Fprintln(&out)
	if m.Help {
		for _, line := range m.labels.HelpLines {
			fmt.Fprintf(&out, "  %s\n", line)
		}
		view := tea.NewView(out.String())
		view.AltScreen = true
		return view
	}
	sections := m.filteredSections()
	rowLines := map[int]int{}
	rowIndex := 0
	renderedLines := 0
	maxBodyLines := m.visibleBodyLines()
	rowCapacity := m.visibleRowCapacity()
visible:
	for sectionIndex, section := range sections {
		sectionStart := rowIndex
		sectionEnd := sectionStart + len(section.Rows)
		if sectionEnd <= m.State.Offset {
			rowIndex = sectionEnd
			continue
		}
		if sectionIndex > 0 && renderedLines > 0 {
			fmt.Fprintln(&out)
			line++
			renderedLines++
		}
		fmt.Fprintf(&out, "%s %s\n", textui.StyleSection(section.Title, m.Color), textui.StyleCount(fmt.Sprintf("(%d)", len(section.Rows)), m.Color))
		line++
		renderedLines++
		includeWanted := hasWanted(section)
		columns := columns(includeWanted, m.labels.Labels)
		windowStart := 0
		if m.State.Offset > sectionStart {
			windowStart = m.State.Offset - sectionStart
		}
		windowEnd := windowStart + rowCapacity + 2
		if windowEnd > len(section.Rows) {
			windowEnd = len(section.Rows)
		}
		rows := styledRows(section.Rows[windowStart:windowEnd], includeWanted, m.Color)
		widths := textui.ColumnWidths(columns, rows)
		fmt.Fprintf(&out, "  %s\n", header(columns, widths, m.Color))
		line++
		renderedLines++
		rowIndex = sectionStart + windowStart
		for rowOffset := windowStart; rowOffset < len(section.Rows); rowOffset++ {
			if rowIndex < m.State.Offset {
				rowIndex++
				continue
			}
			if renderedLines >= maxBodyLines {
				break visible
			}
			rowLines[rowIndex] = line
			styledRow := StyledRow(section.Rows[rowOffset], includeWanted, m.Color)
			prefix := "  "
			if rowIndex == m.State.Selected {
				prefix = "> "
			}
			fmt.Fprintf(&out, "%s%s\n", prefix, tableRow(styledRow, widths))
			line++
			renderedLines++
			if m.State.Expanded[rowIndex] {
				actionFocus := -1
				if rowIndex == m.State.Selected {
					actionFocus = m.actionFocus
				}
				for _, expandedLine := range expandedLinesWithWidthStyledFocus(section.Rows[rowOffset], m.labels.Labels, m.expandedDetailWidth(), m.Color, actionFocus) {
					if renderedLines >= maxBodyLines {
						break
					}
					fmt.Fprintf(&out, "    %s\n", expandedLine)
					line++
					renderedLines++
				}
			}
			rowIndex++
		}
		rowIndex = sectionEnd
	}
	if RowCount(sections) == 0 {
		fmt.Fprintf(&out, "  %s\n", textui.StyleDim(m.labels.NoRows, m.Color))
	}
	view := tea.NewView(out.String())
	view.AltScreen = true
	if m.MouseMode == MouseOff {
		view.MouseMode = tea.MouseModeNone
		return view
	}
	view.MouseMode = tea.MouseModeCellMotion
	view.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		release := false
		switch msg.(type) {
		case tea.MouseClickMsg:
			if m.MouseMode != MouseClick {
				return nil
			}
		case tea.MouseReleaseMsg:
			if m.MouseMode != MouseClick {
				return nil
			}
			release = true
		case tea.MouseWheelMsg:
			mouse := msg.Mouse()
			switch mouse.Button {
			case tea.MouseWheelUp:
				return func() tea.Msg { return tableWheelMsg{Delta: -3} }
			case tea.MouseWheelDown:
				return func() tea.Msg { return tableWheelMsg{Delta: 3} }
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
				return func() tea.Msg { return tableMouseMsg{Index: index, Release: release} }
			}
		}
		return nil
	}
	return view
}

func (m TableBrowserModel) selectedActionHint() string {
	row, ok := m.selectedRow()
	if !ok || len(row.Actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(row.Actions))
	for index, action := range row.Actions {
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
	if len(row.Actions) > len(parts) {
		parts = append(parts, fmt.Sprintf("+%d", len(row.Actions)-len(parts)))
	}
	if m.State.Expanded[m.State.Selected] {
		return "expanded actions: ↑↓ select, Enter run; " + strings.Join(parts, ", ")
	}
	return "focused actions: " + strings.Join(parts, ", ")
}

func (m *TableBrowserModel) ensureSelectedVisible() {
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
	sections := m.filteredSections()
	for attempts := 0; attempts <= m.filteredRowCount(); attempts++ {
		if tableRowBlockVisibleWithWidth(tableRowBlockOptions{
			Sections:     sections,
			Offset:       m.State.Offset,
			MaxBodyLines: m.visibleBodyLines(),
			Expanded:     m.State.Expanded,
			Width:        m.expandedDetailWidth(),
			Selected:     m.State.Selected,
			Labels:       m.labels.Labels,
		}) {
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

type tableRowBlockOptions struct {
	Sections     []Section
	Offset       int
	MaxBodyLines int
	Expanded     map[int]bool
	Width        int
	Selected     int
	Labels       Labels
}

func tableRowBlockVisibleWithWidth(options tableRowBlockOptions) bool {
	if options.MaxBodyLines <= 0 || options.Selected < 0 {
		return false
	}
	renderedLines := 0
	rowIndex := 0
	for sectionIndex, section := range options.Sections {
		sectionStart := rowIndex
		sectionEnd := sectionStart + len(section.Rows)
		if sectionEnd <= options.Offset {
			rowIndex = sectionEnd
			continue
		}
		if sectionIndex > 0 && renderedLines > 0 {
			renderedLines++
			if renderedLines >= options.MaxBodyLines {
				return false
			}
		}
		renderedLines += 2
		if renderedLines >= options.MaxBodyLines {
			return false
		}
		windowStart := 0
		if options.Offset > sectionStart {
			windowStart = options.Offset - sectionStart
		}
		rowIndex = sectionStart + windowStart
		for rowOffset := windowStart; rowOffset < len(section.Rows); rowOffset++ {
			if rowIndex < options.Offset {
				rowIndex++
				continue
			}
			if renderedLines >= options.MaxBodyLines {
				return false
			}
			rowLines := 1
			if options.Expanded[rowIndex] {
				rowLines += len(expandedLinesWithWidth(section.Rows[rowOffset], options.Labels, options.Width))
			}
			if rowIndex == options.Selected {
				return renderedLines+rowLines <= options.MaxBodyLines
			}
			renderedLines += rowLines
			rowIndex++
		}
		rowIndex = sectionEnd
	}
	return false
}

func (m TableBrowserModel) expandedDetailWidth() int {
	if m.Width <= 0 {
		return 0
	}
	return m.Width - 4
}

func (m TableBrowserModel) visibleRowCapacity() int {
	if m.Height <= 0 {
		return 18
	}
	capacity := m.visibleBodyLines() - 4
	if capacity < 4 {
		return 4
	}
	return capacity
}

func (m TableBrowserModel) visibleBodyLines() int {
	if m.Height <= 0 {
		return 22
	}
	lines := m.Height - m.headerLineCount()
	if lines < 6 {
		return 6
	}
	return lines
}

func (m TableBrowserModel) headerLineCount() int {
	lines := 5
	if m.Filtering {
		lines++
	}
	return lines
}

func (m TableBrowserModel) VisibleBodyLines() int {
	return m.visibleBodyLines()
}

func (m TableBrowserModel) positionText() string {
	count := m.filteredRowCount()
	if count == 0 {
		if m.State.Query != "" {
			return m.labels.PositionZeroFilter(RowCount(m.Sections), m.State.Query)
		}
		return m.labels.PositionZero
	}
	if m.State.Query != "" {
		return m.labels.PositionFilter(m.State.Selected+1, count, m.State.Offset+1, m.State.Query)
	}
	return m.labels.Position(m.State.Selected+1, count, m.State.Offset+1)
}

func (m *TableBrowserModel) clampSelection() {
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
	m.clampActionFocus()
	m.ensureSelectedVisible()
}

func (m TableBrowserModel) filteredRowCount() int {
	return RowCount(m.filteredSections())
}

func (m TableBrowserModel) filteredSections() []Section {
	if m.FilteredQuery == strings.TrimSpace(strings.ToLower(m.State.Query)) {
		return m.FilteredSections
	}
	return filteredSections(m.Sections, m.State.Query)
}

func (m TableBrowserModel) VisibleSections() []Section {
	return m.filteredSections()
}

func (m *TableBrowserModel) refreshFilteredSections() {
	m.FilteredQuery = strings.TrimSpace(strings.ToLower(m.State.Query))
	m.FilteredSections = filteredSections(m.Sections, m.State.Query)
}

func nextMouseMode(mode MouseMode) MouseMode {
	switch mode {
	case MouseOff:
		return MouseWheel
	case MouseWheel:
		return MouseClick
	default:
		return MouseOff
	}
}

func fillActions(actions BrowserActions) BrowserActions {
	if actions.Back == "" {
		actions.Back = "back"
	}
	if actions.Home == "" {
		actions.Home = "home"
	}
	if actions.Exit == "" {
		actions.Exit = "exit"
	}
	return actions
}

func fillTableBrowserLabels(labels TableBrowserLabels) TableBrowserLabels {
	labels.Labels = fillLabels(labels.Labels)
	if labels.ControlsHelp == "" {
		labels.ControlsHelp = "Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b/Backspace Back, q Exit"
	}
	if labels.NoRows == "" {
		labels.NoRows = "no matching rows"
	}
	if labels.FilterLabel == "" {
		labels.FilterLabel = "filter:"
	}
	if len(labels.HelpLines) == 0 {
		labels.HelpLines = []string{
			"Up/Down or j/k: move the focused row",
			"PgUp/PgDn or mouse wheel: scroll by a page",
			"Enter or Space: expand/collapse details",
			"/: filter within the current view",
			"m: cycle mouse off/wheel/click",
			"x: clear the current in-view filter",
			"b, Left, or Backspace: go back",
			"h: return to the top hub",
			"q, Esc, or Ctrl-C: exit the selector",
		}
	}
	if labels.PositionZeroFilter == nil {
		labels.PositionZeroFilter = func(total int, query string) string {
			return fmt.Sprintf("0/%d rows, filter=%q", total, query)
		}
	}
	if labels.PositionZero == "" {
		labels.PositionZero = "0 rows"
	}
	if labels.PositionFilter == nil {
		labels.PositionFilter = func(selected int, count int, offset int, query string) string {
			return fmt.Sprintf("row %d/%d, offset %d, filter=%q", selected, count, offset, query)
		}
	}
	if labels.Position == nil {
		labels.Position = func(selected int, count int, offset int) string {
			return fmt.Sprintf("row %d/%d, offset %d", selected, count, offset)
		}
	}
	return labels
}
