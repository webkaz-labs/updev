package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/webkaz-labs/updev/internal/backend"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

const (
	backendPlanReportSchemaVersion = backend.ReportSchemaVersion
	backendDetailActionPrefix      = "backend"
)

type backendOptions struct {
	command string
	format  string
	root    string
}

type (
	backendPlanReport     = backend.Report
	backendFinding        = backend.Finding
	backendPreferenceTier = backend.PreferenceTier
)

func parseBackendOptions(command string, args []string) (backendOptions, error) {
	opts := backendOptions{command: command, format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runBackends(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: updev backends <doctor|plan> [--format text|json]")
		return usageExitCode
	}
	command := args[0]
	if command != "doctor" && command != "plan" {
		fmt.Fprintf(os.Stderr, "unsupported backends command: %s\n", command)
		return usageExitCode
	}
	opts, err := parseBackendOptions(command, args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	report := buildBackendPlanReportWithRunner(context.Background(), opts, runner.Local{})
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printBackendPlanText(os.Stdout, report, textui.ColorEnabled())
	}
	return updateExitCode(report.Status)
}

func buildBackendPlanReport(ctx context.Context, opts backendOptions) backendPlanReport {
	return buildBackendPlanReportWithRunner(ctx, opts, runner.Local{})
}

func buildBackendPlanReportWithRunner(ctx context.Context, opts backendOptions, commandRunner runner.Runner) backendPlanReport {
	return backend.BuildReport(ctx, backend.Options{
		Command:         opts.command,
		Root:            opts.root,
		PreferenceOrder: loadUpdevConfig().Backends.PreferenceOrder,
	}, commandRunner)
}

func backendPreferenceTiers() []backendPreferenceTier {
	return backend.Registry{PreferenceOrder: loadUpdevConfig().Backends.PreferenceOrder}.PreferenceTiers()
}

func backendPreferenceTiersWithConfig(config updevConfig) []backendPreferenceTier {
	return backend.PreferenceTiersWithOrder(config.Backends.PreferenceOrder)
}

func backendPreferenceTierFor(provider string, name string) backendPreferenceTier {
	return backend.Registry{PreferenceOrder: loadUpdevConfig().Backends.PreferenceOrder}.PreferenceTierFor(provider, name)
}

func backendPreferenceTierFromLabel(label string) backendPreferenceTier {
	return backend.PreferenceTierFromLabel(label)
}

func backendSourceNamePrefix(name string) string {
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(prefix))
}

func printBackendPlanText(w io.Writer, report backendPlanReport, color bool) {
	title := "updev backends " + report.Command
	fmt.Fprintf(w, "%s %s\n", textui.Style(title, "\033[1m", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", tr("root:", "root:"), report.Root)
	fmt.Fprintf(w, "%s %d\n", tr("findings:", "検出:"), len(report.Findings))
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "\n"+tr("warnings", "警告"))
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "\n"+tr("no backend convergence findings", "backend 整理の検出はありません"))
		return
	}
	printBackendPreferenceOrder(w)
	rows := make([][]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		action := backendFindingActionText(finding)
		if len(finding.CurrentOS) > 0 {
			action += "; os=" + strings.Join(finding.CurrentOS, ",")
		}
		if finding.ReleaseAssetStatus != "" {
			action += "; " + tr("assets=", "asset=") + finding.ReleaseAssetStatus
		}
		rows = append(rows, []string{
			finding.Type,
			finding.Name,
			finding.RecommendedName,
			finding.Confidence + "/" + finding.CommandStatus,
			action,
		})
	}
	fmt.Fprintln(w, "\n"+tr("findings", "検出"))
	textui.PrintTable(w, []textui.Column{
		{Header: tr("type", "種別"), Min: 18, Max: 24},
		{Header: tr("current", "現在"), Min: 18, Max: 30},
		{Header: tr("target", "候補"), Min: 18, Max: 30},
		{Header: tr("confidence", "確度"), Min: 10, Max: 18},
		{Header: tr("action", "対応"), Min: 24, Max: 56},
	}, rows, color)
	fmt.Fprintln(w, "\n"+tr("next", "次"))
	fmt.Fprintln(w, "  "+tr("review candidates manually; interactive updev can apply safe mise backend rewrites and covered old-entry removals after confirmation", "候補を手動確認します。interactive updev では安全な mise backend rewrite とカバー済み old-entry 削除だけ確認後に適用できます"))
	fmt.Fprintln(w, "  "+tr("Brewfile ownership removal is available only when mise already owns the tool; missing mise entries remain review-only", "Brewfile ownership 削除は mise が既に tool を所有している場合だけ可能です。mise entry が無いものは review-only です"))
}

