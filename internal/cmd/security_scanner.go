package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/securityscanner"
)

type (
	scannerEvidence = securityscanner.Evidence
	scannerFinding  = securityscanner.Finding
)

type (
	osvScannerReport      = securityscanner.OSVReport
	osvScannerResult      = securityscanner.OSVResult
	osvScannerSource      = securityscanner.OSVSource
	osvScannerPackage     = securityscanner.OSVPackage
	osvScannerPackageInfo = securityscanner.PackageInfo
	osvScannerVuln        = securityscanner.OSVVuln
	osvScannerGroup       = securityscanner.OSVGroup
	gitleaksFinding       = securityscanner.GitleaksFinding
	zizmorFinding         = securityscanner.ZizmorFinding
	zizmorLocation        = securityscanner.ZizmorLocation
	trivyReport           = securityscanner.TrivyReport
	trivyResult           = securityscanner.TrivyResult
	trivyVulnerability    = securityscanner.TrivyVulnerability
	trivyMisconfiguration = securityscanner.TrivyMisconfig
	trivyCauseMetadata    = securityscanner.TrivyCauseMetadata
	trivySecret           = securityscanner.TrivySecret
	grypeReport           = securityscanner.GrypeReport
	grypeMatch            = securityscanner.GrypeMatch
	grypeVulnerability    = securityscanner.GrypeVulnerability
	grypeFix              = securityscanner.GrypeFix
	grypeArtifact         = securityscanner.GrypeArtifact
	grypeLocation         = securityscanner.GrypeLocation
	grypeMatchDetail      = securityscanner.GrypeMatchDetail
)

func scannerEvidenceFromOptions(ctx context.Context, commandRunner commandRunner, opts securityOptions, desired []securityPackage) []scannerEvidence {
	if !securityScannersShouldRun(opts) {
		return nil
	}
	tools := securityScannerTools(opts.scanner, opts.root)
	scanners := make([]scannerEvidence, len(tools))
	var wg sync.WaitGroup
	for index, tool := range tools {
		wg.Add(1)
		go func(index int, tool string) {
			defer wg.Done()
			scanners[index] = scannerEvidenceForTool(ctx, commandRunner, opts, desired, tool)
		}(index, tool)
	}
	wg.Wait()
	return scanners
}

func scannerEvidenceForTool(ctx context.Context, commandRunner commandRunner, opts securityOptions, desired []securityPackage, tool string) scannerEvidence {
	switch tool {
	case "osv-scanner":
		return runOSVScannerSourceScan(ctx, commandRunner, opts.root, desired)
	case "gitleaks":
		return runGitleaksDirScan(ctx, commandRunner, opts.root)
	case "zizmor":
		return runZizmorWorkflowScan(ctx, commandRunner, opts.root)
	case "trivy":
		return runTrivyFilesystemScan(ctx, commandRunner, opts.root, desired)
	case "grype":
		return runGrypeDirectoryScan(ctx, commandRunner, opts.root, desired)
	}
	return scannerEvidence{}
}

func securityScannersShouldRun(opts securityOptions) bool {
	return opts.ecosystem == "" && securityScanIncludesProjectProvider(opts.provider)
}

func validateSecurityScannerOption(value string) error {
	_, err := parseSecurityScannerNames(value)
	return err
}

func securityScannerTools(value string, root string) []string {
	return securityscanner.Tools(value, root)
}

func parseSecurityScannerNames(value string) ([]string, error) {
	return securityscanner.ParseNames(value)
}

func normalizeSecurityScannerName(value string) string {
	return securityscanner.NormalizeName(value)
}

func securityScannerNameAllowed(name string) bool {
	return securityscanner.NameAllowed(name)
}

func runOSVScannerSourceScan(ctx context.Context, commandRunner commandRunner, root string, desired []securityPackage) scannerEvidence {
	args := []string{"scan", "source", "--format=json", "--verbosity=error", "--recursive", root}
	evidence := scannerEvidence{
		Tool:     "osv-scanner",
		Target:   root,
		Command:  append([]string{"osv-scanner"}, args...),
		Status:   plan.StatusOK,
		Decision: "allow",
		Reason:   "osv-scanner source scan completed",
	}
	result := commandRunner.Run(ctx, "osv-scanner", args...)
	parsed, ok := parseOSVScannerReport(result.Stdout)
	if ok {
		evidence = osvScannerEvidenceFromReport(evidence, parsed, desired)
		if evidence.VulnerabilityCount > 0 {
			return evidence
		}
		if result.Code != 0 || result.Err != nil {
			evidence.Status = scannerCommandErrorStatus(result)
			evidence.Decision = "review"
			evidence.Reason = "osv-scanner completed with no parsed vulnerabilities but returned an error"
			evidence.Error = firstNonEmpty(result.Stderr, scannerErrorText(result))
		}
		return evidence
	}
	if result.Code != 0 || result.Err != nil {
		evidence.Status = scannerCommandErrorStatus(result)
		evidence.Decision = "review"
		evidence.Reason = "osv-scanner unavailable"
		evidence.Error = firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result))
		return evidence
	}
	return evidence
}

