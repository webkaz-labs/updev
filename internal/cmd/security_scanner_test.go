package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/securityscanner"
)

func TestCollectUpdateSafetyCachesBrewAdvisoryError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	apiCalls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}}
}`))
		case "/repos/jqlang/jq/releases/tags/jq-1.8.1":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()
	osvCalls := 0
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		osvCalls++
		http.Error(w, "temporary osv failure", http.StatusBadGateway)
	}))
	defer osvServer.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", apiServer.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", apiServer.URL)
	t.Setenv("UPDEV_OSV_API_URL", osvServer.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`}}
	first := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	second := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if osvCalls != 1 {
		t.Fatalf("expected brew advisory error to be cached, got %d OSV calls", osvCalls)
	}
	if apiCalls != 2 {
		t.Fatalf("expected metadata/release calls to be skipped on cached advisory error, got %d API calls", apiCalls)
	}
	if !containsSubstring(first.Warnings, "Homebrew advisory query failed") {
		t.Fatalf("expected first advisory warning, got %#v", first.Warnings)
	}
	if !containsSubstring(second.Warnings, "cached Homebrew advisory query failed") {
		t.Fatalf("expected cached advisory warning, got %#v", second.Warnings)
	}
}

func TestSecurityTextLocalizesPostureScannerAndFindingReasons(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	report := securityReport{
		Status:  plan.StatusHeld,
		Root:    "/repo",
		Sources: []string{"inventory"},
		Posture: []githubPosture{{
			Repository:    "owner/repo",
			DefaultBranch: "main",
			Decision:      "review",
			Reason:        "repository is archived",
			Remediation:   "replace the archived repository source or add a temporary policy override after review",
		}},
		Brew: []homebrewPosture{{
			Provider:    "brew",
			Kind:        "cask",
			Name:        "sample",
			Tap:         "example/tap",
			Decision:    "review",
			Reason:      "non-official Homebrew tap needs provenance review",
			Remediation: "review the tap repository and add a temporary allow policy with reason and expiry if accepted",
		}},
		VSCode: []vscodePosture{{
			Provider:    "brew",
			Kind:        "vscode",
			Name:        "publisher.extension",
			Publisher:   "publisher",
			Version:     "1.0.0",
			Decision:    "review",
			Reason:      "publisher domain is not verified in Marketplace metadata",
			Remediation: "verify publisher identity and source repository before adding a temporary policy override",
		}},
		NPM: []npmPosture{{
			Provider:    "mise",
			Kind:        "npm",
			Package:     "leftpad",
			Version:     "1.0.0",
			Decision:    "review",
			Reason:      "npm package has no maintainers in registry metadata",
			Remediation: "review package ownership and source provenance before keeping this package",
		}},
		Cargo: []cargoPosture{{
			Provider:    "mise",
			Kind:        "cargo",
			Crate:       "sample-crate",
			Version:     "1.0.0",
			Decision:    "review",
			Reason:      "installed crate version is yanked",
			Remediation: "update to a non-yanked crate version or replace the crate",
		}},
		PyPI: []pypiPosture{{
			Provider:    "mise",
			Kind:        "pipx",
			Package:     "sample-pkg",
			Version:     "1.0.0",
			Decision:    "review",
			Reason:      "installed PyPI version is yanked",
			Remediation: "update to a non-yanked PyPI release or replace the package",
		}},
		Audits: []nativeAudit{{
			Ecosystem: "npm",
			Tool:      "npm",
			Target:    "package-lock.json",
			Status:    plan.StatusHeld,
			Decision:  "hold",
			Reason:    "npm native audit reported vulnerabilities",
		}},
		Scanners: []scannerEvidence{{
			Tool:         "gitleaks",
			Status:       plan.StatusHeld,
			Decision:     "hold",
			Reason:       "gitleaks reported possible secrets",
			FindingCount: 1,
			Findings: []scannerFinding{{
				Kind:        "secret",
				File:        "config.env",
				RuleID:      "generic-api-key",
				Decision:    "hold",
				Reason:      "gitleaks reported possible secret",
				Remediation: "revoke or rotate the secret if real, remove it from source/history, then rerun gitleaks",
			}},
		}},
		Skipped: []securitySkipped{{
			Provider: "mise",
			Kind:     "aqua",
			Reason:   "unsupported mise backend ecosystem",
			Count:    1,
			Examples: []string{"aqua:owner/tool"},
		}},
		Findings: []securityFinding{{
			Provider:  "mise",
			Name:      "npm:leftpad",
			Version:   "1.0.0",
			Ecosystem: "npm",
			Package:   "leftpad",
			VulnID:    "OSV-2026-1",
			Decision:  "hold",
			Status:    plan.StatusHeld,
			Exposure:  "on-path-binary:leftpad",
		}},
	}
	var buffer bytes.Buffer
	printSecurityText(&buffer, report, false)
	got := buffer.String()
	for _, want := range []string{
		"owner/repo: repository が archi",
		"非公式 Homebrew tap は配布元の確認が必",
		"Marketplace metadata で publisher",
		"npm/leftpad: maintainer 情報が registry",
		"crates.io/sample-crate: インストール済",
		"PyPI/sample-pkg: インストール済み versi",
		"npm が npm の native audit で脆弱性を検出しまし",
		"gitleaks が secret の可能性を報告しました",
		"この mise backend ecosystem は自動照合に未対応です",
		"OSV vulnerability が一致し、on-",
		"source/history から削除してから gitleaks を再実行",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected localized security text to include %q, got %q", want, got)
		}
	}
	for _, unwanted := range []string{
		"repository is archived",
		"non-official Homebrew tap needs provenance review",
		"publisher domain is not verified",
		"gitleaks reported possible secrets",
		"unsupported mise backend ecosystem",
		"OSV vulnerability match; on-PATH binary exposure",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("expected security text to avoid English reason %q, got %q", unwanted, got)
		}
	}
}

