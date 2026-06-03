package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type scannerEvidence struct {
	Tool               string           `json:"tool"`
	Target             string           `json:"target"`
	Command            []string         `json:"command,omitempty"`
	Status             plan.Status      `json:"status"`
	Decision           string           `json:"decision"`
	Reason             string           `json:"reason,omitempty"`
	SourceCount        int              `json:"source_count,omitempty"`
	PackageCount       int              `json:"package_count,omitempty"`
	FindingCount       int              `json:"finding_count,omitempty"`
	VulnerabilityCount int              `json:"vulnerability_count,omitempty"`
	Findings           []scannerFinding `json:"findings,omitempty"`
	Error              string           `json:"error,omitempty"`
}

type scannerFinding struct {
	Kind           string   `json:"kind,omitempty"`
	SourcePath     string   `json:"source_path,omitempty"`
	SourceType     string   `json:"source_type,omitempty"`
	Ecosystem      string   `json:"ecosystem,omitempty"`
	Package        string   `json:"package,omitempty"`
	Version        string   `json:"version,omitempty"`
	DependencyKind string   `json:"dependency_kind,omitempty"`
	VulnID         string   `json:"vuln_id,omitempty"`
	RuleID         string   `json:"rule_id,omitempty"`
	File           string   `json:"file,omitempty"`
	StartLine      int      `json:"start_line,omitempty"`
	EndLine        int      `json:"end_line,omitempty"`
	Commit         string   `json:"commit,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
	Description    string   `json:"description,omitempty"`
	URL            string   `json:"url,omitempty"`
	Decision       string   `json:"decision,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Remediation    string   `json:"remediation,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
	Severity       string   `json:"severity,omitempty"`
	FixedVersions  []string `json:"fixed_versions,omitempty"`
}

type osvScannerReport struct {
	Results []osvScannerResult `json:"results"`
}

type osvScannerResult struct {
	Source   osvScannerSource    `json:"source"`
	Packages []osvScannerPackage `json:"packages"`
}

type osvScannerSource struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type osvScannerPackage struct {
	Package         osvScannerPackageInfo `json:"package"`
	Vulnerabilities []osvScannerVuln      `json:"vulnerabilities"`
	Groups          []osvScannerGroup     `json:"groups,omitempty"`
}

type osvScannerPackageInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

type osvScannerVuln struct {
	ID       string        `json:"id"`
	Aliases  []string      `json:"aliases"`
	Severity []osvSeverity `json:"severity"`
	Affected []osvAffected `json:"affected,omitempty"`
}

type osvScannerGroup struct {
	IDs         []string `json:"ids,omitempty"`
	MaxSeverity string   `json:"max_severity,omitempty"`
}

type gitleaksFinding struct {
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	EndLine     int    `json:"EndLine"`
	Commit      string `json:"Commit"`
	RuleID      string `json:"RuleID"`
	Fingerprint string `json:"Fingerprint"`
}

type zizmorFinding struct {
	Ident          string `json:"ident"`
	Desc           string `json:"desc"`
	URL            string `json:"url"`
	Determinations struct {
		Confidence string `json:"confidence"`
		Severity   string `json:"severity"`
	} `json:"determinations"`
	Locations []zizmorLocation `json:"locations"`
	Ignored   bool             `json:"ignored"`
}

type zizmorLocation struct {
	Symbolic struct {
		Key struct {
			Local struct {
				GivenPath string `json:"given_path"`
			} `json:"Local"`
		} `json:"key"`
	} `json:"symbolic"`
	Concrete struct {
		Location struct {
			StartPoint struct {
				Row int `json:"row"`
			} `json:"start_point"`
			EndPoint struct {
				Row int `json:"row"`
			} `json:"end_point"`
		} `json:"location"`
	} `json:"concrete"`
}

type trivyReport struct {
	Results []trivyResult `json:"Results"`
}

