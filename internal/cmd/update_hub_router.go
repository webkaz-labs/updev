package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type updateHubRouterResult struct {
	Action       string
	ManualPlan   inventoryPlanReport
	ManualReady  bool
	BackendPlan  backendPlanReport
	BackendReady bool
}

type updateHubPlanBuilders struct {
	Manual  func(context.Context, string) inventoryPlanReport
	Backend func(context.Context, string) backendPlanReport
}

type updateHubManualPlanMsg struct {
	Report inventoryPlanReport
}

type updateHubBackendPlanMsg struct {
	Report backendPlanReport
}

type updateHubFilterAction struct {
	Section string
	Facet   string
	Value   string
}

type updateHubQueryAction struct {
	Section string
}

type updateHubRouterScreen string

const (
	updateHubRouterDashboard updateHubRouterScreen = "dashboard"
	updateHubRouterDetail    updateHubRouterScreen = "detail"
	updateHubRouterTable     updateHubRouterScreen = "table"
	updateHubRouterInput     updateHubRouterScreen = "input"
	updateHubRouterConfirm   updateHubRouterScreen = "confirm"
)

type updateHubRouterModel struct {
	ctx            context.Context
	planBuilders   updateHubPlanBuilders
	report         updateReport
	manualPlan     inventoryPlanReport
	manualLoading  bool
	backendPlan    backendPlanReport
	backendLoading bool
	defaultAction  string
	detailStates   map[string]detailBrowserState
	color          bool
	width          int
	height         int

	screen              updateHubRouterScreen
	stateKey            string
	returnAction        string
	finalAction         string
	pendingAction       string
	pendingReason       string
	pendingExpires      string
	pendingReturnAction string

	dashboard updateSummaryBrowserModel
	detail    detailBrowserModel
	table     toolTableBrowserModel
	input     textInputBrowserModel
	confirm   confirmBrowserModel
}

func runUpdateHubRouter(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) (updateHubRouterResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newUpdateHubRouterModelWithContext(ctx, defaultUpdateHubPlanBuilders(), report, manualPlan, manualLoading, backendPlan, backendLoading, preferredAction, defaultAction, color)
	final, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil {
		return updateHubRouterResult{}, err
	}
	if result, ok := final.(updateHubRouterModel); ok {
		return updateHubRouterResult{
			Action:       result.finalAction,
			ManualPlan:   result.manualPlan,
			ManualReady:  !result.manualLoading,
			BackendPlan:  result.backendPlan,
			BackendReady: !result.backendLoading,
		}, nil
	}
	return updateHubRouterResult{}, nil
}

func newUpdateHubRouterModel(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) updateHubRouterModel {
	return newUpdateHubRouterModelWithContext(context.Background(), defaultUpdateHubPlanBuilders(), report, manualPlan, manualLoading, backendPlan, backendLoading, preferredAction, defaultAction, color)
}

func newUpdateHubRouterModelWithContext(ctx context.Context, builders updateHubPlanBuilders, report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) updateHubRouterModel {
	if ctx == nil {
		ctx = context.Background()
	}
	defaultBuilders := defaultUpdateHubPlanBuilders()
	if builders.Manual == nil {
		builders.Manual = defaultBuilders.Manual
	}
	if builders.Backend == nil {
		builders.Backend = defaultBuilders.Backend
	}
	model := updateHubRouterModel{
		ctx:            ctx,
		planBuilders:   builders,
		report:         report,
		manualPlan:     manualPlan,
		manualLoading:  manualLoading,
		backendPlan:    backendPlan,
		backendLoading: backendLoading,
		defaultAction:  defaultAction,
		detailStates:   map[string]detailBrowserState{},
		color:          color,
	}
	action := initialUpdateHubAction(preferredAction, defaultAction)
	if action == "" {
		action = updateHubActionDashboard
	}
	model.showAction(action, updateHubActionDashboard)
	return model
}

func defaultUpdateHubPlanBuilders() updateHubPlanBuilders {
	return updateHubPlanBuilders{
		Manual:  buildUpdateHubManualPlanWithContext,
		Backend: buildUpdateHubBackendPlanWithContext,
	}
}

func buildUpdateHubManualPlanWithContext(ctx context.Context, root string) inventoryPlanReport {
	select {
	case <-ctx.Done():
		return canceledUpdateHubManualPlan(root)
	default:
	}
	return buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: manualProviderName})
}

