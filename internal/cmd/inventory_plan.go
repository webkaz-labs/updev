package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/manualinventory"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/support"
	"github.com/webkaz-labs/updev/internal/textui"
)

const manualPlanDetailActionPrefix = "manual-plan"

type inventoryPlanOptions struct {
	action   string
	format   string
	limit    int
	provider string
	query    string
	root     string
}

type inventoryPlanReport struct {
	SchemaVersion    int                     `json:"schema_version"`
	Status           plan.Status             `json:"status"`
	Root             string                  `json:"root"`
	Provider         string                  `json:"provider"`
	Summary          plan.ProviderSummary    `json:"summary"`
	ActionCounts     map[string]int          `json:"action_counts"`
	AttentionCount   int                     `json:"attention_count"`
	Items            []manualPlanItem        `json:"items"`
	ReviewCandidates []manualReviewCandidate `json:"review_candidates,omitempty"`
	NextSteps        []string                `json:"next_steps,omitempty"`
	Limit            int                     `json:"-"`
}

type manualPlanItem struct {
	Provider          string                      `json:"provider"`
	Kind              string                      `json:"kind"`
	Name              string                      `json:"name"`
	Action            string                      `json:"action"`
	State             string                      `json:"state"`
	SuggestedProvider string                      `json:"suggested_provider,omitempty"`
	Confidence        string                      `json:"confidence,omitempty"`
	ReasonCode        string                      `json:"reason_code,omitempty"`
	RemediationCode   string                      `json:"remediation_code,omitempty"`
	Detail            string                      `json:"detail,omitempty"`
	NextStep          string                      `json:"next_step,omitempty"`
	ReviewURL         string                      `json:"review_url,omitempty"`
	InstallHint       string                      `json:"install_hint,omitempty"`
	CommandPreview    []string                    `json:"command_preview,omitempty"`
	Evidence          []manualReviewEvidence      `json:"evidence,omitempty"`
	SuggestedOverride *manualReviewOverrideFields `json:"suggested_override,omitempty"`
}