type trivyResult struct {
	Target            string                  `json:"Target"`
	Type              string                  `json:"Type"`
	Vulnerabilities   []trivyVulnerability    `json:"Vulnerabilities"`
	Misconfigurations []trivyMisconfiguration `json:"Misconfigurations"`
	Secrets           []trivySecret           `json:"Secrets"`
}

type trivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
	PrimaryURL       string `json:"PrimaryURL"`
	PkgIdentifier    struct {
		PURL string `json:"PURL"`
	} `json:"PkgIdentifier"`
}

type trivyMisconfiguration struct {
	ID            string             `json:"ID"`
	Type          string             `json:"Type"`
	Title         string             `json:"Title"`
	Message       string             `json:"Message"`
	Resolution    string             `json:"Resolution"`
	Severity      string             `json:"Severity"`
	PrimaryURL    string             `json:"PrimaryURL"`
	Status        string             `json:"Status"`
	CauseMetadata trivyCauseMetadata `json:"CauseMetadata"`
}

type trivyCauseMetadata struct {
	StartLine int `json:"StartLine"`
	EndLine   int `json:"EndLine"`
}

type trivySecret struct {
	RuleID    string `json:"RuleID"`
	Category  string `json:"Category"`
	Severity  string `json:"Severity"`
	Title     string `json:"Title"`
	StartLine int    `json:"StartLine"`
	EndLine   int    `json:"EndLine"`
}

type grypeReport struct {
	Matches []grypeMatch `json:"matches"`
}

type grypeMatch struct {
	Vulnerability grypeVulnerability `json:"vulnerability"`
	Artifact      grypeArtifact      `json:"artifact"`
	MatchDetails  []grypeMatchDetail `json:"matchDetails"`
}

type grypeVulnerability struct {
	ID          string   `json:"id"`
	Severity    string   `json:"severity"`
	Description string   `json:"description"`
	Fix         grypeFix `json:"fix"`
	URLs        []string `json:"urls"`
}

type grypeFix struct {
	Versions []string `json:"versions"`
	State    string   `json:"state"`
}

type grypeArtifact struct {
	Name      string          `json:"name"`
	Version   string          `json:"version"`
	Type      string          `json:"type"`
	PURL      string          `json:"purl"`
	Locations []grypeLocation `json:"locations"`
}

type grypeLocation struct {
	Path string `json:"path"`
}

type grypeMatchDetail struct {
	Type    string `json:"type"`
	Matcher string `json:"matcher"`
}

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
	names, err := parseSecurityScannerNames(value)
	if err != nil || len(names) == 0 {
		return nil
	}
	if len(names) == 1 {
		switch names[0] {
		case "none":
			return nil
		case "auto":
			tools := []string{"osv-scanner", "gitleaks"}
			if hasGitHubWorkflowFiles(root) {
				tools = append(tools, "zizmor")
			}
			return tools
		case "all":
			return []string{"osv-scanner", "gitleaks", "zizmor", "trivy", "grype"}
		}
	}
	return names
}

func parseSecurityScannerNames(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "auto"
	}
	names := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		name := normalizeSecurityScannerName(part)
		if name == "" {
			continue
		}
		if !securityScannerNameAllowed(name) {
			return nil, fmt.Errorf("unsupported scanner: %s", strings.TrimSpace(part))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return []string{"auto"}, nil
	}
	if len(names) > 1 {
		for _, name := range names {
			if name == "auto" || name == "all" || name == "none" {
				return nil, fmt.Errorf("scanner %q cannot be combined with other scanners", name)
			}
		}
	}
	return names, nil
}

func normalizeSecurityScannerName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "osv":
		return "osv-scanner"
	case "secret", "secrets":
		return "gitleaks"
	case "workflow", "workflows", "actions", "github-actions":
		return "zizmor"
	case "trivy-fs":
		return "trivy"
	case "anchore-grype", "grype-dir":
		return "grype"
	default:
		return value
	}
}

