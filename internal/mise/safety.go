package mise

import (
	"fmt"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/securitygate"
)

const (
	BumpProvider = "mise-bump"
	BumpSource   = "mise-bump"
)

func SafetyFindingFromOutdated(name string, item OutdatedItem) securitygate.Finding {
	installed := []string{}
	if current := strings.TrimSpace(item.Current); current != "" {
		installed = append(installed, current)
	}
	return securitygate.Finding{
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

func BumpSafetyFindingFromOutdated(name string, item OutdatedItem) (securitygate.Finding, bool) {
	if item.Bump == nil || strings.TrimSpace(*item.Bump) == "" {
		return securitygate.Finding{}, false
	}
	item.Latest = strings.TrimSpace(*item.Bump)
	finding := SafetyFindingFromOutdated(name, item)
	finding.Source = BumpSource
	finding.Evidence = replaceEvidence(finding.Evidence, "mise outdated --json", "mise outdated --json --bump")
	finding.Evidence = appendEvidence(finding.Evidence, "mise pinned-version bump candidate")
	finding.Reason = "mise pinned-version bump candidate needs release-age and provenance evidence before bump"
	finding.Remediation = "review the candidate, then run a scoped mise bump action if accepted"
	return finding, true
}

func SafetyFindingsFromOutdatedJSON(raw string) ([]securitygate.Finding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	report, err := ParseOutdatedReport(raw)
	if err != nil {
		return nil, fmt.Errorf("mise outdated --json returned invalid JSON: %w", err)
	}
	names := sortedOutdatedNames(report)
	findings := make([]securitygate.Finding, 0, len(names))
	for _, name := range names {
		item := report[name]
		if strings.TrimSpace(item.Latest) == "" && strings.TrimSpace(item.Current) == "" {
			continue
		}
		findings = append(findings, SafetyFindingFromOutdated(name, item))
	}
	return findings, nil
}

func BumpSafetyFindingsFromOutdatedJSON(raw string) ([]securitygate.Finding, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	report, err := ParseOutdatedReport(raw)
	if err != nil {
		return nil, fmt.Errorf("mise outdated --json --bump returned invalid JSON: %w", err)
	}
	names := sortedOutdatedNames(report)
	findings := make([]securitygate.Finding, 0, len(names))
	for _, name := range names {
		finding, ok := BumpSafetyFindingFromOutdated(name, report[name])
		if !ok {
			continue
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func sortedOutdatedNames(report map[string]OutdatedItem) []string {
	names := make([]string, 0, len(report))
	for name := range report {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func replaceEvidence(values []string, oldValue string, newValue string) []string {
	out := make([]string, 0, len(values))
	replaced := false
	for _, value := range values {
		if value == oldValue {
			out = append(out, newValue)
			replaced = true
			continue
		}
		out = append(out, value)
	}
	if !replaced {
		out = append(out, newValue)
	}
	return out
}

func appendEvidence(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
