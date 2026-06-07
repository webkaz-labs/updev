package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

const backendPlanReportSchemaVersion = 1
const backendDetailActionPrefix = "backend"

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
	CurrentPlatform     string      `json:"current_platform,omitempty"`
	ReleaseAssetStatus  string      `json:"release_asset_status,omitempty"`
	ReleaseAssetMatches []string    `json:"release_asset_matches,omitempty"`
	ReleaseAssets       []string    `json:"release_assets,omitempty"`
	RecommendationKind  string      `json:"recommendation_kind,omitempty"`
	RewriteAllowed      bool        `json:"rewrite_allowed,omitempty"`
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
	Kind           string
	AssetEvidence  backendGitHubAssetEvidence
	RewriteAllowed bool
}

type backendGitHubAssetEvidence struct {
	Platform string
	Status   string
	Matches  []string
	Assets   []string
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
	homebrewGitHubRepos := homebrewFormulaGitHubRepos(genericHomebrewRecommendationNames(brewItems), commandRunner)
	resultCh := make(chan backendFinding)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	run := func(fn func() (backendFinding, bool)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if finding, ok := fn(); ok {
				resultCh <- finding
			}
		}()
	}
	for _, item := range brewItems {
		item := item
		if item.Kind != "brew" {
			continue
		}
		run(func() (backendFinding, bool) {
			recommendation, ok := homebrewBackendRecommendation(item.Name, homebrewGitHubRepos, commandRunner)
			if !ok {
				return backendFinding{}, false
			}
			return backendHomebrewFinding(item, recommendation, miseTools, commandRunner), true
		})
	}
	for name, spec := range miseTools {
		name, spec := name, spec
		run(func() (backendFinding, bool) {
			recommendation, ok := miseBackendRecommendation(name, commandRunner)
			if !ok {
				return backendFinding{}, false
			}
			return backendMiseFinding(name, spec, recommendation, miseTools, commandRunner), true
		})
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	for finding := range resultCh {
		findings = append(findings, finding)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
		}
		return findings[i].Name < findings[j].Name
	})
	return findings
}

func backendHomebrewFinding(item plan.Item, recommendation backendRecommendation, miseTools map[string]string, commandRunner runner.Runner) backendFinding {
	recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
	findingType := "homebrew-to-mise"
	if backendRecommendationKind(recommendation) == "candidate" {
		findingType = "homebrew-to-mise-candidate"
	}
	return backendFinding{
		Type:                findingType,
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
		CurrentPlatform:     recommendation.AssetEvidence.Platform,
		ReleaseAssetStatus:  recommendation.AssetEvidence.Status,
		ReleaseAssetMatches: recommendation.AssetEvidence.Matches,
		ReleaseAssets:       recommendation.AssetEvidence.Assets,
		RecommendationKind:  backendRecommendationKind(recommendation),
		Confidence:          homebrewRecommendationConfidence(recommendation, alreadyDesired),
		Reason:              recommendation.Reason,
		Action:              homebrewRecommendationAction(item, recommendation, alreadyDesired),
	}
}

func backendMiseFinding(name string, spec string, recommendation backendRecommendation, miseTools map[string]string, commandRunner runner.Runner) backendFinding {
	recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
	findingType := "mise-backend-rewrite"
	if backendRecommendationKind(recommendation) == "candidate" {
		findingType = "mise-backend-candidate"
	}
	return backendFinding{
		Type:                findingType,
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
		CurrentPlatform:     recommendation.AssetEvidence.Platform,
		ReleaseAssetStatus:  recommendation.AssetEvidence.Status,
		ReleaseAssetMatches: recommendation.AssetEvidence.Matches,
		ReleaseAssets:       recommendation.AssetEvidence.Assets,
		RecommendationKind:  backendRecommendationKind(recommendation),
		RewriteAllowed:      recommendation.RewriteAllowed,
		Confidence:          miseRecommendationConfidence(spec, recommendation, alreadyDesired),
		Reason:              recommendation.Reason,
		Action:              miseRecommendationAction(name, spec, recommendedSpec, recommendation, alreadyDesired),
	}
}

func filterBackendDoctorFindings(findings []backendFinding) []backendFinding {
	out := []backendFinding{}
	for _, finding := range findings {
		if finding.Type == "mise-backend-rewrite" || finding.Type == "mise-backend-candidate" {
			out = append(out, finding)
		}
	}
	return out
}

func homebrewBackendRecommendation(name string, githubRepos map[string]string, commandRunner runner.Runner) (backendRecommendation, bool) {
	if recommendation, ok := explicitHomebrewBackendRecommendation(name); ok {
		return recommendation, true
	}
	return homebrewGitHubBackendRecommendation(name, githubRepos, commandRunner)
}

func explicitHomebrewBackendRecommendation(name string) (backendRecommendation, bool) {
	return backendPreferenceRecommendation("brew", name)
}