func buildUpdateHubBackendPlanWithContext(ctx context.Context, root string) backendPlanReport {
	select {
	case <-ctx.Done():
		return canceledUpdateHubBackendPlan(root)
	default:
	}
	return buildBackendPlanReport(ctx, backendOptions{command: "plan", root: root})
}

func canceledUpdateHubManualPlan(root string) inventoryPlanReport {
	return inventoryPlanReport{
		SchemaVersion:  1,
		Status:         plan.StatusHeld,
		Root:           root,
		Provider:       manualProviderName,
		ActionCounts:   map[string]int{},
		AttentionCount: 0,
		NextSteps: []string{
			tr("manual review loading was canceled before completion", "手動アプリ確認の準備は完了前にキャンセルされました"),
		},
	}
}

func canceledUpdateHubBackendPlan(root string) backendPlanReport {
	return backendPlanReport{
		SchemaVersion: backendPlanReportSchemaVersion,
		Status:        plan.StatusHeld,
		Command:       "plan",
		Root:          root,
		Warnings: []string{
			tr("backend evidence loading was canceled before completion", "backend evidence の準備は完了前にキャンセルされました"),
		},
	}
}

func (m updateHubRouterModel) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.manualLoading {
		root := m.report.Root
		ctx := m.ctx
		buildManual := m.planBuilders.Manual
		cmds = append(cmds, func() tea.Msg {
			return updateHubManualPlanMsg{Report: buildManual(ctx, root)}
		})
	}
	if m.backendLoading {
		root := m.report.Root
		ctx := m.ctx
		buildBackend := m.planBuilders.Backend
		cmds = append(cmds, func() tea.Msg {
			return updateHubBackendPlanMsg{Report: buildBackend(ctx, root)}
		})
	}
	return tea.Batch(cmds...)
}

