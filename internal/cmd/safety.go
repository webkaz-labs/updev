package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/registryaudit"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type safetyGate = securitygate.Gate
type safetySummary = securitygate.Summary
type safetyFinding = securitygate.Finding
type brewSafetyEntry = brew.ManifestEntry

type brewSafetyManifest struct {
	brew.Manifest
}

func safetySummaryFromFindings(findings []safetyFinding) *safetySummary {
	return securitygate.SummaryFromFindings(findings)
}

func applySafetyFindings(gate safetyGate, findings []safetyFinding) safetyGate {
	return securitygate.ApplyFindings(gate, findings)
}

var (
	updateSafetyMarketplaceMaxAge   = 6 * time.Hour
	updateSafetyUnavailableMaxAge   = 45 * time.Minute
	updateSafetyHomebrewMetadataAge = 12 * time.Hour
	updateSafetyMiseMetadataAge     = 12 * time.Hour
	updateSafetyBrewOutdatedMaxAge  = 5 * time.Minute
)

type updateSafetyCacheEntry = securitygate.CacheEntry

func loadUpdateSafetyCache(provider string, key string, maxAge time.Duration) (updateSafetyCacheEntry, bool) {
	return securitygate.LoadCache(provider, key, maxAge)
}

func saveUpdateSafetyCache(provider string, key string, findings []safetyFinding, warnings []string) {
	securitygate.SaveCache(provider, key, findings, warnings)
}

func saveUpdateSafetyErrorCache(provider string, key string, status plan.Status, message string, warnings []string) {
	securitygate.SaveErrorCache(provider, key, status, message, warnings)
}

func saveUpdateSafetyUnavailableCache(provider string, key string, message string, findings []safetyFinding, warnings []string) {
	securitygate.SaveUnavailableCache(provider, key, message, findings, warnings)
}

func updateSafetyCachePath(provider string, key string) string {
	return securitygate.CachePath(provider, key)
}

func updateSafetyCacheKey(parts ...string) string {
	return securitygate.CacheKey(parts...)
}

func updateSafetyBrewCacheKey(root string, findings []safetyFinding, minReleaseAge time.Duration) string {
	return securitygate.BrewCandidateCacheKey(root, findings, minReleaseAge)
}

func updateSafetyBrewOutdatedErrorCacheKey(root string) string {
	return updateSafetyCacheKey("brew", root, "outdated-json-v2")
}

func updateSafetyMiseCacheKey(root string, findings []safetyFinding, minReleaseAge time.Duration) string {
	return securitygate.MiseCandidateCacheKey(root, findings, minReleaseAge)
}

func updateSafetyBrewAdvisoryErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("brew", candidateKey, "advisory-unavailable")
}

func updateSafetyVSCodeCacheKey(root string, items []securitygate.ItemIdentity, installed map[string]string) string {
	return securitygate.VSCodeCandidateCacheKey(root, items, installed)
}

func updateSafetyVSCodeMarketplaceErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("vscode", candidateKey, "marketplace-unavailable")
}

func updateSafetyVSCodeAdvisoryErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("vscode", candidateKey, "advisory-unavailable")
}

func planItemIdentities(items []plan.Item) []securitygate.ItemIdentity {
	return securitygate.ItemIdentitiesFromPlanItems(items)
}

func updateSafetyCacheEvidence(findings []safetyFinding, provider string, createdAt time.Time) []safetyFinding {
	if createdAt.IsZero() {
		return findings
	}
	age := textui.FriendlyAge(time.Since(createdAt))
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		finding.Evidence = appendEvidence(finding.Evidence, "updev update safety cache: "+provider+" "+age+" old")
		out = append(out, finding)
	}
	return out
}

func applyUpdateSafetyUnavailableCache(gate *safetyGate, cached updateSafetyCacheEntry, warning string, evidenceProvider string) []safetyFinding {
	if cached.Error != "" {
		gate.Warnings = append(gate.Warnings, warning+": "+cached.Error)
	}
	gate.Warnings = append(gate.Warnings, cached.Warnings...)
	return updateSafetyCacheEvidence(cached.Findings, evidenceProvider, cached.CreatedAt)
}

func setSafetyFindingReason(finding *safetyFinding, reason securityreason.Reason) {
	if finding == nil {
		return
	}
	finding.Reason = reason.Text
	finding.ReasonCode = reason.Code
	finding.ReasonArgs = reason.Args
}

func setSafetyFindingReasonText(finding *safetyFinding, reason string) {
	setSafetyFindingReason(finding, securityreason.Infer(reason))
}

func localizedSafetyFindingReason(finding safetyFinding) string {
	reason := securityreason.Reason{
		Code: finding.ReasonCode,
		Text: finding.Reason,
		Args: finding.ReasonArgs,
	}
	if reason.Code == "" {
		reason = securityreason.Infer(finding.Reason)
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		localized := securityreason.LocalizeJapanese(reason)
		if strings.TrimSpace(localized) != "" {
			return localized
		}
	}
	return localizedSafetyReason(finding.Reason)
}

func collectUpdateSafety(ctx context.Context, commandRunner commandRunner, opts updateOptions) []safetyGate {
	return collectUpdateSafetyWithPolicy(ctx, commandRunner, opts, loadSecurityPolicy())
}

func collectUpdateSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, opts updateOptions, policy securityPolicy) []safetyGate {
	if opts.security == "off" {
		return nil
	}
	tasks := []updateSafetyTask{
		{provider: "brew", collect: func() safetyGate {
			return collectBrewUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
		}},
		{provider: "mise", collect: func() safetyGate {
			return collectMiseUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
		}},
	}
	if updateShouldCheckVSCodeSafety(opts) {
		tasks = append(tasks, updateSafetyTask{provider: "vscode", collect: func() safetyGate {
			return collectVSCodeUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
		}})
	}
	if opts.miseBumpMode != "" && opts.miseBumpMode != "off" {
		tasks = append(tasks, updateSafetyTask{provider: "mise-bump", collect: func() safetyGate {
			return collectMiseBumpSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
		}})
	}
	return collectUpdateSafetyTasks(tasks)
}

type updateSafetyTask struct {
	provider string
	collect  func() safetyGate
}

func collectUpdateSafetyTasks(tasks []updateSafetyTask) []safetyGate {
	gates := make([]safetyGate, len(tasks))
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for index, task := range tasks {
		index := index
		task := task
		go func() {
			defer wg.Done()
			gate := task.collect()
			if gate.Provider == "" {
				gate.Provider = task.provider
			}
			gates[index] = gate
		}()
	}
	wg.Wait()
	return gates
}

func updateShouldCheckVSCodeSafety(opts updateOptions) bool {
	if !updateIncludesVSCode(opts) {
		return false
	}
	root := opts.root
	items, err := vscodeItemsFromBrewfile(root)
	return err == nil && len(items) > 0
}

func updateIncludesVSCode(opts updateOptions) bool {
	return opts.includeVSCode || includeVSCodeExtensionsByDefault()
}

func collectBrewSafety(ctx context.Context, commandRunner commandRunner, root string) safetyGate {
	return collectBrewSafetyWithPolicy(ctx, commandRunner, root, loadSecurityPolicy())
}

func collectMiseUpdateSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "mise", Status: plan.StatusOK}
	gate.Evidence = append(gate.Evidence, miseMinimumReleaseAgeGateEvidence(ctx, commandRunner, root)...)
	findings, warnings, err := parseMiseOutdatedResult(runMiseOutdatedJSON(ctx, commandRunner, root))
	gate.Warnings = append(gate.Warnings, warnings...)
	if err != nil {
		gate.Status = plan.StatusError
		gate.Error = err.Error()
		return gate
	}
	nativeHeld, nativeWarnings := miseNativeReleaseAgeHoldFindings(ctx, commandRunner, root, findings)
	gate.Warnings = append(gate.Warnings, nativeWarnings...)
	findings = append(findings, nativeHeld...)
	if len(findings) > 0 {
		minReleaseAge := minMiseReleaseAge()
		cacheKey := updateSafetyMiseCacheKey(root, findings, minReleaseAge)
		if cached, ok := loadUpdateSafetyCache("mise", cacheKey, updateSafetyMiseMetadataAge); ok {
			gate.Warnings = append(gate.Warnings, cached.Warnings...)
			findings = updateSafetyCacheEvidence(cached.Findings, "mise", cached.CreatedAt)
		} else {
			findings = enrichMiseSafetyFindings(ctx, commandRunner, http.DefaultClient, findings, minReleaseAge)
			saveUpdateSafetyCache("mise", cacheKey, findings, nil)
		}
	}
	findings = applySecurityPolicyToSafetyFindings(policy, findings)
	return applySafetyFindings(gate, findings)
}

func collectBrewUpdateSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "brew", Status: plan.StatusOK}
	manifest, manifestErr := loadBrewSafetyManifest(root)
	if manifestErr != nil {
		gate.Warnings = append(gate.Warnings, "Homebrew manifest unavailable; provenance checks may be incomplete: "+manifestErr.Error())
	}
	outdatedErrorCacheKey := updateSafetyBrewOutdatedErrorCacheKey(root)
	if cached, ok := loadUpdateSafetyCache("brew", outdatedErrorCacheKey, updateSafetyUnavailableMaxAge); ok && cached.Error != "" {
		if brewOutdatedCachedErrorIsReusable(cached.Error) {
			gate.Status = plan.StatusError
			gate.Error = "cached Homebrew outdated unavailable: " + cached.Error
			gate.Warnings = append(gate.Warnings, cached.Warnings...)
			return gate
		}
		gate.Warnings = append(gate.Warnings, "ignored stale Homebrew outdated cache with provider log output")
	}
	var findings []safetyFinding
	if cached, ok := loadUpdateSafetyCache("brew", outdatedErrorCacheKey, updateSafetyBrewOutdatedMaxAge); ok && cached.Status == plan.StatusOK && cached.Error == "" {
		gate.Warnings = append(gate.Warnings, cached.Warnings...)
		findings = updateSafetyCacheEvidence(cached.Findings, "brew outdated", cached.CreatedAt)
	} else {
		var err error
		var warnings []string
		result := runBrewOutdatedJSON(ctx, commandRunner)
		findings, warnings, err = parseBrewOutdatedResult(result, manifest)
		gate.Warnings = append(gate.Warnings, warnings...)
		if err != nil {
			gate.Status = plan.StatusError
			gate.Error = err.Error()
			saveUpdateSafetyErrorCache("brew", outdatedErrorCacheKey, gate.Status, gate.Error, gate.Warnings)
			return gate
		}
		saveUpdateSafetyCache("brew", outdatedErrorCacheKey, findings, gate.Warnings)
	}
	if len(findings) == 0 {
		return gate
	}
	minReleaseAge := minHomebrewReleaseAge()
	cacheKey := updateSafetyBrewCacheKey(root, findings, minReleaseAge)
	if cached, ok := loadUpdateSafetyCache("brew", cacheKey, updateSafetyHomebrewMetadataAge); ok {
		gate.Warnings = append(gate.Warnings, cached.Warnings...)
		findings = updateSafetyCacheEvidence(cached.Findings, "brew", cached.CreatedAt)
	} else if cached, ok := loadUpdateSafetyCache("brew", updateSafetyBrewAdvisoryErrorCacheKey(cacheKey), updateSafetyUnavailableMaxAge); ok && cached.Error != "" {
		findings = applyUpdateSafetyUnavailableCache(&gate, cached, "cached Homebrew advisory query failed", "brew advisory unavailable")
	} else {
		findings = enrichBrewSafetyFindings(ctx, http.DefaultClient, homebrewAPIURL(), findings, minReleaseAge)
		var advisoryErr error
		findings, advisoryErr = enrichBrewSafetyAdvisories(ctx, http.DefaultClient, osvAPIURL(), findings)
		if advisoryErr != nil {
			gate.Warnings = append(gate.Warnings, "Homebrew advisory query failed: "+advisoryErr.Error())
			saveUpdateSafetyUnavailableCache("brew", updateSafetyBrewAdvisoryErrorCacheKey(cacheKey), advisoryErr.Error(), findings, nil)
		} else {
			saveUpdateSafetyCache("brew", cacheKey, findings, nil)
		}
	}
	findings = applySecurityPolicyToSafetyFindings(policy, findings)
	return applySafetyFindings(gate, findings)
}

func collectBrewUpdateSafetyFreshWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "brew", Status: plan.StatusOK}
	manifest, manifestErr := loadBrewSafetyManifest(root)
	if manifestErr != nil {
		gate.Warnings = append(gate.Warnings, "Homebrew manifest unavailable; provenance checks may be incomplete: "+manifestErr.Error())
	}
	result := runBrewOutdatedJSON(ctx, commandRunner)
	findings, warnings, err := parseBrewOutdatedResult(result, manifest)
	gate.Warnings = append(gate.Warnings, warnings...)
	if err != nil {
		gate.Status = plan.StatusError
		gate.Error = err.Error()
		return gate
	}
	saveUpdateSafetyCache("brew", updateSafetyBrewOutdatedErrorCacheKey(root), findings, gate.Warnings)
	if len(findings) == 0 {
		return gate
	}
	minReleaseAge := minHomebrewReleaseAge()
	cacheKey := updateSafetyBrewCacheKey(root, findings, minReleaseAge)
	if cached, ok := loadUpdateSafetyCache("brew", cacheKey, updateSafetyHomebrewMetadataAge); ok {
		gate.Warnings = append(gate.Warnings, cached.Warnings...)
		findings = updateSafetyCacheEvidence(cached.Findings, "brew", cached.CreatedAt)
	} else if cached, ok := loadUpdateSafetyCache("brew", updateSafetyBrewAdvisoryErrorCacheKey(cacheKey), updateSafetyUnavailableMaxAge); ok && cached.Error != "" {
		findings = applyUpdateSafetyUnavailableCache(&gate, cached, "cached Homebrew advisory query failed", "brew advisory unavailable")
	} else {
		findings = enrichBrewSafetyFindings(ctx, http.DefaultClient, homebrewAPIURL(), findings, minReleaseAge)
		var advisoryErr error
		findings, advisoryErr = enrichBrewSafetyAdvisories(ctx, http.DefaultClient, osvAPIURL(), findings)
		if advisoryErr != nil {
			gate.Warnings = append(gate.Warnings, "Homebrew advisory query failed: "+advisoryErr.Error())
			saveUpdateSafetyUnavailableCache("brew", updateSafetyBrewAdvisoryErrorCacheKey(cacheKey), advisoryErr.Error(), findings, nil)
		} else {
			saveUpdateSafetyCache("brew", cacheKey, findings, nil)
		}
	}
	findings = applySecurityPolicyToSafetyFindings(policy, findings)
	return applySafetyFindings(gate, findings)
}

func brewOutdatedCachedErrorIsReusable(message string) bool {
	lower := strings.ToLower(message)
	for _, noisy := range []string{
		"auto-updating homebrew",
		"homebrew_auto_update_secs",
		"homebrew_no_auto_update",
		"adjust how often this is run",
		"tapping homebrew/core",
		"tapping homebrew/cask",
		"cloning into",
		"updating files:",
	} {
		if strings.Contains(lower, noisy) {
			return false
		}
	}
	return true
}

func collectBrewSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "brew", Status: plan.StatusOK}
	manifest, manifestErr := loadBrewSafetyManifest(root)
	if manifestErr != nil {
		gate.Warnings = append(gate.Warnings, "Homebrew manifest unavailable; provenance checks may be incomplete: "+manifestErr.Error())
	}
	result := runBrewOutdatedJSON(ctx, commandRunner)
	findings, warnings, err := parseBrewOutdatedResult(result, manifest)
	gate.Warnings = append(gate.Warnings, warnings...)
	if err != nil {
		gate.Status = plan.StatusError
		gate.Error = err.Error()
		return gate
	}
	findings = enrichBrewSafetyFindings(ctx, http.DefaultClient, homebrewAPIURL(), findings, minHomebrewReleaseAge())
	var advisoryErr error
	findings, advisoryErr = enrichBrewSafetyAdvisories(ctx, http.DefaultClient, osvAPIURL(), findings)
	if advisoryErr != nil {
		gate.Warnings = append(gate.Warnings, "Homebrew advisory query failed: "+advisoryErr.Error())
	}
	if manifestErr == nil {
		findings = append(findings, manifestWarnings(manifest)...)
	}
	findings = applySecurityPolicyToSafetyFindings(policy, findings)
	return applySafetyFindings(gate, findings)
}