func TestEnrichBrewSafetyAdvisoriesHoldsOSVMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/vulns/OSV-2026-0001" {
				t.Fatalf("unexpected OSV detail path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "id": "OSV-2026-0001",
  "affected": [{
    "package": {"ecosystem": "GIT", "name": "https://github.com/jqlang/jq.git"},
    "ranges": [{"events": [{"introduced": "0"}, {"fixed": "jq-1.8.2"}]}]
  }]
}`))
			return
		}
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "GIT" || request.Queries[0].Package.Name != "https://github.com/jqlang/jq.git" || request.Queries[0].Version != "jq-1.8.1" {
			t.Fatalf("unexpected safety OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"OSV-2026-0001","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	findings := []safetyFinding{{
		Provider:       "brew",
		Kind:           "brew",
		Name:           "jq",
		CurrentVersion: "1.8.1",
		URL:            "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz",
		Decision:       "allow",
		Confidence:     "medium",
	}}
	enriched, err := enrichBrewSafetyAdvisories(context.Background(), server.Client(), server.URL, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(enriched) != 1 || enriched[0].Decision != "hold" || !strings.Contains(enriched[0].Reason, "OSV-2026-0001") {
		t.Fatalf("expected OSV hold finding, got %#v", enriched)
	}
	if !containsString(enriched[0].AdvisoryIDs, "OSV-2026-0001") || !containsString(enriched[0].FixedVersions, "jq-1.8.2") || !containsString(enriched[0].Evidence, "osv-git") {
		t.Fatalf("expected OSV advisory evidence, got %#v", enriched[0])
	}
	if !strings.Contains(enriched[0].Remediation, "jq-1.8.2") {
		t.Fatalf("expected fixed-version Homebrew remediation, got %#v", enriched[0])
	}
}

func TestEnrichBrewSafetyAdvisoriesUsesGitHubForCuratedMappings(t *testing.T) {
	t.Setenv("UPDEV_GITHUB_TOKEN", "updev-test-token")
	seenGitHub := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/advisories" {
			if r.URL.Query().Get("type") == "malware" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			seenGitHub = true
			if got := r.URL.Query().Get("ecosystem"); got != "npm" {
				t.Fatalf("unexpected GitHub Advisory ecosystem: %q", got)
			}
			if got := r.URL.Query().Get("affects"); got != "pnpm@10.0.0" {
				t.Fatalf("unexpected GitHub Advisory affects: %q", got)
			}
			_, _ = w.Write([]byte(`[{
  "ghsa_id": "GHSA-homebrew-pnpm",
  "type": "reviewed",
  "severity": "high",
  "html_url": "https://github.com/advisories/GHSA-homebrew-pnpm",
  "vulnerabilities": [{
    "package": {"ecosystem": "npm", "name": "pnpm"},
    "first_patched_version": "10.0.1"
  }]
}]`))
			return
		}
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "npm" || request.Queries[0].Package.Name != "pnpm" || request.Queries[0].Version != "10.0.0" {
			t.Fatalf("unexpected safety OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	findings := []safetyFinding{{
		Provider:       "brew",
		Kind:           "brew",
		Name:           "pnpm",
		CurrentVersion: "10.0.0",
		Decision:       "allow",
		Confidence:     "medium",
	}}
	enriched, err := enrichBrewSafetyAdvisories(context.Background(), server.Client(), server.URL, findings)
	if err != nil {
		t.Fatal(err)
	}
	if !seenGitHub {
		t.Fatal("expected GitHub Advisory query")
	}
	if len(enriched) != 1 || enriched[0].Decision != "hold" || !strings.Contains(enriched[0].Reason, "GitHub Advisory") {
		t.Fatalf("expected GitHub Advisory hold finding, got %#v", enriched)
	}
	if !containsString(enriched[0].AdvisoryIDs, "GHSA-homebrew-pnpm") || !containsString(enriched[0].FixedVersions, "10.0.1") || !containsString(enriched[0].Evidence, "github-advisory-curated-homebrew-map") {
		t.Fatalf("expected GitHub Advisory Homebrew evidence, got %#v", enriched[0])
	}
}

func TestBrewSafetyAdvisoryPackagesFromFindingsUsesCuratedMappings(t *testing.T) {
	packages, indexes := brewSafetyAdvisoryPackagesFromFindings([]safetyFinding{{
		Provider:       "brew",
		Kind:           "brew",
		Name:           "pnpm",
		CurrentVersion: "11.4.0",
		URL:            "https://registry.npmjs.org/pnpm/-/pnpm-11.4.0.tgz",
	}})
	if len(packages) != 1 {
		t.Fatalf("expected one curated package mapping, got %#v", packages)
	}
	if packages[0].Ecosystem != "npm" || packages[0].Package != "pnpm" || packages[0].Version != "11.4.0" {
		t.Fatalf("unexpected curated package mapping: %#v", packages[0])
	}
	key := safetyAdvisoryPackageKey(packages[0])
	if got := indexes[key]; len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected advisory index map, got %#v", indexes)
	}
}

func TestBrewSafetyAdvisoryPackagesFromFindingsMapsMiseCrate(t *testing.T) {
	packages, _ := brewSafetyAdvisoryPackagesFromFindings([]safetyFinding{{
		Provider:       "brew",
		Kind:           "brew",
		Name:           "mise",
		CurrentVersion: "2026.5.14",
	}})
	if len(packages) != 1 || packages[0].Ecosystem != "crates.io" || packages[0].Package != "mise" || packages[0].Version != "2026.5.14" {
		t.Fatalf("expected mise crates.io advisory mapping, got %#v", packages)
	}
}

func TestBrewSafetyAdvisoryPackagesFromFindingsMapsGitHubCaskTags(t *testing.T) {
	packages, indexes := brewSafetyAdvisoryPackagesFromFindings([]safetyFinding{
		{
			Provider:       "brew",
			Kind:           "cask",
			Name:           "demo-app",
			CurrentVersion: "2.0.0",
			URL:            "https://github.com/example/demo-app/releases/download/v2.0.0/demo-app.dmg",
		},
		{
			Provider:       "brew",
			Kind:           "cask",
			Name:           "nongit",
			CurrentVersion: "2.0.0",
			URL:            "https://example.com/nongit.dmg",
		},
	})
	if len(packages) != 1 {
		t.Fatalf("expected one cask GIT advisory package, got %#v", packages)
	}
	if packages[0].Provider != "brew" || packages[0].Name != "demo-app" || packages[0].Ecosystem != "GIT" || packages[0].Package != "https://github.com/example/demo-app.git" || packages[0].Version != "v2.0.0" {
		t.Fatalf("unexpected cask advisory package mapping: %#v", packages[0])
	}
	key := safetyAdvisoryPackageKey(packages[0])
	if got := indexes[key]; len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected cask advisory index map, got %#v", indexes)
	}
}

func TestApplyBrewSafetyAdvisoryReportsCuratedMappingEvidence(t *testing.T) {
	finding := safetyFinding{Provider: "brew", Kind: "brew", Name: "pnpm"}
	setSafetyFindingReason(&finding, securityreason.CandidateReleaseTooNewReason(1, 3))
	finding = applyBrewSafetyAdvisory(finding, securityFinding{
		Ecosystem: "npm",
		VulnID:    "GHSA-pnpm",
	})
	if finding.Decision != "hold" || !containsString(finding.Evidence, "osv-curated-homebrew-map") || !strings.Contains(finding.Reason, "OSV curated mapping") {
		t.Fatalf("expected curated mapping advisory evidence, got %#v", finding)
	}
	if finding.ReasonCode != securityreason.HomebrewAdvisoryMatch || finding.ReasonArgs["advisory_ids"] != "GHSA-pnpm" || finding.ReasonArgs["advisory_source"] != "OSV" || finding.ReasonArgs["advisory_match_type"] != "advisory_related" {
		t.Fatalf("expected advisory reason to replace stale release-age code, got %#v", finding)
	}
}

func TestSecurityPostureStatusTreatsReviewAsHeld(t *testing.T) {
	got := securityPostureStatus(plan.StatusOK, []githubPosture{
		{Provider: "mise", Name: "github:owner/tool", Repository: "owner/tool", Decision: "review", Reason: "repository is archived"},
	}, nil, nil, nil, nil, nil)
	if got != plan.StatusHeld {
		t.Fatalf("expected review posture to hold scan status, got %s", got)
	}
}

func TestRunOSVScannerSourceScanParsesVulnerabilities(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "results": [
    {
      "source": {"path": "/repo/package-lock.json", "type": "lockfile"},
      "packages": [
        {
          "package": {"name": "left-pad", "version": "1.0.0", "ecosystem": "npm"},
          "groups": [{"ids": ["GHSA-0000-0000-0000"], "max_severity": "9.8"}],
          "vulnerabilities": [
            {
              "id": "GHSA-0000-0000-0000",
              "aliases": ["CVE-2026-0001"],
              "affected": [{
                "package": {"name": "left-pad", "ecosystem": "npm"},
                "ranges": [{"events": [{"introduced": "0"}, {"fixed": "1.0.1"}]}]
              }]
            }
          ]
        }
      ]
    }
  ]
}`,
	}}
	evidence := runOSVScannerSourceScan(context.Background(), fake, "/repo", []securityPackage{{Ecosystem: "npm", Package: "left-pad", Version: "1.0.0"}})
	if evidence.Status != plan.StatusHeld || evidence.Decision != "hold" || evidence.FindingCount != 1 || evidence.VulnerabilityCount != 1 || evidence.PackageCount != 1 || evidence.SourceCount != 1 {
		t.Fatalf("expected held scanner evidence, got %#v", evidence)
	}
	if len(evidence.Findings) != 1 || evidence.Findings[0].Kind != "vulnerability" || evidence.Findings[0].SourcePath != "/repo/package-lock.json" || evidence.Findings[0].VulnID != "GHSA-0000-0000-0000" {
		t.Fatalf("expected parsed scanner finding, got %#v", evidence.Findings)
	}
	if !strings.Contains(evidence.Findings[0].Remediation, "/repo/package-lock.json") || !strings.Contains(evidence.Findings[0].Remediation, "npm/left-pad") {
		t.Fatalf("expected osv-scanner remediation, got %#v", evidence.Findings[0])
	}
	if len(evidence.Findings[0].FixedVersions) != 1 || evidence.Findings[0].FixedVersions[0] != "1.0.1" || !strings.Contains(evidence.Findings[0].Remediation, "1.0.1") {
		t.Fatalf("expected fixed-version scanner remediation, got %#v", evidence.Findings[0])
	}
	if evidence.Findings[0].Severity != "CVSS:9.8" {
		t.Fatalf("expected group severity fallback, got %#v", evidence.Findings[0])
	}
	if evidence.Findings[0].DependencyKind != "direct" || !containsString(evidence.Findings[0].Evidence, "direct-dependency") || !containsString(evidence.Findings[0].Evidence, "source-type:lockfile") || !strings.Contains(evidence.Findings[0].Reason, "directly managed") {
		t.Fatalf("expected direct dependency scanner evidence, got %#v", evidence.Findings[0])
	}
	if evidence.Findings[0].ReasonCode != securityreason.ScannerVulnerability || evidence.Findings[0].ReasonArgs["tool"] != "osv-scanner" || evidence.Findings[0].ReasonArgs["dependency_kind"] != "direct" {
		t.Fatalf("expected structured scanner vulnerability reason, got %#v", evidence.Findings[0])
	}
	want := []string{"osv-scanner", "scan", "source", "--format=json", "--verbosity=error", "--recursive", "/repo"}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected scanner command: %#v", fake.calls)
	}
}