func backendFindingActionText(finding backendFinding) string {
	if defaultLanguage() != "ja" {
		return finding.Action
	}
	switch finding.Type {
	case "homebrew-to-mise-candidate":
		return fmt.Sprintf("%s は候補としてのみ確認します。Homebrew 管理を変える前に release asset、version 対応、配布元の正当性を確認してください", finding.RecommendedName)
	case "mise-backend-candidate":
		sourceBackend := backendSourceNamePrefix(finding.Name)
		switch finding.ReleaseAssetStatus {
		case "compatible":
			return fmt.Sprintf("%s は候補として確認します。release asset は %s に合いそうですが、%s を置き換える前に version 対応と公式配布元を確認してください", finding.RecommendedName, finding.CurrentPlatform, finding.Name)
		case "no-assets", "no-match":
			if sourceBackend == "cargo" {
				return fmt.Sprintf("%s は local cargo build として維持します。%s の %s 向け GitHub release asset が確認できるまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
			}
			if sourceBackend == "npm" {
				return fmt.Sprintf("%s は維持します。%s の %s 向け GitHub release asset と npm が公式配布元ではないことを確認するまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
			}
			return fmt.Sprintf("%s は維持します。%s の %s 向け GitHub release asset が確認できるまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
		default:
			return fmt.Sprintf("%s は候補としてのみ確認します。%s を置き換える前に release asset、version 対応、公式配布元を確認してください", finding.RecommendedName, finding.Name)
		}
	case "homebrew-to-mise":
		if finding.RecommendedSpec != "" {
			return fmt.Sprintf("%s を Brewfile に残す理由を確認してください。bootstrap や cask 依存に Homebrew が必要なら維持します", finding.Name)
		}
		return fmt.Sprintf("Brewfile からの削除を検討する前に %s を mise に追加できるか確認してください", finding.RecommendedName)
	case "mise-backend-rewrite":
		if finding.RecommendedSpec != "" {
			if backendFindingCanRemoveCurrentMiseTool(finding) {
				return fmt.Sprintf("優先 entry %s は %s をカバー済みです。確認後に詳細から古い key を削除できます", finding.RecommendedName, finding.Name)
			}
			return fmt.Sprintf("優先 entry %s は既にあります。%s を削除または狭める前に OS 条件を維持してください", finding.RecommendedName, finding.Name)
		}
		if len(finding.CurrentOS) > 0 {
			return fmt.Sprintf("%s を %s に置き換える前に OS 固有条件を確認し、現在の os list を優先 entry にコピーしてください", finding.Name, finding.RecommendedName)
		}
		return fmt.Sprintf("%s から %s への置き換えを確認してください", finding.Name, finding.RecommendedName)
	default:
		return finding.Action
	}
}

func backendFindingEvidenceText(finding backendFinding) string {
	if defaultLanguage() != "ja" {
		return strings.TrimSpace(firstNonEmpty(finding.Reason, finding.Action, finding.Type))
	}
	if strings.TrimSpace(finding.Action) != "" {
		return backendFindingActionText(finding)
	}
	if reason := localizedBackendReasonText(finding.Reason); reason != "" && reason != finding.Reason {
		return reason
	}
	return localizedBackendReasonText(finding.Type)
}

func localizedBackendReasonText(value string) string {
	value = strings.TrimSpace(value)
	if defaultLanguage() != "ja" {
		return value
	}
	switch value {
	case "stable mise core tool is preferred for CLI developer tools":
		return "CLI 開発ツールは安定した mise core tool を優先します"
	case "fd has a registry-backed mise/aqua path":
		return "fd は registry-backed の mise/aqua 経路があります"
	case "ripgrep is already a stable mise-managed CLI":
		return "ripgrep は既に安定した mise 管理 CLI として扱えます"
	case "aqua prebuilt CLI is preferred over a cargo global build":
		return "cargo global build より aqua の prebuilt CLI を優先します"
	case "mise GitHub release backend is preferred over a cargo global build when no core or aqua backend is defined":
		return "core/aqua backend が無い場合は cargo global build より mise GitHub release backend を優先します"
	case "aqua prebuilt CLI avoids npm global package-manager coupling":
		return "aqua の prebuilt CLI は npm global package-manager への結合を避けられます"
	case "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew":
		return "Homebrew formula の upstream は GitHub repository です。Homebrew から移す前に release asset と ownership を確認してください"
	case "homebrew-to-mise-candidate":
		return "Homebrew から mise への移行候補"
	case "homebrew-to-mise":
		return "Homebrew から mise への移行確認"
	case "mise-backend-candidate":
		return "mise backend の移行候補"
	case "mise-backend-rewrite":
		return "mise backend の書き換え確認"
	default:
		return value
	}
}

func printBackendPreferenceOrder(w io.Writer) {
	parts := make([]string, 0, len(backendPreferenceTiers()))
	for _, tier := range backendPreferenceTiers() {
		parts = append(parts, fmt.Sprintf("%d:%s", tier.Rank, tier.Label))
	}
	fmt.Fprintln(w, "\n"+tr("preference order", "優先順"))
	fmt.Fprintf(w, "  %s\n", strings.Join(parts, " > "))
}

func backendDetailRows(report backendPlanReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Findings))
	for _, finding := range report.Findings {
		rows = append(rows, backendFindingDetailRow(finding))
	}
	return rows
}

func backendToolSections(report backendPlanReport) []toolSection {
	sections := []toolSection{}
	indexByName := map[string]int{}
	for _, finding := range report.Findings {
		kind := firstNonEmpty(finding.Type, "backend")
		name := "backend/" + kind
		sectionIndex, ok := indexByName[name]
		if !ok {
			sectionIndex = len(sections)
			indexByName[name] = sectionIndex
			sections = append(sections, toolSection{Name: name, Title: "backend / " + kind})
		}
		sections[sectionIndex].Rows = append(sections[sectionIndex].Rows, reviewui.DetailRowToRow(backendFindingDetailRow(finding)))
	}
	return sections
}

func backendFindingDetailRow(finding backendFinding) detailBrowserRow {
	metadata := []string{
		tr("type: ", "種別: ") + finding.Type,
		tr("provider: ", "provider: ") + finding.Provider,
		tr("kind: ", "kind: ") + finding.Kind,
		tr("current: ", "現在: ") + finding.Current,
		tr("target: ", "候補: ") + finding.RecommendedProvider + "/" + finding.RecommendedName,
		tr("recommendation kind: ", "判定種別: ") + finding.RecommendationKind,
		fmt.Sprintf(tr("preference: rank %d %s", "優先度: rank %d %s"), finding.PreferenceRank, finding.RecommendedTier),
		tr("confidence: ", "確度: ") + finding.Confidence,
		tr("command status: ", "コマンド状態: ") + finding.CommandStatus,
	}
	if len(finding.CommandNames) > 0 {
		metadata = append(metadata, tr("commands: ", "コマンド: ")+strings.Join(finding.CommandNames, ", "))
	}
	if finding.CurrentSpec != "" {
		metadata = append(metadata, tr("current spec: ", "現在の spec: ")+finding.CurrentSpec)
	}
	if finding.RecommendedSpec != "" {
		metadata = append(metadata, tr("recommended spec: ", "候補 spec: ")+finding.RecommendedSpec)
	}
	if len(finding.CurrentOS) > 0 {
		metadata = append(metadata, tr("current os: ", "現在の OS 条件: ")+strings.Join(finding.CurrentOS, ", "))
	}
	if len(finding.RecommendedOS) > 0 {
		metadata = append(metadata, tr("recommended os: ", "候補 OS 条件: ")+strings.Join(finding.RecommendedOS, ", "))
	}
	if finding.CurrentPlatform != "" {
		metadata = append(metadata, tr("current platform: ", "現在の platform: ")+finding.CurrentPlatform)
	}
	if finding.ReleaseAssetStatus != "" {
		metadata = append(metadata, tr("release assets: ", "release asset: ")+finding.ReleaseAssetStatus)
	}
	if len(finding.ReleaseAssetMatches) > 0 {
		metadata = append(metadata, tr("matching release assets: ", "一致した release asset: ")+strings.Join(finding.ReleaseAssetMatches, ", "))
	}
	if len(finding.ReleaseAssets) > 0 {
		metadata = append(metadata, tr("release asset sample: ", "release asset サンプル: ")+strings.Join(finding.ReleaseAssets, ", "))
	}
	for _, evidence := range finding.SourceEvidence {
		metadata = append(metadata, tr("source evidence: ", "根拠: ")+evidence)
	}
	metadata = append(metadata, tr("applyability: ", "適用可否: ")+backendFindingApplyability(finding))
	return detailBrowserRow{
		Title:    finding.Name + " -> " + finding.RecommendedName,
		Status:   string(finding.Status),
		Summary:  localizedBackendReasonText(finding.Reason),
		Detail:   backendFindingActionText(finding),
		Metadata: metadata,
		Actions:  backendDetailActions(finding),
	}
}

func backendPlanActionableCount(report backendPlanReport) int {
	count := 0
	for _, finding := range report.Findings {
		if len(backendDetailActions(finding)) > 0 {
			count++
		}
	}
	return count
}

func backendDetailActions(finding backendFinding) []detailBrowserAction {
	if finding.Type == "homebrew-to-mise" && finding.RecommendationKind == "recommendation" && finding.RecommendedSpec != "" {
		if finding.Kind == "" || finding.Name == "" {
			return nil
		}
		return []detailBrowserAction{{
			Value:       backendDetailActionValue("remove-brew", finding.Kind+":"+finding.Name, finding.RecommendedName),
			Label:       tr("remove Brewfile entry", "Brewfile entry を削除する"),
			Description: tr("remove the Homebrew desired-state entry because mise already owns the tool", "mise が既に所有しているため Homebrew desired-state entry を削除します"),
		}}
	}
	if finding.Type != "mise-backend-rewrite" || !finding.RewriteAllowed {
		return nil
	}
	if finding.Name == "" || finding.RecommendedName == "" {
		return nil
	}
	if finding.RecommendedSpec != "" {
		if !backendFindingCanRemoveCurrentMiseTool(finding) {
			return nil
		}
		return []detailBrowserAction{{
			Value:       backendDetailActionValue("remove-mise", finding.Name, finding.RecommendedName),
			Label:       tr("remove old mise backend", "古い mise backend を削除する"),
			Description: tr("remove the current backend because the preferred entry already covers it", "優先 entry が現在 entry をカバーしているため、現在の backend entry を削除します"),
		}}
	}
	return []detailBrowserAction{{
		Value:       backendDetailActionValue("rewrite-mise", finding.Name, finding.RecommendedName),
		Label:       tr("rewrite mise backend", "mise backend を書き換える"),
		Description: tr("rename the mise tool key while preserving the current spec", "現在の spec を維持したまま mise tool key を rename します"),
	}}
}

func backendFindingCanRemoveCurrentMiseTool(finding backendFinding) bool {
	if finding.RecommendedSpec == "" {
		return false
	}
	return backendOSSelectorsCovered(finding.CurrentOS, finding.RecommendedOS)
}

func backendOSSelectorsCovered(current []string, recommended []string) bool {
	currentOS := normalizedBackendOSSet(current)
	recommendedOS := normalizedBackendOSSet(recommended)
	if len(currentOS) == 0 {
		return len(recommendedOS) == 0
	}
	if len(recommendedOS) == 0 {
		return true
	}
	for osName := range currentOS {
		if !recommendedOS[osName] {
			return false
		}
	}
	return true
}

func normalizedBackendOSSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}

