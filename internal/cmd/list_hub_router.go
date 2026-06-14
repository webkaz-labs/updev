package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/support"
)

type listHubRouterResult struct {
	Action       string
	ReturnAction string
	FromRoute    bool
	BackendPlan  backendPlanReport
	BackendReady bool
}

type listHubBackendPlanMsg struct {
	Report backendPlanReport
}

type listHubFilterAction struct {
	Kind  string
	Value string
}

type listSupportFilterAction struct {
	Kind  string
	Value string
}

type listHubRouterScreen string

const (
	listHubRouterDetail  listHubRouterScreen = "detail"
	listHubRouterTable   listHubRouterScreen = "table"
	listHubRouterInput   listHubRouterScreen = "input"
	listHubRouterConfirm listHubRouterScreen = "confirm"
)

type listHubRouterModel struct {
	report         listReport
	backendPlan    backendPlanReport
	backendLoading bool
	lastUpdate     updateReport
	hasLastUpdate  bool
	detailStates   map[string]detailBrowserState
	color          bool
	width          int
	height         int

	screen       listHubRouterScreen
	stateKey     string
	returnAction string
	finalAction  string
	writeFlow    reviewui.WriteFlow

	detail  detailBrowserModel
	table   toolTableBrowserModel
	input   textInputBrowserModel
	confirm confirmBrowserModel
}

func runListHubRouter(report listReport, backendPlan backendPlanReport, backendLoading bool, lastUpdate updateReport, hasLastUpdate bool, initialAction string, detailStates map[string]detailBrowserState, color bool) (listHubRouterResult, map[string]detailBrowserState, error) {
	model := newListHubRouterModel(report, backendPlan, backendLoading, lastUpdate, hasLastUpdate, initialAction, detailStates, color)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return listHubRouterResult{}, model.detailStates, err
	}
	if result, ok := final.(listHubRouterModel); ok {
		return listHubRouterResult{
			Action:       result.finalAction,
			ReturnAction: result.returnAction,
			FromRoute:    strings.HasPrefix(result.stateKey, "route:"),
			BackendPlan:  result.backendPlan,
			BackendReady: !result.backendLoading,
		}, result.detailStates, nil
	}
	return listHubRouterResult{}, model.detailStates, nil
}

func newListHubRouterModel(report listReport, backendPlan backendPlanReport, backendLoading bool, lastUpdate updateReport, hasLastUpdate bool, initialAction string, detailStates map[string]detailBrowserState, color bool) listHubRouterModel {
	detailStates = reviewui.EnsureStateCache(detailStates)
	model := listHubRouterModel{
		report:         report,
		backendPlan:    backendPlan,
		backendLoading: backendLoading,
		lastUpdate:     lastUpdate,
		hasLastUpdate:  hasLastUpdate,
		detailStates:   detailStates,
		color:          color,
	}
	if initialAction == "" {
		initialAction = listHubActionFull
	}
	model.showAction(initialAction, listHubActionFull)
	return model
}

func (m listHubRouterModel) Init() tea.Cmd {
	if m.backendLoading {
		root := m.report.Root
		return func() tea.Msg {
			return listHubBackendPlanMsg{Report: buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: root})}
		}
	}
	return nil
}