func TestOSVScannerEvidenceMarksTransitiveDependencies(t *testing.T) {
	report := osvScannerReport{Results: []osvScannerResult{{
		Source: osvScannerSource{Path: "/repo/package-lock.json", Type: "lockfile"},
		Packages: []osvScannerPackage{{
			Package: osvScannerPackageInfo{Name: "left-pad", Version: "1.0.0", Ecosystem: "npm"},
			Vulnerabilities: []osvScannerVuln{{
				ID: "GHSA-transitive",
			}},
		}},
	}}}
	evidence := osvScannerEvidenceFromReport(scannerEvidence{Tool: "osv-scanner", Target: "/repo", Status: plan.StatusOK, Decision: "allow"}, report, []securityPackage{{Ecosystem: "npm", Package: "pnpm", Version: "11.1.2"}})
	if len(evidence.Findings) != 1 || evidence.Findings[0].DependencyKind != "transitive" {
		t.Fatalf("expected transitive dependency scanner finding, got %#v", evidence)
	}
	if !containsString(evidence.Findings[0].Evidence, "transitive-dependency") || !strings.Contains(evidence.Findings[0].Reason, "transitive dependency") {
		t.Fatalf("expected transitive dependency evidence, got %#v", evidence.Findings[0])
	}
	if evidence.Findings[0].ReasonCode != securityreason.ScannerVulnerability || evidence.Findings[0].ReasonArgs["dependency_kind"] != "transitive" {
		t.Fatalf("expected structured transitive scanner reason, got %#v", evidence.Findings[0])
	}
}

