package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/textui"
)

var manualPlanURLPattern = regexp.MustCompile(`https?://[^)\s;]+`)

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
		if manualPlanActionNeedsReview(item.Action) {
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
	evidence := manualReviewEvidenceFromRow(row)
	action := manualPlanAction(section, row)
	item := manualPlanItem{
		Provider:          manualProviderName,
		Kind:              "app",
		Name:              row.Name,
		Action:            action,
		State:             row.State,
		SuggestedProvider: manualPlanSuggestedProvider(action, row),
		Confidence:        manualPlanConfidence(action, row),
		ReasonCode:        manualPlanReasonCode(action),
		RemediationCode:   manualPlanRemediationCode(action),
		Detail:            row.Detail,
		NextStep:          manualPlanNextStep(action, row),
		ReviewURL:         manualPlanReviewURL(row),
		InstallHint:       manualPlanInstallHint(action, row),
		CommandPreview:    manualPlanCommandPreview(action, row),
	}
	if evidence.Scanner != "" || evidence.Source != "" || evidence.Path != "" || evidence.MASID != "" || evidence.BundleID != "" || evidence.Version != "" {
		item.Evidence = []manualReviewEvidence{evidence}
	}
	if manualPlanActionNeedsReview(action) {
		override := manualPlanSuggestedOverride(action, row)
		item.SuggestedOverride = &override
	}
	return item
}

func manualPlanAction(section toolSection, row toolRow) string {
	if row.State == "brew" || strings.Contains(row.Detail, "source: homebrew cask") {
		return "adopt-brew"
	}
	if manualSuggestedManagedBy(row) == "mas" {
		return "adopt-mas"
	}
	if manualRowIsUserLocal(row) {
		return "ignore-local"
	}
	if section.Name == "manual/installed-apps" || row.State == "installed" {
		return "needs-review"
	}
	if manualRowHasVendorSource(row) {
		return "open-vendor"
	}
	return "keep-manual"
}

func manualPlanSuggestedProvider(action string, row toolRow) string {
	switch action {
	case "adopt-brew":
		return "brew"
	case "adopt-mas":
		return "mas"
	case "open-vendor":
		return "vendor"
	case "ignore-local", "needs-review":
		return manualSuggestedManagedBy(row)
	default:
		return ""
	}
}

func manualRowHasVendorSource(row toolRow) bool {
	detail := strings.ToLower(row.Detail)
	return strings.Contains(detail, "vendor") ||
		strings.Contains(row.Detail, "ベンダー") ||
		strings.Contains(row.Detail, "入手先:") ||
		strings.Contains(detail, "source:")
}

func manualRowIsUserLocal(row toolRow) bool {
	path := manualDetailValue(row.Detail, "path")
	return strings.Contains(path, "/Users/") && strings.Contains(path, "/Applications/")
}

func manualPlanActionNeedsReview(action string) bool {
	switch action {
	case "adopt-brew", "adopt-mas", "ignore-local", "needs-review", "open-vendor":
		return true
	default:
		return false
	}
}

func manualPlanConfidence(action string, row toolRow) string {
	switch action {
	case "adopt-brew", "adopt-mas":
		return "high"
	case "open-vendor":
		return "medium"
	case "ignore-local":
		return "medium"
	case "needs-review":
		return manualReviewConfidence(row)
	default:
		return ""
	}
}

func manualPlanReasonCode(action string) string {
	switch action {
	case "adopt-brew":
		return "manual_app_homebrew_cask_available"
	case "adopt-mas":
		return "manual_app_mas_available"
	case "open-vendor":
		return "manual_app_vendor_review"
	case "ignore-local":
		return "manual_app_user_local"
	case "needs-review":
		return "manual_app_live_only"
	default:
		return ""
	}
}

func manualPlanRemediationCode(action string) string {
	switch action {
	case "adopt-brew", "adopt-mas", "needs-review":
		return "manual_inventory_override"
	case "open-vendor":
		return "manual_vendor_review"
	case "ignore-local":
		return "manual_inventory_ignore"
	default:
		return ""
	}
}

func manualPlanReviewURL(row toolRow) string {
	match := manualPlanURLPattern.FindString(row.Detail)
	return strings.TrimRight(match, ".,")
}

