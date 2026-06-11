package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type safetyGate = securitygate.Gate
type safetySummary = securitygate.Summary
type safetyFinding = securitygate.Finding

func safetySummaryFromFindings(findings []safetyFinding) *safetySummary {
	return securitygate.SummaryFromFindings(findings)
}

func collectUpdateSafety(ctx context.Context, commandRunner commandRunner, opts updateOptions) []safetyGate {
	return collectUpdateSafetyWithPolicy(ctx, commandRunner, opts, loadSecurityPolicy())
}

func collectUpdateSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, opts updateOptions, policy securityPolicy) []safetyGate {
	if opts.security == "off" {
		return nil
	}
	gateCount := 2
	includeVSCode := updateShouldCheckVSCodeSafety(opts)
	if includeVSCode {
		gateCount++
	}
	includeMiseBump := opts.miseBumpMode != "" && opts.miseBumpMode != "off"
	if includeMiseBump {
		gateCount++
	}
	gates := make([]safetyGate, gateCount)
	var wg sync.WaitGroup
	wg.Add(gateCount)
	go func() {
		defer wg.Done()
		gates[0] = collectBrewUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
	}()
	go func() {
		defer wg.Done()
		gates[1] = collectMiseUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
	}()
	if includeVSCode {
		go func() {
			defer wg.Done()
			gates[2] = collectVSCodeUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
		}()
	}
	if includeMiseBump {
		index := gateCount - 1
		go func() {
			defer wg.Done()
			gates[index] = collectMiseBumpSafetyWithPolicy(ctx, commandRunner, opts.root, policy)
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
	seconds := 60
	if config.Security.Homebrew.OutdatedTimeoutSeconds != nil {
		seconds = *config.Security.Homebrew.OutdatedTimeoutSeconds
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			seconds = parsed
		}
	}
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

type brewSafetyManifest struct {
	entries map[string]brewSafetyEntry
}

type brewSafetyEntry struct {
	Kind     string
	Name     string
	RawName  string
	Source   string
	Tap      string
	URLBased bool
}

type brewOutdatedReport struct {
	Formulae []brewOutdatedItem `json:"formulae"`
	Casks    []brewOutdatedItem `json:"casks"`
}

type brewOutdatedItem struct {
	Name              string          `json:"name"`
	InstalledVersions flexStringSlice `json:"installed_versions"`
	CurrentVersion    string          `json:"current_version"`
}

type miseOutdatedItem struct {
	Requested string  `json:"requested"`
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	Bump      *string `json:"bump"`
}

type flexStringSlice []string

func (values *flexStringSlice) UnmarshalJSON(data []byte) error {
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*values = many
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		if one == "" {
			*values = nil
		} else {
			*values = []string{one}
		}
		return nil
	}
	return fmt.Errorf("expected string or []string")
}

func parseBrewOutdated(raw string, manifest brewSafetyManifest) ([]safetyFinding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	payload, err := brewOutdatedJSONPayload(raw)
	if err != nil {
		return nil, err
	}
	var report brewOutdatedReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		return nil, fmt.Errorf("brew outdated --json=v2 --greedy returned invalid JSON: %w", err)
	}
	findings := make([]safetyFinding, 0, len(report.Formulae)+len(report.Casks))
	for _, item := range report.Formulae {
		findings = append(findings, brewSafetyFinding("brew", item, manifest))
	}
	for _, item := range report.Casks {
		findings = append(findings, brewSafetyFinding("cask", item, manifest))
	}
	return findings, nil
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var report map[string]miseOutdatedItem
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("mise outdated --json returned invalid JSON: %w", err)
	}
	names := make([]string, 0, len(report))
	for name := range report {
		names = append(names, name)
	}
	sort.Strings(names)
	findings := make([]safetyFinding, 0, len(names))
	for _, name := range names {
		item := report[name]
		if strings.TrimSpace(item.Latest) == "" && strings.TrimSpace(item.Current) == "" {
			continue
		}
		findings = append(findings, miseSafetyFinding(name, item))
	}
	return findings, nil
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
	values := []string{result.Stderr, result.Stdout}
	if result.Err != nil {
		values = append(values, result.Err.Error())
	}
	if result.Code != 0 {
		values = append(values, fmt.Sprintf("exit status %d", result.Code))
	}
	values = append(values, fallback)
	return firstNonEmpty(values...)
}

