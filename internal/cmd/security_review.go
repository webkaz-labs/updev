package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
)

func buildSecurityReviewReport(ctx context.Context, opts securityReviewOptions, client *http.Client, commandRunner commandRunner) securityReviewReport {
	scan := buildSecurityReport(ctx, opts.securityOptions, client, commandRunner)
	report := securityReviewReport{
		Status:   plan.StatusOK,
		Root:     scan.Root,
		Source:   "security-scan",
		Policy:   scan.Policy,
		Warnings: append([]string(nil), scan.Warnings...),
		Error:    scan.Error,
	}
	report.Filters = securityReviewFiltersFromOptions(opts)
	report.Candidates = filterSecurityReviewCandidates(securityReviewCandidatesFromReport(scan), opts)
	report.Summary = securityReviewSummaryFromCandidates(report.Candidates)
	report.Status = securityReviewStatus(scan, report.Candidates)
	return report
}

func securityReviewStatus(scan securityReport, candidates []securityReviewCandidate) plan.Status {
	if scan.Status == plan.StatusError || scan.Error != "" {
		return plan.StatusError
	}
	if len(candidates) > 0 {
		return plan.StatusHeld
	}
	return plan.StatusOK
}

func securityReviewFiltersFromOptions(opts securityReviewOptions) *securityReviewFilters {
	filters := securityReviewFilters{
		Decision: strings.ToLower(strings.TrimSpace(opts.decision)),
		Kind:     strings.TrimSpace(opts.kind),
		Name:     strings.TrimSpace(opts.name),
	}
	if filters.Decision == "" && filters.Kind == "" && filters.Name == "" {
		return nil
	}
	return &filters
}