func TestTrivyEvidenceAddsStructuredVulnerabilityReason(t *testing.T) {
	vuln := trivyVulnerability{
		VulnerabilityID:  "CVE-2026-0001",
		PkgName:          "left-pad",
		InstalledVersion: "1.0.0",
		FixedVersion:     "1.0.1",
	}
	report := trivyReport{Results: []trivyResult{{
		Target:          "/repo/package-lock.json",
		Type:            "npm",
		Vulnerabilities: []trivyVulnerability{vuln},
	}}}
	evidence := trivyEvidenceFromReport(scannerEvidence{Tool: "trivy", Target: "/repo", Status: plan.StatusOK, Decision: "allow"}, report, []securityPackage{{Ecosystem: "npm", Package: "left-pad", Version: "1.0.0"}})
	if len(evidence.Findings) != 1 {
		t.Fatalf("expected one trivy finding, got %#v", evidence)
	}
	finding := evidence.Findings[0]
	if finding.ReasonCode != securityreason.ScannerVulnerability || finding.ReasonArgs["tool"] != "trivy" || finding.ReasonArgs["dependency_kind"] != "direct" {
		t.Fatalf("expected structured trivy vulnerability reason, got %#v", finding)
	}
}

func TestGrypeEvidenceAddsStructuredCPEVulnerabilityReason(t *testing.T) {
	report := grypeReport{Matches: []grypeMatch{{
		Vulnerability: grypeVulnerability{ID: "CVE-2026-0002"},
		Artifact: grypeArtifact{
			Name:      "left-pad",
			Version:   "1.0.0",
			Type:      "npm",
			Locations: []grypeLocation{{Path: "/repo/package-lock.json"}},
		},
		MatchDetails: []grypeMatchDetail{{Type: "cpe-match"}},
	}}}
	evidence := grypeEvidenceFromReport(scannerEvidence{Tool: "grype", Target: "/repo", Status: plan.StatusOK, Decision: "allow"}, report, []securityPackage{{Ecosystem: "npm", Package: "left-pad", Version: "1.0.0"}})
	if len(evidence.Findings) != 1 {
		t.Fatalf("expected one grype finding, got %#v", evidence)
	}
	finding := evidence.Findings[0]
	if finding.ReasonCode != securityreason.ScannerVulnerability || finding.ReasonArgs["tool"] != "grype" || finding.ReasonArgs["dependency_kind"] != "direct" || finding.ReasonArgs["match_style"] != "cpe" {
		t.Fatalf("expected structured grype CPE vulnerability reason, got %#v", finding)
	}
}