func (m listHubRouterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listHubBackendPlanMsg:
		m.backendPlan = msg.Report
		m.backendLoading = false
		m.report.Evidence = addBackendListEvidence(m.report.Evidence, msg.Report)
		m.refreshCurrentScreen()
		return m, nil
	}
	switch m.screen {
	case listHubRouterDetail:
		updated, _ := m.detail.Update(msg)
		if detail, ok := updated.(detailBrowserModel); ok {
			action := reviewui.TakeActionAndRemember(m.detailStates, m.stateKey, &detail.State)
			m.detail = detail
			if action != "" {
				return m.handleAction(action)
			}
		}
	case listHubRouterTable:
		updated, _ := m.table.Update(msg)
		if table, ok := updated.(toolTableBrowserModel); ok {
			action := reviewui.TakeActionAndRemember(m.detailStates, m.stateKey, &table.State)
			m.table = table
			if action != "" {
				return m.handleAction(action)
			}
		}
	case listHubRouterInput:
		updated, _ := m.input.Update(msg)
		if input, ok := updated.(textInputBrowserModel); ok {
			m.input = input
			if input.Action != "" {
				return m.handleInputAction(input)
			}
		}
	case listHubRouterConfirm:
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

func (m *listHubRouterModel) refreshCurrentScreen() {
	if strings.HasPrefix(m.stateKey, "route:") {
		return
	}
	if filter, ok := parseListHubFilterStateKey(m.stateKey); ok {
		m.showFilterResult(filter)
		return
	}
	switch m.stateKey {
	case listHubActionFull, listHubActionManual, listHubActionBackends, listHubActionUpdates, listHubActionSecurity, listHubActionSupport, listHubActionDetails:
		m.showAction(m.stateKey, m.returnAction)
	}
}

func (m listHubRouterModel) View() tea.View {
	switch m.screen {
	case listHubRouterDetail:
		return m.detail.View()
	case listHubRouterTable:
		return m.table.View()
	case listHubRouterInput:
		return m.input.View()
	case listHubRouterConfirm:
		return m.confirm.View()
	default:
		view := tea.NewView("")
		view.AltScreen = true
		return view
	}
}

func (m listHubRouterModel) handleAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case updevActionExit:
		m.finalAction = action
		return m, tea.Quit
	case updevActionBack:
		if strings.HasPrefix(m.stateKey, "route:") && m.returnAction != "" {
			m.showAction(m.returnAction, listHubActionFull)
			return m, nil
		}
		if strings.HasPrefix(m.stateKey, "support-filter:") && m.returnAction != "" {
			m.showAction(m.returnAction, listHubActionFull)
			return m, nil
		}
		if strings.HasPrefix(m.stateKey, "filter-result:") && m.returnAction != "" {
			m.showAction(m.returnAction, listHubActionFull)
			return m, nil
		}
		m.finalAction = updevActionBack
		return m, tea.Quit
	case updevActionHome:
		m.showAction(listHubActionFull, listHubActionFull)
		return m, nil
	}
	if filter, ok := parseListHubFilterAction(action); ok {
		m.showFilterResult(filter)
		return m, nil
	}
	if filter, ok := parseListSupportFilterAction(action); ok {
		m.showSupportFilterResult(filter)
		return m, nil
	}
	if route, ok := parseListRouteAction(action); ok {
		m.showRouteDetail(route)
		return m, nil
	}
	if _, ok := routedDetailWriteActionSpec(action); ok {
		m.showWriteAction(action)
		return m, nil
	}
	if listHubRouterExternalAction(action) {
		m.finalAction = action
		return m, tea.Quit
	}
	if listHubActionExists(action) {
		m.showAction(action, listHubActionFull)
		return m, nil
	}
	return m, nil
}

func listHubRouterExternalAction(action string) bool {
	if detailAction, _, _, ok := parseBackendDetailAction(action); ok && !backendDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	if detailAction, _, _, _, ok := parseSecurityDetailAction(action); ok && !securityDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	if detailAction, _, ok := parseManualPlanDetailAction(action); ok && (detailAction == "edit" || !manualPlanDetailActionRequiresConfirmation(detailAction)) {
		return true
	}
	return false
}