func miseBackendRecommendation(name string, commandRunner runner.Runner) (backendRecommendation, bool) {
	if recommendation, ok := explicitMiseBackendRecommendation(name); ok {
		recommendation = finalizeExplicitMiseRecommendation(name, recommendation, commandRunner)
		return recommendation, true
	}
	return miseGitHubBackendRecommendation(name, commandRunner)
}

func explicitMiseBackendRecommendation(name string) (backendRecommendation, bool) {
	return backendPreferenceRecommendation("mise", name)
}

func finalizeExplicitMiseRecommendation(sourceName string, recommendation backendRecommendation, commandRunner runner.Runner) backendRecommendation {
	if !strings.HasPrefix(recommendation.Name, "github:") {
		return recommendation
	}
	sourceBackend, _, ok := strings.Cut(sourceName, ":")
	if !ok {
		return recommendation
	}
	switch sourceBackend {
	case "cargo", "npm":
	default:
		return recommendation
	}
	repo := strings.TrimPrefix(recommendation.Name, "github:")
	recommendation.AssetEvidence = githubReleaseAssetEvidence(repo, commandRunner)
	if recommendation.AssetEvidence.Status != "compatible" {
		recommendation.Kind = "candidate"
		recommendation.RewriteAllowed = false
		recommendation.Reason = strings.TrimSpace(recommendation.Reason + "; compatible GitHub release asset was not verified")
	}
	return recommendation
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
				RewriteAllowed: sourceProvider == "mise",
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
	for _, tier := range deprecatedBackendPreferenceTiers() {
		if tier.Provider != provider {
			continue
		}
		if tier.Backend != "" && (strings.HasPrefix(name, tier.Backend+":") || name == tier.Backend) {
			return tier
		}
	}
	return backendPreferenceTier{Rank: 99, Provider: provider, Label: provider + "/other", Reason: "provider has no explicit preference tier yet"}
}

func backendPreferenceTiers() []backendPreferenceTier {
	return backendPreferenceTiersWithConfig(loadUpdevConfig())
}

func backendPreferenceTiersWithConfig(config updevConfig) []backendPreferenceTier {
	defaults := defaultBackendPreferenceTiers()
	if len(config.Backends.PreferenceOrder) == 0 {
		return defaults
	}
	byLabel := map[string]backendPreferenceTier{}
	for _, tier := range knownBackendPreferenceTiers() {
		byLabel[strings.ToLower(tier.Label)] = tier
	}
	out := make([]backendPreferenceTier, 0, len(defaults)+len(config.Backends.PreferenceOrder))
	seen := map[string]bool{}
	for _, rawLabel := range config.Backends.PreferenceOrder {
		label := strings.TrimSpace(rawLabel)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if seen[key] {
			continue
		}
		seen[key] = true
		tier, ok := byLabel[key]
		if !ok {
			tier = backendPreferenceTierFromLabel(label)
		}
		tier.Rank = len(out) + 1
		out = append(out, tier)
	}
	for _, tier := range defaults {
		key := strings.ToLower(tier.Label)
		if seen[key] {
			continue
		}
		tier.Rank = len(out) + 1
		out = append(out, tier)
	}
	return out
}

func defaultBackendPreferenceTiers() []backendPreferenceTier {
	return []backendPreferenceTier{
		{Rank: 1, Provider: "mise", Backend: "", Label: "mise/core", Reason: "prefer mise core backends for reliable CLI developer tools"},
		{Rank: 2, Provider: "mise", Backend: "aqua", Label: "mise/aqua", Reason: "prefer mise's preferred registry backend for feature and supply-chain coverage"},
		{Rank: 3, Provider: "mise", Backend: "github", Label: "mise/github", Reason: "prefer GitHub release backends when no core or aqua registry backend is available"},
		{Rank: 4, Provider: "mise", Backend: "gitlab", Label: "mise/gitlab", Reason: "prefer GitLab release backends when the upstream project is hosted on GitLab"},
		{Rank: 5, Provider: "mise", Backend: "conda", Label: "mise/conda", Reason: "use conda for tools that cannot reasonably use aqua or release backends"},
		{Rank: 6, Provider: "mise", Backend: "pipx", Label: "mise/pipx", Reason: "use pipx only for Python tools that need Python package distribution"},
		{Rank: 7, Provider: "mise", Backend: "npm", Label: "mise/npm", Reason: "use npm only for Node tools that need npm package distribution"},
		{Rank: 8, Provider: "mise", Backend: "gem", Label: "mise/gem", Reason: "use gem only for Ruby tools that need RubyGems package distribution"},
		{Rank: 9, Provider: "mise", Backend: "go", Label: "mise/go", Reason: "use go only when release-style binary distribution is unavailable"},
		{Rank: 10, Provider: "mise", Backend: "cargo", Label: "mise/cargo", Reason: "use cargo only when release-style binary distribution is unavailable"},
		{Rank: 11, Provider: "mise", Backend: "dotnet", Label: "mise/dotnet", Reason: "use dotnet only for tools that need dotnet package distribution"},
		{Rank: 12, Provider: "mas", Backend: "", Label: "store/native", Reason: "use store ownership when app-store evidence is stronger than tool-manager ownership"},
		{Rank: 13, Provider: "brew", Backend: "", Label: "package-manager/native", Reason: "use native package managers for GUI integration, bootstrap, or platform packaging"},
		{Rank: 14, Provider: "vendor", Backend: "", Label: "vendor/manual", Reason: "keep vendor/manual ownership for proprietary installers or weak package evidence"},
	}
}