func TestOSVScannerRemediationUsesSourceSpecificGuidance(t *testing.T) {
	cases := []struct {
		name      string
		pkg       osvScannerPackageInfo
		source    osvScannerSource
		want      string
		notWanted string
	}{
		{
			name:   "gradle",
			pkg:    osvScannerPackageInfo{Name: "org.example:demo", Ecosystem: "Maven"},
			source: osvScannerSource{Path: "/repo/build.gradle.kts", Type: "manifest"},
			want:   "Gradle dependencyInsight",
		},
		{
			name:      "maven",
			pkg:       osvScannerPackageInfo{Name: "org.example:demo", Ecosystem: "Maven"},
			source:    osvScannerSource{Path: "/repo/pom.xml", Type: "manifest"},
			want:      "Maven dependency tree",
			notWanted: "Gradle dependencyInsight",
		},
		{
			name:   "nuget central management",
			pkg:    osvScannerPackageInfo{Name: "Example.Package", Ecosystem: "NuGet"},
			source: osvScannerSource{Path: "/repo/Directory.Packages.props", Type: "manifest"},
			want:   "central package management entry",
		},
		{
			name:   "pnpm",
			pkg:    osvScannerPackageInfo{Name: "left-pad", Ecosystem: "npm"},
			source: osvScannerSource{Path: "/repo/pnpm-lock.yaml", Type: "lockfile"},
			want:   "pnpm audit/update",
		},
		{
			name:   "bun",
			pkg:    osvScannerPackageInfo{Name: "left-pad", Ecosystem: "npm"},
			source: osvScannerSource{Path: "/repo/bun.lockb", Type: "lockfile"},
			want:   "bun audit/update",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := osvScannerRemediation(nil, tc.pkg, tc.source)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected remediation to contain %q, got %q", tc.want, got)
			}
			if tc.notWanted != "" && strings.Contains(got, tc.notWanted) {
				t.Fatalf("expected remediation not to contain %q, got %q", tc.notWanted, got)
			}
		})
	}
}

func TestPrintSecurityTextIncludesScannerAttention(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status:  plan.StatusHeld,
		Root:    "/repo",
		Sources: []string{"osv-scanner"},
		Scanners: []scannerEvidence{{
			Tool:               "osv-scanner",
			Status:             plan.StatusHeld,
			Decision:           "hold",
			Reason:             "osv-scanner reported vulnerabilities",
			FindingCount:       1,
			VulnerabilityCount: 1,
			Findings: []scannerFinding{{
				Kind:           "vulnerability",
				SourcePath:     "/repo/package-lock.json",
				Package:        "left-pad",
				Version:        "1.0.0",
				VulnID:         "GHSA-0000-0000-0000",
				DependencyKind: "direct",
				FixedVersions:  []string{"1.0.1"},
				Remediation:    "update the affected lockfile",
			}},
		}},
	}, false)
	got := buffer.String()
	if !strings.Contains(got, "scanners: 1 checks, 1 held") || !strings.Contains(got, "osv-scanner reported vulnerabilities") || !strings.Contains(got, "GHSA-0000-0000-0000") || !strings.Contains(got, "direct") || !strings.Contains(got, "1.0.1") || !strings.Contains(got, "scanner next steps") || !strings.Contains(got, "update the affected lockfile") {
		t.Fatalf("expected scanner attention in text output, got %q", got)
	}
}

func TestPrintSecurityTextIncludesPostureNextSteps(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status: plan.StatusHeld,
		Root:   "/repo",
		Posture: []githubPosture{{
			Repository:  "owner/tool",
			Decision:    "review",
			Reason:      "repository is archived",
			Remediation: "replace the archived repository source",
		}},
		Brew: []homebrewPosture{{
			Kind:        "cask",
			Name:        "demo",
			Decision:    "review",
			Reason:      "needs provenance review",
			Remediation: "review vendor homepage and download host",
		}},
		VSCode: []vscodePosture{{
			Name:        "publisher.extension",
			Decision:    "review",
			Reason:      "publisher domain is not verified",
			Remediation: "verify publisher identity",
		}},
		NPM: []npmPosture{{
			Package:     "tool",
			Decision:    "review",
			Reason:      "npm package has no maintainers",
			Remediation: "review package ownership",
		}},
		Cargo: []cargoPosture{{
			Crate:       "fd-find",
			Decision:    "review",
			Reason:      "installed crate version is yanked",
			Remediation: "update to a non-yanked crate version",
		}},
		PyPI: []pypiPosture{{
			Package:     "frogmouth",
			Decision:    "review",
			Reason:      "installed PyPI version is yanked",
			Remediation: "update to a non-yanked PyPI release",
		}},
	}, false)
	got := buffer.String()
	for _, want := range []string{"github posture next steps", "replace the archived repository source", "homebrew posture next steps", "review vendor homepage", "vscode posture next steps", "verify publisher identity", "npm posture next steps", "review package ownership", "cargo posture next steps", "non-yanked crate", "pypi posture next steps", "non-yanked PyPI"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected posture next step %q in output, got %q", want, got)
		}
	}
}

