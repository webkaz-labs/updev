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