func (m updateHubRouterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateHubManualPlanMsg:
		m.manualPlan = msg.Report
		m.manualLoading = false
		m.refreshCurrentScreen()
		return m, nil
	case updateHubBackendPlanMsg:
		m.backendPlan = msg.Report
		m.backendLoading = false
		m.refreshCurrentScreen()
		return m, nil
	}
	switch m.screen {
	case updateHubRouterDashboard:
		updated, _ := m.dashboard.Update(msg)
		if dashboard, ok := updated.(updateSummaryBrowserModel); ok {
			action := dashboard.State.Action
			dashboard.State.Action = ""
			m.dashboard = dashboard
			m.detailStates[m.stateKey] = dashboard.State
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterDetail:
		updated, _ := m.detail.Update(msg)
		if detail, ok := updated.(detailBrowserModel); ok {
			action := detail.State.Action
			detail.State.Action = ""
			m.detail = detail
			m.detailStates[m.stateKey] = detail.State
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterTable:
		updated, _ := m.table.Update(msg)
		if table, ok := updated.(toolTableBrowserModel); ok {
			action := table.State.Action
			table.State.Action = ""
			m.table = table
			m.detailStates[m.stateKey] = table.State
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterInput:
		updated, _ := m.input.Update(msg)
		if input, ok := updated.(textInputBrowserModel); ok {
			m.input = input
			if input.Action != "" {
				return m.handleInputAction(input)
			}
		}
	case updateHubRouterConfirm:
		updated, _ := m.confirm.Update(msg)
		if confirm, ok := updated.(confirmBrowserModel); ok {
			m.confirm = confirm
			if confirm.Action != "" {
				return m.handleConfirmAction(confirm)
			}
		}
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}
	return m, nil
}

func (m *updateHubRouterModel) refreshCurrentScreen() {
	if strings.HasPrefix(m.stateKey, "route:") {
		return
	}
	if route, ok := parseUpdateSummaryRouteStateKey(m.stateKey); ok {
		m.showUpdateSummaryRoute(route)
		return
	}
	switch m.stateKey {
	case "dashboard":
		m.showDashboard(updateHubActionDashboard)
	case "inventory-all":
		m.showAction(updateHubActionInventoryAll, m.returnAction)
	case "inventory-details":
		m.showAction(updateHubActionInventoryDetails, m.returnAction)
	case listHubActionManual:
		m.showAction(listHubActionManual, m.returnAction)
	case "manual-plan":
		m.showAction(updateHubActionManualPlan, m.returnAction)
	case "backends":
		m.showAction(updateHubActionBackends, m.returnAction)
	}
}

func (m updateHubRouterModel) View() tea.View {
	switch m.screen {
	case updateHubRouterDashboard:
		return m.dashboard.View()
	case updateHubRouterDetail:
		return m.detail.View()
	case updateHubRouterTable:
		return m.table.View()
	case updateHubRouterInput:
		return m.input.View()
	case updateHubRouterConfirm:
		return m.confirm.View()
	default:
		view := tea.NewView("")
		view.AltScreen = true
		return view
	}
}

func (m updateHubRouterModel) handleAction(action string) (tea.Model, tea.Cmd) {
	switch {
	case action == updevActionExit:
		m.finalAction = action
		return m, tea.Quit
	case action == updevActionBack:
		if m.screen == updateHubRouterDashboard {
			m.finalAction = updevActionExit
			return m, tea.Quit
		}
		if m.returnAction == updateHubActionDashboard {
			m.showDashboard("")
			m.dashboard.TopAnchor = true
			m.dashboard.State.Offset = 0
			m.detailStates[m.stateKey] = m.dashboard.State
			return m, nil
		}
		if strings.HasPrefix(m.stateKey, "filter-result:") && m.returnAction != "" {
			m.showReturnAction(m.returnAction)
			return m, nil
		}
		m.showReturnAction(m.returnAction)
		return m, nil
	case action == updevActionHome:
		m.showDashboard(updateHubActionDashboard)
		return m, nil
	}
	if _, ok := routedDetailWriteActionSpec(action); ok {
		m.showWriteAction(action)
		return m, nil
	}
	if updateHubExternalAction(action) {
		m.finalAction = action
		return m, tea.Quit
	}
	if route, ok := parseUpdateSummaryRoute(action); ok {
		m.defaultAction = route.Base
		m.showUpdateSummaryRoute(route)
		return m, nil
	}
	if filter, ok := parseUpdateHubFilterAction(action); ok {
		m.showUpdateFilterResult(filter)
		return m, nil
	}
	if query, ok := parseUpdateHubQueryAction(action); ok {
		m.showUpdateQueryInput(query.Section)
		return m, nil
	}
	if route, ok := parseListRouteAction(action); ok {
		m.showListRouteDetail(route)
		return m, nil
	}
	if action == listHubActionManual {
		m.showAction(listHubActionManual, updateHubActionInventoryAll)
		return m, nil
	}
	if routed := updateHubActionFromListAction(action); routed != "" {
		m.showAction(routed, updateHubActionDashboard)
		return m, nil
	}
	if updateHubActionExists(action) {
		m.defaultAction = action
		m.showAction(action, updateHubActionDashboard)
		return m, nil
	}
	return m, nil
}

func updateHubExternalAction(action string) bool {
	if action == updateHubActionInventoryAttention || action == updateHubActionJSON {
		return true
	}
	if detailAction, _, ok := parseManualPlanDetailAction(action); ok && (detailAction == "edit" || !manualPlanDetailActionRequiresConfirmation(detailAction)) {
		return true
	}
	if detailAction, _, _, ok := parseBackendDetailAction(action); ok && !backendDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	if detailAction, _, _, _, ok := parseSecurityDetailAction(action); ok && !securityDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	return false
}

func (m *updateHubRouterModel) showAction(action string, returnAction string) {
	if action == "" {
		action = updateHubActionDashboard
	}
	switch action {
	case updateHubActionDashboard:
		m.showDashboard(updateHubActionDashboard)
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: m.report.Inventory}, listOptions{})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		m.showListFiltered("updev installed inventory", inventory, "inventory-all", returnAction, listHubActionManual, listHubActionManual)
	case listHubActionManual:
		inventory := buildListReport(inventoryResult{Report: m.report.Inventory}, listOptions{})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		manualReport := derivedListReport(inventory, listOptions{provider: manualProviderName})
		m.showListFiltered("updev list manual", manualReport, listHubActionManual, updateHubActionInventoryAll, updateHubActionInventoryAll, updateHubActionInventoryAll)
	case updateHubActionInventoryDetails:
		m.showDetail("updev inventory details", updateInventoryDetailRowsWithBackend(m.report, m.backendPlan), "inventory-details", returnAction)
	case updateHubActionUpdatesFilter:
		m.showUpdateFilterMenu(updateHubActionUpdatesFilter, returnAction)
	case updateHubActionSecurityFilter:
		m.showUpdateFilterMenu(updateHubActionSecurityFilter, returnAction)
	case updateHubActionManualPlan:
		m.showTable("updev manual review plan", manualPlanToolSections(m.manualPlan), "manual-plan", returnAction)
	case updateHubActionBackends:
		m.showTable("updev backend convergence", backendToolSections(m.backendPlan), "backends", returnAction)
	case updateHubActionSecurity:
		m.showDetail("updev security details", updateSecurityDetailRows(m.report), "security", returnAction)
	case updateHubActionLogs:
		m.showDetail("updev update logs", updateLogDetailRows(m.report), "logs", returnAction)
	case updateHubActionFull:
		m.showDetail("updev full report", updateFullReportRows(m.report), "full", returnAction)
	default:
		m.showDashboard(m.defaultAction)
	}
}

