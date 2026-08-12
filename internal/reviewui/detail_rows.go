package reviewui

import (
	"strings"
)

type DetailRow struct {
	Title         string
	Status        string
	Summary       string
	Detail        string
	Metadata      []string
	Actions       []DetailAction
	Columns       []string
	ColumnHeaders []DetailColumn
}

type DetailAction struct {
	Value       string
	Label       string
	Description string
}

type DetailColumn struct {
	Header string
	Min    int
	Max    int
}

type DetailRowGroupFunc func(DetailRow) (string, string)

func DetailRowsToSections(rows []DetailRow, group DetailRowGroupFunc) []Section {
	sections := []Section{}
	indexByName := map[string]int{}
	for _, row := range rows {
		name, title := group(row)
		if strings.TrimSpace(name) == "" {
			name = "details"
		}
		if strings.TrimSpace(title) == "" {
			title = strings.ReplaceAll(name, "/", " / ")
		}
		sectionIndex, ok := indexByName[name]
		if !ok {
			sectionIndex = len(sections)
			indexByName[name] = sectionIndex
			sections = append(sections, Section{Name: name, Title: title})
		}
		sections[sectionIndex].Rows = append(sections[sectionIndex].Rows, DetailRowToRow(row))
	}
	return sections
}

func DetailRowToRow(row DetailRow) Row {
	actions := make([]Action, 0, len(row.Actions))
	for _, action := range row.Actions {
		actions = append(actions, Action{
			Value:       action.Value,
			Label:       action.Label,
			Description: action.Description,
		})
	}
	return Row{
		Name:    row.Title,
		State:   firstNonEmpty(row.Status, "ok"),
		Detail:  detailRowText(row),
		Actions: actions,
	}
}

func detailRowText(row DetailRow) string {
	lines := []string{}
	if strings.TrimSpace(row.Summary) != "" {
		lines = append(lines, "summary: "+strings.TrimSpace(row.Summary))
	}
	if strings.TrimSpace(row.Detail) != "" {
		lines = append(lines, detailTextLines(row.Detail)...)
	}
	for _, meta := range row.Metadata {
		if strings.TrimSpace(meta) != "" {
			lines = append(lines, strings.TrimSpace(meta))
		}
	}
	return strings.Join(lines, "\n")
}

func detailTextLines(detail string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(detail, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isExpandedKeyValueLine(line) {
			lines = append(lines, line)
		} else {
			lines = append(lines, "detail: "+line)
		}
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
