package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/support"
	"github.com/webkaz-labs/updev/internal/textui"
)

const supportReportSchemaVersion = 1

type supportOptions struct {
	format  string
	surface string
	label   string
}

type supportReport struct {
	SchemaVersion int             `json:"schema_version"`
	Status        plan.Status     `json:"status"`
	Tool          string          `json:"tool"`
	Version       string          `json:"version"`
	Entries       []support.Entry `json:"entries"`
	Summary       map[string]int  `json:"summary"`
}

func parseSupportOptions(args []string) (supportOptions, error) {
	opts := supportOptions{format: "text", surface: "all"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--surface":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--surface requires a value")
			}
			opts.surface = args[i+1]
			i++
		case "--label":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--label requires a value")
			}
			opts.label = args[i+1]
			i++
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if !support.ValidSurface(opts.surface) {
		return opts, fmt.Errorf("unsupported support surface: %s", opts.surface)
	}
	if !support.ValidLabel(opts.label) {
		return opts, fmt.Errorf("unsupported support label: %s", opts.label)
	}
	return opts, nil
}

func runSupport(opts supportOptions) int {
	report := buildSupportReport(opts)
	if opts.format == "json" {
		return encodeJSON(report)
	}
	printSupportText(os.Stdout, report, textui.ColorEnabled())
	return 0
}

func buildSupportReport(opts supportOptions) supportReport {
	entries := support.Filter(support.Catalog(), opts.surface, opts.label)
	return supportReport{
		SchemaVersion: supportReportSchemaVersion,
		Status:        plan.StatusOK,
		Tool:          toolName,
		Version:       toolVersion,
		Entries:       entries,
		Summary:       supportSummary(entries),
	}
}

func supportSummary(entries []support.Entry) map[string]int {
	summary := map[string]int{}
	for _, entry := range entries {
		summary[entry.Label]++
	}
	return summary
}

func printSupportText(w io.Writer, report supportReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev support", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("version:", color), report.Version)
	labels := []string{support.LabelSupportedPreview, support.LabelExperimental, support.LabelCompatibility, support.LabelDeferred}
	parts := []string{}
	for _, label := range labels {
		if count := report.Summary[label]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", label, count))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("summary:", color), textui.StyleCount(strings.Join(parts, ", "), color))
	}
	bySurface := map[string][]support.Entry{}
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