const updateHubFilterActionPrefix = "update-filter"
const updateHubQueryActionPrefix = "update-query"

func updateHubFilterActionValue(section string, facet string, value string) string {
	return strings.Join([]string{updateHubFilterActionPrefix, section, facet, value}, "\t")
}

func updateHubQueryActionValue(section string) string {
	return strings.Join([]string{updateHubQueryActionPrefix, section}, "\t")
}

func parseUpdateHubFilterAction(value string) (updateHubFilterAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 4 || parts[0] != updateHubFilterActionPrefix {
		return updateHubFilterAction{}, false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return updateHubFilterAction{}, false
	}
	return updateHubFilterAction{Section: parts[1], Facet: parts[2], Value: parts[3]}, true
}

func parseUpdateHubQueryAction(value string) (updateHubQueryAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 2 || parts[0] != updateHubQueryActionPrefix || strings.TrimSpace(parts[1]) == "" {
		return updateHubQueryAction{}, false
	}
	return updateHubQueryAction{Section: parts[1]}, true
}

func (m *updateHubRouterModel) showUpdateFilterMenu(action string, returnAction string) {
	title := "updev update filter"
	stateKey := "filter-menu:updates"
	rows := updateFilterRows("updates", updateFilterActionProvider, updateStepProviderCounts(m.report.Steps))
	rows = append(rows, updateFilterRows("updates", updateFilterActionStatus, updateStepStatusCounts(m.report.Steps))...)
	if action == updateHubActionSecurityFilter {
		title = "updev security filter"
		stateKey = "filter-menu:security"
		rows = updateFilterRows("security", updateFilterActionProvider, safetyProviderCounts(m.report.Safety))
		rows = append(rows, updateFilterRows("security", updateFilterActionDecision, safetyDecisionCounts(m.report.Safety))...)
	}
	section := "updates"
	if action == updateHubActionSecurityFilter {
		section = "security"
	}
	rows = append(rows, updateQueryFilterRow(section))
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no filter values", "filter 値がありません"),
			Detail:  tr("The selected report section has no available filter values.", "選択した report section に利用可能な filter 値がありません。"),
		}}
	}
	m.showDetail(title, rows, stateKey, returnAction)
	m.detail.PrimaryEnterAction = true
}

func updateFilterRows(section string, facet string, counts map[string]int) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(counts))
	for _, value := range sortedMapKeys(counts) {
		rows = append(rows, detailBrowserRow{
			Title:   facet + ": " + value,
			Status:  value,
			Summary: fmt.Sprintf("%d rows", counts[value]),
			Detail:  tr("Open filtered update evidence for this value.", "この値で絞り込んだ update evidence を開きます。"),
			Actions: []detailBrowserAction{{
				Value:       updateHubFilterActionValue(section, facet, value),
				Label:       tr("open filter", "filter を開く"),
				Description: tr("show filtered evidence", "絞り込んだ evidence を表示します"),
			}},
		})
	}
	return rows
}

func updateQueryFilterRow(section string) detailBrowserRow {
	return detailBrowserRow{
		Title:   tr("query search", "query 検索"),
		Status:  "query",
		Summary: tr("search by text", "文字列で検索"),
		Detail:  tr("Search the selected evidence with a free text query.", "選択した evidence を自由入力の query で検索します。"),
		Actions: []detailBrowserAction{{
			Value:       updateHubQueryActionValue(section),
			Label:       tr("type query", "query を入力"),
			Description: tr("open query input", "query 入力を開きます"),
		}},
	}
}