func parseInventoryPlanOptions(args []string) (inventoryPlanOptions, error) {
	opts := inventoryPlanOptions{format: "text", limit: 24, provider: manualProviderName, root: defaultRoot()}
	fs := flag.NewFlagSet("inventory plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.action, "action", opts.action, "plan action filter")
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.IntVar(&opts.limit, "limit", opts.limit, "maximum text rows; 0 means unlimited")
	fs.StringVar(&opts.provider, "provider", opts.provider, "inventory provider to plan")
	fs.StringVar(&opts.query, "query", opts.query, "case-insensitive name/detail filter")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.provider == "" {
		return opts, fmt.Errorf("--provider requires a value")
	}
	if !strings.EqualFold(opts.provider, manualProviderName) {
		return opts, fmt.Errorf("unsupported inventory plan provider: %s", opts.provider)
	}
	opts.provider = manualProviderName
	opts.action = strings.TrimSpace(strings.ToLower(opts.action))
	opts.query = strings.TrimSpace(opts.query)
	return opts, nil
}

func runInventoryPlan(opts inventoryPlanOptions) int {
	report := buildInventoryPlanReport(opts)
	if opts.format == "json" {
		code := encodeJSON(report)
		if code != 0 {
			return code
		}
		return inventoryPlanExitCode(report)
	}
	printInventoryPlanText(os.Stdout, report)
	return inventoryPlanExitCode(report)
}

func buildInventoryPlanReport(opts inventoryPlanOptions) inventoryPlanReport {
	sections := manualAppSectionsForInventoryCommand(opts.root)
	items := manualPlanItems(sections)
	items = filterManualPlanItems(items, opts)
	candidates := filterManualReviewCandidatesForPlan(manualReviewCandidates(sections), items)
	status := plan.StatusOK
	for _, item := range items {
		if manualinventory.PlanActionNeedsReview(item.Action) {
			status = plan.StatusDrift
			break
		}
	}
	return inventoryPlanReport{
		SchemaVersion:    1,
		Status:           status,
		Root:             opts.root,
		Provider:         manualProviderName,
		Summary:          manualProviderSummary(sections),
		ActionCounts:     manualPlanActionCounts(items),
		AttentionCount:   manualPlanAttentionCount(items),
		Items:            items,
		ReviewCandidates: candidates,
		NextSteps:        manualPlanNextSteps(items),
		Limit:            opts.limit,
	}
}

func inventoryPlanExitCode(report inventoryPlanReport) int {
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func manualPlanItems(sections []toolSection) []manualPlanItem {
	items := []manualPlanItem{}
	for _, section := range sections {
		for _, row := range section.Rows {
			items = append(items, manualPlanItemFromRow(section, row))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftPriority := manualPlanActionPriority(items[i].Action)
		rightPriority := manualPlanActionPriority(items[j].Action)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func manualPlanActionPriority(action string) int {
	switch action {
	case "needs-review":
		return 0
	case "ignore-local":
		return 1
	case "adopt-brew":
		return 2
	case "adopt-mas":
		return 3
	case "open-vendor":
		return 4
	case "keep-manual":
		return 5
	default:
		return 9
	}
}

func manualPlanItemFromRow(section toolSection, row toolRow) manualPlanItem {
	manualRow := manualReviewRow(section.Name, row)
	evidence := manualinventory.EvidenceFromRow(manualRow)
	action := manualinventory.PlanAction(manualRow)
	item := manualPlanItem{
		Provider:          manualProviderName,
		Kind:              "app",
		Name:              row.Name,
		Action:            action,
		State:             row.State,
		SuggestedProvider: manualinventory.PlanSuggestedProvider(action, manualRow),
		Confidence:        manualinventory.PlanConfidence(action, manualRow),
		ReasonCode:        manualinventory.PlanReasonCode(action),
		RemediationCode:   manualinventory.PlanRemediationCode(action),
		Detail:            row.Detail,
		NextStep:          manualinventory.PlanNextStep(action, manualRow),
		ReviewURL:         manualinventory.PlanReviewURL(manualRow),
		InstallHint:       manualinventory.PlanInstallHint(action, manualRow),
		CommandPreview:    manualinventory.PlanCommandPreview(action, manualRow),
	}
	if !manualReviewEvidenceEmpty(evidence) {
		item.Evidence = []manualReviewEvidence{evidence}
	}
	if manualinventory.PlanActionNeedsReview(action) {
		override := manualinventory.PlanSuggestedOverride(action, manualRow)
		item.SuggestedOverride = &override
	}
	return item
}

func filterManualPlanItems(items []manualPlanItem, opts inventoryPlanOptions) []manualPlanItem {
	out := make([]manualPlanItem, 0, len(items))
	for _, item := range items {
		if opts.action != "" && item.Action != opts.action {
			continue
		}
		if opts.query != "" && !manualPlanItemMatches(item, opts.query) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterManualReviewCandidatesForPlan(candidates []manualReviewCandidate, items []manualPlanItem) []manualReviewCandidate {
	if len(candidates) == 0 || len(items) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, item := range items {
		names[strings.ToLower(item.Name)] = true
	}
	out := make([]manualReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if names[strings.ToLower(candidate.Name)] {
			out = append(out, candidate)
		}
	}
	return out
}

func manualPlanItemMatches(item manualPlanItem, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		item.Name,
		item.Action,
		item.State,
		item.SuggestedProvider,
		item.Detail,
		item.NextStep,
		item.ReviewURL,
		item.InstallHint,
		item.ReasonCode,
	}, " "))
	return strings.Contains(haystack, query)
}

func manualPlanActionCounts(items []manualPlanItem) map[string]int {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Action]++
	}
	return counts
}

func manualPlanAttentionCount(items []manualPlanItem) int {
	count := 0
	for _, item := range items {
		if manualinventory.PlanActionNeedsReview(item.Action) {
			count++
		}
	}
	return count
}

func manualPlanNextSteps(items []manualPlanItem) []string {
	counts := manualPlanActionCounts(items)
	steps := []string{}
	if counts["needs-review"] > 0 {
		steps = append(steps, fmt.Sprintf("review %d live-only manual apps with `updev inventory review --provider manual`", counts["needs-review"]))
	}
	if counts["adopt-brew"] > 0 {
		steps = append(steps, fmt.Sprintf("review %d apps that appear adoptable by Homebrew cask before changing desired state", counts["adopt-brew"]))
	}
	if counts["adopt-mas"] > 0 {
		steps = append(steps, fmt.Sprintf("review %d apps that appear managed by the Mac App Store before adding overrides", counts["adopt-mas"]))
	}
	if counts["ignore-local"] > 0 {
		steps = append(steps, fmt.Sprintf("review %d user-local apps before ignoring or documenting them", counts["ignore-local"]))
	}
	if counts["open-vendor"] > 0 {
		steps = append(steps, fmt.Sprintf("review %d vendor-managed apps manually before adding overrides", counts["open-vendor"]))
	}
	return steps
}

func printInventoryPlanText(w io.Writer, report inventoryPlanReport) {
	color := textui.ColorEnabled()
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("inventory plan", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("provider:", "provider:"), color), report.Provider)
	fmt.Fprintf(w, "%s desired=%d live=%d attention=%d review=%d\n", textui.StyleLabel(tr("summary:", "サマリー:"), color), report.Summary.Desired, report.Summary.Live, report.AttentionCount, len(report.ReviewCandidates))
	if len(report.ActionCounts) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("actions:", "actions:"), color), manualPlanActionSummary(report.ActionCounts))
	}
	if len(report.NextSteps) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, textui.StyleHeading(tr("next steps", "次の確認"), color))
		for _, step := range manualPlanDisplayNextSteps(report.ActionCounts) {
			fmt.Fprintf(w, "  %s\n", textui.StyleDim(step, color))
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, textui.StyleHeading(tr("manual inventory plan", "manual inventory plan"), color))
	if len(report.Items) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no rows", "行はありません"), color))
		return
	}
	items := report.Items
	if report.Limit > 0 && len(items) > report.Limit {
		items = items[:report.Limit]
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			textui.StyleName(item.Name, color),
			textui.StyleStatus(item.Action, color),
			textui.StyleStatus(item.State, color),
			textui.StyleStatus(item.Confidence, color),
			manualPlanDisplayNextStep(item),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: tr("name", "名前"), Min: 12, Max: 28},
		{Header: "action", Min: 11, Max: 13},
		{Header: tr("state", "状態"), Min: 8, Max: 10},
		{Header: "conf", Min: 6, Max: 7},
		{Header: tr("next", "次"), Min: 20, Max: 72},
	}, rows, color)
	if report.Limit > 0 && len(report.Items) > report.Limit {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(fmt.Sprintf(tr("showing %d of %d rows; use --action or --limit 0 for more", "%d/%d 行を表示中。--action または --limit 0 で絞り込み/全表示できます"), report.Limit, len(report.Items)), color))
	}
	printManualPlanTextDetails(w, items, color)
}

