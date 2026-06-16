package nativeaudit

import (
	"fmt"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

const (
	FailureMissingBinary     = "missing-binary"
	FailureUnsupportedTarget = "unsupported-target"
	FailureSkippedByScope    = "skipped-by-scope"
	FailureTimeout           = "timeout"
	FailureRateLimit         = "rate-limit"
	FailureParseFailure      = "parse-failure"
	FailureCommandError      = "command-error"
)

type Evidence struct {
	Provider          string            `json:"provider"`
	Ecosystem         string            `json:"ecosystem"`
	Tool              string            `json:"tool"`
	Command           []string          `json:"command,omitempty"`
	Target            string            `json:"target,omitempty"`
	Status            plan.Status       `json:"status"`
	Decision          string            `json:"decision"`
	Reason            string            `json:"reason,omitempty"`
	ReasonCode        string            `json:"reason_code,omitempty"`
	ReasonArgs        map[string]string `json:"reason_args,omitempty"`
	UnavailableReason string            `json:"unavailable_reason,omitempty"`
	ErrorKind         string            `json:"error_kind,omitempty"`
	AdvisoryCount     int               `json:"advisory_count,omitempty"`
	Vulnerabilities   *Counts           `json:"vulnerabilities,omitempty"`
	Error             string            `json:"error,omitempty"`
}

func ReportStatus(current plan.Status, audits []Evidence) plan.Status {
	for _, audit := range audits {
		switch audit.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusBlocked:
			return plan.StatusBlocked
		case plan.StatusHeld:
			if current != plan.StatusError && current != plan.StatusBlocked {
				current = plan.StatusHeld
			}
		}
	}
	return current
}

func Summary(audits []Evidence) (held int, unavailable int, errors int) {
	for _, audit := range audits {
		switch audit.Status {
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

func HasAttention(audits []Evidence) bool {
	for _, audit := range audits {
		if audit.Status != plan.StatusOK || securitygate.DecisionNeedsAttention(audit.Decision) {
			return true
		}
	}
	return false
}

func FromNPMReport(audit Evidence, report NPMReport, unavailableReason string, vulnerableReason string) Evidence {
	if report.Error.Code != "" {
		audit.Status = NPMErrorStatus(report.Error.Code)
		audit.Decision = "review"
		audit.Reason = firstNonEmpty(report.Error.Summary, unavailableReason)
		audit.Error = firstNonEmpty(report.Error.Detail, report.Error.Code)
		setAuditFailureKind(&audit, NPMErrorKind(report.Error.Code))
		return audit
	}
	audit.AdvisoryCount = len(report.Vulnerabilities)
	counts := report.Metadata.Vulnerabilities
	if counts.Total == 0 {
		counts.Total = audit.AdvisoryCount
	}
	if counts.Total > 0 {
		audit.Vulnerabilities = &counts
	}
	if audit.AdvisoryCount > 0 || counts.Total > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, vulnerableReason)
		return audit
	}
	return audit
}

func FromGenericReport(audit Evidence, report GenericReport, unavailableReason string, vulnerableReason string) Evidence {
	if report.Error.Code != "" {
		audit.Status = NPMErrorStatus(report.Error.Code)
		audit.Decision = "review"
		audit.Reason = firstNonEmpty(report.Error.Summary, unavailableReason)
		audit.Error = firstNonEmpty(report.Error.Detail, report.Error.Code)
		setAuditFailureKind(&audit, NPMErrorKind(report.Error.Code))
		return audit
	}
	counts := report.Vulnerable
	if counts.Total == 0 {
		counts.Total = report.Advisories
	}
	audit.AdvisoryCount = report.Advisories
	if counts.Total > 0 {
		audit.Vulnerabilities = &counts
	}
	if audit.AdvisoryCount > 0 || counts.Total > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, vulnerableReason)
	}
	return audit
}