func (m *updateHubRouterModel) showUpdateFilterResult(filter updateHubFilterAction) {
	opts := lastReportOptions{}
	switch filter.Section {
	case "security":
		opts.section = "security"
	default:
		opts.section = "updates"
	}
	switch filter.Facet {
	case updateFilterActionProvider:
		opts.provider = filter.Value
	case updateFilterActionStatus, updateFilterActionDecision:
		opts.status = filter.Value
	case updateFilterActionQuery:
		opts.query = filter.Value
	}
	filtered := filterUpdateReport(m.report, opts)
	stateKey := "filter-result:" + filter.Section + ":" + filter.Facet + ":" + filter.Value
	title := "updev update filter: " + filter.Value
	returnAction := updateHubActionUpdatesFilter
	rows := updateLogDetailRows(filtered)
	if filter.Section == "security" {
		title = "updev security filter: " + filter.Value
		returnAction = updateHubActionSecurityFilter
		rows = updateSecurityDetailRowsForFilter(filtered, opts)
	}
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *updateHubRouterModel) showUpdateQueryInput(section string) {
	title := "updev update query"
	description := tr("Search update commands, reasons, stdout, and stderr. Empty input returns to the filter menu.", "update command / reason / stdout / stderr を検索します。空入力なら filter menu に戻ります。")
	returnAction := updateHubActionUpdatesFilter
	if section == "security" {
		title = "updev security query"
		description = tr("Search security reasons, evidence, remediation, advisory IDs, and URLs. Empty input returns to the filter menu.", "security reason / evidence / remediation / advisory ID / URL を検索します。空入力なら filter menu に戻ります。")
		returnAction = updateHubActionSecurityFilter
	}
	m.screen = updateHubRouterInput
	m.stateKey = "query-input:" + section
	m.returnAction = returnAction
	m.input = newTextInputBrowserModel(title, description, "brew, hold, provenance, ...", "", m.color)
}

func (m updateHubRouterModel) handleInputAction(input textInputBrowserModel) (tea.Model, tea.Cmd) {
	switch input.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		if strings.HasPrefix(m.stateKey, "write-") {
			m.showReturnAction(m.pendingReturnAction)
			return m, nil
		}
		m.showReturnAction(m.returnAction)
		return m, nil
	case "submit":
		if strings.HasPrefix(m.stateKey, "write-reason:") {
			m.pendingReason = strings.TrimSpace(input.Value)
			if m.pendingReason == "" {
				m.showReturnAction(m.pendingReturnAction)
				return m, nil
			}
			m.showWriteExpiryInput()
			return m, nil
		}
		if strings.HasPrefix(m.stateKey, "write-expiry:") {
			expires, err := validateSecurityPolicyAllowExpiry(input.Value, time.Now())
			if err != nil {
				m.showReturnAction(m.pendingReturnAction)
				return m, nil
			}
			m.pendingExpires = expires
			m.showWriteConfirm()
			return m, nil
		}
		section := strings.TrimPrefix(m.stateKey, "query-input:")
		query := strings.TrimSpace(input.Value)
		if query == "" {
			m.showReturnAction(m.returnAction)
			return m, nil
		}
		m.showUpdateFilterResult(updateHubFilterAction{Section: section, Facet: updateFilterActionQuery, Value: query})
		return m, nil
	default:
		return m, nil
	}
}

func (m *updateHubRouterModel) showWriteAction(action string) {
	spec, ok := routedDetailWriteActionSpec(action)
	if !ok {
		return
	}
	m.pendingAction = action
	m.pendingReason = spec.DefaultReason
	m.pendingExpires = spec.DefaultExpires
	m.pendingReturnAction = m.currentAction()
	if m.pendingReturnAction == "" {
		m.pendingReturnAction = updateHubActionDashboard
	}
	if spec.NeedsReason {
		m.showWriteReasonInput(spec)
		return
	}
	m.showWriteConfirm()
}

func (m *updateHubRouterModel) showWriteReasonInput(spec detailWriteActionSpec) {
	model := newTextInputBrowserModel(spec.Title, spec.Description, spec.DefaultReason, spec.DefaultReason, m.color)
	model.Label = tr("reason:", "reason:")
	m.screen = updateHubRouterInput
	m.stateKey = "write-reason:" + m.pendingAction
	m.returnAction = m.pendingReturnAction
	m.input = model
}

func (m *updateHubRouterModel) showWriteExpiryInput() {
	_, _, _, _, ok := parseSecurityDetailAction(m.pendingAction)
	if !ok {
		m.showWriteConfirm()
		return
	}
	defaultExpiry := m.pendingExpires
	if strings.TrimSpace(defaultExpiry) == "" {
		defaultExpiry = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	}
	model := newTextInputBrowserModel(
		tr("security allow expiry", "security allow 期限"),
		tr("Enter the YYYY-MM-DD expiry for this temporary allow rule.", "一時 allow rule の期限を YYYY-MM-DD で入力します。"),
		defaultExpiry,
		defaultExpiry,
		m.color,
	)
	model.Label = tr("expires:", "expires:")
	m.screen = updateHubRouterInput
	m.stateKey = "write-expiry:" + m.pendingAction
	m.returnAction = m.pendingReturnAction
	m.input = model
}