func knownBackendPreferenceTiers() []backendPreferenceTier {
	known := append([]backendPreferenceTier{}, defaultBackendPreferenceTiers()...)
	known = append(known, deprecatedBackendPreferenceTiers()...)
	return known
}

func deprecatedBackendPreferenceTiers() []backendPreferenceTier {
	return []backendPreferenceTier{
		{Rank: 90, Provider: "mise", Backend: "ubi", Label: "mise/ubi", Reason: "ubi is deprecated in mise; prefer github"},
		{Rank: 91, Provider: "mise", Backend: "vfox", Label: "mise/vfox", Reason: "vfox is useful for private/custom plugins but new registry entries should use aqua or github"},
		{Rank: 92, Provider: "mise", Backend: "asdf", Label: "mise/asdf", Reason: "asdf plugins are legacy and carry higher supply-chain and portability risk"},
	}
}

func backendPreferenceTierFromLabel(label string) backendPreferenceTier {
	provider, backend, ok := strings.Cut(label, "/")
	if !ok {
		provider = label
		backend = ""
	}
	provider = strings.TrimSpace(provider)
	backend = strings.TrimSpace(backend)
	if provider == "" {
		provider = "custom"
	}
	return backendPreferenceTier{
		Provider: provider,
		Backend:  backend,
		Label:    label,
		Reason:   "configured backend preference tier",
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
		{SourceProvider: "mise", SourceName: "cargo:broot", RecommendedProvider: "mise", RecommendedName: "github:Canop/broot", Commands: []string{"broot"}, Reason: "mise GitHub release backend is preferred over a cargo global build when no core or aqua backend is defined"},
		{SourceProvider: "mise", SourceName: "npm:pnpm", RecommendedProvider: "mise", RecommendedName: "aqua:pnpm/pnpm", Commands: []string{"pnpm"}, Reason: "aqua prebuilt CLI avoids npm global package-manager coupling"},
	}
}

func homebrewGitHubBackendRecommendation(name string, githubRepos map[string]string, commandRunner runner.Runner) (backendRecommendation, bool) {
	repo, ok := githubRepos[name]
	if !ok {
		return backendRecommendation{}, false
	}
	if _, err := commandRunner.LookPath(name); err != nil {
		return backendRecommendation{}, false
	}
	return githubBackendCandidate(repo, []string{name}, "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew", commandRunner)
}