func FromCargoReport(audit Evidence, report CargoReport) Evidence {
	count := report.Vulnerabilities.Count
	if count == 0 {
		count = len(report.Vulnerabilities.List)
	}
	if report.Vulnerabilities.Found || count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "cargo audit reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func FromPipReport(audit Evidence, report PipReport) Evidence {
	count := 0
	for _, dependency := range report.Dependencies {
		count += len(dependency.Vulns)
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "pip-audit reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func FromGovulncheckReport(audit Evidence, report GovulncheckReport) Evidence {
	seen := map[string]bool{}
	count := 0
	for _, finding := range report.Findings {
		id := strings.TrimSpace(finding.OSV)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		count++
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "govulncheck reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func FromComposerReport(audit Evidence, report ComposerReport) Evidence {
	count := CountComposerAdvisories(report.Advisories)
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "composer audit reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func FromBundlerReport(audit Evidence, report BundlerReport) Evidence {
	count := CountBundlerFindings(report)
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "bundle-audit reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func FromDotnetReport(audit Evidence, report DotnetReport) Evidence {
	count := 0
	for _, project := range report.Projects {
		for _, framework := range project.Frameworks {
			for _, pkg := range framework.TopLevelPackages {
				count += len(pkg.Vulnerabilities)
			}
			for _, pkg := range framework.TransitivePackages {
				count += len(pkg.Vulnerabilities)
			}
		}
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		setVulnerabilityReason(&audit, "dotnet package list reported vulnerabilities")
		audit.AdvisoryCount = count
		counts := Counts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func NPMErrorStatus(code string) plan.Status {
	if strings.EqualFold(code, "EAUDITGLOBAL") || strings.EqualFold(code, "ENOLOCK") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func NPMErrorKind(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "EAUDITGLOBAL":
		return FailureUnsupportedTarget
	case "ENOLOCK":
		return FailureUnsupportedTarget
	default:
		return FailureCommandError
	}
}

func PackageManagerErrorStatus(result runner.Result) plan.Status {
	status, _ := PackageManagerFailureStatusAndKind(result)
	return status
}

func PackageManagerFailureStatusAndKind(result runner.Result) (plan.Status, string) {
	detail := normalizedResultDetail(result)
	if strings.Contains(detail, "no package.json") ||
		strings.Contains(detail, "no lockfile") ||
		strings.Contains(detail, "lockfile not found") ||
		strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable, FailureUnsupportedTarget
	}
	if result.Code == 127 ||
		strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") {
		return plan.StatusUnavailable, FailureMissingBinary
	}
	if strings.Contains(detail, "deadline exceeded") ||
		strings.Contains(detail, "timed out") ||
		strings.Contains(detail, "timeout") {
		return plan.StatusUnavailable, FailureTimeout
	}
	if strings.Contains(detail, "rate limit") ||
		strings.Contains(detail, "too many requests") {
		return plan.StatusUnavailable, FailureRateLimit
	}
	return plan.StatusError, FailureCommandError
}

func CargoErrorStatus(result runner.Result) plan.Status {
	status, _ := CargoFailureStatusAndKind(result)
	return status
}

func CargoFailureStatusAndKind(result runner.Result) (plan.Status, string) {
	detail := normalizedResultDetail(result)
	if strings.Contains(detail, "no such command") ||
		strings.Contains(detail, "no such subcommand") ||
		strings.Contains(detail, "install cargo-audit") {
		return plan.StatusUnavailable, FailureMissingBinary
	}
	if strings.Contains(detail, "cargo.lock") ||
		strings.Contains(detail, "no such file") ||
		strings.Contains(detail, "file does not exist") {
		return plan.StatusUnavailable, FailureUnsupportedTarget
	}
	if strings.Contains(detail, "deadline exceeded") ||
		strings.Contains(detail, "timed out") ||
		strings.Contains(detail, "timeout") {
		return plan.StatusUnavailable, FailureTimeout
	}
	return plan.StatusError, FailureCommandError
}

func PipErrorStatus(result runner.Result) plan.Status {
	status, _ := PipFailureStatusAndKind(result)
	return status
}

func PipFailureStatusAndKind(result runner.Result) (plan.Status, string) {
	detail := normalizedResultDetail(result)
	if result.Code == 127 ||
		strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") {
		return plan.StatusUnavailable, FailureMissingBinary
	}
	if strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable, FailureUnsupportedTarget
	}
	if strings.Contains(detail, "deadline exceeded") ||
		strings.Contains(detail, "timed out") ||
		strings.Contains(detail, "timeout") {
		return plan.StatusUnavailable, FailureTimeout
	}
	return plan.StatusError, FailureCommandError
}

func GovulncheckErrorStatus(result runner.Result) plan.Status {
	status, _ := GovulncheckFailureStatusAndKind(result)
	return status
}

func GovulncheckFailureStatusAndKind(result runner.Result) (plan.Status, string) {
	detail := normalizedResultDetail(result)
	if result.Code == 127 ||
		strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") {
		return plan.StatusUnavailable, FailureMissingBinary
	}
	if strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable, FailureUnsupportedTarget
	}
	if strings.Contains(detail, "deadline exceeded") ||
		strings.Contains(detail, "timed out") ||
		strings.Contains(detail, "timeout") {
		return plan.StatusUnavailable, FailureTimeout
	}
	return plan.StatusError, FailureCommandError
}

func ErrorText(result runner.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Code != 0 {
		return fmt.Sprintf("command exited with status %d", result.Code)
	}
	return "unknown error"
}

func setVulnerabilityReason(audit *Evidence, text string) {
	if audit == nil {
		return
	}
	reason := securityreason.NativeAuditVulnerabilityReason(audit.Tool, audit.Ecosystem, audit.Target, text)
	audit.Reason = reason.Text
	audit.ReasonCode = reason.Code
	audit.ReasonArgs = reason.Args
}

func setAuditFailureKind(audit *Evidence, kind string) {
	if audit == nil || kind == "" {
		return
	}
	if audit.Status == plan.StatusUnavailable {
		audit.UnavailableReason = kind
		return
	}
	audit.ErrorKind = kind
}

func normalizedResultDetail(result runner.Result) string {
	return strings.ToLower(runner.ResultDetail(result, ErrorText(result), runner.ResultDetailOption{}))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
