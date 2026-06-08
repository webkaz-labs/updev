package cmd

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
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
	Evidence         listEvidenceIndex       `json:"-"`
	Details          bool                    `json:"-"`
	Limit            int                     `json:"-"`
}

type toolSection = reviewui.Section
type toolRow = reviewui.Row

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
	cache := loadLegacyCache()
	pending := pendingTranslations(report, cache, opts.retranslateAll)
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
	for key, en := range pending {
		cache.en[key] = en
		if ja := translated[key]; ja != "" {
			cache.ja[key] = ja
		}
	}
	saveTranslationCache(cache.en, cache.ja)
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

func pendingTranslations(report listReport, cache legacyCache, force bool) map[string]string {
	pending := map[string]string{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		key := legacyKey(item.Provider, item.Kind, item.Name)
		if key == "" {
			continue
		}
		desc := cache.translationSourceForItem(item, key)
		addPendingTranslation(pending, cache, key, desc, force)
	}
	for _, section := range report.Sections {
		for _, row := range section.Rows {
			key := row.TranslationKey
			if key == "" {
				key = section.Name + ":" + row.Name
			}
			desc := row.TranslationSource
			if desc == "" {
				desc = cache.en[key]
			}
			addPendingTranslation(pending, cache, key, desc, force)
		}
	}
	return pending
}