func printManualPlanTextDetails(w io.Writer, items []manualPlanItem, color bool) {
	detailItems := manualPlanItemsWithTextDetails(items)
	if len(detailItems) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("review details", "確認詳細"), color))
	for _, item := range detailItems {
		fmt.Fprintf(w, "  %s %s\n", textui.StyleName(item.Name, color), textui.StyleDim(item.Action, color))
		if item.ReviewURL != "" {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel("url:", color), item.ReviewURL)
		} else if item.Action == "open-vendor" {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel("url:", color), textui.StyleDim(tr("missing; add source_url/review_url to a structured manual source or keep as evidence gap", "未設定。structured manual source に source_url/review_url を追加するか evidence gap として残します"), color))
		}
		if item.InstallHint != "" {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel("hint:", color), item.InstallHint)
		}
		for _, command := range item.CommandPreview {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel("cmd:", color), command)
		}
	}
}

func manualPlanItemsWithTextDetails(items []manualPlanItem) []manualPlanItem {
	out := []manualPlanItem{}
	for _, item := range items {
		if item.ReviewURL != "" || item.InstallHint != "" || len(item.CommandPreview) > 0 || item.Action == "open-vendor" {
			out = append(out, item)
		}
	}
	if len(out) > 8 {
		return out[:8]
	}
	return out
}