func securityScannerNameAllowed(name string) bool {
	switch name {
	case "auto", "none", "all", "osv-scanner", "gitleaks", "zizmor", "trivy", "grype":
		return true
	default:
		return false
	}
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
	var report osvScannerReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
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
				findings = append(findings, scannerFinding{
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
					Reason:         osvScannerFindingReason(dependencyKind),
					Remediation:    osvScannerRemediation(fixedVersions, pkg.Package, result.Source),
					Confidence:     "high",
					Evidence:       evidenceValues,
					FixedVersions:  fixedVersions,
				})
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
		Severity: vuln.Severity,
		Affected: vuln.Affected,
	}, securityPackage{
		Ecosystem: pkg.Ecosystem,
		Package:   pkg.Name,
	})
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

func scannerVulnerabilityReason(tool string, dependencyKind string) string {
	switch dependencyKind {
	case "direct":
		return tool + " reported vulnerability in a directly managed package"
	case "transitive":
		return tool + " reported vulnerability in a transitive dependency"
	default:
		return tool + " reported vulnerability"
	}
}

func osvScannerFindingReason(dependencyKind string) string {
	switch dependencyKind {
	case "direct":
		return "osv-scanner reported vulnerability in a directly managed package"
	case "transitive":
		return "osv-scanner reported vulnerability in a transitive dependency"
	default:
		return "osv-scanner reported vulnerability"
	}
}

