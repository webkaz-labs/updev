package cmd

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/legacycache"
	"github.com/webkaz-labs/updev/internal/manualinventory"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/support"
	"github.com/webkaz-labs/updev/internal/syncreport"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updatereason"

	tea "charm.land/bubbletea/v2"
)

const inventoryCacheMaxAge = 10 * time.Minute

const (
	descriptionTranslationAuto   = "auto"
	descriptionTranslationManual = "manual"
	descriptionTranslationOff    = "off"
)

type listOptions struct {
	format         string
	root           string
	title          string
	provider       string
	kind           string
	category       string
	status         string
	query          string
	limit          int
	refresh        bool
	translateNow   bool
	retranslateAll bool
	autoTranslate  bool
	details        bool
	includeVSCode  bool
	tui            bool
	noTUI          bool
	hub            bool
}

type listReport struct {
	Status           plan.Status             `json:"status"`
	Root             string                  `json:"root"`
	Cached           bool                    `json:"cached"`
	CacheAge         string                  `json:"cache_age,omitempty"`
	Filters          map[string]string       `json:"filters,omitempty"`
	Providers        []plan.ProviderSummary  `json:"providers"`
	Items            []plan.Item             `json:"items"`
	Sections         []toolSection           `json:"sections,omitempty"`
	ReviewCandidates []manualReviewCandidate `json:"review_candidates,omitempty"`
	Evidence         plan.EvidenceIndex      `json:"-"`
	Details          bool                    `json:"-"`
	Limit            int                     `json:"-"`
}

type (
	toolSection = reviewui.Section
	toolRow     = reviewui.Row
)

type listTranslationUpdate struct {
	Attempted bool
	Changed   bool
	Message   string
}

func parseListOptions(args []string) (listOptions, error) {
	opts := listOptions{format: "text", root: defaultRoot(), title: "updev inventory"}
	plain := false
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	fs.StringVar(&opts.provider, "provider", "", "provider filter")
	fs.StringVar(&opts.kind, "kind", "", "kind filter")
	fs.StringVar(&opts.category, "category", "", "category filter")
	fs.StringVar(&opts.status, "status", "", "status filter")
	fs.StringVar(&opts.query, "query", "", "case-insensitive name/detail filter")
	fs.IntVar(&opts.limit, "limit", 0, "maximum rows per section; 0 means unlimited")
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached inventory")
	fs.BoolVar(&opts.refresh, "r", false, "ignore cached inventory")
	fs.BoolVar(&opts.translateNow, "translate-now", false, "translate missing descriptions with Codex now")
	fs.BoolVar(&opts.retranslateAll, "retranslate-all", false, "retranslate all descriptions")
	fs.BoolVar(&opts.details, "details", false, "print full details below truncated tables")
	fs.BoolVar(&opts.includeVSCode, "include-vscode", false, "include Brewfile-managed VS Code extensions")
	fs.BoolVar(&opts.tui, "interactive", false, "open the TTY selector")
	fs.BoolVar(&opts.tui, "tui", false, "open the TTY selector")
	fs.BoolVar(&opts.noTUI, "no-interactive", false, "disable the TTY selector")
	fs.BoolVar(&opts.noTUI, "no-tui", false, "disable the TTY selector")
	fs.BoolVar(&plain, "plain", false, "print text output and disable the TTY selector")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if plain {
		opts.format = "text"
		opts.noTUI = true
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.limit < 0 {
		return opts, fmt.Errorf("--limit must be 0 or greater")
	}
	return opts, nil
}

func runList(opts listOptions) int {
	progress := startupProgress{}
	if opts.format == "text" {
		lang := defaultLanguage()
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, inventoryProgressMessage(lang, opts.refresh))
	}
	var result inventoryResult
	if listManualOnly(opts) {
		result = inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: opts.root}}
	} else {
		progress.Start()
		maxAge := inventoryCacheMaxAge
		if !opts.refresh {
			maxAge = 0
		}
		result = collectInventoryCachedWithOptions(context.Background(), opts.root, opts.refresh, maxAge, inventoryOptions{IncludeVSCode: listIncludesVSCode(opts)})
		progress.Done()
	}
	report := buildListReport(result, opts)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		opts.autoTranslate = isTerminal(os.Stdin) && isTerminal(os.Stdout)
		translationProgress := startupProgress{}
		if shouldAutoUpdateListTranslations(opts) {
			translationProgress = newStartupProgress(os.Stdin, os.Stderr, opts.format, descriptionTranslationProgressMessage(defaultLanguage()))
		}
		translationProgress.Start()
		translation := maybeUpdateListTranslations(opts, report)
		translationProgress.Done()
		if translation.Changed {
			report = buildListReport(result, opts)
		}
		if shouldRunListHub(opts, os.Stdin, os.Stdout) {
			runListHub(report, opts.hub)
		} else {
			warnInteractiveUnavailable(os.Stdin, os.Stdout, opts.format, opts.tui, opts.noTUI)
			printListText(os.Stdout, report, opts.title, textui.ColorEnabled())
		}
		if translation.Message != "" {
			fmt.Println()
			fmt.Println(translation.Message)
		}
	}
	if report.Status == plan.StatusError {
		return 1
	}
	return 0
}

func listIncludesVSCode(opts listOptions) bool {
	return opts.includeVSCode || includeVSCodeExtensionsByDefault() || kindFilterIsVSCode(opts.kind) || providerFilterIsVSCode(opts.provider)
}

func shouldRunListHub(opts listOptions, input io.Reader, output io.Writer) bool {
	if opts.translateNow || opts.retranslateAll {
		return false
	}
	if opts.tui {
		return shouldRunUpdevInteractive(input, output, opts.format, true, opts.noTUI)
	}
	if opts.provider != "" || opts.kind != "" || opts.category != "" || opts.status != "" || opts.query != "" || opts.details || opts.limit > 0 {
		return false
	}
	return shouldRunUpdevInteractive(input, output, opts.format, false, opts.noTUI)
}

func shouldAutoUpdateListTranslations(opts listOptions) bool {
	if opts.translateNow || opts.retranslateAll {
		return false
	}
	return opts.autoTranslate && descriptionTranslationMode() == descriptionTranslationAuto && i18n.IsJapanese(defaultLanguage())
}

func maybeUpdateListTranslations(opts listOptions, report listReport) listTranslationUpdate {
	mode := descriptionTranslationMode()
	explicit := opts.translateNow || opts.retranslateAll
	if mode == descriptionTranslationOff {
		if explicit {
			return listTranslationUpdate{Attempted: true, Message: "translations: disabled by [ui].description_translation"}
		}
		return listTranslationUpdate{}
	}
	if !explicit {
		if !shouldAutoUpdateListTranslations(opts) {
			return listTranslationUpdate{}
		}
	}
	if _, err := exec.LookPath("codex"); err != nil {
		if explicit {
			return listTranslationUpdate{Attempted: true, Message: "translations: Codex CLI not found; install codex or set [ui].description_translation = \"off\""}
		}
		return listTranslationUpdate{}
	}
	cache := legacycache.Load()
	pending := cache.PendingTranslations(displayListItems(report.Items, report.Sections), report.Sections, opts.retranslateAll)
	if len(pending) == 0 {
		if explicit {
			return listTranslationUpdate{Attempted: true, Message: "translations: up to date"}
		}
		return listTranslationUpdate{Attempted: true}
	}
	translated, err := translateBatch(pending)
	if err != nil {
		if explicit {
			return listTranslationUpdate{Attempted: true, Message: fmt.Sprintf("translations: %d pending, but Codex translation failed: %s", len(pending), err)}
		}
		return listTranslationUpdate{Attempted: true}
	}
	if len(translated) == 0 {
		if explicit {
			return listTranslationUpdate{Attempted: true, Message: fmt.Sprintf("translations: %d pending, but Codex translation returned no updates", len(pending))}
		}
		return listTranslationUpdate{Attempted: true}
	}
	cache.ApplyTranslations(pending, translated)
	cache.SaveTranslations()
	message := ""
	if explicit {
		message = fmt.Sprintf("translations: updated %d entries", len(translated))
	}
	return listTranslationUpdate{Attempted: true, Changed: true, Message: message}
}

func descriptionTranslationMode() string {
	mode := descriptionTranslationAuto
	if configured := loadUpdevConfig().UI.DescriptionTranslation; configured != nil && validDescriptionTranslationMode(*configured) {
		mode = strings.ToLower(strings.TrimSpace(*configured))
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_DESCRIPTION_TRANSLATION")); value != "" && validDescriptionTranslationMode(value) {
		mode = strings.ToLower(value)
	}
	return mode
}

func validDescriptionTranslationMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case descriptionTranslationAuto, descriptionTranslationManual, descriptionTranslationOff:
		return true
	default:
		return false
	}
}

func translateBatch(pending map[string]string) (map[string]string, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var input strings.Builder
	for _, key := range keys {
		input.WriteString(key)
		input.WriteString("\t")
		input.WriteString(pending[key])
		input.WriteString("\n")
	}
	prompt := "以下のTSVの2列目を自然な日本語に翻訳してください。\n" +
		"1列目のkeyは変更しないでください。\n" +
		"出力は次の形式のみ:\nBEGIN_TSV\nkey<TAB>ja\nEND_TSV\n\nINPUT:\n" +
		input.String()
	responseFile, err := os.CreateTemp("", "updev-translation-*.txt")
	if err != nil {
		return nil, err
	}
	responsePath := responseFile.Name()
	_ = responseFile.Close()
	defer os.Remove(responsePath)
	command := exec.CommandContext(context.Background(), "codex", "exec", "--skip-git-repo-check", "--output-last-message", responsePath, "-")
	command.Stdin = strings.NewReader(prompt)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("codex exec: %w: %s", err, truncate(strings.TrimSpace(string(output)), 180))
	}
	response, err := os.ReadFile(responsePath)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(response))) == 0 {
		response = output
	}
	return parseTranslatedTSV(response, pending), nil
}

func parseTranslatedTSV(output []byte, pending map[string]string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, raw := range bytes.Split(output, []byte("\n")) {
		line := strings.TrimSpace(string(raw))
		switch line {
		case "BEGIN_TSV":
			inBlock = true
			continue
		case "END_TSV":
			return out
		}
		if !inBlock || !strings.Contains(line, "\t") {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if _, exists := pending[key]; exists && strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func buildListReport(result inventoryResult, opts listOptions) listReport {
	legacy := legacycache.Load()
	root := opts.root
	if root == "" {
		root = result.Report.Root
	}
	enriched := enrichItems(result.Report.Items, legacy, manualAppIndex(root))
	enriched = enrichHomebrewTapCaskItems(enriched, manualHomebrewTapIndex(root))
	filtered := filterItems(enriched, opts)
	sections := legacy.ToolSections(legacyCacheFilters(opts))
	manualInventoryItems := enriched
	if listManualOnly(opts) && len(manualInventoryItems) == 0 {
		manualInventoryItems = manualCachedInventoryItems(root)
	}
	manualSections := manualAppSections(root, opts, manualInventoryItems)
	sections = append(sections, manualSections...)
	evidence := buildListEvidenceIndex(root)
	providers := filteredProviders(result.Report.Providers, opts.provider)
	if len(manualSections) > 0 {
		providers = append(providers, manualProviderSummary(manualSections))
	}
	report := listReport{
		Status:           result.Report.Status,
		Root:             result.Report.Root,
		Cached:           result.Cached,
		Providers:        providers,
		Items:            filtered,
		Sections:         sections,
		ReviewCandidates: manualReviewCandidates(manualSections),
		Evidence:         evidence,
		Details:          opts.details,
		Limit:            opts.limit,
	}
	if result.Cached && !result.CreatedAt.IsZero() {
		report.CacheAge = textui.FriendlyAge(time.Since(result.CreatedAt))
	}
	filters := map[string]string{}
	if opts.provider != "" {
		filters["provider"] = opts.provider
	}
	if opts.kind != "" {
		filters["kind"] = opts.kind
	}
	if opts.category != "" {
		filters["category"] = opts.category
	}
	if opts.status != "" {
		filters["status"] = opts.status
	}
	if opts.query != "" {
		filters["query"] = opts.query
	}
	if opts.limit > 0 {
		filters["limit"] = fmt.Sprint(opts.limit)
	}
	if listIncludesVSCode(opts) {
		filters["include_vscode"] = "true"
	}
	if len(filters) > 0 {
		report.Filters = filters
	}
	return report
}

func legacyCacheFilters(opts listOptions) legacycache.Filters {
	return legacycache.Filters{
		Provider: opts.provider,
		Kind:     opts.kind,
		Category: opts.category,
		Status:   opts.status,
		Query:    opts.query,
	}
}

func printListText(w io.Writer, report listReport, title string, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(title, color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	if report.Cached {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("cache:", "キャッシュ:"), color), textui.StyleCount(report.CacheAge+" old", color), textui.StyleDim(tr("(use --refresh for a fresh read)", "(再取得は --refresh)"), color))
	}
	if len(report.Filters) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("filters:", "フィルター:"), color), textui.StyleRequested(textui.FilterSummary(report.Filters, filterSummaryKeys...), color))
	}
	printListAttentionSummary(w, report, color)
	printListEvidenceSummary(w, report, color)
	fmt.Fprintln(w)
	printProviderSummary(w, report.Providers, color)
	if summary := listCategorySummary(report, color); summary != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(tr("categories", "categories"), color), summary)
		if meanings := listCategoryMeaningSummary(report, color); meanings != "" {
			fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("meaning:", "意味:"), color), meanings)
		}
	}
	fmt.Fprintln(w)
	displayItems := listTextDisplayItems(report)
	displaySections := listTextDisplaySections(report)
	if len(displayItems) > 0 || len(report.Sections) == 0 {
		printGroupedItems(w, displayItems, report.Limit, color)
	}
	if len(displaySections) > 0 {
		fmt.Fprintln(w)
		printToolSections(w, displaySections, report.Limit, color)
	}
	if report.Details && reportHasDetails(report) {
		fmt.Fprintln(w)
		printListDetails(w, report, color)
	}
}

