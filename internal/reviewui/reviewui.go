package reviewui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/textui"
)

const defaultExpandedDetailWidth = 80
const maxActionBadgeWidth = 18

const (
	actionBadgeMarker        = "▶"
	actionBadgeBackendToken  = "bak"
	actionBadgeUpdateToken   = "upd"
	actionBadgeUpdatedToken  = "up"
	actionBadgeHoldToken     = "hold"
	actionBadgeSecurityToken = "sec"
	actionBadgeManualToken   = "man"
	actionBadgeFilterToken   = "flt"
)

var detailURLPattern = regexp.MustCompile(`https?://\S+`)
var nerdFontHintCache = struct {
	sync.Mutex
	key     string
	ready   bool
	enabled bool
}{}

type Section struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Rows  []Row  `json:"rows"`
}

type Row struct {
	Name              string   `json:"name"`
	Version           string   `json:"version,omitempty"`
	Wanted            string   `json:"wanted,omitempty"`
	State             string   `json:"state,omitempty"`
	Detail            string   `json:"detail,omitempty"`
	Actions           []Action `json:"-"`
	TranslationKey    string   `json:"-"`
	TranslationSource string   `json:"-"`
}

type Action struct {
	Value       string
	Label       string
	Description string
	Badge       string
	BadgeStatus string
}

type State struct {
	Selected int
	Offset   int
	Query    string
	Expanded map[int]bool
	Action   string
}

type Labels struct {
	Name          string
	Version       string
	Wanted        string
	State         string
	Action        string
	Detail        string
	MoreRows      func(int) string
	NoExtraDetail string
}

func DefaultLabels() Labels {
	return Labels{
		Name:          "name",
		Version:       "version",
		Wanted:        "wanted",
		State:         "state",
		Action:        "check",
		Detail:        "detail",
		MoreRows:      func(count int) string { return fmt.Sprintf("... %d more rows", count) },
		NoExtraDetail: "no additional detail",
	}
}

func RowCount(sections []Section) int {
	count := 0
	for _, section := range sections {
		count += len(section.Rows)
	}
	return count
}

func HasWanted(section Section) bool {
	for _, row := range section.Rows {
		if row.Wanted != "" {
			return true
		}
	}
	return false
}

func LimitedRows(rows []Row, limit int) []Row {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func FilteredSections(sections []Section, rawQuery string) []Section {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return sections
	}
	out := make([]Section, 0, len(sections))
	for _, section := range sections {
		rows := make([]Row, 0, len(section.Rows))
		for _, row := range section.Rows {
			if RowMatches(section, row, query) {
				rows = append(rows, row)
			}
		}
		if len(rows) > 0 {
			filtered := section
			filtered.Rows = rows
			out = append(out, filtered)
		}
	}
	return out
}

func RowMatches(section Section, row Row, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		section.Name,
		section.Title,
		row.Name,
		row.Version,
		row.Wanted,
		row.State,
		row.Detail,
	}, " "))
	for _, action := range row.Actions {
		haystack += " " + strings.ToLower(action.Label+" "+action.Description)
	}
	return strings.Contains(haystack, query)
}

func StyledRow(row Row, includeWanted bool, color bool) []string {
	dimmed := RowDimmed(row)
	name := textui.StyleName(row.Name, color)
	version := styleVersion(row.Version, dimmed, color)
	state := styleState(row.State, color)
	action := actionBadge(row.Actions, color)
	detail := styleDetail(tableDetail(row.Detail), row.State, color)
	if includeWanted {
		wanted := styleWanted(row.Wanted, dimmed, color)
		return []string{name, version, wanted, state, action, detail}
	}
	return []string{name, version, state, action, detail}
}

func actionBadge(actions []Action, color bool) string {
	labels := []actionBadgeEntry{}
	seen := map[string]bool{}
	for _, action := range actions {
		label := compactAction(action)
		if label.Text == "" || seen[label.Text] {
			continue
		}
		seen[label.Text] = true
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return ""
	}
	sort.SliceStable(labels, func(i int, j int) bool {
		return actionBadgePriority(labels[i].Text) < actionBadgePriority(labels[j].Text)
	})
	return joinActionBadges(labels, maxActionBadgeWidth, color)
}

type actionBadgeEntry struct {
	Text   string
	Status string
}

