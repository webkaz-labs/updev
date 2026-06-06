package textui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiGreen   = "\033[32m"
	ansiBlue    = "\033[34m"
	ansiYellow  = "\033[33m"
	ansiRed     = "\033[31m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

type Column struct {
	Header string
	Min    int
	Max    int
}

func PrintTable(w io.Writer, columns []Column, rows [][]string, color bool) {
	widths := ColumnWidths(columns, rows)
	header := make([]string, 0, len(columns))
	for i, column := range columns {
		header = append(header, Style(PadRight(column.Header, widths[i]), ansiBold, color))
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(header, " "))
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for i := range columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			values = append(values, PadRight(Truncate(value, widths[i]), widths[i]))
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(values, " "))
	}
}

func StyleStatus(status string, color bool) string {
	switch status {
	case "ok", "allow", "active", "updated", "keep-manual":
		return Style(status, ansiGreen, color)
	case "missing", "extra", "drift", "held", "hold", "review", "unavailable", "inactive", "attention", "skipped", "deferred", "needs-review", "ignore-local", "adopt-brew", "adopt-mas", "open-vendor":
		return Style(status, ansiYellow, color)
	case "error", "blocked", "block":
		return Style(status, ansiRed, color)
	default:
		return Style(status, ansiCyan, color)
	}
}

func StyleDim(value string, color bool) string {
	return Style(value, ansiDim, color)
}

func StyleHeading(value string, color bool) string {
	return Style(value, ansiBold, color)
}

func StyleSection(value string, color bool) string {
	return Style(value, ansiBold+ansiMagenta, color)
}

func StyleKey(value string, color bool) string {
	return Style(value, ansiCyan, color)
}

func StyleAction(value string, color bool) string {
	return Style(value, ansiGreen, color)
}

func StyleLabel(value string, color bool) string {
	return Style(value, ansiCyan, color)
}

func StyleName(value string, color bool) string {
	return Style(value, ansiCyan, color)
}

func StyleVersion(value string, color bool) string {
	return Style(value, ansiGreen, color)
}

func StyleRequested(value string, color bool) string {
	return Style(value, ansiBlue, color)
}

func StyleCount(value string, color bool) string {
	return Style(value, ansiBlue, color)
}

func StyleWarning(value string, color bool) string {
	return Style(value, ansiYellow, color)
}

func StyleError(value string, color bool) string {
	return Style(value, ansiRed, color)
}

func StyleBool(value bool, color bool) string {
	if value {
		return Style("yes", ansiGreen, color)
	}
	return Style("-", ansiDim, color)
}

func Style(value string, code string, color bool) string {
	if !color {
		return value
	}
	return code + value + ansiReset
}

func ColorEnabled() bool {
	term := os.Getenv("TERM")
	return os.Getenv("NO_COLOR") == "" && term != "" && term != "dumb"
}

func PadRight(value string, width int) string {
	padding := width - DisplayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if DisplayWidth(value) <= width {
		return value
	}
	if width <= 1 {
		if hasANSI(value) {
			return "…" + ansiReset
		}
		return "…"
	}
	out := strings.Builder{}
	current := 0
	sawANSI := false
	for len(value) > 0 {
		if n := ansiSequenceLen(value); n > 0 {
			out.WriteString(value[:n])
			value = value[n:]
			sawANSI = true
			continue
		}
		r, size := utf8.DecodeRuneInString(value)
		rw := runeWidth(r)
		if current+rw > width-1 {
			break
		}
		out.WriteRune(r)
		current += rw
		value = value[size:]
	}
	out.WriteString("…")
	if sawANSI {
		out.WriteString(ansiReset)
	}
	return out.String()
}

func DisplayWidth(value string) int {
	width := 0
	for len(value) > 0 {
		if n := ansiSequenceLen(value); n > 0 {
			value = value[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(value)
		width += runeWidth(r)
		value = value[size:]
	}
	return width
}

func WrapText(value string, width int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if width <= 0 || DisplayWidth(value) <= width {
		return []string{value}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{value}
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		for _, part := range splitTextPart(word, width) {
			if current == "" {
				current = part
				continue
			}
			next := current + " " + part
			if DisplayWidth(next) <= width {
				current = next
				continue
			}
			lines = append(lines, current)
			current = part
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitTextPart(value string, width int) []string {
	if width <= 0 || DisplayWidth(value) <= width {
		return []string{value}
	}
	parts := []string{}
	current := strings.Builder{}
	currentWidth := 0
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		rw := runeWidth(r)
		if currentWidth > 0 && currentWidth+rw > width {
			parts = append(parts, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(r)
		currentWidth += rw
		value = value[size:]
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func hasANSI(value string) bool {
	for len(value) > 0 {
		if n := ansiSequenceLen(value); n > 0 {
			return true
		}
		_, size := utf8.DecodeRuneInString(value)
		value = value[size:]
	}
	return false
}

func ansiSequenceLen(value string) int {
	if len(value) < 2 || value[0] != 0x1b || value[1] != '[' {
		return 0
	}
	for i := 2; i < len(value); i++ {
		if value[i] >= 0x40 && value[i] <= 0x7e {
			return i + 1
		}
	}
	return 0
}

func runeWidth(r rune) int {
	if r == utf8.RuneError {
		return 1
	}
	if r == '…' {
		return 1
	}
	if r >= 0x1100 {
		return 2
	}
	return 1
}

func ColumnWidths(columns []Column, rows [][]string) []int {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = DisplayWidth(column.Header)
		if widths[i] < column.Min {
			widths[i] = column.Min
		}
	}
	for _, row := range rows {
		for i, value := range row {
			if i >= len(widths) {
				continue
			}
			width := DisplayWidth(value)
			if width > widths[i] {
				widths[i] = width
			}
		}
	}
	for i, column := range columns {
		if column.Max > 0 && widths[i] > column.Max {
			widths[i] = column.Max
		}
	}
	return widths
}