func printListAttentionSummary(w io.Writer, report listReport, color bool) {
	providerAttention := 0
	for _, provider := range report.Providers {
		if plan.ProviderStatus(provider) != plan.StatusOK {
			providerAttention++
		}
	}
	itemCounts := map[string]int{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		if plan.IsAttentionStatus(item.Status) {
			itemCounts[inventoryannotate.ItemStatusLabel(item)]++
		}
	}
	parts := []string{
		fmt.Sprintf(tr("%d providers", "provider %d件"), len(report.Providers)),
		fmt.Sprintf(tr("%d items", "項目 %d件"), len(report.Items)),
	}
	if rows := listVisibleRowCount(report); rows != len(report.Items) {
		parts = append(parts, fmt.Sprintf(tr("%d rows", "行 %d件"), rows))
	}
	if providerAttention > 0 {
		parts = append(parts, textui.StyleWarning(fmt.Sprintf(tr("%d provider attention", "provider 注意 %d件"), providerAttention), color))
	}
	for _, status := range plan.AttentionStatusOrder() {
		if count := itemCounts[string(status)]; count > 0 {
			parts = append(parts, textui.StyleStatus(fmt.Sprintf("%s=%d", status, count), color))
		}
		if status == plan.StatusExtra {
			if count := itemCounts["profile-mismatch"]; count > 0 {
				parts = append(parts, textui.StyleStatus(fmt.Sprintf("profile-mismatch=%d", count), color))
			}
		}
	}
	if len(parts) == 2 {
		parts = append(parts, textui.StyleDim(tr("no attention items", "注意項目なし"), color))
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("summary:", "サマリー:"), color), strings.Join(parts, ", "))
}

func printListEvidenceSummary(w io.Writer, report listReport, color bool) {
	summary := listReportEvidenceSummary(report, color)
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("review:", "確認:"), color), summary)
}

func listVisibleRowCount(report listReport) int {
	rows := len(displayListItems(report.Items, report.Sections))
	for _, section := range report.Sections {
		rows += len(section.Rows)
	}
	return rows
}

func printProviderSummary(w io.Writer, providers []plan.ProviderSummary, color bool) {
	fmt.Fprintln(w, textui.StyleHeading(tr("providers", "providers"), color))
	if len(providers) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no provider summary", "provider サマリーはありません"), color))
		return
	}
	rows := make([][]string, 0, len(providers))
	for _, provider := range providers {
		status := string(plan.ProviderStatus(provider))
		rows = append(rows, []string{
			textui.StyleName(provider.Name, color),
			textui.StyleCount(fmt.Sprint(provider.Desired), color),
			textui.StyleCount(fmt.Sprint(provider.Live), color),
			styleDriftCount(provider.Missing, color),
			styleDriftCount(provider.Extra, color),
			textui.StyleStatus(status, color),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: tr("name", "名前"), Min: 6, Max: 12},
		{Header: tr("desired", "要求"), Min: 7, Max: 8},
		{Header: "live", Min: 4, Max: 8},
		{Header: tr("missing", "不足"), Min: 7, Max: 8},
		{Header: tr("extra", "余剰"), Min: 5, Max: 8},
		{Header: tr("status", "状態"), Min: 6, Max: 12},
	}, rows, color)
}

func printGroupedItems(w io.Writer, items []plan.Item, limit int, color bool) {
	if len(items) == 0 {
		fmt.Fprintln(w, textui.StyleHeading(tr("items", "項目"), color))
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no matching entries", "該当する entry はありません"), color))
		return
	}
	grouped := map[string][]plan.Item{}
	order := []string{}
	for _, item := range items {
		key := item.Provider + " / " + item.Kind
		if item.Category != "" {
			key += " / " + item.Category
		}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], item)
	}
	for index, key := range order {
		if index > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(key, color), textui.StyleCount(fmt.Sprintf("(%d)", len(grouped[key])), color))
		visible := limitedItems(grouped[key], limit)
		rows := make([][]string, 0, len(visible))
		for _, item := range visible {
			rows = append(rows, []string{
				textui.StyleName(item.Name, color),
				styleInventoryItemVersion(item.Version, item.Status, color),
				textui.StyleStatus(inventoryannotate.ItemStatusLabel(item), color),
				textui.StyleBool(item.Desired, color),
				textui.StyleBool(item.Live, color),
				styleInventoryItemDetail(item.Detail, item.Status, color),
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: tr("name", "名前"), Min: 12, Max: 36},
			{Header: "version", Min: 7, Max: 14},
			{Header: tr("status", "状態"), Min: 7, Max: 9},
			{Header: "desired", Min: 7, Max: 7},
			{Header: "live", Min: 4, Max: 4},
			{Header: tr("detail", "詳細"), Min: 0, Max: 64},
		}, rows, color)
		printOmittedRows(w, len(grouped[key])-len(visible), color)
	}
}

func printToolSections(w io.Writer, sections []toolSection, limit int, color bool) {
	reviewui.PrintSections(w, sections, limit, reviewLabels(), color)
}

func limitedItems(items []plan.Item, limit int) []plan.Item {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func limitedToolRows(rows []toolRow, limit int) []toolRow {
	return reviewui.LimitedRows(rows, limit)
}

func listTextDisplayItems(report listReport) []plan.Item {
	return enrichListItemsWithEvidence(displayListItems(report.Items, report.Sections), report.Evidence)
}

func listTextDisplaySections(report listReport) []toolSection {
	return enrichToolSectionsWithEvidence(listDisplaySections(report), report.Evidence)
}

func enrichListItemsWithEvidence(items []plan.Item, evidence plan.EvidenceIndex) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		enriched := item
		itemEvidence := itemListEvidence(item, evidence)
		if listItemEvidenceSummary(itemEvidence) != "" {
			enriched.Detail = itemDetailWithEvidence(enriched.Detail, itemEvidence)
		}
		out = append(out, enriched)
	}
	return out
}

func printOmittedRows(w io.Writer, omitted int, color bool) {
	reviewui.PrintOmittedRows(w, omitted, reviewLabels(), color)
}

func reviewLabels() reviewui.Labels {
	return reviewui.Labels{
		Name:    tr("name", "名前"),
		Version: "version",
		Wanted:  "wanted",
		State:   tr("state", "状態"),
		Action:  tr("check", "確認"),
		Detail:  tr("detail", "詳細"),
		MoreRows: func(count int) string {
			return fmt.Sprintf(tr("... %d more rows; rerun with --limit 0 or --details", "... あと %d 行。全件は --limit 0 または --details"), count)
		},
		NoExtraDetail: tr("no additional detail", "追加の詳細はありません"),
	}
}

func reportHasDetails(report listReport) bool {
	for _, item := range listTextDisplayItems(report) {
		if strings.TrimSpace(item.Detail) != "" {
			return true
		}
	}
	for _, section := range listTextDisplaySections(report) {
		for _, row := range section.Rows {
			if strings.TrimSpace(row.Detail) != "" {
				return true
			}
		}
	}
	return false
}

func printListDetails(w io.Writer, report listReport, color bool) {
	fmt.Fprintln(w, textui.StyleHeading(tr("details", "詳細"), color))
	wrote := false
	for _, item := range listTextDisplayItems(report) {
		if strings.TrimSpace(item.Detail) == "" {
			continue
		}
		wrote = true
		fmt.Fprintf(w, "%s %s\n", textui.StyleName(item.Provider+"/"+item.Kind+" "+item.Name, color), textui.StyleStatus(inventoryannotate.ItemStatusLabel(item), color))
		printDetailLine(w, "detail", localizedBuiltInNoteText(item.Detail), color)
	}
	for _, section := range listTextDisplaySections(report) {
		for _, row := range section.Rows {
			if strings.TrimSpace(row.Detail) == "" {
				continue
			}
			wrote = true
			fmt.Fprintf(w, "%s %s\n", textui.StyleName(section.Title+" "+row.Name, color), textui.StyleStatus(firstNonEmpty(row.State, "active"), color))
			printDetailLine(w, "version", row.Version, color)
			printDetailLine(w, "wanted", row.Wanted, color)
			printDetailLine(w, "detail", row.Detail, color)
		}
	}
	if !wrote {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no details", "詳細はありません"), color))
	}
}

const (
	listHubActionAttention = "attention"
	listHubActionProvider  = "provider"
	listHubActionKind      = "kind"
	listHubActionCategory  = "category"
	listHubActionStatus    = "status"
	listHubActionQuery     = "query"
	listHubActionManual    = "manual"
	listHubActionBackends  = "backends"
	listHubActionUpdates   = "updates"
	listHubActionSecurity  = "security"
	listHubActionSupport   = "support"
	listHubActionLimited   = "limited"
	listHubActionDetails   = "details"
	listHubActionFull      = "full"
)

const (
	listRouteActionPrefix = "list-route"
	brewDriftActionPrefix = "brew-drift"
)

type listRouteAction struct {
	Domain   string
	Provider string
	Kind     string
	Name     string
}

type listRouteDetailOutcome string

const (
	listRouteDetailBack listRouteDetailOutcome = "back"
	listRouteDetailHome listRouteDetailOutcome = "home"
	listRouteDetailExit listRouteDetailOutcome = "exit"
)

