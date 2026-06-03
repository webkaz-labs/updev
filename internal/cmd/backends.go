package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

const backendPlanReportSchemaVersion = 1

type backendOptions struct {
	command string
	format  string
	root    string
}

type backendPlanReport struct {
	SchemaVersion int              `json:"schema_version"`
	Status        plan.Status      `json:"status"`
	Command       string           `json:"command"`
	Root          string           `json:"root"`
	Findings      []backendFinding `json:"findings,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
}

type backendFinding struct {
	Type                string      `json:"type"`
	Status              plan.Status `json:"status"`
	Provider            string      `json:"provider"`
	Kind                string      `json:"kind"`
	Name                string      `json:"name"`
	Current             string      `json:"current,omitempty"`
	RecommendedProvider string      `json:"recommended_provider,omitempty"`
	RecommendedName     string      `json:"recommended_name,omitempty"`
	RecommendedTier     string      `json:"recommended_tier,omitempty"`
	PreferenceRank      int         `json:"preference_rank,omitempty"`
	CommandNames        []string    `json:"command_names,omitempty"`
	CommandStatus       string      `json:"command_status,omitempty"`
	CurrentSpec         string      `json:"current_spec,omitempty"`
	RecommendedSpec     string      `json:"recommended_spec,omitempty"`
	CurrentOS           []string    `json:"current_os,omitempty"`
	RecommendedOS       []string    `json:"recommended_os,omitempty"`
	Confidence          string      `json:"confidence"`
	Reason              string      `json:"reason"`
	Action              string      `json:"action"`
}

type backendRecommendation struct {
	Provider       string
	Name           string
	Tier           string
	PreferenceRank int
	Reason         string
	Commands       []string
}

type backendPreferenceRule struct {
	SourceProvider      string
	SourceName          string
	RecommendedProvider string
	RecommendedName     string
	Commands            []string
	Reason              string
}

type backendPreferenceTier struct {
	Rank     int
	Provider string
	Backend  string
	Label    string
	Reason   string
}

func parseBackendOptions(command string, args []string) (backendOptions, error) {
	opts := backendOptions{command: command, format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
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
	return opts, nil
}

func runBackends(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: updev backends <doctor|plan> [--format text|json]")
		return usageExitCode
	}
	command := args[0]
	if command != "doctor" && command != "plan" {
		fmt.Fprintf(os.Stderr, "unsupported backends command: %s\n", command)
		return usageExitCode
	}
	opts, err := parseBackendOptions(command, args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	report := buildBackendPlanReportWithRunner(context.Background(), opts, runner.Local{})
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printBackendPlanText(os.Stdout, report, textui.ColorEnabled())
	}
	return updateExitCode(report.Status)
}

func buildBackendPlanReport(ctx context.Context, opts backendOptions) backendPlanReport {
	return buildBackendPlanReportWithRunner(ctx, opts, runner.Local{})
}

func buildBackendPlanReportWithRunner(ctx context.Context, opts backendOptions, commandRunner runner.Runner) backendPlanReport {
	brewItems, brewWarnings := desiredBrewItems(ctx, opts.root)
	miseTools, miseWarnings := mise.DesiredTools(opts.root)
	warnings := append([]string{}, brewWarnings...)
	if miseWarnings != nil {
		warnings = append(warnings, "mise desired tools unavailable: "+miseWarnings.Error())
		miseTools = map[string]string{}
	}
	findings := backendFindings(brewItems, miseTools, commandRunner)
	status := plan.StatusOK
	if len(findings) > 0 {
		status = plan.StatusDrift
	}
	if opts.command == "doctor" {
		findings = filterBackendDoctorFindings(findings)
		if len(findings) == 0 {
			status = plan.StatusOK
		}
	}
	return backendPlanReport{
		SchemaVersion: backendPlanReportSchemaVersion,
		Status:        status,
		Command:       opts.command,
		Root:          opts.root,
		Findings:      findings,
		Warnings:      warnings,
	}
}

func desiredBrewItems(ctx context.Context, root string) ([]plan.Item, []string) {
	_ = ctx
	items, err := brew.DesiredFromPath(brewfile.SourcePath(root))
	if err != nil {
		return nil, []string{err.Error()}
	}
	return items, nil
}

func backendFindings(brewItems []plan.Item, miseTools map[string]string, commandRunner runner.Runner) []backendFinding {
	findings := []backendFinding{}
	for _, item := range brewItems {
		if item.Kind != "brew" {
			continue
		}
		if recommendation, ok := homebrewBackendRecommendation(item.Name); ok {
			recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
			findings = append(findings, backendFinding{
				Type:                "homebrew-to-mise",
				Status:              plan.StatusHeld,
				Provider:            "brew",
				Kind:                item.Kind,
				Name:                item.Name,
				Current:             "brew:" + item.Name,
				RecommendedProvider: recommendation.Provider,
				RecommendedName:     recommendation.Name,
				RecommendedTier:     recommendation.Tier,
				PreferenceRank:      recommendation.PreferenceRank,
				CommandNames:        recommendation.Commands,
				CommandStatus:       backendCommandStatus(commandRunner, recommendation.Commands),
				RecommendedSpec:     recommendedSpec,
				RecommendedOS:       backendOSConditions(recommendedSpec),
				Confidence:          homebrewRecommendationConfidence(alreadyDesired),
				Reason:              recommendation.Reason,
				Action:              homebrewRecommendationAction(item, recommendation, alreadyDesired),
			})
		}
	}
	for name, spec := range miseTools {
		if recommendation, ok := miseBackendRecommendation(name); ok {
			recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
			findings = append(findings, backendFinding{
				Type:                "mise-backend-rewrite",
				Status:              plan.StatusHeld,
				Provider:            "mise",
				Kind:                "tool",
				Name:                name,
				Current:             name,
				RecommendedProvider: recommendation.Provider,
				RecommendedName:     recommendation.Name,
				RecommendedTier:     recommendation.Tier,
				PreferenceRank:      recommendation.PreferenceRank,
				CommandNames:        recommendation.Commands,
				CommandStatus:       backendCommandStatus(commandRunner, recommendation.Commands),
				CurrentSpec:         spec,
				RecommendedSpec:     recommendedSpec,
				CurrentOS:           backendOSConditions(spec),
				RecommendedOS:       backendOSConditions(recommendedSpec),
				Confidence:          miseRecommendationConfidence(spec, alreadyDesired),
				Reason:              recommendation.Reason,
				Action:              miseRecommendationAction(name, spec, recommendation, alreadyDesired),
			})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
		}
		return findings[i].Name < findings[j].Name
	})
	return findings
}

func filterBackendDoctorFindings(findings []backendFinding) []backendFinding {
	out := []backendFinding{}
	for _, finding := range findings {
		if finding.Type == "mise-backend-rewrite" {
			out = append(out, finding)
		}
	}
	return out
}

func homebrewBackendRecommendation(name string) (backendRecommendation, bool) {
	return backendPreferenceRecommendation("brew", name)
}

func miseBackendRecommendation(name string) (backendRecommendation, bool) {
	return backendPreferenceRecommendation("mise", name)
}

func backendPreferenceRecommendation(sourceProvider string, sourceName string) (backendRecommendation, bool) {
	for _, rule := range backendPreferencePolicy() {
		if rule.SourceProvider == sourceProvider && rule.SourceName == sourceName {
			tier := backendPreferenceTierFor(rule.RecommendedProvider, rule.RecommendedName)
			return backendRecommendation{
				Provider:       rule.RecommendedProvider,
				Name:           rule.RecommendedName,
				Tier:           tier.Label,
				PreferenceRank: tier.Rank,
				Commands:       rule.Commands,
				Reason:         rule.Reason,
			}, true
		}
	}
	return backendRecommendation{}, false
}

func backendPreferenceTierFor(provider string, name string) backendPreferenceTier {
	for _, tier := range backendPreferenceTiers() {
		if tier.Provider != provider {
			continue
		}
		if tier.Backend == "" && !strings.Contains(name, ":") {
			return tier
		}
		if tier.Backend != "" && (strings.HasPrefix(name, tier.Backend+":") || name == tier.Backend) {
			return tier
		}
	}
	return backendPreferenceTier{Rank: 99, Provider: provider, Label: provider + "/other", Reason: "provider has no explicit preference tier yet"}
}

func backendPreferenceTiers() []backendPreferenceTier {
	return []backendPreferenceTier{
		{Rank: 1, Provider: "mise", Backend: "", Label: "mise/core", Reason: "prefer mise core backends for reliable CLI developer tools"},
		{Rank: 2, Provider: "mise", Backend: "aqua", Label: "mise/aqua", Reason: "prefer registry-backed mise external backends over language global package managers"},
		{Rank: 3, Provider: "mas", Backend: "", Label: "store/native", Reason: "use store ownership when app-store evidence is stronger than tool-manager ownership"},
		{Rank: 4, Provider: "brew", Backend: "", Label: "package-manager/native", Reason: "use native package managers for GUI integration, bootstrap, or platform packaging"},
		{Rank: 5, Provider: "vendor", Backend: "", Label: "vendor/manual", Reason: "keep vendor/manual ownership for proprietary installers or weak package evidence"},
	}
}

func backendPreferencePolicy() []backendPreferenceRule {
	miseCoreReason := "stable mise core tool is preferred for CLI developer tools"
	return []backendPreferenceRule{
		{SourceProvider: "brew", SourceName: "bat", RecommendedProvider: "mise", RecommendedName: "bat", Commands: []string{"bat"}, Reason: miseCoreReason},
		{SourceProvider: "brew", SourceName: "eza", RecommendedProvider: "mise", RecommendedName: "eza", Commands: []string{"eza"}, Reason: miseCoreReason},
		{SourceProvider: "brew", SourceName: "fd", RecommendedProvider: "mise", RecommendedName: "aqua:sharkdp/fd", Commands: []string{"fd"}, Reason: "fd has a registry-backed mise/aqua path"},
		{SourceProvider: "brew", SourceName: "fzf", RecommendedProvider: "mise", RecommendedName: "fzf", Commands: []string{"fzf"}, Reason: miseCoreReason},
		{SourceProvider: "brew", SourceName: "ripgrep", RecommendedProvider: "mise", RecommendedName: "ripgrep", Commands: []string{"rg"}, Reason: "ripgrep is already a stable mise-managed CLI"},
		{SourceProvider: "brew", SourceName: "shellcheck", RecommendedProvider: "mise", RecommendedName: "shellcheck", Commands: []string{"shellcheck"}, Reason: miseCoreReason},
		{SourceProvider: "brew", SourceName: "starship", RecommendedProvider: "mise", RecommendedName: "starship", Commands: []string{"starship"}, Reason: miseCoreReason},
		{SourceProvider: "brew", SourceName: "zoxide", RecommendedProvider: "mise", RecommendedName: "zoxide", Commands: []string{"zoxide"}, Reason: miseCoreReason},
		{SourceProvider: "mise", SourceName: "cargo:fd-find", RecommendedProvider: "mise", RecommendedName: "aqua:sharkdp/fd", Commands: []string{"fd"}, Reason: "aqua prebuilt CLI is preferred over a cargo global build"},
		{SourceProvider: "mise", SourceName: "cargo:git-delta", RecommendedProvider: "mise", RecommendedName: "aqua:dandavison/delta", Commands: []string{"delta"}, Reason: "aqua prebuilt CLI is preferred over a cargo global build"},
		{SourceProvider: "mise", SourceName: "cargo:sheldon", RecommendedProvider: "mise", RecommendedName: "aqua:rossmacarthur/sheldon", Commands: []string{"sheldon"}, Reason: "aqua prebuilt CLI is preferred over a cargo global build"},
		{SourceProvider: "mise", SourceName: "npm:pnpm", RecommendedProvider: "mise", RecommendedName: "aqua:pnpm/pnpm", Commands: []string{"pnpm"}, Reason: "aqua prebuilt CLI avoids npm global package-manager coupling"},
	}
}

func homebrewRecommendationConfidence(alreadyDesired bool) string {
	if alreadyDesired {
		return "high"
	}
	return "medium"
}

func miseRecommendationConfidence(spec string, alreadyDesired bool) string {
	if alreadyDesired && backendSpecHasOSConditions(spec) {
		return "high"
	}
	if alreadyDesired {
		return "medium"
	}
	return "low"
}

func homebrewRecommendationAction(item plan.Item, recommendation backendRecommendation, alreadyDesired bool) string {
	if alreadyDesired {
		return fmt.Sprintf("review why %s remains in Brewfile before removing; keep it if bootstrap or cask dependency requires Homebrew", item.Name)
	}
	return fmt.Sprintf("review adding %s to mise before considering Brewfile removal", recommendation.Name)
}

func miseRecommendationAction(name string, spec string, recommendation backendRecommendation, alreadyDesired bool) string {
	if alreadyDesired {
		return fmt.Sprintf("preferred entry %s already exists; preserve OS conditions before removing or narrowing %s", recommendation.Name, name)
	}
	if backendSpecHasOSConditions(spec) {
		return fmt.Sprintf("review OS-specific conditions before replacing %s with %s; copy current os list to the preferred entry", name, recommendation.Name)
	}
	return fmt.Sprintf("review replacing %s with %s", name, recommendation.Name)
}

func backendCommandStatus(commandRunner runner.Runner, commands []string) string {
	if len(commands) == 0 {
		return "unknown"
	}
	missing := 0
	for _, command := range commands {
		if _, err := commandRunner.LookPath(command); err != nil {
			missing++
		}
	}
	switch missing {
	case 0:
		return "on-path"
	case len(commands):
		return "missing"
	default:
		return "partial"
	}
}

func backendOSConditions(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	keyIndex := strings.Index(spec, "os")
	if keyIndex < 0 {
		return nil
	}
	assignment := spec[keyIndex:]
	start := strings.Index(assignment, "[")
	if start < 0 {
		return nil
	}
	end := strings.Index(assignment[start:], "]")
	if end < 0 {
		return nil
	}
	rawList := assignment[start+1 : start+end]
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(rawList, ",") {
		token := strings.Trim(strings.TrimSpace(raw), `"'`)
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func backendSpecHasOSConditions(spec string) bool {
	return len(backendOSConditions(spec)) > 0
}

func printBackendPlanText(w io.Writer, report backendPlanReport, color bool) {
	title := "updev backends " + report.Command
	fmt.Fprintf(w, "%s %s\n", textui.Style(title, "\033[1m", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "root: %s\n", report.Root)
	fmt.Fprintf(w, "findings: %d\n", len(report.Findings))
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "\nwarnings")
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "\nno backend convergence findings")
		return
	}
	printBackendPreferenceOrder(w)
	rows := make([][]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		action := finding.Action
		if len(finding.CurrentOS) > 0 {
			action += "; os=" + strings.Join(finding.CurrentOS, ",")
		}
		rows = append(rows, []string{
			finding.Type,
			finding.Name,
			finding.RecommendedName,
			finding.Confidence + "/" + finding.CommandStatus,
			action,
		})
	}
	fmt.Fprintln(w, "\nfindings")
	textui.PrintTable(w, []textui.Column{
		{Header: "type", Min: 18, Max: 24},
		{Header: "current", Min: 18, Max: 30},
		{Header: "recommended", Min: 18, Max: 30},
		{Header: "confidence", Min: 10, Max: 18},
		{Header: "action", Min: 24, Max: 56},
	}, rows, color)
	fmt.Fprintln(w, "\nnext")
	fmt.Fprintln(w, "  review findings manually; v1 stretch is read-only and does not migrate manifests")
}

