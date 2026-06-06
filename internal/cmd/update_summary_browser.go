package cmd

import (
	"bytes"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type updateSummaryLine struct {
	Text            string
	Action          string
	Label           string
	HideInlineBadge bool
	Kind            updateSummaryLineKind
}

type updateSummaryLineKind string

const (
	updateSummaryLineNormal      updateSummaryLineKind = ""
	updateSummaryLineSection     updateSummaryLineKind = "section"
	updateSummaryLineTableHeader updateSummaryLineKind = "table-header"
)

type updateSummaryRoute struct {
	Base     string
	Provider string
	Query    string
}

const updateSummaryRoutePrefix = "summary-route"

type updateSummaryBrowserModel struct {
	Title     string
	Lines     []updateSummaryLine
	State     reviewui.State
	Color     bool
	Height    int
	Width     int
	Help      bool
	actionMap []int
}

func runUpdateSummaryBrowser(title string, report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, state reviewui.State, focusAction string, color bool) (reviewui.State, error) {
	model := newUpdateSummaryBrowserModel(title, report, manualPlan, backendPlan, state, focusAction, color)
	return runActionSummaryBrowserModel(model)
}

func runActionSummaryBrowserModel(model updateSummaryBrowserModel) (reviewui.State, error) {
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return model.State, err
	}
	if result, ok := final.(updateSummaryBrowserModel); ok {
		return result.State, nil
	}
	return model.State, nil
}

func newUpdateSummaryBrowserModel(title string, report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	return newActionSummaryBrowserModel(title, updateSummaryBrowserLines(report, manualPlan, backendPlan, color), state, focusAction, color)
}

func newActionSummaryBrowserModel(title string, lines []updateSummaryLine, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	model := updateSummaryBrowserModel{Title: title, Lines: lines, State: state, Color: color}
	model.refreshActionMap()
	if focusAction != "" {
		model.focusAction(focusAction)
	}
	model.clampSelection()
	return model
}

func updateSummaryBrowserLines(report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, color bool) []updateSummaryLine {
	var styled bytes.Buffer
	var plain bytes.Buffer
	printUpdateBodyTo(&styled, report, color)
	printUpdateBodyTo(&plain, report, false)
	styledLines := strings.Split(strings.TrimRight(styled.String(), "\n"), "\n")
	plainLines := strings.Split(strings.TrimRight(plain.String(), "\n"), "\n")
	out := make([]updateSummaryLine, 0, len(styledLines)+6)
	section := ""
	for index, styledLine := range styledLines {
		plainLine := ""
		if index < len(plainLines) {
			plainLine = plainLines[index]
		}
		trimmed := strings.TrimSpace(plainLine)
		action := ""
		label := ""
		allowTableRoute := true
		kind := updateSummaryLineNormal
		switch trimmed {
		case tr("updates", "更新"):
			section = "updates"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionLogs
			label = tr("open update details", "更新詳細を開く")
		case tr("update outcome", "更新結果"):
			section = "outcome"
			kind = updateSummaryLineSection
			styledLine = trimmed
		case tr("security attention", "セキュリティ注意項目"), tr("top security items", "主なセキュリティ項目"):
			section = "security"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionSecurity
			label = tr("open security details", "security 詳細を開く")
		case tr("top inventory items", "主な inventory 項目"):
			section = "inventory-items"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionInventoryDetails
			label = tr("open inventory details", "inventory 詳細を開く")
		default:
			if strings.HasPrefix(trimmed, "inventory ") {
				section = "inventory"
				kind = updateSummaryLineSection
				styledLine = trimmed
				action = updateHubActionInventoryAll
				label = tr("open installed inventory", "インストール済み一覧を開く")
			}
		}
		if action == "" {
			switch {
			case strings.HasPrefix(trimmed, tr("safety summary:", "安全性サマリー:")):
				section = "security"
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("update summary:", "更新サマリー:")):
				section = "updates"
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("report:", "レポート:")):
				action = updateHubActionFull
				label = tr("open full report", "full report を開く")
				allowTableRoute = false
			}
		}
		if kind == updateSummaryLineNormal && isUpdateSummaryTableHeaderLine(trimmed) {
			kind = updateSummaryLineTableHeader
			styledLine = plainLine
		}
		if action == "" && allowTableRoute && isUpdateSummaryTableDataLine(trimmed) {
			if route, routeLabel, ok := updateSummaryRouteForTableLine(section, trimmed); ok {
				action = route.Encode()
				label = routeLabel
			}
		}
		out = append(out, updateSummaryLine{
			Text:            styledLine,
			Action:          action,
			Label:           label,
			HideInlineBadge: kind == updateSummaryLineSection,
			Kind:            kind,
		})
	}
	reviewLines := updateSummaryReviewActionLines(manualPlan, backendPlan, color)
	if len(reviewLines) > 0 {
		out = append(out, updateSummaryLine{})
		out = append(out, updateSummaryLine{Text: tr("review actions", "確認アクション"), Kind: updateSummaryLineSection})
		out = append(out, updateSummaryLine{Text: updateSummaryReviewHeaderLine(false), Kind: updateSummaryLineTableHeader})
		out = append(out, reviewLines...)
	}
	return out
}