func (m *listHubRouterModel) showAction(action string, returnAction string) {
	switch action {
	case listHubActionAttention:
		attentionReport := derivedListReport(m.report, listOptions{status: "attention"})
		m.showListFiltered("updev list attention", attentionReport, "filter-result:attention:attention", listHubActionFull, "", "")
	case listHubActionProvider:
		m.showFilterMenu("updev list provider", listHubActionProvider, listProviderFilterRows(m.report), returnAction)
	case listHubActionKind:
		m.showFilterMenu("updev list kind", listHubActionKind, listCountFilterRows(listHubActionKind, listKindCounts(m.report), func(value string) string { return value }), returnAction)
	case listHubActionCategory:
		m.showFilterMenu("updev list category", listHubActionCategory, listCountFilterRows(listHubActionCategory, listCategoryCounts(m.report), categoryDescription), returnAction)
	case listHubActionStatus:
		m.showFilterMenu("updev list status", listHubActionStatus, listStatusFilterRows(), returnAction)
	case listHubActionQuery:
		m.showInput("updev list query", tr("Type a text filter. Empty input returns to the installed inventory.", "text filter を入力します。空入力ならインストール済み一覧に戻ります。"), "git, node, missing, ...", listHubActionQuery, returnAction)
	case listHubActionManual:
		manualReport := derivedListReport(m.report, listOptions{provider: manualProviderName})
		m.showListFiltered("updev list manual", manualReport, listHubActionManual, returnAction, listHubActionFull, listHubActionFull)
	case listHubActionBackends:
		m.showTable("updev backend convergence", backendToolSections(m.backendPlan), listHubActionBackends, returnAction, tableBrowserActions(), tableBrowserLabels())
	case listHubActionUpdates:
		if m.hasLastUpdate {
			m.showDetail("updev update evidence", updateLogDetailRows(m.lastUpdate), listHubActionUpdates, returnAction)
			return
		}
		m.showEmptyDetail("updev update evidence", listHubActionUpdates, returnAction)
	case listHubActionSecurity:
		if m.hasLastUpdate {
			m.showDetail("updev security review", updateSecurityDetailRows(m.lastUpdate), listHubActionSecurity, returnAction)
			return
		}
		m.showEmptyDetail("updev security review", listHubActionSecurity, returnAction)
	case listHubActionSupport:
		m.showSupportCatalog(supportOptions{Surface: "all"}, listHubActionSupport, returnAction)
	case listHubActionDetails:
		detailReport := derivedListReport(m.report, listOptions{limit: 10})
		m.showDetail("updev list details", listDetailRows(detailReport), listHubActionDetails, returnAction)
	case listHubActionLimited:
		limitedReport := derivedListReport(m.report, listOptions{limit: 10})
		m.showListFiltered("updev compact list", limitedReport, listHubActionLimited, returnAction, "", "")
	case listHubActionFull:
		m.showListFiltered("updev installed inventory", m.report, listHubActionFull, returnAction, listHubActionManual, listHubActionManual)
	default:
		m.showListFiltered("updev installed inventory", m.report, listHubActionFull, returnAction, listHubActionManual, listHubActionManual)
	}
}

func (m listHubRouterModel) handleInputAction(input textInputBrowserModel) (tea.Model, tea.Cmd) {
	switch input.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		if reviewui.IsWriteStateKey(m.stateKey) {
			m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
			return m, nil
		}
		m.showAction(m.returnAction, listHubActionFull)
		return m, nil
	case "submit":
		if reviewui.IsWriteReasonStateKey(m.stateKey) {
			if !m.writeFlow.AcceptReason(input.Value) {
				m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
				return m, nil
			}
			m.showWriteExpiryInput()
			return m, nil
		}
		if reviewui.IsWriteExpiryStateKey(m.stateKey) {
			if !m.writeFlow.AcceptExpiry(input.Value, time.Now(), validateSecurityPolicyAllowExpiry) {
				m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
				return m, nil
			}
			m.showWriteConfirm()
			return m, nil
		}
		query := strings.TrimSpace(input.Value)
		if query == "" {
			m.showAction(listHubActionFull, listHubActionFull)
			return m, nil
		}
		m.showFilterResult(listHubFilterAction{Kind: listHubActionQuery, Value: query})
		return m, nil
	default:
		return m, nil
	}
}

func (m *listHubRouterModel) showWriteAction(action string) {
	spec, ok := routedDetailWriteActionSpec(action)
	if !ok {
		return
	}
	m.writeFlow = reviewui.NewWriteFlow(action, m.currentAction(), listHubActionFull, spec)
	if spec.NeedsReason {
		m.showWriteReasonInput(spec)
		return
	}
	m.showWriteConfirm()
}

func (m *listHubRouterModel) showWriteReasonInput(spec detailWriteActionSpec) {
	model := newTextInputBrowserModel(spec.Title, spec.Description, spec.DefaultReason, spec.DefaultReason, m.color)
	model.Label = tr("reason:", "reason:")
	m.screen = listHubRouterInput
	m.stateKey = reviewui.WriteReasonStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.input = model
}

func (m *listHubRouterModel) showWriteExpiryInput() {
	defaultExpiry := m.writeFlow.DefaultExpiry(time.Now())
	model := newTextInputBrowserModel(
		tr("security allow expiry", "security allow 期限"),
		tr("Enter the YYYY-MM-DD expiry for this temporary allow rule.", "一時 allow rule の期限を YYYY-MM-DD で入力します。"),
		defaultExpiry,
		defaultExpiry,
		m.color,
	)
	model.Label = tr("expires:", "expires:")
	m.screen = listHubRouterInput
	m.stateKey = reviewui.WriteExpiryStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.input = model
}