func backendFindingApplyability(finding backendFinding) string {
	switch {
	case finding.Type == "mise-backend-candidate":
		return tr("review-only candidate; no write action until release assets, version mapping, and official distribution are verified", "review-only 候補。release asset、version 対応、公式配布元を確認するまで write action はありません")
	case finding.Type == "homebrew-to-mise-candidate":
		return tr("review-only candidate; Brewfile migration remains preview-only", "review-only 候補。Brewfile 移行は preview のみです")
	case finding.Type == "homebrew-to-mise":
		if finding.RecommendedSpec != "" {
			return tr("applyable: remove the Homebrew desired-state entry because mise already owns the tool", "適用可能: mise が既に所有しているため Homebrew desired-state entry を削除します")
		}
		return tr("review-only; add or verify the mise entry before removing Brewfile ownership", "review-only。Brewfile ownership を外す前に mise entry を追加または確認してください")
	case finding.Type == "mise-backend-rewrite" && !finding.RewriteAllowed:
		return tr("review-only; rewrite is not allowed for this finding", "review-only。この検出では rewrite は許可されていません")
	case finding.Type == "mise-backend-rewrite" && finding.RecommendedSpec == "":
		return tr("applyable: rewrite current mise key to the preferred backend", "適用可能: 現在の mise key を優先 backend に書き換えます")
	case finding.Type == "mise-backend-rewrite" && backendFindingCanRemoveCurrentMiseTool(finding):
		return tr("applyable: remove current mise key because the preferred entry already covers it", "適用可能: 優先 entry がカバー済みのため現在の mise key を削除します")
	case finding.Type == "mise-backend-rewrite":
		return tr("review-only; preserve OS conditions before removing or narrowing the current entry", "review-only。現在 entry を削除または狭める前に OS 条件を維持してください")
	default:
		return tr("review-only", "review-only")
	}
}

func backendDetailActionValue(action string, current string, recommended string) string {
	return backendDetailActionPrefix + "\t" + action + "\t" + current + "\t" + recommended
}

func parseBackendDetailAction(value string) (string, string, string, bool) {
	parts := strings.SplitN(value, "\t", 4)
	if len(parts) != 4 || parts[0] != backendDetailActionPrefix {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}