func updateSummaryRouteForTableLine(section string, line string) (updateSummaryRoute, string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return updateSummaryRoute{}, "", false
	}
	switch section {
	case "updates":
		provider := fields[0]
		return updateSummaryRoute{Base: updateHubActionLogs, Provider: provider}, fmt.Sprintf(tr("open %s update details", "%s の更新詳細を開く"), provider), true
	case "outcome":
		if len(fields) < 3 {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[1]
		query := fields[2]
		return updateSummaryRoute{Base: updateHubActionLogs, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s update details", "%s の更新詳細を開く"), provider), true
	case "security":
		providerIndex := 0
		if strings.EqualFold(fields[0], "hold") || strings.EqualFold(fields[0], "review") || strings.EqualFold(fields[0], "allow") || strings.EqualFold(fields[0], "block") {
			providerIndex = 1
		}
		if providerIndex >= len(fields) {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[providerIndex]
		query := ""
		if providerIndex+1 < len(fields) {
			query = fields[providerIndex+1]
		}
		return updateSummaryRoute{Base: updateHubActionSecurity, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s security details", "%s の security 詳細を開く"), provider), true
	case "inventory":
		provider := fields[0]
		return updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: provider}, fmt.Sprintf(tr("open %s inventory", "%s の inventory を開く"), provider), true
	case "inventory-items":
		if len(fields) < 4 {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[1]
		query := fields[3]
		return updateSummaryRoute{Base: updateHubActionInventoryDetails, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s inventory details", "%s の inventory 詳細を開く"), provider), true
	default:
		return updateSummaryRoute{}, "", false
	}
}

func (r updateSummaryRoute) Encode() string {
	return strings.Join([]string{updateSummaryRoutePrefix, r.Base, r.Provider, r.Query}, "\t")
}

func parseUpdateSummaryRoute(value string) (updateSummaryRoute, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 4 || parts[0] != updateSummaryRoutePrefix {
		return updateSummaryRoute{}, false
	}
	return updateSummaryRoute{Base: parts[1], Provider: parts[2], Query: parts[3]}, true
}

func updateSummaryReviewHeaderLine(color bool) string {
	return "  " + strings.Join([]string{
		textui.StyleHeading(textui.PadRight(tr("action", "操作"), 24), color),
		textui.StyleHeading(tr("description", "説明"), color),
	}, " ")
}

func updateSummaryReviewActionLines(manualPlan inventoryPlanReport, backendPlan backendPlanReport, color bool) []updateSummaryLine {
	lines := []updateSummaryLine{}
	if manualPlan.AttentionCount > 0 {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("manual review", "手動アプリ確認"), fmt.Sprintf("%s - %s", manualPlanStatus(manualPlan), updateDashboardManualPlanSummary(manualPlan)), color),
			Action:          updateHubActionManualPlan,
			Label:           tr("open manual review", "手動アプリ確認を開く"),
			HideInlineBadge: true,
		})
	}
	if len(backendPlan.Findings) > 0 {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("backend convergence", "backend 整理"), fmt.Sprintf("%s - %s", backendPlan.Status, updateDashboardBackendSummary(backendPlan)), color),
			Action:          updateHubActionBackends,
			Label:           tr("open backend convergence", "backend 整理を開く"),
			HideInlineBadge: true,
		})
	}
	return lines
}

func updateSummaryReviewRowLine(name string, description string, color bool) string {
	return strings.Join([]string{
		textui.PadRight(name, 24),
		description,
	}, " ")
}