func runListHub(report listReport, selectorHub bool) {
	defaultAction := listHubActionFull
	pendingAction := listHubActionFull
	if selectorHub {
		pendingAction = ""
	}
	backendPlan := backendPlanReport{}
	backendLoading := true
	lastUpdate, hasLastUpdate := loadLastUpdateReport()
	detailStates := map[string]detailBrowserState{}
	for {
		action := pendingAction
		pendingAction = ""
		if action == "" {
			var err error
			color := textui.ColorEnabled()
			choices := listHubChoices(report, backendPlan, backendLoading, lastUpdate.Report, hasLastUpdate, selectorHub)
			if selectorHub {
				printListHubDashboard(os.Stdout, report, color)
				action, err = runUpdevSelect("updev hub", tr("Choose a review domain. Back/Home/Exit are available after each view.", "確認 domain を選択します。各 view から Back/Home/Exit できます。"), choices, defaultAction)
				if err != nil {
					return
				}
			} else {
				action = listHubActionFull
			}
		}
		if action == updevActionExit {
			return
		}
		defaultAction = action
		if shouldRunListHubRouterAction(action) {
			result, updatedStates, err := runListHubRouter(listHubRouterOptions{
				Report:         report,
				BackendPlan:    backendPlan,
				BackendLoading: backendLoading,
				LastUpdate:     lastUpdate.Report,
				HasLastUpdate:  hasLastUpdate,
				InitialAction:  action,
				DetailStates:   detailStates,
				Color:          textui.ColorEnabled(),
			})
			if err == nil && result.Action != "" {
				detailStates = updatedStates
				if result.BackendReady {
					backendPlan = result.BackendPlan
					if backendLoading {
						report.Evidence = addBackendListEvidence(report.Evidence, backendPlan)
					}
					backendLoading = false
				}
				nextAction, exit := handleListHubRouterResult(report.Root, result, &backendPlan, &lastUpdate.Report, &hasLastUpdate)
				if exit {
					return
				}
				if nextAction == "" || nextAction == updevActionBack {
					selectorHub = true
					defaultAction = listHubActionFull
					pendingAction = ""
					continue
				}
				defaultAction = nextAction
				pendingAction = nextAction
				continue
			}
		}
		switch action {
		case listHubActionAttention:
			printListText(os.Stdout, derivedListReport(report, listOptions{status: "attention", limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionProvider:
			provider, ok := selectListProvider(report)
			if !ok {
				continue
			}
			defaultAction = listHubActionProvider
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:         "updev list " + provider,
				Report:        derivedListReport(report, listOptions{provider: provider}),
				DetailStates:  detailStates,
				DefaultAction: &defaultAction,
				PendingAction: &pendingAction,
				BackendPlan:   &backendPlan,
				LastUpdate:    lastUpdate.Report,
				HasLastUpdate: hasLastUpdate,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{provider: provider, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionKind:
			kind, ok := selectListKind(report)
			if !ok {
				continue
			}
			defaultAction = listHubActionKind
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:         "updev list " + kind,
				Report:        derivedListReport(report, listOptions{kind: kind}),
				DetailStates:  detailStates,
				DefaultAction: &defaultAction,
				PendingAction: &pendingAction,
				BackendPlan:   &backendPlan,
				LastUpdate:    lastUpdate.Report,
				HasLastUpdate: hasLastUpdate,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{kind: kind, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionCategory:
			category, ok := selectListCategory(report)
			if !ok {
				continue
			}
			defaultAction = listHubActionCategory
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:         "updev list " + category,
				Report:        derivedListReport(report, listOptions{category: category}),
				DetailStates:  detailStates,
				DefaultAction: &defaultAction,
				PendingAction: &pendingAction,
				BackendPlan:   &backendPlan,
				LastUpdate:    lastUpdate.Report,
				HasLastUpdate: hasLastUpdate,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{category: category, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionStatus:
			status, ok := selectListStatus()
			if !ok {
				continue
			}
			defaultAction = listHubActionStatus
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:         "updev list " + status,
				Report:        derivedListReport(report, listOptions{status: status}),
				DetailStates:  detailStates,
				DefaultAction: &defaultAction,
				PendingAction: &pendingAction,
				BackendPlan:   &backendPlan,
				LastUpdate:    lastUpdate.Report,
				HasLastUpdate: hasLastUpdate,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{status: status, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionQuery:
			query, ok := selectListQuery()
			if !ok {
				continue
			}
			defaultAction = listHubActionQuery
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:         "updev list query",
				Report:        derivedListReport(report, listOptions{query: query}),
				DetailStates:  detailStates,
				DefaultAction: &defaultAction,
				PendingAction: &pendingAction,
				BackendPlan:   &backendPlan,
				LastUpdate:    lastUpdate.Report,
				HasLastUpdate: hasLastUpdate,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{query: query, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionManual:
			defaultAction = listHubActionManual
			manualReport := derivedListReport(report, listOptions{provider: manualProviderName})
			handled, exit := runListFilteredSelection(listFilteredSelectionOptions{
				Title:          "updev list manual",
				Report:         manualReport,
				DetailStates:   detailStates,
				DefaultAction:  &defaultAction,
				PendingAction:  &pendingAction,
				BackendPlan:    &backendPlan,
				LastUpdate:     lastUpdate.Report,
				HasLastUpdate:  hasLastUpdate,
				NextAction:     listHubActionFull,
				PreviousAction: listHubActionFull,
			})
			if exit {
				return
			}
			if handled {
				continue
			}
			printListText(os.Stdout, derivedListReport(report, listOptions{provider: manualProviderName, limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionBackends:
			defaultAction = listHubActionBackends
			stateKey := "list-backends"
			state, err := runToolTableBrowserWithState("updev backend convergence", backendToolSectionsWithLoading(backendPlan, backendLoading), detailStates[stateKey], textui.ColorEnabled())
			if err != nil {
				printBackendPlanText(os.Stdout, backendPlan, textui.ColorEnabled())
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = listHubActionFull
				continue
			}
			if handleBackendDetailAction(report.Root, state.Action) {
				backendPlan = buildBackendPlanForHub(report.Root)
				defaultAction = listHubActionBackends
				continue
			}
			continue
		case listHubActionUpdates:
			defaultAction = listHubActionUpdates
			if !hasLastUpdate {
				continue
			}
			stateKey := "list-updates"
			state, err := runDetailBrowserWithState("updev update evidence", updateLogDetailRows(lastUpdate.Report), detailStates[stateKey], textui.ColorEnabled())
			if err != nil {
				printLastUpdateLogs(os.Stdout, lastUpdate.Report, textui.ColorEnabled())
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = listHubActionFull
				continue
			}
			if route, ok := parseUpdateSummaryRoute(state.Action); ok {
				if runUpdateSummaryRouteDetail(lastUpdate.Report, route, backendPlan, detailStates, textui.ColorEnabled()) {
					return
				}
				continue
			}
			if state.Action == listHubActionSecurity || state.Action == updateHubActionSecurity {
				pendingAction = listHubActionSecurity
				defaultAction = listHubActionSecurity
			}
			continue
		case listHubActionSecurity:
			defaultAction = listHubActionSecurity
			if !hasLastUpdate {
				continue
			}
			stateKey := "list-security"
			state, err := runDetailBrowserWithState("updev security review", updateSecurityDetailRows(lastUpdate.Report), detailStates[stateKey], textui.ColorEnabled())
			if err != nil {
				printLastSecuritySection(os.Stdout, lastUpdate.Report, true, textui.ColorEnabled())
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = listHubActionFull
				continue
			}
			if handleMiseBumpDetailAction(&lastUpdate.Report, state.Action) {
				hasLastUpdate = true
				defaultAction = listHubActionSecurity
				continue
			}
			if handleSecurityDetailAction(&lastUpdate.Report, state.Action) {
				hasLastUpdate = true
				defaultAction = listHubActionSecurity
				continue
			}
			continue
		case listHubActionLimited:
			printListText(os.Stdout, derivedListReport(report, listOptions{limit: 10}), "updev inventory", textui.ColorEnabled())
		case listHubActionDetails:
			detailReport := derivedListReport(report, listOptions{limit: 10})
			stateKey := "list-details"
			state, err := runDetailBrowserWithState("updev list details", listDetailRows(detailReport), detailStates[stateKey], textui.ColorEnabled())
			if err != nil {
				printListText(os.Stdout, derivedListReport(report, listOptions{details: true, limit: 10}), "updev inventory", textui.ColorEnabled())
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = listHubActionFull
				continue
			}
			continue
		case listHubActionFull:
			action, handled := runListFilteredBrowserWithViewActions("updev installed inventory", report, detailStates, textui.ColorEnabled(), listHubActionManual, listHubActionManual)
			if action == updevActionExit {
				return
			}
			if action == updevActionHome || action == updevActionBack {
				selectorHub = true
				defaultAction = listHubActionFull
				pendingAction = ""
				continue
			}
			if route, ok := parseListRouteAction(action); ok {
				outcome := runListRouteDetail(listRouteDetailOptions{
					Root:          report.Root,
					Route:         route,
					BackendPlan:   backendPlan,
					LastUpdate:    lastUpdate.Report,
					HasLastUpdate: hasLastUpdate,
					DetailStates:  detailStates,
					Color:         textui.ColorEnabled(),
				})
				if outcome == listRouteDetailExit {
					return
				}
				if outcome == listRouteDetailHome {
					defaultAction = listHubActionFull
					continue
				}
				if route.Domain == listHubActionBackends {
					backendPlan = buildBackendPlanForHub(report.Root)
				}
				defaultAction = listHubActionFull
				pendingAction = listHubActionFull
				continue
			}
			if listHubActionExists(action) {
				defaultAction = action
				pendingAction = action
				continue
			}
			if handled {
				continue
			}
			printListText(os.Stdout, report, "updev inventory", textui.ColorEnabled())
		}
		next, err := runPostSectionNavigation()
		if err != nil || next == updevActionExit {
			return
		}
		if next == updevActionHome {
			defaultAction = listHubActionFull
		}
	}
}

func buildBackendPlanForHub(root string) backendPlanReport {
	progress := newStartupProgress(os.Stdin, os.Stderr, "text", reviewPlanProgressMessage(defaultLanguage()))
	progress.Start()
	report := buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: root})
	progress.Done()
	return report
}

func listHubActionExists(action string) bool {
	switch action {
	case listHubActionAttention, listHubActionProvider, listHubActionKind,
		listHubActionCategory, listHubActionStatus, listHubActionQuery,
		listHubActionManual, listHubActionBackends, listHubActionUpdates,
		listHubActionSecurity, listHubActionSupport, listHubActionLimited, listHubActionDetails,
		listHubActionFull:
		return true
	default:
		return false
	}
}

func shouldRunListHubRouterAction(action string) bool {
	switch action {
	case listHubActionAttention, listHubActionProvider, listHubActionKind, listHubActionCategory, listHubActionStatus, listHubActionQuery, listHubActionManual, listHubActionBackends, listHubActionUpdates, listHubActionSecurity, listHubActionSupport, listHubActionLimited, listHubActionDetails, listHubActionFull:
		return true
	default:
		return false
	}
}

func handleListHubRouterResult(root string, result listHubRouterResult, backendPlan *backendPlanReport, lastUpdate *updateReport, hasLastUpdate *bool) (string, bool) {
	action := result.Action
	switch {
	case action == updevActionBack || action == updevActionHome:
		return updevActionBack, false
	case action == updevActionExit:
		return "", true
	case handleManualPlanDetailAction(root, action):
		if result.FromRoute && result.ReturnAction != "" {
			return result.ReturnAction, false
		}
		return listHubActionManual, false
	case handleBackendDetailAction(root, action):
		*backendPlan = buildBackendPlanForHub(root)
		if result.FromRoute && result.ReturnAction != "" {
			return result.ReturnAction, false
		}
		return listHubActionBackends, false
	case handleMiseBumpDetailAction(lastUpdate, action):
		*hasLastUpdate = true
		if result.FromRoute && result.ReturnAction != "" {
			return result.ReturnAction, false
		}
		return listHubActionSecurity, false
	case handleSecurityDetailAction(lastUpdate, action):
		*hasLastUpdate = true
		if result.FromRoute && result.ReturnAction != "" {
			return result.ReturnAction, false
		}
		return listHubActionSecurity, false
	case listHubActionExists(action):
		return action, false
	default:
		return listHubActionFull, false
	}
}

func printListHubDashboard(w io.Writer, report listReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev list", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	if report.Cached {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("cache:", "キャッシュ:"), color), textui.StyleCount(report.CacheAge+" old", color), textui.StyleDim(tr("(use --refresh for a fresh read)", "(再取得は --refresh)"), color))
	}
	printListAttentionSummary(w, report, color)
	fmt.Fprintln(w)
	printProviderSummary(w, report.Providers, color)
}

func listHubChoices(report listReport, backendPlan backendPlanReport, backendLoading bool, lastUpdate updateReport, hasLastUpdate bool, selectorHub bool) []updevChoice {
	choices := []updevChoice{
		{Value: listHubActionFull, Label: tr("Installed inventory", "インストール済み一覧"), Description: fmt.Sprintf(tr("Review all %d installed and desired inventory rows with grouping, filters, and expansion.", "全 %d 件の installed / desired inventory 行を grouping / filter / 展開付きで確認します。"), listInventoryReviewCount(report)), Selected: true},
		{Value: listHubActionManual, Label: tr("Manual apps", "手動管理アプリ"), Description: tr("Review read-only manual, vendor, App Store, and non-provider app rows.", "manual / vendor / App Store / provider 外アプリを read-only で確認します。")},
	}
	if !selectorHub {
		choices = append(
			choices,
			updevChoice{Value: listHubActionProvider, Label: tr("Provider filter", "provider filter"), Description: tr("Choose a provider; rich rows expand with Enter or Space.", "provider を選びます。詳細行は Enter / Space で展開できます。")},
			updevChoice{Value: listHubActionKind, Label: tr("Kind filter", "kind filter"), Description: tr("Choose brew, cask, vscode, tap, or tool rows.", "brew / cask / vscode / tap / tool で絞り込みます。")},
			updevChoice{Value: listHubActionCategory, Label: tr("Category filter", "category filter"), Description: tr("Choose work, personal, runtime, core, npm, or another group.", "work / personal / runtime / core / npm などで絞り込みます。")},
			updevChoice{Value: listHubActionStatus, Label: tr("Status filter", "status filter"), Description: tr("Choose attention, active, inactive, missing, extra, drift, unavailable, or ok.", "attention / active / inactive / missing / extra / drift / unavailable / ok を選びます。")},
			updevChoice{Value: listHubActionQuery, Label: tr("Query filter", "query filter"), Description: tr("Search row names, versions, and descriptions.", "name / version / description を検索します。")},
		)
	}
	if backendLoading {
		choices = append(choices, updevChoice{Value: listHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: tr("Prepare provider/backend recommendations asynchronously after the view opens.", "view を開いてから provider/backend 推奨を非同期で準備します。")})
	} else if findings := len(backendPlan.Findings); findings > 0 {
		actions := backendPlanActionableCount(backendPlan)
		choices = append(choices, updevChoice{Value: listHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: fmt.Sprintf(tr("Review %d provider/backend recommendations; %d can be applied from details.", "%d 件の provider/backend 推奨を確認します。%d 件は詳細から適用できます。"), findings, actions)})
	}
	if hasLastUpdate && plan.EvidenceRootsMatch(report.Root, lastUpdate.Root) {
		if updateRows := updateReportUpdatedItemCount(lastUpdate) + updateReportDeferredItemCount(lastUpdate); updateRows > 0 || len(lastUpdate.Steps) > 0 {
			choices = append(choices, updevChoice{Value: listHubActionUpdates, Label: tr("Update evidence", "update evidence"), Description: fmt.Sprintf(tr("Review cached provider update evidence from %d steps.", "%d 件の cached provider update evidence を確認します。"), len(lastUpdate.Steps))})
		}
		if updateDashboardSecurityAttention(lastUpdate) > 0 {
			choices = append(choices, updevChoice{Value: listHubActionSecurity, Label: tr("Security review", "security review"), Description: tr("Review cached security holds, advisories, and policy actions.", "cached security hold / advisory / policy action を確認します。")})
		}
	}
	choices = append(
		choices,
		updevChoice{Value: listHubActionSupport, Label: tr("Support catalog", "support catalog"), Description: tr("Review support labels for providers, commands, reports, and inventory sources.", "provider / command / report / inventory source の support label を確認します。")},
		updevChoice{Value: listHubActionLimited, Label: tr("Advanced: compact list", "上級: compact list"), Description: tr("Auxiliary view with at most 10 rows per section.", "補助 view として各 section 最大 10 行で表示します。")},
		updevChoice{Value: listHubActionDetails, Label: tr("Advanced: details", "上級: 詳細"), Description: tr("Auxiliary detail view for limited rows and expanded descriptions.", "補助 detail view として絞り込んだ行と説明を表示します。")},
		updevChoice{Value: updevActionExit, Label: tr("Exit", "終了"), Description: tr("Leave the selector.", "selector を終了します。")},
	)
	return choices
}

func listInventoryReviewCount(report listReport) int {
	return len(displayListItems(report.Items, report.Sections)) + toolTableRowCount(report.Sections)
}

func selectListProvider(report listReport) (string, bool) {
	choices := []updevChoice{{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return to the list hub.", "list hub に戻ります。"), Selected: true}}
	for _, provider := range report.Providers {
		choices = append(choices, updevChoice{
			Value:       provider.Name,
			Label:       provider.Name,
			Description: fmt.Sprintf("desired=%d live=%d missing=%d extra=%d", provider.Desired, provider.Live, provider.Missing, provider.Extra),
		})
	}
	value, err := runUpdevSelect("updev list provider", tr("Choose a provider filter.", "provider filter を選択します。"), choices, updevActionBack)
	if err != nil || value == updevActionBack {
		return "", false
	}
	return value, true
}

func selectListKind(report listReport) (string, bool) {
	counts := listKindCounts(report)
	order := sortedMapKeys(counts)
	choices := []updevChoice{{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return to the list hub.", "list hub に戻ります。"), Selected: true}}
	for _, kind := range order {
		choices = append(choices, updevChoice{
			Value:       kind,
			Label:       kind,
			Description: fmt.Sprintf("%d rows", counts[kind]),
		})
	}
	value, err := runUpdevSelect("updev list kind", tr("Choose a kind filter.", "kind filter を選択します。"), choices, updevActionBack)
	if err != nil || value == updevActionBack {
		return "", false
	}
	return value, true
}

func selectListCategory(report listReport) (string, bool) {
	counts := listCategoryCounts(report)
	order := sortedMapKeys(counts)
	choices := []updevChoice{{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return to the list hub.", "list hub に戻ります。"), Selected: true}}
	for _, category := range order {
		choices = append(choices, updevChoice{
			Value:       category,
			Label:       category,
			Description: fmt.Sprintf("%d rows - %s", counts[category], categoryDescription(category)),
		})
	}
	value, err := runUpdevSelect("updev list category", tr("Choose a category filter.", "category filter を選択します。"), choices, updevActionBack)
	if err != nil || value == updevActionBack {
		return "", false
	}
	return value, true
}

func selectListStatus() (string, bool) {
	choices := []updevChoice{
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return to the list hub.", "list hub に戻ります。"), Selected: true},
		{Value: "attention", Label: "attention", Description: tr("Rows needing attention.", "対応が必要な行。")},
		{Value: "active", Label: "active", Description: tr("Active mise/tool rows.", "active な mise/tool 行。")},
		{Value: "inactive", Label: "inactive", Description: tr("Inactive mise/tool rows.", "inactive な mise/tool 行。")},
		{Value: "installed", Label: "installed", Description: tr("Installed mise/tool rows.", "インストール済みの mise/tool 行。")},
		{Value: "profile-mismatch", Label: "profile-mismatch", Description: tr("Rows installed from an inactive deployment scope.", "inactive な deployment scope 由来でインストール済みの行。")},
		{Value: "missing", Label: "missing", Description: tr("Desired but not installed.", "desired だが未インストール。")},
		{Value: "extra", Label: "extra", Description: tr("Installed but not desired.", "インストール済みだが desired ではない。")},
		{Value: "drift", Label: "drift", Description: tr("Desired and live state differ.", "desired と live state が異なる。")},
		{Value: "unavailable", Label: "unavailable", Description: tr("Provider data was unavailable.", "provider data が取得できない。")},
		{Value: "ok", Label: "ok", Description: tr("Rows already matching desired state.", "desired state と一致済み。")},
	}
	value, err := runUpdevSelect("updev list status", tr("Choose a status filter.", "status filter を選択します。"), choices, updevActionBack)
	if err != nil || value == updevActionBack {
		return "", false
	}
	return value, true
}

func selectListQuery() (string, bool) {
	value, err := runUpdevInput("updev list query", tr("Type a text filter. Empty input returns to the list hub.", "text filter を入力します。空入力なら list hub に戻ります。"), "git, node, missing, ...", "")
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}

type listFilteredSelectionOptions struct {
	Title          string
	Report         listReport
	DetailStates   map[string]detailBrowserState
	DefaultAction  *string
	PendingAction  *string
	BackendPlan    *backendPlanReport
	LastUpdate     updateReport
	HasLastUpdate  bool
	NextAction     string
	PreviousAction string
}

func runListFilteredSelection(options listFilteredSelectionOptions) (bool, bool) {
	for {
		action, handled := runListFilteredBrowserWithViewActions(options.Title, options.Report, options.DetailStates, textui.ColorEnabled(), options.NextAction, options.PreviousAction)
		if route, ok := parseListRouteAction(action); ok {
			backendPlan := backendPlanReport{}
			if options.BackendPlan != nil {
				backendPlan = *options.BackendPlan
			}
			outcome := runListRouteDetail(listRouteDetailOptions{
				Root:          options.Report.Root,
				Route:         route,
				BackendPlan:   backendPlan,
				LastUpdate:    options.LastUpdate,
				HasLastUpdate: options.HasLastUpdate,
				DetailStates:  options.DetailStates,
				Color:         textui.ColorEnabled(),
			})
			if outcome == listRouteDetailExit {
				return true, true
			}
			if outcome == listRouteDetailHome {
				if options.DefaultAction != nil {
					*options.DefaultAction = listHubActionFull
				}
				if options.PendingAction != nil {
					*options.PendingAction = ""
				}
				return true, false
			}
			if route.Domain == listHubActionBackends {
				if options.BackendPlan != nil {
					*options.BackendPlan = buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: options.Report.Root})
				}
			}
			continue
		}
		return handleListFilteredAction(action, handled, options.DefaultAction, options.PendingAction)
	}
}

func handleListFilteredAction(action string, handled bool, defaultAction *string, pendingAction *string) (bool, bool) {
	switch action {
	case updevActionExit:
		return handled, true
	case updevActionHome:
		if defaultAction != nil {
			*defaultAction = listHubActionFull
		}
		return true, false
	default:
		if listHubActionExists(action) {
			if defaultAction != nil {
				*defaultAction = action
			}
			if pendingAction != nil {
				*pendingAction = action
			}
			return true, false
		}
		return handled, false
	}
}

func runListFilteredBrowser(title string, report listReport, detailStates map[string]detailBrowserState, color bool) (string, bool) {
	return runListFilteredBrowserWithViewActions(title, report, detailStates, color, "", "")
}

func runListFilteredBrowserWithViewActions(title string, report listReport, detailStates map[string]detailBrowserState, color bool, nextAction string, previousAction string) (string, bool) {
	stateKey := listBrowserStateKey(report)
	sections := listTableSections(report)
	if toolTableRowCount(sections) > 0 || nextAction != "" || previousAction != "" {
		actions := tableBrowserActions()
		labels := tableBrowserLabels()
		if nextAction != "" || previousAction != "" {
			actions = tableBrowserActionsWithViewToggle(nextAction, previousAction)
			labels = tableBrowserLabelsWithViewToggle()
		}
		state, err := runToolTableBrowserWithStateAndActions(title, sections, detailStates[stateKey], actions, labels, color)
		if err != nil {
			return "", false
		}
		detailStates[stateKey] = state
		return state.Action, true
	}
	rows := listDetailRows(report)
	if len(rows) == 0 {
		return "", false
	}
	state, err := runDetailBrowserWithState(title, rows, detailStates[stateKey], color)
	if err != nil {
		return "", false
	}
	detailStates[stateKey] = state
	return state.Action, true
}

func runListFilteredDetailBrowser(title string, report listReport, detailStates map[string]detailBrowserState, stateKey string, color bool) (bool, bool) {
	if stateKey == "" {
		action, handled := runListFilteredBrowser(title, report, detailStates, color)
		return handled, action == updevActionExit
	}
	sections := listTableSections(report)
	if toolTableRowCount(sections) == 0 {
		return false, false
	}
	state, err := runToolTableBrowserWithState(title, sections, detailStates[stateKey], color)
	if err != nil {
		return false, false
	}
	detailStates[stateKey] = state
	return true, state.Action == updevActionExit
}

func listBrowserStateKey(report listReport) string {
	if summary := textui.FilterSummary(report.Filters, filterSummaryKeys...); summary != "" {
		return "list:" + summary
	}
	return "list:all"
}

func listTableSections(report listReport) []toolSection {
	sections := itemToolSections(displayListItems(report.Items, report.Sections), report.Evidence)
	sections = append(sections, enrichToolSectionsWithEvidence(listDisplaySections(report), report.Evidence)...)
	return sections
}

func listDisplaySections(report listReport) []toolSection {
	if !listReportIsManualOnly(report) {
		return report.Sections
	}
	return groupManualInstalledAppSections(report.Sections)
}

func listReportIsManualOnly(report listReport) bool {
	return strings.EqualFold(report.Filters["provider"], manualProviderName) ||
		strings.EqualFold(report.Filters["kind"], manualProviderName) ||
		strings.EqualFold(report.Filters["category"], manualProviderName)
}

func enrichToolSectionsWithEvidence(sections []toolSection, evidence plan.EvidenceIndex) []toolSection {
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		section.Rows = append([]toolRow{}, section.Rows...)
		for index, row := range section.Rows {
			items := toolSectionRowEvidenceItems(section, row)
			itemEvidence := plan.ItemEvidence{}
			actions := row.Actions
			for _, item := range items {
				itemEvidence = mergeListItemEvidence(itemEvidence, itemListEvidence(item, evidence))
				actions = reviewui.MergeActions(actions, itemToolRowActions(item, evidence))
			}
			if listItemEvidenceSummary(itemEvidence) != "" {
				row.Detail = itemDetailWithEvidence(row.Detail, itemEvidence)
			}
			row.Actions = actions
			section.Rows[index] = row
		}
		out = append(out, section)
	}
	return out
}

func toolSectionRowEvidenceItems(section toolSection, row toolRow) []plan.Item {
	items := []plan.Item{toolSectionRowEvidenceItem(section, row)}
	if cask := manualinventory.DetailValue(row.Detail, "cask"); cask != "" {
		items = append(items, plan.Item{
			Provider: "brew",
			Kind:     "cask",
			Name:     cask,
			Version:  row.Version,
			Status:   plan.Status(row.State),
		})
	}
	return items
}

func toolSectionRowEvidenceItem(section toolSection, row toolRow) plan.Item {
	provider := section.Name
	category := ""
	if left, right, ok := strings.Cut(section.Name, "/"); ok {
		provider = left
		category = right
	}
	kind := provider
	if provider == "mise" {
		kind = "tool"
	}
	return plan.Item{
		Provider: provider,
		Kind:     kind,
		Category: category,
		Name:     row.Name,
		Version:  row.Version,
		Status:   plan.Status(row.State),
	}
}

func mergeListItemEvidence(left plan.ItemEvidence, right plan.ItemEvidence) plan.ItemEvidence {
	return plan.MergeItemEvidence(left, right)
}

func itemToolSections(items []plan.Item, evidence plan.EvidenceIndex) []toolSection {
	grouped := map[string][]toolRow{}
	order := []string{}
	for _, item := range items {
		key := itemToolSectionKey(item)
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], itemToolRow(item, evidence))
	}
	sections := make([]toolSection, 0, len(order))
	for _, name := range order {
		sections = append(sections, toolSection{Name: name, Title: itemToolSectionTitle(name), Rows: grouped[name]})
	}
	return sections
}

func itemToolSectionKey(item plan.Item) string {
	name := item.Provider + "/" + item.Kind
	if item.Category != "" {
		name += "/" + item.Category
	}
	return name
}

func itemToolSectionTitle(name string) string {
	return strings.ReplaceAll(name, "/", " / ")
}

func itemToolRow(item plan.Item, evidence plan.EvidenceIndex) toolRow {
	itemEvidence := itemListEvidence(item, evidence)
	actions := itemToolRowActions(item, evidence)
	return toolRow{
		Name:    item.Name,
		Version: item.Version,
		State:   inventoryannotate.ItemStatusLabel(item),
		Detail:  inventoryItemDetail(item, itemEvidence, detailActionsFromReviewActions(actions)),
		Actions: actions,
	}
}

func itemToolRowActions(item plan.Item, evidence plan.EvidenceIndex) []reviewui.Action {
	actions := []reviewui.Action{}
	if item.Provider == manualProviderName {
		actions = append(actions, manualReviewRouteAction(item))
	}
	actions = append(actions, brewDriftReviewActions(item)...)
	itemEvidence := itemListEvidence(item, evidence)
	if len(itemEvidence.Backends) > 0 {
		actions = append(actions, reviewui.Action{
			Value:       listRouteActionValue(listHubActionBackends, item),
			Label:       tr("open backend review", "backend 整理を開く"),
			Description: firstNonEmpty(itemEvidence.Backends...),
		})
	}
	if len(itemEvidence.Updates) > 0 {
		badge, badgeStatus := updateEvidenceActionBadge(itemEvidence.Updates)
		actions = append(actions, reviewui.Action{
			Value:       listRouteActionValue(listHubActionUpdates, item),
			Label:       tr("open update evidence", "update evidence を開く"),
			Description: firstNonEmpty(itemEvidence.Updates...),
			Badge:       badge,
			BadgeStatus: badgeStatus,
		})
	}
	if len(itemEvidence.Security) > 0 {
		badge, badgeStatus := securityEvidenceActionBadge(itemEvidence.Security)
		actions = append(actions, reviewui.Action{
			Value:       listRouteActionValue(listHubActionSecurity, item),
			Label:       tr("open security review", "security review を開く"),
			Description: firstNonEmpty(itemEvidence.Security...),
			Badge:       badge,
			BadgeStatus: badgeStatus,
		})
	}
	return actions
}

func brewDriftReviewActions(item plan.Item) []reviewui.Action {
	if !syncreport.HomebrewExtraAdoptable(item) {
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	name := strings.TrimSpace(item.Name)
	if kind == "" || name == "" {
		return nil
	}
	return []reviewui.Action{
		{
			Value:       brewDriftActionValue("adopt", kind, name, "work"),
			Label:       tr("add to Brewfile (work)", "Brewfile に追加(work)"),
			Description: fmt.Sprintf(tr("adopt %s %q into the work category", "%s %q を work category に採用します"), kind, name),
			Badge:       "fix",
			BadgeStatus: string(plan.StatusDrift),
		},
		{
			Value:       brewDriftActionValue("adopt", kind, name, "personal"),
			Label:       tr("add to Brewfile (personal)", "Brewfile に追加(personal)"),
			Description: fmt.Sprintf(tr("adopt %s %q into the personal category", "%s %q を personal category に採用します"), kind, name),
			Badge:       "fix",
			BadgeStatus: string(plan.StatusDrift),
		},
	}
}

func manualReviewRouteAction(item plan.Item) reviewui.Action {
	return manualReviewRouteActionForTarget(item.Name, item.Provider, item.Kind)
}

func manualReviewRouteActionForTarget(name string, provider string, kind string) reviewui.Action {
	return reviewui.Action{
		Value:       listRouteActionValueForTarget(listHubActionManual, provider, kind, name),
		Label:       tr("open manual review", "manual review を開く"),
		Description: tr("open manual app review rows for this item", "この項目に関係する manual app review 行を開きます"),
	}
}

func listRouteActionValue(domain string, item plan.Item) string {
	return listRouteActionValueForTarget(domain, item.Provider, item.Kind, item.Name)
}

func listRouteActionValueForTarget(domain string, provider string, kind string, name string) string {
	return strings.Join([]string{listRouteActionPrefix, domain, provider, kind, name}, "\t")
}

func parseListRouteAction(value string) (listRouteAction, bool) {
	parts := strings.SplitN(value, "\t", 5)
	if len(parts) != 5 || parts[0] != listRouteActionPrefix {
		return listRouteAction{}, false
	}
	return listRouteAction{Domain: parts[1], Provider: parts[2], Kind: parts[3], Name: parts[4]}, true
}

func brewDriftActionValue(action string, kind string, name string, category string) string {
	return strings.Join([]string{brewDriftActionPrefix, action, kind, name, category}, "\t")
}

func parseBrewDriftAction(value string) (action string, kind string, name string, category string, ok bool) {
	parts := strings.SplitN(value, "\t", 5)
	if len(parts) != 5 || parts[0] != brewDriftActionPrefix {
		return "", "", "", "", false
	}
	action = strings.TrimSpace(parts[1])
	kind = strings.TrimSpace(parts[2])
	name = strings.TrimSpace(parts[3])
	category = strings.TrimSpace(parts[4])
	if action == "" || kind == "" || name == "" {
		return "", "", "", "", false
	}
	return action, kind, name, category, true
}

type listRouteDetailOptions struct {
	Root          string
	Route         listRouteAction
	BackendPlan   backendPlanReport
	LastUpdate    updateReport
	HasLastUpdate bool
	DetailStates  map[string]detailBrowserState
	Color         bool
}

func runListRouteDetail(options listRouteDetailOptions) listRouteDetailOutcome {
	route := options.Route
	detailStates := reviewui.EnsureStateCache(options.DetailStates)
	stateKey := "route:" + route.Domain + ":" + route.Provider + ":" + route.Kind + ":" + route.Name
	var rows []detailBrowserRow
	title := routeDetailTitle(route)
	switch route.Domain {
	case listHubActionManual:
		manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: options.Root, provider: manualProviderName, query: route.Name})
		rows = manualPlanDetailRows(manualPlan)
	case listHubActionBackends:
		rows = backendDetailRowsForListRoute(options.BackendPlan, route)
	case listHubActionUpdates:
		if options.HasLastUpdate {
			filtered := filterUpdateReport(options.LastUpdate, lastReportOptions{section: "logs", provider: route.Provider, query: route.Name})
			rows = updateLogDetailRows(filtered)
		}
	case listHubActionSecurity:
		if options.HasLastUpdate {
			opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Name}
			filtered := filterUpdateReport(options.LastUpdate, opts)
			rows = updateSecurityDetailRowsForFilter(filtered, opts)
		}
	default:
		return listRouteDetailBack
	}
	if len(rows) == 0 {
		rows = []detailBrowserRow{emptyRouteDetailRow(route)}
	}
	state, err := runDetailBrowserWithState(title, rows, focusedRouteDetailState(), options.Color)
	if err != nil {
		return listRouteDetailBack
	}
	detailStates[stateKey] = state
	if state.Action == updevActionExit {
		return listRouteDetailExit
	}
	if state.Action == updevActionHome {
		return listRouteDetailHome
	}
	if state.Action == updevActionBack {
		return listRouteDetailBack
	}
	if summaryRoute, ok := parseUpdateSummaryRoute(state.Action); ok && options.HasLastUpdate {
		if runUpdateSummaryRouteDetail(options.LastUpdate, summaryRoute, options.BackendPlan, detailStates, options.Color) {
			return listRouteDetailExit
		}
		return listRouteDetailBack
	}
	switch route.Domain {
	case listHubActionManual:
		_ = handleManualPlanDetailAction(options.Root, state.Action)
	case listHubActionBackends:
		_ = handleBackendDetailAction(options.Root, state.Action)
	case listHubActionSecurity:
		if options.HasLastUpdate {
			_ = handleMiseBumpDetailAction(&options.LastUpdate, state.Action)
			_ = handleSecurityDetailAction(&options.LastUpdate, state.Action)
		}
	}
	return listRouteDetailBack
}

func focusedRouteDetailState() detailBrowserState {
	return detailBrowserState{
		Selected: 0,
		Offset:   0,
		Expanded: map[int]bool{0: true},
	}
}

func routeDetailTitle(route listRouteAction) string {
	target := strings.TrimSpace(route.Name)
	if target == "" {
		target = strings.Trim(strings.Join([]string{route.Provider, route.Kind}, " "), " ")
	}
	switch route.Domain {
	case listHubActionManual:
		return "updev manual review: " + target
	case listHubActionBackends:
		return "updev backend convergence: " + target
	case listHubActionUpdates:
		return "updev update evidence: " + target
	case listHubActionSecurity:
		return "updev security review: " + target
	default:
		return "updev detail: " + target
	}
}

func emptyRouteDetailRow(route listRouteAction) detailBrowserRow {
	return detailBrowserRow{
		Title:   route.Name,
		Status:  "review-only",
		Summary: tr("no matching focused evidence", "対象項目に一致する詳細 evidence はありません"),
		Detail:  tr("The installed inventory row exposed a route, but no focused detail row matched the item. Return and open the full domain list if broader review is needed.", "installed inventory 行から導線はありますが、この項目に絞った詳細行は見つかりません。広く確認する場合は戻って domain 全体を開きます。"),
		Metadata: []string{
			"domain: " + route.Domain,
			"provider: " + route.Provider,
			"kind: " + route.Kind,
			"name: " + route.Name,
		},
	}
}

func backendDetailRowsForListRoute(report backendPlanReport, route listRouteAction) []detailBrowserRow {
	filtered := report
	filtered.Findings = nil
	for _, finding := range report.Findings {
		if backendFindingMatchesListRoute(finding, route) {
			filtered.Findings = append(filtered.Findings, finding)
		}
	}
	return backendDetailRows(filtered)
}

func backendFindingMatchesListRoute(finding backendFinding, route listRouteAction) bool {
	name := plan.EvidenceNameKey(route.Name)
	if name == "" {
		return false
	}
	values := []string{
		finding.Name,
		finding.RecommendedName,
		finding.Kind + ":" + finding.Name,
		finding.Provider + ":" + finding.Name,
	}
	for _, value := range values {
		candidate := plan.EvidenceNameKey(value)
		if candidate == "" {
			continue
		}
		if candidate == name || strings.Contains(candidate, name) || strings.Contains(name, candidate) {
			return true
		}
	}
	return false
}

func listTitleWithEvidenceSummary(title string, report listReport) string {
	summary := listReportEvidenceSummary(report, false)
	if summary == "" {
		return title
	}
	return title + " [" + summary + "]"
}

func listReportEvidenceSummary(report listReport, color bool) string {
	updates, security, backends := listReportEvidenceCounts(report)
	return listEvidenceCountSummary(updates, security, backends, color)
}

func listEvidenceSummary(evidence plan.EvidenceIndex, color bool) string {
	updates, security, backends := plan.EvidenceCounts(evidence)
	return listEvidenceCountSummary(updates, security, backends, color)
}

func listEvidenceCountSummary(updates int, security int, backends int, color bool) string {
	return fmt.Sprintf(
		"%s=%s %s=%s %s=%s",
		textui.StyleRequested("upd", color),
		textui.StyleCount(fmt.Sprint(updates), color),
		textui.StyleRequested("sec", color),
		textui.StyleCount(fmt.Sprint(security), color),
		textui.StyleRequested("bak", color),
		textui.StyleCount(fmt.Sprint(backends), color),
	)
}

func listReportEvidenceCounts(report listReport) (int, int, int) {
	updateKeys := map[string]bool{}
	securityKeys := map[string]bool{}
	backendKeys := map[string]bool{}
	for _, section := range listTableSections(report) {
		for _, row := range section.Rows {
			for _, action := range row.Actions {
				route, ok := parseListRouteAction(action.Value)
				if !ok {
					continue
				}
				key := listEvidenceActionCountKey(route, action)
				switch route.Domain {
				case listHubActionUpdates:
					updateKeys[key] = true
				case listHubActionSecurity:
					securityKeys[key] = true
				case listHubActionBackends:
					backendKeys[key] = true
				}
			}
		}
	}
	return len(updateKeys), len(securityKeys), len(backendKeys)
}

func listEvidenceActionCountKey(route listRouteAction, action reviewui.Action) string {
	key := strings.Join([]string{route.Domain, route.Provider, route.Kind, route.Name}, "\x00")
	if strings.Trim(key, "\x00") != "" {
		return key
	}
	return strings.TrimSpace(action.Value + "\x00" + action.Description)
}

func buildListEvidenceIndex(root string) plan.EvidenceIndex {
	index := plan.NewEvidenceIndex()
	entry, ok := loadLastUpdateReport()
	if !ok || !plan.EvidenceRootsMatch(root, entry.Report.Root) {
		return index
	}
	for _, step := range entry.Report.Steps {
		for _, item := range step.Updated {
			detail := fmt.Sprintf("%s updated: %s", step.Name, strings.TrimSpace(item))
			for _, key := range plan.EvidenceUpdateItemKeys(step.Name, item, miseBumpProvider) {
				plan.AddEvidence(index.Updates, key, detail)
			}
		}
		for _, item := range step.SkippedItems {
			status := "deferred"
			if step.Status == plan.StatusHeld {
				status = "held"
			}
			detail := firstNonEmpty(step.Reason, status)
			for _, key := range plan.EvidenceUpdateItemKeys(step.Name, item, miseBumpProvider) {
				plan.AddEvidence(index.Updates, key, fmt.Sprintf("%s %s: %s", step.Name, status, detail))
			}
		}
	}
	for _, gate := range entry.Report.Safety {
		for _, finding := range gate.Findings {
			detail := listSecurityEvidenceDetail(entry.Report, gate, finding)
			for _, key := range listEvidenceFindingKeys(gate, finding) {
				plan.AddEvidence(index.Security, key, fmt.Sprintf("%s/%s %s: %s", firstNonEmpty(finding.Provider, gate.Provider), finding.Kind, finding.Name, detail))
			}
		}
	}
	return index
}

func listSecurityEvidenceDetail(report updateReport, gate safetyGate, finding safetyFinding) string {
	decision := strings.TrimSpace(firstNonEmpty(finding.Decision, string(gate.Status), finding.Reason))
	if decision == "" {
		decision = "security review"
	}
	reason := strings.TrimSpace(localizedSafetyReasonWithReleaseAge(finding))
	context := listSecurityEvidenceContext(finding)
	if report.Security == "strict" && gate.Status == plan.StatusHeld && securityDecisionNeedsAttention(decision) {
		if strings.EqualFold(decision, "hold") || strings.EqualFold(decision, "held") {
			return listEvidenceDetailWithContext(listEvidenceDetailWithReason(decision, reason), context)
		}
		return listEvidenceDetailWithContext(listEvidenceDetailWithReason("held (decision: "+decision+")", reason), context)
	}
	return listEvidenceDetailWithContext(listEvidenceDetailWithReason(decision, reason), context)
}

func listEvidenceDetailWithReason(detail string, reason string) string {
	detail = strings.TrimSpace(detail)
	reason = strings.TrimSpace(reason)
	if reason == "" || strings.EqualFold(reason, detail) {
		return detail
	}
	return detail + ": " + oneLine(reason)
}

func listEvidenceDetailWithContext(detail string, context []string) string {
	detail = strings.TrimSpace(detail)
	if len(context) == 0 {
		return detail
	}
	context = nonEmptyStrings(context...)
	if len(context) == 0 {
		return detail
	}
	if detail == "" {
		return strings.Join(context, "; ")
	}
	return detail + "; " + strings.Join(context, "; ")
}

func listSecurityEvidenceContext(finding safetyFinding) []string {
	lines := []string{}
	if releaseAge := safetyFindingReleaseAgeSummary(finding); releaseAge != "" {
		lines = append(lines, tr("release age: ", "リリース経過: ")+releaseAge)
	}
	if cache := safetyFindingCacheEvidenceSummary(finding); cache != "" {
		lines = append(lines, tr("cache: ", "キャッシュ: ")+cache)
	}
	if finding.ReleaseDate != "" {
		lines = append(lines, tr("release date: ", "リリース日: ")+finding.ReleaseDate)
	}
	if finding.PublishedDate != "" {
		lines = append(lines, tr("published: ", "公開日: ")+finding.PublishedDate)
	}
	if finding.LastUpdated != "" {
		lines = append(lines, tr("last updated: ", "最終更新: ")+finding.LastUpdated)
	}
	if finding.Source != "" {
		lines = append(lines, tr("source: ", "source: ")+finding.Source)
	}
	if finding.Tap != "" {
		lines = append(lines, tr("tap: ", "tap: ")+finding.Tap)
	}
	if finding.Publisher != "" {
		lines = append(lines, tr("publisher: ", "publisher: ")+finding.Publisher)
	}
	if finding.RepositoryURL != "" {
		lines = append(lines, tr("repository: ", "repository: ")+finding.RepositoryURL)
	}
	if finding.SupportURL != "" {
		lines = append(lines, tr("support: ", "support: ")+finding.SupportURL)
	}
	if finding.Homepage != "" {
		lines = append(lines, tr("homepage: ", "homepage: ")+finding.Homepage)
	}
	if finding.URL != "" {
		lines = append(lines, tr("download: ", "download: ")+finding.URL)
	}
	if finding.HomepageHost != "" {
		lines = append(lines, tr("homepage host: ", "homepage host: ")+finding.HomepageHost)
	}
	if finding.URLHost != "" {
		lines = append(lines, tr("download host: ", "download host: ")+finding.URLHost)
	}
	return lines
}

func addBackendListEvidence(index plan.EvidenceIndex, report backendPlanReport) plan.EvidenceIndex {
	if index.Backends == nil {
		index.Backends = map[string][]string{}
	}
	for _, finding := range report.Findings {
		detail := strings.TrimSpace(backendFindingEvidenceText(finding))
		if detail == "" {
			detail = tr("backend convergence review", "backend 整理の確認")
		}
		for _, key := range listEvidenceBackendFindingKeys(finding) {
			plan.AddEvidence(index.Backends, key, detail)
		}
	}
	return index
}

func listEvidenceBackendFindingKeys(finding backendFinding) []string {
	keys := []string{}
	add := func(key string) {
		if strings.TrimSpace(key) == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}
	addRef := func(provider string, kind string, name string) {
		add(plan.EvidenceExactKey(provider, kind, name))
		add(plan.EvidenceProviderNameKey(provider, name))
		add(plan.EvidenceNameKey(name))
	}
	addRef(finding.Provider, finding.Kind, finding.Name)
	addRef(finding.Provider, finding.Kind, finding.Current)
	recommendedKind := finding.Kind
	if finding.RecommendedProvider == "mise" {
		recommendedKind = "tool"
	}
	addRef(finding.RecommendedProvider, recommendedKind, finding.RecommendedName)
	for _, command := range finding.CommandNames {
		add(plan.EvidenceNameKey(command))
	}
	return keys
}

func listEvidenceFindingKeys(gate safetyGate, finding safetyFinding) []string {
	provider := firstNonEmpty(finding.Provider, gate.Provider)
	keys := []string{}
	add := func(key string) {
		if strings.TrimSpace(key) == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}
	for _, name := range plan.EvidenceItemNameCandidates(finding.Name) {
		add(plan.EvidenceExactKey(provider, finding.Kind, name))
		add(plan.EvidenceProviderNameKey(provider, name))
		if strings.TrimSpace(provider) == "" {
			add(plan.EvidenceNameKey(name))
		}
		if strings.EqualFold(provider, "brew") {
			add(plan.EvidenceExactKey(provider, "brew", name))
			add(plan.EvidenceExactKey(provider, "cask", name))
			add(plan.EvidenceExactKey(provider, "tap", name))
		}
	}
	if finding.Tap != "" {
		add(plan.EvidenceExactKey(provider, "tap", finding.Tap))
		add(plan.EvidenceProviderNameKey(provider, finding.Tap))
		if strings.TrimSpace(provider) == "" {
			add(plan.EvidenceNameKey(finding.Tap))
		}
	}
	return keys
}

func itemListEvidence(item plan.Item, index plan.EvidenceIndex) plan.ItemEvidence {
	return plan.ItemEvidenceFor(item, index)
}

func listItemEvidenceMetadata(e plan.ItemEvidence) []string {
	metadata := []string{}
	for _, value := range e.Updates {
		metadata = append(metadata, tr("update evidence: ", "更新根拠: ")+localizedListEvidenceText(value))
	}
	for _, value := range e.Security {
		metadata = append(metadata, tr("security evidence: ", "セキュリティ根拠: ")+localizedListEvidenceText(value))
	}
	for _, value := range e.Backends {
		metadata = append(metadata, tr("backend evidence: ", "backend 根拠: ")+localizedListEvidenceText(value))
	}
	return metadata
}

func listItemEvidenceCompactMetadata(e plan.ItemEvidence) []string {
	metadata := []string{}
	if text := compactListEvidenceGroup(e.Updates); text != "" {
		metadata = append(metadata, tr("updates: ", "更新: ")+text)
	}
	if text := compactListEvidenceGroup(e.Security); text != "" {
		metadata = append(metadata, tr("security: ", "セキュリティ: ")+text)
	}
	if text := compactListEvidenceGroup(e.Backends); text != "" {
		metadata = append(metadata, tr("backend: ", "backend: ")+text)
	}
	return metadata
}

func compactListEvidenceGroup(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := []string{compactListEvidenceText(values[0])}
	if len(values) > 1 {
		parts = append(parts, fmt.Sprintf(tr("+%d more", "+%d 件"), len(values)-1))
	}
	return strings.Join(parts, ", ")
}

func compactListEvidenceText(value string) string {
	if text := compactKnownListEvidenceText(value); text != "" {
		return text
	}
	text := localizedListEvidenceText(value)
	text = strings.Join(strings.Fields(text), " ")
	for _, delimiter := range []string{"; source:", "; tap:", "; homepage:", "; download:", "; homepage host:", "; download host:", "; cache:", "; release age:", "; キャッシュ:", "; リリース経過:"} {
		if before, _, ok := strings.Cut(text, delimiter); ok {
			text = strings.TrimSpace(before)
		}
	}
	return truncate(text, 96)
}

func compactKnownListEvidenceText(value string) string {
	raw := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if raw == "" {
		return ""
	}
	if text := compactStrictSafetySummary(raw); text != "" {
		return text
	}
	if text := compactStrictGateSummary(raw); text != "" {
		return text
	}
	if text := compactReleaseAgeHoldSummary(raw); text != "" {
		return text
	}
	if text := compactBackendCandidateSummary(raw); text != "" {
		return text
	}
	return ""
}

func compactStrictSafetySummary(value string) string {
	const marker = "strict safety will apply "
	start := strings.Index(value, marker)
	if start < 0 {
		return ""
	}
	after := value[start+len(marker):]
	safeCount, rest, ok := strings.Cut(after, " safe Homebrew candidates and hold ")
	if !ok {
		return ""
	}
	holdCount, _, ok := strings.Cut(rest, " unsafe candidates")
	if !ok {
		return ""
	}
	if defaultLanguage() == "ja" {
		return fmt.Sprintf("安全 %s / 保留 %s (Homebrew)", strings.TrimSpace(safeCount), strings.TrimSpace(holdCount))
	}
	return fmt.Sprintf("safe %s / held %s (Homebrew)", strings.TrimSpace(safeCount), strings.TrimSpace(holdCount))
}

func compactReleaseAgeHoldSummary(value string) string {
	if !strings.Contains(value, "候補リリースが新しすぎます") &&
		!strings.Contains(value, "candidate release is too new") &&
		!(strings.Contains(value, "経過 ") && strings.Contains(value, "最小 ")) {
		return ""
	}
	age := compactFieldAfter(value, "経過 ", "日")
	minimum := compactFieldAfter(value, "最小 ", "日")
	remaining := compactFieldAfter(value, "あと約", "日")
	if age == "" {
		age = compactFieldAfter(value, "age ", " days")
	}
	if minimum == "" {
		minimum = compactFieldAfter(value, "minimum ", " days")
	}
	if remaining != "" && age != "" && minimum != "" {
		if defaultLanguage() == "ja" {
			return fmt.Sprintf("リリース待ち: あと約%s日 (%s/%s日)", remaining, age, minimum)
		}
		return fmt.Sprintf("release-age hold: about %s days left (%s/%s days)", remaining, age, minimum)
	}
	if age != "" && minimum != "" {
		if defaultLanguage() == "ja" {
			return fmt.Sprintf("リリース待ち: %s/%s日", age, minimum)
		}
		return fmt.Sprintf("release-age hold: %s/%s days", age, minimum)
	}
	if defaultLanguage() == "ja" {
		return "リリース待ち"
	}
	return "release-age hold"
}

func compactStrictGateSummary(value string) string {
	if strings.Contains(value, "security=strict held update because safety gate requires review") ||
		strings.Contains(value, "security=strict のため更新を保留しました") {
		if defaultLanguage() == "ja" {
			return "安全確認待ち"
		}
		return "waiting for safety review"
	}
	return ""
}

func compactBackendCandidateSummary(value string) string {
	if before, _, ok := strings.Cut(value, " は候補としてのみ確認"); ok {
		name := strings.TrimSpace(before)
		if name != "" {
			label := "backend候補"
			if strings.HasPrefix(strings.ToLower(name), "github:") {
				label = "GitHub候補"
			}
			return fmt.Sprintf("%s %s (要確認)", label, truncate(name, 42))
		}
	}
	if before, _, ok := strings.Cut(value, " is reviewed as a candidate only"); ok {
		name := strings.TrimSpace(before)
		if name != "" {
			return fmt.Sprintf("GitHub candidate %s (review)", truncate(name, 42))
		}
	}
	if before, _, ok := strings.Cut(value, " as a candidate only"); ok && strings.Contains(before, "github:") {
		fields := strings.Fields(before)
		name := fields[len(fields)-1]
		return fmt.Sprintf("GitHub candidate %s (review)", truncate(name, 42))
	}
	return ""
}

func compactFieldAfter(value string, prefix string, suffix string) string {
	start := strings.Index(value, prefix)
	if start < 0 {
		return ""
	}
	after := value[start+len(prefix):]
	end := strings.Index(after, suffix)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(after[:end])
}

func listItemEvidenceSummary(e plan.ItemEvidence) string {
	parts := []string{}
	if len(e.Updates) > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d update evidence", "%d 件の update evidence"), len(e.Updates)))
	}
	if len(e.Security) > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d security evidence", "%d 件の security evidence"), len(e.Security)))
	}
	if len(e.Backends) > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d backend evidence", "%d 件の backend evidence"), len(e.Backends)))
	}
	return strings.Join(parts, ", ")
}

func itemDetailWithEvidence(detail string, evidence plan.ItemEvidence) string {
	parts := []string{}
	if strings.TrimSpace(detail) != "" {
		parts = append(parts, strings.TrimSpace(detail))
	}
	parts = append(parts, listItemEvidenceMetadata(evidence)...)
	return strings.Join(parts, "\n")
}

func inventoryItemDetail(item plan.Item, evidence plan.ItemEvidence, actions []detailBrowserAction) string {
	parts := []string{}
	if strings.TrimSpace(item.Detail) != "" {
		parts = append(parts, tr("description: ", "説明: ")+localizedBuiltInNoteText(item.Detail))
	} else {
		parts = append(parts, tr("summary: ", "概要: ")+inventoryItemStatusSummary(item))
	}
	parts = append(parts, tr("status: ", "状態: ")+inventoryItemStatusSummary(item))
	parts = append(parts, tr("managed by: ", "管理: ")+inventoryItemIdentity(item))
	if strings.TrimSpace(item.Category) != "" {
		parts = append(parts, tr("category: ", "カテゴリ: ")+item.Category)
	}
	if summary := compactEvidenceBadgeSummary(evidence); summary != "" {
		parts = append(parts, tr("evidence: ", "確認: ")+summary)
	}
	parts = append(parts, listItemEvidenceCompactMetadata(evidence)...)
	if len(actions) > 0 {
		parts = append(parts, fmt.Sprintf(tr("actions: %d available below", "操作: 下の actions から %d 件選択できます"), len(actions)))
	}
	return strings.Join(parts, "\n")
}

func compactEvidenceBadgeSummary(e plan.ItemEvidence) string {
	parts := []string{}
	if len(e.Updates) > 0 {
		parts = append(parts, fmt.Sprintf("upd %d", len(e.Updates)))
	}
	if len(e.Security) > 0 {
		parts = append(parts, fmt.Sprintf("sec %d", len(e.Security)))
	}
	if len(e.Backends) > 0 {
		parts = append(parts, fmt.Sprintf("bak %d", len(e.Backends)))
	}
	return strings.Join(parts, " / ")
}

func localizedBuiltInNoteText(value string) string {
	value = strings.TrimSpace(value)
	if defaultLanguage() != "ja" {
		return value
	}
	switch value {
	case "keep macOS/system git available":
		return "macOS/system git を使える状態に保つ"
	default:
		return value
	}
}

func localizedListEvidenceText(value string) string {
	value = strings.TrimSpace(value)
	if defaultLanguage() != "ja" {
		return value
	}
	value = localizedListEvidencePrefixes(value)
	switch value {
	case "backend convergence review":
		return "backend 整理の確認"
	case "mise bump candidates available; mode=manual requires item review":
		return "mise の更新候補があります。mode=manual のため項目ごとの確認が必要です"
	case "mise bump candidates require review; no safe auto candidates":
		return "mise の更新候補は確認が必要です。自動適用できる安全な候補はありません"
	case "mise bump applied 1 safe candidates; 18 candidates require review":
		return "安全な更新候補 1 件を適用済みです。18 件は確認が必要です"
	case "mise backend is unsupported or opaque for updev-owned release-age evidence":
		return "mise のバックエンドから updev がリリース経過日数の根拠を十分に確認できないため、確認が必要です"
	case "mise minimum_release_age held candidate before it appeared in normal outdated output":
		return "mise のリリース経過日数ゲートにより、通常の更新候補一覧に出る前の候補を保留しています"
	default:
		value = localizedListEvidenceDynamicText(value)
		return localizedBackendReasonText(value)
	}
}

func localizedListEvidencePrefixes(value string) string {
	replacements := []struct {
		old string
		new string
	}{
		{"mise-bump held: ", "mise-bump 保留: "},
		{"mise-bump deferred: ", "mise-bump 見送り: "},
		{"mise-bump skipped: ", "mise-bump skipped: "},
		{"mise-bump updated: ", "mise-bump 更新: "},
		{"brew held: ", "brew 保留: "},
		{"brew deferred: ", "brew 見送り: "},
		{"brew skipped: ", "brew skipped: "},
		{"brew updated: ", "brew 更新: "},
	}
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.old, replacement.new)
	}
	return value
}

func localizedListEvidenceDynamicText(value string) string {
	value = localizedMiseBumpAppliedEvidenceText(value)
	value = localizedHomebrewStrictSafetyEvidenceText(value)
	replacements := []struct {
		old string
		new string
	}{
		{"mise bump candidates available; mode=manual requires item review", "mise の更新候補があります。mode=manual のため項目ごとの確認が必要です"},
		{"mise bump candidates require review; no safe auto candidates", "mise の更新候補は確認が必要です。自動適用できる安全な候補はありません"},
		{"held (decision: review): ", "保留（判定: 確認）: "},
		{"held (decision: hold): ", "保留（判定: 保留）: "},
		{"hold: ", "保留: "},
		{"allow: ", "許可: "},
		{"review: ", "確認: "},
		{"mise backend is unsupported or opaque for updev-owned release-age evidence", "mise のバックエンドから updev がリリース経過日数の根拠を十分に確認できないため、確認が必要です"},
		{"mise minimum_release_age held candidate before it appeared in normal outdated output", "mise のリリース経過日数ゲートにより、通常の更新候補一覧に出る前の候補を保留しています"},
		{"mise minimum_release_age held newer candidate ", "mise のリリース経過日数ゲートにより新しい候補 "},
		{"; normal age-gated candidate is ", " を保留しています。通常の経過日数判定で許可された候補は "},
		{"mise pinned-version bump candidate passed release-age and provenance checks", "mise の固定バージョン更新候補はリリース経過日数と配布元確認を通過しました"},
	}
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.old, replacement.new)
	}
	return value
}

