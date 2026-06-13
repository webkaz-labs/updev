package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/legacycache"
	"github.com/webkaz-labs/updev/internal/manualinventory"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updevpath"
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

type manualStructuredApp = manualinventory.StructuredApp
type manualMASApp = manualinventory.MASApp

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
	for _, content := range configuredManualMarkdownInventories(root) {
		sections = append(sections, parseManualAppSections(content)...)
	}
	if structured := configuredManualStructuredAppSections(root); len(structured) > 0 {
		sections = append(sections, structured...)
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
			if manualRowHiddenByDefault(section, row, filters) {
				continue
			}
			if filters.status != "" && !legacycache.RowStatusMatches(row.State, filters.status) {
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

func manualRowHiddenByDefault(section toolSection, row toolRow, filters listOptions) bool {
	if section.Name != "manual/installed-apps" || row.State != "brew" {
		return false
	}
	return filters.status == "" && filters.query == ""
}

func manualAppSectionsForInventoryCommand(root string) []toolSection {
	return manualAppSections(root, listOptions{root: root, provider: manualProviderName}, manualCachedInventoryItems(root))
}

func manualCachedInventoryItems(root string) []plan.Item {
	entry, ok := loadInventoryCache(root, inventoryCacheMaxAge, inventoryOptions{IncludeVSCode: false})
	if !ok {
		return manualLiveCaskInventoryItems(root, runner.Local{})
	}
	return entry.Report.Items
}

func manualLiveCaskInventoryItems(root string, local runner.Runner) []plan.Item {
	return manualinventory.LiveCaskInventoryItems(root, shouldUseHomeBrewfile(root), local)
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
		details := []string{
			"source: homebrew cask",
			"cask: " + item.Name,
			"managed_by: brew",
			"update_owner: brew",
			"ownership_confidence: high",
			"provider_metadata: Homebrew cask inventory",
		}
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
			Actions: []reviewui.Action{
				manualReviewRouteActionForTarget(manualCaskDisplayName(item.Name), "brew", "cask"),
			},
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

func manualHomebrewTapSectionTitle(title string) bool {
	return strings.Contains(title, "Homebrew Tap") || strings.Contains(title, "自動配布")
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

type manualInstalledAppGroupRule struct {
	Name  string
	Title string
	Match func(toolRow) bool
}

func groupManualInstalledAppSections(sections []toolSection) []toolSection {
	if len(sections) == 0 {
		return sections
	}
	groupedRows := map[string][]toolRow{}
	out := make([]toolSection, 0, len(sections))
	for _, section := range sections {
		if section.Name != "manual/installed-apps" {
			out = append(out, section)
			continue
		}
		for _, row := range section.Rows {
			group := manualInstalledAppGroup(row)
			groupedRows[group.Name] = append(groupedRows[group.Name], row)
		}
	}
	for _, group := range manualInstalledAppGroupRules() {
		rows := groupedRows[group.Name]
		if len(rows) == 0 {
			continue
		}
		out = append(out, toolSection{
			Name:  group.Name,
			Title: group.Title,
			Rows:  rows,
		})
	}
	return out
}

func manualInstalledAppGroup(row toolRow) manualInstalledAppGroupRule {
	for _, group := range manualInstalledAppGroupRules() {
		if group.Match(row) {
			return group
		}
	}
	return manualInstalledAppFallbackGroup()
}

func manualInstalledAppGroupRules() []manualInstalledAppGroupRule {
	return []manualInstalledAppGroupRule{
		{
			Name:  "manual/installed-app-store",
			Title: "manual / Installed / App Store",
			Match: func(row toolRow) bool {
				source := manualinventory.DetailValue(row.Detail, "source")
				managedBy := manualinventory.DetailValue(row.Detail, "managed_by")
				return strings.EqualFold(managedBy, "mas") ||
					strings.EqualFold(source, "mac app store receipt") ||
					strings.EqualFold(source, "mas list") ||
					manualinventory.DetailValue(row.Detail, "mas_id") != ""
			},
		},
		{
			Name:  "manual/installed-homebrew-casks",
			Title: "manual / Installed / Homebrew casks",
			Match: func(row toolRow) bool {
				source := manualinventory.DetailValue(row.Detail, "source")
				return row.State == "brew" ||
					strings.EqualFold(source, "homebrew cask") ||
					manualinventory.DetailValue(row.Detail, "cask") != ""
			},
		},
		{
			Name:  "manual/installed-overrides",
			Title: "manual / Installed / Overrides",
			Match: func(row toolRow) bool {
				switch row.State {
				case "local-only", "ignored", "draft":
					return true
				default:
					return false
				}
			},
		},
		manualInstalledAppFallbackGroup(),
	}
}

func manualInstalledAppFallbackGroup() manualInstalledAppGroupRule {
	return manualInstalledAppGroupRule{
		Name:  "manual/installed-vendor-apps",
		Title: "manual / Installed / Vendor and standalone",
		Match: func(toolRow) bool {
			return true
		},
	}
}

func manualEvidenceSection(name string) bool {
	return name == "manual/installed-apps" || name == "manual/mac-app-store" || name == "manual/homebrew-casks" || name == "manual/homebrew-tap"
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
	if bundleID := manualinventory.DetailValue(row.Detail, "bundle_id"); bundleID != "" {
		keys = append(keys, "bundle:"+strings.ToLower(bundleID))
	}
	if masID := manualinventory.DetailValue(row.Detail, "mas_id"); masID != "" {
		keys = append(keys, "mas:"+strings.ToLower(masID))
	}
	if path := manualinventory.DetailValue(row.Detail, "path"); path != "" {
		for _, key := range manualAppPathKeys(path) {
			if key != "" {
				keys = append(keys, "name:"+key)
			}
		}
	}
	if cask := manualinventory.DetailValue(row.Detail, "cask"); cask != "" {
		for _, key := range manualAppKeys(manualCaskDisplayName(cask)) {
			if key != "" {
				keys = append(keys, "name:"+key)
			}
		}
	}
	for _, key := range manualAppKeys(row.Name) {
		if key != "" {
			keys = append(keys, "name:"+key)
		}
	}
	return keys
}

func manualAppPathKeys(path string) []string {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	base = strings.TrimSuffix(base, ".app")
	if base == "" || base == "." || base == string(filepath.Separator) {
		return nil
	}
	return manualAppKeys(base)
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
	row.Actions = reviewui.MergeActions(row.Actions, live.Actions)
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
	row.Actions = reviewui.MergeActions(row.Actions, live.Actions)
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
		return joinManualDetailParts(left, right)
	}
}

func joinManualDetailParts(values ...string) string {
	parts := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ";") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "; ")
}

type manualScannedApp = manualinventory.App

type manualReviewCandidate = manualinventory.ReviewCandidate
type manualReviewEvidence = manualinventory.ReviewEvidence
type manualReviewOverrideFields = manualinventory.ReviewOverrideFields

func manualReviewRow(sectionName string, row toolRow) manualinventory.ReviewRow {
	return manualinventory.ReviewRow{
		SectionName: sectionName,
		Name:        row.Name,
		State:       row.State,
		Detail:      row.Detail,
		Version:     row.Version,
	}
}

func manualScannedAppSections(root string) []toolSection {
	apps := scanManualApplications(root)
	if len(apps) == 0 {
		return nil
	}
	rows := make([]toolRow, 0, len(apps))
	agentAvailable := manualAgentEnrichmentAvailable(root)
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
		details = append(details, manualScannedAppOwnershipDetails(app)...)
		actions := []reviewui.Action{
			manualReviewRouteActionForTarget(app.Name, manualProviderName, "app"),
		}
		if agentAvailable {
			actions = append(actions, reviewui.Action{
				Value:       manualPlanDetailActionValue("enrich", app.Name),
				Label:       tr("generate draft", "draft を生成"),
				Description: tr("run the configured agent and append draft manual metadata", "設定済み agent を実行して draft manual metadata を追記します"),
				Badge:       "man",
				BadgeStatus: "drift",
			})
		}
		rows = append(rows, toolRow{
			Name:    app.Name,
			Version: app.Version,
			State:   "installed",
			Detail:  strings.Join(details, "; "),
			Actions: actions,
		})
	}
	return []toolSection{{
		Name:  "manual/installed-apps",
		Title: "manual / Installed apps",
		Rows:  rows,
	}}
}