func miseSafetyFinding(name string, item miseOutdatedItem) safetyFinding {
	installed := []string{}
	if current := strings.TrimSpace(item.Current); current != "" {
		installed = append(installed, current)
	}
	return safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              strings.TrimSpace(name),
		InstalledVersions: installed,
		CurrentVersion:    strings.TrimSpace(item.Latest),
		Version:           strings.TrimSpace(item.Requested),
		Decision:          "review",
		Reason:            "mise update candidate needs updev-owned provider evidence before update",
		Remediation:       "review the mise backend, source, and candidate version; add a temporary allow policy with reason and expiry if accepted",
		Evidence:          []string{"mise outdated --json"},
		Confidence:        "low",
	}
}

func brewOutdatedResultDetail(result runner.Result, fallback string) string {
	values := []string{result.Stderr, result.Stdout}
	if result.Err != nil {
		values = append(values, result.Err.Error())
	}
	values = append(values, fallback)
	return firstNonEmpty(values...)
}

func brewOutdatedJSONPayload(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("brew outdated --json=v2 --greedy returned no JSON object")
	}
	return raw[start : end+1], nil
}

func brewSafetyFinding(kind string, item brewOutdatedItem, manifest brewSafetyManifest) safetyFinding {
	entry := manifest.entry(kind, item.Name)
	decision := "unknown"
	reason := "release-age and provenance evidence are not available in the first Go safety slice"
	remediation := "retry after Homebrew metadata is available; strict mode requires metadata and release-age evidence"
	confidence := "low"
	trustKind := ""
	trustTarget := ""
	trustCommand := ""
	if entry.URLBased {
		decision = "review"
		reason = "URL-based Homebrew cask needs manual provenance review before update"
		remediation = "review the cask source URL and add a temporary allow policy with reason and expiry if accepted"
	} else if entry.Tap != "" && !isOfficialBrewTap(entry.Tap) {
		decision = "review"
		reason = "non-official Homebrew tap needs provenance review before update"
		trustKind = "formula"
		if kind == "cask" {
			trustKind = "cask"
		}
		trustTarget = firstNonEmpty(entry.RawName, item.Name)
		trustCommand = "brew trust --" + trustKind + " " + trustTarget
		remediation = "review the tap repository; if the package is accepted, prefer item-scoped trust with " + trustCommand + " before adding a temporary allow policy"
	} else if kind == "cask" {
		decision = "review"
		reason = "Homebrew cask updates need provenance and URL/release-age checks before strict mode can allow them"
		remediation = "review vendor provenance and add a temporary allow policy with reason and expiry if accepted"
	}
	finding := safetyFinding{
		Provider:          "brew",
		Kind:              kind,
		Name:              item.Name,
		InstalledVersions: []string(item.InstalledVersions),
		CurrentVersion:    item.CurrentVersion,
		Decision:          decision,
		Reason:            reason,
		Remediation:       remediation,
		Evidence:          []string{"brew outdated --json=v2 --greedy"},
		Source:            entry.Source,
		Tap:               entry.Tap,
		Confidence:        confidence,
	}
	if trustCommand != "" {
		finding.TrustStatus = "needs-review"
		finding.TrustTarget = trustTarget
		finding.TrustCommand = trustCommand
		finding.Evidence = appendEvidence(finding.Evidence, "Homebrew 6 tap trust target: "+trustKind+" "+trustTarget)
	}
	return finding
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
			finding.Reason = "Homebrew metadata unavailable before update: " + err.Error()
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
		finding.Reason = "OSV advisory match for Homebrew source tag: " + strings.Join(finding.AdvisoryIDs, ",")
	case isGitHubAdvisoryFinding(advisory):
		finding.Reason = "GitHub Advisory match for curated Homebrew mapping: " + strings.Join(finding.AdvisoryIDs, ",")
	default:
		finding.Reason = "OSV advisory match for curated Homebrew mapping: " + strings.Join(finding.AdvisoryIDs, ",")
	}
	for _, fixed := range advisory.FixedVersions {
		finding.FixedVersions = appendUniqueString(finding.FixedVersions, fixed)
	}
	finding.Remediation = homebrewAdvisoryRemediation(finding.FixedVersions)
	return finding
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

type githubRelease struct {
	PublishedAt string `json:"published_at"`
	CreatedAt   string `json:"created_at"`
}

type githubGitObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

