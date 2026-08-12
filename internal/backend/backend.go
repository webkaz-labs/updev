package backend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

const ReportSchemaVersion = 1

type Options struct {
	Command         string
	Root            string
	PreferenceOrder []string
	KeepHomebrew    []string
}

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	Status        plan.Status `json:"status"`
	Command       string      `json:"command"`
	Root          string      `json:"root"`
	Findings      []Finding   `json:"findings,omitempty"`
	Warnings      []string    `json:"warnings,omitempty"`
}

type Finding struct {
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
	SourceEvidence      []string    `json:"source_evidence,omitempty"`
	RecommendationKind  string      `json:"recommendation_kind,omitempty"`
	RewriteAllowed      bool        `json:"rewrite_allowed,omitempty"`
	Confidence          string      `json:"confidence"`
	Reason              string      `json:"reason"`
	Action              string      `json:"action"`
}

type Recommendation struct {
	Provider       string
	Name           string
	Tier           string
	PreferenceRank int
	Reason         string
	Commands       []string
	SourceEvidence []string
	Kind           string
	AssetEvidence  GitHubAssetEvidence
	RewriteAllowed bool
	Version        string
}

type GitHubAssetEvidence struct {
	Platform string
	Status   string
	Matches  []string
	Assets   []string
}

type PreferenceRule struct {
	SourceProvider      string
	SourceName          string
	RecommendedProvider string
	RecommendedName     string
	Commands            []string
	Reason              string
	SourceEvidence      []string
}

type PreferenceTier struct {
	Rank     int
	Provider string
	Backend  string
	Label    string
	Reason   string
}

type Registry struct {
	PreferenceOrder []string
	KeepHomebrew    []string
}