func manualScannedAppOwnershipDetails(app manualScannedApp) []string {
	switch app.Source {
	case "mac app store receipt":
		return []string{
			"managed_by: mas",
			"update_owner: mas",
			"ownership_confidence: high",
			"provider_metadata: mac app store receipt",
		}
	default:
		return []string{
			"managed_by: manual",
			"update_owner: unknown",
			"ownership_confidence: low",
			"provider_metadata: Info.plist",
		}
	}
}

func scanManualApplications(root string) []manualScannedApp {
	return manualinventory.ScanApplications(root, defaultRoot())
}

func manualMASListSections(root string) []toolSection {
	return manualMASListSectionsFromApps(manualinventory.InstalledMASApps(root, defaultRoot(), runner.Local{}))
}

func manualMASListSectionsFromOutput(output string) []toolSection {
	return manualMASListSectionsFromApps(parseManualMASList(output))
}

func manualMASListSectionsFromApps(apps []manualMASApp) []toolSection {
	if len(apps) == 0 {
		return nil
	}
	rows := make([]toolRow, 0, len(apps))
	for _, app := range apps {
		details := []string{"source: mas list"}
		if app.ID != "" {
			details = append(details, "mas_id: "+app.ID)
		}
		if app.Version != "" {
			details = append(details, "version: "+app.Version)
		}
		details = append(details,
			"managed_by: mas",
			"update_owner: mas",
			"ownership_confidence: high",
			"provider_metadata: mas list",
		)
		rows = append(rows, toolRow{
			Name:    app.Name,
			Version: app.Version,
			State:   "installed",
			Detail:  strings.Join(details, "; "),
		})
	}
	return []toolSection{{
		Name:  "manual/mac-app-store",
		Title: "manual / Mac App Store",
		Rows:  rows,
	}}
}