func (m *listHubRouterModel) showWriteConfirm() {
	spec, ok := routedDetailWriteActionSpec(m.writeFlow.Action)
	if !ok {
		m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
		return
	}
	spec.Description = m.writeFlow.ConfirmDescription(spec, tr("expires: ", "期限: "), tr("reason: ", "理由: "))
	m.screen = listHubRouterConfirm
	m.stateKey = reviewui.WriteConfirmStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.confirm = newConfirmBrowserModel(spec.Title, spec.Prompt, spec.Description, m.color)
}

func (m listHubRouterModel) handleConfirmAction(confirm confirmBrowserModel) (tea.Model, tea.Cmd) {
	switch confirm.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
		return m, nil
	case "apply":
		var report *updateReport
		if m.hasLastUpdate {
			report = &m.lastUpdate
		}
		_ = applyRoutedDetailWriteAction(m.report.Root, report, m.writeFlow.Action, m.writeFlow.Reason, m.writeFlow.Expires)
		m.refreshAfterWriteAction()
		m.showAction(m.writeFlow.ReturnAction, listHubActionFull)
		return m, nil
	default:
		return m, nil
	}
}

func (m *listHubRouterModel) refreshAfterWriteAction() {
	if action, _, _, ok := parseBackendDetailAction(m.writeFlow.Action); ok && backendDetailActionRequiresConfirmation(action) {
		m.backendPlan = buildBackendPlanForHub(m.report.Root)
		m.backendLoading = false
		m.report.Evidence = addBackendListEvidence(m.report.Evidence, m.backendPlan)
	}
	if action, _, _, _, ok := parseSecurityDetailAction(m.writeFlow.Action); ok && securityDetailActionRequiresConfirmation(action) {
		m.hasLastUpdate = true
	}
}

func (m *listHubRouterModel) showFilterMenu(title string, kind string, rows []detailBrowserRow, returnAction string) {
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no filter values", "filter 値がありません"),
			Detail:  tr("The selected filter has no available values.", "選択した filter に利用可能な値がありません。"),
		}}
	}
	stateKey := "filter-menu:" + kind
	m.showDetail(title, rows, stateKey, returnAction)
	m.detail.PrimaryEnterAction = true
}

func (m *listHubRouterModel) showFilterResult(filter listHubFilterAction) {
	opts := listOptions{}
	title := "updev list " + filter.Value
	switch filter.Kind {
	case listHubActionProvider:
		opts.provider = filter.Value
	case listHubActionKind:
		opts.kind = filter.Value
	case listHubActionCategory:
		opts.category = filter.Value
	case listHubActionStatus:
		opts.status = filter.Value
	case listHubActionQuery:
		opts.query = filter.Value
	}
	report := derivedListReport(m.report, opts)
	stateKey := "filter-result:" + filter.Kind + ":" + filter.Value
	m.showListFiltered(title, report, stateKey, filter.Kind, "", "")
}

const listSupportFilterActionPrefix = "support-filter"

func listSupportFilterActionValue(kind string, value string) string {
	return strings.Join([]string{listSupportFilterActionPrefix, kind, value}, "\t")
}

func parseListSupportFilterAction(value string) (listSupportFilterAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 3 || parts[0] != listSupportFilterActionPrefix {
		return listSupportFilterAction{}, false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return listSupportFilterAction{}, false
	}
	return listSupportFilterAction{Kind: parts[1], Value: parts[2]}, true
}

func (m *listHubRouterModel) showSupportFilterResult(filter listSupportFilterAction) {
	opts := supportOptions{Surface: "all"}
	switch filter.Kind {
	case "surface":
		opts.Surface = filter.Value
	case "label":
		if filter.Value != "all" {
			opts.Label = filter.Value
		}
	}
	stateKey := "support-filter:" + filter.Kind + ":" + filter.Value
	m.showSupportCatalog(opts, stateKey, listHubActionSupport)
}

func (m *listHubRouterModel) showInput(title string, description string, placeholder string, stateKey string, returnAction string) {
	model := newTextInputBrowserModel(title, description, placeholder, "", m.color)
	m.screen = listHubRouterInput
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.input = model
}