func TestRunGitleaksDirScanParsesRedactedFindings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `[{
  "Description": "Generic API Key",
  "File": ".env",
  "StartLine": 3,
  "EndLine": 3,
  "RuleID": "generic-api-key",
  "Fingerprint": ".env:generic-api-key:3"
}]`,
	}}
	evidence := runGitleaksDirScan(context.Background(), fake, "/repo")
	if evidence.Status != plan.StatusHeld || evidence.Decision != "hold" || evidence.FindingCount != 1 || evidence.VulnerabilityCount != 0 {
		t.Fatalf("expected held gitleaks evidence, got %#v", evidence)
	}
	if len(evidence.Findings) != 1 || evidence.Findings[0].Kind != "secret" || evidence.Findings[0].RuleID != "generic-api-key" || evidence.Findings[0].File != ".env" {
		t.Fatalf("expected redacted secret finding metadata, got %#v", evidence.Findings)
	}
	if evidence.Findings[0].Decision != "hold" || evidence.Findings[0].Confidence == "" {
		t.Fatalf("expected finding decision metadata, got %#v", evidence.Findings[0])
	}
	if evidence.Findings[0].ReasonCode != securityreason.ScannerSecret || evidence.Findings[0].ReasonArgs["tool"] != "gitleaks" {
		t.Fatalf("expected structured gitleaks secret reason, got %#v", evidence.Findings[0])
	}
	if !strings.Contains(evidence.Findings[0].Remediation, "revoke") {
		t.Fatalf("expected gitleaks remediation, got %#v", evidence.Findings[0])
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "gitleaks" || fake.calls[0][1] != "dir" || !containsString(fake.calls[0], "--redact") || !containsString(fake.calls[0], "--report-path") {
		t.Fatalf("unexpected gitleaks command: %#v", fake.calls)
	}
}

func TestSortScannerFindingsPrioritizesAttention(t *testing.T) {
	findings := []scannerFinding{
		{Kind: "workflow", RuleID: "zizmor", Decision: "hold"},
		{Kind: "vulnerability", VulnID: "low", Decision: "hold", Severity: "CVSS_V3:4.0"},
		{Kind: "vulnerability", VulnID: "transitive", Decision: "hold", Severity: "CVSS_V3:5.0", DependencyKind: "transitive"},
		{Kind: "vulnerability", VulnID: "direct", Decision: "hold", Severity: "CVSS_V3:5.0", DependencyKind: "direct"},
		{Kind: "secret", RuleID: "secret", Decision: "review"},
		{Kind: "vulnerability", VulnID: "critical", Decision: "hold", Severity: "CVSS_V3:9.8", FixedVersions: []string{"1.0.1"}},
		{Kind: "secret", RuleID: "allowed", Decision: "allow"},
	}
	sortScannerFindings(findings)
	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		got = append(got, scannerFindingID(finding))
	}
	if strings.Join(got, ",") != "critical,direct,transitive,low,zizmor,secret,allowed" {
		t.Fatalf("unexpected scanner finding order: %#v", got)
	}
}

func TestRunZizmorWorkflowScanParsesFindings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `[{
  "ident": "template-injection",
  "desc": "code injection through template expansion",
  "url": "https://docs.zizmor.sh/audits/#template-injection",
  "determinations": {"confidence": "High", "severity": "High"},
  "locations": [{
    "symbolic": {"key": {"Local": {"given_path": ".github/workflows/ci.yml"}}},
    "concrete": {"location": {"start_point": {"row": 9}, "end_point": {"row": 10}}}
  }],
  "ignored": false
}]`,
	}}
	evidence := runZizmorWorkflowScan(context.Background(), fake, "/repo")
	if evidence.Status != plan.StatusHeld || evidence.Decision != "hold" || evidence.FindingCount != 1 {
		t.Fatalf("expected held zizmor evidence, got %#v", evidence)
	}
	if len(evidence.Findings) != 1 || evidence.Findings[0].Kind != "workflow" || evidence.Findings[0].RuleID != "template-injection" || evidence.Findings[0].StartLine != 10 {
		t.Fatalf("expected parsed workflow finding, got %#v", evidence.Findings)
	}
	if evidence.Findings[0].ReasonCode != securityreason.ScannerWorkflow || evidence.Findings[0].ReasonArgs["tool"] != "zizmor" {
		t.Fatalf("expected structured zizmor workflow reason, got %#v", evidence.Findings[0])
	}
	want := []string{"zizmor", "--format=json-v1", "--offline", "--collect=workflows", "/repo"}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected zizmor command: %#v", fake.calls)
	}
}