func parseManualMASList(output string) []manualMASApp {
	return manualinventory.ParseMASList(output)
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
				if !manualRowIsHomebrewTapEvidence(row) {
					summary.Desired++
				}
				summary.Live++
			case "local-only", "ignored":
				if manualRowHasLiveEvidence(row) {
					summary.Live++
				}
			case "draft":
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
	return manualinventory.DetailValue(row.Detail, "path") != "" ||
		manualinventory.DetailValue(row.Detail, "mas_id") != "" ||
		strings.Contains(row.Detail, "source: homebrew cask")
}

func manualRowIsHomebrewTapEvidence(row toolRow) bool {
	return row.State == "brew" && strings.Contains(row.Detail, "source: homebrew tap docs")
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
				Confidence:      manualinventory.ReviewConfidence(manualReviewRow(section.Name, row)),
				Params: map[string]string{
					"name": row.Name,
				},
				Evidence: []manualReviewEvidence{manualReviewEvidenceFromRow(row)},
				SuggestedOverride: manualReviewOverrideFields{
					Name:      row.Name,
					Aliases:   manualinventory.SuggestedAliases(manualReviewRow(section.Name, row)),
					ManagedBy: manualinventory.SuggestedManagedBy(manualReviewRow(section.Name, row)),
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
	return manualinventory.EvidenceFromRow(manualReviewRow("", row))
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
	for _, content := range configuredManualMarkdownInventories(root) {
		for _, section := range parseManualAppSections(content) {
			for _, row := range section.Rows {
				addManualIndexRow(index, row, nil)
			}
		}
	}
	for _, app := range configuredManualStructuredApps(root) {
		if !manualStructuredAppAccepted(app) {
			continue
		}
		row := manualStructuredAppRow(app)
		addManualIndexRow(index, row, app.Aliases)
	}
	for _, override := range loadManualAppOverrides(root) {
		row := manualAppRow(override.Name, "manual / "+manualOverrideCategory(override), override.Detail)
		addManualIndexRow(index, row, override.Aliases)
	}
	return index
}

func manualHomebrewTapIndex(root string) map[string]toolRow {
	index := map[string]toolRow{}
	for _, content := range configuredManualMarkdownInventories(root) {
		for _, section := range parseManualAppSections(content) {
			if section.Name != "manual/homebrew-tap" {
				continue
			}
			for _, row := range section.Rows {
				addManualIndexRow(index, row, nil)
			}
		}
	}
	return index
}

func configuredManualStructuredAppSections(root string) []toolSection {
	apps := configuredManualStructuredApps(root)
	if len(apps) == 0 {
		return nil
	}
	byCategory := map[string][]toolRow{}
	for _, app := range apps {
		row := manualStructuredAppRow(app)
		category := manualStructuredAppCategory(app)
		if !manualStructuredAppAccepted(app) {
			category = "Agent drafts"
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
		name := "manual/" + manualSectionKey(category)
		if category == "Agent drafts" {
			name = "manual/installed-apps"
		}
		sections = append(sections, toolSection{
			Name:  name,
			Title: "manual / " + category,
			Rows:  byCategory[category],
		})
	}
	return sections
}

func configuredManualStructuredApps(root string) []manualStructuredApp {
	paths := configuredManualStructuredInventoryPaths(root)
	if len(paths) == 0 {
		return nil
	}
	out := []manualStructuredApp{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, manualinventory.ParseStructuredApps(string(data))...)
	}
	return out
}

func configuredManualStructuredInventoryPaths(root string) []string {
	config := loadUpdevConfig()
	paths := []string{}
	for _, source := range config.Inventory.Manual.Sources {
		if manualinventory.SourceIsStructured(source) {
			if path := resolveUpdevConfigPath(root, source); path != "" {
				paths = append(paths, path)
			}
		}
	}
	return dedupeStrings(paths)
}

func configuredManualMarkdownInventories(root string) []string {
	paths := configuredManualMarkdownInventoryPaths(root)
	if len(paths) == 0 {
		return nil
	}
	out := []string{}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err == nil {
			out = append(out, string(content))
		}
	}
	return out
}