func addPendingTranslation(pending map[string]string, cache legacyCache, key string, desc string, force bool) {
	desc = strings.TrimSpace(desc)
	if desc == "" || desc == noDescription {
		return
	}
	if force || cache.ja[key] == "" || cache.en[key] != desc {
		pending[key] = desc
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
	command := exec.Command("codex", "exec", "--skip-git-repo-check", "--output-last-message", responsePath, "-")
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
	legacy := loadLegacyCache()
	root := opts.root
	if root == "" {
		root = result.Report.Root
	}
	enriched := enrichItems(result.Report.Items, legacy, manualAppIndex(root))
	enriched = enrichHomebrewTapCaskItems(enriched, manualHomebrewTapIndex(root))
	filtered := filterItems(enriched, opts)
	sections := legacy.toolSections(opts)
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
		report.CacheAge = friendlyAge(time.Since(result.CreatedAt))
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

func printListText(w io.Writer, report listReport, title string, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(title, color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	if report.Cached {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("cache:", "キャッシュ:"), color), textui.StyleCount(report.CacheAge+" old", color), textui.StyleDim(tr("(use --refresh for a fresh read)", "(再取得は --refresh)"), color))
	}
	if len(report.Filters) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("filters:", "フィルター:"), color), textui.StyleRequested(filterSummary(report.Filters), color))
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
		if providerStatus(provider) != "ok" {
			providerAttention++
		}
	}
	itemCounts := map[string]int{}
	for _, item := range displayListItems(report.Items, report.Sections) {
		if isAttentionStatus(item.Status) {
			itemCounts[inventoryItemStatusLabel(item)]++
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
	for _, status := range attentionStatusOrder() {
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
	summary := listEvidenceSummary(report.Evidence, color)
	if summary == "" {
		return
	}
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
		status := providerStatus(provider)
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
				textui.StyleStatus(inventoryItemStatusLabel(item), color),
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

func enrichListItemsWithEvidence(items []plan.Item, evidence listEvidenceIndex) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		enriched := item
		itemEvidence := itemListEvidence(item, evidence)
		if itemEvidence.Summary() != "" {
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
		fmt.Fprintf(w, "%s %s\n", textui.StyleName(item.Provider+"/"+item.Kind+" "+item.Name, color), textui.StyleStatus(inventoryItemStatusLabel(item), color))
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
	listHubActionLimited   = "limited"
	listHubActionDetails   = "details"
	listHubActionFull      = "full"
)

const listRouteActionPrefix = "list-route"

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
			choices := listHubChoices(report, backendPlan, backendLoading, lastUpdate.Report, hasLastUpdate)
			if selectorHub {
				printListHubDashboard(os.Stdout, report, color)
				action, err = runUpdevSelect("updev hub", tr("Choose a view or filter. Back/Home/Exit are available after each view.", "表示または filter を選択します。各 view から Back/Home/Exit できます。"), choices, defaultAction)
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
			result, updatedStates, err := runListHubRouter(report, backendPlan, backendLoading, lastUpdate.Report, hasLastUpdate, action, detailStates, textui.ColorEnabled())
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
			handled, exit := runListFilteredSelection("updev list "+provider, derivedListReport(report, listOptions{provider: provider}), detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate)
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
			handled, exit := runListFilteredSelection("updev list "+kind, derivedListReport(report, listOptions{kind: kind}), detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate)
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
			handled, exit := runListFilteredSelection("updev list "+category, derivedListReport(report, listOptions{category: category}), detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate)
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
			handled, exit := runListFilteredSelection("updev list "+status, derivedListReport(report, listOptions{status: status}), detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate)
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
			handled, exit := runListFilteredSelection("updev list query", derivedListReport(report, listOptions{query: query}), detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate)
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
			handled, exit := runListFilteredSelectionWithViewActions("updev list manual", manualReport, detailStates, &defaultAction, &pendingAction, &backendPlan, lastUpdate.Report, hasLastUpdate, listHubActionFull, listHubActionFull)
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
			state, err := runToolTableBrowserWithState("updev backend convergence", backendToolSections(backendPlan), detailStates[stateKey], textui.ColorEnabled())
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
				outcome := runListRouteDetail(report.Root, route, backendPlan, lastUpdate.Report, hasLastUpdate, detailStates, textui.ColorEnabled())
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
		listHubActionSecurity, listHubActionLimited, listHubActionDetails,
		listHubActionFull:
		return true
	default:
		return false
	}
}

func shouldRunListHubRouterAction(action string) bool {
	switch action {
	case listHubActionAttention, listHubActionProvider, listHubActionKind, listHubActionCategory, listHubActionStatus, listHubActionQuery, listHubActionManual, listHubActionBackends, listHubActionUpdates, listHubActionSecurity, listHubActionLimited, listHubActionDetails, listHubActionFull:
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

func listHubChoices(report listReport, backendPlan backendPlanReport, backendLoading bool, lastUpdate updateReport, hasLastUpdate bool) []updevChoice {
	choices := []updevChoice{
		{Value: listHubActionFull, Label: tr("Installed inventory", "インストール済み一覧"), Description: fmt.Sprintf(tr("Review all %d installed and desired inventory rows with grouping, filters, and expansion.", "全 %d 件の installed / desired inventory 行を grouping / filter / 展開付きで確認します。"), listInventoryReviewCount(report)), Selected: true},
		{Value: listHubActionAttention, Label: tr("Attention items", "注意項目"), Description: tr("Show only rows needing attention.", "対応が必要な行だけを表示します。")},
		{Value: listHubActionProvider, Label: tr("Provider filter", "provider filter"), Description: tr("Choose a provider; rich rows expand with Enter or Space.", "provider を選びます。詳細行は Enter / Space で展開できます。")},
		{Value: listHubActionKind, Label: tr("Kind filter", "kind filter"), Description: tr("Choose brew, cask, vscode, tap, or tool rows.", "brew / cask / vscode / tap / tool で絞り込みます。")},
		{Value: listHubActionCategory, Label: tr("Category filter", "category filter"), Description: tr("Choose work, personal, runtime, core, npm, or another group.", "work / personal / runtime / core / npm などで絞り込みます。")},
		{Value: listHubActionStatus, Label: tr("Status filter", "status filter"), Description: tr("Choose attention, active, inactive, missing, extra, drift, unavailable, or ok.", "attention / active / inactive / missing / extra / drift / unavailable / ok を選びます。")},
		{Value: listHubActionQuery, Label: tr("Query filter", "query filter"), Description: tr("Search row names, versions, and descriptions.", "name / version / description を検索します。")},
		{Value: listHubActionManual, Label: tr("Manual apps", "手動管理アプリ"), Description: tr("Review read-only manual, vendor, App Store, and non-provider app rows.", "manual / vendor / App Store / provider 外アプリを read-only で確認します。")},
	}
	if backendLoading {
		choices = append(choices, updevChoice{Value: listHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: tr("Prepare provider/backend recommendations asynchronously after the view opens.", "view を開いてから provider/backend 推奨を非同期で準備します。")})
	} else if findings := len(backendPlan.Findings); findings > 0 {
		actions := backendPlanActionableCount(backendPlan)
		choices = append(choices, updevChoice{Value: listHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: fmt.Sprintf(tr("Review %d provider/backend recommendations; %d can be applied from details.", "%d 件の provider/backend 推奨を確認します。%d 件は詳細から適用できます。"), findings, actions)})
	}
	if hasLastUpdate && listEvidenceMatchesRoot(report.Root, lastUpdate.Root) {
		if updateRows := updateReportUpdatedItemCount(lastUpdate) + updateReportDeferredItemCount(lastUpdate); updateRows > 0 || len(lastUpdate.Steps) > 0 {
			choices = append(choices, updevChoice{Value: listHubActionUpdates, Label: tr("Update evidence", "update evidence"), Description: fmt.Sprintf(tr("Review cached provider update evidence from %d steps.", "%d 件の cached provider update evidence を確認します。"), len(lastUpdate.Steps))})
		}
		if updateDashboardSecurityAttention(lastUpdate) > 0 {
			choices = append(choices, updevChoice{Value: listHubActionSecurity, Label: tr("Security review", "security review"), Description: tr("Review cached security holds, advisories, and policy actions.", "cached security hold / advisory / policy action を確認します。")})
		}
	}
	choices = append(choices,
		updevChoice{Value: listHubActionLimited, Label: tr("Compact list", "compact list"), Description: tr("Show at most 10 rows per section.", "各 section 最大 10 行で表示します。")},
		updevChoice{Value: listHubActionDetails, Label: tr("Details", "詳細"), Description: tr("Show limited rows plus expanded descriptions.", "絞り込んだ行と展開可能な説明を表示します。")},
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

func runListFilteredSelection(title string, report listReport, detailStates map[string]detailBrowserState, defaultAction *string, pendingAction *string, backendPlan *backendPlanReport, lastUpdate updateReport, hasLastUpdate bool) (bool, bool) {
	return runListFilteredSelectionWithViewActions(title, report, detailStates, defaultAction, pendingAction, backendPlan, lastUpdate, hasLastUpdate, "", "")
}

func runListFilteredSelectionWithViewActions(title string, report listReport, detailStates map[string]detailBrowserState, defaultAction *string, pendingAction *string, backendPlan *backendPlanReport, lastUpdate updateReport, hasLastUpdate bool, nextAction string, previousAction string) (bool, bool) {
	for {
		action, handled := runListFilteredBrowserWithViewActions(title, report, detailStates, textui.ColorEnabled(), nextAction, previousAction)
		if route, ok := parseListRouteAction(action); ok {
			outcome := runListRouteDetail(report.Root, route, *backendPlan, lastUpdate, hasLastUpdate, detailStates, textui.ColorEnabled())
			if outcome == listRouteDetailExit {
				return true, true
			}
			if outcome == listRouteDetailHome {
				if defaultAction != nil {
					*defaultAction = listHubActionFull
				}
				if pendingAction != nil {
					*pendingAction = ""
				}
				return true, false
			}
			if route.Domain == listHubActionBackends {
				*backendPlan = buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: report.Root})
			}
			continue
		}
		return handleListFilteredAction(action, handled, defaultAction, pendingAction)
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
	if summary := filterSummary(report.Filters); summary != "" {
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

func enrichToolSectionsWithEvidence(sections []toolSection, evidence listEvidenceIndex) []toolSection {
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		section.Rows = append([]toolRow{}, section.Rows...)
		for index, row := range section.Rows {
			items := toolSectionRowEvidenceItems(section, row)
			itemEvidence := listItemEvidence{}
			actions := row.Actions
			for _, item := range items {
				itemEvidence = mergeListItemEvidence(itemEvidence, itemListEvidence(item, evidence))
				actions = mergeReviewActions(actions, itemToolRowActions(item, evidence))
			}
			if itemEvidence.Summary() != "" {
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
	if cask := manualDetailValue(row.Detail, "cask"); cask != "" {
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

func mergeListItemEvidence(left listItemEvidence, right listItemEvidence) listItemEvidence {
	return listItemEvidence{
		Updates:  mergeStringsUnique(left.Updates, right.Updates),
		Security: mergeStringsUnique(left.Security, right.Security),
		Backends: mergeStringsUnique(left.Backends, right.Backends),
	}
}

func mergeStringsUnique(left []string, right []string) []string {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	out := append([]string{}, left...)
	seen := map[string]bool{}
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range right {
		if strings.TrimSpace(value) == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func mergeReviewActions(left []reviewui.Action, right []reviewui.Action) []reviewui.Action {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	out := append([]reviewui.Action{}, left...)
	seen := map[string]bool{}
	for _, action := range out {
		seen[action.Value] = true
	}
	for _, action := range right {
		if strings.TrimSpace(action.Value) == "" || seen[action.Value] {
			continue
		}
		seen[action.Value] = true
		out = append(out, action)
	}
	return out
}

func itemToolSections(items []plan.Item, evidence listEvidenceIndex) []toolSection {
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

func itemToolRow(item plan.Item, evidence listEvidenceIndex) toolRow {
	itemEvidence := itemListEvidence(item, evidence)
	actions := itemToolRowActions(item, evidence)
	return toolRow{
		Name:    item.Name,
		Version: item.Version,
		State:   inventoryItemStatusLabel(item),
		Detail:  inventoryItemDetail(item, itemEvidence, detailActionsFromReviewActions(actions)),
		Actions: actions,
	}
}

func itemToolRowActions(item plan.Item, evidence listEvidenceIndex) []reviewui.Action {
	actions := []reviewui.Action{}
	if item.Provider == manualProviderName {
		actions = append(actions, manualReviewRouteAction(item))
	}
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

func runListRouteDetail(root string, route listRouteAction, backendPlan backendPlanReport, lastUpdate updateReport, hasLastUpdate bool, detailStates map[string]detailBrowserState, color bool) listRouteDetailOutcome {
	stateKey := "route:" + route.Domain + ":" + route.Provider + ":" + route.Kind + ":" + route.Name
	var rows []detailBrowserRow
	title := routeDetailTitle(route)
	switch route.Domain {
	case listHubActionManual:
		manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: manualProviderName, query: route.Name})
		rows = manualPlanDetailRows(manualPlan)
	case listHubActionBackends:
		rows = backendDetailRowsForListRoute(backendPlan, route)
	case listHubActionUpdates:
		if hasLastUpdate {
			filtered := filterUpdateReport(lastUpdate, lastReportOptions{section: "logs", provider: route.Provider, query: route.Name})
			rows = updateLogDetailRows(filtered)
		}
	case listHubActionSecurity:
		if hasLastUpdate {
			opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Name}
			filtered := filterUpdateReport(lastUpdate, opts)
			rows = updateSecurityDetailRowsForFilter(filtered, opts)
		}
	default:
		return listRouteDetailBack
	}
	if len(rows) == 0 {
		rows = []detailBrowserRow{emptyRouteDetailRow(route)}
	}
	state, err := runDetailBrowserWithState(title, rows, initialRouteDetailState(detailStates[stateKey]), color)
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
	switch route.Domain {
	case listHubActionManual:
		_ = handleManualPlanDetailAction(root, state.Action)
	case listHubActionBackends:
		_ = handleBackendDetailAction(root, state.Action)
	case listHubActionSecurity:
		if hasLastUpdate {
			_ = handleSecurityDetailAction(&lastUpdate, state.Action)
		}
	}
	return listRouteDetailBack
}

func initialRouteDetailState(state detailBrowserState) detailBrowserState {
	if state.Expanded == nil {
		state.Expanded = map[int]bool{}
	}
	if len(state.Expanded) == 0 && state.Query == "" && state.Selected == 0 && state.Offset == 0 {
		state.Expanded[0] = true
	}
	return state
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
	name := listEvidenceNameKey(route.Name)
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
		candidate := listEvidenceNameKey(value)
		if candidate == "" {
			continue
		}
		if candidate == name || strings.Contains(candidate, name) || strings.Contains(name, candidate) {
			return true
		}
	}
	return false
}

type listEvidenceIndex struct {
	Updates  map[string][]string
	Security map[string][]string
	Backends map[string][]string
}

type listItemEvidence struct {
	Updates  []string
	Security []string
	Backends []string
}

func listTitleWithEvidenceSummary(title string, evidence listEvidenceIndex) string {
	summary := listEvidenceSummary(evidence, false)
	if summary == "" {
		return title
	}
	return title + " [" + summary + "]"
}

func listEvidenceSummary(evidence listEvidenceIndex, color bool) string {
	updates, security, backends := listEvidenceCounts(evidence)
	return fmt.Sprintf("%s=%s %s=%s %s=%s",
		textui.StyleRequested("upd", color),
		textui.StyleCount(fmt.Sprint(updates), color),
		textui.StyleRequested("sec", color),
		textui.StyleCount(fmt.Sprint(security), color),
		textui.StyleRequested("bak", color),
		textui.StyleCount(fmt.Sprint(backends), color),
	)
}

func listEvidenceCounts(evidence listEvidenceIndex) (int, int, int) {
	return listEvidenceValueCount(evidence.Updates), listEvidenceValueCount(evidence.Security), listEvidenceValueCount(evidence.Backends)
}

func listEvidenceValueCount(values map[string][]string) int {
	seen := map[string]bool{}
	for _, entries := range values {
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			seen[entry] = true
		}
	}
	return len(seen)
}

func buildListEvidenceIndex(root string) listEvidenceIndex {
	index := listEvidenceIndex{
		Updates:  map[string][]string{},
		Security: map[string][]string{},
		Backends: map[string][]string{},
	}
	entry, ok := loadLastUpdateReport()
	if !ok || !listEvidenceMatchesRoot(root, entry.Report.Root) {
		return index
	}
	for _, step := range entry.Report.Steps {
		for _, item := range step.Updated {
			detail := fmt.Sprintf("%s updated: %s", step.Name, strings.TrimSpace(item))
			for _, key := range listEvidenceUpdateItemKeys(step.Name, item) {
				listEvidenceAdd(index.Updates, key, detail)
			}
		}
		for _, item := range step.SkippedItems {
			status := "deferred"
			if step.Status == plan.StatusHeld {
				status = "held"
			}
			detail := firstNonEmpty(step.Reason, status)
			for _, key := range listEvidenceUpdateItemKeys(step.Name, item) {
				listEvidenceAdd(index.Updates, key, fmt.Sprintf("%s %s: %s", step.Name, status, detail))
			}
		}
	}
	for _, gate := range entry.Report.Safety {
		for _, finding := range gate.Findings {
			detail := listSecurityEvidenceDetail(entry.Report, gate, finding)
			for _, key := range listEvidenceFindingKeys(gate, finding) {
				listEvidenceAdd(index.Security, key, fmt.Sprintf("%s/%s %s: %s", firstNonEmpty(finding.Provider, gate.Provider), finding.Kind, finding.Name, detail))
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
	reason := strings.TrimSpace(localizedSafetyReason(finding.Reason))
	if report.Security == "strict" && gate.Status == plan.StatusHeld && securityDecisionNeedsAttention(decision) {
		if strings.EqualFold(decision, "hold") || strings.EqualFold(decision, "held") {
			return listEvidenceDetailWithReason(decision, reason)
		}
		return listEvidenceDetailWithReason("held (decision: "+decision+")", reason)
	}
	return listEvidenceDetailWithReason(decision, reason)
}

func listEvidenceDetailWithReason(detail string, reason string) string {
	detail = strings.TrimSpace(detail)
	reason = strings.TrimSpace(reason)
	if reason == "" || strings.EqualFold(reason, detail) {
		return detail
	}
	return detail + ": " + oneLine(reason)
}

func listEvidenceUpdateItemKeys(provider string, item string) []string {
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
	for _, name := range listEvidenceItemNameCandidates(item) {
		add(listEvidenceProviderNameKey(provider, name))
		if strings.TrimSpace(provider) == "" {
			add(listEvidenceNameKey(name))
		}
		if strings.EqualFold(provider, "brew") {
			add(listEvidenceExactKey(provider, "brew", name))
			add(listEvidenceExactKey(provider, "cask", name))
			add(listEvidenceExactKey(provider, "tap", name))
		}
	}
	return keys
}

func listEvidenceItemNameCandidates(value string) []string {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" {
		return nil
	}
	candidates := []string{value}
	fields := strings.Fields(value)
	leadingTokenIsPrefix := false
	if len(fields) >= 2 {
		switch strings.ToLower(strings.Trim(fields[0], `:;,.`)) {
		case "brew", "formula", "cask", "tap", "tool", "mise":
			leadingTokenIsPrefix = true
			candidates = append(candidates, fields[1])
		}
	}
	if len(fields) > 0 && !leadingTokenIsPrefix && listEvidenceSafeLeadingToken(fields[0]) {
		candidates = append(candidates, fields[0])
	}
	if strings.Contains(value, "/") {
		candidates = append(candidates, filepath.Base(value))
	}
	out := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.Trim(strings.TrimSpace(candidate), `"'():;,.`)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func listEvidenceSafeLeadingToken(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'():;,.`))
	if value == "" {
		return false
	}
	switch value {
	case "security", "held", "deferred", "skipped", "skip", "gate", "policy", "review", "blocked", "updated", "update":
		return false
	default:
		return true
	}
}

func addBackendListEvidence(index listEvidenceIndex, report backendPlanReport) listEvidenceIndex {
	if index.Backends == nil {
		index.Backends = map[string][]string{}
	}
	for _, finding := range report.Findings {
		detail := strings.TrimSpace(backendFindingEvidenceText(finding))
		if detail == "" {
			detail = tr("backend convergence review", "backend 整理の確認")
		}
		for _, key := range listEvidenceBackendFindingKeys(finding) {
			listEvidenceAdd(index.Backends, key, detail)
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
		add(listEvidenceExactKey(provider, kind, name))
		add(listEvidenceProviderNameKey(provider, name))
		add(listEvidenceNameKey(name))
	}
	addRef(finding.Provider, finding.Kind, finding.Name)
	addRef(finding.Provider, finding.Kind, finding.Current)
	recommendedKind := finding.Kind
	if finding.RecommendedProvider == "mise" {
		recommendedKind = "tool"
	}
	addRef(finding.RecommendedProvider, recommendedKind, finding.RecommendedName)
	for _, command := range finding.CommandNames {
		add(listEvidenceNameKey(command))
	}
	return keys
}

func listEvidenceMatchesRoot(root string, reportRoot string) bool {
	root = strings.TrimSpace(root)
	reportRoot = strings.TrimSpace(reportRoot)
	if root == "" || reportRoot == "" {
		return true
	}
	return filepath.Clean(root) == filepath.Clean(reportRoot)
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
	for _, name := range listEvidenceItemNameCandidates(finding.Name) {
		add(listEvidenceExactKey(provider, finding.Kind, name))
		add(listEvidenceProviderNameKey(provider, name))
		if strings.TrimSpace(provider) == "" {
			add(listEvidenceNameKey(name))
		}
		if strings.EqualFold(provider, "brew") {
			add(listEvidenceExactKey(provider, "brew", name))
			add(listEvidenceExactKey(provider, "cask", name))
			add(listEvidenceExactKey(provider, "tap", name))
		}
	}
	if finding.Tap != "" {
		add(listEvidenceExactKey(provider, "tap", finding.Tap))
		add(listEvidenceProviderNameKey(provider, finding.Tap))
		if strings.TrimSpace(provider) == "" {
			add(listEvidenceNameKey(finding.Tap))
		}
	}
	return keys
}

func itemListEvidence(item plan.Item, index listEvidenceIndex) listItemEvidence {
	keys := []string{
		listEvidenceExactKey(item.Provider, item.Kind, item.Name),
		listEvidenceProviderNameKey(item.Provider, item.Name),
		listEvidenceNameKey(item.Name),
	}
	return listItemEvidence{
		Updates:  listEvidenceLookup(index.Updates, keys),
		Security: listEvidenceLookup(index.Security, keys),
		Backends: listEvidenceLookup(index.Backends, keys),
	}
}

func listEvidenceLookup(index map[string][]string, keys []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		for _, value := range index[key] {
			if strings.TrimSpace(value) == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func listEvidenceAdd(index map[string][]string, key string, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	for _, existing := range index[key] {
		if existing == value {
			return
		}
	}
	index[key] = append(index[key], value)
}

func listEvidenceExactKey(provider string, kind string, name string) string {
	provider = listEvidenceToken(provider)
	kind = listEvidenceToken(kind)
	name = listEvidenceToken(name)
	if provider == "" || kind == "" || name == "" {
		return ""
	}
	return provider + "/" + kind + "/" + name
}

func listEvidenceProviderNameKey(provider string, name string) string {
	provider = listEvidenceToken(provider)
	name = listEvidenceToken(name)
	if provider == "" || name == "" {
		return ""
	}
	return provider + "/" + name
}

func listEvidenceNameKey(name string) string {
	return listEvidenceToken(name)
}

func listEvidenceToken(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
}

func (e listItemEvidence) Metadata() []string {
	metadata := []string{}
	for _, value := range e.Updates {
		metadata = append(metadata, tr("update evidence: ", "update evidence: ")+localizedListEvidenceText(value))
	}
	for _, value := range e.Security {
		metadata = append(metadata, tr("security evidence: ", "security evidence: ")+localizedListEvidenceText(value))
	}
	for _, value := range e.Backends {
		metadata = append(metadata, tr("backend evidence: ", "backend evidence: ")+localizedListEvidenceText(value))
	}
	return metadata
}

func (e listItemEvidence) Summary() string {
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

func itemDetailWithEvidence(detail string, evidence listItemEvidence) string {
	parts := []string{}
	if strings.TrimSpace(detail) != "" {
		parts = append(parts, strings.TrimSpace(detail))
	}
	parts = append(parts, evidence.Metadata()...)
	return strings.Join(parts, "\n")
}

func inventoryItemDetail(item plan.Item, evidence listItemEvidence, actions []detailBrowserAction) string {
	parts := []string{}
	if strings.TrimSpace(item.Detail) != "" {
		parts = append(parts, tr("description: ", "説明: ")+localizedBuiltInNoteText(item.Detail))
	} else {
		parts = append(parts, tr("summary: ", "概要: ")+inventoryItemStatusSummary(item))
	}
	parts = append(parts, tr("identity: ", "識別子: ")+inventoryItemIdentity(item))
	parts = append(parts, tr("status: ", "状態: ")+inventoryItemStatusSummary(item))
	if strings.TrimSpace(item.Category) != "" {
		parts = append(parts, tr("category: ", "カテゴリ: ")+item.Category+" - "+categoryDescription(item.Category))
	}
	if summary := evidence.Summary(); summary != "" {
		parts = append(parts, tr("linked evidence: ", "関連 evidence: ")+summary)
	}
	parts = append(parts, evidence.Metadata()...)
	for _, action := range actions {
		parts = append(parts, tr("next action: ", "次の操作: ")+detailActionSummary(action))
	}
	return strings.Join(parts, "\n")
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
	switch value {
	case "backend convergence review":
		return "backend 整理の確認"
	default:
		return localizedBackendReasonText(value)
	}
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
	return inventoryItemStatusLabel(item) + " - " + inventoryItemManagementSummary(item)
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

func itemDetailRow(item plan.Item, evidence listEvidenceIndex) detailBrowserRow {
	metadata := []string{
		"status: " + inventoryItemStatusLabel(item),
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
	metadata = append(metadata, itemEvidence.Metadata()...)
	actions := detailActionsFromReviewActions(itemToolRowActions(item, evidence))
	metadata = append(metadata, actionRouteEvidence(actions)...)
	return detailBrowserRow{
		Title:    item.Provider + "/" + item.Kind + " " + item.Name,
		Status:   inventoryItemStatusLabel(item),
		Summary:  firstNonEmpty(item.Detail, itemEvidence.Summary(), item.Version),
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
		return "hold", "held", true
	default:
		return "", "", false
	}
}

func securityEvidenceActionBadge(values []string) (string, string) {
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(lower, ": hold") || strings.Contains(lower, ": held") || strings.Contains(lower, " decision: hold") || strings.Contains(lower, " decision: held") {
			return "hold", "held"
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

func enrichItems(items []plan.Item, cache legacyCache, manualIndex map[string]toolRow) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	related := syncProviderMismatchIndex(items)
	for _, item := range items {
		enriched := cache.enrichItem(item)
		if strings.TrimSpace(enriched.Detail) == "" {
			if reason := syncReasonForItemWithManual(enriched, related, manualIndex); reason != "" {
				guidance := syncGuidanceForItem(reason, enriched, related)
				enriched.Detail = listDriftGuidanceDetail(guidance)
			}
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
		if opts.query != "" && !queryMatches(item, opts.query) {
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

func statusMatches(status plan.Status, filter string) bool {
	return itemStatusMatches(plan.Item{Status: status}, filter)
}

func itemStatusMatches(item plan.Item, filter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filter))
	switch normalized {
	case "attention", "problem", "problems":
		return isAttentionStatus(item.Status)
	case "profile-mismatch", "profile":
		return itemHasProfileMismatch(item)
	case "changed", "changes", "drift":
		return item.Status == plan.StatusMissing || item.Status == plan.StatusExtra || item.Status == plan.StatusDrift
	default:
		return strings.EqualFold(string(item.Status), normalized)
	}
}

func isAttentionStatus(status plan.Status) bool {
	for _, candidate := range attentionStatusOrder() {
		if status == candidate {
			return true
		}
	}
	return false
}

func attentionStatusOrder() []plan.Status {
	return []plan.Status{
		plan.StatusError,
		plan.StatusBlocked,
		plan.StatusHeld,
		plan.StatusMissing,
		plan.StatusExtra,
		plan.StatusDrift,
		plan.StatusUnavailable,
	}
}

func queryMatches(item plan.Item, query string) bool {
	needle := strings.ToLower(query)
	haystack := strings.ToLower(strings.Join([]string{item.Name, item.Kind, item.Category, item.Version, item.Detail}, " "))
	return strings.Contains(haystack, needle)
}

func providerStatus(provider plan.ProviderSummary) string {
	switch {
	case provider.Unavailable:
		return "unavailable"
	case provider.Error != "":
		return "error"
	case provider.Missing > 0 || provider.Extra > 0:
		return "drift"
	default:
		return "ok"
	}
}

func filterSummary(filters map[string]string) string {
	parts := []string{}
	for _, key := range []string{"provider", "kind", "category", "status", "query", "limit", "include_vscode"} {
		if value := filters[key]; value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, " ")
}

func friendlyAge(age time.Duration) string {
	if age < time.Second {
		return "0s"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return fmt.Sprintf("%dh", int(age.Hours()))
}