func (m *listHubRouterModel) showSupportCatalog(opts supportOptions, stateKey string, returnAction string) {
	report := buildSupportReport(opts)
	rows := append(supportCatalogFilterRows(), supportCatalogDetailRows(report)...)
	m.showDetail(supportCatalogTitle(opts), rows, stateKey, returnAction)
}

func supportCatalogTitle(opts supportOptions) string {
	parts := []string{"updev support catalog"}
	if opts.Surface != "" && opts.Surface != "all" {
		parts = append(parts, "surface="+opts.Surface)
	}
	if opts.Label != "" {
		parts = append(parts, "label="+opts.Label)
	}
	return strings.Join(parts, " ")
}

func supportCatalogDetailRows(report supportReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Entries))
	for _, entry := range report.Entries {
		metadata := []string{
			"surface: " + entry.Surface,
			"support_label: " + entry.Label,
		}
		if entry.Scope != "" {
			metadata = append(metadata, "scope: "+entry.Scope)
		}
		for _, evidence := range entry.Evidence {
			metadata = append(metadata, "evidence: "+evidence)
		}
		for _, limitation := range entry.Limitations {
			metadata = append(metadata, "limitation: "+limitation)
		}
		if entry.Next != "" {
			metadata = append(metadata, "next: "+entry.Next)
		}
		rows = append(rows, detailBrowserRow{
			Title:    entry.Surface + "/" + entry.Name,
			Status:   entry.Label,
			Summary:  entry.Summary,
			Detail:   entry.Summary,
			Metadata: metadata,
		})
	}
	return rows
}

func supportCatalogFilterRows() []detailBrowserRow {
	surfaceActions := []detailBrowserAction{}
	for _, surface := range []string{"all", "provider", "command", "report", "inventory_source"} {
		surfaceActions = append(surfaceActions, detailBrowserAction{
			Value:       listSupportFilterActionValue("surface", surface),
			Label:       surface,
			Description: tr("filter support catalog by surface", "support catalog を surface で絞り込みます"),
		})
	}
	labelActions := []detailBrowserAction{}
	for _, label := range []string{"all", support.LabelSupportedPreview, support.LabelExperimental, support.LabelCompatibility, support.LabelDeferred} {
		labelActions = append(labelActions, detailBrowserAction{
			Value:       listSupportFilterActionValue("label", label),
			Label:       label,
			Description: tr("filter support catalog by label", "support catalog を label で絞り込みます"),
		})
	}
	return []detailBrowserRow{
		{
			Title:   tr("surface filter", "surface filter"),
			Status:  string(plan.StatusOK),
			Summary: "all / provider / command / report / inventory_source",
			Detail:  tr("Choose a support surface. Use / for free-text query filtering.", "support surface を選択します。自由検索は / を使います。"),
			Actions: surfaceActions,
		},
		{
			Title:   tr("label filter", "label filter"),
			Status:  string(plan.StatusOK),
			Summary: strings.Join([]string{support.LabelSupportedPreview, support.LabelExperimental, support.LabelCompatibility, support.LabelDeferred}, " / "),
			Detail:  tr("Choose a support label. Use / for free-text query filtering.", "support label を選択します。"),
			Actions: labelActions,
		},
	}
}

func providerSupportLabel(name string) string {
	return support.ProviderLabel(name)
}

func (m *listHubRouterModel) showRouteDetail(route listRouteAction) {
	stateKey := "route:" + route.Domain + ":" + route.Provider + ":" + route.Kind + ":" + route.Name
	rows := m.routeRows(route)
	if len(rows) == 0 {
		rows = []detailBrowserRow{emptyRouteDetailRow(route)}
	}
	m.detailStates[stateKey] = initialRouteDetailState(m.detailStates[stateKey])
	m.showDetail(routeDetailTitle(route), rows, stateKey, m.currentAction())
}

func (m listHubRouterModel) routeRows(route listRouteAction) []detailBrowserRow {
	switch route.Domain {
	case listHubActionManual:
		manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: m.report.Root, provider: manualProviderName, query: route.Name})
		return manualPlanDetailRows(manualPlan)
	case listHubActionBackends:
		return backendDetailRowsForListRoute(m.backendPlan, route)
	case listHubActionUpdates:
		if m.hasLastUpdate {
			filtered := filterUpdateReport(m.lastUpdate, lastReportOptions{section: "logs", provider: route.Provider, query: route.Name})
			return updateLogDetailRows(filtered)
		}
	case listHubActionSecurity:
		if m.hasLastUpdate {
			opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Name}
			filtered := filterUpdateReport(m.lastUpdate, opts)
			return updateSecurityDetailRowsForFilter(filtered, opts)
		}
	}
	return nil
}