func securityReviewCandidatesFromReport(report securityReport) []securityReviewCandidate {
	candidates := []securityReviewCandidate{}
	for _, finding := range report.Findings {
		if !securityDecisionNeedsAttention(finding.Decision) {
			continue
		}
		candidates = append(candidates, securityReviewCandidate{
			Provider:    finding.Provider,
			Kind:        "advisory",
			Name:        firstNonEmpty(finding.Package, finding.Name),
			Version:     finding.Version,
			Ecosystem:   finding.Ecosystem,
			Package:     finding.Package,
			Decision:    finding.Decision,
			Reason:      securityFindingReason(finding),
			Remediation: finding.Remediation,
			Source:      finding.VulnID,
			URL:         finding.URL,
		})
	}
	for _, posture := range report.Posture {
		if !securityDecisionNeedsAttention(posture.Decision) {
			continue
		}
		candidates = append(candidates, securityReviewCandidate{
			Provider:    "github-repo",
			Kind:        posture.Provider,
			Name:        firstNonEmpty(posture.Repository, posture.Name),
			Decision:    posture.Decision,
			Reason:      posture.Reason,
			Remediation: posture.Remediation,
			Evidence:    posture.Evidence,
			URL:         posture.URL,
		})
	}
	for _, posture := range report.Brew {
		if !securityDecisionNeedsAttention(posture.Decision) {
			continue
		}
		candidates = append(candidates, securityReviewCandidate{
			Provider:    posture.Provider,
			Kind:        posture.Kind,
			Name:        posture.Name,
			Version:     posture.Version,
			Decision:    posture.Decision,
			Reason:      posture.Reason,
			Remediation: posture.Remediation,
			Evidence:    posture.Evidence,
			URL:         firstNonEmpty(posture.URL, posture.Homepage),
		})
	}
	for _, posture := range report.VSCode {
		if !securityDecisionNeedsAttention(posture.Decision) {
			continue
		}
		candidates = append(candidates, securityReviewCandidate{
			Provider:    posture.Provider,
			Kind:        posture.Kind,
			Name:        posture.Name,
			Version:     posture.Version,
			Decision:    posture.Decision,
			Reason:      posture.Reason,
			Remediation: posture.Remediation,
			Evidence:    posture.Evidence,
			URL:         firstNonEmpty(posture.RepositoryURL, posture.URL),
		})
	}
	for _, posture := range report.NPM {
		candidates = append(candidates, packagePostureReviewCandidate(posture.Provider, posture.Kind, posture.Package, posture.Version, posture.Decision, posture.Reason, posture.Remediation, posture.Evidence, posture.URL))
	}
	for _, posture := range report.Cargo {
		candidates = append(candidates, packagePostureReviewCandidate(posture.Provider, posture.Kind, posture.Crate, posture.Version, posture.Decision, posture.Reason, posture.Remediation, posture.Evidence, posture.URL))
	}
	for _, posture := range report.PyPI {
		candidates = append(candidates, packagePostureReviewCandidate(posture.Provider, posture.Kind, posture.Package, posture.Version, posture.Decision, posture.Reason, posture.Remediation, posture.Evidence, posture.URL))
	}
	for _, audit := range report.Audits {
		if !securityDecisionNeedsAttention(audit.Decision) {
			continue
		}
		candidates = append(candidates, securityReviewCandidate{
			Provider:    audit.Provider,
			Kind:        "native-audit",
			Name:        audit.Tool,
			Decision:    audit.Decision,
			Reason:      audit.Reason,
			Remediation: audit.Error,
			Source:      audit.Target,
		})
	}
	for _, scanner := range report.Scanners {
		for _, finding := range scanner.Findings {
			if !securityDecisionNeedsAttention(finding.Decision) {
				continue
			}
			candidates = append(candidates, securityReviewCandidate{
				Provider:       "scanner",
				Kind:           firstNonEmpty(scanner.Tool, finding.Kind),
				Name:           firstNonEmpty(scannerFindingID(finding), scanner.Tool),
				Version:        finding.Version,
				Ecosystem:      finding.Ecosystem,
				Package:        finding.Package,
				DependencyKind: finding.DependencyKind,
				Decision:       finding.Decision,
				Reason:         finding.Reason,
				Remediation:    finding.Remediation,
				Evidence:       finding.Evidence,
				Source:         scannerFindingSource(finding),
				URL:            finding.URL,
			})
		}
	}
	candidates = filterEmptyReviewCandidates(candidates)
	for index := range candidates {
		candidates[index].Prompt = securityReviewPrompt(candidates[index])
		candidates[index].PolicyCommand = securityReviewPolicyCommand(candidates[index])
	}
	sortSecurityReviewCandidates(candidates)
	return candidates
}

func packagePostureReviewCandidate(provider string, kind string, name string, version string, decision string, reason string, remediation string, evidence []string, url string) securityReviewCandidate {
	if !securityDecisionNeedsAttention(decision) {
		return securityReviewCandidate{}
	}
	return securityReviewCandidate{
		Provider:    provider,
		Kind:        kind,
		Name:        name,
		Version:     version,
		Decision:    decision,
		Reason:      reason,
		Remediation: remediation,
		Evidence:    evidence,
		URL:         url,
	}
}