func osvScannerVulnSeverity(vuln osvScannerVuln, groups []osvScannerGroup) string {
	if severity := primaryOSVSeverity(vuln.Severity); severity != "" {
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
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var findings []gitleaksFinding
	if err := json.Unmarshal([]byte(raw), &findings); err != nil {
		return nil, false
	}
	return findings, true
}

func gitleaksEvidenceFromReport(evidence scannerEvidence, report []gitleaksFinding) scannerEvidence {
	evidence.FindingCount = len(report)
	if len(report) == 0 {
		return evidence
	}
	findings := make([]scannerFinding, 0, len(report))
	for _, finding := range report {
		findings = append(findings, scannerFinding{
			Kind:        "secret",
			RuleID:      finding.RuleID,
			File:        finding.File,
			StartLine:   finding.StartLine,
			EndLine:     finding.EndLine,
			Commit:      finding.Commit,
			Fingerprint: finding.Fingerprint,
			Description: finding.Description,
			Decision:    "hold",
			Reason:      "gitleaks reported possible secret",
			Remediation: "revoke or rotate the secret if real, remove it from source/history, then rerun gitleaks",
			Confidence:  "medium",
			Evidence:    []string{"gitleaks"},
		})
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
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var findings []zizmorFinding
	if err := json.Unmarshal([]byte(raw), &findings); err != nil {
		return nil, false
	}
	return findings, true
}

func zizmorEvidenceFromReport(evidence scannerEvidence, report []zizmorFinding) scannerEvidence {
	findings := []scannerFinding{}
	for _, item := range report {
		if item.Ignored {
			continue
		}
		file, startLine, endLine := zizmorFindingPrimaryLocation(item)
		findings = append(findings, scannerFinding{
			Kind:        "workflow",
			RuleID:      item.Ident,
			File:        file,
			StartLine:   startLine,
			EndLine:     endLine,
			Description: item.Desc,
			URL:         item.URL,
			Decision:    "hold",
			Reason:      "zizmor reported workflow security finding",
			Remediation: "update the GitHub Actions workflow according to the zizmor finding and rerun zizmor",
			Confidence:  item.Determinations.Confidence,
			Evidence:    []string{"zizmor"},
			Severity:    item.Determinations.Severity,
		})
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
	var report trivyReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
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
			findings = append(findings, scannerFinding{
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
				Reason:         scannerVulnerabilityReason("trivy", dependencyKind),
				Remediation:    trivyVulnerabilityRemediation(vuln, result.Target),
				Confidence:     "high",
				Evidence:       evidenceValues,
				FixedVersions:  splitCommaFields(vuln.FixedVersion),
			})
		}
		for _, misconfig := range result.Misconfigurations {
			if strings.EqualFold(strings.TrimSpace(misconfig.Status), "passed") || strings.TrimSpace(misconfig.ID) == "" {
				continue
			}
			findings = append(findings, scannerFinding{
				Kind:        "misconfiguration",
				SourcePath:  result.Target,
				SourceType:  firstNonEmpty(misconfig.Type, result.Type),
				RuleID:      misconfig.ID,
				StartLine:   misconfig.CauseMetadata.StartLine,
				EndLine:     misconfig.CauseMetadata.EndLine,
				Description: firstNonEmpty(misconfig.Title, misconfig.Message),
				URL:         misconfig.PrimaryURL,
				Decision:    "hold",
				Reason:      "trivy reported misconfiguration",
				Remediation: firstNonEmpty(misconfig.Resolution, "review the configuration finding and rerun trivy"),
				Confidence:  "medium",
				Evidence:    []string{"trivy"},
				Severity:    misconfig.Severity,
			})
		}
		for _, secret := range result.Secrets {
			if strings.TrimSpace(secret.RuleID) == "" {
				continue
			}
			findings = append(findings, scannerFinding{
				Kind:        "secret",
				SourcePath:  result.Target,
				SourceType:  result.Type,
				RuleID:      secret.RuleID,
				StartLine:   secret.StartLine,
				EndLine:     secret.EndLine,
				Description: firstNonEmpty(secret.Title, secret.Category),
				Decision:    "hold",
				Reason:      "trivy reported possible secret",
				Remediation: "revoke or rotate the secret if real, remove it from source/history, then rerun trivy",
				Confidence:  "medium",
				Evidence:    []string{"trivy"},
				Severity:    secret.Severity,
			})
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
	var report grypeReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
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
		reason := scannerVulnerabilityReason("grype", dependencyKind)
		if usesCPE {
			reason += " via CPE-style match"
		}
		findings = append(findings, scannerFinding{
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
			Reason:         reason,
			Remediation:    grypeRemediation(match, fixedVersions),
			Confidence:     confidence,
			Evidence:       evidenceValues,
			FixedVersions:  fixedVersions,
		})
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
	pkg := parseScannerPURL(rawPURL)
	if pkg.Name == "" {
		pkg.Name = strings.TrimSpace(fallbackName)
	}
	if pkg.Version == "" {
		pkg.Version = strings.TrimSpace(fallbackVersion)
	}
	if pkg.Ecosystem == "" {
		pkg.Ecosystem = scannerEcosystemFromType(fallbackType)
	}
	return pkg
}

func parseScannerPURL(rawPURL string) osvScannerPackageInfo {
	value := strings.TrimSpace(rawPURL)
	if value == "" || !strings.HasPrefix(strings.ToLower(value), "pkg:") {
		return osvScannerPackageInfo{}
	}
	value = value[len("pkg:"):]
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		value = value[:index]
	}
	purlType, path, ok := strings.Cut(value, "/")
	if !ok {
		return osvScannerPackageInfo{Ecosystem: scannerEcosystemFromType(purlType)}
	}
	version := ""
	if index := strings.LastIndex(path, "@"); index > 0 {
		version = scannerURLUnescape(path[index+1:])
		path = path[:index]
	}
	return osvScannerPackageInfo{
		Name:      scannerPackageNameFromPURLPath(purlType, path),
		Version:   version,
		Ecosystem: scannerEcosystemFromType(purlType),
	}
}

func scannerPackageNameFromPURLPath(purlType string, path string) string {
	parts := []string{}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		parts = append(parts, scannerURLUnescape(part))
	}
	if len(parts) == 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(purlType)) {
	case "maven":
		if len(parts) >= 2 {
			return parts[len(parts)-2] + ":" + parts[len(parts)-1]
		}
	case "npm":
		if len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-2], "@") {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	case "composer":
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	return parts[len(parts)-1]
}