func isUpdateSummaryTableDataLine(line string) bool {
	if line == "" {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToLower(fields[0])
	switch first {
	case "name", "名前", "provider", "decision", "status", "状態", "type", "種別", "missing", "extra":
		return false
	}
	return len(fields) >= 2
}

func isUpdateSummaryTableHeaderLine(line string) bool {
	if line == "" {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "name", "名前", "provider", "decision", "status", "状態", "type", "種別", "missing", "extra", "action", "操作":
		return len(fields) >= 2
	default:
		return false
	}
}

func (m updateSummaryBrowserModel) Init() tea.Cmd {
	return nil
}

func (m updateSummaryBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.Help {
			m.Help = false
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc", "b", "left", "backspace":
			m.State.Action = updevActionExit
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			m.move(-10)
		case "pgdown":
			m.move(10)
		case "home":
			m.State.Selected = 0
			m.ensureSelectedVisible()
		case "end":
			if len(m.actionMap) > 0 {
				m.State.Selected = len(m.actionMap) - 1
				m.ensureSelectedVisible()
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
		m.ensureSelectedVisible()
	}
	return m, nil
}

func (m updateSummaryBrowserModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", textui.StyleHeading(m.Title, m.Color))
	if m.Help {
		fmt.Fprintf(&out, "  %s\n", tr("Up/Down or j/k: move between selectable summary rows", "↑↓ または j/k: 選択可能な summary 行を移動"))
		fmt.Fprintf(&out, "  %s\n", tr("Enter, Space, or a: open the selected detail", "Enter / Space / a: 選択した詳細を開く"))
		fmt.Fprintf(&out, "  %s\n", tr("b, q, Esc, or Ctrl-C: exit", "b / q / Esc / Ctrl-C: 終了"))
		view := tea.NewView(out.String())
		view.AltScreen = true
		return view
	}
	fmt.Fprintf(&out, "%s\n", textui.StyleDim(tr("Up/Down/j/k move, Enter open selected summary, Space/a open, ? help, q exit", "↑↓/j/k 移動、Enter で選択 summary を開く、Space/a も開く、? help、q 終了"), m.Color))
	if hint := m.selectedHint(); hint != "" {
		fmt.Fprintf(&out, "%s\n", hint)
	}
	fmt.Fprintln(&out)
	start, end := m.visibleLineRange()
	selectedLine := m.selectedLineIndex()
	for index := start; index < end; index++ {
		line := m.Lines[index]
		text := m.renderLineText(line)
		prefix := "  "
		if index == selectedLine {
			prefix = textui.StyleRequested("> ", m.Color)
			text = m.selectedLineText(line, text)
		} else if line.Action == "" {
			prefix = ""
		} else {
			prefix = textui.StyleDim("  ", m.Color)
		}
		if m.Width > 0 {
			text = textTruncate(text, m.Width-2)
		}
		fmt.Fprintf(&out, "%s%s\n", prefix, text)
	}
	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}

func (m updateSummaryBrowserModel) renderLineText(line updateSummaryLine) string {
	switch line.Kind {
	case updateSummaryLineSection:
		return browserSectionHeadingText(line.Text, m.Color)
	case updateSummaryLineTableHeader:
		return textui.StyleHeading(line.Text, m.Color)
	default:
		return line.Text
	}
}

func browserSectionHeadingText(text string, color bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return textui.StyleSection(trimmed, color)
}

func (m *updateSummaryBrowserModel) refreshActionMap() {
	m.actionMap = m.actionMap[:0]
	for index, line := range m.Lines {
		if line.Action != "" {
			m.actionMap = append(m.actionMap, index)
		}
	}
}

func (m *updateSummaryBrowserModel) clampSelection() {
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
	m.ensureSelectedVisible()
}

func (m *updateSummaryBrowserModel) move(delta int) {
	if len(m.actionMap) == 0 {
		return
	}
	m.State.Selected += delta
	m.clampSelection()
}

func (m *updateSummaryBrowserModel) focusAction(action string) {
	for actionIndex, lineIndex := range m.actionMap {
		if m.Lines[lineIndex].Action == action {
			m.State.Selected = actionIndex
			return
		}
	}
	for actionIndex, lineIndex := range m.actionMap {
		if route, ok := parseUpdateSummaryRoute(m.Lines[lineIndex].Action); ok && route.Base == action {
			m.State.Selected = actionIndex
			return
		}
	}
}

func (m *updateSummaryBrowserModel) ensureSelectedVisible() {
	selectedLine := m.selectedLineIndex()
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

func (m updateSummaryBrowserModel) visibleLineRange() (int, int) {
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

func (m updateSummaryBrowserModel) visibleBodyCapacity() int {
	if m.Height <= 0 {
		return 28
	}
	capacity := m.Height - 5
	if capacity < 8 {
		return 8
	}
	return capacity
}

func (m updateSummaryBrowserModel) selectedLineIndex() int {
	if len(m.actionMap) == 0 || m.State.Selected < 0 || m.State.Selected >= len(m.actionMap) {
		return -1
	}
	return m.actionMap[m.State.Selected]
}

func (m *updateSummaryBrowserModel) selectAction() bool {
	lineIndex := m.selectedLineIndex()
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

func (m updateSummaryBrowserModel) selectedHint() string {
	lineIndex := m.selectedLineIndex()
	if lineIndex < 0 || lineIndex >= len(m.Lines) {
		return ""
	}
	line := m.Lines[lineIndex]
	label := firstNonEmpty(line.Label, line.Action)
	position := fmt.Sprintf("%d/%d", m.State.Selected+1, len(m.actionMap))
	return fmt.Sprintf("%s %s  %s",
		textui.StyleDim(tr("focused actions:", "選択中の操作:"), m.Color),
		textui.StyleAction("a/1="+label, m.Color),
		textui.StyleCount(position, m.Color),
	)
}

func (m updateSummaryBrowserModel) selectedLineText(line updateSummaryLine, text string) string {
	if line.HideInlineBadge {
		return text
	}
	label := firstNonEmpty(line.Label, line.Action)
	badge := " [" + fmt.Sprintf(tr("Enter: %s", "Enter: %s"), label) + "]"
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

var _ tea.Model = updateSummaryBrowserModel{}