func manualPlanInstallHint(action string, row toolRow) string {
	switch action {
	case "adopt-brew":
		if cask := manualDetailValue(row.Detail, "cask"); cask != "" {
			return "review Homebrew cask metadata before moving ownership to cask " + cask
		}
		return "review Homebrew cask metadata before moving ownership to Homebrew"
	case "adopt-mas":
		if masID := manualDetailValue(row.Detail, "mas_id"); masID != "" {
			return "verify Mac App Store ownership for mas id " + masID + " before adding an override"
		}
		return "verify Mac App Store ownership before adding an override"
	case "open-vendor":
		if url := manualPlanReviewURL(row); url != "" {
			return "open the vendor URL for review only; do not run installer commands from inventory output"
		}
		return "review the vendor source manually; inventory output must not install external packages"
	case "ignore-local":
		return "add a local-only override only after confirming this app is machine-local"
	case "needs-review":
		return "accept, edit, or ignore one explicit override after ownership review"
	default:
		return ""
	}
}

func manualPlanCommandPreview(action string, row toolRow) []string {
	switch action {
	case "adopt-brew":
		if cask := manualDetailValue(row.Detail, "cask"); cask != "" {
			return []string{"brew info --cask " + strconv.Quote(cask)}
		}
	case "adopt-mas":
		if masID := manualDetailValue(row.Detail, "mas_id"); masID != "" {
			return []string{"mas lookup " + strconv.Quote(masID)}
		}
		return []string{"mas search " + strconv.Quote(row.Name)}
	case "open-vendor":
		if url := manualPlanReviewURL(row); url != "" {
			return []string{"open " + strconv.Quote(url)}
		}
	case "ignore-local":
		return []string{"updev inventory review --provider manual --action ignore --query " + strconv.Quote(row.Name)}
	case "needs-review":
		return []string{"updev inventory review --provider manual --query " + strconv.Quote(row.Name)}
	}
	return nil
}

func manualPlanSuggestedOverride(action string, row toolRow) manualReviewOverrideFields {
	override := manualReviewOverrideFields{
		Name:    row.Name,
		Aliases: manualPlanSuggestedAliases(action, row),
		Detail:  manualPlanInstallHint(action, row),
	}
	switch action {
	case "adopt-brew":
		override.ManagedBy = "brew"
	case "adopt-mas":
		override.ManagedBy = "mas"
	case "open-vendor":
		override.ManagedBy = "vendor"
	case "ignore-local":
		override.Category = "Ignored"
		override.Lifecycle = "local-only"
	case "needs-review":
		override.ManagedBy = manualSuggestedManagedBy(row)
		override.Detail = "review installed app ownership and lifecycle"
	}
	return override
}

func manualPlanSuggestedAliases(action string, row toolRow) []string {
	aliases := manualSuggestedAliases(row)
	if action == "adopt-brew" {
		if cask := manualDetailValue(row.Detail, "cask"); cask != "" {
			aliases = append(aliases, cask)
		}
	}
	return aliases
}

func manualPlanNextStep(action string, row toolRow) string {
	switch action {
	case "adopt-brew":
		if cask := manualDetailValue(row.Detail, "cask"); cask != "" {
			return "review Homebrew cask ownership, then keep desired state with cask " + cask + " or add an alias override"
		}
		return "review Homebrew cask ownership, then keep desired state or add an alias override"
	case "adopt-mas":
		return "review Mac App Store ownership, then add an override with managed_by = \"mas\""
	case "open-vendor":
		if url := manualPlanReviewURL(row); url != "" {
			return "open the vendor source " + url + " for review, then keep manual management or add an ignore/override decision"
		}
		return "open the vendor source manually, then keep manual management or add an ignore/override decision"
	case "ignore-local":
		return "review whether this user-local app should stay local-only or be ignored by manual inventory"
	case "needs-review":
		return "review ownership and lifecycle, then accept/edit/ignore the suggested inventory override"
	default:
		return "keep this manual inventory row as documented"
	}
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
		if manualPlanActionNeedsReview(item.Action) {
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
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel("url:", color), textui.StyleDim(tr("missing; fill docs/apps.md or keep as evidence gap", "未設定。docs/apps.md を補完するか evidence gap として残します"), color))
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
	return detailRowsToToolSections(manualPlanDetailRows(report), func(row detailBrowserRow) (string, string) {
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
	if evidence.Path != "" {
		parts = append(parts, "path="+evidence.Path)
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

func manualPlanDetailActions(item manualPlanItem, root string) []detailBrowserAction {
	actions := []detailBrowserAction{}
	if item.State == "installed" && manualPlanActionNeedsReview(item.Action) {
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
		if cask := manualDetailValue(item.Detail, "cask"); cask != "" {
			actions = append(actions, detailBrowserAction{Value: manualPlanDetailActionValue("review-cask", cask), Label: tr("review cask", "cask を確認"), Description: tr("run brew info --cask for ownership evidence", "ownership evidence として brew info --cask を実行します")})
		}
	}
	if item.Action == "adopt-mas" {
		if masID := manualDetailValue(item.Detail, "mas_id"); masID != "" {
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
		if cask := manualDetailValue(item.Detail, "cask"); cask != "" {
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