func parseOSVScannerReport(raw string) (osvScannerReport, bool) {
	return securityscanner.ParseOSVReport(raw)
}

func osvScannerEvidenceFromReport(evidence scannerEvidence, report osvScannerReport, desired []securityPackage) scannerEvidence {
	evidence.SourceCount = len(report.Results)
	findings := []scannerFinding{}
	for _, result := range report.Results {
		for _, pkg := range result.Packages {
			evidence.PackageCount++
			for _, vuln := range pkg.Vulnerabilities {
				if vuln.ID == "" {
					continue
				}
				fixedVersions := fixedVersionsFromOSVScannerVuln(vuln, pkg.Package)
				dependencyKind := scannerDependencyKind(pkg.Package, desired)
				evidenceValues := []string{"osv-scanner"}
				if dependencyKind != "" {
					evidenceValues = append(evidenceValues, dependencyKind+"-dependency")
				}
				if source := strings.TrimSpace(result.Source.Type); source != "" {
					evidenceValues = append(evidenceValues, "source-type:"+source)
				}
				reason := scannerVulnerabilitySecurityReason("osv-scanner", dependencyKind)
				findings = append(findings, scannerFindingWithReason(scannerFinding{
					Kind:           "vulnerability",
					SourcePath:     result.Source.Path,
					SourceType:     result.Source.Type,
					Ecosystem:      pkg.Package.Ecosystem,
					Package:        pkg.Package.Name,
					Version:        pkg.Package.Version,
					DependencyKind: dependencyKind,
					VulnID:         vuln.ID,
					Aliases:        vuln.Aliases,
					Severity:       osvScannerVulnSeverity(vuln, pkg.Groups),
					Decision:       "hold",
					Remediation:    osvScannerRemediation(fixedVersions, pkg.Package, result.Source),
					Confidence:     "high",
					Evidence:       evidenceValues,
					FixedVersions:  fixedVersions,
				}, reason))
			}
		}
	}
	evidence.FindingCount = len(findings)
	evidence.VulnerabilityCount = len(findings)
	if len(findings) > 0 {
		sortScannerFindings(findings)
		evidence.Status = plan.StatusHeld
		evidence.Decision = "hold"
		evidence.Reason = "osv-scanner reported vulnerabilities"
		evidence.Findings = findings
	}
	return evidence
}

func fixedVersionsFromOSVScannerVuln(vuln osvScannerVuln, pkg osvScannerPackageInfo) []string {
	return fixedVersionsFromOSVDetail(osvVulnDetail{
		ID:       vuln.ID,
		Aliases:  vuln.Aliases,
		Severity: osvSeverityFromScanner(vuln.Severity),
		Affected: osvAffectedFromScanner(vuln.Affected),
	}, securityPackage{
		Ecosystem: pkg.Ecosystem,
		Package:   pkg.Name,
	})
}

func osvSeverityFromScanner(values []securityscanner.OSVSeverity) []osvSeverity {
	if len(values) == 0 {
		return nil
	}
	converted := make([]osvSeverity, 0, len(values))
	for _, value := range values {
		converted = append(converted, osvSeverity{
			Type:  value.Type,
			Score: value.Score,
		})
	}
	return converted
}

func osvAffectedFromScanner(values []securityscanner.OSVAffected) []osvAffected {
	if len(values) == 0 {
		return nil
	}
	converted := make([]osvAffected, 0, len(values))
	for _, value := range values {
		affected := osvAffected{}
		affected.Package.Ecosystem = value.Package.Ecosystem
		affected.Package.Name = value.Package.Name
		for _, sourceRange := range value.Ranges {
			targetRange := osvRange{}
			for _, sourceEvent := range sourceRange.Events {
				targetRange.Events = append(targetRange.Events, osvRangeEvent{
					Fixed: sourceEvent.Fixed,
				})
			}
			affected.Ranges = append(affected.Ranges, targetRange)
		}
		converted = append(converted, affected)
	}
	return converted
}