func runBrewOutdatedJSON(ctx context.Context, commandRunner commandRunner) runner.Result {
	timeout := brewOutdatedTimeout()
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1"}
	args = append(args, "brew", "outdated", "--json=v2", "--greedy")
	result := commandRunner.Run(requestCtx, "env", args...)
	if requestCtx.Err() == context.DeadlineExceeded && result.Stdout == "" && result.Stderr == "" {
		result.Stderr = fmt.Sprintf("brew outdated --json=v2 --greedy timed out after %s", timeout)
	}
	return result
}

func brewOutdatedTimeout() time.Duration {
	config := loadUpdevConfig()
	seconds := configuredNonNegativeInt(60, config.Security.Homebrew.OutdatedTimeoutSeconds, "UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS")
	if seconds <= 0 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func runMiseOutdatedJSON(ctx context.Context, commandRunner commandRunner, root string) runner.Result {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := runMiseCommand(requestCtx, commandRunner, nil, nil, "mise", "outdated", "--json", "--cd", root)
	if requestCtx.Err() == context.DeadlineExceeded && result.Stdout == "" && result.Stderr == "" {
		result.Stderr = "mise outdated --json timed out after 20s"
	}
	return result
}

func runMiseOutdatedJSONAgeDisabled(ctx context.Context, commandRunner commandRunner, root string) runner.Result {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := runMiseCommand(requestCtx, commandRunner, nil, nil, "env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root)
	if requestCtx.Err() == context.DeadlineExceeded && result.Stdout == "" && result.Stderr == "" {
		result.Stderr = "MISE_MINIMUM_RELEASE_AGE=0d mise outdated --json timed out after 20s"
	}
	return result
}

func parseBrewOutdated(raw string, manifest brewSafetyManifest) ([]safetyFinding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	report, err := brew.ParseOutdatedReport(raw)
	if err != nil {
		return nil, err
	}
	return brew.SafetyFindingsFromOutdated(report, manifest.Manifest), nil
}

func parseBrewOutdatedResult(result runner.Result, manifest brewSafetyManifest) ([]safetyFinding, []string, error) {
	warnings := []string{}
	if strings.TrimSpace(result.Stdout) == "" && (result.Code != 0 || result.Err != nil) {
		return nil, nil, fmt.Errorf("%s", brewOutdatedResultDetail(result, "brew outdated --json=v2 --greedy returned no output"))
	}
	findings, parseErr := parseBrewOutdated(result.Stdout, manifest)
	if parseErr == nil {
		if result.Code != 0 || result.Err != nil {
			warning := "brew outdated --json=v2 --greedy returned non-zero but JSON output was parsed"
			if detail := brewOutdatedResultDetail(result, ""); detail != "" {
				warning += ": " + detail
			}
			warnings = append(warnings, warning)
		}
		return findings, warnings, nil
	}
	if result.Code != 0 || result.Err != nil {
		return nil, nil, fmt.Errorf("%s", brewOutdatedResultDetail(result, parseErr.Error()))
	}
	return nil, nil, parseErr
}

func parseMiseOutdated(raw string) ([]safetyFinding, error) {
	return mise.SafetyFindingsFromOutdatedJSON(raw)
}

func parseMiseOutdatedResult(result runner.Result) ([]safetyFinding, []string, error) {
	warnings := []string{}
	if strings.TrimSpace(result.Stdout) == "" && (result.Code != 0 || result.Err != nil) {
		return nil, nil, fmt.Errorf("%s", miseOutdatedResultDetail(result, "mise outdated --json returned no output"))
	}
	findings, parseErr := parseMiseOutdated(result.Stdout)
	if parseErr == nil {
		if result.Code != 0 || result.Err != nil {
			warning := "mise outdated --json returned non-zero but JSON output was parsed"
			if detail := miseOutdatedResultDetail(result, ""); detail != "" {
				warning += ": " + detail
			}
			warnings = append(warnings, warning)
		}
		return findings, warnings, nil
	}
	if result.Code != 0 || result.Err != nil {
		return nil, nil, fmt.Errorf("%s", miseOutdatedResultDetail(result, parseErr.Error()))
	}
	return nil, nil, parseErr
}

func miseOutdatedResultDetail(result runner.Result, fallback string) string {
	return runner.ResultDetail(result, fallback, runner.ResultDetailOption{IncludeExitStatus: true})
}

func brewOutdatedResultDetail(result runner.Result, fallback string) string {
	return runner.ResultDetail(result, fallback, runner.ResultDetailOption{})
}

func enrichBrewSafetyFindings(ctx context.Context, client *http.Client, apiBase string, findings []safetyFinding, minReleaseAge time.Duration) []safetyFinding {
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Provider != "brew" || (finding.Kind != "brew" && finding.Kind != "cask") {
			out = append(out, finding)
			continue
		}
		if finding.Tap != "" && !isOfficialBrewTap(finding.Tap) {
			out = append(out, finding)
			continue
		}
		metadata, err := fetchHomebrewMetadata(ctx, client, apiBase, finding.Kind, finding.Name)
		if err != nil {
			finding.Decision = "review"
			setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewMetadataUnavailable, finding.Kind, finding.Name, "Homebrew metadata unavailable before update: "+err.Error(), map[string]string{"error": err.Error()}))
			finding.Remediation = "retry after Homebrew metadata is reachable; otherwise review manually or allow by policy with reason and expiry"
			finding.Confidence = "low"
			finding.Evidence = appendEvidence(finding.Evidence, "formulae.brew.sh metadata")
			out = append(out, finding)
			continue
		}
		finding = applyHomebrewSafetyMetadata(finding, metadata)
		finding = applyHomebrewReleaseAge(ctx, client, githubAPIURL(), finding, minReleaseAge)
		out = append(out, finding)
	}
	return out
}

func enrichBrewSafetyAdvisories(ctx context.Context, client *http.Client, apiURL string, findings []safetyFinding) ([]safetyFinding, error) {
	packages, indexes := brewSafetyAdvisoryPackagesFromFindings(findings)
	if len(packages) == 0 {
		return findings, nil
	}
	advisories, err := queryOSVBatch(ctx, client, apiURL, packages)
	if err != nil {
		return findings, err
	}
	out := append([]safetyFinding(nil), findings...)
	for _, advisory := range advisories {
		key := safetyAdvisoryPackageKey(securityPackage{
			Provider:  advisory.Provider,
			Name:      advisory.Name,
			Version:   advisory.Version,
			Ecosystem: advisory.Ecosystem,
			Package:   advisory.Package,
		})
		for _, index := range indexes[key] {
			out[index] = applyBrewSafetyAdvisory(out[index], advisory)
		}
	}
	githubAdvisories, githubErr := queryGitHubAdvisories(ctx, client, githubAPIURL(), packages)
	if githubErr != nil {
		return out, fmt.Errorf("github advisory query failed: %w", githubErr)
	}
	for _, advisory := range githubAdvisories {
		key := safetyAdvisoryPackageKey(securityPackage{
			Provider:  advisory.Provider,
			Name:      advisory.Name,
			Version:   advisory.Version,
			Ecosystem: advisory.Ecosystem,
			Package:   advisory.Package,
		})
		for _, index := range indexes[key] {
			out[index] = applyBrewSafetyAdvisory(out[index], advisory)
		}
	}
	return out, nil
}

func brewSafetyAdvisoryPackagesFromFindings(findings []safetyFinding) ([]securityPackage, map[string][]int) {
	packages := []securityPackage{}
	indexes := map[string][]int{}
	seen := map[string]bool{}
	for index, finding := range findings {
		if finding.Provider != "brew" || (finding.Kind != "brew" && finding.Kind != "cask") {
			continue
		}
		for _, pkg := range homebrewAdvisoryPackages(finding.Kind, finding.Name, firstNonEmpty(finding.CurrentVersion, finding.Version), finding.URL) {
			key := safetyAdvisoryPackageKey(pkg)
			indexes[key] = append(indexes[key], index)
			if seen[key] {
				continue
			}
			seen[key] = true
			packages = append(packages, pkg)
		}
	}
	return packages, indexes
}

