package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securitypolicy"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type securityPolicy = securitypolicy.Policy

type securityPolicyLoadResult struct {
	Policy   securityPolicy
	Path     string
	Loaded   bool
	Warnings []string
}

type securityPolicyRule = securitypolicy.Rule

func loadSecurityPolicy() securityPolicy {
	path := securityPolicyPath()
	if path == "" {
		return securityPolicy{}
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return securityPolicy{}
	}
	return policy
}

func loadSecurityPolicyForReport() securityPolicyLoadResult {
	return loadSecurityPolicyForReportPath(securityPolicyPath())
}

func securityPolicyPathFromOption(path string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return securityPolicyPath()
}

func loadSecurityPolicyForReportPath(path string) securityPolicyLoadResult {
	path = securityPolicyPathFromOption(path)
	if path == "" {
		return securityPolicyLoadResult{}
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return securityPolicyLoadResult{
			Path:     path,
			Warnings: []string{"security policy ignored: " + err.Error()},
		}
	}
	return securityPolicyLoadResult{
		Policy:   policy,
		Path:     path,
		Loaded:   len(policy.Rules) > 0,
		Warnings: securityPolicyDiagnosticWarnings(policy),
	}
}

func (result securityPolicyLoadResult) View() *securityPolicyUse {
	if result.Path == "" {
		return nil
	}
	summary := securityPolicyRuleSummary(result.Policy)
	view := securityPolicyUse{
		Path:            result.Path,
		Loaded:          result.Loaded,
		RuleCount:       len(result.Policy.Rules),
		ActiveRules:     summary.Active,
		ExpiredRules:    summary.Expired,
		InvalidRules:    summary.Invalid,
		DuplicateRules:  summary.Duplicate,
		ShadowedRules:   summary.Shadowed,
		MissingReasons:  summary.MissingReason,
		MissingExpiries: summary.MissingExpiry,
		BroadRules:      summary.Broad,
	}
	if len(result.Warnings) > 0 && strings.HasPrefix(result.Warnings[0], "security policy ignored: ") {
		view.Error = strings.TrimPrefix(result.Warnings[0], "security policy ignored: ")
	}
	return &view
}

func securityPolicyUseSummary(policy *securityPolicyUse) string {
	if policy == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("%d rules", policy.RuleCount),
		fmt.Sprintf("%d active", policy.ActiveRules),
	}
	if policy.ExpiredRules > 0 {
		parts = append(parts, fmt.Sprintf("%d expired", policy.ExpiredRules))
	}
	if policy.InvalidRules > 0 {
		parts = append(parts, fmt.Sprintf("%d invalid", policy.InvalidRules))
	}
	if policy.DuplicateRules > 0 {
		parts = append(parts, fmt.Sprintf("%d duplicate", policy.DuplicateRules))
	}
	if policy.ShadowedRules > 0 {
		parts = append(parts, fmt.Sprintf("%d shadowed", policy.ShadowedRules))
	}
	if policy.MissingReasons > 0 {
		parts = append(parts, fmt.Sprintf("%d missing reason", policy.MissingReasons))
	}
	if policy.MissingExpiries > 0 {
		parts = append(parts, fmt.Sprintf("%d missing expiry", policy.MissingExpiries))
	}
	if policy.BroadRules > 0 {
		parts = append(parts, fmt.Sprintf("%d broad", policy.BroadRules))
	}
	return strings.Join(parts, ", ")
}

type securityPolicyRuleCounts = securitypolicy.RuleCounts

func securityPolicyRuleViews(policy securityPolicy) []securityPolicyRuleView {
	views := securitypolicy.RuleViews(policy)
	for index := range views {
		view := views[index]
		view.Remediation = securityPolicyRuleRemediation(view)
		views[index] = view
	}
	return views
}

func securityPolicyDecisionNeedsExpiry(decision string) bool {
	return securitypolicy.DecisionNeedsExpiry(decision)
}

func validSecurityPolicyStateFilter(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "active", "expired", "invalid", "duplicate", "shadowed", "needs-cleanup":
		return true
	default:
		return false
	}
}

func securityPolicyRuleMatchesStateFilter(view securityPolicyRuleView, state string) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		return true
	}
	if state == "needs-cleanup" {
		return securityPolicyRuleNeedsCleanup(view)
	}
	return strings.EqualFold(view.State, state)
}

func securityPolicyStateFilterForAction(opts securityPolicyOptions) string {
	state := opts.state
	if opts.action == "cleanup" && state == "" {
		return "needs-cleanup"
	}
	return state
}