func scannerDependencyKind(pkg osvScannerPackageInfo, desired []securityPackage) string {
	if len(desired) == 0 || pkg.Name == "" {
		return ""
	}
	for _, candidate := range desired {
		if !strings.EqualFold(candidate.Ecosystem, pkg.Ecosystem) {
			continue
		}
		if !strings.EqualFold(candidate.Package, pkg.Name) {
			continue
		}
		if candidate.Version != "" && pkg.Version != "" && candidate.Version != pkg.Version {
			continue
		}
		return "direct"
	}
	return "transitive"
}

func scannerDependencyKindForPackage(pkg osvScannerPackageInfo, desired []securityPackage) string {
	if pkg.Ecosystem == "" || pkg.Name == "" {
		return ""
	}
	return scannerDependencyKind(pkg, desired)
}

func scannerVulnerabilitySecurityReason(tool string, dependencyKind string) securityreason.Reason {
	return securityreason.ScannerVulnerabilityReason(tool, dependencyKind)
}

func scannerSecurityFindingReason(code string, tool string, text string) securityreason.Reason {
	return securityreason.ScannerFindingReason(code, tool, text)
}

func setScannerFindingReason(finding *scannerFinding, reason securityreason.Reason) {
	if finding == nil {
		return
	}
	finding.Reason = reason.Text
	finding.ReasonCode = reason.Code
	finding.ReasonArgs = reason.Args
}

func scannerFindingWithReason(finding scannerFinding, reason securityreason.Reason) scannerFinding {
	setScannerFindingReason(&finding, reason)
	return finding
}

func osvScannerVulnSeverity(vuln osvScannerVuln, groups []osvScannerGroup) string {
	if severity := primaryOSVSeverity(osvSeverityFromScanner(vuln.Severity)); severity != "" {
		return severity
	}
	for _, group := range groups {
		if !osvScannerGroupMatchesVuln(group, vuln) {
			continue
		}
		if severity := strings.TrimSpace(group.MaxSeverity); severity != "" {
			return "CVSS:" + severity
		}
	}
	return ""
}

func osvScannerGroupMatchesVuln(group osvScannerGroup, vuln osvScannerVuln) bool {
	if vuln.ID == "" {
		return false
	}
	ids := append([]string{vuln.ID}, vuln.Aliases...)
	for _, groupID := range group.IDs {
		for _, id := range ids {
			if strings.EqualFold(strings.TrimSpace(groupID), strings.TrimSpace(id)) {
				return true
			}
		}
	}
	return false
}

func osvScannerRemediation(fixedVersions []string, pkg osvScannerPackageInfo, source osvScannerSource) string {
	target := "affected lockfile or manifest"
	if source.Path != "" {
		target = source.Path
		if source.Type != "" {
			target = source.Type + " " + target
		}
	}
	pkgName := "affected package"
	if pkg.Name != "" {
		pkgName = pkg.Name
		if pkg.Ecosystem != "" {
			pkgName = pkg.Ecosystem + "/" + pkgName
		}
	}
	guidance := osvScannerEcosystemRemediation(pkg.Ecosystem, source)
	if len(fixedVersions) > 0 {
		return "update " + pkgName + " in " + target + " to fixed version: " + strings.Join(fixedVersions, ",") + guidance + "; then rerun osv-scanner"
	}
	return "update " + pkgName + " in " + target + " to a non-vulnerable version" + guidance + "; then rerun osv-scanner"
}

func osvScannerEcosystemRemediation(ecosystem string, source osvScannerSource) string {
	sourcePath := strings.ToLower(strings.TrimSpace(source.Path))
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		switch {
		case strings.HasSuffix(sourcePath, "pnpm-lock.yaml"):
			return "; use pnpm audit/update for this lockfile"
		case strings.HasSuffix(sourcePath, "bun.lock") || strings.HasSuffix(sourcePath, "bun.lockb"):
			return "; use bun audit/update for this lockfile"
		}
		return "; use the owning package manager audit/update command for this lockfile"
	case "maven":
		if strings.Contains(sourcePath, "gradle") {
			return "; inspect the Gradle dependencyInsight report and update the direct dependency or version catalog"
		}
		return "; inspect the Maven dependency tree and update the direct dependency or dependencyManagement override"
	case "nuget":
		if strings.HasSuffix(sourcePath, "directory.packages.props") {
			return "; update the central package management entry"
		}
		return "; update the PackageReference or central package management entry"
	case "packagist":
		return "; update composer constraints and regenerate composer.lock"
	case "rubygems":
		return "; update Gemfile constraints and regenerate Gemfile.lock"
	case "crates.io":
		return "; update Cargo.toml/Cargo.lock with cargo update or an explicit dependency constraint"
	case "go":
		return "; update go.mod/go.sum with go get or go mod tidy"
	case "pypi":
		return "; update Python requirements or lockfile constraints"
	default:
		return ""
	}
}