func filterEmptyReviewCandidates(candidates []securityReviewCandidate) []securityReviewCandidate {
	out := make([]securityReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider == "" || candidate.Name == "" {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func filterSecurityReviewCandidates(candidates []securityReviewCandidate, opts securityReviewOptions) []securityReviewCandidate {
	decision := strings.ToLower(strings.TrimSpace(opts.decision))
	kind := strings.ToLower(strings.TrimSpace(opts.kind))
	name := strings.ToLower(strings.TrimSpace(opts.name))
	if decision == "" && kind == "" && name == "" {
		return candidates
	}
	out := make([]securityReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if decision != "" && strings.ToLower(strings.TrimSpace(candidate.Decision)) != decision {
			continue
		}
		if kind != "" && strings.ToLower(strings.TrimSpace(candidate.Kind)) != kind {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(candidate.Name), name) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func sortSecurityReviewCandidates(candidates []securityReviewCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if securityDecisionPriority(left.Decision) != securityDecisionPriority(right.Decision) {
			return securityDecisionPriority(left.Decision) > securityDecisionPriority(right.Decision)
		}
		return strings.ToLower(strings.Join([]string{left.Provider, left.Kind, left.Name, left.Version}, "\x00")) <
			strings.ToLower(strings.Join([]string{right.Provider, right.Kind, right.Name, right.Version}, "\x00"))
	})
}

func securityReviewSummaryFromCandidates(candidates []securityReviewCandidate) *securityReviewSummary {
	if len(candidates) == 0 {
		return nil
	}
	summary := securityReviewSummary{
		Candidates: len(candidates),
		Decisions:  map[string]int{},
		Providers:  map[string]int{},
	}
	for _, candidate := range candidates {
		decision := strings.ToLower(strings.TrimSpace(candidate.Decision))
		if decision != "" {
			summary.Decisions[decision]++
		}
		provider := strings.ToLower(strings.TrimSpace(candidate.Provider))
		if provider != "" {
			summary.Providers[provider]++
		}
	}
	return &summary
}

func countMapSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func securityReviewPrompt(candidate securityReviewCandidate) string {
	parts := []string{
		fmt.Sprintf("Review updev security candidate %s/%s %s", candidate.Provider, candidate.Kind, candidate.Name),
	}
	if candidate.Version != "" {
		parts = append(parts, "version "+candidate.Version)
	}
	if candidate.Ecosystem != "" {
		parts = append(parts, "ecosystem "+candidate.Ecosystem)
	}
	if candidate.Package != "" {
		parts = append(parts, "package "+candidate.Package)
	}
	if candidate.DependencyKind != "" {
		parts = append(parts, "dependency role "+candidate.DependencyKind)
	}
	if candidate.Decision != "" {
		parts = append(parts, "current decision "+candidate.Decision)
	}
	if candidate.Reason != "" {
		parts = append(parts, "reason: "+candidate.Reason)
	}
	if candidate.Remediation != "" {
		parts = append(parts, "remediation: "+candidate.Remediation)
	}
	if candidate.Source != "" {
		parts = append(parts, "source: "+candidate.Source)
	}
	if candidate.URL != "" {
		parts = append(parts, "url: "+candidate.URL)
	}
	if len(candidate.Evidence) > 0 {
		parts = append(parts, "evidence: "+oneLine(strings.Join(candidate.Evidence, "; ")))
	}
	parts = append(parts, "Check upstream provenance, advisory status, maintainer/release health, and recommend allow/review/hold/block with a reason and expiry if allowing temporarily.")
	return strings.Join(parts, ". ")
}

func securityReviewPolicyCommand(candidate securityReviewCandidate) string {
	provider, kind, ok := securityReviewPolicyTarget(candidate)
	if !ok || !validSecurityPolicyDecision(candidate.Decision) {
		return ""
	}
	command := []string{"updev", "security", "policy", candidate.Decision, "--provider", provider}
	if kind != "" {
		command = append(command, "--kind", kind)
	}
	command = append(command, "--name", candidate.Name, "--reason", firstNonEmpty(candidate.Reason, "reviewed security candidate"))
	if securityReviewPolicyCommandNeedsTTL(candidate.Decision) {
		command = append(command, "--ttl-days", "30")
	}
	return joinCommand(command)
}

func securityReviewPolicyCommandNeedsTTL(decision string) bool {
	return securityPolicyDecisionNeedsExpiry(decision)
}

func securityReviewPolicyTarget(candidate securityReviewCandidate) (string, string, bool) {
	provider := strings.ToLower(strings.TrimSpace(candidate.Provider))
	kind := strings.ToLower(strings.TrimSpace(candidate.Kind))
	switch {
	case provider == "" || candidate.Name == "":
		return "", "", false
	case kind == "native-audit":
		return "", "", false
	case kind == "advisory":
		if provider := securityPolicyProviderForEcosystem(candidate.Ecosystem); provider != "" {
			return provider, "", true
		}
		return provider, "", true
	case provider == "github-repo":
		return provider, "", true
	case provider == "scanner":
		return provider, kind, true
	case kind == "npm":
		return "npm", "", true
	case kind == "cargo":
		return "cargo", "", true
	case kind == "pipx":
		return "pypi", "", true
	default:
		return provider, kind, true
	}
}

func printSecurityReviewText(w io.Writer, report securityReviewReport) {
	fmt.Fprintf(w, "status: %s\n", report.Status)
	fmt.Fprintf(w, "root: %s\n", report.Root)
	if report.Policy != nil && report.Policy.Path != "" {
		fmt.Fprintf(w, "policy: %s (%s)\n", report.Policy.Path, securityPolicyUseSummary(report.Policy))
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
	if report.Error != "" {
		fmt.Fprintf(w, "error: %s\n", report.Error)
	}
	if report.Filters != nil {
		if filterSummary := securityReviewFilterSummary(*report.Filters); filterSummary != "" {
			fmt.Fprintf(w, "filters: %s\n", filterSummary)
		}
	}
	fmt.Fprintf(w, "review candidates: %d\n", len(report.Candidates))
	if report.Summary != nil {
		if decisionSummary := countMapSummary(report.Summary.Decisions); decisionSummary != "" {
			fmt.Fprintf(w, "decisions: %s\n", decisionSummary)
		}
		if providerSummary := countMapSummary(report.Summary.Providers); providerSummary != "" {
			fmt.Fprintf(w, "providers: %s\n", providerSummary)
		}
	}
	for index, candidate := range report.Candidates {
		fmt.Fprintf(w, "\n%d. [%s] %s/%s %s", index+1, candidate.Decision, candidate.Provider, candidate.Kind, candidate.Name)
		if candidate.Version != "" {
			fmt.Fprintf(w, " %s", candidate.Version)
		}
		fmt.Fprintln(w)
		if candidate.Reason != "" {
			fmt.Fprintf(w, "   reason: %s\n", localizedSecurityReason(candidate.Reason))
		}
		if candidate.Ecosystem != "" || candidate.Package != "" || candidate.DependencyKind != "" {
			fmt.Fprintf(w, "   package: %s\n", securityReviewPackageContext(candidate))
		}
		if candidate.Remediation != "" {
			fmt.Fprintf(w, "   remediation: %s\n", localizedSecurityRemediation(candidate.Remediation))
		}
		if candidate.Source != "" {
			fmt.Fprintf(w, "   source: %s\n", candidate.Source)
		}
		if candidate.URL != "" {
			fmt.Fprintf(w, "   url: %s\n", candidate.URL)
		}
		if len(candidate.Evidence) > 0 {
			fmt.Fprintf(w, "   evidence: %s\n", truncate(oneLine(strings.Join(candidate.Evidence, "; ")), 180))
		}
		if candidate.PolicyCommand != "" {
			fmt.Fprintf(w, "   policy: %s\n", candidate.PolicyCommand)
		}
		fmt.Fprintf(w, "   prompt: %s\n", candidate.Prompt)
	}
}

func securityReviewFilterSummary(filters securityReviewFilters) string {
	parts := []string{}
	if filters.Decision != "" {
		parts = append(parts, "decision="+filters.Decision)
	}
	if filters.Kind != "" {
		parts = append(parts, "kind="+filters.Kind)
	}
	if filters.Name != "" {
		parts = append(parts, "name="+filters.Name)
	}
	return strings.Join(parts, ", ")
}

func securityReviewPackageContext(candidate securityReviewCandidate) string {
	parts := []string{}
	if candidate.Ecosystem != "" {
		parts = append(parts, candidate.Ecosystem)
	}
	if candidate.Package != "" {
		parts = append(parts, candidate.Package)
	}
	if candidate.DependencyKind != "" {
		parts = append(parts, candidate.DependencyKind)
	}
	return strings.Join(parts, " ")
}
