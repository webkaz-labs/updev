package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/plan"
)

const manualProviderName = "manual"

var markdownLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

type manualAppOverride struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases,omitempty"`
	Category  string   `json:"category,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	ManagedBy string   `json:"managed_by,omitempty"`
	Lifecycle string   `json:"lifecycle,omitempty"`
}

func manualAppSections(root string, filters listOptions, inventoryItems []plan.Item) []toolSection {
	if !listIncludesManual(filters) {
		return nil
	}
	if filters.provider != "" && !strings.EqualFold(filters.provider, manualProviderName) {
		return nil
	}
	if filters.kind != "" && !strings.EqualFold(filters.kind, manualProviderName) {
		return nil
	}
	if filters.category != "" && !strings.EqualFold(filters.category, manualProviderName) {
		return nil
	}
	sections := []toolSection{}
	content, err := os.ReadFile(filepath.Join(root, "docs", "apps.md"))
	if err == nil {
		sections = parseManualAppSections(string(content))
	}
	if overrides := loadManualAppOverrides(root); len(overrides) > 0 {
		sections = append(sections, manualOverrideSections(overrides)...)
	}
	if scanned := manualScannedAppSections(root); len(scanned) > 0 {
		sections = append(sections, scanned...)
	}
	if masApps := manualMASListSections(root); len(masApps) > 0 {
		sections = append(sections, masApps...)
	}
	if casks := manualCaskSections(inventoryItems); len(casks) > 0 {
		sections = append(sections, casks...)
	}
	sections = reconcileManualAppSections(sections)
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		rows := make([]toolRow, 0, len(section.Rows))
		for _, row := range section.Rows {
			if filters.status != "" && !toolRowStatusMatches(row.State, filters.status) {
				continue
			}
			if filters.query != "" && !manualRowMatches(section, row, filters.query) {
				continue
			}
			rows = append(rows, row)
		}
		if len(rows) > 0 {
			section.Rows = rows
			out = append(out, section)
		}
	}
	return out
}

func manualAppSectionsForInventoryCommand(root string) []toolSection {
	return manualAppSections(root, listOptions{root: root, provider: manualProviderName}, manualCachedInventoryItems(root))
}

func manualCachedInventoryItems(root string) []plan.Item {
	entry, ok := loadInventoryCache(root, inventoryCacheMaxAge, inventoryOptions{IncludeVSCode: false})
	if !ok {
		return nil
	}
	return entry.Report.Items
}

func manualCaskSections(items []plan.Item) []toolSection {
	rows := []toolRow{}
	for _, item := range items {
		if item.Provider != "brew" || item.Kind != "cask" {
			continue
		}
		if item.Status != plan.StatusOK && item.Status != plan.StatusExtra {
			continue
		}
		details := []string{"source: homebrew cask", "cask: " + item.Name}
		if item.Status != "" {
			details = append(details, "status: "+string(item.Status))
		}
		if item.Category != "" {
			details = append(details, "category: "+item.Category)
		}
		if item.Detail != "" {
			details = append(details, item.Detail)
		}
		rows = append(rows, toolRow{
			Name:   manualCaskDisplayName(item.Name),
			State:  "brew",
			Detail: strings.Join(details, "; "),
		})
	}
	if len(rows) == 0 {
		return nil
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return []toolSection{{
		Name:  "manual/homebrew-casks",
		Title: "manual / Homebrew casks",
		Rows:  rows,
	}}
}

func manualCaskDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func reconcileManualAppSections(sections []toolSection) []toolSection {
	if len(sections) == 0 {
		return sections
	}
	index := map[string]manualRowRef{}
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		if manualEvidenceSection(section.Name) {
			continue
		}
		out = append(out, section)
		sectionIndex := len(out) - 1
		for rowIndex, row := range out[sectionIndex].Rows {
			for _, key := range manualRowIdentityKeys(row) {
				if key != "" {
					index[key] = manualRowRef{Section: sectionIndex, Row: rowIndex}
				}
			}
		}
	}
	installedRows := []toolRow{}
	evidenceIndex := map[string]manualRowRef{}
	for _, section := range sections {
		if !manualEvidenceSection(section.Name) {
			continue
		}
		for _, row := range section.Rows {
			ref, ok := manualRowMatch(index, row)
			if !ok {
				if ref, ok := manualRowMatch(evidenceIndex, row); ok {
					mergeManualEvidenceRow(&installedRows[ref.Row], row)
					continue
				}
				installedRows = append(installedRows, row)
				rowIndex := len(installedRows) - 1
				for _, key := range manualRowIdentityKeys(row) {
					if key != "" {
						evidenceIndex[key] = manualRowRef{Row: rowIndex}
					}
				}
				continue
			}
			existing := &out[ref.Section].Rows[ref.Row]
			mergeManualLiveRow(existing, row)
		}
	}
	if len(installedRows) > 0 {
		out = append(out, toolSection{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows:  installedRows,
		})
	}
	return out
}

func manualEvidenceSection(name string) bool {
	return name == "manual/installed-apps" || name == "manual/mac-app-store" || name == "manual/homebrew-casks"
}

type manualRowRef struct {
	Section int
	Row     int
}

func manualRowMatch(index map[string]manualRowRef, row toolRow) (manualRowRef, bool) {
	for _, key := range manualRowIdentityKeys(row) {
		if ref, ok := index[key]; ok {
			return ref, true
		}
	}
	return manualRowRef{}, false
}

func manualRowIdentityKeys(row toolRow) []string {
	keys := []string{}
	if bundleID := manualDetailValue(row.Detail, "bundle_id"); bundleID != "" {
		keys = append(keys, "bundle:"+strings.ToLower(bundleID))
	}
	if masID := manualDetailValue(row.Detail, "mas_id"); masID != "" {
		keys = append(keys, "mas:"+strings.ToLower(masID))
	}
	for _, key := range manualAppKeys(row.Name) {
		if key != "" {
			keys = append(keys, "name:"+key)
		}
	}
	return keys
}

func mergeManualLiveRow(row *toolRow, live toolRow) {
	if row.Version == "" {
		row.Version = live.Version
	}
	switch {
	case row.State == "local-only" || row.State == "ignored":
		// Explicit local decisions suppress review candidates without becoming desired state.
	case row.State == "brew" || live.State == "brew":
		row.State = "brew"
	default:
		row.State = "managed"
	}
	row.Detail = joinManualDetails(row.Detail, live.Detail)
}

func mergeManualEvidenceRow(row *toolRow, live toolRow) {
	if row.Version == "" {
		row.Version = live.Version
	}
	if row.State == "brew" || live.State == "brew" {
		row.State = "brew"
	} else {
		row.State = "installed"
	}
	row.Detail = joinManualDetails(row.Detail, live.Detail)
}

func joinManualDetails(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	case strings.Contains(left, right):
		return left
	case strings.Contains(right, left):
		return right
	default:
		return left + "; " + right
	}
}

func manualDetailValue(detail string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	prefix := key + ":"
	for _, part := range strings.Split(detail, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(part, prefix))
		}
	}
	return ""
}

type manualScannedApp struct {
	Name     string
	Source   string
	Path     string
	BundleID string
	Version  string
}

type manualReviewCandidate struct {
	Provider          string                     `json:"provider"`
	Kind              string                     `json:"kind"`
	Name              string                     `json:"name"`
	ReasonCode        string                     `json:"reason_code"`
	RemediationCode   string                     `json:"remediation_code,omitempty"`
	Confidence        string                     `json:"confidence,omitempty"`
	Params            map[string]string          `json:"params,omitempty"`
	Evidence          []manualReviewEvidence     `json:"evidence,omitempty"`
	SuggestedOverride manualReviewOverrideFields `json:"suggested_override"`
}

type manualReviewEvidence struct {
	Scanner  string `json:"scanner"`
	Source   string `json:"source,omitempty"`
	Path     string `json:"path,omitempty"`
	MASID    string `json:"mas_id,omitempty"`
	BundleID string `json:"bundle_id,omitempty"`
	Version  string `json:"version,omitempty"`
}

type manualReviewOverrideFields struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases,omitempty"`
	Category  string   `json:"category,omitempty"`
	ManagedBy string   `json:"managed_by,omitempty"`
	Lifecycle string   `json:"lifecycle,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

func manualScannedAppSections(root string) []toolSection {
	apps := scanManualApplications(root)
	if len(apps) == 0 {
		return nil
	}
	rows := make([]toolRow, 0, len(apps))
	for _, app := range apps {
		details := []string{"source: app bundle", "path: " + app.Path}
		if app.Source != "" {
			details[0] = "source: " + app.Source
		}
		if app.BundleID != "" {
			details = append(details, "bundle_id: "+app.BundleID)
		}
		if app.Version != "" {
			details = append(details, "version: "+app.Version)
		}
		rows = append(rows, toolRow{
			Name:    app.Name,
			Version: app.Version,
			State:   "installed",
			Detail:  strings.Join(details, "; "),
		})
	}
	return []toolSection{{
		Name:  "manual/installed-apps",
		Title: "manual / Installed apps",
		Rows:  rows,
	}}
}

func scanManualApplications(root string) []manualScannedApp {
	apps := []manualScannedApp{}
	seen := map[string]bool{}
	for _, scanner := range manualApplicationScanners() {
		for _, app := range scanner(root) {
			key := manualScannedAppKey(app)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			apps = append(apps, app)
		}
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].Path < apps[j].Path
	})
	return apps
}

type manualApplicationScanner func(root string) []manualScannedApp

func manualApplicationScanners() []manualApplicationScanner {
	return []manualApplicationScanner{
		scanManualMacApplications,
	}
}

func manualScannedAppKey(app manualScannedApp) string {
	if app.BundleID != "" {
		return "bundle:" + strings.ToLower(app.BundleID)
	}
	if app.Path != "" {
		return "path:" + filepath.Clean(app.Path)
	}
	return "name:" + normalizedManualAppKey(app.Name)
}

func firstNonEmptyManualValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func manualProviderSummary(sections []toolSection) plan.ProviderSummary {
	summary := plan.ProviderSummary{Name: manualProviderName, Supported: true}
	for _, section := range sections {
		for _, row := range section.Rows {
			switch row.State {
			case "installed":
				summary.Live++
			case "managed":
				summary.Desired++
				summary.Live++
			case "brew":
				summary.Desired++
				summary.Live++
			case "local-only", "ignored":
				if manualRowHasLiveEvidence(row) {
					summary.Live++
				}
			default:
				summary.Desired++
			}
		}
	}
	return summary
}

func manualRowHasLiveEvidence(row toolRow) bool {
	return manualDetailValue(row.Detail, "path") != "" ||
		manualDetailValue(row.Detail, "mas_id") != "" ||
		strings.Contains(row.Detail, "source: homebrew cask")
}

func manualReviewCandidates(sections []toolSection) []manualReviewCandidate {
	candidates := []manualReviewCandidate{}
	for _, section := range sections {
		if section.Name != "manual/installed-apps" {
			continue
		}
		for _, row := range section.Rows {
			if row.State != "installed" {
				continue
			}
			candidate := manualReviewCandidate{
				Provider:        manualProviderName,
				Kind:            "app",
				Name:            row.Name,
				ReasonCode:      "manual_app_live_only",
				RemediationCode: "manual_inventory_override",
				Confidence:      manualReviewConfidence(row),
				Params: map[string]string{
					"name": row.Name,
				},
				Evidence: []manualReviewEvidence{manualReviewEvidenceFromRow(row)},
				SuggestedOverride: manualReviewOverrideFields{
					Name:      row.Name,
					Aliases:   manualSuggestedAliases(row),
					ManagedBy: manualSuggestedManagedBy(row),
					Detail:    "review installed app ownership and lifecycle",
				},
			}
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	return candidates
}

func manualReviewEvidenceFromRow(row toolRow) manualReviewEvidence {
	version := row.Version
	if version == "" {
		version = manualDetailValue(row.Detail, "version")
	}
	path := manualDetailValue(row.Detail, "path")
	masID := manualDetailValue(row.Detail, "mas_id")
	scanner := "macos_app_bundle"
	if path == "" && masID != "" {
		scanner = "mas_list"
	}
	return manualReviewEvidence{
		Scanner:  scanner,
		Source:   manualDetailValue(row.Detail, "source"),
		Path:     path,
		MASID:    masID,
		BundleID: manualDetailValue(row.Detail, "bundle_id"),
		Version:  version,
	}
}

func manualSuggestedAliases(row toolRow) []string {
	aliases := []string{}
	if path := manualDetailValue(row.Detail, "path"); path != "" {
		base := filepath.Base(path)
		if base != "" {
			aliases = append(aliases, base)
		}
	}
	if bundleID := manualDetailValue(row.Detail, "bundle_id"); bundleID != "" {
		aliases = append(aliases, bundleID)
	}
	return aliases
}

func manualSuggestedManagedBy(row toolRow) string {
	if strings.Contains(row.Detail, "source: mas list") {
		return "mas"
	}
	switch manualDetailValue(row.Detail, "source") {
	case "mac app store receipt":
		return "mas"
	default:
		return "manual"
	}
}

func manualReviewConfidence(row toolRow) string {
	if strings.Contains(row.Detail, "source: mas list") {
		return "high"
	}
	switch manualDetailValue(row.Detail, "source") {
	case "mac app store receipt":
		return "high"
	default:
		return "medium"
	}
}

func listIncludesManual(opts listOptions) bool {
	return strings.EqualFold(opts.provider, manualProviderName) ||
		strings.EqualFold(opts.kind, manualProviderName) ||
		strings.EqualFold(opts.category, manualProviderName)
}

func listManualOnly(opts listOptions) bool {
	return listIncludesManual(opts) && !listIncludesVSCode(opts)
}

func parseManualAppSections(content string) []toolSection {
	sections := []toolSection{}
	current := -1
	tableHeaders := []string(nil)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			sections = append(sections, toolSection{
				Name:  "manual/" + manualSectionKey(title),
				Title: "manual / " + title,
			})
			current = len(sections) - 1
			tableHeaders = nil
			continue
		}
		if current < 0 {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			name := cleanManualMarkdown(strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			if name != "" {
				sections[current].Rows = append(sections[current].Rows, manualAppRow(name, sections[current].Title, ""))
			}
			tableHeaders = nil
			continue
		}
		if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") {
			cells := markdownTableCells(line)
			if len(cells) == 0 || markdownTableSeparator(cells) {
				continue
			}
			if tableHeaders == nil {
				tableHeaders = cells
				continue
			}
			for _, row := range manualAppRowsFromTable(sections[current].Title, tableHeaders, cells) {
				sections[current].Rows = append(sections[current].Rows, row)
			}
			continue
		}
		if line != "" {
			tableHeaders = nil
		}
	}
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		if len(section.Rows) > 0 {
			out = append(out, section)
		}
	}
	return out
}

func manualAppIndex(root string) map[string]toolRow {
	index := map[string]toolRow{}
	content, err := os.ReadFile(filepath.Join(root, "docs", "apps.md"))
	if err == nil {
		for _, section := range parseManualAppSections(string(content)) {
			for _, row := range section.Rows {
				addManualIndexRow(index, row, nil)
			}
		}
	}
	for _, override := range loadManualAppOverrides(root) {
		row := manualAppRow(override.Name, "manual / "+manualOverrideCategory(override), override.Detail)
		addManualIndexRow(index, row, override.Aliases)
	}
	return index
}

func addManualIndexRow(index map[string]toolRow, row toolRow, aliases []string) {
	for _, key := range manualAppKeys(row.Name) {
		if key != "" {
			index[key] = row
		}
	}
	for _, alias := range aliases {
		if key := normalizedManualAppKey(alias); key != "" {
			index[key] = row
		}
	}
}

func manualAppKeys(name string) []string {
	name = cleanManualMarkdown(name)
	keys := []string{normalizedManualAppKey(name)}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '／'
	}) {
		keys = append(keys, normalizedManualAppKey(part))
	}
	return keys
}

func normalizedManualAppKey(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func manualAppMatch(index map[string]toolRow, name string) (toolRow, bool) {
	if len(index) == 0 {
		return toolRow{}, false
	}
	row, ok := index[normalizedManualAppKey(name)]
	return row, ok
}

func loadManualAppOverrides(root string) []manualAppOverride {
	path := inventoryOverridesPath(root)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseManualAppOverrides(string(data))
}

func inventoryOverridesPath(root string) string {
	if path := loadUpdevConfig().Inventory.Overrides; path != nil {
		return resolveUpdevConfigPath(root, *path)
	}
	return defaultInventoryOverridesPath()
}

func defaultInventoryOverridesPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "updev", "inventory-overrides.toml")
}

func parseManualAppOverrides(content string) []manualAppOverride {
	overrides := []manualAppOverride{}
	current := -1
	for _, raw := range strings.Split(content, "\n") {
		line := stripTOMLComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")))
			if section == "manual.apps" {
				overrides = append(overrides, manualAppOverride{})
				current = len(overrides) - 1
			} else {
				current = -1
			}
			continue
		}
		if current < 0 {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		stringValue := strings.Trim(value, "\"'")
		switch key {
		case "name":
			overrides[current].Name = stringValue
		case "aliases":
			overrides[current].Aliases = parseStringArray(value)
		case "category":
			overrides[current].Category = stringValue
		case "detail":
			overrides[current].Detail = stringValue
		case "managed_by":
			overrides[current].ManagedBy = stringValue
		case "lifecycle":
			overrides[current].Lifecycle = stringValue
		}
	}
	out := make([]manualAppOverride, 0, len(overrides))
	for _, override := range overrides {
		if strings.TrimSpace(override.Name) != "" {
			out = append(out, override)
		}
	}
	return out
}

func manualOverrideSections(overrides []manualAppOverride) []toolSection {
	byCategory := map[string][]toolRow{}
	for _, override := range overrides {
		category := manualOverrideCategory(override)
		row := manualAppRow(override.Name, "manual / "+category, override.Detail)
		if override.Lifecycle != "" {
			row.State = override.Lifecycle
		} else if override.ManagedBy != "" {
			row.State = override.ManagedBy
		}
		byCategory[category] = append(byCategory[category], row)
	}
	categories := make([]string, 0, len(byCategory))
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	sections := make([]toolSection, 0, len(categories))
	for _, category := range categories {
		sections = append(sections, toolSection{
			Name:  "manual/" + manualSectionKey(category),
			Title: "manual / " + category,
			Rows:  byCategory[category],
		})
	}
	return sections
}

func manualOverrideCategory(override manualAppOverride) string {
	category := strings.TrimSpace(override.Category)
	if category == "" {
		return "Overrides"
	}
	return category
}

func manualAppRowsFromTable(sectionTitle string, headers []string, cells []string) []toolRow {
	appIndex := manualAppColumnIndex(headers)
	if appIndex < 0 || appIndex >= len(cells) {
		return nil
	}
	details := []string{strings.TrimPrefix(sectionTitle, "manual / ")}
	for index, cell := range cells {
		if index == appIndex || index >= len(headers) {
			continue
		}
		value := cleanManualMarkdown(cell)
		if value == "" {
			continue
		}
		details = append(details, cleanManualMarkdown(headers[index])+": "+value)
	}
	rows := []toolRow{}
	for _, name := range splitManualAppNames(cells[appIndex]) {
		rows = append(rows, manualAppRow(name, sectionTitle, strings.Join(details, "; ")))
	}
	return rows
}

func manualAppRow(name string, sectionTitle string, detail string) toolRow {
	name = cleanManualMarkdown(name)
	if detail == "" {
		detail = strings.TrimPrefix(sectionTitle, "manual / ")
	}
	return toolRow{Name: name, State: "manual", Detail: detail}
}

func manualAppColumnIndex(headers []string) int {
	for index, header := range headers {
		normalized := strings.ToLower(cleanManualMarkdown(header))
		if normalized == "アプリ" || normalized == "app" || normalized == "application" || normalized == "name" {
			return index
		}
	}
	if len(headers) > 0 {
		return 0
	}
	return -1
}

func splitManualAppNames(value string) []string {
	value = cleanManualMarkdown(value)
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	raw := strings.Split(line, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func markdownTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.Trim(cell, " :-")
		if trimmed != "" {
			return false
		}
	}
	return true
}

func cleanManualMarkdown(value string) string {
	value = strings.TrimSpace(value)
	value = markdownLinkPattern.ReplaceAllString(value, "$1 ($2)")
	value = strings.ReplaceAll(value, "`", "")
	return strings.Join(strings.Fields(value), " ")
}

func manualRowMatches(section toolSection, row toolRow, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		section.Name,
		section.Title,
		row.Name,
		row.State,
		row.Detail,
	}, " "))
	return strings.Contains(haystack, query)
}

func manualSectionKey(title string) string {
	switch {
	case strings.Contains(title, "Adobe"):
		return "adobe"
	case strings.Contains(title, "Mac App Store"):
		return "app-store"
	case strings.Contains(title, "ベンダー"):
		return "vendor"
	case strings.Contains(title, "親アプリ"):
		return "bundled"
	case strings.Contains(title, "Intel"):
		return "homebrew-tap"
	case strings.Contains(title, "ハードウェア"):
		return "hardware"
	case strings.Contains(title, "業務"):
		return "business"
	default:
		key := strings.ToLower(title)
		replacer := strings.NewReplacer(" ", "-", "/", "-", "・", "-", "（", "-", "）", "-")
		key = replacer.Replace(key)
		key = strings.Trim(key, "-")
		if key == "" {
			return "other"
		}
		return key
	}
}
