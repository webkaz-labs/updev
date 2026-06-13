package cmd

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type updateSummaryLine = reviewui.ActionSummaryLine
type updateSummaryLineKind = reviewui.ActionSummaryLineKind

const (
	updateSummaryLineNormal      = reviewui.ActionSummaryLineNormal
	updateSummaryLineSection     = reviewui.ActionSummaryLineSection
	updateSummaryLineTableHeader = reviewui.ActionSummaryLineTableHeader
	updateSummaryLineMeta        = reviewui.ActionSummaryLineMeta
)

type updateSummaryRoute struct {
	Base     string
	Provider string
	Query    string
}

const updateSummaryRoutePrefix = "summary-route"

type updateSummaryBrowserModel = reviewui.ActionSummaryModel

func runUpdateSummaryBrowser(title string, report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, state reviewui.State, focusAction string, color bool) (reviewui.State, error) {
	model := newUpdateSummaryBrowserModel(title, report, manualPlan, backendPlan, state, focusAction, color)
	return reviewui.RunActionSummaryModel(model)
}

func newUpdateSummaryBrowserModel(title string, report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	return newUpdateSummaryBrowserModelWithLoading(title, report, manualPlan, false, backendPlan, false, state, focusAction, color)
}

func newUpdateSummaryBrowserModelWithLoading(title string, report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	return newActionSummaryBrowserModel(title, updateSummaryBrowserLinesWithLoading(report, manualPlan, manualLoading, backendPlan, backendLoading, color), state, focusAction, color)
}

func newActionSummaryBrowserModel(title string, lines []updateSummaryLine, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	labels := reviewui.ActionSummaryLabels{
		HelpMove:      tr("Up/Down or j/k: move between selectable summary rows", "↑↓ または j/k: 選択可能な summary 行を移動"),
		HelpOpen:      tr("Enter, Space, or a: open the selected detail", "Enter / Space / a: 選択した詳細を開く"),
		HelpExit:      tr("b, q, Esc, or Ctrl-C: exit", "b / q / Esc / Ctrl-C: 終了"),
		Controls:      tr("Up/Down/j/k move, Enter open selected summary, Space/a open, ? help, q exit", "↑↓/j/k 移動、Enter で選択 summary を開く、Space/a も開く、? help、q 終了"),
		FocusedPrefix: tr("focused actions:", "選択中の操作:"),
		EnterFormat:   tr("Enter: %s", "Enter: %s"),
	}
	actions := reviewui.ActionSummaryActions{Exit: updevActionExit}
	focusMatcher := func(lineAction string, focusAction string) bool {
		route, ok := parseUpdateSummaryRoute(lineAction)
		return ok && route.Base == focusAction
	}
	return reviewui.NewActionSummaryModel(title, lines, state, focusAction, labels, actions, focusMatcher, color)
}

func updateSummaryBrowserLines(report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, color bool) []updateSummaryLine {
	return updateSummaryBrowserLinesWithLoading(report, manualPlan, false, backendPlan, false, color)
}

func updateSummaryBrowserLinesWithLoading(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, color bool) []updateSummaryLine {
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
				kind = updateSummaryLineMeta
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("reason:", "理由:")):
				kind = updateSummaryLineMeta
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
				styledLine = strings.TrimPrefix(styledLine, "  ")
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
	reviewLines := updateSummaryReviewActionLinesWithLoading(manualPlan, manualLoading, backendPlan, backendLoading, color)
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
		hasDecision := false
		if strings.EqualFold(fields[0], "hold") || strings.EqualFold(fields[0], "review") || strings.EqualFold(fields[0], "allow") || strings.EqualFold(fields[0], "block") {
			providerIndex = 1
			hasDecision = true
		}
		if providerIndex >= len(fields) {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[providerIndex]
		query := ""
		if hasDecision && providerIndex+1 < len(fields) {
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
	return updateSummaryReviewActionLinesWithLoading(manualPlan, false, backendPlan, false, color)
}

func updateSummaryReviewActionLinesWithLoading(manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, color bool) []updateSummaryLine {
	lines := []updateSummaryLine{}
	if manualLoading {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("manual review", "手動アプリ確認"), tr("loading - preparing manual/vendor app candidates", "loading - 手動/vendor app 候補を準備中"), color),
			Action:          updateHubActionManualPlan,
			Label:           tr("open manual review", "手動アプリ確認を開く"),
			HideInlineBadge: true,
		})
	} else if manualPlan.AttentionCount > 0 {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("manual review", "手動アプリ確認"), fmt.Sprintf("%s - %s", manualPlanStatus(manualPlan), updateDashboardManualPlanSummary(manualPlan)), color),
			Action:          updateHubActionManualPlan,
			Label:           tr("open manual review", "手動アプリ確認を開く"),
			HideInlineBadge: true,
		})
	}
	if backendLoading {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("backend convergence", "backend 整理"), tr("loading - preparing backend evidence", "loading - backend evidence を準備中"), color),
			Action:          updateHubActionBackends,
			Label:           tr("open backend convergence", "backend 整理を開く"),
			HideInlineBadge: true,
		})
	} else if len(backendPlan.Findings) > 0 {
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

func browserSectionHeadingText(text string, color bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return textui.StyleSection(trimmed, color)
}