func BuildReport(ctx context.Context, opts Options, commandRunner runner.Runner) Report {
	brewItems, brewWarnings := desiredBrewItems(ctx, opts.Root)
	miseTools, miseWarnings := mise.DesiredTools(opts.Root)
	warnings := append([]string{}, brewWarnings...)
	if miseWarnings != nil {
		warnings = append(warnings, "mise desired tools unavailable: "+miseWarnings.Error())
		miseTools = map[string]string{}
	}
	registry := Registry{PreferenceOrder: opts.PreferenceOrder, KeepHomebrew: opts.KeepHomebrew}
	findings, findingWarnings := Findings(ctx, brewItems, miseTools, registry, commandRunner)
	warnings = append(warnings, findingWarnings...)
	status := plan.StatusOK
	if len(findings) > 0 {
		status = plan.StatusDrift
	}
	if opts.Command == "doctor" {
		findings = filterBackendDoctorFindings(findings)
		if len(findings) == 0 {
			status = plan.StatusOK
		}
	}
	return Report{
		SchemaVersion: ReportSchemaVersion,
		Status:        status,
		Command:       opts.Command,
		Root:          opts.Root,
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

func Findings(ctx context.Context, brewItems []plan.Item, miseTools map[string]string, registry Registry, commandRunner runner.Runner) ([]Finding, []string) {
	findings := []Finding{}
	warnings := []string{}
	miseRegistry, err := loadMiseRegistry(ctx, commandRunner)
	if err != nil {
		warnings = append(warnings, err.Error())
		miseRegistry = map[string]mise.RegistryEntry{}
	}
	homebrewEvidence, err := loadHomebrewPackageEvidence(ctx, brewItems, commandRunner)
	if err != nil {
		warnings = append(warnings, err.Error())
		homebrewEvidence = map[string]homebrewPackageEvidence{}
	}
	genericNames := genericHomebrewRecommendationNames(brewItems, registry, miseRegistry)
	homebrewGitHubRepos := homebrewFormulaGitHubReposFromEvidence(genericNames, homebrewEvidence)
	if len(homebrewGitHubRepos) == 0 && len(genericNames) > 0 && err != nil {
		homebrewGitHubRepos = homebrewFormulaGitHubRepos(genericNames, commandRunner)
	}
	resultCh := make(chan Finding)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	run := func(fn func() (Finding, bool)) {
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
	orderedBrewItems := append([]plan.Item(nil), brewItems...)
	sort.SliceStable(orderedBrewItems, func(i, j int) bool {
		if orderedBrewItems[i].Kind != orderedBrewItems[j].Kind {
			return orderedBrewItems[i].Kind < orderedBrewItems[j].Kind
		}
		return orderedBrewItems[i].Name < orderedBrewItems[j].Name
	})
	registryPlatformChecks := 0
	skippedRegistryPlatformChecks := 0
	for _, item := range orderedBrewItems {
		item := item
		if item.Kind != "brew" && item.Kind != "cask" {
			continue
		}
		if isHomebrewRegistryCheckCandidate(item, homebrewEvidence, miseRegistry, registry, commandRunner) {
			if registryPlatformChecks >= maxRegistryPlatformChecks {
				skippedRegistryPlatformChecks++
				continue
			}
			registryPlatformChecks++
		}
		run(func() (Finding, bool) {
			recommendation, ok := homebrewBackendRecommendation(ctx, item, homebrewGitHubRepos, homebrewEvidence, miseRegistry, registry, commandRunner)
			if !ok {
				return Finding{}, false
			}
			return backendHomebrewFinding(item, recommendation, miseTools, commandRunner), true
		})
	}
	if skippedRegistryPlatformChecks > 0 {
		warnings = append(warnings, fmt.Sprintf("mise registry platform check limit reached: checked %d candidates and skipped %d", maxRegistryPlatformChecks, skippedRegistryPlatformChecks))
	}
	for name, spec := range miseTools {
		name, spec := name, spec
		run(func() (Finding, bool) {
			recommendation, ok := miseBackendRecommendation(name, registry, commandRunner)
			if !ok {
				return Finding{}, false
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
	sort.Strings(warnings)
	return findings, warnings
}

func backendHomebrewFinding(item plan.Item, recommendation Recommendation, miseTools map[string]string, commandRunner runner.Runner) Finding {
	recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
	findingType := "homebrew-to-mise"
	if RecommendationKind(recommendation) == "candidate" {
		findingType = "homebrew-to-mise-candidate"
	}
	return Finding{
		Type:                findingType,
		Status:              plan.StatusHeld,
		Provider:            "brew",
		Kind:                item.Kind,
		Name:                item.Name,
		Current:             item.Kind + ":" + item.Name,
		RecommendedProvider: recommendation.Provider,
		RecommendedName:     recommendation.Name,
		RecommendedTier:     recommendation.Tier,
		PreferenceRank:      recommendation.PreferenceRank,
		CommandNames:        recommendation.Commands,
		CommandStatus:       backendCommandStatus(commandRunner, recommendation.Commands),
		RecommendedSpec:     recommendedSpec,
		CurrentSpec:         recommendation.Version,
		RecommendedOS:       backendOSConditions(recommendedSpec),
		CurrentPlatform:     recommendation.AssetEvidence.Platform,
		ReleaseAssetStatus:  recommendation.AssetEvidence.Status,
		ReleaseAssetMatches: recommendation.AssetEvidence.Matches,
		ReleaseAssets:       recommendation.AssetEvidence.Assets,
		SourceEvidence:      recommendation.SourceEvidence,
		RecommendationKind:  RecommendationKind(recommendation),
		Confidence:          homebrewRecommendationConfidence(recommendation, alreadyDesired),
		Reason:              recommendation.Reason,
		Action:              homebrewRecommendationAction(item, recommendation, alreadyDesired),
	}
}

func backendMiseFinding(name string, spec string, recommendation Recommendation, miseTools map[string]string, commandRunner runner.Runner) Finding {
	recommendedSpec, alreadyDesired := miseTools[recommendation.Name]
	findingType := "mise-backend-rewrite"
	if RecommendationKind(recommendation) == "candidate" {
		findingType = "mise-backend-candidate"
	}
	return Finding{
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
		SourceEvidence:      recommendation.SourceEvidence,
		RecommendationKind:  RecommendationKind(recommendation),
		RewriteAllowed:      recommendation.RewriteAllowed,
		Confidence:          miseRecommendationConfidence(spec, recommendation, alreadyDesired),
		Reason:              recommendation.Reason,
		Action:              miseRecommendationAction(name, spec, recommendedSpec, recommendation, alreadyDesired),
	}
}

func filterBackendDoctorFindings(findings []Finding) []Finding {
	out := []Finding{}
	for _, finding := range findings {
		if finding.Type == "mise-backend-rewrite" || finding.Type == "mise-backend-candidate" {
			out = append(out, finding)
		}
	}
	return out
}

func homebrewBackendRecommendation(ctx context.Context, item plan.Item, githubRepos map[string]string, evidence map[string]homebrewPackageEvidence, miseRegistry map[string]mise.RegistryEntry, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	if registry.KeepsHomebrew(item.Kind, item.Name) {
		return Recommendation{}, false
	}
	if entry, ok := mise.RegistryEntryForTool(miseRegistry, item.Name); ok {
		packageEvidence := evidence[homebrewEvidenceKey(item.Kind, item.Name)]
		return homebrewRegistryRecommendation(ctx, item, entry, packageEvidence, registry, commandRunner)
	}
	if item.Kind != "brew" {
		return Recommendation{}, false
	}
	if recommendation, ok := explicitHomebrewBackendRecommendation(item.Name, registry); ok {
		return recommendation, true
	}
	return homebrewGitHubBackendRecommendation(item.Name, githubRepos, registry, commandRunner)
}

func explicitHomebrewBackendRecommendation(name string, registry Registry) (Recommendation, bool) {
	return registry.PreferenceRecommendation("brew", name)
}

func miseBackendRecommendation(name string, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	if recommendation, ok := explicitMiseBackendRecommendation(name, registry); ok {
		recommendation = finalizeExplicitMiseRecommendation(name, recommendation, commandRunner)
		return recommendation, true
	}
	return miseGitHubBackendRecommendation(name, registry, commandRunner)
}

func explicitMiseBackendRecommendation(name string, registry Registry) (Recommendation, bool) {
	return registry.PreferenceRecommendation("mise", name)
}

func finalizeExplicitMiseRecommendation(sourceName string, recommendation Recommendation, commandRunner runner.Runner) Recommendation {
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