func TestRunTrivyFilesystemScanParsesFindings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "Results": [
    {
      "Target": "package-lock.json",
      "Type": "npm",
      "Vulnerabilities": [{
        "VulnerabilityID": "CVE-2026-0001",
        "PkgName": "left-pad",
        "InstalledVersion": "1.0.0",
        "FixedVersion": "1.0.1, 1.0.2",
        "Severity": "CRITICAL",
        "Title": "left-pad issue",
        "PrimaryURL": "https://avd.aquasec.com/nvd/cve-2026-0001",
        "PkgIdentifier": {"PURL": "pkg:npm/left-pad@1.0.0"}
      }]
    },
    {
      "Target": ".github/workflows/ci.yml",
      "Type": "yaml",
      "Misconfigurations": [{
        "ID": "AVD-GHA-0001",
        "Title": "workflow issue",
        "Severity": "HIGH",
        "Status": "FAIL",
        "Resolution": "pin the action",
        "CauseMetadata": {"StartLine": 4, "EndLine": 5}
      }],
      "Secrets": [{
        "RuleID": "generic-api-key",
        "Category": "API Key",
        "Severity": "HIGH",
        "Title": "API key",
        "StartLine": 8,
        "EndLine": 8
      }]
    }
  ]
}`,
	}}
	evidence := runTrivyFilesystemScan(context.Background(), fake, "/repo", []securityPackage{{Ecosystem: "npm", Package: "left-pad", Version: "1.0.0"}})
	if evidence.Status != plan.StatusHeld || evidence.Decision != "hold" || evidence.FindingCount != 3 || evidence.VulnerabilityCount != 1 || evidence.PackageCount != 1 {
		t.Fatalf("expected held trivy evidence, got %#v", evidence)
	}
	var vulnerability scannerFinding
	var misconfiguration scannerFinding
	var secret scannerFinding
	for _, finding := range evidence.Findings {
		switch {
		case finding.VulnID == "CVE-2026-0001":
			vulnerability = finding
		case finding.RuleID == "AVD-GHA-0001":
			misconfiguration = finding
		case finding.RuleID == "generic-api-key":
			secret = finding
		}
	}
	if vulnerability.VulnID == "" || !containsString(vulnerability.FixedVersions, "1.0.1") || !containsString(vulnerability.FixedVersions, "1.0.2") {
		t.Fatalf("expected parsed vulnerability, got %#v", evidence.Findings)
	}
	if vulnerability.DependencyKind != "direct" || vulnerability.Ecosystem != "npm" || !containsString(vulnerability.Evidence, "direct-dependency") {
		t.Fatalf("expected direct trivy dependency evidence, got %#v", vulnerability)
	}
	if misconfiguration.ReasonCode != securityreason.ScannerMisconfiguration || misconfiguration.ReasonArgs["tool"] != "trivy" {
		t.Fatalf("expected structured trivy misconfiguration reason, got %#v", misconfiguration)
	}
	if secret.ReasonCode != securityreason.ScannerSecret || secret.ReasonArgs["tool"] != "trivy" {
		t.Fatalf("expected structured trivy secret reason, got %#v", secret)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "trivy" || !containsString(fake.calls[0], "fs") || !containsString(fake.calls[0], "--format") || !containsString(fake.calls[0], "json") {
		t.Fatalf("unexpected trivy command: %#v", fake.calls)
	}
}

func TestRunGrypeDirectoryScanParsesFindings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "matches": [
    {
      "vulnerability": {
        "id": "CVE-2026-0002",
        "severity": "High",
        "description": "demo vulnerability",
        "fix": {"versions": ["1.0.1"], "state": "fixed"},
        "urls": ["https://github.com/advisories/GHSA-demo"]
      },
      "artifact": {
        "name": "left-pad",
        "version": "1.0.0",
        "type": "npm",
        "purl": "pkg:npm/left-pad@1.0.0",
        "locations": [{"path": "package-lock.json"}]
      },
      "matchDetails": [{"type": "exact-direct-match", "matcher": "javascript-matcher"}]
    },
    {
      "vulnerability": {
        "id": "CVE-2026-0003",
        "severity": "Low",
        "description": "cpe vulnerability"
      },
      "artifact": {
        "name": "demo",
        "version": "2.0.0",
        "type": "binary",
        "locations": [{"path": "/usr/local/bin/demo"}]
      },
      "matchDetails": [{"type": "cpe-match", "matcher": "stock-matcher"}]
    }
  ]
}`,
	}}
	evidence := runGrypeDirectoryScan(context.Background(), fake, "/repo", []securityPackage{{Ecosystem: "npm", Package: "left-pad", Version: "1.0.0"}})
	if evidence.Status != plan.StatusHeld || evidence.Decision != "hold" || evidence.FindingCount != 2 || evidence.VulnerabilityCount != 2 || evidence.PackageCount != 2 || evidence.SourceCount != 2 {
		t.Fatalf("expected held grype evidence, got %#v", evidence)
	}
	var direct scannerFinding
	var cpe scannerFinding
	for _, finding := range evidence.Findings {
		switch finding.VulnID {
		case "CVE-2026-0002":
			direct = finding
		case "CVE-2026-0003":
			cpe = finding
		}
	}
	if direct.VulnID == "" || direct.Package != "left-pad" || direct.SourcePath != "package-lock.json" || !containsString(direct.FixedVersions, "1.0.1") {
		t.Fatalf("expected parsed direct grype finding, got %#v", evidence.Findings)
	}
	if direct.DependencyKind != "direct" || direct.Ecosystem != "npm" || !containsString(direct.Evidence, "direct-dependency") {
		t.Fatalf("expected direct grype dependency evidence, got %#v", direct)
	}
	if cpe.VulnID == "" || cpe.Confidence != "low" || !containsString(cpe.Evidence, "cpe-match") {
		t.Fatalf("expected lower-confidence cpe grype finding, got %#v", evidence.Findings)
	}
	want := []string{"grype", "-o", "json", "dir:/repo"}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected grype command: %#v", fake.calls)
	}
}

func TestParseScannerPURL(t *testing.T) {
	tests := []struct {
		name      string
		purl      string
		ecosystem string
		pkg       string
		version   string
	}{
		{name: "npm", purl: "pkg:npm/left-pad@1.0.0", ecosystem: "npm", pkg: "left-pad", version: "1.0.0"},
		{name: "npm scoped", purl: "pkg:npm/%40scope/name@2.0.0", ecosystem: "npm", pkg: "@scope/name", version: "2.0.0"},
		{name: "cargo", purl: "pkg:cargo/ripgrep@14.1.1", ecosystem: "crates.io", pkg: "ripgrep", version: "14.1.1"},
		{name: "maven", purl: "pkg:maven/org.apache.commons/commons-lang3@3.14.0", ecosystem: "Maven", pkg: "org.apache.commons:commons-lang3", version: "3.14.0"},
		{name: "composer", purl: "pkg:composer/symfony/console@7.0.0", ecosystem: "Packagist", pkg: "symfony/console", version: "7.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScannerPURL(tt.purl)
			if got.Ecosystem != tt.ecosystem || got.Name != tt.pkg || got.Version != tt.version {
				t.Fatalf("unexpected parsed PURL: %#v", got)
			}
		})
	}
}

