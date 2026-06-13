package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
)

type inventoryScanOptions struct {
	format   string
	provider string
	root     string
}

type inventoryScanReport struct {
	SchemaVersion    int                     `json:"schema_version"`
	Status           plan.Status             `json:"status"`
	Root             string                  `json:"root"`
	Provider         string                  `json:"provider"`
	Summary          plan.ProviderSummary    `json:"summary"`
	Sections         []toolSection           `json:"sections,omitempty"`
	ReviewCandidates []manualReviewCandidate `json:"review_candidates,omitempty"`
}

func parseInventoryScanOptions(args []string) (inventoryScanOptions, error) {
	opts := inventoryScanOptions{format: "text", provider: manualProviderName, root: defaultRoot()}
	fs := flag.NewFlagSet("inventory scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.provider, "provider", opts.provider, "inventory provider to scan")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.provider == "" {
		return opts, fmt.Errorf("--provider requires a value")
	}
	if !strings.EqualFold(opts.provider, manualProviderName) {
		return opts, fmt.Errorf("unsupported inventory scan provider: %s", opts.provider)
	}
	opts.provider = manualProviderName
	return opts, nil
}

func runInventoryScan(opts inventoryScanOptions) int {
	report := buildInventoryScanReport(opts)
	if opts.format == "json" {
		code := encodeJSON(report)
		if code != 0 {
			return code
		}
		return inventoryScanExitCode(report)
	}
	printInventoryScanText(report)
	return inventoryScanExitCode(report)
}

func buildInventoryScanReport(opts inventoryScanOptions) inventoryScanReport {
	sections := manualAppSectionsForInventoryCommand(opts.root)
	candidates := manualReviewCandidates(sections)
	status := plan.StatusOK
	if len(candidates) > 0 {
		status = plan.StatusDrift
	}
	return inventoryScanReport{
		SchemaVersion:    1,
		Status:           status,
		Root:             opts.root,
		Provider:         manualProviderName,
		Summary:          manualProviderSummary(sections),
		Sections:         sections,
		ReviewCandidates: candidates,
	}
}

func inventoryScanExitCode(report inventoryScanReport) int {
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func printInventoryScanText(report inventoryScanReport) {
	fmt.Printf("inventory scan: %s\n", report.Status)
	fmt.Printf("provider: %s\n", report.Provider)
	fmt.Printf("summary: desired=%d live=%d missing=%d extra=%d\n", report.Summary.Desired, report.Summary.Live, report.Summary.Missing, report.Summary.Extra)
	fmt.Printf("review candidates: %d\n", len(report.ReviewCandidates))
	for _, section := range report.Sections {
		fmt.Println()
		fmt.Println(section.Title)
		for _, row := range section.Rows {
			version := row.Version
			if version == "" {
				version = "-"
			}
			fmt.Printf("- %s [%s] %s\n", row.Name, row.State, version)
			if row.Detail != "" {
				fmt.Printf("  %s\n", row.Detail)
			}
		}
	}
}

type inventoryRenderOptions struct {
	format string
	root   string
	report string
}

type inventoryRenderReport struct {
	SchemaVersion    int                     `json:"schema_version"`
	Status           plan.Status             `json:"status"`
	Root             string                  `json:"root"`
	Report           string                  `json:"report"`
	Path             string                  `json:"path"`
	Content          string                  `json:"content"`
	ReviewCandidates []manualReviewCandidate `json:"review_candidates,omitempty"`
}

func parseInventoryRenderOptions(args []string) (inventoryRenderOptions, error) {
	opts := inventoryRenderOptions{format: "text", root: defaultRoot(), report: "manual-apps"}
	fs := flag.NewFlagSet("inventory render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	fs.StringVar(&opts.report, "report", opts.report, "configured inventory report name")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.report == "" {
		return opts, fmt.Errorf("--report requires a value")
	}
	if opts.report != "manual-apps" {
		return opts, fmt.Errorf("unsupported inventory report: %s", opts.report)
	}
	return opts, nil
}

func runInventoryRender(opts inventoryRenderOptions) int {
	report := buildInventoryRenderReport(opts)
	if opts.format == "json" {
		return encodeJSON(report)
	}
	fmt.Print(report.Content)
	return 0
}

func buildInventoryRenderReport(opts inventoryRenderOptions) inventoryRenderReport {
	sections := manualAppSectionsForInventoryCommand(opts.root)
	path := configuredInventoryReportPath(opts.root, opts.report)
	content := renderManualAppMarkdown(opts.report, path, sections)
	return inventoryRenderReport{
		SchemaVersion:    1,
		Status:           plan.StatusOK,
		Root:             opts.root,
		Report:           opts.report,
		Path:             path,
		Content:          content,
		ReviewCandidates: manualReviewCandidates(sections),
	}
}

func configuredInventoryReportPath(root string, name string) string {
	config := loadUpdevConfig()
	for _, report := range config.Inventory.Reports {
		if report.Name == name && report.Path != "" {
			return resolveUpdevConfigPath(root, report.Path)
		}
	}
	if name == "manual-apps" {
		return resolveUpdevConfigPath(root, "docs/apps.md")
	}
	return ""
}

func renderManualAppMarkdown(reportName string, path string, sections []toolSection) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(reportName)
	builder.WriteString("\n\n")
	builder.WriteString("> Generated preview by `updev inventory render`. Do not treat this output as applied desired state until an explicit apply/write flow exists.\n")
	if path != "" {
		builder.WriteString("> Target: `")
		builder.WriteString(path)
		builder.WriteString("`\n")
	}
	builder.WriteString("\n")
	for _, section := range sections {
		builder.WriteString("## ")
		builder.WriteString(strings.TrimPrefix(section.Title, "manual / "))
		builder.WriteString("\n\n")
		if len(section.Rows) == 0 {
			builder.WriteString("_No rows._\n\n")
			continue
		}
		builder.WriteString("| App | State | Version | Detail |\n")
		builder.WriteString("|-----|-------|---------|--------|\n")
		for _, row := range section.Rows {
			builder.WriteString("| ")
			builder.WriteString(markdownCell(row.Name))
			builder.WriteString(" | ")
			builder.WriteString(markdownCell(row.State))
			builder.WriteString(" | ")
			builder.WriteString(markdownCell(row.Version))
			builder.WriteString(" | ")
			builder.WriteString(markdownCell(row.Detail))
			builder.WriteString(" |\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
}