func safetyAdvisoryPackageKey(pkg securityPackage) string {
	return strings.ToLower(strings.Join([]string{pkg.Provider, pkg.Name, pkg.Version, pkg.Ecosystem, pkg.Package}, "\x00"))
}

func applyBrewSafetyAdvisory(finding safetyFinding, advisory securityFinding) safetyFinding {
	finding.Decision = "hold"
	finding.Confidence = "medium"
	switch {
	case advisory.Ecosystem == "GIT":
		finding.Evidence = appendEvidence(finding.Evidence, "osv-git")
	case isGitHubAdvisoryFinding(advisory):
		finding.Evidence = appendEvidence(finding.Evidence, "github-advisory-curated-homebrew-map")
	default:
		finding.Evidence = appendEvidence(finding.Evidence, "osv-curated-homebrew-map")
	}
	finding.AdvisoryIDs = appendUniqueString(finding.AdvisoryIDs, advisory.VulnID)
	switch {
	case advisory.Ecosystem == "GIT":
		setHomebrewAdvisoryReason(&finding, "OSV source tag", "OSV advisory match for Homebrew source tag")
	case isGitHubAdvisoryFinding(advisory):
		setHomebrewAdvisoryReason(&finding, "GitHub Advisory", "GitHub Advisory match for curated Homebrew mapping")
	default:
		setHomebrewAdvisoryReason(&finding, "OSV curated mapping", "OSV advisory match for curated Homebrew mapping")
	}
	for _, fixed := range advisory.FixedVersions {
		finding.FixedVersions = appendUniqueString(finding.FixedVersions, fixed)
	}
	finding.Remediation = homebrewAdvisoryRemediation(finding.FixedVersions)
	return finding
}

func setHomebrewAdvisoryReason(finding *safetyFinding, source string, textPrefix string) {
	ids := strings.Join(finding.AdvisoryIDs, ",")
	setSafetyFindingReason(finding, securityreason.HomebrewPostureReason(securityreason.HomebrewAdvisoryMatch, finding.Kind, finding.Name, textPrefix+": "+ids, map[string]string{
		"advisory_source": source,
		"advisory_ids":    ids,
	}))
}

func homebrewAdvisoryRemediation(fixedVersions []string) string {
	if len(fixedVersions) > 0 {
		return "upgrade to a Homebrew version that includes upstream fixed version: " + strings.Join(fixedVersions, ",") + "; otherwise wait or add a temporary policy override with reason and expiry after review"
	}
	return "review the OSV advisory and wait for a fixed Homebrew version, or add a temporary policy override with reason and expiry after review"
}

func appendUniqueString(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func applyHomebrewReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	if finding.Provider != "brew" || (finding.Kind != "brew" && finding.Kind != "cask") || minAge <= 0 || (finding.URL == "" && finding.Homepage == "") {
		return finding
	}
	repo, tag, ok := githubrepo.RepoTagFromURL(finding.URL)
	if ok {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, apiBase, repo, tag, false)
		if err != nil {
			if finding.Decision == "allow" {
				finding.Decision = "review"
				setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewReleaseUnavailable, finding.Kind, finding.Name, "GitHub release/tag date unavailable before update: "+err.Error(), map[string]string{"error": err.Error()}))
				finding.Remediation = "review the upstream release manually; retry when GitHub release or tag metadata is available or allow by policy"
				finding.Confidence = "medium"
			}
			finding.Evidence = appendEvidence(finding.Evidence, "GitHub release metadata")
			return finding
		}
		return applyHomebrewReleaseAgeFromRelease(finding, release, minAge, evidence)
	}
	repo, tags, ok := inferredHomebrewGitHubReleaseTags(finding)
	if !ok {
		return finding
	}
	for _, tag := range tags {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, apiBase, repo, tag, true)
		if err == nil {
			return applyHomebrewReleaseAgeFromRelease(finding, release, minAge, evidence)
		}
	}
	return finding
}

func applyHomebrewReleaseAgeFromRelease(finding safetyFinding, release githubrepo.Release, minAge time.Duration, evidence string) safetyFinding {
	releasedAt, err := parseGitHubReleaseTime(release)
	if err != nil {
		if finding.Decision == "allow" {
			finding.Decision = "review"
			setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewReleaseUnavailable, finding.Kind, finding.Name, "GitHub release date unavailable before update: "+err.Error(), map[string]string{"error": err.Error()}))
			finding.Remediation = "review the upstream release manually; retry when GitHub release metadata is available or allow by policy"
			finding.Confidence = "medium"
		}
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
		return finding
	}
	finding, age := securitygate.AnnotateReleaseAge(finding, releasedAt, minAge, evidence)
	if age < minAge {
		finding.Decision = "hold"
		setSafetyFindingReason(&finding, securityreason.CandidateReleaseTooNewReason(finding.ReleaseAgeDays, finding.MinReleaseAgeDays))
		finding.Remediation = "wait until the release reaches the minimum age or allow temporarily by policy after review"
		finding.Confidence = "medium"
	}
	return finding
}

func inferredHomebrewGitHubReleaseTags(finding safetyFinding) (string, []string, bool) {
	repo, ok := githubrepo.RepoFromURLs(finding.URL, finding.Homepage)
	if !ok {
		return "", nil, false
	}
	tags := githubrepo.VersionTagCandidates(finding.Name, firstNonEmpty(finding.CurrentVersion, finding.Version))
	return repo, tags, len(tags) > 0
}

func fetchGitHubReleaseOrTagByTag(ctx context.Context, client *http.Client, apiBase string, repository string, tag string, inferred bool) (githubrepo.Release, string, error) {
	return githubrepo.FetchReleaseOrTagByTag(ctx, client, apiBase, githubToken(), repository, tag, inferred)
}

func parseGitHubReleaseTime(release githubrepo.Release) (time.Time, error) {
	return githubrepo.ParseReleaseTime(release)
}

func minHomebrewReleaseAge() time.Duration {
	return minHomebrewReleaseAgeWithConfig(loadUpdevConfig())
}

func minHomebrewReleaseAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(3, config.Security.Homebrew.MinReleaseAgeDays, "UPDEV_HOMEBREW_MIN_RELEASE_AGE_DAYS")
	return time.Duration(days) * 24 * time.Hour
}

func applyHomebrewSafetyMetadata(finding safetyFinding, metadata homebrewMetadata) safetyFinding {
	finding.Tap = firstNonEmpty(metadata.Tap, finding.Tap)
	finding.Homepage = metadata.Homepage
	finding.URL = firstNonEmpty(metadata.URL, metadata.URLs.Stable.URL)
	finding.HomepageHost = hostFromURL(finding.Homepage)
	finding.URLHost = hostFromURL(finding.URL)
	finding.HostMatched = finding.HomepageHost != "" && finding.URLHost != "" && finding.HomepageHost == finding.URLHost
	finding.Version = firstNonEmpty(metadata.Version, metadata.Versions.Stable)
	finding.Deprecated = metadata.Deprecated
	finding.Disabled = metadata.Disabled
	finding.SkipLivecheck = metadata.SkipLivecheck
	finding.Autobump = metadata.Autobump
	finding.Evidence = appendEvidence(finding.Evidence, "formulae.brew.sh metadata")
	switch {
	case metadata.Disabled:
		finding.Decision = "review"
		reasonText := firstNonEmpty(metadata.DisableReason, "Homebrew metadata marks this entry disabled")
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewEntryDisabled, finding.Kind, finding.Name, reasonText, map[string]string{"reason_text": reasonText}))
		finding.Remediation = "remove or replace the disabled Homebrew entry before updating"
		finding.Confidence = "medium"
	case metadata.Deprecated:
		finding.Decision = "review"
		reasonText := firstNonEmpty(metadata.DeprecationReason, "Homebrew metadata marks this entry deprecated")
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewEntryDeprecated, finding.Kind, finding.Name, reasonText, map[string]string{"reason_text": reasonText}))
		finding.Remediation = "replace the deprecated Homebrew entry or allow temporarily by policy after review"
		finding.Confidence = "medium"
	case finding.Kind == "brew":
		finding.Decision = "allow"
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewOfficialFormula, finding.Kind, finding.Name, "official Homebrew formula metadata is available and not disabled or deprecated", nil))
		finding.Remediation = ""
		finding.Confidence = "medium"
	case finding.Kind == "cask":
		finding.Decision = "review"
		setSafetyFindingReason(&finding, securityreason.HomebrewCaskProvenanceReason(finding.Name, caskProvenanceReason(finding.HomepageHost, finding.URLHost), finding.HomepageHost, finding.URLHost))
		finding.Remediation = caskProvenanceRemediation(finding.HomepageHost, finding.URLHost)
		finding.Confidence = "low"
	}
	return finding
}