const listHubFilterActionPrefix = "list-filter"

func listHubFilterActionValue(kind string, value string) string {
	return strings.Join([]string{listHubFilterActionPrefix, kind, value}, "\t")
}

func parseListHubFilterAction(value string) (listHubFilterAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 3 || parts[0] != listHubFilterActionPrefix {
		return listHubFilterAction{}, false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return listHubFilterAction{}, false
	}
	return listHubFilterAction{Kind: parts[1], Value: parts[2]}, true
}

func parseListHubFilterStateKey(stateKey string) (listHubFilterAction, bool) {
	const prefix = "filter-result:"
	rest, ok := strings.CutPrefix(stateKey, prefix)
	if !ok {
		return listHubFilterAction{}, false
	}
	kind, value, ok := strings.Cut(rest, ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(value) == "" {
		return listHubFilterAction{}, false
	}
	return listHubFilterAction{Kind: kind, Value: value}, true
}

func listProviderFilterRows(report listReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Providers))
	for _, provider := range report.Providers {
		summaryParts := []string{fmt.Sprintf("desired=%d live=%d missing=%d extra=%d", provider.Desired, provider.Live, provider.Missing, provider.Extra)}
		if label := providerSupportLabel(provider.Name); support.LabelIsDenseBadge(label) {
			summaryParts = append(summaryParts, "support="+label)
		}
		metadata := []string{}
		if label := providerSupportLabel(provider.Name); label != "" {
			metadata = append(metadata, "support_label: "+label)
		}
		rows = append(rows, detailBrowserRow{
			Title:    provider.Name,
			Status:   string(plan.ProviderStatus(provider)),
			Summary:  strings.Join(summaryParts, " "),
			Detail:   tr("Open installed inventory rows for this provider.", "この provider の installed inventory 行を開きます。"),
			Metadata: metadata,
			Actions: []detailBrowserAction{{
				Value:       listHubFilterActionValue(listHubActionProvider, provider.Name),
				Label:       tr("open provider", "provider を開く"),
				Description: tr("show provider-filtered inventory", "provider で絞り込んだ inventory を表示します"),
			}},
		})
	}
	return rows
}

func listCountFilterRows(kind string, counts map[string]int, describe func(string) string) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(counts))
	for _, value := range sortedMapKeys(counts) {
		summary := fmt.Sprintf("%d rows", counts[value])
		if description := strings.TrimSpace(describe(value)); description != "" {
			summary += " - " + description
		}
		rows = append(rows, detailBrowserRow{
			Title:   value,
			Status:  string(plan.StatusOK),
			Summary: summary,
			Detail:  tr("Open installed inventory rows for this filter value.", "この filter 値で絞り込んだ installed inventory 行を開きます。"),
			Actions: []detailBrowserAction{{
				Value:       listHubFilterActionValue(kind, value),
				Label:       tr("open filter", "filter を開く"),
				Description: tr("show filtered inventory", "絞り込んだ inventory を表示します"),
			}},
		})
	}
	return rows
}

func listStatusFilterRows() []detailBrowserRow {
	statuses := []struct {
		value       string
		description string
	}{
		{"attention", tr("Rows needing attention.", "対応が必要な行。")},
		{"active", tr("Active mise/tool rows.", "active な mise/tool 行。")},
		{"inactive", tr("Inactive mise/tool rows.", "inactive な mise/tool 行。")},
		{"installed", tr("Installed mise/tool rows.", "インストール済みの mise/tool 行。")},
		{"profile-mismatch", tr("Rows installed from an inactive deployment scope.", "inactive な deployment scope 由来でインストール済みの行。")},
		{"missing", tr("Desired but not installed.", "desired だが未インストール。")},
		{"extra", tr("Installed but not desired.", "インストール済みだが desired ではない。")},
		{"drift", tr("Desired and live state differ.", "desired と live state が異なる。")},
		{"unavailable", tr("Provider data was unavailable.", "provider data が取得できない。")},
		{"ok", tr("Rows already matching desired state.", "desired state と一致済み。")},
	}
	rows := make([]detailBrowserRow, 0, len(statuses))
	for _, status := range statuses {
		rows = append(rows, detailBrowserRow{
			Title:   status.value,
			Status:  status.value,
			Summary: status.description,
			Detail:  tr("Open installed inventory rows for this status.", "この status の installed inventory 行を開きます。"),
			Actions: []detailBrowserAction{{
				Value:       listHubFilterActionValue(listHubActionStatus, status.value),
				Label:       tr("open status", "status を開く"),
				Description: tr("show status-filtered inventory", "status で絞り込んだ inventory を表示します"),
			}},
		})
	}
	return rows
}