func localizedHomebrewStrictSafetyEvidenceText(value string) string {
	const prefix = "strict safety will apply "
	start := strings.Index(value, prefix)
	if start < 0 {
		return value
	}
	after := value[start+len(prefix):]
	safeCount, rest, ok := strings.Cut(after, " safe Homebrew candidates and hold ")
	if !ok {
		return value
	}
	holdCount, suffix, ok := strings.Cut(rest, " unsafe candidates")
	if !ok {
		return value
	}
	japanese := updatereason.LocalizeJapanese(updatereason.StrictBrewPartialReason(mustAtoi(safeCount), mustAtoi(holdCount)))
	suffix = strings.ReplaceAll(suffix, "; Homebrew cannot generally install an older intermediate release", "")
	return value[:start] + japanese + suffix
}

func mustAtoi(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func localizedMiseBumpAppliedEvidenceText(value string) string {
	const prefix = "mise bump applied "
	start := strings.Index(value, prefix)
	if start < 0 {
		return value
	}
	after := value[start+len(prefix):]
	safeCount, rest, ok := strings.Cut(after, " safe candidates; ")
	if !ok {
		return value
	}
	reviewCount, suffix, ok := strings.Cut(rest, " candidates require review")
	if !ok {
		return value
	}
	japanese := fmt.Sprintf("安全な更新候補 %s 件を適用済みです。%s 件は確認が必要です", strings.TrimSpace(safeCount), strings.TrimSpace(reviewCount))
	return value[:start] + japanese + suffix
}

func releaseAgeHoldAvailabilityText(finding safetyFinding) string {
	timing, ok := releaseAgeHoldTiming(finding)
	if !ok {
		return ""
	}
	if timing.remainingDays > 0 {
		return fmt.Sprintf("経過 %d日、最小 %d日。リリース日: %s。解除目安は %s（あと約%d日）です", timing.ageDays, finding.MinReleaseAgeDays, timing.releaseDate, timing.availableDate, timing.remainingDays)
	}
	return fmt.Sprintf("経過 %d日、最小 %d日。リリース日: %s。%s 以降に解除対象です", timing.ageDays, finding.MinReleaseAgeDays, timing.releaseDate, timing.availableDate)
}

func releaseAgeHoldDateAvailabilityText(finding safetyFinding) string {
	timing, ok := releaseAgeHoldTiming(finding)
	if !ok {
		return ""
	}
	if timing.remainingDays > 0 {
		return fmt.Sprintf("リリース日: %s。解除目安は %s（あと約%d日）です", timing.releaseDate, timing.availableDate, timing.remainingDays)
	}
	return fmt.Sprintf("リリース日: %s。%s 以降に解除対象です", timing.releaseDate, timing.availableDate)
}

type releaseAgeTiming struct {
	releaseDate   string
	availableDate string
	ageDays       int
	remainingDays int
}

func releaseAgeHoldTiming(finding safetyFinding) (releaseAgeTiming, bool) {
	if defaultLanguage() != "ja" || finding.ReleaseDate == "" || finding.MinReleaseAgeDays <= 0 || !releaseAgeHoldFindingNeedsAvailability(finding) {
		return releaseAgeTiming{}, false
	}
	releasedAt, err := time.Parse(time.RFC3339, finding.ReleaseDate)
	if err != nil {
		return releaseAgeTiming{}, false
	}
	availableAt := releasedAt.Add(time.Duration(finding.MinReleaseAgeDays) * 24 * time.Hour)
	now := time.Now()
	timing := releaseAgeTiming{
		releaseDate:   releasedAt.Local().Format("2006-01-02"),
		availableDate: availableAt.Local().Format("2006-01-02"),
		ageDays:       int(now.Sub(releasedAt).Hours() / 24),
	}
	if timing.ageDays < 0 {
		timing.ageDays = 0
	}
	if now.Before(availableAt) {
		hours := int(availableAt.Sub(now).Hours())
		days := hours / 24
		if hours%24 != 0 {
			days++
		}
		if days < 1 {
			days = 1
		}
		timing.remainingDays = days
	}
	return timing, true
}

func releaseAgeHoldFindingNeedsAvailability(finding safetyFinding) bool {
	decision := strings.ToLower(strings.TrimSpace(finding.Decision))
	if decision == "hold" || decision == "held" {
		return true
	}
	reason := strings.ToLower(strings.TrimSpace(finding.Reason))
	return strings.Contains(reason, "minimum_release_age held") || strings.Contains(reason, "release is too new")
}

func inventoryItemIdentity(item plan.Item) string {
	parts := []string{item.Provider, item.Kind, item.Name}
	out := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, " / ")
}

