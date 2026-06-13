package securityscanner

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
)

type Evidence struct {
	Tool               string      `json:"tool"`
	Target             string      `json:"target"`
	Command            []string    `json:"command,omitempty"`
	Status             plan.Status `json:"status"`
	Decision           string      `json:"decision"`
	Reason             string      `json:"reason,omitempty"`
	SourceCount        int         `json:"source_count,omitempty"`
	PackageCount       int         `json:"package_count,omitempty"`
	FindingCount       int         `json:"finding_count,omitempty"`
	VulnerabilityCount int         `json:"vulnerability_count,omitempty"`
	Findings           []Finding   `json:"findings,omitempty"`
	Error              string      `json:"error,omitempty"`
}

type Finding struct {
	Kind           string            `json:"kind,omitempty"`
	SourcePath     string            `json:"source_path,omitempty"`
	SourceType     string            `json:"source_type,omitempty"`
	Ecosystem      string            `json:"ecosystem,omitempty"`
	Package        string            `json:"package,omitempty"`
	Version        string            `json:"version,omitempty"`
	DependencyKind string            `json:"dependency_kind,omitempty"`
	VulnID         string            `json:"vuln_id,omitempty"`
	RuleID         string            `json:"rule_id,omitempty"`
	File           string            `json:"file,omitempty"`
	StartLine      int               `json:"start_line,omitempty"`
	EndLine        int               `json:"end_line,omitempty"`
	Commit         string            `json:"commit,omitempty"`
	Fingerprint    string            `json:"fingerprint,omitempty"`
	Description    string            `json:"description,omitempty"`
	URL            string            `json:"url,omitempty"`
	Decision       string            `json:"decision,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	ReasonCode     string            `json:"reason_code,omitempty"`
	ReasonArgs     map[string]string `json:"reason_args,omitempty"`
	Remediation    string            `json:"remediation,omitempty"`
	Confidence     string            `json:"confidence,omitempty"`
	Evidence       []string          `json:"evidence,omitempty"`
	Aliases        []string          `json:"aliases,omitempty"`
	Severity       string            `json:"severity,omitempty"`
	FixedVersions  []string          `json:"fixed_versions,omitempty"`
}

func FindingSource(finding Finding) string {
	return firstNonEmpty(finding.SourcePath, finding.File)
}

func FindingID(finding Finding) string {
	return firstNonEmpty(finding.VulnID, finding.RuleID, finding.Fingerprint)
}

func FindingDetail(finding Finding) string {
	if finding.Package != "" {
		if finding.Version != "" {
			return finding.Package + "@" + finding.Version
		}
		return finding.Package
	}
	if finding.StartLine > 0 {
		if finding.EndLine > finding.StartLine {
			return fmt.Sprintf("lines %d-%d", finding.StartLine, finding.EndLine)
		}
		return fmt.Sprintf("line %d", finding.StartLine)
	}
	return finding.Description
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
		leftKey := strings.ToLower(FindingSource(findings[i]) + "\x00" + FindingID(findings[i]) + "\x00" + FindingDetail(findings[i]))
		rightKey := strings.ToLower(FindingSource(findings[j]) + "\x00" + FindingID(findings[j]) + "\x00" + FindingDetail(findings[j]))
		return leftKey < rightKey
	})
}

func ApplyPolicyDecision(evidence Evidence) Evidence {
	if len(evidence.Findings) == 0 || (evidence.Status != plan.StatusHeld && evidence.Status != plan.StatusOK) {
		return evidence
	}
	attention := false
	blocked := false
	for _, finding := range evidence.Findings {
		switch strings.ToLower(strings.TrimSpace(finding.Decision)) {
		case "block":
			blocked = true
		case "hold", "review", "":
			attention = true
		}
	}
	switch {
	case blocked:
		evidence.Status = plan.StatusBlocked
		evidence.Decision = "block"
		evidence.Reason = "scanner findings blocked by security policy"
	case attention:
		evidence.Status = plan.StatusHeld
		if evidence.Decision == "allow" {
			evidence.Decision = "hold"
		}
	case evidence.Status == plan.StatusHeld:
		evidence.Status = plan.StatusOK
		evidence.Decision = "allow"
		evidence.Reason = "scanner findings allowed by security policy"
	}
	return evidence
}

func ReportStatus(current plan.Status, scanners []Evidence) plan.Status {
	status := current
	for _, scanner := range scanners {
		if scanner.Status == plan.StatusBlocked {
			return plan.StatusBlocked
		}
		if scanner.Status == plan.StatusHeld {
			if status == plan.StatusOK {
				status = plan.StatusHeld
			}
		}
	}
	return status
}

func Summary(scanners []Evidence) (held int, unavailable int, errors int) {
	for _, scanner := range scanners {
		switch scanner.Status {
		case plan.StatusHeld:
			held++
		case plan.StatusUnavailable:
			unavailable++
		case plan.StatusError:
			errors++
		}
	}
	return held, unavailable, errors
}

func HasAttention(scanners []Evidence) bool {
	for _, scanner := range scanners {
		if scanner.Status != plan.StatusOK || securitygate.DecisionNeedsAttention(scanner.Decision) {
			return true
		}
	}
	return false
}

func HasFindingAttention(scanners []Evidence) bool {
	for _, scanner := range scanners {
		for _, finding := range scanner.Findings {
			if securitygate.DecisionNeedsAttention(finding.Decision) {
				return true
			}
		}
	}
	return false
}

func findingPriority(finding Finding) []int {
	return []int{
		securitygate.DecisionPriority(finding.Decision),
		findingKindPriority(finding.Kind),
		int(severityScore(finding.Severity) * 10),
		boolPriority(len(finding.FixedVersions) > 0),
		dependencyKindPriority(finding.DependencyKind),
	}
}

func dependencyKindPriority(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "direct":
		return 2
	case "transitive":
		return 1
	default:
		return 0
	}
}

func findingKindPriority(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "secret":
		return 4
	case "vulnerability":
		return 3
	case "workflow":
		return 2
	default:
		return 0
	}
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

func boolPriority(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
