package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
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