func inventoryItemStatusSummary(item plan.Item) string {
	return inventoryannotate.ItemStatusLabel(item) + " - " + inventoryItemManagementSummary(item)
}

func inventoryItemManagementSummary(item plan.Item) string {
	switch {
	case item.Desired && item.Live:
		return tr("desired and installed", "desired かつ installed")
	case item.Desired && !item.Live:
		return tr("desired but missing locally", "desired だが local にありません")
	case !item.Desired && item.Live:
		return tr("installed but not in desired state", "installed だが desired state にはありません")
	default:
		return tr("not desired and not installed", "desired でも installed でもありません")
	}
}

func detailActionSummary(action detailBrowserAction) string {
	summary := strings.TrimSpace(action.Label)
	if strings.TrimSpace(action.Description) != "" {
		summary += " - " + localizedListEvidenceText(action.Description)
	}
	return summary
}

func derivedListReport(report listReport, opts listOptions) listReport {
	if opts.root == "" {
		opts.root = report.Root
	}
	result := inventoryResult{
		Cached: report.Cached,
		Report: plan.Report{
			Status:    report.Status,
			Root:      report.Root,
			Providers: report.Providers,
			Items:     report.Items,
		},
	}
	derived := buildListReport(result, opts)
	derived.CacheAge = report.CacheAge
	derived.Evidence = report.Evidence
	return derived
}

