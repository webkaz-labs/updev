package brew

import (
	"sort"

	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

func SafetyFindingsFromOutdated(report OutdatedReport, manifest Manifest) []securitygate.Finding {
	findings := make([]securitygate.Finding, 0, len(report.Formulae)+len(report.Casks))
	for _, item := range report.Formulae {
		findings = append(findings, SafetyFindingFromOutdated("brew", item, manifest))
	}
	for _, item := range report.Casks {
		findings = append(findings, SafetyFindingFromOutdated("cask", item, manifest))
	}
	return findings
}

func SafetyFindingFromOutdated(kind string, item OutdatedItem, manifest Manifest) securitygate.Finding {
	entry := manifest.Entry(kind, item.Name)
	decision := "unknown"
	reason := "release-age and provenance evidence are not available in the first Go safety slice"
	remediation := "retry after Homebrew metadata is available; strict mode requires metadata and release-age evidence"
	confidence := "low"
	trustKind := ""
	trustTarget := ""
	trustCommand := ""
	trustCommandArgv := []string{}
	if entry.URLBased {
		decision = "review"
		reason = "URL-based Homebrew cask needs manual provenance review before update"
		remediation = "review the cask source URL and add a temporary allow policy with reason and expiry if accepted"
	} else if entry.Tap != "" && !IsOfficialTap(entry.Tap) {
		decision = "review"
		reason = "non-official Homebrew tap needs provenance review before update"
		trustKind = "formula"
		if kind == "cask" {
			trustKind = "cask"
		}
		trustTarget = firstNonEmpty(entry.RawName, item.Name)
		trustCommandArgv = TrustCommandArgv(trustKind, trustTarget)
		trustCommand = JoinCommand(trustCommandArgv)
		remediation = "review the tap repository; if the package is accepted, prefer item-scoped trust with " + trustCommand + " before adding a temporary allow policy"
	} else if kind == "cask" {
		decision = "review"
		reason = "Homebrew cask updates need provenance and URL/release-age checks before strict mode can allow them"
		remediation = "review vendor provenance and add a temporary allow policy with reason and expiry if accepted"
	}
	structuredReason := securityreason.Infer(reason)
	if structuredReason.Code == "" {
		structuredReason = securityreason.HomebrewPostureReason(securityreason.HomebrewEvidenceUnavailable, kind, item.Name, reason, nil)
	}
	finding := securitygate.Finding{
		Provider:          "brew",
		Kind:              kind,
		Name:              item.Name,
		InstalledVersions: item.InstalledVersions,
		CurrentVersion:    item.CurrentVersion,
		Decision:          decision,
		Reason:            structuredReason.Text,
		ReasonCode:        structuredReason.Code,
		ReasonArgs:        structuredReason.Args,
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
		finding.TrustCommandArgv = trustCommandArgv
		finding.Evidence = appendEvidence(finding.Evidence, "Homebrew 6 tap trust target: "+trustKind+" "+trustTarget)
	}
	return finding
}

func ManifestWarnings(manifest Manifest) []securitygate.Finding {
	findings := []securitygate.Finding{}
	for _, entry := range manifest.Entries() {
		if !entry.URLBased {
			continue
		}
		reason := securityreason.HomebrewPostureReason(securityreason.HomebrewURLBasedCask, entry.Kind, entry.RawName, "URL-based Homebrew cask needs manual provenance review before update", nil)
		findings = append(findings, securitygate.Finding{
			Provider:    "brew",
			Kind:        entry.Kind,
			Name:        entry.RawName,
			Decision:    "review",
			Reason:      reason.Text,
			ReasonCode:  securityreason.HomebrewURLBasedCask,
			ReasonArgs:  reason.Args,
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