func runGitleaksDirScan(ctx context.Context, commandRunner commandRunner, root string) scannerEvidence {
	reportFile, err := os.CreateTemp("", "updev-gitleaks-*.json")
	if err != nil {
		return scannerEvidence{
			Tool:     "gitleaks",
			Target:   root,
			Status:   plan.StatusUnavailable,
			Decision: "review",
			Reason:   "gitleaks report file unavailable",
			Error:    err.Error(),
		}
	}
	reportPath := reportFile.Name()
	_ = reportFile.Close()
	_ = os.Remove(reportPath)
	defer os.Remove(reportPath)

	args := []string{"dir", "--no-banner", "--no-color", "--redact", "--report-format", "json", "--report-path", reportPath, root}
	command := []string{"gitleaks", "dir", "--no-banner", "--no-color", "--redact", "--report-format", "json", "--report-path", "<temp-report>", root}
	evidence := scannerEvidence{
		Tool:     "gitleaks",
		Target:   root,
		Command:  command,
		Status:   plan.StatusOK,
		Decision: "allow",
		Reason:   "gitleaks directory scan completed",
	}
	result := commandRunner.Run(ctx, "gitleaks", args...)
	raw := result.Stdout
	if fileRaw, readErr := os.ReadFile(reportPath); readErr == nil && strings.TrimSpace(string(fileRaw)) != "" {
		raw = string(fileRaw)
	}
	parsed, ok := parseGitleaksReport(raw)
	if ok {
		evidence = gitleaksEvidenceFromReport(evidence, parsed)
		if evidence.FindingCount > 0 {
			return evidence
		}
		if result.Code != 0 || result.Err != nil {
			evidence.Status = scannerCommandErrorStatus(result)
			evidence.Decision = "review"
			evidence.Reason = "gitleaks completed with no parsed findings but returned an error"
			evidence.Error = firstNonEmpty(result.Stderr, scannerErrorText(result))
		}
		return evidence
	}
	if result.Code != 0 || result.Err != nil {
		evidence.Status = scannerCommandErrorStatus(result)
		evidence.Decision = "review"
		evidence.Reason = "gitleaks unavailable"
		evidence.Error = firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result))
		return evidence
	}
	return evidence
}

func parseGitleaksReport(raw string) ([]gitleaksFinding, bool) {
	return securityscanner.ParseGitleaksReport(raw)
}

func gitleaksEvidenceFromReport(evidence scannerEvidence, report []gitleaksFinding) scannerEvidence {
	evidence.FindingCount = len(report)
	if len(report) == 0 {
		return evidence
	}
	findings := make([]scannerFinding, 0, len(report))
	for _, finding := range report {
		reason := scannerSecurityFindingReason(securityreason.ScannerSecret, "gitleaks", "gitleaks reported possible secret")
		findings = append(findings, scannerFindingWithReason(scannerFinding{
			Kind:        "secret",
			RuleID:      finding.RuleID,
			File:        finding.File,
			StartLine:   finding.StartLine,
			EndLine:     finding.EndLine,
			Commit:      finding.Commit,
			Fingerprint: finding.Fingerprint,
			Description: finding.Description,
			Decision:    "hold",
			Remediation: "revoke or rotate the secret if real, remove it from source/history, then rerun gitleaks",
			Confidence:  "medium",
			Evidence:    []string{"gitleaks"},
		}, reason))
	}
	evidence.Status = plan.StatusHeld
	evidence.Decision = "hold"
	evidence.Reason = "gitleaks reported possible secrets"
	sortScannerFindings(findings)
	evidence.Findings = findings
	return evidence
}

