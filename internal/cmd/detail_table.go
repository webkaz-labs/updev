package cmd

import (
	"strings"

	"github.com/webkaz-labs/updev/internal/reviewui"
)

type detailRowGroupFunc func(detailBrowserRow) (string, string)

func detailRowsToToolSections(rows []detailBrowserRow, group detailRowGroupFunc) []toolSection {
	sections := []toolSection{}
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
			sections = append(sections, toolSection{Name: name, Title: title})
		}
		sections[sectionIndex].Rows = append(sections[sectionIndex].Rows, detailRowToToolRow(row))
	}
	return sections
}

func detailRowToToolRow(row detailBrowserRow) toolRow {
	actions := make([]reviewui.Action, 0, len(row.Actions))
	for _, action := range row.Actions {
		actions = append(actions, reviewui.Action{
			Value:       action.Value,
			Label:       action.Label,
			Description: action.Description,
		})
	}
	return toolRow{
		Name:    row.Title,
		State:   firstNonEmpty(row.Status, "ok"),
		Detail:  detailRowToolDetail(row),
		Actions: actions,
	}
}

func detailRowToolDetail(row detailBrowserRow) string {
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
		if isDetailBrowserKeyValueLine(line) {
			lines = append(lines, line)
		} else {
			lines = append(lines, "detail: "+line)
		}
	}
	return lines
}
