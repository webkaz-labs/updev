package reviewui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type ActionSummaryLineKind string

const (
	ActionSummaryLineNormal      ActionSummaryLineKind = ""
	ActionSummaryLineSection     ActionSummaryLineKind = "section"
	ActionSummaryLineTableHeader ActionSummaryLineKind = "table-header"
	ActionSummaryLineMeta        ActionSummaryLineKind = "meta"
)

type ActionSummaryLine struct {
	Text            string
	Action          string
	Label           string
	HideInlineBadge bool
	Kind            ActionSummaryLineKind
}

type ActionSummaryLabels struct {
	HelpMove      string
	HelpOpen      string
	HelpExit      string
	Controls      string
	FocusedPrefix string
	EnterFormat   string
}

type ActionSummaryActions struct {
	Exit string
}

type ActionSummaryModel struct {
	Title     string
	Lines     []ActionSummaryLine
	State     State
	Color     bool
	Height    int
	Width     int
	Help      bool
	TopAnchor bool

	Labels       ActionSummaryLabels
	Actions      ActionSummaryActions
	FocusMatcher func(lineAction string, focusAction string) bool
	actionMap    []int
}

type ActionSummaryOptions struct {
	Title        string
	Lines        []ActionSummaryLine
	State        State
	FocusAction  string
	Labels       ActionSummaryLabels
	Actions      ActionSummaryActions
	FocusMatcher func(string, string) bool
	Color        bool
}

func NewActionSummaryModel(options ActionSummaryOptions) ActionSummaryModel {
	model := ActionSummaryModel{
		Title:        options.Title,
		Lines:        options.Lines,
		State:        options.State,
		Color:        options.Color,
		Labels:       fillActionSummaryLabels(options.Labels),
		Actions:      fillActionSummaryActions(options.Actions),
		FocusMatcher: options.FocusMatcher,
	}
	model.refreshActionMap()
	if options.State.Offset == 0 && options.State.Selected == 0 && options.State.Action == "" {
		model.TopAnchor = true
	}
	if options.FocusAction != "" {
		model.FocusAction(options.FocusAction)
	}
	model.ClampSelection()
	return model
}

func (m ActionSummaryModel) Init() tea.Cmd {
	return nil
}

func (m ActionSummaryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.Help {
			m.Help = false
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc", "b", "left", "backspace":
			m.State.Action = m.Actions.Exit
			return m, tea.Quit
		case "up", "k":
			m.Move(-1)
		case "down", "j":
			m.Move(1)
		case "pgup":
			m.Move(-10)
		case "pgdown":
			m.Move(10)
		case "home":
			m.State.Selected = 0
			m.EnsureSelectedVisible()
		case "end":
			if len(m.actionMap) > 0 {
				m.State.Selected = len(m.actionMap) - 1
				m.EnsureSelectedVisible()
			}
		case "enter", "a", " ":
			if m.selectAction() {
				return m, tea.Quit
			}
		case "?":
			m.Help = true
		}
	case tea.WindowSizeMsg:
		m.Height = msg.Height
		m.Width = msg.Width
		m.EnsureSelectedVisible()
	}
	return m, nil
}

func (m ActionSummaryModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	if m.Help {
		fmt.Fprintf(&out, "  %s\n", m.Labels.HelpMove)
		fmt.Fprintf(&out, "  %s\n", m.Labels.HelpOpen)
		fmt.Fprintf(&out, "  %s\n", m.Labels.HelpExit)
		view := tea.NewView(out.String())
		view.AltScreen = true
		return view
	}
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(m.Labels.Controls, m.Color))
	if hint := m.selectedHint(); hint != "" {
		fmt.Fprintf(&out, "%s\n", hint)
	}
	fmt.Fprintln(&out)
	start, end := m.visibleLineRange()
	selectedLine := m.SelectedLineIndex()
	for index := start; index < end; index++ {
		line := m.Lines[index]
		text := m.renderLineText(line)
		prefix := "  "
		if index == selectedLine {
			prefix = textui.StyleRequested("> ", m.Color)
			text = m.selectedLineText(line, text)
		} else if line.Action == "" {
			prefix = ""
		} else if line.Kind == ActionSummaryLineMeta {
			prefix = ""
		} else {
			prefix = textui.StyleDim("  ", m.Color)
		}
		if m.Width > 0 {
			text = textui.Truncate(text, m.Width-2)
		}
		fmt.Fprintf(&out, "%s%s\n", prefix, text)
	}
	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}

func (m ActionSummaryModel) renderLineText(line ActionSummaryLine) string {
	switch line.Kind {
	case ActionSummaryLineSection:
		return actionSummarySectionHeadingText(line.Text, m.Color)
	case ActionSummaryLineTableHeader:
		return textui.StyleHeading(line.Text, m.Color)
	default:
		return line.Text
	}
}

func actionSummarySectionHeadingText(text string, color bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return textui.StyleSection(trimmed, color)
}

func (m *ActionSummaryModel) refreshActionMap() {
	m.actionMap = m.actionMap[:0]
	for index, line := range m.Lines {
		if line.Action != "" {
			m.actionMap = append(m.actionMap, index)
		}
	}
}