func (m *updateHubRouterModel) showWriteConfirm() {
	spec, ok := routedDetailWriteActionSpec(m.pendingAction)
	if !ok {
		m.showReturnAction(m.pendingReturnAction)
		return
	}
	if m.pendingExpires != "" {
		spec.Description = spec.Description + "\n" + tr("expires: ", "期限: ") + m.pendingExpires
	}
	if m.pendingReason != "" {
		spec.Description = spec.Description + "\n" + tr("reason: ", "理由: ") + m.pendingReason
	}
	m.screen = updateHubRouterConfirm
	m.stateKey = "write-confirm:" + m.pendingAction
	m.returnAction = m.pendingReturnAction
	m.confirm = newConfirmBrowserModel(spec.Title, spec.Prompt, spec.Description, m.color)
}

func (m updateHubRouterModel) handleConfirmAction(confirm confirmBrowserModel) (tea.Model, tea.Cmd) {
	switch confirm.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		m.showReturnAction(m.pendingReturnAction)
		return m, nil
	case "apply":
		_ = applyRoutedDetailWriteAction(m.report.Root, &m.report, m.pendingAction, m.pendingReason, m.pendingExpires)
		m.refreshPlansAfterWriteAction()
		m.showReturnAction(m.pendingReturnAction)
		return m, nil
	default:
		return m, nil
	}
}

func (m *updateHubRouterModel) refreshPlansAfterWriteAction() {
	if action, _, ok := parseManualPlanDetailAction(m.pendingAction); ok && manualPlanDetailActionRequiresConfirmation(action) {
		m.manualPlan = buildInventoryPlanForHub(m.report.Root)
		m.manualLoading = false
	}
	if action, _, _, ok := parseBackendDetailAction(m.pendingAction); ok && backendDetailActionRequiresConfirmation(action) {
		m.backendPlan = buildBackendPlanForHub(m.report.Root)
		m.backendLoading = false
	}
	if action, _, _, _, ok := parseSecurityDetailAction(m.pendingAction); ok && securityDetailActionRequiresConfirmation(action) {
		m.report.Report = saveLastUpdateReport(m.report)
	}
}

func (m *updateHubRouterModel) showDashboard(focusAction string) {
	stateKey := "dashboard"
	state, hasState := m.detailStates[stateKey]
	if !hasState && (focusAction == "" || focusAction == updateHubActionDashboard) {
		focusAction = m.initialDashboardFocusAction()
	} else if hasState && focusAction == updateHubActionDashboard {
		state.Offset = 0
		state.Action = ""
		focusAction = m.initialDashboardFocusAction()
	}
	model := newUpdateSummaryBrowserModelWithLoading(updateHubTitle(m.report), m.report, m.manualPlan, m.manualLoading, m.backendPlan, m.backendLoading, state, focusAction, m.color)
	m.applyDashboardSize(&model)
	m.screen = updateHubRouterDashboard
	m.stateKey = stateKey
	m.returnAction = updateHubActionDashboard
	m.dashboard = model
}

func (m updateHubRouterModel) initialDashboardFocusAction() string {
	return updateHubActionLogs
}

func (m *updateHubRouterModel) showUpdateSummaryRoute(route updateSummaryRoute) {
	opts := lastReportOptions{provider: route.Provider, query: route.Query}
	filtered := filterUpdateReport(m.report, opts)
	suffix := updateSummaryRouteTitleSuffix(route)
	stateKey := updateSummaryRouteStateKey(route)
	switch route.Base {
	case updateHubActionLogs:
		m.showDetail("updev update logs"+suffix, updateLogDetailRows(filtered), stateKey, updateHubActionDashboard)
	case updateHubActionSecurity:
		m.showDetail("updev security details"+suffix, updateSecurityDetailRowsForFilter(filtered, opts), stateKey, updateHubActionDashboard)
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: filtered.Inventory}, listOptions{provider: route.Provider, query: route.Query})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		m.showListFiltered("updev installed inventory"+suffix, inventory, stateKey, updateHubActionDashboard, listHubActionManual, listHubActionManual)
	case updateHubActionInventoryDetails:
		m.showDetail("updev inventory details"+suffix, updateInventoryDetailRowsWithBackend(filtered, m.backendPlan), stateKey, updateHubActionDashboard)
	default:
		m.showDashboard(route.Base)
	}
}