type githubGitRef struct {
	Ref    string          `json:"ref"`
	Object githubGitObject `json:"object"`
}

type githubGitIdentity struct {
	Date string `json:"date"`
}

type githubGitTag struct {
	Tagger githubGitIdentity `json:"tagger"`
	Object githubGitObject   `json:"object"`
}

type githubGitCommit struct {
	Author    githubGitIdentity `json:"author"`
	Committer githubGitIdentity `json:"committer"`
}

func applyHomebrewReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	if finding.Provider != "brew" || (finding.Kind != "brew" && finding.Kind != "cask") || minAge <= 0 || (finding.URL == "" && finding.Homepage == "") {
		return finding
	}
	repo, tag, ok := githubRepoTagFromURL(finding.URL)
	if ok {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, apiBase, repo, tag, false)
		if err != nil {
			if finding.Decision == "allow" {
				finding.Decision = "review"
				finding.Reason = "GitHub release/tag date unavailable before update: " + err.Error()
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

func applyHomebrewReleaseAgeFromRelease(finding safetyFinding, release githubRelease, minAge time.Duration, evidence string) safetyFinding {
	releasedAt, err := parseGitHubReleaseTime(release)
	if err != nil {
		if finding.Decision == "allow" {
			finding.Decision = "review"
			finding.Reason = "GitHub release date unavailable before update: " + err.Error()
			finding.Remediation = "review the upstream release manually; retry when GitHub release metadata is available or allow by policy"
			finding.Confidence = "medium"
		}
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
		return finding
	}
	age := time.Since(releasedAt)
	finding.ReleaseDate = releasedAt.Format(time.RFC3339)
	finding.ReleaseAgeDays = int(age.Hours() / 24)
	finding.MinReleaseAgeDays = int(minAge.Hours() / 24)
	finding.Evidence = appendEvidence(finding.Evidence, evidence)
	if age < minAge {
		finding.Decision = "hold"
		finding.Reason = fmt.Sprintf("candidate release is too new: age %d days, minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays)
		finding.Remediation = "wait until the release reaches the minimum age or allow temporarily by policy after review"
		finding.Confidence = "medium"
	}
	return finding
}

func inferredHomebrewGitHubReleaseTags(finding safetyFinding) (string, []string, bool) {
	repo, ok := githubRepoFromHomebrewFinding(finding)
	if !ok {
		return "", nil, false
	}
	version := strings.TrimSpace(firstNonEmpty(finding.CurrentVersion, finding.Version))
	if version == "" {
		return "", nil, false
	}
	tags := []string{}
	tags = appendUniqueString(tags, version)
	if !strings.HasPrefix(strings.ToLower(version), "v") {
		tags = appendUniqueString(tags, "v"+version)
	}
	if finding.Name != "" {
		tags = appendUniqueString(tags, finding.Name+"-"+version)
		if !strings.HasPrefix(strings.ToLower(version), "v") {
			tags = appendUniqueString(tags, finding.Name+"-v"+version)
		}
	}
	return repo, tags, len(tags) > 0
}

func githubRepoFromHomebrewFinding(finding safetyFinding) (string, bool) {
	for _, rawURL := range []string{finding.URL, finding.Homepage} {
		repo, ok := githubRepoFromURL(rawURL)
		if ok {
			return repo, true
		}
	}
	return "", false
}

func githubRepoFromURL(rawURL string) (string, bool) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if validGitHubPathPart(owner) && validGitHubPathPart(repo) {
		return owner + "/" + repo, true
	}
	return "", false
}

func githubRepoTagFromURL(rawURL string) (string, string, bool) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 4 {
		return "", "", false
	}
	owner := parts[0]
	repo := parts[1]
	switch {
	case len(parts) >= 5 && parts[2] == "releases" && parts[3] == "download":
		tag := parts[4]
		if validGitHubPathPart(owner) && validGitHubPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	case len(parts) >= 6 && parts[2] == "archive" && parts[3] == "refs" && parts[4] == "tags":
		tag := strings.Join(parts[5:], "/")
		tag = trimArchiveSuffix(tag)
		if validGitHubPathPart(owner) && validGitHubPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	case parts[2] == "archive":
		tag := strings.Join(parts[3:], "/")
		tag = trimArchiveSuffix(tag)
		if validGitHubPathPart(owner) && validGitHubPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	}
	return "", "", false
}

func splitPath(path string) []string {
	parts := []string{}
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		unescaped, err := neturl.PathUnescape(part)
		if err != nil {
			unescaped = part
		}
		parts = append(parts, unescaped)
	}
	return parts
}

