package reviewui

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/webkaz-labs/updev/internal/textui"
)

const defaultExpandedDetailWidth = 80

var detailURLPattern = regexp.MustCompile(`https?://\S+`)

type Section struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Rows  []Row  `json:"rows"`
}

type Row struct {
	Name              string `json:"name"`
	Version           string `json:"version,omitempty"`
	Wanted            string `json:"wanted,omitempty"`
	State             string `json:"state,omitempty"`
	Detail            string `json:"detail,omitempty"`
	TranslationKey    string `json:"-"`
	TranslationSource string `json:"-"`
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
	return strings.Contains(haystack, query)
}

func StyledRow(row Row, includeWanted bool, color bool) []string {
	dimmed := RowDimmed(row)
	name := textui.StyleName(row.Name, color)
	version := styleVersion(row.Version, dimmed, color)
	state := styleState(row.State, color)
	detail := styleDetail(tableDetail(row.Detail), row.State, color)
	if includeWanted {
		wanted := styleWanted(row.Wanted, dimmed, color)
		return []string{name, version, wanted, state, detail}
	}
	return []string{name, version, state, detail}
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
			{Header: labels.Detail, Min: 0, Max: 72},
		}
	}
	return []textui.Column{
		{Header: labels.Name, Min: 12, Max: 32},
		{Header: labels.Version, Min: 7, Max: 14},
		{Header: labels.State, Min: 6, Max: 8},
		{Header: labels.Detail, Min: 0, Max: 72},
	}
}

func PrintSections(w io.Writer, sections []Section, limit int, labels Labels, color bool) {
	labels = fillLabels(labels)
	for index, section := range sections {
		if index > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(section.Title, color), textui.StyleCount(fmt.Sprintf("(%d)", len(section.Rows)), color))
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
	labels = fillLabels(labels)
	lines := []string{}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, textui.WrapText(labels.Detail+": "+strings.TrimSpace(row.Detail), expandedDetailWidth(width))...)
	}
	if row.Version != "" {
		lines = append(lines, labels.Version+": "+row.Version)
	}
	if row.Wanted != "" {
		lines = append(lines, labels.Wanted+": "+row.Wanted)
	}
	if row.State != "" {
		lines = append(lines, labels.State+": "+row.State)
	}
	if len(lines) == 0 {
		lines = append(lines, labels.NoExtraDetail)
	}
	return lines
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
	case "missing", "extra", "drift", "held", "hold", "review", "unavailable", "attention", "skipped", "deferred":
		return textui.StyleWarning(value, color)
	case "error", "blocked":
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