func compactAction(action Action) actionBadgeEntry {
	if badge := strings.TrimSpace(action.Badge); badge != "" {
		return actionBadgeEntry{Text: compactActionCustomBadge(badge), Status: strings.TrimSpace(action.BadgeStatus)}
	}
	return actionBadgeEntry{Text: compactActionLabel(action.Label)}
}

func compactActionCustomBadge(badge string) string {
	fields := strings.Fields(strings.TrimSpace(badge))
	if len(fields) == 0 {
		return ""
	}
	token := fields[0]
	if !knownActionBadgeToken(token) {
		return strings.TrimSpace(badge)
	}
	out := actionBadgeToken(token)
	if len(fields) > 1 {
		out += " " + strings.Join(fields[1:], " ")
	}
	return out
}

func knownActionBadgeToken(token string) bool {
	switch token {
	case actionBadgeBackendToken, actionBadgeUpdateToken, actionBadgeUpdatedToken, actionBadgeHoldToken, actionBadgeSecurityToken, actionBadgeManualToken, actionBadgeFilterToken:
		return true
	default:
		return false
	}
}

func compactActionLabel(label string) string {
	label = strings.TrimSpace(label)
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "backend"):
		return actionBadgeToken(actionBadgeBackendToken)
	case strings.Contains(lower, "update"):
		return actionBadgeToken(actionBadgeUpdateToken)
	case strings.Contains(lower, "security"):
		return actionBadgeToken(actionBadgeSecurityToken)
	case strings.Contains(lower, "manual"):
		return actionBadgeToken(actionBadgeManualToken)
	case strings.Contains(lower, "filter"):
		return actionBadgeToken(actionBadgeFilterToken)
	case strings.Contains(label, "backend"):
		return actionBadgeToken(actionBadgeBackendToken)
	case strings.Contains(label, "更新"):
		return actionBadgeToken(actionBadgeUpdateToken)
	case strings.Contains(label, "security") || strings.Contains(label, "セキュリティ"):
		return actionBadgeToken(actionBadgeSecurityToken)
	case strings.Contains(label, "手動") || strings.Contains(label, "manual"):
		return actionBadgeToken(actionBadgeManualToken)
	}
	replacements := []struct {
		old string
		new string
	}{
		{"を開く", ""},
		{"を確認", ""},
		{"open ", ""},
		{"review ", ""},
	}
	for _, replacement := range replacements {
		label = strings.ReplaceAll(label, replacement.old, replacement.new)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return textui.Truncate(label, 18)
}

func actionBadgePriority(label string) int {
	base := actionBadgeBaseToken(label)
	if fields := strings.Fields(base); len(fields) > 0 {
		base = fields[0]
	}
	switch base {
	case actionBadgeUpdatedToken:
		return 10
	case actionBadgeUpdateToken:
		return 15
	case actionBadgeSecurityToken:
		return 20
	case actionBadgeHoldToken:
		return 25
	case actionBadgeManualToken:
		return 30
	case actionBadgeBackendToken:
		return 40
	case actionBadgeFilterToken:
		return 50
	default:
		return 100
	}
}

func joinActionBadges(labels []actionBadgeEntry, maxWidth int, color bool) string {
	raw := ""
	styled := ""
	for index, label := range labels {
		candidateRaw := label.Text
		candidateStyled := styleActionBadge(label, color)
		if raw != "" {
			candidateRaw = raw + " " + label.Text
			candidateStyled = styled + " " + styleActionBadge(label, color)
		}
		remaining := len(labels) - index - 1
		if remaining > 0 {
			withOmitted := candidateRaw + fmt.Sprintf(" +%d", remaining)
			if textui.DisplayWidth(withOmitted) > maxWidth {
				if raw == "" {
					return styleActionBadge(actionBadgeEntry{Text: textui.Truncate(label.Text, maxWidth), Status: label.Status}, color)
				}
				return appendOmittedActionBadges(raw, styled, len(labels)-index, maxWidth)
			}
		} else if textui.DisplayWidth(candidateRaw) > maxWidth {
			if raw == "" {
				return styleActionBadge(actionBadgeEntry{Text: textui.Truncate(label.Text, maxWidth), Status: label.Status}, color)
			}
			return appendOmittedActionBadges(raw, styled, 1, maxWidth)
		}
		raw = candidateRaw
		styled = candidateStyled
	}
	return styled
}