func manualPlanDetailRows(report inventoryPlanReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Items))
	if manualPlanCanBatchEnrich(report) {
		rows = append(rows, detailBrowserRow{
			Title:   tr("batch draft enrichment", "batch draft 生成"),
			Status:  "draft",
			Summary: tr("generate draft metadata for the current manual review queue", "現在の manual review queue から draft metadata を生成します"),
			Detail:  tr("Runs the configured manual inventory agent for up to the built-in batch limit. Every generated entry stays draft until accepted or edited.", "設定済み manual inventory agent を built-in batch limit まで実行します。生成された entry は accepted/edit されるまで draft のままです。"),
			Metadata: []string{
				fmt.Sprintf("candidates: %d", len(report.ReviewCandidates)),
				"action: enrich-batch",
			},
			Actions: []detailBrowserAction{{
				Value:       manualPlanDetailActionValue("enrich-batch", "*"),
				Label:       tr("generate batch drafts", "batch draft を生成"),
				Description: tr("run configured agent for a bounded batch of manual review candidates", "manual review candidates の bounded batch に設定済み agent を実行します"),
			}},
		})
	}
	for _, item := range report.Items {
		rows = append(rows, manualPlanDetailRow(item, report.Root))
	}
	return rows
}

func manualPlanCanBatchEnrich(report inventoryPlanReport) bool {
	if len(report.ReviewCandidates) == 0 {
		return false
	}
	config := loadUpdevConfig()
	return config.Inventory.Agent.Enabled != nil && *config.Inventory.Agent.Enabled &&
		config.Inventory.Agent.Batch != nil && *config.Inventory.Agent.Batch &&
		len(config.Inventory.Agent.Command) > 0 &&
		manualAgentDraftSourcePath(report.Root) != ""
}

func manualPlanToolSections(report inventoryPlanReport) []toolSection {
	return reviewui.DetailRowsToSections(manualPlanDetailRows(report), func(row detailBrowserRow) (string, string) {
		status := firstNonEmpty(row.Status, "review")
		return "manual/" + status, "manual / " + status
	})
}

func manualPlanDetailRow(item manualPlanItem, root string) detailBrowserRow {
	metadata := []string{
		"provider: " + item.Provider,
		"suggested_provider: " + firstNonEmpty(item.SuggestedProvider, "-"),
		"confidence: " + firstNonEmpty(item.Confidence, "-"),
		"reason: " + firstNonEmpty(item.ReasonCode, "-"),
		"remediation: " + firstNonEmpty(item.RemediationCode, "-"),
	}
	if item.ReviewURL != "" {
		metadata = append(metadata, "url: "+item.ReviewURL)
	}
	if item.InstallHint != "" {
		metadata = append(metadata, "hint: "+item.InstallHint)
	}
	for _, command := range item.CommandPreview {
		metadata = append(metadata, "cmd: "+command)
	}
	for _, evidence := range item.Evidence {
		metadata = append(metadata, manualPlanEvidenceDetail(evidence))
	}
	if item.SuggestedOverride != nil {
		metadata = append(metadata, "suggested_override: managed_by="+firstNonEmpty(item.SuggestedOverride.ManagedBy, "-")+" lifecycle="+firstNonEmpty(item.SuggestedOverride.Lifecycle, "-"))
		if len(item.SuggestedOverride.Aliases) > 0 {
			metadata = append(metadata, "aliases: "+strings.Join(item.SuggestedOverride.Aliases, ", "))
		}
	}
	return detailBrowserRow{
		Title:    item.Name,
		Status:   item.Action,
		Summary:  manualPlanDisplayNextStep(item),
		Detail:   item.Detail,
		Metadata: metadata,
		Actions:  manualPlanDetailActions(item, root),
	}
}