func displayListItems(items []plan.Item, sections []toolSection) []plan.Item {
	if !hasMiseSections(sections) {
		return items
	}
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		if item.Provider == "mise" && item.Kind == "tool" && item.Status == plan.StatusOK {
			continue
		}
		out = append(out, item)
	}
	return out
}

func listDetailRows(report listReport) []detailBrowserRow {
	rows := []detailBrowserRow{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		rows = append(rows, itemDetailRow(item, report.Evidence))
	}
	for _, section := range enrichToolSectionsWithEvidence(listDisplaySections(report), report.Evidence) {
		for _, row := range limitedToolRows(section.Rows, report.Limit) {
			rows = append(rows, toolDetailRow(section, row))
		}
	}
	return rows
}

func itemDetailRow(item plan.Item, evidence plan.EvidenceIndex) detailBrowserRow {
	metadata := []string{
		"status: " + inventoryannotate.ItemStatusLabel(item),
		"provider: " + item.Provider,
		"kind: " + item.Kind,
		"name: " + item.Name,
		fmt.Sprintf("desired: %t", item.Desired),
		fmt.Sprintf("live: %t", item.Live),
	}
	if item.Version != "" {
		metadata = append(metadata, "version: "+item.Version)
	}
	if item.Category != "" {
		metadata = append(metadata, "category: "+item.Category)
	}
	itemEvidence := itemListEvidence(item, evidence)
	metadata = append(metadata, listItemEvidenceMetadata(itemEvidence)...)
	actions := detailActionsFromReviewActions(itemToolRowActions(item, evidence))
	metadata = append(metadata, actionRouteEvidence(actions)...)
	return detailBrowserRow{
		Title:    item.Provider + "/" + item.Kind + " " + item.Name,
		Status:   inventoryannotate.ItemStatusLabel(item),
		Summary:  firstNonEmpty(item.Detail, listItemEvidenceSummary(itemEvidence), item.Version),
		Detail:   inventoryItemDetail(item, itemEvidence, actions),
		Metadata: metadata,
		Actions:  actions,
	}
}