func securityPolicyRuleNeedsCleanup(view securityPolicyRuleView) bool {
	return securitypolicy.RuleNeedsCleanup(view)
}

func securityPolicyRuleMatchesListFilters(view securityPolicyRuleView, opts securityPolicyOptions) bool {
	if opts.set == nil {
		return true
	}
	if opts.set["provider"] && !securityPolicyFieldFilterMatches(view.Provider, opts.rule.Provider) {
		return false
	}
	if opts.set["kind"] && !securityPolicyFieldFilterMatches(view.Kind, opts.rule.Kind) {
		return false
	}
	if opts.set["name"] && !securityPolicyNameFilterMatches(view.Name, opts.rule.Name) {
		return false
	}
	if opts.set["decision"] && !securityPolicyFieldFilterMatches(view.Decision, opts.rule.Decision) {
		return false
	}
	return true
}

func securityPolicyRuleRemediation(view securityPolicyRuleView) string {
	actions := []string{}
	switch {
	case view.Invalid:
		actions = append(actions, fmt.Sprintf("update or remove this invalid policy rule with --index %d", view.Index))
	case view.Expired:
		actions = append(actions, fmt.Sprintf("remove the expired rule or renew it with updev security policy renew --index %d --ttl-days 30 after review", view.Index))
	case view.Duplicate:
		actions = append(actions, fmt.Sprintf("remove the duplicate rule with updev security policy remove --index %d; an earlier active rule has the same provider, kind, and name", view.Index))
	case view.Shadowed:
		actions = append(actions, fmt.Sprintf("remove or narrow this rule with --index %d; rule #%d already covers it", view.Index, view.ShadowedBy))
	}
	if view.MissingReason {
		actions = append(actions, fmt.Sprintf("add a review reason with updev security policy update --index %d --reason text", view.Index))
	}
	if view.MissingExpiry {
		actions = append(actions, fmt.Sprintf("add an expiry with updev security policy renew --index %d --ttl-days 30", view.Index))
	}
	if view.Broad {
		actions = append(actions, "narrow the rule with --provider, --kind, or --name before keeping it long term")
	}
	return strings.Join(actions, "; ")
}

func securityPolicyFieldFilterMatches(value string, filter string) bool {
	return strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(filter))
}

func securityPolicyNameFilterMatches(value string, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "*" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value), filter)
}

func securityPolicyRuleSummary(policy securityPolicy) securityPolicyRuleCounts {
	return securitypolicy.RuleCountsForPolicy(policy)
}

func securityPolicySummaryFromViews(views []securityPolicyRuleView) *securityPolicySummary {
	return securitypolicy.SummaryFromViews(views)
}

func securityPolicyDiagnosticWarnings(policy securityPolicy) []string {
	summary := securityPolicyRuleSummary(policy)
	warnings := []string{}
	if summary.Invalid > 0 {
		warnings = append(warnings, "security policy has invalid rules; run updev security policy for details")
	}
	if summary.Duplicate > 0 {
		warnings = append(warnings, "security policy has duplicate active rules; run updev security policy for details")
	}
	if summary.Shadowed > 0 {
		warnings = append(warnings, "security policy has shadowed rules; run updev security policy for details")
	}
	if summary.MissingReason > 0 {
		warnings = append(warnings, "security policy has allow rules without reason; run updev security policy for details")
	}
	if summary.MissingExpiry > 0 {
		warnings = append(warnings, "security policy has temporary rules without expiry; run updev security policy for details")
	}
	if summary.Broad > 0 {
		warnings = append(warnings, "security policy has broad active rules; run updev security policy for details")
	}
	return warnings
}

func readSecurityPolicy(path string) (securityPolicy, error) {
	return securitypolicy.Read(path)
}

func addSecurityPolicyRule(path string, rule securityPolicyRule) error {
	path = securityPolicyPathFromOption(path)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("security policy path is required")
	}
	rule = normalizeSecurityPolicyRule(rule)
	if err := validateSecurityPolicyRuleForAdd(rule); err != nil {
		return err
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return err
	}
	policy.Rules = append(policy.Rules, rule)
	return writeSecurityPolicy(path, policy)
}

func removeSecurityPolicyRule(path string, index int) error {
	path = securityPolicyPathFromOption(path)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("security policy path is required")
	}
	if index <= 0 {
		return fmt.Errorf("--index must be a positive 1-based rule index")
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return err
	}
	if index > len(policy.Rules) {
		return fmt.Errorf("--index %d is out of range; policy has %d rules", index, len(policy.Rules))
	}
	policy.Rules = append(policy.Rules[:index-1], policy.Rules[index:]...)
	return writeSecurityPolicy(path, policy)
}