func manualPlanEvidenceDetail(evidence manualReviewEvidence) string {
	parts := []string{"evidence"}
	if evidence.Scanner != "" {
		parts = append(parts, "scanner="+evidence.Scanner)
	}
	if evidence.Source != "" {
		parts = append(parts, "source="+evidence.Source)
	}
	if sourceName, label := manualInventorySourceSupportLabel(evidence); label != "" {
		parts = append(parts, "inventory_source="+sourceName)
		parts = append(parts, "support_label="+label)
	}
	if evidence.Path != "" {
		parts = append(parts, "path="+evidence.Path)
	}
	if evidence.ReviewURL != "" {
		parts = append(parts, "review_url="+evidence.ReviewURL)
	}
	if evidence.SourceURL != "" {
		parts = append(parts, "source_url="+evidence.SourceURL)
	}
	if evidence.Owner != "" {
		parts = append(parts, "owner="+evidence.Owner)
	}
	if evidence.ManagedBy != "" {
		parts = append(parts, "managed_by="+evidence.ManagedBy)
	}
	if evidence.UpdateOwner != "" {
		parts = append(parts, "update_owner="+evidence.UpdateOwner)
	}
	if evidence.OwnershipConfidence != "" {
		parts = append(parts, "ownership_confidence="+evidence.OwnershipConfidence)
	}
	if evidence.ProviderMetadata != "" {
		parts = append(parts, "provider_metadata="+evidence.ProviderMetadata)
	}
	if evidence.BundleID != "" {
		parts = append(parts, "bundle_id="+evidence.BundleID)
	}
	if evidence.MASID != "" {
		parts = append(parts, "mas_id="+evidence.MASID)
	}
	if evidence.Version != "" {
		parts = append(parts, "version="+evidence.Version)
	}
	return strings.Join(parts, " ")
}

func manualInventorySourceSupportLabel(evidence manualReviewEvidence) (string, string) {
	source := strings.ToLower(strings.TrimSpace(evidence.Source))
	scanner := strings.ToLower(strings.TrimSpace(evidence.Scanner))
	sourceName := ""
	switch {
	case strings.Contains(source, "mac app store"), strings.Contains(source, "mas list"):
		sourceName = "mac-app-store"
	case strings.Contains(source, "homebrew cask"):
		sourceName = "homebrew-cask"
	case strings.Contains(source, "homebrew tap docs"):
		sourceName = "manual-markdown"
	case strings.Contains(source, "app bundle"), scanner == "macos_app_bundle":
		sourceName = "macos-app-bundle"
	}
	if sourceName == "" {
		return "", ""
	}
	for _, entry := range support.Catalog() {
		if entry.Surface == "inventory_source" && entry.Name == sourceName {
			return sourceName, entry.Label
		}
	}
	return sourceName, ""
}

func manualReviewEvidenceEmpty(evidence manualReviewEvidence) bool {
	return evidence.Scanner == "" &&
		evidence.Source == "" &&
		evidence.Path == "" &&
		evidence.ReviewURL == "" &&
		evidence.SourceURL == "" &&
		evidence.Owner == "" &&
		evidence.ManagedBy == "" &&
		evidence.UpdateOwner == "" &&
		evidence.OwnershipConfidence == "" &&
		evidence.ProviderMetadata == "" &&
		evidence.MASID == "" &&
		evidence.BundleID == "" &&
		evidence.Version == ""
}

func manualPlanDetailActions(item manualPlanItem, root string) []detailBrowserAction {
	actions := []detailBrowserAction{}
	if item.State == "installed" && manualinventory.PlanActionNeedsReview(item.Action) {
		actions = append(actions,
			detailBrowserAction{Value: manualPlanDetailActionValue("accept", item.Name), Label: tr("accept override", "override を採用"), Description: tr("append the suggested manual inventory override", "提案された manual inventory override を追記します")},
			detailBrowserAction{Value: manualPlanDetailActionValue("ignore", item.Name), Label: tr("ignore local app", "local app として無視"), Description: tr("append a local-only ignore override", "local-only の ignore override を追記します")},
			detailBrowserAction{Value: manualPlanDetailActionValue("edit", item.Name), Label: tr("edit override", "override を編集"), Description: tr("open the suggested override before writing", "書き込む前に提案 override を editor で開きます")},
		)
		if manualAgentEnrichmentAvailable(root) {
			actions = append(actions, detailBrowserAction{Value: manualPlanDetailActionValue("enrich", item.Name), Label: tr("generate draft", "draft を生成"), Description: tr("run the configured agent and append draft manual metadata", "設定済み agent を実行して draft manual metadata を追記します")})
		}
	}
	if item.Action == "adopt-brew" {
		if cask := manualinventory.DetailValue(item.Detail, "cask"); cask != "" {
			actions = append(actions, detailBrowserAction{Value: manualPlanDetailActionValue("review-cask", cask), Label: tr("review cask", "cask を確認"), Description: tr("run brew info --cask for ownership evidence", "ownership evidence として brew info --cask を実行します")})
		}
	}
	if item.Action == "adopt-mas" {
		if masID := manualinventory.DetailValue(item.Detail, "mas_id"); masID != "" {
			actions = append(actions, detailBrowserAction{Value: manualPlanDetailActionValue("review-mas", masID), Label: tr("review App Store", "App Store を確認"), Description: tr("run mas info when mas is available", "mas が使える場合は mas info を実行します")})
		}
	}
	if item.Action == "open-vendor" {
		if url := item.ReviewURL; url != "" {
			actions = append(actions, detailBrowserAction{Value: manualPlanDetailActionValue("open-vendor", url), Label: tr("open vendor", "vendor を開く"), Description: tr("open the vendor source URL", "vendor source URL を開きます")})
		}
	}
	return actions
}