func runZizmorWorkflowScan(ctx context.Context, commandRunner commandRunner, root string) scannerEvidence {
	args := []string{"--format=json-v1", "--offline", "--collect=workflows", root}
	evidence := scannerEvidence{
		Tool:     "zizmor",
		Target:   root,
		Command:  append([]string{"zizmor"}, args...),
		Status:   plan.StatusOK,
		Decision: "allow",
		Reason:   "zizmor workflow scan completed",
	}
	result := commandRunner.Run(ctx, "zizmor", args...)
	parsed, ok := parseZizmorReport(result.Stdout)
	if ok {
		evidence = zizmorEvidenceFromReport(evidence, parsed)
		if evidence.FindingCount > 0 {
			return evidence
		}
		if result.Code != 0 || result.Err != nil {
			evidence.Status = scannerCommandErrorStatus(result)
			evidence.Decision = "review"
			evidence.Reason = "zizmor completed with no parsed findings but returned an error"
			evidence.Error = firstNonEmpty(result.Stderr, scannerErrorText(result))
		}
		return evidence
	}
	if result.Code != 0 || result.Err != nil {
		evidence.Status = scannerCommandErrorStatus(result)
		evidence.Decision = "review"
		evidence.Reason = "zizmor unavailable"
		evidence.Error = firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result))
		return evidence
	}
	return evidence
}

func parseZizmorReport(raw string) ([]zizmorFinding, bool) {
	return securityscanner.ParseZizmorReport(raw)
}

func zizmorEvidenceFromReport(evidence scannerEvidence, report []zizmorFinding) scannerEvidence {
	findings := []scannerFinding{}
	for _, item := range report {
		if item.Ignored {
			continue
		}
		file, startLine, endLine := zizmorFindingPrimaryLocation(item)
		reason := scannerSecurityFindingReason(securityreason.ScannerWorkflow, "zizmor", "zizmor reported workflow security finding")
		findings = append(findings, scannerFindingWithReason(scannerFinding{
			Kind:        "workflow",
			RuleID:      item.Ident,
			File:        file,
			StartLine:   startLine,
			EndLine:     endLine,
			Description: item.Desc,
			URL:         item.URL,
			Decision:    "hold",
			Remediation: "update the GitHub Actions workflow according to the zizmor finding and rerun zizmor",
			Confidence:  item.Determinations.Confidence,
			Evidence:    []string{"zizmor"},
			Severity:    item.Determinations.Severity,
		}, reason))
	}
	evidence.FindingCount = len(findings)
	if len(findings) == 0 {
		return evidence
	}
	evidence.Status = plan.StatusHeld
	evidence.Decision = "hold"
	evidence.Reason = "zizmor reported workflow security findings"
	sortScannerFindings(findings)
	evidence.Findings = findings
	return evidence
}

func zizmorFindingPrimaryLocation(finding zizmorFinding) (file string, startLine int, endLine int) {
	if len(finding.Locations) == 0 {
		return "", 0, 0
	}
	location := finding.Locations[0]
	file = location.Symbolic.Key.Local.GivenPath
	startLine = location.Concrete.Location.StartPoint.Row + 1
	endLine = location.Concrete.Location.EndPoint.Row + 1
	return file, startLine, endLine
}

func runTrivyFilesystemScan(ctx context.Context, commandRunner commandRunner, root string, desired []securityPackage) scannerEvidence {
	args := []string{"fs", "--format", "json", "--scanners", "vuln,secret,misconfig", "--quiet", "--exit-code", "1", root}
	evidence := scannerEvidence{
		Tool:     "trivy",
		Target:   root,
		Command:  append([]string{"trivy"}, args...),
		Status:   plan.StatusOK,
		Decision: "allow",
		Reason:   "trivy filesystem scan completed",
	}
	result := commandRunner.Run(ctx, "trivy", args...)
	parsed, ok := parseTrivyReport(result.Stdout)
	if ok {
		evidence = trivyEvidenceFromReport(evidence, parsed, desired)
		if evidence.FindingCount > 0 {
			return evidence
		}
		if result.Code != 0 || result.Err != nil {
			evidence.Status = scannerCommandErrorStatus(result)
			evidence.Decision = "review"
			evidence.Reason = "trivy completed with no parsed findings but returned an error"
			evidence.Error = firstNonEmpty(result.Stderr, scannerErrorText(result))
		}
		return evidence
	}
	if result.Code != 0 || result.Err != nil {
		evidence.Status = scannerCommandErrorStatus(result)
		evidence.Decision = "review"
		evidence.Reason = "trivy unavailable"
		evidence.Error = firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result))
		return evidence
	}
	return evidence
}

func parseTrivyReport(raw string) (trivyReport, bool) {
	return securityscanner.ParseTrivyReport(raw)
}