func printBackendPreferenceOrder(w io.Writer) {
	parts := make([]string, 0, len(backendPreferenceTiers()))
	for _, tier := range backendPreferenceTiers() {
		parts = append(parts, fmt.Sprintf("%d:%s", tier.Rank, tier.Label))
	}
	fmt.Fprintln(w, "\npreference order")
	fmt.Fprintf(w, "  %s\n", strings.Join(parts, " > "))
}

func backendDetailRows(report backendPlanReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Findings))
	for _, finding := range report.Findings {
		metadata := []string{
			"type: " + finding.Type,
			"provider: " + finding.Provider,
			"kind: " + finding.Kind,
			"current: " + finding.Current,
			"recommended: " + finding.RecommendedProvider + "/" + finding.RecommendedName,
			fmt.Sprintf("preference: rank %d %s", finding.PreferenceRank, finding.RecommendedTier),
			"confidence: " + finding.Confidence,
			"command status: " + finding.CommandStatus,
		}
		if len(finding.CommandNames) > 0 {
			metadata = append(metadata, "commands: "+strings.Join(finding.CommandNames, ", "))
		}
		if finding.CurrentSpec != "" {
			metadata = append(metadata, "current spec: "+finding.CurrentSpec)
		}
		if finding.RecommendedSpec != "" {
			metadata = append(metadata, "recommended spec: "+finding.RecommendedSpec)
		}
		if len(finding.CurrentOS) > 0 {
			metadata = append(metadata, "current os: "+strings.Join(finding.CurrentOS, ", "))
		}
		if len(finding.RecommendedOS) > 0 {
			metadata = append(metadata, "recommended os: "+strings.Join(finding.RecommendedOS, ", "))
		}
		rows = append(rows, detailBrowserRow{
			Title:    finding.Name + " -> " + finding.RecommendedName,
			Status:   string(finding.Status),
			Summary:  finding.Reason,
			Detail:   finding.Action,
			Metadata: metadata,
		})
	}
	return rows
}