func scannerURLUnescape(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func scannerEcosystemFromType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "npm", "node-pkg":
		return "npm"
	case "cargo", "rust":
		return "crates.io"
	case "pypi", "python":
		return "PyPI"
	case "golang", "go":
		return "Go"
	case "maven", "java":
		return "Maven"
	case "gem", "ruby", "rubygems":
		return "RubyGems"
	case "nuget", "dotnet":
		return "NuGet"
	case "composer", "php":
		return "Packagist"
	default:
		return ""
	}
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
	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			return true
		}
	}
	return false
}

func scannerCommandErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, scannerErrorText(result)))
	if result.Code == 127 || strings.Contains(detail, "executable file not found") || strings.Contains(detail, "command not found") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func scannerEvidenceReportStatus(current plan.Status, scanners []scannerEvidence) plan.Status {
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

func scannerEvidenceSummary(scanners []scannerEvidence) (held int, unavailable int, errors int) {
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

func hasScannerEvidenceAttention(scanners []scannerEvidence) bool {
	for _, scanner := range scanners {
		if scanner.Status != plan.StatusOK || securityDecisionNeedsAttention(scanner.Decision) {
			return true
		}
	}
	return false
}

func hasScannerFindings(scanners []scannerEvidence) bool {
	for _, scanner := range scanners {
		for _, finding := range scanner.Findings {
			if securityDecisionNeedsAttention(finding.Decision) {
				return true
			}
		}
	}
	return false
}

func scannerFindingSource(finding scannerFinding) string {
	return firstNonEmpty(finding.SourcePath, finding.File)
}

func scannerFindingID(finding scannerFinding) string {
	return firstNonEmpty(finding.VulnID, finding.RuleID, finding.Fingerprint)
}

func scannerFindingDetail(finding scannerFinding) string {
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

func sortScannerFindings(findings []scannerFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left := scannerFindingPriority(findings[i])
		right := scannerFindingPriority(findings[j])
		for index := range left {
			if left[index] != right[index] {
				return left[index] > right[index]
			}
		}
		leftKey := strings.ToLower(scannerFindingSource(findings[i]) + "\x00" + scannerFindingID(findings[i]) + "\x00" + scannerFindingDetail(findings[i]))
		rightKey := strings.ToLower(scannerFindingSource(findings[j]) + "\x00" + scannerFindingID(findings[j]) + "\x00" + scannerFindingDetail(findings[j]))
		return leftKey < rightKey
	})
}

func scannerFindingPriority(finding scannerFinding) []int {
	return []int{
		securityDecisionPriority(finding.Decision),
		scannerFindingKindPriority(finding.Kind),
		int(securitySeverityScore(finding.Severity) * 10),
		boolPriority(len(finding.FixedVersions) > 0),
		scannerDependencyKindPriority(finding.DependencyKind),
	}
}

func scannerDependencyKindPriority(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "direct":
		return 2
	case "transitive":
		return 1
	default:
		return 0
	}
}

func scannerFindingKindPriority(kind string) int {
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

func scannerEvidenceWithPolicyDecision(scanner scannerEvidence) scannerEvidence {
	if len(scanner.Findings) == 0 || (scanner.Status != plan.StatusHeld && scanner.Status != plan.StatusOK) {
		return scanner
	}
	attention := false
	blocked := false
	for _, finding := range scanner.Findings {
		switch strings.ToLower(strings.TrimSpace(finding.Decision)) {
		case "block":
			blocked = true
		case "hold", "review", "":
			attention = true
		}
	}
	switch {
	case blocked:
		scanner.Status = plan.StatusBlocked
		scanner.Decision = "block"
		scanner.Reason = "scanner findings blocked by security policy"
	case attention:
		scanner.Status = plan.StatusHeld
		if scanner.Decision == "allow" {
			scanner.Decision = "hold"
		}
	case scanner.Status == plan.StatusHeld:
		scanner.Status = plan.StatusOK
		scanner.Decision = "allow"
		scanner.Reason = "scanner findings allowed by security policy"
	}
	return scanner
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