func manualPlanDetailActionValue(action string, value string) string {
	return manualPlanDetailActionPrefix + "\t" + action + "\t" + value
}

func parseManualPlanDetailAction(value string) (string, string, bool) {
	parts := strings.SplitN(value, "\t", 3)
	if len(parts) != 3 || parts[0] != manualPlanDetailActionPrefix || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func manualPlanActionSummary(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func manualPlanDisplayNextSteps(counts map[string]int) []string {
	steps := []string{}
	if counts["needs-review"] > 0 {
		steps = append(steps, fmt.Sprintf(tr("review %d live-only manual apps with `updev inventory review --provider manual`", "%d 件の live-only 手動アプリを `updev inventory review --provider manual` で確認"), counts["needs-review"]))
	}
	if counts["adopt-brew"] > 0 {
		steps = append(steps, fmt.Sprintf(tr("review %d apps that appear adoptable by Homebrew cask before changing desired state", "%d 件の Homebrew cask 採用候補を desired state 変更前に確認"), counts["adopt-brew"]))
	}
	if counts["adopt-mas"] > 0 {
		steps = append(steps, fmt.Sprintf(tr("review %d apps that appear managed by the Mac App Store before adding overrides", "%d 件の Mac App Store 管理候補を override 追加前に確認"), counts["adopt-mas"]))
	}
	if counts["ignore-local"] > 0 {
		steps = append(steps, fmt.Sprintf(tr("review %d user-local apps before ignoring or documenting them", "%d 件の user-local app を ignore または記録する前に確認"), counts["ignore-local"]))
	}
	if counts["open-vendor"] > 0 {
		steps = append(steps, fmt.Sprintf(tr("review %d vendor-managed apps manually before adding overrides", "%d 件の vendor 管理アプリを override 追加前に手動確認"), counts["open-vendor"]))
	}
	return steps
}

func manualPlanDisplayNextStep(item manualPlanItem) string {
	switch item.Action {
	case "adopt-brew":
		if cask := manualinventory.DetailValue(item.Detail, "cask"); cask != "" {
			return fmt.Sprintf(tr("review Homebrew cask ownership, then keep desired state with cask %s or add an alias override", "Homebrew cask %s の所有元を確認し、desired state 維持または alias override を追加"), cask)
		}
		return tr("review Homebrew cask ownership, then keep desired state or add an alias override", "Homebrew cask の所有元を確認し、desired state 維持または alias override を追加")
	case "adopt-mas":
		return tr("review Mac App Store ownership, then add an override with managed_by = \"mas\"", "Mac App Store 管理元を確認し、managed_by = \"mas\" の override を追加")
	case "open-vendor":
		return tr("open the vendor source manually, then keep manual management or add an ignore/override decision", "vendor 入手元を手動確認し、manual 維持または ignore/override 判断を追加")
	case "ignore-local":
		return tr("review whether this user-local app should stay local-only or be ignored by manual inventory", "この user-local app を local-only 維持または inventory ignore にするか確認")
	case "needs-review":
		return tr("review ownership and lifecycle, then accept/edit/ignore the suggested inventory override", "所有元と更新方法を確認し、提案 override を accept/edit/ignore する")
	default:
		return tr("keep this manual inventory row as documented", "この manual inventory 行を docs 通り維持")
	}
}