func caskProvenanceReason(homepageHost string, urlHost string) string {
	if homepageHost != "" && urlHost != "" && homepageHost != urlHost {
		return "Homebrew cask download host differs from homepage host; vendor provenance review required"
	}
	return "Homebrew cask needs vendor provenance review before update"
}

func caskProvenanceRemediation(homepageHost string, urlHost string) string {
	if homepageHost != "" && urlHost != "" && homepageHost != urlHost {
		return "review vendor provenance between homepage host " + homepageHost + " and download host " + urlHost + ", then add a temporary allow policy with reason and expiry if accepted"
	}
	return "review vendor homepage and download host, then add a temporary allow policy with reason and expiry if accepted"
}

func hostFromURL(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.TrimPrefix(host, "www.")
}

func appendEvidence(evidence []string, value string) []string {
	return securitygate.AppendEvidence(evidence, value)
}

func loadBrewSafetyManifest(root string) (brewSafetyManifest, error) {
	path := updevpath.HomeOrRootBrewfile(root)
	file, err := os.Open(path)
	if err != nil {
		return brewSafetyManifest{}, err
	}
	defer file.Close()
	return parseBrewSafetyManifest(file, path)
}

func parseBrewSafetyManifest(reader io.Reader, source string) (brewSafetyManifest, error) {
	manifest, err := brew.ParseManifest(reader, source)
	return brewSafetyManifest{Manifest: manifest}, err
}

func (manifest brewSafetyManifest) entry(kind string, name string) brewSafetyEntry {
	return manifest.Entry(kind, name)
}

func manifestWarnings(manifest brewSafetyManifest) []safetyFinding {
	return brew.ManifestWarnings(manifest.Manifest)
}

func brewSafetyNormalizeName(kind string, name string) string {
	return brew.NormalizePackageName(kind, name)
}

func brewSafetyTap(kind string, name string) string {
	return brew.TapName(kind, name)
}

func isOfficialBrewTap(tap string) bool {
	return brew.IsOfficialTap(tap)
}

func isURLBrewName(name string) bool {
	return brew.IsURLName(name)
}

func providerHeldBySafety(provider string, opts updateOptions, gates []safetyGate) string {
	if opts.security != "strict" {
		return ""
	}
	for _, gate := range gates {
		if gate.Provider != provider {
			continue
		}
		if gate.Status == plan.StatusError {
			return "security=strict held update because safety gate failed: " + gate.Error
		}
		if gate.Status == plan.StatusHeld {
			return "security=strict held update because safety gate requires review"
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const (
	miseBumpProvider = mise.BumpProvider
	miseBumpSource   = mise.BumpSource
)

func defaultMiseBumpMode() string {
	return miseBumpModeWithConfig(loadUpdevConfig())
}

func miseBumpModeWithConfig(config updevConfig) string {
	mode := "manual"
	if config.Update.MiseBump.Mode != nil && validMiseBumpMode(*config.Update.MiseBump.Mode) {
		mode = strings.ToLower(strings.TrimSpace(*config.Update.MiseBump.Mode))
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_MISE_BUMP_MODE")); value != "" && validMiseBumpMode(value) {
		mode = strings.ToLower(value)
	}
	return mode
}

func collectMiseBumpSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: miseBumpProvider, Status: plan.StatusOK}
	findings, warnings, err := parseMiseBumpOutdatedResult(runMiseOutdatedJSONBump(ctx, commandRunner, root))
	gate.Warnings = append(gate.Warnings, warnings...)
	if err != nil {
		if miseBumpOutdatedUnavailable(err.Error()) {
			finding := unavailableMiseBumpDiscoveryFinding(err.Error())
			gate.Status = plan.StatusHeld
			gate.Warnings = append(gate.Warnings, "mise bump candidate discovery unavailable: "+err.Error())
			gate.Findings = []safetyFinding{finding}
			gate.Summary = safetySummaryFromFindings(gate.Findings)
			return gate
		}
		gate.Status = plan.StatusError
		gate.Error = err.Error()
		return gate
	}
	nativeHeld, nativeWarnings := miseNativeReleaseAgeBumpHoldFindings(ctx, commandRunner, root, findings)
	gate.Warnings = append(gate.Warnings, nativeWarnings...)
	findings = append(findings, nativeHeld...)
	if len(findings) > 0 {
		minReleaseAge := minMiseReleaseAge()
		cacheKey := updateSafetyMiseCacheKey(root+"#bump", findings, minReleaseAge)
		if cached, ok := loadUpdateSafetyCache(miseBumpProvider, cacheKey, updateSafetyMiseMetadataAge); ok {
			gate.Warnings = append(gate.Warnings, cached.Warnings...)
			findings = updateSafetyCacheEvidence(cached.Findings, miseBumpProvider, cached.CreatedAt)
		} else {
			findings = enrichMiseSafetyFindings(ctx, commandRunner, http.DefaultClient, findings, minReleaseAge)
			findings = classifyMiseBumpFindings(findings)
			saveUpdateSafetyCache(miseBumpProvider, cacheKey, findings, nil)
		}
	}
	findings = applySecurityPolicyToSafetyFindings(policy, findings)
	gate.Findings = findings
	gate.Summary = safetySummaryFromFindings(findings)
	for _, finding := range findings {
		if finding.Decision != "allow" {
			gate.Status = plan.StatusHeld
			break
		}
	}
	return gate
}

func runMiseOutdatedJSONBump(ctx context.Context, commandRunner commandRunner, root string) runner.Result {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := runMiseCommand(requestCtx, commandRunner, nil, nil, "mise", "outdated", "--json", "--bump", "--cd", root)
	if requestCtx.Err() == context.DeadlineExceeded && result.Stdout == "" && result.Stderr == "" {
		result.Stderr = "mise outdated --json --bump timed out after 20s"
	}
	return result
}

func runMiseOutdatedJSONBumpAgeDisabled(ctx context.Context, commandRunner commandRunner, root string) runner.Result {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := runMiseCommand(requestCtx, commandRunner, nil, nil, "env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root)
	if requestCtx.Err() == context.DeadlineExceeded && result.Stdout == "" && result.Stderr == "" {
		result.Stderr = "MISE_MINIMUM_RELEASE_AGE=0d mise outdated --json --bump timed out after 20s"
	}
	return result
}

func parseMiseBumpOutdatedResult(result runner.Result) ([]safetyFinding, []string, error) {
	warnings := []string{}
	if strings.TrimSpace(result.Stdout) == "" && (result.Code != 0 || result.Err != nil) {
		return nil, nil, fmt.Errorf("%s", miseOutdatedResultDetail(result, "mise outdated --json --bump returned no output"))
	}
	findings, parseErr := parseMiseBumpOutdated(result.Stdout)
	if parseErr == nil {
		if result.Code != 0 || result.Err != nil {
			warning := "mise outdated --json --bump returned non-zero but JSON output was parsed"
			if detail := miseOutdatedResultDetail(result, ""); detail != "" {
				warning += ": " + detail
			}
			warnings = append(warnings, warning)
		}
		return findings, warnings, nil
	}
	if result.Code != 0 || result.Err != nil {
		return nil, nil, fmt.Errorf("%s", miseOutdatedResultDetail(result, parseErr.Error()))
	}
	return nil, nil, parseErr
}

func miseBumpOutdatedUnavailable(detail string) bool {
	lower := strings.ToLower(strings.TrimSpace(detail))
	return strings.Contains(lower, "github rate limit") ||
		strings.Contains(lower, "rate limit exceeded") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "returned no output")
}

func unavailableMiseBumpDiscoveryFinding(detail string) safetyFinding {
	return safetyFinding{
		Provider:    "mise",
		Kind:        "provider",
		Name:        "candidate-discovery",
		Decision:    "review",
		Reason:      "mise bump candidate discovery is temporarily unavailable: " + strings.TrimSpace(detail),
		Remediation: "retry after the GitHub rate limit resets, authenticate GitHub access for mise, or run the bump review again later",
		Evidence:    []string{"mise outdated --json --bump"},
		Confidence:  "low",
	}
}

func parseMiseBumpOutdated(raw string) ([]safetyFinding, error) {
	return mise.BumpSafetyFindingsFromOutdatedJSON(raw)
}

func miseNativeReleaseAgeBumpHoldFindings(ctx context.Context, commandRunner commandRunner, root string, normal []safetyFinding) ([]safetyFinding, []string) {
	disabled, warnings, err := parseMiseBumpOutdatedResult(runMiseOutdatedJSONBumpAgeDisabled(ctx, commandRunner, root))
	if err != nil {
		return nil, []string{"mise minimum_release_age bump comparison unavailable: " + err.Error()}
	}
	held, moreWarnings := miseNativeReleaseAgeHoldFindingsFrom(normal, disabled, "mise outdated --json --bump with MISE_MINIMUM_RELEASE_AGE=0d")
	return held, append(warnings, moreWarnings...)
}

func miseNativeReleaseAgeHoldFindingsFrom(normal []safetyFinding, disabled []safetyFinding, evidence string) ([]safetyFinding, []string) {
	if len(disabled) == 0 {
		return nil, nil
	}
	normalByName := map[string]safetyFinding{}
	for _, finding := range normal {
		normalByName[strings.ToLower(strings.TrimSpace(finding.Name))] = finding
	}
	held := []safetyFinding{}
	for _, disabledFinding := range disabled {
		key := strings.ToLower(strings.TrimSpace(disabledFinding.Name))
		normalFinding, ok := normalByName[key]
		switch {
		case !ok:
			held = append(held, miseNativeReleaseAgeHoldFindingWithEvidence(disabledFinding, "mise minimum_release_age held candidate before it appeared in normal outdated output", evidence))
		case miseCandidateVersion(disabledFinding) != "" && miseCandidateVersion(disabledFinding) != miseCandidateVersion(normalFinding):
			reason := fmt.Sprintf("mise minimum_release_age held newer candidate %s; normal age-gated candidate is %s", miseCandidateVersion(disabledFinding), miseCandidateVersion(normalFinding))
			held = append(held, miseNativeReleaseAgeHoldFindingWithEvidence(disabledFinding, reason, evidence))
		}
	}
	return held, nil
}

func miseNativeReleaseAgeHoldFindingWithEvidence(finding safetyFinding, reason string, evidence string) safetyFinding {
	finding = miseNativeReleaseAgeHoldFinding(finding, reason)
	finding.Evidence = appendEvidence(finding.Evidence, evidence)
	return finding
}

func classifyMiseBumpFindings(findings []safetyFinding) []safetyFinding {
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Source == miseNativeReleaseAgeSource {
			out = append(out, finding)
			continue
		}
		if !strings.EqualFold(finding.Decision, "allow") {
			out = append(out, finding)
			continue
		}
		if reason := miseBumpUnsafeVersionReason(finding); reason != "" {
			finding.Decision = "review"
			finding.Reason = reason
			finding.Remediation = "review the pinned-version bump manually before applying it"
			finding.Confidence = "low"
			out = append(out, finding)
			continue
		}
		finding.Reason = "mise pinned-version bump candidate passed release-age and provenance checks"
		finding.Remediation = ""
		finding.Confidence = firstNonEmpty(finding.Confidence, "medium")
		out = append(out, finding)
	}
	return out
}

