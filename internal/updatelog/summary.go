package updatelog

import (
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/textui"
)

const (
	maxSummaryItems       = 12
	maxCappedSummaryText  = 160
	maxSkippedSummaryText = 220
)

type Summary struct {
	Updated []string
	Skipped []string
}

func Summarize(stdout string, stderr string) Summary {
	summary := Summary{}
	for _, raw := range strings.Split(strings.Join([]string{stdout, stderr}, "\n"), "\n") {
		line := normalizeLogLine(raw)
		if line == "" || IsProgressLine(line) {
			continue
		}
		if isSkippedLine(line) {
			if IsGenericSkippedLine(line) {
				continue
			}
			summary.Skipped = appendCappedUniqueSummary(summary.Skipped, NormalizeSkippedItem(line))
			continue
		}
		if isUpdatedLine(line) {
			summary.Updated = appendCappedUniqueUpdatedSummary(summary.Updated, line)
		}
	}
	return summary
}

func AppendUniqueUpdated(values []string, more ...string) []string {
	for _, value := range more {
		values = appendCappedUniqueUpdatedSummary(values, value)
	}
	return values
}

func AppendUniqueSkipped(values []string, more ...string) []string {
	for _, value := range more {
		values = appendCappedUniqueSummary(values, value)
	}
	return values
}

func NormalizeUpdatedItem(value string) string {
	value = textui.Truncate(oneLine(value), maxCappedSummaryText)
	if before, after, ok := strings.Cut(value, " -> "); ok {
		after = strings.TrimSpace(after)
		if left, _, ok := strings.Cut(after, " ("); ok {
			after = strings.TrimSpace(left)
		}
		return strings.TrimSpace(before) + " -> " + after
	}
	return value
}

func NormalizeSkippedItem(value string) string {
	value = textui.Truncate(oneLine(value), maxSkippedSummaryText)
	if name, detail, ok := parseHomebrewSkippingWarning(value); ok {
		return name + " skipped: " + detail
	}
	return value
}

func UpdatedItemParts(value string) (string, string) {
	value = oneLine(strings.TrimSpace(value))
	if value == "" {
		return "", ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "updated ") && (strings.Contains(lower, " tap ") || strings.Contains(lower, " taps ")) {
		return "Homebrew taps", value
	}
	if strings.HasPrefix(lower, "updated homebrew") {
		return "Homebrew", value
	}
	if before, after, ok := strings.Cut(value, " -> "); ok {
		fields := strings.Fields(before)
		if len(fields) == 1 {
			return strings.TrimSpace(before), strings.TrimSpace(after)
		}
		if len(fields) >= 2 {
			name := strings.Join(fields[:len(fields)-1], " ")
			from := fields[len(fields)-1]
			return name, from + " -> " + strings.TrimSpace(after)
		}
	}
	return value, ""
}

func IsProgressLine(line string) bool {
	lower := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(line), "."))
	for _, needle := range []string{
		"adjust how often this is run with",
		"homebrew_auto_update_secs",
		"homebrew_no_auto_update",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	for _, progress := range []string{
		"auto-updating homebrew",
		"updating homebrew",
		"updating homebrew bundle",
		"updating homebrew/bundle",
	} {
		if lower == progress {
			return true
		}
	}
	if strings.HasPrefix(lower, "upgrading ") {
		return true
	}
	if strings.HasPrefix(lower, "upgraded ") && strings.Contains(lower, " outdated package") {
		return true
	}
	return false
}

func IsGenericSkippedLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, generic := range []string{
		"already up-to-date.",
		"already up-to-date",
		"nothing to do",
	} {
		if lower == generic {
			return true
		}
	}
	return false
}

func normalizeLogLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "==>")
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "🍺")
	line = strings.TrimSpace(line)
	return line
}

func isUpdatedLine(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(line, " -> ") {
		return lineHasPackageVersionChange(line)
	}
	for _, prefix := range []string{"upgraded ", "installing ", "installed ", "updated ", "pruning ", "pruned "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func lineHasPackageVersionChange(line string) bool {
	before, after, ok := strings.Cut(line, " -> ")
	if !ok || strings.TrimSpace(after) == "" {
		return false
	}
	fields := strings.Fields(before)
	if len(fields) < 2 {
		return false
	}
	return !tokenLooksVersion(fields[0])
}

func tokenLooksVersion(token string) bool {
	var hasDigit, hasLetter bool
	for _, r := range token {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	return hasDigit && !hasLetter
}

func isSkippedLine(line string) bool {
	lower := strings.ToLower(line)
	for _, needle := range []string{
		"already up-to-date",
		"already installed",
		"nothing to do",
		"no outdated",
		"not upgrading",
		"skipping",
		"skipped",
		"kept current",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func appendCappedUniqueSummary(values []string, value string) []string {
	if len(values) >= maxSummaryItems {
		return values
	}
	value = textui.Truncate(oneLine(value), maxCappedSummaryText)
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendCappedUniqueUpdatedSummary(values []string, value string) []string {
	value = NormalizeUpdatedItem(value)
	if value == "" || len(values) >= maxSummaryItems {
		return values
	}
	key := updatedItemKey(value)
	for index, existing := range values {
		if existing == value || (key != "" && updatedItemKey(existing) == key) {
			if len(value) < len(existing) {
				values[index] = value
			}
			return values
		}
	}
	return append(values, value)
}

func parseHomebrewSkippingWarning(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"warning: skipping ", "skipping "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(prefix):])
		if rest == "" {
			return "", "", false
		}
		name := rest
		detail := trimmed
		if before, after, ok := strings.Cut(rest, " because "); ok {
			name = strings.TrimSpace(before)
			detail = "because " + strings.TrimSpace(after)
		}
		name = strings.Trim(name, "`\"'")
		if name == "" {
			return "", "", false
		}
		return name, detail, true
	}
	return "", "", false
}

func updatedItemKey(value string) string {
	name, detail := UpdatedItemParts(value)
	if name == "" || detail == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(name))
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