func styleActionBadge(label actionBadgeEntry, color bool) string {
	if label.Status != "" {
		return textui.StyleStatus(label.Text, color)
	}
	return textui.StyleAction(label.Text, color)
}

func appendOmittedActionBadges(raw string, styled string, omitted int, maxWidth int) string {
	if omitted <= 0 {
		return styled
	}
	candidate := raw + fmt.Sprintf(" +%d", omitted)
	if textui.DisplayWidth(candidate) <= maxWidth {
		return styled + fmt.Sprintf(" +%d", omitted)
	}
	return textui.Truncate(styled, maxWidth)
}

func actionBadgeToken(ascii string) string {
	if actionBadgeIconsEnabled() {
		return actionBadgeNerdMarker(ascii) + ascii
	}
	return actionBadgeMarker + ascii
}

func actionBadgeNerdMarker(token string) string {
	switch token {
	case actionBadgeSecurityToken:
		return "🔒"
	case actionBadgeUpdatedToken:
		return "✅"
	case actionBadgeHoldToken:
		return "⏳"
	case actionBadgeUpdateToken:
		return "🔄"
	case actionBadgeManualToken:
		return "📝"
	case actionBadgeBackendToken:
		return "📦"
	case actionBadgeFilterToken:
		return "🔎"
	default:
		return actionBadgeMarker
	}
}

func actionBadgeBaseToken(label string) string {
	label = strings.TrimSpace(label)
	label = strings.TrimPrefix(label, actionBadgeMarker)
	for _, marker := range actionBadgeNerdMarkers() {
		label = strings.TrimPrefix(label, marker)
	}
	return strings.TrimSpace(label)
}

func actionBadgeNerdMarkers() []string {
	return []string{
		actionBadgeNerdMarker(actionBadgeSecurityToken),
		actionBadgeNerdMarker(actionBadgeUpdatedToken),
		actionBadgeNerdMarker(actionBadgeHoldToken),
		actionBadgeNerdMarker(actionBadgeUpdateToken),
		actionBadgeNerdMarker(actionBadgeManualToken),
		actionBadgeNerdMarker(actionBadgeBackendToken),
		actionBadgeNerdMarker(actionBadgeFilterToken),
	}
}

func actionBadgeIconsEnabled() bool {
	if value, ok := envSetting("UPDEV_NERD_FONT"); ok {
		return nerdFontSettingEnabled(value)
	}
	if value, ok := envSetting("NERD_FONT"); ok {
		return nerdFontSettingEnabled(value)
	}
	return detectNerdFontHint()
}

func nerdFontSettingEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "auto":
		return detectNerdFontHint()
	default:
		return false
	}
}

func envSetting(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func detectNerdFontHint() bool {
	key := strings.Join([]string{
		os.Getenv("TERM"),
		os.Getenv("WEZTERM_CONFIG_FILE"),
		os.Getenv("XDG_CONFIG_HOME"),
		os.Getenv("HOME"),
	}, "\x00")
	nerdFontHintCache.Lock()
	if nerdFontHintCache.ready && nerdFontHintCache.key == key {
		enabled := nerdFontHintCache.enabled
		nerdFontHintCache.Unlock()
		return enabled
	}
	nerdFontHintCache.Unlock()

	enabled := detectNerdFontHintUncached()

	nerdFontHintCache.Lock()
	nerdFontHintCache.key = key
	nerdFontHintCache.ready = true
	nerdFontHintCache.enabled = enabled
	nerdFontHintCache.Unlock()
	return enabled
}

func detectNerdFontHintUncached() bool {
	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return false
	}
	for _, path := range terminalConfigPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if configMentionsNerdFont(string(data)) {
			return true
		}
	}
	return false
}

func terminalConfigPaths() []string {
	paths := []string{}
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	add(os.Getenv("WEZTERM_CONFIG_FILE"))
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configHome = filepath.Join(home, ".config")
		}
	}
	if configHome != "" {
		add(filepath.Join(configHome, "wezterm", "wezterm.lua"))
		add(filepath.Join(configHome, "ghostty", "config"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config"))
	}
	return paths
}

func configMentionsNerdFont(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "nerd") || strings.Contains(value, " NF")
}

func RowDimmed(row Row) bool {
	return strings.EqualFold(strings.TrimSpace(row.State), "inactive")
}

