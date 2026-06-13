package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

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
