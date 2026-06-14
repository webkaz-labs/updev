package support

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/textui"
)

const ReportSchemaVersion = 1

type Options struct {
	Format  string
	Surface string
	Label   string
}

type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Status        plan.Status    `json:"status"`
	Tool          string         `json:"tool"`
	Version       string         `json:"version"`
	Entries       []Entry        `json:"entries"`
	Summary       map[string]int `json:"summary"`
}

func BuildReport(tool string, version string, opts Options) Report {
	entries := Filter(Catalog(), opts.Surface, opts.Label)
	return Report{
		SchemaVersion: ReportSchemaVersion,
		Status:        plan.StatusOK,
		Tool:          tool,
		Version:       version,
		Entries:       entries,
		Summary:       Summary(entries),
	}
}

func Summary(entries []Entry) map[string]int {
	summary := map[string]int{}
	for _, entry := range entries {
		summary[entry.Label]++
	}
	return summary
}

func PrintText(w io.Writer, report Report, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev support", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("version:", color), report.Version)
	labels := []string{LabelSupportedPreview, LabelExperimental, LabelCompatibility, LabelDeferred}
	parts := []string{}
	for _, label := range labels {
		if count := report.Summary[label]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", label, count))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("summary:", color), textui.StyleCount(strings.Join(parts, ", "), color))
	}
	bySurface := map[string][]Entry{}
	surfaces := []string{}
	for _, entry := range report.Entries {
		if _, ok := bySurface[entry.Surface]; !ok {
			surfaces = append(surfaces, entry.Surface)
		}
		bySurface[entry.Surface] = append(bySurface[entry.Surface], entry)
	}
	sort.Strings(surfaces)
	for _, surface := range surfaces {
		fmt.Fprintln(w)
		fmt.Fprintln(w, textui.StyleHeading(surface, color))
		rows := [][]string{}
		for _, entry := range bySurface[surface] {
			rows = append(rows, []string{
				textui.StyleName(entry.Name, color),
				textui.StyleStatus(entry.Label, color),
				entry.Summary,
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "name", Min: 14, Max: 34},
			{Header: "label", Min: 14, Max: 19},
			{Header: "summary", Min: 24, Max: 72},
		}, rows, color)
	}
}

func ProviderLabel(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "brew":
		name = "homebrew"
	case "manual":
		name = "manual-apps"
	}
	for _, entry := range Catalog() {
		if entry.Surface == "provider" && strings.EqualFold(entry.Name, name) {
			return entry.Label
		}
	}
	return ""
}

func LabelIsDenseBadge(label string) bool {
	switch label {
	case LabelExperimental, LabelCompatibility, LabelDeferred:
		return true
	default:
		return false
	}
}