func Columns(includeWanted bool, labels Labels) []textui.Column {
	labels = fillLabels(labels)
	if includeWanted {
		return []textui.Column{
			{Header: labels.Name, Min: 12, Max: 32},
			{Header: labels.Version, Min: 7, Max: 14},
			{Header: labels.Wanted, Min: 7, Max: 10},
			{Header: labels.State, Min: 6, Max: 8},
			{Header: labels.Action, Min: 5, Max: 18},
			{Header: labels.Detail, Min: 0, Max: 64},
		}
	}
	return []textui.Column{
		{Header: labels.Name, Min: 12, Max: 32},
		{Header: labels.Version, Min: 7, Max: 14},
		{Header: labels.State, Min: 6, Max: 8},
		{Header: labels.Action, Min: 5, Max: 18},
		{Header: labels.Detail, Min: 0, Max: 64},
	}
}

func PrintSections(w io.Writer, sections []Section, limit int, labels Labels, color bool) {
	labels = fillLabels(labels)
	for index, section := range sections {
		if index > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s %s\n", textui.StyleSection(section.Title, color), textui.StyleCount(fmt.Sprintf("(%d)", len(section.Rows)), color))
		visible := LimitedRows(section.Rows, limit)
		includeWanted := false
		for _, row := range visible {
			if row.Wanted != "" {
				includeWanted = true
				break
			}
		}
		rows := make([][]string, 0, len(visible))
		for _, row := range visible {
			rows = append(rows, StyledRow(row, includeWanted, color))
		}
		textui.PrintTable(w, Columns(includeWanted, labels), rows, color)
		PrintOmittedRows(w, len(section.Rows)-len(visible), labels, color)
	}
}

func StyledRows(rows []Row, includeWanted bool, color bool) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, StyledRow(row, includeWanted, color))
	}
	return out
}

func Header(columns []textui.Column, widths []int, color bool) string {
	values := make([]string, 0, len(columns))
	for i, column := range columns {
		values = append(values, textui.StyleHeading(textui.PadRight(column.Header, widths[i]), color))
	}
	return strings.Join(values, " ")
}

func TableRow(row []string, widths []int) string {
	values := make([]string, 0, len(widths))
	for i := range widths {
		value := ""
		if i < len(row) {
			value = row[i]
		}
		values = append(values, textui.PadRight(textui.Truncate(value, widths[i]), widths[i]))
	}
	return strings.Join(values, " ")
}

func ExpandedLines(row Row, labels Labels) []string {
	return ExpandedLinesWithWidth(row, labels, defaultExpandedDetailWidth)
}

func ExpandedLinesWithWidth(row Row, labels Labels, width int) []string {
	return ExpandedLinesWithWidthStyled(row, labels, width, false)
}

func ExpandedLinesWithWidthStyled(row Row, labels Labels, width int, color bool) []string {
	return ExpandedLinesWithWidthStyledFocus(row, labels, width, color, -1)
}

func ExpandedLinesWithWidthStyledFocus(row Row, labels Labels, width int, color bool, actionFocus int) []string {
	labels = fillLabels(labels)
	lines := []string{}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, textui.StyleSection(labels.Detail, color))
		lines = append(lines, expandedKeyValueLines(labels.Detail, row.Detail, expandedDetailWidth(width), color)...)
	}
	if len(row.Actions) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, textui.StyleSection("actions", color))
		for index, action := range row.Actions {
			if strings.TrimSpace(action.Label) == "" {
				continue
			}
			key := fmt.Sprintf("%d", index+1)
			if index == 0 {
				key = "a or 1"
			}
			prefix := "  "
			if index == actionFocus {
				prefix = textui.StyleRequested("> ", color)
			}
			line := prefix + textui.StyleKey(fmt.Sprintf("action %d [press %s]:", index+1, key), color) + " " + textui.StyleAction(action.Label, color)
			if strings.TrimSpace(action.Description) != "" {
				line += textui.StyleDim(" - "+strings.TrimSpace(action.Description), color)
			}
			lines = append(lines, textui.WrapText(line, expandedDetailWidth(width))...)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, labels.NoExtraDetail)
	}
	return lines
}

func expandedKeyValueLines(label string, value string, width int, color bool) []string {
	lines := []string{}
	rawLines := splitNonEmptyLines(strings.ReplaceAll(value, "\r\n", "\n"))
	for index, rawLine := range rawLines {
		line := strings.TrimSpace(rawLine)
		if isExpandedKeyValueLine(line) {
			lines = append(lines, textui.WrapText(expandedKeyValueLine(line, color), width)...)
			continue
		}
		key := label
		if index > 0 {
			key = "note"
		}
		lines = append(lines, textui.WrapText(textui.StyleKey(key+":", color)+" "+line, width)...)
	}
	return lines
}