func cleanupSecurityPolicyRules(opts securityPolicyOptions) ([]securityPolicyCleanup, error) {
	path := securityPolicyPathFromOption(opts.path)
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("security policy path is required")
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return nil, err
	}
	plan := securityPolicyFilteredCleanupPlan(path, securityPolicyRuleViews(policy), opts, opts.apply)
	if !opts.apply || len(plan) == 0 {
		return plan, nil
	}
	indexes := make([]int, 0, len(plan))
	for _, action := range plan {
		indexes = append(indexes, action.Index)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indexes)))
	for _, index := range indexes {
		if index <= 0 || index > len(policy.Rules) {
			return nil, fmt.Errorf("--index %d is out of range; policy has %d rules", index, len(policy.Rules))
		}
		policy.Rules = append(policy.Rules[:index-1], policy.Rules[index:]...)
	}
	return plan, writeSecurityPolicy(path, policy)
}

func securityPolicyFilteredCleanupPlan(path string, views []securityPolicyRuleView, opts securityPolicyOptions, applied bool) []securityPolicyCleanup {
	filtered := make([]securityPolicyRuleView, 0, len(views))
	for _, view := range views {
		if !securityPolicyRuleMatchesStateFilter(view, securityPolicyStateFilterForAction(opts)) {
			continue
		}
		if !securityPolicyRuleMatchesListFilters(view, opts) {
			continue
		}
		filtered = append(filtered, view)
	}
	return securityPolicyCleanupPlan(path, filtered, applied)
}

func securityPolicyCleanupPlan(path string, views []securityPolicyRuleView, applied bool) []securityPolicyCleanup {
	plan := []securityPolicyCleanup{}
	for _, view := range views {
		action, ok := securityPolicyCleanupForRule(path, view, applied)
		if ok {
			plan = append(plan, action)
		}
	}
	return plan
}

func securityPolicyCleanupForRule(path string, view securityPolicyRuleView, applied bool) (securityPolicyCleanup, bool) {
	reason := ""
	switch {
	case view.Expired:
		reason = "expired rule no longer applies"
	case view.Duplicate:
		reason = "duplicate rule is covered by an earlier active rule"
	case view.Shadowed:
		reason = fmt.Sprintf("shadowed rule is covered by rule #%d", view.ShadowedBy)
	default:
		return securityPolicyCleanup{}, false
	}
	command := []string{"updev", "security", "policy", "remove", "--index", strconv.Itoa(view.Index)}
	if strings.TrimSpace(path) != "" {
		command = append(command, "--path", path)
	}
	return securityPolicyCleanup{
		Action:   "remove",
		Index:    view.Index,
		Provider: view.Provider,
		Kind:     view.Kind,
		Name:     view.Name,
		Decision: view.Decision,
		State:    view.State,
		Reason:   reason,
		Command:  joinCommand(command),
		Applied:  applied,
	}, true
}

func updateSecurityPolicyRule(path string, index int, patch securityPolicyRule, set map[string]bool) error {
	path = securityPolicyPathFromOption(path)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("security policy path is required")
	}
	if index <= 0 {
		return fmt.Errorf("--index must be a positive 1-based rule index")
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		return err
	}
	if index > len(policy.Rules) {
		return fmt.Errorf("--index %d is out of range; policy has %d rules", index, len(policy.Rules))
	}
	rule := policy.Rules[index-1]
	if set["provider"] {
		rule.Provider = patch.Provider
	}
	if set["kind"] {
		rule.Kind = patch.Kind
	}
	if set["name"] {
		rule.Name = patch.Name
	}
	if set["decision"] {
		rule.Decision = patch.Decision
	}
	if set["reason"] {
		rule.Reason = patch.Reason
	}
	if set["expires"] {
		rule.Expires = patch.Expires
	}
	rule = normalizeSecurityPolicyRule(rule)
	if err := validateSecurityPolicyRuleForAdd(rule); err != nil {
		return err
	}
	policy.Rules[index-1] = rule
	return writeSecurityPolicy(path, policy)
}