func genericHomebrewRecommendationNames(items []plan.Item) []string {
	names := []string{}
	for _, item := range items {
		if item.Kind != "brew" {
			continue
		}
		if _, ok := explicitHomebrewBackendRecommendation(item.Name); ok {
			continue
		}
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names
}

func homebrewFormulaGitHubRepos(names []string, commandRunner runner.Runner) map[string]string {
	repos := map[string]string{}
	if len(names) == 0 {
		return repos
	}
	args := append([]string{"info", "--json=v2"}, names...)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "brew", args...)
	if result.Err != nil || result.Code != 0 {
		return repos
	}
	var payload struct {
		Formulae []struct {
			Name string `json:"name"`
			URLs struct {
				Stable struct {
					URL string `json:"url"`
				} `json:"stable"`
				Head struct {
					URL string `json:"url"`
				} `json:"head"`
			} `json:"urls"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return repos
	}
	for _, formula := range payload.Formulae {
		if strings.TrimSpace(formula.Name) == "" {
			continue
		}
		for _, rawURL := range []string{formula.URLs.Stable.URL, formula.URLs.Head.URL} {
			if repo, ok := backendGitHubRepoFromURL(rawURL); ok {
				repos[formula.Name] = repo
				break
			}
		}
	}
	return repos
}

func miseGitHubBackendRecommendation(name string, commandRunner runner.Runner) (backendRecommendation, bool) {
	backend, packageName, ok := strings.Cut(name, ":")
	if !ok || strings.TrimSpace(packageName) == "" {
		return backendRecommendation{}, false
	}
	var repo string
	switch backend {
	case "cargo":
		repo, ok = cargoPackageGitHubRepo(packageName, commandRunner)
	case "npm":
		repo, ok = npmPackageGitHubRepo(packageName, commandRunner)
	default:
		return backendRecommendation{}, false
	}
	if !ok {
		return backendRecommendation{}, false
	}
	command := backendPackageCommandGuess(packageName)
	if _, err := commandRunner.LookPath(command); err != nil {
		return backendRecommendation{}, false
	}
	return githubBackendCandidate(repo, []string{command}, fmt.Sprintf("%s package metadata points to a GitHub repository; verify GitHub release assets, version mapping, and official distribution ownership before replacing the language package backend", backend), commandRunner)
}

func githubBackendRecommendation(repo string, commands []string, reason string) (backendRecommendation, bool) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return backendRecommendation{}, false
	}
	recommended := "github:" + repo
	tier := backendPreferenceTierFor("mise", recommended)
	return backendRecommendation{
		Provider:       "mise",
		Name:           recommended,
		Tier:           tier.Label,
		PreferenceRank: tier.Rank,
		Commands:       commands,
		Kind:           "recommendation",
		Reason:         reason,
	}, true
}

func githubBackendCandidate(repo string, commands []string, reason string, commandRunner runner.Runner) (backendRecommendation, bool) {
	recommendation, ok := githubBackendRecommendation(repo, commands, reason)
	if !ok {
		return backendRecommendation{}, false
	}
	recommendation.Kind = "candidate"
	recommendation.AssetEvidence = githubReleaseAssetEvidence(repo, commandRunner)
	return recommendation, true
}

func backendRecommendationKind(recommendation backendRecommendation) string {
	if recommendation.Kind != "" {
		return recommendation.Kind
	}
	return "recommendation"
}

func githubReleaseAssetEvidence(repo string, commandRunner runner.Runner) backendGitHubAssetEvidence {
	evidence := backendGitHubAssetEvidence{
		Platform: backendCurrentPlatform(),
		Status:   "unknown",
	}
	if _, err := commandRunner.LookPath("gh"); err != nil {
		evidence.Status = "gh-unavailable"
		return evidence
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "gh", "api", "repos/"+repo+"/releases/latest", "--jq", ".assets[].name")
	if result.Err != nil || result.Code != 0 {
		evidence.Status = "unavailable"
		return evidence
	}
	assets := backendReleaseAssetNames(result.Stdout)
	evidence.Assets = limitedStrings(assets, 12)
	if len(assets) == 0 {
		evidence.Status = "no-assets"
		return evidence
	}
	matches := backendReleaseAssetMatchesPlatform(assets, evidence.Platform)
	evidence.Matches = limitedStrings(matches, 6)
	if len(matches) == 0 {
		evidence.Status = "no-match"
		return evidence
	}
	evidence.Status = "compatible"
	return evidence
}

func backendCurrentPlatform() string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "macos"
	}
	archName := runtime.GOARCH
	switch archName {
	case "amd64":
		archName = "x64"
	case "386":
		archName = "x86"
	}
	return osName + "/" + archName
}

func backendReleaseAssetNames(stdout string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func backendReleaseAssetMatchesPlatform(assets []string, platform string) []string {
	osName, archName, ok := strings.Cut(platform, "/")
	if !ok {
		return nil
	}
	matches := []string{}
	for _, asset := range assets {
		if backendAssetMatchesOS(asset, osName) && backendAssetMatchesArch(asset, archName) {
			matches = append(matches, asset)
		}
	}
	return matches
}

func backendAssetMatchesOS(asset string, osName string) bool {
	asset = strings.ToLower(asset)
	switch osName {
	case "macos":
		return strings.Contains(asset, "darwin") || strings.Contains(asset, "macos") || strings.Contains(asset, "apple-darwin") || strings.Contains(asset, "osx")
	case "linux":
		return strings.Contains(asset, "linux")
	case "windows":
		return strings.Contains(asset, "windows") || strings.Contains(asset, "win32") || strings.Contains(asset, "win64") || strings.Contains(asset, "win-")
	default:
		return strings.Contains(asset, osName)
	}
}

func backendAssetMatchesArch(asset string, archName string) bool {
	asset = strings.ToLower(asset)
	if strings.Contains(asset, "universal") || strings.Contains(asset, "noarch") {
		return true
	}
	switch archName {
	case "arm64":
		return strings.Contains(asset, "arm64") || strings.Contains(asset, "aarch64")
	case "x64":
		return strings.Contains(asset, "x86_64") || strings.Contains(asset, "amd64") || strings.Contains(asset, "x64")
	case "x86":
		return strings.Contains(asset, "i386") || strings.Contains(asset, "i686") || strings.Contains(asset, "x86")
	default:
		return strings.Contains(asset, archName)
	}
}

func limitedStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	out := append([]string{}, values[:limit]...)
	out = append(out, fmt.Sprintf("... %d more", len(values)-limit))
	return out
}

func cargoPackageGitHubRepo(name string, commandRunner runner.Runner) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "cargo", "info", name)
	if result.Err != nil || result.Code != 0 {
		return "", false
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "repository", "homepage":
			if repo, ok := backendGitHubRepoFromURL(strings.TrimSpace(value)); ok {
				return repo, true
			}
		}
	}
	return "", false
}

func npmPackageGitHubRepo(name string, commandRunner runner.Runner) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "npm", "view", name, "repository", "homepage", "--json")
	if result.Err != nil || result.Code != 0 {
		return "", false
	}
	for _, rawURL := range npmMetadataURLs(result.Stdout) {
		if repo, ok := backendGitHubRepoFromURL(rawURL); ok {
			return repo, true
		}
	}
	return "", false
}

func npmMetadataURLs(stdout string) []string {
	var payload any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil
	}
	urls := []string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			urls = append(urls, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for key, item := range typed {
				switch strings.ToLower(key) {
				case "url", "repository", "homepage":
					walk(item)
				default:
					if strings.Contains(strings.ToLower(key), "repository") {
						walk(item)
					}
				}
			}
		}
	}
	walk(payload)
	return urls
}

func backendPackageCommandGuess(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "@")
	if _, after, ok := strings.Cut(name, "/"); ok {
		name = after
	}
	return name
}

func backendGitHubRepoFromURL(rawURL string) (string, bool) {
	rawURL = strings.TrimPrefix(strings.TrimSpace(rawURL), "git+")
	return githubRepoFromURL(rawURL)
}

func homebrewRecommendationConfidence(recommendation backendRecommendation, alreadyDesired bool) string {
	if backendRecommendationKind(recommendation) == "candidate" {
		if recommendation.AssetEvidence.Status == "compatible" {
			return "medium"
		}
		return "low"
	}
	if alreadyDesired {
		return "high"
	}
	return "medium"
}

func miseRecommendationConfidence(spec string, recommendation backendRecommendation, alreadyDesired bool) string {
	if backendRecommendationKind(recommendation) == "candidate" {
		if recommendation.AssetEvidence.Status == "compatible" {
			return "medium"
		}
		return "low"
	}
	if alreadyDesired && backendSpecHasOSConditions(spec) {
		return "high"
	}
	if alreadyDesired {
		return "medium"
	}
	return "low"
}

func homebrewRecommendationAction(item plan.Item, recommendation backendRecommendation, alreadyDesired bool) string {
	if backendRecommendationKind(recommendation) == "candidate" {
		return fmt.Sprintf("review %s as a candidate only; verify release assets, version mapping, and ownership before changing Homebrew ownership", recommendation.Name)
	}
	if alreadyDesired {
		return fmt.Sprintf("review why %s remains in Brewfile before removing; keep it if bootstrap or cask dependency requires Homebrew", item.Name)
	}
	return fmt.Sprintf("review adding %s to mise before considering Brewfile removal", recommendation.Name)
}

func miseRecommendationAction(name string, spec string, recommendedSpec string, recommendation backendRecommendation, alreadyDesired bool) string {
	if backendRecommendationKind(recommendation) == "candidate" {
		sourceBackend := backendSourceNamePrefix(name)
		switch recommendation.AssetEvidence.Status {
		case "compatible":
			return fmt.Sprintf("review %s as a candidate; release assets appear to match %s, but verify version mapping and official distribution before replacing %s", recommendation.Name, recommendation.AssetEvidence.Platform, name)
		case "no-assets", "no-match":
			if sourceBackend == "cargo" {
				return fmt.Sprintf("keep %s as a local cargo build unless compatible GitHub release assets for %s are verified on %s", name, recommendation.Name, recommendation.AssetEvidence.Platform)
			}
			if sourceBackend == "npm" {
				return fmt.Sprintf("keep %s unless compatible GitHub release assets for %s are verified on %s and npm is not the official distribution path", name, recommendation.Name, recommendation.AssetEvidence.Platform)
			}
			return fmt.Sprintf("keep %s unless compatible GitHub release assets for %s are verified on %s", name, recommendation.Name, recommendation.AssetEvidence.Platform)
		default:
			return fmt.Sprintf("review %s as a candidate only; verify release assets, version mapping, and official distribution before replacing %s", recommendation.Name, name)
		}
	}
	if alreadyDesired {
		if backendOSSelectorsCovered(backendOSConditions(spec), backendOSConditions(recommendedSpec)) {
			return fmt.Sprintf("preferred entry %s already covers %s; remove the old key after confirmation", recommendation.Name, name)
		}
		return fmt.Sprintf("preferred entry %s already exists; preserve OS conditions before removing or narrowing %s", recommendation.Name, name)
	}
	if backendSpecHasOSConditions(spec) {
		return fmt.Sprintf("review OS-specific conditions before replacing %s with %s; copy current os list to the preferred entry", name, recommendation.Name)
	}
	return fmt.Sprintf("review replacing %s with %s", name, recommendation.Name)
}

func backendSourceNamePrefix(name string) string {
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(prefix))
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
	fmt.Fprintf(w, "%s %s\n", tr("root:", "root:"), report.Root)
	fmt.Fprintf(w, "%s %d\n", tr("findings:", "検出:"), len(report.Findings))
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "\n"+tr("warnings", "警告"))
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "\n"+tr("no backend convergence findings", "backend 整理の検出はありません"))
		return
	}
	printBackendPreferenceOrder(w)
	rows := make([][]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		action := backendFindingActionText(finding)
		if len(finding.CurrentOS) > 0 {
			action += "; os=" + strings.Join(finding.CurrentOS, ",")
		}
		if finding.ReleaseAssetStatus != "" {
			action += "; " + tr("assets=", "asset=") + finding.ReleaseAssetStatus
		}
		rows = append(rows, []string{
			finding.Type,
			finding.Name,
			finding.RecommendedName,
			finding.Confidence + "/" + finding.CommandStatus,
			action,
		})
	}
	fmt.Fprintln(w, "\n"+tr("findings", "検出"))
	textui.PrintTable(w, []textui.Column{
		{Header: tr("type", "種別"), Min: 18, Max: 24},
		{Header: tr("current", "現在"), Min: 18, Max: 30},
		{Header: tr("target", "候補"), Min: 18, Max: 30},
		{Header: tr("confidence", "確度"), Min: 10, Max: 18},
		{Header: tr("action", "対応"), Min: 24, Max: 56},
	}, rows, color)
	fmt.Fprintln(w, "\n"+tr("next", "次"))
	fmt.Fprintln(w, "  "+tr("review candidates manually; interactive updev can apply safe mise backend rewrites and covered old-entry removals after confirmation", "候補を手動確認します。interactive updev では安全な mise backend rewrite とカバー済み old-entry 削除だけ確認後に適用できます"))
	fmt.Fprintln(w, "  "+tr("Brewfile ownership removal is available only when mise already owns the tool; missing mise entries remain review-only", "Brewfile ownership 削除は mise が既に tool を所有している場合だけ可能です。mise entry が無いものは review-only です"))
}

func backendFindingActionText(finding backendFinding) string {
	if defaultLanguage() != "ja" {
		return finding.Action
	}
	switch finding.Type {
	case "homebrew-to-mise-candidate":
		return fmt.Sprintf("%s は候補としてのみ確認します。Homebrew 管理を変える前に release asset、version 対応、配布元の正当性を確認してください", finding.RecommendedName)
	case "mise-backend-candidate":
		sourceBackend := backendSourceNamePrefix(finding.Name)
		switch finding.ReleaseAssetStatus {
		case "compatible":
			return fmt.Sprintf("%s は候補として確認します。release asset は %s に合いそうですが、%s を置き換える前に version 対応と公式配布元を確認してください", finding.RecommendedName, finding.CurrentPlatform, finding.Name)
		case "no-assets", "no-match":
			if sourceBackend == "cargo" {
				return fmt.Sprintf("%s は local cargo build として維持します。%s の %s 向け GitHub release asset が確認できるまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
			}
			if sourceBackend == "npm" {
				return fmt.Sprintf("%s は維持します。%s の %s 向け GitHub release asset と npm が公式配布元ではないことを確認するまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
			}
			return fmt.Sprintf("%s は維持します。%s の %s 向け GitHub release asset が確認できるまで変更しないでください", finding.Name, finding.RecommendedName, finding.CurrentPlatform)
		default:
			return fmt.Sprintf("%s は候補としてのみ確認します。%s を置き換える前に release asset、version 対応、公式配布元を確認してください", finding.RecommendedName, finding.Name)
		}
	case "homebrew-to-mise":
		if finding.RecommendedSpec != "" {
			return fmt.Sprintf("%s を Brewfile に残す理由を確認してください。bootstrap や cask 依存に Homebrew が必要なら維持します", finding.Name)
		}
		return fmt.Sprintf("Brewfile からの削除を検討する前に %s を mise に追加できるか確認してください", finding.RecommendedName)
	case "mise-backend-rewrite":
		if finding.RecommendedSpec != "" {
			if backendFindingCanRemoveCurrentMiseTool(finding) {
				return fmt.Sprintf("優先 entry %s は %s をカバー済みです。確認後に詳細から古い key を削除できます", finding.RecommendedName, finding.Name)
			}
			return fmt.Sprintf("優先 entry %s は既にあります。%s を削除または狭める前に OS 条件を維持してください", finding.RecommendedName, finding.Name)
		}
		if len(finding.CurrentOS) > 0 {
			return fmt.Sprintf("%s を %s に置き換える前に OS 固有条件を確認し、現在の os list を優先 entry にコピーしてください", finding.Name, finding.RecommendedName)
		}
		return fmt.Sprintf("%s から %s への置き換えを確認してください", finding.Name, finding.RecommendedName)
	default:
		return finding.Action
	}
}

func backendFindingEvidenceText(finding backendFinding) string {
	if defaultLanguage() != "ja" {
		return strings.TrimSpace(firstNonEmpty(finding.Reason, finding.Action, finding.Type))
	}
	if strings.TrimSpace(finding.Action) != "" {
		return backendFindingActionText(finding)
	}
	if reason := localizedBackendReasonText(finding.Reason); reason != "" && reason != finding.Reason {
		return reason
	}
	return localizedBackendReasonText(finding.Type)
}

func localizedBackendReasonText(value string) string {
	value = strings.TrimSpace(value)
	if defaultLanguage() != "ja" {
		return value
	}
	switch value {
	case "stable mise core tool is preferred for CLI developer tools":
		return "CLI 開発ツールは安定した mise core tool を優先します"
	case "fd has a registry-backed mise/aqua path":
		return "fd は registry-backed の mise/aqua 経路があります"
	case "ripgrep is already a stable mise-managed CLI":
		return "ripgrep は既に安定した mise 管理 CLI として扱えます"
	case "aqua prebuilt CLI is preferred over a cargo global build":
		return "cargo global build より aqua の prebuilt CLI を優先します"
	case "mise GitHub release backend is preferred over a cargo global build when no core or aqua backend is defined":
		return "core/aqua backend が無い場合は cargo global build より mise GitHub release backend を優先します"
	case "aqua prebuilt CLI avoids npm global package-manager coupling":
		return "aqua の prebuilt CLI は npm global package-manager への結合を避けられます"
	case "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew":
		return "Homebrew formula の upstream は GitHub repository です。Homebrew から移す前に release asset と ownership を確認してください"
	case "homebrew-to-mise-candidate":
		return "Homebrew から mise への移行候補"
	case "homebrew-to-mise":
		return "Homebrew から mise への移行確認"
	case "mise-backend-candidate":
		return "mise backend の移行候補"
	case "mise-backend-rewrite":
		return "mise backend の書き換え確認"
	default:
		return value
	}
}

func printBackendPreferenceOrder(w io.Writer) {
	parts := make([]string, 0, len(backendPreferenceTiers()))
	for _, tier := range backendPreferenceTiers() {
		parts = append(parts, fmt.Sprintf("%d:%s", tier.Rank, tier.Label))
	}
	fmt.Fprintln(w, "\n"+tr("preference order", "優先順"))
	fmt.Fprintf(w, "  %s\n", strings.Join(parts, " > "))
}

func backendDetailRows(report backendPlanReport) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(report.Findings))
	for _, finding := range report.Findings {
		rows = append(rows, backendFindingDetailRow(finding))
	}
	return rows
}

func backendToolSections(report backendPlanReport) []toolSection {
	sections := []toolSection{}
	indexByName := map[string]int{}
	for _, finding := range report.Findings {
		kind := firstNonEmpty(finding.Type, "backend")
		name := "backend/" + kind
		sectionIndex, ok := indexByName[name]
		if !ok {
			sectionIndex = len(sections)
			indexByName[name] = sectionIndex
			sections = append(sections, toolSection{Name: name, Title: "backend / " + kind})
		}
		sections[sectionIndex].Rows = append(sections[sectionIndex].Rows, detailRowToToolRow(backendFindingDetailRow(finding)))
	}
	return sections
}

func backendFindingDetailRow(finding backendFinding) detailBrowserRow {
	metadata := []string{
		tr("type: ", "種別: ") + finding.Type,
		tr("provider: ", "provider: ") + finding.Provider,
		tr("kind: ", "kind: ") + finding.Kind,
		tr("current: ", "現在: ") + finding.Current,
		tr("target: ", "候補: ") + finding.RecommendedProvider + "/" + finding.RecommendedName,
		tr("recommendation kind: ", "判定種別: ") + finding.RecommendationKind,
		fmt.Sprintf(tr("preference: rank %d %s", "優先度: rank %d %s"), finding.PreferenceRank, finding.RecommendedTier),
		tr("confidence: ", "確度: ") + finding.Confidence,
		tr("command status: ", "コマンド状態: ") + finding.CommandStatus,
	}
	if len(finding.CommandNames) > 0 {
		metadata = append(metadata, tr("commands: ", "コマンド: ")+strings.Join(finding.CommandNames, ", "))
	}
	if finding.CurrentSpec != "" {
		metadata = append(metadata, tr("current spec: ", "現在の spec: ")+finding.CurrentSpec)
	}
	if finding.RecommendedSpec != "" {
		metadata = append(metadata, tr("recommended spec: ", "候補 spec: ")+finding.RecommendedSpec)
	}
	if len(finding.CurrentOS) > 0 {
		metadata = append(metadata, tr("current os: ", "現在の OS 条件: ")+strings.Join(finding.CurrentOS, ", "))
	}
	if len(finding.RecommendedOS) > 0 {
		metadata = append(metadata, tr("recommended os: ", "候補 OS 条件: ")+strings.Join(finding.RecommendedOS, ", "))
	}
	if finding.CurrentPlatform != "" {
		metadata = append(metadata, tr("current platform: ", "現在の platform: ")+finding.CurrentPlatform)
	}
	if finding.ReleaseAssetStatus != "" {
		metadata = append(metadata, tr("release assets: ", "release asset: ")+finding.ReleaseAssetStatus)
	}
	if len(finding.ReleaseAssetMatches) > 0 {
		metadata = append(metadata, tr("matching release assets: ", "一致した release asset: ")+strings.Join(finding.ReleaseAssetMatches, ", "))
	}
	if len(finding.ReleaseAssets) > 0 {
		metadata = append(metadata, tr("release asset sample: ", "release asset サンプル: ")+strings.Join(finding.ReleaseAssets, ", "))
	}
	metadata = append(metadata, tr("applyability: ", "適用可否: ")+backendFindingApplyability(finding))
	return detailBrowserRow{
		Title:    finding.Name + " -> " + finding.RecommendedName,
		Status:   string(finding.Status),
		Summary:  localizedBackendReasonText(finding.Reason),
		Detail:   backendFindingActionText(finding),
		Metadata: metadata,
		Actions:  backendDetailActions(finding),
	}
}

func backendPlanActionableCount(report backendPlanReport) int {
	count := 0
	for _, finding := range report.Findings {
		if len(backendDetailActions(finding)) > 0 {
			count++
		}
	}
	return count
}

func backendDetailActions(finding backendFinding) []detailBrowserAction {
	if finding.Type == "homebrew-to-mise" && finding.RecommendationKind == "recommendation" && finding.RecommendedSpec != "" {
		if finding.Kind == "" || finding.Name == "" {
			return nil
		}
		return []detailBrowserAction{{
			Value:       backendDetailActionValue("remove-brew", finding.Kind+":"+finding.Name, finding.RecommendedName),
			Label:       tr("remove Brewfile entry", "Brewfile entry を削除する"),
			Description: tr("remove the Homebrew desired-state entry because mise already owns the tool", "mise が既に所有しているため Homebrew desired-state entry を削除します"),
		}}
	}
	if finding.Type != "mise-backend-rewrite" || !finding.RewriteAllowed {
		return nil
	}
	if finding.Name == "" || finding.RecommendedName == "" {
		return nil
	}
	if finding.RecommendedSpec != "" {
		if !backendFindingCanRemoveCurrentMiseTool(finding) {
			return nil
		}
		return []detailBrowserAction{{
			Value:       backendDetailActionValue("remove-mise", finding.Name, finding.RecommendedName),
			Label:       tr("remove old mise backend", "古い mise backend を削除する"),
			Description: tr("remove the current backend because the preferred entry already covers it", "優先 entry が現在 entry をカバーしているため、現在の backend entry を削除します"),
		}}
	}
	return []detailBrowserAction{{
		Value:       backendDetailActionValue("rewrite-mise", finding.Name, finding.RecommendedName),
		Label:       tr("rewrite mise backend", "mise backend を書き換える"),
		Description: tr("rename the mise tool key while preserving the current spec", "現在の spec を維持したまま mise tool key を rename します"),
	}}
}

func backendFindingCanRemoveCurrentMiseTool(finding backendFinding) bool {
	if finding.RecommendedSpec == "" {
		return false
	}
	return backendOSSelectorsCovered(finding.CurrentOS, finding.RecommendedOS)
}

func backendOSSelectorsCovered(current []string, recommended []string) bool {
	currentOS := normalizedBackendOSSet(current)
	recommendedOS := normalizedBackendOSSet(recommended)
	if len(currentOS) == 0 {
		return len(recommendedOS) == 0
	}
	if len(recommendedOS) == 0 {
		return true
	}
	for osName := range currentOS {
		if !recommendedOS[osName] {
			return false
		}
	}
	return true
}

func normalizedBackendOSSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}

func backendFindingApplyability(finding backendFinding) string {
	switch {
	case finding.Type == "mise-backend-candidate":
		return tr("review-only candidate; no write action until release assets, version mapping, and official distribution are verified", "review-only 候補。release asset、version 対応、公式配布元を確認するまで write action はありません")
	case finding.Type == "homebrew-to-mise-candidate":
		return tr("review-only candidate; Brewfile migration remains preview-only", "review-only 候補。Brewfile 移行は preview のみです")
	case finding.Type == "homebrew-to-mise":
		if finding.RecommendedSpec != "" {
			return tr("applyable: remove the Homebrew desired-state entry because mise already owns the tool", "適用可能: mise が既に所有しているため Homebrew desired-state entry を削除します")
		}
		return tr("review-only; add or verify the mise entry before removing Brewfile ownership", "review-only。Brewfile ownership を外す前に mise entry を追加または確認してください")
	case finding.Type == "mise-backend-rewrite" && !finding.RewriteAllowed:
		return tr("review-only; rewrite is not allowed for this finding", "review-only。この検出では rewrite は許可されていません")
	case finding.Type == "mise-backend-rewrite" && finding.RecommendedSpec == "":
		return tr("applyable: rewrite current mise key to the preferred backend", "適用可能: 現在の mise key を優先 backend に書き換えます")
	case finding.Type == "mise-backend-rewrite" && backendFindingCanRemoveCurrentMiseTool(finding):
		return tr("applyable: remove current mise key because the preferred entry already covers it", "適用可能: 優先 entry がカバー済みのため現在の mise key を削除します")
	case finding.Type == "mise-backend-rewrite":
		return tr("review-only; preserve OS conditions before removing or narrowing the current entry", "review-only。現在 entry を削除または狭める前に OS 条件を維持してください")
	default:
		return tr("review-only", "review-only")
	}
}

func backendDetailActionValue(action string, current string, recommended string) string {
	return backendDetailActionPrefix + "\t" + action + "\t" + current + "\t" + recommended
}

func parseBackendDetailAction(value string) (string, string, string, bool) {
	parts := strings.SplitN(value, "\t", 4)
	if len(parts) != 4 || parts[0] != backendDetailActionPrefix {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}