func (m *listHubRouterModel) showListFiltered(title string, report listReport, stateKey string, returnAction string, nextAction string, previousAction string) {
	title = listTitleWithEvidenceSummary(title, report)
	sections := listTableSections(report)
	if toolTableRowCount(sections) > 0 || nextAction != "" || previousAction != "" {
		actions := tableBrowserActions()
		labels := tableBrowserLabels()
		if nextAction != "" || previousAction != "" {
			actions = tableBrowserActionsWithViewToggle(nextAction, previousAction)
			labels = tableBrowserLabelsWithViewToggle()
		}
		m.showTable(title, sections, stateKey, returnAction, actions, labels)
		return
	}
	rows := listDetailRows(report)
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no matching rows", "該当する行はありません"),
			Detail:  tr("The selected inventory view has no rows.", "選択した inventory view に一致する行はありません。"),
		}}
	}
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *listHubRouterModel) showEmptyDetail(title string, stateKey string, returnAction string) {
	m.showDetail(title, []detailBrowserRow{{
		Title:   title,
		Status:  string(plan.StatusOK),
		Summary: tr("no cached evidence", "cached evidence はありません"),
		Detail:  tr("Run updev first to create cached update/security evidence.", "cached update/security evidence を作るには先に updev を実行します。"),
	}}, stateKey, returnAction)
}

func (m *listHubRouterModel) showDetail(title string, rows []detailBrowserRow, stateKey string, returnAction string) {
	model := newDetailBrowserModel(title, rows, reviewui.CachedState(m.detailStates, stateKey), m.color)
	model.Width = m.width
	model.Height = m.height
	model.EnsureSelectedVisible()
	m.screen = listHubRouterDetail
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.detail = model
}

func (m *listHubRouterModel) showTable(title string, sections []toolSection, stateKey string, returnAction string, actions reviewui.BrowserActions, labels reviewui.TableBrowserLabels) {
	title = m.loadingTitle(title, stateKey)
	model := reviewui.NewTableBrowserModel(title, sections, reviewui.CachedState(m.detailStates, stateKey), labels, actions, m.color)
	model.Width = m.width
	model.Height = m.height
	m.screen = listHubRouterTable
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.table = model
}

func (m listHubRouterModel) loadingTitle(title string, stateKey string) string {
	if !m.backendLoading {
		return title
	}
	switch stateKey {
	case listHubActionFull, listHubActionManual, listHubActionBackends, listHubActionSupport, listHubActionDetails:
		return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
	default:
		if _, ok := parseListHubFilterStateKey(stateKey); ok {
			return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
		}
		return title
	}
}

func (m listHubRouterModel) currentAction() string {
	if m.stateKey != "" {
		switch m.stateKey {
		case listHubActionFull, listHubActionManual, listHubActionBackends, listHubActionUpdates, listHubActionSecurity, listHubActionSupport, listHubActionDetails:
			return m.stateKey
		}
		if strings.HasPrefix(m.stateKey, "support-filter:") {
			return listHubActionSupport
		}
	}
	if m.returnAction != "" {
		return m.returnAction
	}
	return listHubActionFull
}

type detailWriteActionSpec = reviewui.WriteActionSpec