func trivyEvidenceFromReport(evidence scannerEvidence, report trivyReport, desired []securityPackage) scannerEvidence {
	evidence.SourceCount = len(report.Results)
	findings := []scannerFinding{}
	for _, result := range report.Results {
		for _, vuln := range result.Vulnerabilities {
			if strings.TrimSpace(vuln.VulnerabilityID) == "" {
				continue
			}
			pkg := scannerPackageInfoFromPURL(vuln.PkgIdentifier.PURL, vuln.PkgName, vuln.InstalledVersion, result.Type)
			dependencyKind := scannerDependencyKindForPackage(pkg, desired)
			evidenceValues := []string{"trivy"}
			if dependencyKind != "" {
				evidenceValues = append(evidenceValues, dependencyKind+"-dependency")
			}
			reason := scannerVulnerabilitySecurityReason("trivy", dependencyKind)
			findings = append(findings, scannerFindingWithReason(scannerFinding{
				Kind:           "vulnerability",
				SourcePath:     result.Target,
				SourceType:     result.Type,
				Ecosystem:      pkg.Ecosystem,
				Package:        firstNonEmpty(pkg.Name, vuln.PkgName),
				Version:        firstNonEmpty(pkg.Version, vuln.InstalledVersion),
				DependencyKind: dependencyKind,
				VulnID:         vuln.VulnerabilityID,
				Severity:       vuln.Severity,
				Description:    vuln.Title,
				URL:            vuln.PrimaryURL,
				Decision:       "hold",
				Remediation:    trivyVulnerabilityRemediation(vuln, result.Target),
				Confidence:     "high",
				Evidence:       evidenceValues,
				FixedVersions:  splitCommaFields(vuln.FixedVersion),
			}, reason))
		}
		for _, misconfig := range result.Misconfigurations {
			if strings.EqualFold(strings.TrimSpace(misconfig.Status), "passed") || strings.TrimSpace(misconfig.ID) == "" {
				continue
			}
			reason := scannerSecurityFindingReason(securityreason.ScannerMisconfiguration, "trivy", "trivy reported misconfiguration")
			findings = append(findings, scannerFindingWithReason(scannerFinding{
				Kind:        "misconfiguration",
				SourcePath:  result.Target,
				SourceType:  firstNonEmpty(misconfig.Type, result.Type),
				RuleID:      misconfig.ID,
				StartLine:   misconfig.CauseMetadata.StartLine,
				EndLine:     misconfig.CauseMetadata.EndLine,
				Description: firstNonEmpty(misconfig.Title, misconfig.Message),
				URL:         misconfig.PrimaryURL,
				Decision:    "hold",
				Remediation: firstNonEmpty(misconfig.Resolution, "review the configuration finding and rerun trivy"),
				Confidence:  "medium",
				Evidence:    []string{"trivy"},
				Severity:    misconfig.Severity,
			}, reason))
		}
		for _, secret := range result.Secrets {
			if strings.TrimSpace(secret.RuleID) == "" {
				continue
			}
			reason := scannerSecurityFindingReason(securityreason.ScannerSecret, "trivy", "trivy reported possible secret")
			findings = append(findings, scannerFindingWithReason(scannerFinding{
				Kind:        "secret",
				SourcePath:  result.Target,
				SourceType:  result.Type,
				RuleID:      secret.RuleID,
				StartLine:   secret.StartLine,
				EndLine:     secret.EndLine,
				Description: firstNonEmpty(secret.Title, secret.Category),
				Decision:    "hold",
				Remediation: "revoke or rotate the secret if real, remove it from source/history, then rerun trivy",
				Confidence:  "medium",
				Evidence:    []string{"trivy"},
				Severity:    secret.Severity,
			}, reason))
		}
	}
	evidence.PackageCount = trivyPackageCount(findings)
	evidence.FindingCount = len(findings)
	evidence.VulnerabilityCount = trivyVulnerabilityCount(findings)
	if len(findings) > 0 {
		sortScannerFindings(findings)
		evidence.Status = plan.StatusHeld
		evidence.Decision = "hold"
		evidence.Reason = "trivy reported security findings"
		evidence.Findings = findings
	}
	return evidence
}

func trivyVulnerabilityRemediation(vuln trivyVulnerability, target string) string {
	pkgName := firstNonEmpty(vuln.PkgName, "affected package")
	if len(splitCommaFields(vuln.FixedVersion)) > 0 {
		return "update " + pkgName + " in " + firstNonEmpty(target, "affected target") + " to fixed version: " + vuln.FixedVersion + "; then rerun trivy"
	}
	return "update " + pkgName + " in " + firstNonEmpty(target, "affected target") + " to a non-vulnerable version; then rerun trivy"
}