func miseBumpUnsafeVersionReason(finding safetyFinding) string {
	from := firstNonEmpty(strings.Join(finding.InstalledVersions, ","), finding.Version)
	to := miseCandidateVersion(finding)
	fromMajor, fromOK := semanticMajor(from)
	toMajor, toOK := semanticMajor(to)
	switch {
	case !fromOK || !toOK:
		return "mise bump candidate version is not a comparable semantic version"
	case fromMajor != toMajor:
		return fmt.Sprintf("mise bump candidate changes major version: %s -> %s", from, to)
	default:
		return ""
	}
}

var semanticVersionMajorPattern = regexp.MustCompile(`^[vV]?([0-9]+)(?:[.][0-9]+){0,2}(?:[-+].*)?$`)

func semanticMajor(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		value = strings.TrimSpace(parts[len(parts)-1])
	}
	match := semanticVersionMajorPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return 0, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return major, true
}

func miseBumpGate(gates []safetyGate) (safetyGate, bool) {
	for _, gate := range gates {
		if gate.Provider == miseBumpProvider {
			return gate, true
		}
	}
	return safetyGate{}, false
}

func safeMiseBumpFindings(gate safetyGate) []safetyFinding {
	out := []safetyFinding{}
	for _, finding := range gate.Findings {
		if strings.EqualFold(finding.Decision, "allow") && miseBumpUnsafeVersionReason(finding) == "" {
			out = append(out, finding)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func unsafeMiseBumpFindings(gate safetyGate) []safetyFinding {
	out := []safetyFinding{}
	for _, finding := range gate.Findings {
		if !strings.EqualFold(finding.Decision, "allow") || miseBumpUnsafeVersionReason(finding) != "" {
			out = append(out, finding)
		}
	}
	return out
}

func validateMiseBumpPlannedCandidates(ctx context.Context, commandRunner commandRunner, root string, planned []safetyFinding) error {
	var result runner.Result
	if miseBumpNeedsReleaseAgeBypass(planned) {
		result = runMiseOutdatedJSONBumpAgeDisabled(ctx, commandRunner, root)
	} else {
		result = runMiseOutdatedJSONBump(ctx, commandRunner, root)
	}
	current, _, err := parseMiseBumpOutdatedResult(result)
	if err != nil {
		return err
	}
	currentByName := map[string]safetyFinding{}
	for _, finding := range current {
		currentByName[strings.ToLower(strings.TrimSpace(finding.Name))] = finding
	}
	for _, finding := range planned {
		name := strings.TrimSpace(finding.Name)
		currentFinding, ok := currentByName[strings.ToLower(name)]
		if !ok {
			return fmt.Errorf("planned candidate %s is no longer reported by mise outdated --bump", name)
		}
		if miseCandidateVersion(currentFinding) != miseCandidateVersion(finding) {
			return fmt.Errorf("planned candidate %s changed from %s to %s", name, miseCandidateVersion(finding), miseCandidateVersion(currentFinding))
		}
	}
	return nil
}

const miseNativeReleaseAgeSource = "mise-native-minimum-release-age"

func miseMinimumReleaseAgeGateEvidence(ctx context.Context, commandRunner commandRunner, root string) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"settings", "ls", "--json-extended"}
	if strings.TrimSpace(root) != "" {
		args = append(args, "--cd", root)
	}
	result := commandRunner.Run(ctx, "mise", args...)
	if result.Err != nil || result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return []string{"mise minimum_release_age evidence unavailable: " + miseOutdatedResultDetail(result, "mise settings output is empty")}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return []string{"mise minimum_release_age evidence unavailable: settings JSON parse failed"}
	}
	value, source, ok := mise.MinimumReleaseAgeFromSettings(payload)
	if !ok || value == "" {
		return []string{"mise minimum_release_age inactive"}
	}
	if source != "" {
		return []string{"mise minimum_release_age active: " + value + " from " + source}
	}
	return []string{"mise minimum_release_age active: " + value}
}

func miseNativeReleaseAgeHoldFindings(ctx context.Context, commandRunner commandRunner, root string, normal []safetyFinding) ([]safetyFinding, []string) {
	disabled, warnings, err := parseMiseOutdatedResult(runMiseOutdatedJSONAgeDisabled(ctx, commandRunner, root))
	if err != nil {
		return nil, []string{"mise minimum_release_age hold comparison unavailable: " + err.Error()}
	}
	held, moreWarnings := miseNativeReleaseAgeHoldFindingsFrom(normal, disabled, "mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d")
	return held, append(warnings, moreWarnings...)
}