func configuredManualMarkdownInventoryPaths(root string) []string {
	config := loadUpdevConfig()
	paths := []string{}
	if config.Inventory.Manual.MarkdownCompat != nil && *config.Inventory.Manual.MarkdownCompat {
		paths = append(paths, resolveUpdevConfigPath(root, "docs/apps.md"))
	}
	for _, source := range config.Inventory.Manual.Sources {
		if manualinventory.SourceIsMarkdown(source) {
			if path := resolveUpdevConfigPath(root, source); path != "" {
				paths = append(paths, path)
			}
		}
	}
	return dedupeStrings(paths)
}

func dedupeStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func addManualIndexRow(index map[string]toolRow, row toolRow, aliases []string) {
	for _, key := range manualAppKeys(row.Name) {
		if key != "" {
			index[key] = row
		}
	}
	if cask := manualinventory.DetailValue(row.Detail, "cask"); cask != "" {
		aliases = append(aliases, cask)
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
	return updevpath.InventoryOverridesFile()
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

func manualStructuredAppAccepted(app manualStructuredApp) bool {
	return manualinventory.StructuredAppAccepted(app)
}

func manualStructuredAppCategory(app manualStructuredApp) string {
	category := strings.TrimSpace(app.Category)
	if category == "" {
		return "Manual apps"
	}
	return category
}

func manualStructuredAppRow(app manualStructuredApp) toolRow {
	category := manualStructuredAppCategory(app)
	details := []string{}
	if app.Detail != "" {
		details = append(details, app.Detail)
	}
	if app.ManagedBy != "" {
		details = append(details, "managed_by: "+app.ManagedBy)
	}
	if app.Lifecycle != "" {
		details = append(details, "lifecycle: "+app.Lifecycle)
	}
	if app.Confidence != "" {
		details = append(details, "confidence: "+app.Confidence)
	}
	if app.ReviewStatus != "" {
		details = append(details, "review_status: "+app.ReviewStatus)
	}
	for _, key := range []string{"bundle_id", "mas_id", "cask", "path"} {
		if value := strings.TrimSpace(app.Identifiers[key]); value != "" {
			details = append(details, key+": "+value)
		}
	}
	if source := strings.TrimSpace(app.Provenance["source"]); source != "" {
		details = append(details, "source: "+source)
	}
	for _, key := range manualinventory.StructuredProvenanceDetailKeys() {
		if value := strings.TrimSpace(app.Provenance[key]); value != "" {
			details = append(details, key+": "+value)
		}
	}
	if len(app.Evidence) > 0 {
		details = append(details, "evidence: "+strings.Join(app.Evidence, ", "))
	}
	if len(details) == 0 {
		details = append(details, category)
	}
	row := manualAppRow(app.Name, "manual / "+category, strings.Join(details, "; "))
	switch {
	case !manualStructuredAppAccepted(app):
		row.State = "draft"
		row.Actions = []reviewui.Action{
			manualDraftReviewAction("accept-draft", app.Name, tr("accept draft", "draft を採用"), tr("mark this structured manual app draft as accepted", "この structured manual app draft を accepted にします")),
			manualDraftReviewAction("edit-draft", app.Name, tr("edit draft", "draft を編集"), tr("edit this structured manual app draft before keeping it", "保持する前にこの structured manual app draft を編集します")),
			manualDraftReviewAction("ignore-draft", app.Name, tr("ignore draft", "draft を破棄"), tr("remove this structured manual app draft", "この structured manual app draft を削除します")),
		}
	case app.Lifecycle != "":
		row.State = app.Lifecycle
	case app.ManagedBy != "":
		row.State = app.ManagedBy
	default:
		row.State = "manual"
	}
	return row
}

func manualDraftReviewAction(action string, target string, label string, description string) reviewui.Action {
	return reviewui.Action{
		Value:       manualPlanDetailActionValue(action, target),
		Label:       label,
		Description: description,
		Badge:       "man",
		BadgeStatus: "drift",
	}
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
	if manualHomebrewTapSectionTitle(sectionTitle) {
		return manualHomebrewTapRowsFromTable(sectionTitle, headers, cells, appIndex)
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

func manualHomebrewTapRowsFromTable(sectionTitle string, headers []string, cells []string, appIndex int) []toolRow {
	cask := ""
	details := []string{strings.TrimPrefix(sectionTitle, "manual / "), "source: homebrew tap docs"}
	for index, cell := range cells {
		if index == appIndex || index >= len(headers) {
			continue
		}
		header := cleanManualMarkdown(headers[index])
		value := cleanManualMarkdown(cell)
		if value == "" {
			continue
		}
		if strings.Contains(header, "Cask") {
			cask = value
			details = append(details, "cask: "+value)
			continue
		}
		details = append(details, header+": "+value)
	}
	rows := []toolRow{}
	for _, name := range splitManualAppNames(cells[appIndex]) {
		row := manualAppRow(name, sectionTitle, strings.Join(details, "; "))
		row.State = "brew"
		if cask != "" {
			row.Actions = []reviewui.Action{
				manualReviewRouteActionForTarget(name, "brew", "cask"),
			}
		}
		rows = append(rows, row)
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