func trivyPackageCount(findings []scannerFinding) int {
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.Package == "" {
			continue
		}
		seen[strings.ToLower(finding.SourcePath+"\x00"+finding.Package+"\x00"+finding.Version)] = true
	}
	return len(seen)
}

func trivyVulnerabilityCount(findings []scannerFinding) int {
	count := 0
	for _, finding := range findings {
		if finding.Kind == "vulnerability" {
			count++
		}
	}
	return count
}

func runGrypeDirectoryScan(ctx context.Context, commandRunner commandRunner, root string, desired []securityPackage) scannerEvidence {
	args := []string{"-o", "json", "dir:" + root}
	evidence := scannerEvidence{
		Tool:     "grype",
		Target:   root,
		Command:  append([]string{"grype"}, args...),
		Status:   plan.StatusOK,
		Decision: "allow",
		Reason:   "grype directory scan completed",
	}
	result := commandRunner.Run(ctx, "grype", args...)
	parsed, ok := parseGrypeReport(result.Stdout)
	if ok {
		evidence = grypeEvidenceFromReport(evidence, parsed, desired)
		if evidence.FindingCount > 0 {
			return evidence
		}
		if result.Code != 0 || result.Err != nil {
			evidence.Status = scannerCommandErrorStatus(result)
			evidence.Decision = "review"
			evidence.Reason = "grype completed with no parsed findings but returned an error"
			evidence.Error = firstNonEmpty(result.Stderr, scannerErrorText(result))
		}
		return evidence
	}
	if result.Code != 0 || result.Err != nil {
		evidence.Status = scannerCommandErrorStatus(result)
		evidence.Decision = "review"
		evidence.Reason = "grype unavailable"
		evidence.Error = firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result))
		return evidence
	}
	return evidence
}

func parseGrypeReport(raw string) (grypeReport, bool) {
	return securityscanner.ParseGrypeReport(raw)
}

func grypeEvidenceFromReport(evidence scannerEvidence, report grypeReport, desired []securityPackage) scannerEvidence {
	findings := []scannerFinding{}
	for _, match := range report.Matches {
		vulnID := strings.TrimSpace(match.Vulnerability.ID)
		if vulnID == "" {
			continue
		}
		fixedVersions := cleanedStringSlice(match.Vulnerability.Fix.Versions)
		confidence := "high"
		evidenceValues := []string{"grype"}
		usesCPE := grypeMatchUsesCPE(match)
		if usesCPE {
			confidence = "low"
			evidenceValues = append(evidenceValues, "cpe-match")
		}
		pkg := scannerPackageInfoFromPURL(match.Artifact.PURL, match.Artifact.Name, match.Artifact.Version, match.Artifact.Type)
		dependencyKind := scannerDependencyKindForPackage(pkg, desired)
		if dependencyKind != "" {
			evidenceValues = append(evidenceValues, dependencyKind+"-dependency")
		}
		reason := scannerVulnerabilitySecurityReason("grype", dependencyKind)
		if usesCPE {
			reason.Text += " via CPE-style match"
			if reason.Args == nil {
				reason.Args = map[string]string{}
			}
			reason.Args["match_style"] = "cpe"
		}
		findings = append(findings, scannerFindingWithReason(scannerFinding{
			Kind:           "vulnerability",
			SourcePath:     grypeArtifactSource(match.Artifact),
			SourceType:     match.Artifact.Type,
			Ecosystem:      pkg.Ecosystem,
			Package:        firstNonEmpty(pkg.Name, match.Artifact.Name),
			Version:        firstNonEmpty(pkg.Version, match.Artifact.Version),
			DependencyKind: dependencyKind,
			VulnID:         vulnID,
			Severity:       match.Vulnerability.Severity,
			Description:    match.Vulnerability.Description,
			URL:            firstString(match.Vulnerability.URLs),
			Decision:       "hold",
			Remediation:    grypeRemediation(match, fixedVersions),
			Confidence:     confidence,
			Evidence:       evidenceValues,
			FixedVersions:  fixedVersions,
		}, reason))
	}
	evidence.SourceCount = grypeSourceCount(report.Matches)
	evidence.PackageCount = grypePackageCount(findings)
	evidence.FindingCount = len(findings)
	evidence.VulnerabilityCount = len(findings)
	if len(findings) > 0 {
		sortScannerFindings(findings)
		evidence.Status = plan.StatusHeld
		evidence.Decision = "hold"
		evidence.Reason = "grype reported vulnerabilities"
		evidence.Findings = findings
	}
	return evidence
}