func trimArchiveSuffix(tag string) string {
	for _, suffix := range []string{".tar.gz", ".tar.xz", ".tar.bz2", ".tgz", ".zip"} {
		tag = strings.TrimSuffix(tag, suffix)
	}
	return tag
}

func fetchGitHubReleaseByTag(ctx context.Context, client *http.Client, apiBase string, repository string, tag string) (githubRelease, error) {
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/releases/tags/" + neturl.PathEscape(tag)
	var release githubRelease
	if err := fetchGitHubJSON(ctx, client, endpoint, &release); err != nil {
		return githubRelease{}, err
	}
	return release, nil
}

func fetchGitHubReleaseOrTagByTag(ctx context.Context, client *http.Client, apiBase string, repository string, tag string, inferred bool) (githubRelease, string, error) {
	release, releaseErr := fetchGitHubReleaseByTag(ctx, client, apiBase, repository, tag)
	if releaseErr == nil {
		if inferred {
			return release, "GitHub inferred release metadata", nil
		}
		return release, "GitHub release metadata", nil
	}
	release, tagErr := fetchGitHubTagDateByRef(ctx, client, apiBase, repository, tag)
	if tagErr == nil {
		if inferred {
			return release, "GitHub inferred tag metadata", nil
		}
		return release, "GitHub tag metadata", nil
	}
	return githubRelease{}, "", fmt.Errorf("%w; tag metadata fallback failed: %v", releaseErr, tagErr)
}

func fetchGitHubTagDateByRef(ctx context.Context, client *http.Client, apiBase string, repository string, tag string) (githubRelease, error) {
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/ref/tags/" + neturl.PathEscape(tag)
	var ref githubGitRef
	if err := fetchGitHubJSON(ctx, client, endpoint, &ref); err != nil {
		return githubRelease{}, err
	}
	switch ref.Object.Type {
	case "tag":
		return fetchGitHubAnnotatedTagDate(ctx, client, apiBase, repository, ref.Object.SHA)
	case "commit":
		return fetchGitHubCommitDate(ctx, client, apiBase, repository, ref.Object.SHA)
	default:
		return githubRelease{}, fmt.Errorf("unsupported github tag object type: %s", ref.Object.Type)
	}
}

func fetchGitHubAnnotatedTagDate(ctx context.Context, client *http.Client, apiBase string, repository string, sha string) (githubRelease, error) {
	if sha == "" {
		return githubRelease{}, fmt.Errorf("github tag object sha is empty")
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/tags/" + neturl.PathEscape(sha)
	var tag githubGitTag
	if err := fetchGitHubJSON(ctx, client, endpoint, &tag); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(tag.Tagger.Date) != "" {
		return githubRelease{CreatedAt: tag.Tagger.Date}, nil
	}
	if tag.Object.Type == "commit" && tag.Object.SHA != "" {
		return fetchGitHubCommitDate(ctx, client, apiBase, repository, tag.Object.SHA)
	}
	return githubRelease{}, fmt.Errorf("github annotated tag date is empty")
}

func fetchGitHubCommitDate(ctx context.Context, client *http.Client, apiBase string, repository string, sha string) (githubRelease, error) {
	if sha == "" {
		return githubRelease{}, fmt.Errorf("github commit object sha is empty")
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/commits/" + neturl.PathEscape(sha)
	var commit githubGitCommit
	if err := fetchGitHubJSON(ctx, client, endpoint, &commit); err != nil {
		return githubRelease{}, err
	}
	date := firstNonEmpty(commit.Committer.Date, commit.Author.Date)
	if strings.TrimSpace(date) == "" {
		return githubRelease{}, fmt.Errorf("github commit date is empty")
	}
	return githubRelease{CreatedAt: date}, nil
}

func fetchGitHubJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := githubToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("github query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func parseGitHubReleaseTime(release githubRelease) (time.Time, error) {
	for _, value := range []string{release.PublishedAt, release.CreatedAt} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("published_at and created_at are empty")
}

func minHomebrewReleaseAge() time.Duration {
	return minHomebrewReleaseAgeWithConfig(loadUpdevConfig())
}

func minHomebrewReleaseAgeWithConfig(config updevConfig) time.Duration {
	days := 3
	if config.Security.Homebrew.MinReleaseAgeDays != nil && *config.Security.Homebrew.MinReleaseAgeDays >= 0 {
		days = *config.Security.Homebrew.MinReleaseAgeDays
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_HOMEBREW_MIN_RELEASE_AGE_DAYS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			days = parsed
		}
	}
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
		finding.Reason = firstNonEmpty(metadata.DisableReason, "Homebrew metadata marks this entry disabled")
		finding.Remediation = "remove or replace the disabled Homebrew entry before updating"
		finding.Confidence = "medium"
	case metadata.Deprecated:
		finding.Decision = "review"
		finding.Reason = firstNonEmpty(metadata.DeprecationReason, "Homebrew metadata marks this entry deprecated")
		finding.Remediation = "replace the deprecated Homebrew entry or allow temporarily by policy after review"
		finding.Confidence = "medium"
	case finding.Kind == "brew":
		finding.Decision = "allow"
		finding.Reason = "official Homebrew formula metadata is available and not disabled or deprecated"
		finding.Remediation = ""
		finding.Confidence = "medium"
	case finding.Kind == "cask":
		finding.Decision = "review"
		finding.Reason = caskProvenanceReason(finding.HomepageHost, finding.URLHost)
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
	for _, existing := range evidence {
		if existing == value {
			return evidence
		}
	}
	return append(evidence, value)
}

func loadBrewSafetyManifest(root string) (brewSafetyManifest, error) {
	home := updevpath.HomeDir()
	path := filepath.Join(home, "Brewfile")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(root, "Brewfile.tmpl")
	}
	file, err := os.Open(path)
	if err != nil {
		return brewSafetyManifest{}, err
	}
	defer file.Close()
	return parseBrewSafetyManifest(file, path)
}

