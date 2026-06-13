package manualinventory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StructuredApp struct {
	Name         string
	Aliases      []string
	Category     string
	Detail       string
	ManagedBy    string
	Lifecycle    string
	Confidence   string
	ReviewStatus string
	Identifiers  map[string]string
	Provenance   map[string]string
	Evidence     []string
}

type StructuredAppRawBlock struct {
	App   StructuredApp
	Start int
	End   int
	Text  string
}

func SourceIsMarkdown(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

func SourceIsStructured(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(lower, ".toml")
}

func ParseStructuredApps(content string) []StructuredApp {
	apps := []StructuredApp{}
	current := -1
	subsection := ""
	for _, raw := range strings.Split(collapseMultilineArrays(content), "\n") {
		line := stripComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")))
			if section == "manual.apps" {
				apps = append(apps, StructuredApp{
					Identifiers: map[string]string{},
					Provenance:  map[string]string{},
				})
				current = len(apps) - 1
				subsection = "manual.apps"
			} else {
				current = -1
				subsection = section
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			subsection = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
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
		app := &apps[current]
		switch subsection {
		case "manual.apps":
			switch key {
			case "name":
				app.Name = stringValue
			case "aliases":
				app.Aliases = parseStringArray(value)
			case "category":
				app.Category = stringValue
			case "description", "detail":
				app.Detail = stringValue
			case "managed_by":
				app.ManagedBy = stringValue
			case "lifecycle":
				app.Lifecycle = stringValue
			case "confidence":
				app.Confidence = stringValue
			case "review_status":
				app.ReviewStatus = strings.ToLower(stringValue)
			}
		case "manual.apps.identifiers":
			if app.Identifiers == nil {
				app.Identifiers = map[string]string{}
			}
			app.Identifiers[key] = stringValue
		case "manual.apps.provenance":
			switch key {
			case "evidence":
				app.Evidence = parseStringArray(value)
			default:
				if app.Provenance == nil {
					app.Provenance = map[string]string{}
				}
				app.Provenance[key] = stringValue
			}
		}
	}
	out := make([]StructuredApp, 0, len(apps))
	for _, app := range apps {
		if strings.TrimSpace(app.Name) != "" {
			out = append(out, app)
		}
	}
	return out
}

func StructuredAppAccepted(app StructuredApp) bool {
	return strings.EqualFold(strings.TrimSpace(app.ReviewStatus), "accepted")
}

func StructuredAppMatchesQuery(app StructuredApp, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	parts := []string{app.Name, app.Category, app.Detail, app.ManagedBy, app.Lifecycle, app.Confidence, app.ReviewStatus}
	parts = append(parts, app.Aliases...)
	for _, key := range []string{"bundle_id", "mas_id", "cask", "path"} {
		parts = append(parts, app.Identifiers[key])
	}
	for _, value := range app.Provenance {
		parts = append(parts, value)
	}
	parts = append(parts, app.Evidence...)
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func StructuredProvenanceDetailKeys() []string {
	return []string{
		"source_url",
		"review_url",
		"homepage",
		"owner",
		"publisher",
		"developer",
		"vendor",
		"update_owner",
		"provider_metadata",
	}
}

func ParseStructuredAppRawBlocks(content string) []StructuredAppRawBlock {
	type rawBlock struct {
		start int
		end   int
	}
	blocks := []rawBlock{}
	offset := 0
	currentStart := -1
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := stripComment(strings.TrimSpace(line))
		if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
			section := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "[["), "]]")))
			if section == "manual.apps" {
				if currentStart >= 0 {
					blocks = append(blocks, rawBlock{start: currentStart, end: offset})
				}
				currentStart = offset
			}
		}
		offset += len(line)
	}
	if currentStart >= 0 {
		blocks = append(blocks, rawBlock{start: currentStart, end: len(content)})
	}
	out := make([]StructuredAppRawBlock, 0, len(blocks))
	for _, block := range blocks {
		text := content[block.start:block.end]
		apps := ParseStructuredApps(text)
		if len(apps) != 1 || strings.TrimSpace(apps[0].Name) == "" {
			continue
		}
		out = append(out, StructuredAppRawBlock{App: apps[0], Start: block.start, End: block.end, Text: text})
	}
	return out
}

func SelectStructuredDraftBlock(blocks []StructuredAppRawBlock, query string) (StructuredAppRawBlock, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := []StructuredAppRawBlock{}
	for _, block := range blocks {
		if StructuredAppAccepted(block.App) {
			continue
		}
		if query == "" || StructuredAppMatchesQuery(block.App, query) {
			matches = append(matches, block)
		}
	}
	switch len(matches) {
	case 0:
		if query == "" {
			return StructuredAppRawBlock{}, fmt.Errorf("no manual structured draft entries")
		}
		return StructuredAppRawBlock{}, fmt.Errorf("no manual structured draft entries match %q", query)
	case 1:
		return matches[0], nil
	default:
		return StructuredAppRawBlock{}, fmt.Errorf("manual draft action requires a query matching exactly one draft; matched %d", len(matches))
	}
}