func expandedKeyValueLine(line string, color bool) string {
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok {
		return line
	}
	return textui.StyleKey(strings.TrimSpace(key)+":", color) + " " + strings.TrimSpace(value)
}

func isExpandedKeyValueLine(line string) bool {
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	key = strings.TrimSpace(key)
	if !ok || key == "" || len([]rune(key)) > 32 || strings.Contains(key, "/") || strings.Contains(key, "\t") {
		return false
	}
	if keyLooksLikeURLSchemePrefix(key, value) {
		return false
	}
	if !strings.Contains(key, " ") {
		return true
	}
	switch strings.ToLower(key) {
	case "linked evidence", "update evidence", "security evidence", "backend evidence", "next action":
		return true
	case "関連 evidence", "更新根拠", "セキュリティ根拠", "backend 根拠", "次の操作":
		return true
	default:
		return false
	}
}

func keyLooksLikeURLSchemePrefix(key string, value string) bool {
	if !strings.HasPrefix(strings.TrimSpace(value), "//") {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, scheme := range []string{"http", "https", "file", "ssh"} {
		if lower == scheme || strings.HasSuffix(lower, scheme) {
			return true
		}
	}
	return false
}

func splitNonEmptyLines(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func expandedDetailWidth(width int) int {
	if width <= 0 {
		return defaultExpandedDetailWidth
	}
	if width < 40 {
		return 40
	}
	return width
}

func tableDetail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, line := range splitNonEmptyLines(strings.ReplaceAll(value, "\r\n", "\n")) {
		value = line
		break
	}
	if key, rest, ok := strings.Cut(value, ":"); ok {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "description", "summary":
			value = strings.TrimSpace(rest)
		}
		switch strings.TrimSpace(key) {
		case "説明", "概要":
			value = strings.TrimSpace(rest)
		}
	}
	withoutURLs := detailURLPattern.ReplaceAllString(value, "")
	return strings.TrimSpace(strings.Join(strings.Fields(withoutURLs), " "))
}

func PrintOmittedRows(w io.Writer, omitted int, labels Labels, color bool) {
	if omitted <= 0 {
		return
	}
	labels = fillLabels(labels)
	fmt.Fprintf(w, "  %s\n", textui.StyleDim(labels.MoreRows(omitted), color))
}

func styleVersion(value string, dimmed bool, color bool) string {
	if value == "" {
		return value
	}
	if dimmed {
		return textui.StyleDim(value, color)
	}
	return textui.StyleVersion(value, color)
}

func styleWanted(value string, dimmed bool, color bool) string {
	if value == "" {
		return value
	}
	if dimmed {
		return textui.StyleDim(value, color)
	}
	return textui.StyleRequested(value, color)
}

func styleState(value string, color bool) string {
	if value == "" {
		return value
	}
	return textui.StyleStatus(value, color)
}

func styleDetail(value string, status string, color bool) string {
	if value == "" {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "inactive":
		return textui.StyleDim(value, color)
	case "missing", "extra", "drift", "held", "hold", "review", "unavailable", "attention", "skipped", "deferred", "needs-review", "ignore-local", "adopt-brew", "adopt-mas", "open-vendor":
		return textui.StyleWarning(value, color)
	case "error", "blocked", "block":
		return textui.StyleError(value, color)
	default:
		return textui.StyleLabel(value, color)
	}
}

func fillLabels(labels Labels) Labels {
	defaults := DefaultLabels()
	if labels.Name == "" {
		labels.Name = defaults.Name
	}
	if labels.Version == "" {
		labels.Version = defaults.Version
	}
	if labels.Wanted == "" {
		labels.Wanted = defaults.Wanted
	}
	if labels.State == "" {
		labels.State = defaults.State
	}
	if labels.Action == "" {
		labels.Action = defaults.Action
	}
	if labels.Detail == "" {
		labels.Detail = defaults.Detail
	}
	if labels.MoreRows == nil {
		labels.MoreRows = defaults.MoreRows
	}
	if labels.NoExtraDetail == "" {
		labels.NoExtraDetail = defaults.NoExtraDetail
	}
	return labels
}