func toolDetailRow(section toolSection, row toolRow) detailBrowserRow {
	state := firstNonEmpty(row.State, "active")
	metadata := []string{"section: " + section.Title}
	if row.Version != "" {
		metadata = append(metadata, "version: "+row.Version)
	}
	if row.Wanted != "" {
		metadata = append(metadata, "wanted: "+row.Wanted)
	}
	actions := detailActionsFromReviewActions(row.Actions)
	metadata = append(metadata, actionRouteEvidence(actions)...)
	return detailBrowserRow{
		Title:    section.Title + " " + row.Name,
		Status:   state,
		Summary:  firstNonEmpty(row.Detail, row.Version),
		Detail:   row.Detail,
		Metadata: metadata,
		Actions:  actions,
	}
}

func detailActionsFromReviewActions(actions []reviewui.Action) []detailBrowserAction {
	out := make([]detailBrowserAction, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Value) == "" || strings.TrimSpace(action.Label) == "" {
			continue
		}
		out = append(out, detailBrowserAction{
			Value:       action.Value,
			Label:       action.Label,
			Description: action.Description,
		})
	}
	return out
}

func updateEvidenceActionBadge(values []string) (string, string) {
	for _, value := range values {
		if badge, status, ok := updateEvidenceActionBadgeForValue(value); ok {
			return badge, status
		}
	}
	return "upd", ""
}

func updateEvidenceActionBadgeForValue(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, " updated:"):
		if delta := updateEvidenceVersionDelta(value); delta != "" {
			return "up " + delta, "updated", true
		}
		return "up", "updated", true
	case strings.Contains(lower, " held:"), strings.Contains(lower, " deferred:"), strings.Contains(lower, " skipped:"):
		return "upd", "held", true
	default:
		return "", "", false
	}
}

func securityEvidenceActionBadge(values []string) (string, string) {
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(lower, ": hold") || strings.Contains(lower, ": held") || strings.Contains(lower, " decision: hold") || strings.Contains(lower, " decision: held") {
			return "sec", "held"
		}
	}
	return "sec", ""
}

func updateEvidenceVersionDelta(value string) string {
	_, detail, ok := strings.Cut(value, ":")
	if !ok {
		return ""
	}
	left, right, ok := strings.Cut(detail, "->")
	if !ok {
		return ""
	}
	from := updateEvidenceLastField(left)
	to := updateEvidenceFirstField(right)
	if from == "" || to == "" {
		return ""
	}
	if strings.EqualFold(from, to) || updateEvidenceVersionSymbolic(from) || updateEvidenceVersionSymbolic(to) {
		return ""
	}
	return from + "→" + to
}

func updateEvidenceVersionSymbolic(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "latest", "nightly", "head", "main", "master", "trunk", "stable", "edge", "snapshot", "dev", "canary":
		return true
	default:
		return false
	}
}

func updateEvidenceFirstField(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"'(),;`)
}

func updateEvidenceLastField(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[len(fields)-1], `"'(),;`)
}

func actionRouteEvidence(actions []detailBrowserAction) []string {
	if len(actions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(actions))
	for _, action := range actions {
		detail := strings.TrimSpace(action.Label)
		if strings.TrimSpace(action.Description) != "" {
			detail += " - " + localizedListEvidenceText(action.Description)
		}
		lines = append(lines, tr("next action: ", "次の操作: ")+detail)
	}
	return lines
}

func listKindCounts(report listReport) map[string]int {
	counts := map[string]int{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		if item.Kind != "" {
			counts[item.Kind]++
		}
	}
	for _, section := range report.Sections {
		if len(section.Rows) == 0 {
			continue
		}
		kind := "tool"
		if strings.HasPrefix(section.Name, "manual/") {
			kind = manualProviderName
		}
		counts[kind] += len(section.Rows)
	}
	return counts
}

func listCategoryCounts(report listReport) map[string]int {
	counts := map[string]int{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		if item.Category != "" {
			counts[item.Category]++
		}
	}
	for _, section := range report.Sections {
		category := toolSectionCategory(section)
		if category != "" {
			counts[category] += len(section.Rows)
		}
	}
	return counts
}