func ReplaceStructuredBlock(content string, block StructuredAppRawBlock, replacement string) string {
	replacement = strings.TrimSpace(replacement)
	prefix := content[:block.Start]
	suffix := content[block.End:]
	if replacement != "" {
		replacement += "\n"
		if strings.TrimSpace(prefix) != "" && !strings.HasSuffix(prefix, "\n\n") {
			replacement = "\n" + replacement
		}
	}
	return strings.TrimRight(prefix+replacement+strings.TrimLeft(suffix, "\n"), "\n") + "\n"
}

func AppendStructuredDrafts(path string, drafts []StructuredApp) error {
	blocks := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		blocks = append(blocks, RenderStructuredDraftBlock(draft))
	}
	return appendStructuredContent(path, strings.Join(blocks, "\n"))
}

func appendStructuredContent(path string, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("manual structured content is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var builder strings.Builder
	if len(existing) > 0 {
		builder.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(strings.TrimRight(content, "\n"))
	builder.WriteString("\n")
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func RenderStructuredDraftBlock(app StructuredApp) string {
	app.ReviewStatus = "draft"
	if app.Provenance == nil {
		app.Provenance = map[string]string{}
	}
	app.Provenance["source"] = "agent"
	return RenderStructuredAppBlock(app)
}

func RenderStructuredAppBlock(app StructuredApp) string {
	var builder strings.Builder
	builder.WriteString("[[manual.apps]]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlString(app.Name))
	builder.WriteString("\n")
	if len(app.Aliases) > 0 {
		builder.WriteString("aliases = ")
		builder.WriteString(tomlStringArray(app.Aliases))
		builder.WriteString("\n")
	}
	if app.Category != "" {
		builder.WriteString("category = ")
		builder.WriteString(tomlString(app.Category))
		builder.WriteString("\n")
	}
	if app.ManagedBy != "" {
		builder.WriteString("managed_by = ")
		builder.WriteString(tomlString(app.ManagedBy))
		builder.WriteString("\n")
	}
	if app.Lifecycle != "" {
		builder.WriteString("lifecycle = ")
		builder.WriteString(tomlString(app.Lifecycle))
		builder.WriteString("\n")
	}
	if app.Detail != "" {
		builder.WriteString("description = ")
		builder.WriteString(tomlString(app.Detail))
		builder.WriteString("\n")
	}
	if app.Confidence != "" {
		builder.WriteString("confidence = ")
		builder.WriteString(tomlString(app.Confidence))
		builder.WriteString("\n")
	}
	builder.WriteString("review_status = ")
	builder.WriteString(tomlString(firstNonEmpty(app.ReviewStatus, "draft")))
	builder.WriteString("\n")
	if len(app.Identifiers) > 0 {
		builder.WriteString("\n[manual.apps.identifiers]\n")
		for _, key := range []string{"bundle_id", "mas_id", "cask", "path"} {
			if value := app.Identifiers[key]; value != "" {
				builder.WriteString(key)
				builder.WriteString(" = ")
				builder.WriteString(tomlString(value))
				builder.WriteString("\n")
			}
		}
	}
	if len(app.Provenance) > 0 || len(app.Evidence) > 0 {
		builder.WriteString("\n[manual.apps.provenance]\n")
		if source := firstNonEmpty(app.Provenance["source"], "agent"); source != "" {
			builder.WriteString("source = ")
			builder.WriteString(tomlString(source))
			builder.WriteString("\n")
		}
		if command := app.Provenance["command"]; command != "" {
			builder.WriteString("command = ")
			builder.WriteString(tomlString(command))
			builder.WriteString("\n")
		}
		for _, key := range StructuredProvenanceDetailKeys() {
			if value := app.Provenance[key]; value != "" {
				builder.WriteString(key)
				builder.WriteString(" = ")
				builder.WriteString(tomlString(value))
				builder.WriteString("\n")
			}
		}
		if reviewedAt := app.Provenance["reviewed_at"]; reviewedAt != "" {
			builder.WriteString("reviewed_at = ")
			builder.WriteString(tomlString(reviewedAt))
			builder.WriteString("\n")
		}
		if len(app.Evidence) > 0 {
			builder.WriteString("evidence = ")
			builder.WriteString(tomlStringArray(app.Evidence))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func collapseMultilineArrays(data string) string {
	lines := strings.Split(data, "\n")
	out := []string{}
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if strings.Contains(line, "[") && !strings.Contains(line, "]") {
			parts := []string{line}
			for index+1 < len(lines) {
				index++
				parts = append(parts, strings.TrimSpace(lines[index]))
				if strings.Contains(lines[index], "]") {
					break
				}
			}
			out = append(out, strings.Join(parts, " "))
			continue
		}
		out = append(out, lines[index])
	}
	return strings.Join(out, "\n")
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	out := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.Trim(strings.TrimSpace(part), `"'`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		quoted = append(quoted, tomlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func tomlString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return "\"" + value + "\""
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false
	for index, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(line[:index])
			}
		}
	}
	return line
}