func validateSecurityPolicyRuleForAdd(rule securityPolicyRule) error {
	if rule.Name == "" {
		return fmt.Errorf("--name is required")
	}
	if !validSecurityPolicyDecision(rule.Decision) {
		return fmt.Errorf("--decision must be allow, review, hold, or block")
	}
	if rule.Reason == "" {
		return fmt.Errorf("--reason is required")
	}
	if rule.Decision == "allow" && rule.Expires == "" {
		return fmt.Errorf("--expires is required for allow rules")
	}
	if _, invalid := securityPolicyRuleExpiryState(rule); invalid {
		return fmt.Errorf("--expires must be YYYY-MM-DD")
	}
	return nil
}

func writeSecurityPolicy(path string, policy securityPolicy) error {
	return securitypolicy.Write(path, policy)
}

func securityPolicyPath() string {
	return updevpath.SecurityPolicyFile()
}

func applySecurityPolicyToSafetyFindings(policy securityPolicy, findings []safetyFinding) []safetyFinding {
	if len(policy.Rules) == 0 || len(findings) == 0 {
		return findings
	}
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if rule, ok := matchingSecurityPolicyRule(policy, finding.Provider, finding.Kind, finding.Name); ok {
			finding.Decision = rule.Decision
			setSafetyFindingReason(&finding, securityreason.SecurityPolicyOverrideReason(rule.Decision, firstNonEmpty(rule.Reason, "security policy override")))
			finding.Confidence = "policy"
			if rule.Decision == "allow" {
				finding.Remediation = ""
			} else {
				finding.Remediation = "follow the local security policy before updating"
			}
			finding.Evidence = appendEvidence(finding.Evidence, "security-policy")
		}
		out = append(out, finding)
	}
	return out
}

func applySecurityPolicyToFindings(policy securityPolicy, findings []securityFinding) []securityFinding {
	if len(policy.Rules) == 0 || len(findings) == 0 {
		return findings
	}
	out := make([]securityFinding, 0, len(findings))
	for _, finding := range findings {
		rule, ok := matchingSecurityPolicyRule(policy, finding.Provider, "", finding.Name)
		if !ok {
			rule, ok = matchingSecurityPolicyRule(policy, securityPolicyProviderForEcosystem(finding.Ecosystem), "", finding.Package)
		}
		if ok {
			finding.Decision = rule.Decision
			finding.Reason = firstNonEmpty(rule.Reason, "security policy override")
			finding.Confidence = "policy"
			finding.Status = securityStatusFromPolicyFindingDecision(rule.Decision)
			if rule.Decision == "allow" {
				finding.Remediation = ""
			} else {
				finding.Remediation = "follow the local security policy before changing this package"
			}
		}
		out = append(out, finding)
	}
	return out
}

func applySecurityPolicyToScanners(policy securityPolicy, scanners []scannerEvidence) []scannerEvidence {
	if len(policy.Rules) == 0 || len(scanners) == 0 {
		return scanners
	}
	out := make([]scannerEvidence, 0, len(scanners))
	for _, scanner := range scanners {
		if len(scanner.Findings) == 0 {
			out = append(out, scanner)
			continue
		}
		findings := make([]scannerFinding, 0, len(scanner.Findings))
		for _, finding := range scanner.Findings {
			if rule, ok := matchingScannerPolicyRule(policy, scanner.Tool, finding); ok {
				finding.Decision = rule.Decision
				finding.Reason = firstNonEmpty(rule.Reason, "security policy override")
				reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, finding.Reason)
				finding.ReasonCode = reason.Code
				finding.ReasonArgs = reason.Args
				if rule.Decision == "allow" {
					finding.Remediation = ""
				} else {
					finding.Remediation = "follow the local security policy before changing this scanner finding"
				}
				finding.Confidence = "policy"
				finding.Evidence = appendEvidence(finding.Evidence, "security-policy")
			}
			findings = append(findings, finding)
		}
		scanner.Findings = findings
		out = append(out, scannerEvidenceWithPolicyDecision(scanner))
	}
	return out
}

func matchingScannerPolicyRule(policy securityPolicy, tool string, finding scannerFinding) (securityPolicyRule, bool) {
	ids := []string{scannerFindingID(finding)}
	if finding.Fingerprint != "" && finding.Fingerprint != ids[0] {
		ids = append(ids, finding.Fingerprint)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		for _, candidate := range []struct {
			provider string
			kind     string
		}{
			{provider: "scanner", kind: tool},
			{provider: tool, kind: finding.Kind},
			{provider: "scanner", kind: finding.Kind},
		} {
			if rule, ok := matchingSecurityPolicyRule(policy, candidate.provider, candidate.kind, id); ok {
				return rule, true
			}
		}
	}
	return securityPolicyRule{}, false
}