func (m *updateHubRouterModel) showReturnAction(action string) {
	if route, ok := parseUpdateSummaryRouteStateKey(action); ok {
		m.showUpdateSummaryRoute(route)
		return
	}
	m.showAction(action, updateHubActionDashboard)
}

func updateSummaryRouteStateKey(route updateSummaryRoute) string {
	return "summary:" + route.Encode()
}

func parseUpdateSummaryRouteStateKey(stateKey string) (updateSummaryRoute, bool) {
	encoded, ok := strings.CutPrefix(stateKey, "summary:")
	if !ok {
		return updateSummaryRoute{}, false
	}
	return parseUpdateSummaryRoute(encoded)
}

func (m *updateHubRouterModel) showListRouteDetail(route listRouteAction) {
	stateKey := "route:" + route.Domain + ":" + route.Provider + ":" + route.Kind + ":" + route.Name
	rows := m.listRouteRows(route)
	if len(rows) == 0 {
		rows = []detailBrowserRow{emptyRouteDetailRow(route)}
	}
	m.detailStates[stateKey] = initialRouteDetailState(m.detailStates[stateKey])
	m.showDetail(routeDetailTitle(route), rows, stateKey, m.currentAction())
}

func (m updateHubRouterModel) listRouteRows(route listRouteAction) []detailBrowserRow {
	switch route.Domain {
	case listHubActionManual:
		manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: m.report.Root, provider: manualProviderName, query: route.Name})
		return manualPlanDetailRows(manualPlan)
	case listHubActionBackends:
		return backendDetailRowsForListRoute(m.backendPlan, route)
	case listHubActionUpdates:
		filtered := filterUpdateReport(m.report, lastReportOptions{section: "logs", provider: route.Provider, query: route.Name})
		return updateLogDetailRows(filtered)
	case listHubActionSecurity:
		opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Name}
		filtered := filterUpdateReport(m.report, opts)
		return updateSecurityDetailRowsForFilter(filtered, opts)
	default:
		return nil
	}
}

func (m *updateHubRouterModel) showListFiltered(title string, report listReport, stateKey string, returnAction string, nextAction string, previousAction string) {
	title = listTitleWithEvidenceSummary(title, report)
	sections := listTableSections(report)
	if toolTableRowCount(sections) > 0 || nextAction != "" || previousAction != "" {
		actions := tableBrowserActions()
		labels := tableBrowserLabels()
		if nextAction != "" || previousAction != "" {
			actions = tableBrowserActionsWithViewToggle(nextAction, previousAction)
			labels = tableBrowserLabelsWithViewToggle()
		}
		m.showTableWithActions(title, sections, stateKey, returnAction, actions, labels)
		return
	}
	rows := listDetailRows(report)
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no matching rows", "該当する行はありません"),
			Detail:  tr("The selected inventory filter has no rows.", "選択した inventory filter に一致する行はありません。"),
		}}
	}
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *updateHubRouterModel) showDetail(title string, rows []detailBrowserRow, stateKey string, returnAction string) {
	model := newDetailBrowserModel(title, rows, m.detailStates[stateKey], m.color)
	m.applyDetailSize(&model)
	m.screen = updateHubRouterDetail
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.detail = model
}

func (m *updateHubRouterModel) showTable(title string, sections []toolSection, stateKey string, returnAction string) {
	m.showTableWithActions(title, sections, stateKey, returnAction, tableBrowserActions(), tableBrowserLabels())
}

func (m *updateHubRouterModel) showTableWithActions(title string, sections []toolSection, stateKey string, returnAction string, actions reviewui.BrowserActions, labels reviewui.TableBrowserLabels) {
	title = m.loadingTitle(title, stateKey)
	model := newToolTableBrowserModelWithActions(title, sections, m.detailStates[stateKey], actions, labels, m.color)
	m.applyTableSize(&model)
	m.screen = updateHubRouterTable
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.table = model
}

func (m updateHubRouterModel) loadingTitle(title string, stateKey string) string {
	switch {
	case stateKey == "manual-plan" && m.manualLoading:
		return title + " " + tr("(manual review loading)", "(manual review 準備中)")
	case stateKey == "backends" && m.backendLoading:
		return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
	case (stateKey == "inventory-all" || stateKey == "inventory-details") && m.backendLoading:
		return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
	default:
		if route, ok := parseUpdateSummaryRouteStateKey(stateKey); ok && m.backendLoading && (route.Base == updateHubActionInventoryAll || route.Base == updateHubActionInventoryDetails) {
			return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
		}
		return title
	}
}