func listCategorySummary(report listReport, color bool) string {
	counts := listCategoryCounts(report)
	keys := sortedMapKeys(counts)
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", textui.StyleRequested(key, color), counts[key]))
	}
	return strings.Join(parts, ", ")
}

func listCategoryMeaningSummary(report listReport, color bool) string {
	counts := listCategoryCounts(report)
	parts := []string{}
	for _, key := range []string{"work", "personal", "linux", "common"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%s", textui.StyleRequested(key, color), categoryDescription(key)))
		}
	}
	if counts[manualProviderName] > 0 {
		parts = append(parts, fmt.Sprintf("%s=%s", textui.StyleRequested(manualProviderName, color), categoryDescription(manualProviderName)))
	}
	if hasBackendCategory(counts) {
		parts = append(parts, tr("other categories are provider/backend groups", "その他は provider/backend の分類"))
	}
	return strings.Join(parts, "; ")
}

func hasBackendCategory(counts map[string]int) bool {
	scope := map[string]bool{"work": true, "personal": true, "linux": true, "common": true, manualProviderName: true}
	for key, count := range counts {
		if count > 0 && !scope[strings.ToLower(strings.TrimSpace(key))] {
			return true
		}
	}
	return false
}

func categoryDescription(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "common":
		return tr("legacy shared category; treat as work for Homebrew", "旧 shared category。Homebrew では work 扱い")
	case "work":
		return tr("baseline macOS desired state, also included by personal", "macOS 基本 desired state。personal にも含まれます")
	case "personal":
		return tr("personal-only additions on top of work", "work に追加される個人用 profile 専用")
	case "linux":
		return tr("Linux-only desired state", "Linux 専用")
	case "runtime":
		return tr("language runtimes and SDKs", "言語 runtime / SDK")
	case "core":
		return tr("core CLI tools", "基本 CLI ツール")
	case "aqua":
		return tr("mise aqua-backed CLI tools", "mise aqua backend の CLI")
	case "cargo":
		return tr("Rust cargo-backed CLI tools", "Rust cargo backend の CLI")
	case "github":
		return tr("GitHub release-backed tools", "GitHub release backend のツール")
	case "npm":
		return tr("npm-backed global tools", "npm backend の global tool")
	case "pipx":
		return tr("Python pipx-backed tools", "Python pipx backend のツール")
	case "vfox":
		return tr("vfox-backed runtimes", "vfox backend の runtime")
	case "manual":
		return tr("read-only manual app inventory", "read-only の手動管理アプリ inventory")
	default:
		if category == "" {
			return tr("uncategorized", "未分類")
		}
		return tr("provider-defined category", "provider 定義の category")
	}
}

func sortedMapKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toolSectionCategory(section toolSection) string {
	if before, after, ok := strings.Cut(section.Name, "/"); ok && before == "mise" {
		return after
	}
	if strings.HasPrefix(section.Name, "manual/") {
		return manualProviderName
	}
	return section.Name
}

func hasMiseSections(sections []toolSection) bool {
	for _, section := range sections {
		if strings.HasPrefix(section.Name, "mise/") {
			return true
		}
	}
	return false
}

func styledToolRow(row toolRow, includeWanted bool, color bool) []string {
	return reviewui.StyledRow(row, includeWanted, color)
}

func toolRowDimmed(row toolRow) bool {
	return reviewui.RowDimmed(row)
}

func styleVersion(value string, active bool, color bool) string {
	if value == "" {
		return value
	}
	if active {
		return textui.StyleVersion(value, color)
	}
	return textui.StyleDim(value, color)
}

func styleInventoryItemVersion(value string, status plan.Status, color bool) string {
	if value == "" {
		return value
	}
	switch status {
	case plan.StatusOK:
		return textui.StyleVersion(value, color)
	case plan.StatusError, plan.StatusBlocked:
		return textui.StyleError(value, color)
	default:
		return textui.StyleWarning(value, color)
	}
}

func styleInventoryItemDetail(value string, status plan.Status, color bool) string {
	if value == "" {
		return value
	}
	value = localizedBuiltInNoteText(value)
	switch status {
	case plan.StatusOK:
		return textui.StyleLabel(value, color)
	case plan.StatusError, plan.StatusBlocked:
		return textui.StyleError(value, color)
	default:
		return textui.StyleWarning(value, color)
	}
}

func styleState(value string, active bool, color bool) string {
	if value == "" {
		return value
	}
	if active {
		return textui.StyleVersion(value, color)
	}
	return textui.StyleDim(value, color)
}

func styleDriftCount(count int, color bool) string {
	value := fmt.Sprint(count)
	if count == 0 {
		return textui.StyleDim(value, color)
	}
	return textui.StyleWarning(value, color)
}

func enrichItems(items []plan.Item, cache legacycache.Cache, manualIndex map[string]toolRow) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	related := syncProviderMismatchIndex(items)
	for _, item := range items {
		enriched := cache.EnrichItem(item)
		if strings.TrimSpace(enriched.Detail) == "" {
			if reason := syncReasonForItemWithManual(enriched, related, manualIndex); reason != "" {
				guidance := syncGuidanceForItem(reason, enriched, related)
				enriched.Detail = listDriftGuidanceDetail(guidance)
			}
		}
		if detail := syncreport.HomebrewExtraDriftDetail(enriched, defaultLanguage()); detail != "" {
			enriched.Detail = joinManualDetails(enriched.Detail, tr("drift: ", "ドリフト: ")+detail)
		}
		out = append(out, enriched)
	}
	return out
}

func enrichHomebrewTapCaskItems(items []plan.Item, tapIndex map[string]toolRow) []plan.Item {
	if len(tapIndex) == 0 {
		return items
	}
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		enriched := item
		if strings.EqualFold(item.Provider, "brew") && strings.EqualFold(item.Kind, "cask") {
			if row, ok := manualAppMatch(tapIndex, item.Name); ok {
				enriched.Detail = joinManualDetails(enriched.Detail, row.Detail)
			}
		}
		out = append(out, enriched)
	}
	return out
}

func listDriftGuidanceDetail(guidance syncGuidance) string {
	switch {
	case guidance.Action != "" && guidance.Detail != "":
		return guidance.Action + ": " + guidance.Detail
	case guidance.Detail != "":
		return guidance.Detail
	case guidance.Action != "":
		return guidance.Action
	default:
		return ""
	}
}

func filterItems(items []plan.Item, opts listOptions) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		if opts.provider != "" && !listProviderMatches(item, opts.provider) {
			continue
		}
		if opts.kind != "" && !strings.EqualFold(item.Kind, opts.kind) {
			continue
		}
		if opts.category != "" && !strings.EqualFold(item.Category, opts.category) {
			continue
		}
		if opts.status != "" && !itemStatusMatches(item, opts.status) {
			continue
		}
		if opts.query != "" && !plan.ItemMatchesQuery(item, opts.query) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func listProviderMatches(item plan.Item, provider string) bool {
	if providerFilterIsVSCode(provider) {
		return item.Kind == "vscode"
	}
	return strings.EqualFold(item.Provider, provider)
}

func filteredProviders(providers []plan.ProviderSummary, provider string) []plan.ProviderSummary {
	if provider == "" {
		return providers
	}
	if providerFilterIsVSCode(provider) {
		return providers
	}
	out := []plan.ProviderSummary{}
	for _, summary := range providers {
		if strings.EqualFold(summary.Name, provider) {
			out = append(out, summary)
		}
	}
	return out
}

func itemStatusMatches(item plan.Item, filter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filter))
	switch normalized {
	case "profile-mismatch", "profile":
		return inventoryannotate.ItemHasProfileMismatch(item)
	default:
		return plan.StatusMatches(item.Status, filter)
	}
}

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

type listHubRouterOptions struct {
	Report         listReport
	BackendPlan    backendPlanReport
	BackendLoading bool
	LastUpdate     updateReport
	HasLastUpdate  bool
	InitialAction  string
	DetailStates   map[string]detailBrowserState
	Color          bool
}

func runListHubRouter(options listHubRouterOptions) (listHubRouterResult, map[string]detailBrowserState, error) {
	model := newListHubRouterModel(options)
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

func newListHubRouterModel(options listHubRouterOptions) listHubRouterModel {
	detailStates := reviewui.EnsureStateCache(options.DetailStates)
	model := listHubRouterModel{
		report:         options.Report,
		backendPlan:    options.BackendPlan,
		backendLoading: options.BackendLoading,
		lastUpdate:     options.LastUpdate,
		hasLastUpdate:  options.HasLastUpdate,
		detailStates:   detailStates,
		color:          options.Color,
	}
	initialAction := options.InitialAction
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
	if route, ok := parseUpdateSummaryRoute(action); ok {
		m.showUpdateSummaryRoute(route, m.currentAction())
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
		m.showTable("updev backend convergence", backendToolSectionsWithLoading(m.backendPlan, m.backendLoading), listHubActionBackends, returnAction, tableBrowserActions(), tableBrowserLabels())
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
	if action, _, _, _, ok := parseBrewDriftAction(m.writeFlow.Action); ok && action == "adopt" {
		result := collectInventory(context.Background(), m.report.Root, runner.Local{})
		updated := buildListReport(inventoryResult{Report: result}, listOptions{root: m.report.Root})
		if !m.backendLoading {
			updated.Evidence = addBackendListEvidence(updated.Evidence, m.backendPlan)
		}
		m.report = updated
	}
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
			Detail:  tr("Choose a support label. Use / for free-text query filtering.", "support label を選択します。自由検索は / を使います。"),
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
	m.detailStates[stateKey] = focusedRouteDetailState()
	m.showDetail(routeDetailTitle(route), rows, stateKey, m.currentAction())
}

func (m *listHubRouterModel) showUpdateSummaryRoute(route updateSummaryRoute, returnAction string) {
	if !m.hasLastUpdate {
		m.showEmptyDetail("updev update evidence", m.currentAction(), returnAction)
		return
	}
	opts := lastReportOptions{provider: route.Provider, status: route.Status, query: route.Query}
	filtered := filterUpdateReport(m.lastUpdate, opts)
	suffix := updateSummaryRouteTitleSuffix(route)
	stateKey := updateSummaryRouteStateKey(route)
	switch route.Base {
	case updateHubActionLogs:
		m.showFocusedDetail("updev update evidence"+suffix, updateLogDetailRows(filtered), stateKey, returnAction)
	case updateHubActionSecurity:
		m.showFocusedDetail("updev security review"+suffix, updateSecurityDetailRowsForFilter(filtered, opts), stateKey, returnAction)
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: filtered.Inventory}, listOptions{provider: route.Provider, status: route.Status, query: route.Query})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		m.showListFiltered("updev installed inventory"+suffix, inventory, stateKey, returnAction, listHubActionManual, listHubActionManual)
	case updateHubActionInventoryDetails:
		m.showFocusedDetail("updev inventory details"+suffix, updateInventoryDetailRowsWithBackend(filtered, m.backendPlan), stateKey, returnAction)
	default:
		m.showAction(returnAction, listHubActionFull)
	}
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
		label := providerSupportLabel(provider.Name)
		if support.LabelIsDenseBadge(label) {
			summaryParts = append(summaryParts, "support="+label)
		}
		metadata := []string{}
		if label != "" {
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

func (m *listHubRouterModel) showFocusedDetail(title string, rows []detailBrowserRow, stateKey string, returnAction string) {
	m.detailStates[stateKey] = focusedRouteDetailState()
	m.showDetail(title, rows, stateKey, returnAction)
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
	if action, kind, name, category, ok := parseBrewDriftAction(value); ok {
		if action != "adopt" || (category != "work" && category != "personal") {
			return detailWriteActionSpec{}, false
		}
		return detailWriteActionSpec{
			Title:       tr("Homebrew drift action", "Homebrew drift 操作"),
			Prompt:      fmt.Sprintf(tr("Add %s %q to Brewfile category %s?", "Brewfile に %s %q を category %s として追加しますか?"), kind, name, category),
			Description: tr("This changes desired state only. It does not install or uninstall the Homebrew package. Brewfile writes require [brewfile].write_mode to allow mutation.", "desired state だけを変更します。Homebrew package の install/uninstall は行いません。Brewfile 書き込みには [brewfile].write_mode で mutation を許可している必要があります。"),
		}, true
	}
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
		decision, reason, expires, ok := defaultSecurityDetailActionInputs(action)
		if !ok {
			return detailWriteActionSpec{}, false
		}
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
			spec.DefaultReason = "accepted from updev detail browser after local review"
			spec.DefaultExpires = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
		}
		return spec, true
	}
	return detailWriteActionSpec{}, false
}

func applyRoutedDetailWriteAction(root string, report *updateReport, value string, reason string, expires string) bool {
	if action, kind, name, category, ok := parseBrewDriftAction(value); ok {
		if action != "adopt" {
			return false
		}
		mutation := buildMutationReport(context.Background(), mutationOptions{
			action:   "add",
			root:     root,
			provider: "brew",
			kind:     kind,
			name:     name,
			category: category,
		})
		return mutation.Status == plan.StatusOK && mutation.Changed
	}
	if action, target, ok := parseManualPlanDetailAction(value); ok {
		return applyConfirmedManualPlanDetailAction(root, action, target)
	}
	if action, current, recommended, ok := parseBackendDetailAction(value); ok {
		return applyConfirmedBackendDetailAction(root, action, current, recommended)
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(value); ok {
		if !securityDetailActionRequiresCustomInput(action) {
			_, defaultReason, defaultExpires, ok := defaultSecurityDetailActionInputs(action)
			if !ok {
				return false
			}
			reason = defaultReason
			expires = defaultExpires
		}
		return applyConfirmedSecurityDetailActionSilently(report, action, provider, kind, name, strings.TrimSpace(reason), strings.TrimSpace(expires))
	}
	return false
}