func parseBrewSafetyManifest(reader io.Reader, source string) (brewSafetyManifest, error) {
	manifest := brewSafetyManifest{entries: map[string]brewSafetyEntry{}}
	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap|vscode)\s+"([^"]+)"`)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		kind := match[1]
		if kind != "brew" && kind != "cask" {
			continue
		}
		rawName := strings.TrimSpace(match[2])
		entry := brewSafetyEntry{
			Kind:     kind,
			Name:     brewSafetyNormalizeName(kind, rawName),
			RawName:  rawName,
			Source:   source,
			Tap:      brewSafetyTap(kind, rawName),
			URLBased: isURLBrewName(rawName),
		}
		manifest.entries[brewSafetyKey(kind, entry.Name)] = entry
	}
	return manifest, scanner.Err()
}

func (manifest brewSafetyManifest) entry(kind string, name string) brewSafetyEntry {
	if manifest.entries == nil {
		return brewSafetyEntry{}
	}
	return manifest.entries[brewSafetyKey(kind, brewSafetyNormalizeName(kind, name))]
}

func manifestWarnings(manifest brewSafetyManifest) []safetyFinding {
	if manifest.entries == nil {
		return nil
	}
	findings := []safetyFinding{}
	for _, entry := range manifest.entries {
		if !entry.URLBased {
			continue
		}
		findings = append(findings, safetyFinding{
			Provider:    "brew",
			Kind:        entry.Kind,
			Name:        entry.RawName,
			Decision:    "review",
			Reason:      "URL-based Homebrew cask needs manual provenance review before update",
			Remediation: "review the cask source URL and add a temporary allow policy with reason and expiry if accepted",
			Evidence:    []string{"Brewfile"},
			Source:      entry.Source,
			Tap:         entry.Tap,
		})
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Name < findings[j].Name
	})
	return findings
}

func brewSafetyKey(kind string, name string) string {
	return kind + ":" + name
}

func brewSafetyNormalizeName(kind string, name string) string {
	name = strings.TrimSpace(name)
	if (kind == "brew" || kind == "cask") && strings.Contains(name, "/") && !isURLBrewName(name) {
		parts := strings.Split(name, "/")
		return parts[len(parts)-1]
	}
	return name
}

func brewSafetyTap(kind string, name string) string {
	if kind != "brew" && kind != "cask" {
		return ""
	}
	if !strings.Contains(name, "/") || isURLBrewName(name) {
		return ""
	}
	parts := strings.Split(name, "/")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func isOfficialBrewTap(tap string) bool {
	return tap == "" || strings.HasPrefix(tap, "homebrew/")
}

func isURLBrewName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://")
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
