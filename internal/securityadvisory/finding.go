package securityadvisory

import (
	"github.com/webkaz-labs/updev/internal/securitygate"
	"sort"
	"strconv"
	"strings"
)

func AppendUniqueFindings(findings []Finding, additions ...Finding) []Finding {
	out := make([]Finding, 0, len(findings)+len(additions))
	out = append(out, findings...)
	for _, addition := range additions {
		if index, ok := matchingFindingIndex(out, addition); ok {
			out[index] = mergeFindings(out[index], addition)
		} else {
			out = append(out, addition)
		}
	}
	return out
}

func ExposureFromPackage(pkg Package) string {
	switch strings.TrimSpace(pkg.PathState) {
	case "on-path":
		if pkg.BinaryName != "" {
			return "on-path-binary:" + pkg.BinaryName
		}
		return "on-path-binary"
	case "not-found":
		if pkg.BinaryName != "" {
			return "binary-not-found:" + pkg.BinaryName
		}
		return "binary-not-found"
	case "unknown":
		return "binary-exposure-unknown"
	default:
		return ""
	}
}

func Reason(finding Finding) string {
	if finding.Reason != "" {
		return finding.Reason
	}
	if finding.KEV != nil {
		return "known exploited vulnerability"
	}
	if finding.Decision == "hold" {
		if strings.HasPrefix(finding.Exposure, "on-path-binary") {
			return "OSV vulnerability match; on-PATH binary exposure"
		}
		return "OSV vulnerability match"
	}
	return ""
}

func Remediation(finding Finding) string {
	if finding.Decision == "allow" {
		return ""
	}
	actions := []string{}
	if len(finding.FixedVersions) > 0 {
		actions = append(actions, "upgrade to a fixed version: "+strings.Join(finding.FixedVersions, ","))
	} else {
		actions = append(actions, "review the advisory and wait for a fixed version or replacement")
	}
	if strings.HasPrefix(finding.Exposure, "on-path-binary") {
		actions = append(actions, "remove or disable the on-PATH binary until fixed")
	}
	return strings.Join(actions, "; ")
}

func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left := findingPriority(findings[i])
		right := findingPriority(findings[j])
		for index := range left {
			if left[index] != right[index] {
				return left[index] > right[index]
			}
		}
		return strings.ToLower(findings[i].Name+"\x00"+findings[i].VulnID) < strings.ToLower(findings[j].Name+"\x00"+findings[j].VulnID)
	})
}

func matchingFindingIndex(findings []Finding, candidate Finding) (int, bool) {
	for index, finding := range findings {
		if !sameFindingSubject(finding, candidate) {
			continue
		}
		if findingIDOverlap(finding, candidate) {
			return index, true
		}
	}
	return 0, false
}

func mergeFindings(primary Finding, secondary Finding) Finding {
	primary.Aliases = appendUniqueStrings(primary.Aliases, secondary.Aliases...)
	primary.FixedVersions = appendUniqueStrings(primary.FixedVersions, secondary.FixedVersions...)
	sort.Strings(primary.FixedVersions)
	if primary.Modified == "" {
		primary.Modified = secondary.Modified
	}
	if primary.Severity == "" {
		primary.Severity = secondary.Severity
	}
	if primary.URL == "" {
		primary.URL = secondary.URL
	}
	primary.Remediation = Remediation(primary)
	return primary
}

func appendUniqueStrings(values []string, additions ...string) []string {
	out := append([]string{}, values...)
	seen := map[string]bool{}
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range additions {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sameFindingSubject(left Finding, right Finding) bool {
	return strings.EqualFold(left.Provider, right.Provider) &&
		strings.EqualFold(left.Name, right.Name) &&
		strings.EqualFold(left.Version, right.Version) &&
		strings.EqualFold(left.Ecosystem, right.Ecosystem) &&
		strings.EqualFold(left.Package, right.Package)
}

func findingIDOverlap(left Finding, right Finding) bool {
	leftIDs := findingIDs(left)
	for id := range findingIDs(right) {
		if leftIDs[strings.ToUpper(id)] {
			return true
		}
	}
	return false
}

func findingIDs(finding Finding) map[string]bool {
	ids := map[string]bool{}
	for _, id := range append([]string{finding.VulnID}, finding.Aliases...) {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		ids[id] = true
	}
	return ids
}

func findingPriority(finding Finding) []int {
	return []int{
		securitygate.DecisionPriority(finding.Decision),
		boolPriority(finding.KEV != nil),
		int(findingEPSSScore(finding) * 100000),
		int(severityScore(finding.Severity) * 10),
		boolPriority(len(finding.FixedVersions) > 0),
		exposurePriority(finding.Exposure),
	}
}

func boolPriority(value bool) int {
	if value {
		return 1
	}
	return 0
}

func findingEPSSScore(finding Finding) float64 {
	if finding.EPSS == nil {
		return 0
	}
	return finding.EPSS.Score
}

func severityScore(severity string) float64 {
	severity = strings.TrimSpace(severity)
	if severity == "" {
		return 0
	}
	if _, after, ok := strings.Cut(severity, ":"); ok {
		severity = after
	}
	switch strings.ToLower(severity) {
	case "critical":
		return 9
	case "high":
		return 7
	case "medium", "moderate":
		return 4
	case "low":
		return 0.1
	}
	value, err := strconv.ParseFloat(severity, 64)
	if err != nil {
		return 0
	}
	return value
}

func exposurePriority(exposure string) int {
	switch {
	case strings.HasPrefix(exposure, "on-path-binary"):
		return 3
	case strings.HasPrefix(exposure, "binary-exposure-unknown"):
		return 2
	case strings.HasPrefix(exposure, "binary-not-found"):
		return 1
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-1] + "…"
}