func routedDetailWriteActionSpec(value string) (detailWriteActionSpec, bool) {
	if action, target, ok := parseManualPlanDetailAction(value); ok {
		if action == "edit" || !manualPlanDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		return detailWriteActionSpec{
			Title:       tr("manual inventory action", "manual inventory 操作"),
			Prompt:      fmt.Sprintf(tr("Apply %s to %s?", "%s を %s に適用しますか?"), action, target),
			Description: tr("Write the selected manual inventory override.", "選択した manual inventory override を書き込みます。"),
		}, true
	}
	if action, current, recommended, ok := parseBackendDetailAction(value); ok {
		if !backendDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		switch action {
		case "remove-brew":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Remove Brewfile ownership %s? mise entry %s already exists.", "Brewfile ownership %s を削除しますか? mise entry %s は既に存在します。"), current, recommended),
				Description: tr("Remove desired-state ownership from Brewfile. This does not uninstall the Homebrew package.", "Brewfile の desired-state ownership を削除します。Homebrew package の uninstall は行いません。"),
			}, true
		case "rewrite-mise":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Rewrite mise backend %s -> %s?", "mise backend を %s -> %s に書き換えますか?"), current, recommended),
				Description: tr("Rename the mise tool key and preserve its current spec.", "現在の spec を維持したまま mise tool key を rename します。"),
			}, true
		case "remove-mise":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Remove old mise backend %s? Preferred entry %s already covers it.", "古い mise backend %s を削除しますか? 優先 entry %s が既にカバーしています。"), current, recommended),
				Description: tr("Remove the current mise tool key because the preferred entry already covers it.", "優先 entry がカバー済みのため、現在の mise tool key を削除します。"),
			}, true
		}
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(value); ok {
		if !securityDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		if isHomebrewTrustSecurityAction(action) {
			command, ok := homebrewTrustCommandForSecurityAction(action, name)
			if !ok {
				return detailWriteActionSpec{}, false
			}
			description := tr("Trust only this Homebrew package after reviewing its source.", "出所を確認したうえで、この Homebrew package だけを trust します。")
			if action == securityActionBrewTrustTap {
				description = tr("Trust the whole tap only when you accept all current and future entries.", "現在と今後の entry すべてを受け入れる場合だけ、tap 全体を trust します。")
			}
			return detailWriteActionSpec{
				Title:       tr("homebrew trust action", "Homebrew trust 操作"),
				Prompt:      fmt.Sprintf(tr("Run %s?", "%s を実行しますか?"), joinCommand(command)),
				Description: description,
			}, true
		}
		decision, reason, expires, _ := defaultSecurityDetailActionInputs(action)
		spec := detailWriteActionSpec{
			Title:          tr("security policy action", "security policy 操作"),
			Prompt:         fmt.Sprintf(tr("Mark %s/%s %s as %s?", "%s/%s %s を %s にしますか?"), provider, kind, name, decision),
			Description:    tr("Write a local security policy rule.", "local security policy rule を書き込みます。"),
			DefaultReason:  reason,
			DefaultExpires: expires,
		}
		if expires != "" {
			spec.Description = fmt.Sprintf(tr("Write a local security policy rule that expires on %s.", "%s に期限切れになる local security policy rule を書き込みます。"), expires)
		}
		if securityDetailActionRequiresCustomInput(action) {
			spec.Prompt = fmt.Sprintf(tr("Allow %s/%s %s with a custom reason and expiry?", "%s/%s %s を理由/期限付きで許可しますか?"), provider, kind, name)
			spec.Description = tr("Enter a review reason and YYYY-MM-DD expiry before writing the local allow rule.", "local allow rule を書く前に review reason と YYYY-MM-DD expiry を入力します。")
			spec.NeedsReason = true
			spec.NeedsExpiry = true
			spec.DefaultReason = "accepted from updev detail browser after local review"
			spec.DefaultExpires = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
		}
		return spec, true
	}
	return detailWriteActionSpec{}, false
}

func applyRoutedDetailWriteAction(root string, report *updateReport, value string, reason string, expires string) bool {
	if action, target, ok := parseManualPlanDetailAction(value); ok {
		return applyConfirmedManualPlanDetailAction(root, action, target)
	}
	if action, current, recommended, ok := parseBackendDetailAction(value); ok {
		return applyConfirmedBackendDetailAction(root, action, current, recommended)
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(value); ok {
		if !securityDetailActionRequiresCustomInput(action) {
			_, reason, expires, _ = defaultSecurityDetailActionInputs(action)
		}
		return applyConfirmedSecurityDetailActionSilently(report, action, provider, kind, name, strings.TrimSpace(reason), strings.TrimSpace(expires))
	}
	return false
}