func (m *ActionSummaryModel) ClampSelection() {
	if len(m.actionMap) == 0 {
		m.State.Selected = 0
		m.State.Offset = 0
		return
	}
	if m.State.Selected < 0 {
		m.State.Selected = 0
	}
	if m.State.Selected >= len(m.actionMap) {
		m.State.Selected = len(m.actionMap) - 1
	}
	m.EnsureSelectedVisible()
}

func (m *ActionSummaryModel) Move(delta int) {
	if len(m.actionMap) == 0 {
		return
	}
	m.TopAnchor = false
	m.State.Selected += delta
	m.ClampSelection()
}

func (m *ActionSummaryModel) FocusAction(action string) {
	for actionIndex, lineIndex := range m.actionMap {
		if m.Lines[lineIndex].Action == action {
			m.State.Selected = actionIndex
			return
		}
	}
	if m.FocusMatcher == nil {
		return
	}
	for actionIndex, lineIndex := range m.actionMap {
		if m.FocusMatcher(m.Lines[lineIndex].Action, action) {
			m.State.Selected = actionIndex
			return
		}
	}
}

func (m *ActionSummaryModel) EnsureSelectedVisible() {
	if m.TopAnchor {
		m.State.Offset = 0
		return
	}
	selectedLine := m.SelectedLineIndex()
	if selectedLine < 0 {
		m.State.Offset = 0
		return
	}
	capacity := m.visibleBodyCapacity()
	if selectedLine < m.State.Offset {
		m.State.Offset = selectedLine
	}
	if selectedLine >= m.State.Offset+capacity {
		m.State.Offset = selectedLine - capacity + 1
	}
	if m.State.Offset < 0 {
		m.State.Offset = 0
	}
}

func (m ActionSummaryModel) visibleLineRange() (int, int) {
	start := m.State.Offset
	if start < 0 {
		start = 0
	}
	if start > len(m.Lines) {
		start = len(m.Lines)
	}
	end := start + m.visibleBodyCapacity()
	if end > len(m.Lines) {
		end = len(m.Lines)
	}
	return start, end
}

func (m ActionSummaryModel) visibleBodyCapacity() int {
	if m.Height <= 0 {
		return 28
	}
	capacity := m.Height - 5
	if capacity < 8 {
		return 8
	}
	return capacity
}

func (m ActionSummaryModel) SelectedLineIndex() int {
	if len(m.actionMap) == 0 || m.State.Selected < 0 || m.State.Selected >= len(m.actionMap) {
		return -1
	}
	return m.actionMap[m.State.Selected]
}

func (m *ActionSummaryModel) selectAction() bool {
	lineIndex := m.SelectedLineIndex()
	if lineIndex < 0 || lineIndex >= len(m.Lines) {
		return false
	}
	action := m.Lines[lineIndex].Action
	if action == "" {
		return false
	}
	m.State.Action = action
	return true
}

func (m ActionSummaryModel) selectedHint() string {
	lineIndex := m.SelectedLineIndex()
	if lineIndex < 0 || lineIndex >= len(m.Lines) {
		return ""
	}
	line := m.Lines[lineIndex]
	label := firstNonEmpty(line.Label, line.Action)
	position := fmt.Sprintf("%d/%d", m.State.Selected+1, len(m.actionMap))
	return fmt.Sprintf(
		"%s %s  %s",
		textui.StyleDim(m.Labels.FocusedPrefix, m.Color),
		textui.StyleAction("a/1="+label, m.Color),
		textui.StyleCount(position, m.Color),
	)
}

func (m ActionSummaryModel) selectedLineText(line ActionSummaryLine, text string) string {
	if line.HideInlineBadge {
		return text
	}
	label := firstNonEmpty(line.Label, line.Action)
	badge := " [" + fmt.Sprintf(m.Labels.EnterFormat, label) + "]"
	badge = textui.StyleLabel(badge, m.Color)
	if m.Width <= 0 {
		return text + badge
	}
	available := m.Width - 2
	if available <= textui.DisplayWidth(badge)+8 {
		return text
	}
	textWidth := available - textui.DisplayWidth(badge)
	return textui.Truncate(text, textWidth) + badge
}

func fillActionSummaryLabels(labels ActionSummaryLabels) ActionSummaryLabels {
	if labels.HelpMove == "" {
		labels.HelpMove = "Up/Down or j/k: move between selectable summary rows"
	}
	if labels.HelpOpen == "" {
		labels.HelpOpen = "Enter, Space, or a: open the selected detail"
	}
	if labels.HelpExit == "" {
		labels.HelpExit = "b, q, Esc, or Ctrl-C: exit"
	}
	if labels.Controls == "" {
		labels.Controls = "Up/Down/j/k move, Enter open selected summary, Space/a open, ? help, q exit"
	}
	if labels.FocusedPrefix == "" {
		labels.FocusedPrefix = "focused actions:"
	}
	if labels.EnterFormat == "" {
		labels.EnterFormat = "Enter: %s"
	}
	return labels
}

func fillActionSummaryActions(actions ActionSummaryActions) ActionSummaryActions {
	if actions.Exit == "" {
		actions.Exit = "exit"
	}
	return actions
}

var _ tea.Model = ActionSummaryModel{}