func applySecurityPolicyToGitHubPostures(policy securityPolicy, postures []githubPosture) []githubPosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]githubPosture, 0, len(postures))
	for _, posture := range postures {
		rule, ok := matchingSecurityPolicyRule(policy, "github-repo", "", posture.Repository)
		if !ok {
			rule, ok = matchingSecurityPolicyRule(policy, posture.Provider, "", posture.Name)
		}
		if ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "repository")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func applySecurityPolicyToHomebrewPostures(policy securityPolicy, postures []homebrewPosture) []homebrewPosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]homebrewPosture, 0, len(postures))
	for _, posture := range postures {
		if rule, ok := matchingSecurityPolicyRule(policy, posture.Provider, posture.Kind, posture.Name); ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "Homebrew entry")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func applySecurityPolicyToVSCodePostures(policy securityPolicy, postures []vscodePosture) []vscodePosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]vscodePosture, 0, len(postures))
	for _, posture := range postures {
		if rule, ok := matchingSecurityPolicyRule(policy, posture.Provider, posture.Kind, posture.Name); ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "VS Code extension")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func applySecurityPolicyToNPMPostures(policy securityPolicy, postures []npmPosture) []npmPosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]npmPosture, 0, len(postures))
	for _, posture := range postures {
		rule, ok := matchingSecurityPolicyRule(policy, posture.Provider, posture.Kind, posture.Name)
		if !ok {
			rule, ok = matchingSecurityPolicyRule(policy, "npm", "", posture.Package)
		}
		if ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "npm package")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func applySecurityPolicyToCargoPostures(policy securityPolicy, postures []cargoPosture) []cargoPosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]cargoPosture, 0, len(postures))
	for _, posture := range postures {
		rule, ok := matchingSecurityPolicyRule(policy, posture.Provider, posture.Kind, posture.Name)
		if !ok {
			rule, ok = matchingSecurityPolicyRule(policy, "cargo", "", posture.Crate)
		}
		if ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "Cargo crate")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func applySecurityPolicyToPyPIPostures(policy securityPolicy, postures []pypiPosture) []pypiPosture {
	if len(policy.Rules) == 0 || len(postures) == 0 {
		return postures
	}
	out := make([]pypiPosture, 0, len(postures))
	for _, posture := range postures {
		rule, ok := matchingSecurityPolicyRule(policy, posture.Provider, posture.Kind, posture.Name)
		if !ok {
			rule, ok = matchingSecurityPolicyRule(policy, "pypi", "", posture.Package)
		}
		if ok {
			posture.Decision = rule.Decision
			posture.Reason = firstNonEmpty(rule.Reason, "security policy override")
			reason := securityreason.SecurityPolicyOverrideReason(rule.Decision, posture.Reason)
			posture.ReasonCode = reason.Code
			posture.ReasonArgs = reason.Args
			posture.Confidence = "policy"
			posture.Remediation = securityPolicyPostureRemediation(rule.Decision, "PyPI package")
			posture.Evidence = appendEvidence(posture.Evidence, "security-policy")
		}
		out = append(out, posture)
	}
	return out
}

func securityPolicyPostureRemediation(decision string, target string) string {
	if strings.EqualFold(strings.TrimSpace(decision), "allow") {
		return ""
	}
	return "follow the local security policy before changing this " + target
}

func matchingSecurityPolicyRule(policy securityPolicy, provider string, kind string, name string) (securityPolicyRule, bool) {
	return securitypolicy.MatchingRule(policy, provider, kind, name)
}

func normalizeSecurityPolicyRule(rule securityPolicyRule) securityPolicyRule {
	return securitypolicy.NormalizeRule(rule)
}

func validSecurityPolicyDecision(decision string) bool {
	return securitygate.ValidDecision(decision)
}

func securityPolicyDecisionAction(action string) bool {
	return validSecurityPolicyDecision(strings.ToLower(strings.TrimSpace(action)))
}

func securityDecisionNeedsAttention(decision string) bool {
	return securitygate.DecisionNeedsAttention(decision)
}

func securityPolicyProviderForEcosystem(ecosystem string) string {
	return securitypolicy.ProviderForEcosystem(ecosystem)
}

func securityStatusFromPolicyFindingDecision(decision string) plan.Status {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "block":
		return plan.StatusBlocked
	case "hold", "review":
		return plan.StatusHeld
	default:
		return plan.StatusOK
	}
}

func securityPolicyRuleExpiryState(rule securityPolicyRule) (bool, bool) {
	return securitypolicy.RuleExpiryState(rule)
}
