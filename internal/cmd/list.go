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
	if err := fs.Parse(args); err != nil {
		return opts, err
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
		result = collectInventoryCachedWithOptions(context.Background(), opts.root, opts.refresh, inventoryCacheMaxAge, inventoryOptions{IncludeVSCode: listIncludesVSCode(opts)})
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
			runListHub(report)
		} else {
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
	filtered := filterItems(enriched, opts)
	sections := legacy.toolSections(opts)
	manualSections := manualAppSections(root, opts, enriched)
	sections = append(sections, manualSections...)
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
	displayItems := displayListItems(report.Items, report.Sections)
	if len(displayItems) > 0 || len(report.Sections) == 0 {
		printGroupedItems(w, displayItems, report.Limit, color)
	}
	if len(report.Sections) > 0 {
		fmt.Fprintln(w)
		printToolSections(w, report.Sections, report.Limit, color)
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

func printOmittedRows(w io.Writer, omitted int, color bool) {
	reviewui.PrintOmittedRows(w, omitted, reviewLabels(), color)
}

func reviewLabels() reviewui.Labels {
	return reviewui.Labels{
		Name:    tr("name", "名前"),
		Version: "version",
		Wanted:  "wanted",
		State:   tr("state", "状態"),
		Detail:  tr("detail", "詳細"),
		MoreRows: func(count int) string {
			return fmt.Sprintf(tr("... %d more rows; rerun with --limit 0 or --details", "... あと %d 行。全件は --limit 0 または --details"), count)
		},
		NoExtraDetail: tr("no additional detail", "追加の詳細はありません"),
	}
}

func reportHasDetails(report listReport) bool {
	for _, item := range report.Items {
		if strings.TrimSpace(item.Detail) != "" {
			return true
		}
	}
	for _, section := range report.Sections {
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
	for _, item := range displayListItems(report.Items, report.Sections) {
		if strings.TrimSpace(item.Detail) == "" {
			continue
		}
		wrote = true
		fmt.Fprintf(w, "%s %s\n", textui.StyleName(item.Provider+"/"+item.Kind+" "+item.Name, color), textui.StyleStatus(inventoryItemStatusLabel(item), color))
		printDetailLine(w, "detail", item.Detail, color)
	}
	for _, section := range report.Sections {
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
	listHubActionLimited   = "limited"
	listHubActionDetails   = "details"
	listHubActionFull      = "full"
)

func runListHub(report listReport) {
	defaultAction := listHubActionFull
	backendPlan := buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: report.Root})
	detailStates := map[string]detailBrowserState{}
	for {
		printListHubDashboard(os.Stdout, report, textui.ColorEnabled())
		action, err := runUpdevSelect("updev list", tr("Choose a view or filter. Back/Home/Exit are available after each view.", "表示または filter を選択します。各 view から Back/Home/Exit できます。"), listHubChoices(report, backendPlan), defaultAction)
		if err != nil {
			return
		}
		if action == updevActionExit {
			return
		}
		defaultAction = action
		switch action {
		case listHubActionAttention:
			printListText(os.Stdout, derivedListReport(report, listOptions{status: "attention", limit: 20}), "updev inventory", textui.ColorEnabled())
		case listHubActionProvider:
			provider, ok := selectListProvider(report)
			if !ok {
				continue
			}
			defaultAction = listHubActionProvider
			handled, exit := runListFilteredSelection("updev list "+provider, derivedListReport(report, listOptions{provider: provider}), detailStates, &defaultAction)
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
			handled, exit := runListFilteredSelection("updev list "+kind, derivedListReport(report, listOptions{kind: kind}), detailStates, &defaultAction)
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
			handled, exit := runListFilteredSelection("updev list "+category, derivedListReport(report, listOptions{category: category}), detailStates, &defaultAction)
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
			handled, exit := runListFilteredSelection("updev list "+status, derivedListReport(report, listOptions{status: status}), detailStates, &defaultAction)
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
			handled, exit := runListFilteredSelection("updev list query", derivedListReport(report, listOptions{query: query}), detailStates, &defaultAction)
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
			handled, exit := runListFilteredSelection("updev list manual", manualReport, detailStates, &defaultAction)
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
			state, err := runDetailBrowserWithState("updev backend convergence", backendDetailRows(backendPlan), detailStates[stateKey], textui.ColorEnabled())
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
			action, handled := runListFilteredBrowser("updev installed inventory", report, detailStates, textui.ColorEnabled())
			if action == updevActionExit {
				return
			}
			if action == updevActionHome {
				defaultAction = listHubActionFull
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

func listHubChoices(report listReport, backendPlan backendPlanReport) []updevChoice {
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
	if findings := len(backendPlan.Findings); findings > 0 {
		choices = append(choices, updevChoice{Value: listHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: fmt.Sprintf(tr("Review %d provider/backend recommendations.", "%d 件の provider/backend 推奨を確認します。"), findings)})
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

func runListFilteredSelection(title string, report listReport, detailStates map[string]detailBrowserState, defaultAction *string) (bool, bool) {
	action, handled := runListFilteredBrowser(title, report, detailStates, textui.ColorEnabled())
	switch action {
	case updevActionExit:
		return handled, true
	case updevActionHome:
		if defaultAction != nil {
			*defaultAction = listHubActionFull
		}
		return true, false
	default:
		return handled, false
	}
}

func runListFilteredBrowser(title string, report listReport, detailStates map[string]detailBrowserState, color bool) (string, bool) {
	stateKey := listBrowserStateKey(report)
	sections := listTableSections(report)
	if toolTableRowCount(sections) > 0 {
		state, err := runToolTableBrowserWithState(title, sections, detailStates[stateKey], color)
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
	sections := itemToolSections(displayListItems(report.Items, report.Sections))
	sections = append(sections, report.Sections...)
	return sections
}

func itemToolSections(items []plan.Item) []toolSection {
	grouped := map[string][]toolRow{}
	order := []string{}
	for _, item := range items {
		key := itemToolSectionKey(item)
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], itemToolRow(item))
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

func itemToolRow(item plan.Item) toolRow {
	return toolRow{
		Name:    item.Name,
		Version: item.Version,
		State:   inventoryItemStatusLabel(item),
		Detail:  item.Detail,
	}
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
		rows = append(rows, itemDetailRow(item))
	}
	for _, section := range report.Sections {
		for _, row := range limitedToolRows(section.Rows, report.Limit) {
			rows = append(rows, toolDetailRow(section, row))
		}
	}
	return rows
}

func itemDetailRow(item plan.Item) detailBrowserRow {
	metadata := []string{
		"provider: " + item.Provider,
		"kind: " + item.Kind,
		fmt.Sprintf("desired: %t", item.Desired),
		fmt.Sprintf("live: %t", item.Live),
	}
	if item.Version != "" {
		metadata = append(metadata, "version: "+item.Version)
	}
	if item.Category != "" {
		metadata = append(metadata, "category: "+item.Category)
	}
	return detailBrowserRow{
		Title:    item.Provider + "/" + item.Kind + " " + item.Name,
		Status:   inventoryItemStatusLabel(item),
		Summary:  firstNonEmpty(item.Detail, item.Version),
		Detail:   item.Detail,
		Metadata: metadata,
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
	return detailBrowserRow{
		Title:    section.Title + " " + row.Name,
		Status:   state,
		Summary:  firstNonEmpty(row.Detail, row.Version),
		Detail:   row.Detail,
		Metadata: metadata,
	}
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