func TestScannerEvidenceSkipsZizmorWithoutWorkflows(t *testing.T) {
	root := t.TempDir()
	scanners := scannerEvidenceFromOptions(context.Background(), &fakeCommandRunner{result: runner.Result{}}, securityOptions{root: root}, nil)
	for _, scanner := range scanners {
		if scanner.Tool == "zizmor" {
			t.Fatalf("expected no zizmor scanner without workflow files, got %#v", scanners)
		}
	}
}

func TestSecurityScannerToolsSupportsExplicitFilters(t *testing.T) {
	root := t.TempDir()
	got := securityScannerTools("none", root)
	if len(got) != 0 {
		t.Fatalf("expected none scanner to disable scanners, got %#v", got)
	}
	got = securityScannerTools("osv,gitleaks,secrets,trivy-fs,anchore-grype", root)
	want := []string{"osv-scanner", "gitleaks", "trivy", "grype"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected scanner tools: %#v", got)
	}
	got = securityScannerTools("all", root)
	want = []string{"osv-scanner", "gitleaks", "zizmor", "trivy", "grype"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected all scanner tools: %#v", got)
	}
}

func TestScannerEvidenceRunsForProjectProviderSelection(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCommandRunner{result: runner.Result{Code: 127, Stderr: "scanner not found"}}
	scanners := scannerEvidenceFromOptions(context.Background(), fake, securityOptions{root: root, provider: "all", scanner: "all"}, nil)
	if len(scanners) != 5 {
		t.Fatalf("expected all scanners for provider all, got %#v", scanners)
	}
	gotTools := make([]string, 0, len(scanners))
	for _, scanner := range scanners {
		gotTools = append(gotTools, scanner.Tool)
	}
	wantTools := []string{"osv-scanner", "gitleaks", "zizmor", "trivy", "grype"}
	if strings.Join(gotTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("expected stable scanner order %v, got %v", wantTools, gotTools)
	}
	scanners = scannerEvidenceFromOptions(context.Background(), fake, securityOptions{root: root, provider: "project", scanner: "osv"}, nil)
	if len(scanners) != 1 || scanners[0].Tool != "osv-scanner" {
		t.Fatalf("expected osv scanner for provider project, got %#v", scanners)
	}
	scanners = scannerEvidenceFromOptions(context.Background(), fake, securityOptions{root: root, provider: "brew", scanner: "all"}, nil)
	if len(scanners) != 0 {
		t.Fatalf("expected no project scanners for provider brew, got %#v", scanners)
	}
}

func TestScannerEvidenceRunsZizmorWithWorkflows(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "ci.yml"), []byte("name: ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanners := scannerEvidenceFromOptions(context.Background(), &fakeCommandRunner{result: runner.Result{Stdout: `[]`}}, securityOptions{root: root}, nil)
	found := false
	for _, scanner := range scanners {
		if scanner.Tool == "zizmor" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected zizmor scanner with workflow files, got %#v", scanners)
	}
}

func TestRunOSVScannerSourceScanUnavailableDoesNotHoldReport(t *testing.T) {
	evidence := runOSVScannerSourceScan(context.Background(), &fakeCommandRunner{result: runner.Result{
		Code:   127,
		Stderr: "osv-scanner: command not found",
	}}, "/repo", nil)
	if evidence.Status != plan.StatusUnavailable || evidence.Decision != "review" || evidence.UnavailableReason != securityscanner.FailureMissingBinary {
		t.Fatalf("expected unavailable scanner evidence, got %#v", evidence)
	}
	status := scannerEvidenceReportStatus(plan.StatusOK, []scannerEvidence{evidence})
	if status != plan.StatusOK {
		t.Fatalf("expected unavailable scanner evidence not to change report status, got %s", status)
	}
}

func TestScannerEvidenceClassifiesUnavailableAndParseFailures(t *testing.T) {
	root := t.TempDir()
	gitleaks := runGitleaksDirScan(context.Background(), &fakeCommandRunner{result: runner.Result{
		Code:   127,
		Stderr: "gitleaks: command not found",
	}}, root)
	if gitleaks.Status != plan.StatusUnavailable || gitleaks.UnavailableReason != securityscanner.FailureMissingBinary {
		t.Fatalf("expected gitleaks missing-binary evidence, got %#v", gitleaks)
	}

	zizmor := runZizmorWorkflowScan(context.Background(), &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stderr: "GitHub API rate limit exceeded",
	}}, root)
	if zizmor.Status != plan.StatusUnavailable || zizmor.UnavailableReason != securityscanner.FailureRateLimit {
		t.Fatalf("expected zizmor rate-limit evidence, got %#v", zizmor)
	}

	grype := runGrypeDirectoryScan(context.Background(), &fakeCommandRunner{result: runner.Result{
		Stdout: `{not-json`,
	}}, root, nil)
	if grype.Status != plan.StatusError || grype.ErrorKind != securityscanner.FailureParseFailure {
		t.Fatalf("expected grype parse-failure evidence, got %#v", grype)
	}
}