func (m updateHubRouterModel) currentAction() string {
	if _, ok := parseUpdateSummaryRouteStateKey(m.stateKey); ok {
		return m.stateKey
	}
	switch m.stateKey {
	case "inventory-all":
		return updateHubActionInventoryAll
	case "inventory-details":
		return updateHubActionInventoryDetails
	case "manual-plan":
		return updateHubActionManualPlan
	case "backends":
		return updateHubActionBackends
	case "security":
		return updateHubActionSecurity
	case "logs":
		return updateHubActionLogs
	case "full":
		return updateHubActionFull
	default:
		if m.returnAction != "" {
			return m.returnAction
		}
		return updateHubActionDashboard
	}
}

func (m updateHubRouterModel) applyDashboardSize(model *updateSummaryBrowserModel) {
	model.Width = m.width
	model.Height = m.height
	if model.TopAnchor {
		model.State.Offset = 0
		return
	}
	model.ensureSelectedVisible()
}

func (m updateHubRouterModel) applyDetailSize(model *detailBrowserModel) {
	model.Width = m.width
	model.Height = m.height
	model.ensureSelectedVisible()
}

func (m updateHubRouterModel) applyTableSize(model *toolTableBrowserModel) {
	model.Width = m.width
	model.Height = m.height
}

func updateHubDefaultAction(manualPlan inventoryPlanReport, backendPlan backendPlanReport, preferredAction string, report updateReport) string {
	defaultAction := updateHubActionInventoryAll
	if manualPlan.AttentionCount > 0 {
		defaultAction = updateHubActionManualPlan
	} else if len(backendPlan.Findings) > 0 {
		defaultAction = updateHubActionBackends
	}
	if updateHubActionExists(preferredAction) {
		defaultAction = preferredAction
	} else if updateHubActionAvailable(preferredAction, updateHubChoices(report, manualPlan, backendPlan, defaultAction)) {
		defaultAction = preferredAction
	}
	return defaultAction
}

func handleUpdateHubExternalAction(report *updateReport, manualPlan *inventoryPlanReport, backendPlan *backendPlanReport, action string) (string, bool) {
	switch {
	case action == "" || action == updevActionExit:
		return "", true
	case action == updateHubActionInventoryAttention:
		printLastInventorySection(os.Stdout, *report, lastReportOptions{section: "inventory", status: "attention", details: false}, textui.ColorEnabled())
		return updateHubActionDashboard, false
	case action == updateHubActionUpdatesFilter:
		opts, ok := selectUpdateStepFilter(*report)
		if !ok {
			return updateHubActionDashboard, false
		}
		filtered := filterUpdateReport(*report, opts)
		state, _ := runDetailBrowserWithState("updev update steps", updateLogDetailRows(filtered), detailBrowserState{}, textui.ColorEnabled())
		if state.Action == updevActionExit {
			return "", true
		}
		return updateHubActionDashboard, false
	case action == updateHubActionSecurityFilter:
		opts, ok := selectUpdateSecurityFilter(*report)
		if !ok {
			return updateHubActionDashboard, false
		}
		filtered := filterUpdateReport(*report, opts)
		state, _ := runDetailBrowserWithState("updev security filter", updateSecurityDetailRowsForFilter(filtered, opts), detailBrowserState{}, textui.ColorEnabled())
		if state.Action == updevActionExit {
			return "", true
		}
		return updateHubActionDashboard, false
	case action == updateHubActionJSON:
		entry := updateReportCacheEntry{Version: 1, Type: "update", CreatedAt: time.Now(), Report: *report}
		if cached, ok := loadLastUpdateReport(); ok {
			entry = cached
		}
		_ = encodeJSON(buildUpdateReportSectionView(entry, lastReportOptions{section: "full"}))
		return "", true
	}
	if handleManualPlanDetailAction(report.Root, action) {
		*manualPlan = buildInventoryPlanForHub(report.Root)
		return updateHubActionManualPlan, false
	}
	if handleBackendDetailAction(report.Root, action) {
		*backendPlan = buildBackendPlanForHub(report.Root)
		return updateHubActionBackends, false
	}
	if handleMiseBumpDetailAction(report, action) {
		return updateHubActionSecurity, false
	}
	if handleSecurityDetailAction(report, action) {
		return updateHubActionSecurity, false
	}
	return updateHubActionDashboard, false
}