func grypeArtifactSource(artifact grypeArtifact) string {
	for _, location := range artifact.Locations {
		if path := strings.TrimSpace(location.Path); path != "" {
			return path
		}
	}
	return ""
}

func grypeMatchUsesCPE(match grypeMatch) bool {
	for _, detail := range match.MatchDetails {
		if strings.Contains(strings.ToLower(detail.Type), "cpe") || strings.Contains(strings.ToLower(detail.Matcher), "cpe") {
			return true
		}
	}
	return false
}

func grypeRemediation(match grypeMatch, fixedVersions []string) string {
	pkgName := firstNonEmpty(match.Artifact.Name, "affected package")
	target := firstNonEmpty(grypeArtifactSource(match.Artifact), "affected target")
	if len(fixedVersions) > 0 {
		return "update " + pkgName + " in " + target + " to fixed version: " + strings.Join(fixedVersions, ",") + "; then rerun grype"
	}
	return "update " + pkgName + " in " + target + " to a non-vulnerable version or replace it; then rerun grype"
}

func scannerPackageInfoFromPURL(rawPURL string, fallbackName string, fallbackVersion string, fallbackType string) osvScannerPackageInfo {
	return securityscanner.PackageInfoFromPURL(rawPURL, fallbackName, fallbackVersion, fallbackType)
}

func parseScannerPURL(rawPURL string) osvScannerPackageInfo {
	return securityscanner.ParsePURL(rawPURL)
}

func scannerPackageNameFromPURLPath(purlType string, path string) string {
	return securityscanner.PackageNameFromPURLPath(purlType, path)
}

func scannerURLUnescape(value string) string {
	return securityscanner.URLUnescape(value)
}

func scannerEcosystemFromType(value string) string {
	return securityscanner.EcosystemFromType(value)
}

func grypeSourceCount(matches []grypeMatch) int {
	seen := map[string]bool{}
	for _, match := range matches {
		source := grypeArtifactSource(match.Artifact)
		if source == "" {
			source = match.Artifact.Type
		}
		if source == "" {
			continue
		}
		seen[strings.ToLower(source)] = true
	}
	return len(seen)
}

func grypePackageCount(findings []scannerFinding) int {
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.Package == "" {
			continue
		}
		seen[strings.ToLower(finding.SourcePath+"\x00"+finding.Package+"\x00"+finding.Version)] = true
	}
	return len(seen)
}

func cleanedStringSlice(values []string) []string {
	cleaned := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func firstString(values []string) string {
	for _, value := range values {
		if value := strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func splitCommaFields(value string) []string {
	parts := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func hasGitHubWorkflowFiles(root string) bool {
	return securityscanner.HasGitHubWorkflowFiles(root)
}

func scannerCommandErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result)))
	if result.Code == 127 || strings.Contains(detail, "executable file not found") || strings.Contains(detail, "command not found") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func scannerEvidenceReportStatus(current plan.Status, scanners []scannerEvidence) plan.Status {
	return securityscanner.ReportStatus(current, scanners)
}

func scannerEvidenceSummary(scanners []scannerEvidence) (held int, unavailable int, errors int) {
	return securityscanner.Summary(scanners)
}

func hasScannerEvidenceAttention(scanners []scannerEvidence) bool {
	return securityscanner.HasAttention(scanners)
}

func hasScannerFindings(scanners []scannerEvidence) bool {
	return securityscanner.HasFindingAttention(scanners)
}

func scannerFindingSource(finding scannerFinding) string {
	return securityscanner.FindingSource(finding)
}

func scannerFindingID(finding scannerFinding) string {
	return securityscanner.FindingID(finding)
}

func scannerFindingDetail(finding scannerFinding) string {
	return securityscanner.FindingDetail(finding)
}

func sortScannerFindings(findings []scannerFinding) {
	securityscanner.SortFindings(findings)
}

func scannerEvidenceWithPolicyDecision(scanner scannerEvidence) scannerEvidence {
	return securityscanner.ApplyPolicyDecision(scanner)
}

func scannerErrorText(result runner.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Code != 0 {
		return fmt.Sprintf("exit status %d", result.Code)
	}
	return ""
}