func miseNativeReleaseAgeHoldFinding(finding safetyFinding, reason string) safetyFinding {
	finding.Source = miseNativeReleaseAgeSource
	finding.Decision = "hold"
	finding.Reason = reason
	finding.Remediation = "wait until mise minimum_release_age allows this candidate, or add a temporary policy allow after review"
	finding.Confidence = "medium"
	finding.Evidence = appendEvidence(finding.Evidence, "mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d")
	finding.Evidence = appendEvidence(finding.Evidence, "mise minimum_release_age provider comparison")
	return finding
}

func enrichMiseSafetyFindings(ctx context.Context, commandRunner commandRunner, client *http.Client, findings []safetyFinding, minReleaseAge time.Duration) []safetyFinding {
	registry := miseRegistryIndex(ctx, commandRunner)
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Provider != "mise" {
			out = append(out, finding)
			continue
		}
		nativeHold := finding.Source == miseNativeReleaseAgeSource
		originalReason := finding.Reason
		switch {
		case strings.HasPrefix(finding.Name, "github:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "aqua:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseAquaReleaseAge(ctx, client, finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "npm:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseNPMReleaseAge(ctx, client, npmRegistryURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "cargo:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseCargoReleaseAge(ctx, client, cratesIOAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "pipx:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMisePyPIReleaseAge(ctx, client, pypiAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		default:
			if enriched, ok := applyMiseCoreReleaseAge(ctx, client, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			if enriched, ok := applyMiseRegistryGitHubReleaseAge(ctx, client, registry, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			if enriched, ok := applyMiseRegistryProviderMetadataReleaseAge(ctx, client, registry, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			finding.Decision = "review"
			setSafetyFindingReason(&finding, securityreason.MiseOpaqueBackendReason())
			finding.Remediation = "keep the update held until mise native policy evidence or provider metadata can be verified"
			finding.Confidence = "low"
			out = append(out, preserveMiseNativeReleaseAgeHold(finding, nativeHold, originalReason))
		}
	}
	return out
}

func applyMiseCoreReleaseAge(ctx context.Context, client *http.Client, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	repo, tags, ok := miseCoreGitHubRelease(finding)
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.RepositoryURL = "https://github.com/" + repo
	finding.URL = finding.RepositoryURL
	finding.Evidence = appendEvidence(finding.Evidence, "mise core backend "+originalName)
	for _, tag := range tags {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, githubAPIURL(), repo, tag, true)
		if err == nil {
			enriched := applyMiseReleaseAgeFromTime(finding, firstNonEmpty(release.PublishedAt, release.CreatedAt), minAge, evidence)
			enriched.Name = originalName
			enriched.Kind = "tool"
			return enriched, true
		}
	}
	finding.Name = originalName
	finding.Kind = "tool"
	finding.Evidence = appendEvidence(finding.Evidence, "GitHub release/tag metadata")
	return miseReviewFinding(finding, "GitHub release/tag date unavailable before mise core update", "retry when GitHub metadata is reachable or review the core runtime release manually before allowing"), true
}

func miseCoreGitHubRelease(finding safetyFinding) (string, []string, bool) {
	name := strings.TrimSpace(finding.Name)
	version := miseCandidateVersion(finding)
	if version == "" {
		return "", nil, false
	}
	switch name {
	case "go":
		return "golang/go", []string{"go" + version}, true
	case "node":
		return "nodejs/node", []string{"v" + version, version}, true
	case "rust":
		return "rust-lang/rust", []string{version}, true
	default:
		return "", nil, false
	}
}

func applyMiseAquaReleaseAge(ctx context.Context, client *http.Client, finding safetyFinding, minAge time.Duration) safetyFinding {
	repo := strings.TrimSpace(strings.TrimPrefix(finding.Name, "aqua:"))
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !githubrepo.ValidPathPart(parts[0]) || !githubrepo.ValidPathPart(parts[1]) {
		return miseReviewFinding(finding, "mise aqua backend did not expose a valid GitHub repository", "review the mise aqua backend source before allowing this update")
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.Evidence = appendEvidence(finding.Evidence, "mise aqua backend "+repo)
	enriched := applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minAge)
	enriched.Name = originalName
	enriched.Kind = "tool"
	return enriched
}

func miseRegistryIndex(ctx context.Context, commandRunner commandRunner) map[string]mise.RegistryEntry {
	if commandRunner == nil {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := commandRunner.Run(requestCtx, "mise", "registry", "--json")
	if result.Err != nil || result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	return mise.RegistryIndexFromJSON(result.Stdout)
}

func applyMiseRegistryGitHubReleaseAge(ctx context.Context, client *http.Client, registry map[string]mise.RegistryEntry, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	entry, ok := mise.RegistryEntryForTool(registry, finding.Name)
	if !ok {
		return finding, false
	}
	backend, repo, ok := mise.RegistryGitHubBackend(entry)
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.Evidence = appendEvidence(finding.Evidence, "mise registry backend "+backend)
	enriched := applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minAge)
	enriched.Name = originalName
	enriched.Kind = "tool"
	return enriched, true
}

func applyMiseRegistryProviderMetadataReleaseAge(ctx context.Context, client *http.Client, registry map[string]mise.RegistryEntry, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	entry, ok := mise.RegistryEntryForTool(registry, finding.Name)
	if !ok {
		return finding, false
	}
	backend, metadata, ok := mise.RegistryProviderMetadataBackend(entry, mise.ProviderMetadataRegistry())
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Kind = "tool"
	finding.Evidence = appendEvidence(finding.Evidence, "mise registry backend "+backend)
	finding.Evidence = appendEvidence(finding.Evidence, "provider metadata "+metadata.ID)
	switch metadata.ResolverType {
	case mise.ResolverVendorReleaseNotes:
		enriched := applyMiseVendorReleaseNotesAge(ctx, client, metadata, finding, minAge)
		enriched.Name = originalName
		return enriched, true
	default:
		finding.Name = originalName
		return miseReviewFinding(finding, "provider metadata resolver is unsupported for mise backend", "keep the candidate held until updev can resolve provider metadata for this backend"), true
	}
}

func applyMiseVendorReleaseNotesAge(ctx context.Context, client *http.Client, metadata mise.ProviderMetadataEntry, finding safetyFinding, minAge time.Duration) safetyFinding {
	version := miseCandidateVersion(finding)
	if version == "" {
		return miseReviewFinding(finding, "mise provider metadata candidate version is empty", "retry after mise reports a concrete candidate version")
	}
	releaseDate, err := mise.FetchVendorReleaseNoteDate(ctx, client, metadata, version)
	finding.URL = mise.ProviderMetadataURL(metadata)
	finding.SupportURL = metadata.SupportURL
	finding.Evidence = appendEvidence(finding.Evidence, metadata.Evidence)
	if err != nil {
		return miseReviewFinding(finding, "vendor release notes metadata unavailable before mise update: "+err.Error(), "retry when vendor release notes are reachable or review the upstream release manually before allowing")
	}
	return applyMiseReleaseAgeFromTime(finding, releaseDate.Format(time.RFC3339), minAge, metadata.Evidence)
}

func preserveMiseNativeReleaseAgeHold(finding safetyFinding, nativeHold bool, reason string) safetyFinding {
	if !nativeHold {
		return finding
	}
	finding.Source = miseNativeReleaseAgeSource
	finding.Decision = "hold"
	setSafetyFindingReason(&finding, securityreason.MiseMinimumAgeHeldReason(firstNonEmpty(reason, "mise minimum_release_age held this candidate")))
	finding.Remediation = "wait until mise minimum_release_age allows this candidate, or add a temporary policy allow after review"
	if finding.Confidence == "" || finding.Confidence == "low" {
		finding.Confidence = "medium"
	}
	return finding
}

func applyMiseGitHubReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	repo, ok := miseGitHubRepository(finding.Name)
	if !ok {
		return miseReviewFinding(finding, "mise github backend did not expose a valid GitHub repository", "review the mise backend source before allowing this update")
	}
	finding.RepositoryURL = "https://github.com/" + repo
	finding.URL = finding.RepositoryURL
	if minAge <= 0 {
		return allowMiseFinding(finding, "mise github backend candidate accepted because release-age gate is disabled", "GitHub repository metadata")
	}
	tags := miseGitHubReleaseTags(finding)
	for _, tag := range tags {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, apiBase, repo, tag, true)
		if err == nil {
			return applyMiseReleaseAgeFromTime(finding, firstNonEmpty(release.PublishedAt, release.CreatedAt), minAge, evidence)
		}
	}
	finding.Evidence = appendEvidence(finding.Evidence, "GitHub release/tag metadata")
	return miseReviewFinding(finding, "GitHub release/tag date unavailable before mise update", "retry when GitHub metadata is reachable or review the upstream release manually before allowing")
}

func applyMiseNPMReleaseAge(ctx context.Context, client *http.Client, registryBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	pkg := strings.TrimSpace(strings.TrimPrefix(finding.Name, "npm:"))
	if pkg == "" {
		return miseReviewFinding(finding, "mise npm backend package name is empty", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchNPMMetadata(ctx, client, registryBase, pkg)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "npm registry metadata")
		return miseReviewFinding(finding, "npm registry metadata unavailable before mise update: "+err.Error(), "retry when the registry is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	versionInfo := metadata.Versions[version]
	deprecated := firstNonEmpty(versionInfo.Deprecated, metadata.Deprecated)
	finding.Kind = "npm"
	finding.RepositoryURL = registryaudit.NormalizeNPMRepositoryURL(firstNonEmpty(versionInfo.Repository.URL, metadata.Repository.URL))
	finding.URL = "https://www.npmjs.com/package/" + pkg
	finding.Evidence = appendEvidence(finding.Evidence, "npm registry metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise npm candidate version is empty", "retry after mise reports a concrete candidate version")
	case versionInfo.Version == "":
		return miseReviewFinding(finding, "mise npm candidate version is not present in registry metadata", "verify the candidate version before allowing this update")
	case deprecated != "":
		return miseReviewFinding(finding, "mise npm candidate version is deprecated: "+deprecated, "replace the deprecated npm package version or update to a non-deprecated version after review")
	case len(metadata.Maintainers) == 0:
		return miseReviewFinding(finding, "npm package has no maintainers in registry metadata", "review package ownership and source provenance before allowing this update")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "npm package does not expose a source repository URL", "review package provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, metadata.Time[version], minAge, "npm publish metadata")
}

func applyMiseCargoReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	crate := strings.TrimSpace(strings.TrimPrefix(finding.Name, "cargo:"))
	if crate == "" || strings.ContainsAny(crate, " \t\n\r/") {
		return miseReviewFinding(finding, "mise cargo backend crate name is invalid", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchCratesIOMetadata(ctx, client, apiBase, crate)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "crates.io metadata")
		return miseReviewFinding(finding, "crates.io metadata unavailable before mise update: "+err.Error(), "retry when crates.io is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	versionInfo, versionFound := registryaudit.CratesIOVersionByNumber(metadata.Versions, version)
	finding.Kind = "cargo"
	finding.RepositoryURL = metadata.Crate.Repository
	finding.URL = "https://crates.io/crates/" + crate
	finding.Evidence = appendEvidence(finding.Evidence, "crates.io metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise cargo candidate version is empty", "retry after mise reports a concrete candidate version")
	case !versionFound:
		return miseReviewFinding(finding, "mise cargo candidate version is not present in crates.io metadata", "verify the candidate version before allowing this update")
	case versionInfo.Yanked:
		return miseReviewFinding(finding, "mise cargo candidate version is yanked", "update to a non-yanked crate version or replace the crate")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "crate does not expose a source repository URL", "review crate provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, versionInfo.CreatedAt, minAge, "crates.io publish metadata")
}

func applyMisePyPIReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	pkg := strings.TrimSpace(strings.TrimPrefix(finding.Name, "pipx:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r/") {
		return miseReviewFinding(finding, "mise pipx backend package name is invalid", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchPyPIMetadata(ctx, client, apiBase, pkg)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "PyPI metadata")
		return miseReviewFinding(finding, "PyPI metadata unavailable before mise update: "+err.Error(), "retry when PyPI is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	release, releaseFound := registryaudit.PyPIReleaseForVersion(metadata.Releases, version)
	finding.Kind = "pipx"
	finding.RepositoryURL = registryaudit.PyPIRepositoryURL(metadata.Info.ProjectURLs)
	finding.URL = "https://pypi.org/project/" + pkg
	finding.Evidence = appendEvidence(finding.Evidence, "PyPI metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise pipx candidate version is empty", "retry after mise reports a concrete candidate version")
	case !releaseFound:
		return miseReviewFinding(finding, "mise pipx candidate version is not present in PyPI metadata", "verify the candidate version before allowing this update")
	case release.Yanked:
		reason := "mise pipx candidate version is yanked"
		if release.YankedReason != "" {
			reason += ": " + release.YankedReason
		}
		return miseReviewFinding(finding, reason, "update to a non-yanked PyPI release or replace the package")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "PyPI package does not expose a source repository URL", "review package provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, release.UploadTimeISO8601, minAge, "PyPI upload metadata")
}

func applyMiseReleaseAgeFromTime(finding safetyFinding, rawTime string, minAge time.Duration, evidence string) safetyFinding {
	releasedAt, err := parseMiseReleaseTime(rawTime)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
		return miseReviewFinding(finding, "candidate release date unavailable before mise update: "+err.Error(), "retry when provider metadata includes a release date or review manually before allowing")
	}
	finding = securitygate.AnnotateReleaseDate(finding, releasedAt, evidence)
	finding.PublishedDate = finding.ReleaseDate
	if minAge <= 0 {
		return allowMiseFinding(finding, "mise candidate accepted because release-age gate is disabled", "")
	}
	finding, age := securitygate.AnnotateReleaseAge(finding, releasedAt, minAge, "")
	if age < minAge {
		finding.Decision = "hold"
		setSafetyFindingReason(&finding, securityreason.MiseReleaseTooNewReason(finding.ReleaseAgeDays, finding.MinReleaseAgeDays))
		finding.Remediation = "wait until the release reaches the minimum age or allow temporarily by policy after review"
		finding.Confidence = "medium"
		return finding
	}
	return allowMiseFinding(finding, fmt.Sprintf("mise candidate release age passed: age %d days, minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays), "")
}

func allowMiseFinding(finding safetyFinding, reason string, evidence string) safetyFinding {
	finding.Decision = "allow"
	setSafetyFindingReasonText(&finding, reason)
	finding.Remediation = ""
	if finding.Confidence == "" || finding.Confidence == "low" {
		finding.Confidence = "medium"
	}
	if evidence != "" {
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
	}
	return finding
}

func miseReviewFinding(finding safetyFinding, reason string, remediation string) safetyFinding {
	finding.Decision = "review"
	setSafetyFindingReasonText(&finding, reason)
	finding.Remediation = remediation
	if finding.Confidence == "" {
		finding.Confidence = "low"
	}
	return finding
}

func miseCandidateVersion(finding safetyFinding) string {
	return strings.TrimSpace(firstNonEmpty(finding.CurrentVersion, finding.Version))
}

func miseGitHubRepository(name string) (string, bool) {
	repo := strings.TrimSpace(strings.TrimPrefix(name, "github:"))
	repo = strings.TrimSuffix(repo, ".git")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !githubrepo.ValidPathPart(parts[0]) || !githubrepo.ValidPathPart(parts[1]) {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

func miseGitHubReleaseTags(finding safetyFinding) []string {
	version := miseCandidateVersion(finding)
	if version == "" {
		return nil
	}
	tags := []string{}
	tags = appendUniqueString(tags, version)
	if !strings.HasPrefix(strings.ToLower(version), "v") {
		tags = appendUniqueString(tags, "v"+version)
	}
	_, repoName, ok := strings.Cut(strings.TrimPrefix(finding.Name, "github:"), "/")
	if ok && repoName != "" {
		tags = appendUniqueString(tags, repoName+"-"+version)
		if !strings.HasPrefix(strings.ToLower(version), "v") {
			tags = appendUniqueString(tags, repoName+"-v"+version)
		}
	}
	return tags
}

func parseMiseReleaseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("release timestamp is empty")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported release timestamp: %s", value)
}

func minMiseReleaseAge() time.Duration {
	return minMiseReleaseAgeWithConfig(loadUpdevConfig())
}

func minMiseReleaseAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(3, config.Security.Mise.MinReleaseAgeDays, "UPDEV_MISE_MIN_RELEASE_AGE_DAYS")
	return time.Duration(days) * 24 * time.Hour
}
