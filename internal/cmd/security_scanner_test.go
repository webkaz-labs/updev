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
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
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
		"repository が archived です",
		"非公式 Homebrew tap は配布元の確認が必",
		"Marketplace metadata で publisher",
		"npm package の maintainers が registry",
		"インストール済み crate version は yanke",
		"インストール済み PyPI version は yanked",
		"npm audit が vulnerability を報告しました",
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
	finding := applyBrewSafetyAdvisory(safetyFinding{Provider: "brew", Kind: "brew", Name: "pnpm"}, securityFinding{
		Ecosystem: "npm",
		VulnID:    "GHSA-pnpm",
	})
	if finding.Decision != "hold" || !containsString(finding.Evidence, "osv-curated-homebrew-map") || !strings.Contains(finding.Reason, "curated Homebrew mapping") {
		t.Fatalf("expected curated mapping advisory evidence, got %#v", finding)
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
	for _, finding := range evidence.Findings {
		if finding.VulnID == "CVE-2026-0001" {
			vulnerability = finding
		}
	}
	if vulnerability.VulnID == "" || !containsString(vulnerability.FixedVersions, "1.0.1") || !containsString(vulnerability.FixedVersions, "1.0.2") {
		t.Fatalf("expected parsed vulnerability, got %#v", evidence.Findings)
	}
	if vulnerability.DependencyKind != "direct" || vulnerability.Ecosystem != "npm" || !containsString(vulnerability.Evidence, "direct-dependency") {
		t.Fatalf("expected direct trivy dependency evidence, got %#v", vulnerability)
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
	if evidence.Status != plan.StatusUnavailable || evidence.Decision != "review" {
		t.Fatalf("expected unavailable scanner evidence, got %#v", evidence)
	}
	status := scannerEvidenceReportStatus(plan.StatusOK, []scannerEvidence{evidence})
	if status != plan.StatusOK {
		t.Fatalf("expected unavailable scanner evidence not to change report status, got %s", status)
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

func TestSecurityPackageFromMiseItems(t *testing.T) {
	items := []plan.Item{
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2"},
		{Provider: "mise", Name: "pipx:frogmouth", Version: "0.9.2"},
		{Provider: "mise", Name: "github:owner/tool", Version: "1.0.0"},
	}
	packages := securityPackagesFromItems(items)
	if len(packages) != 3 {
		t.Fatalf("expected three high-confidence packages, got %#v", packages)
	}
	if packages[0].Ecosystem != "PyPI" || packages[0].Package != "frogmouth" {
		t.Fatalf("unexpected PyPI package mapping: %#v", packages[0])
	}
	if packages[1].Ecosystem != "crates.io" || packages[1].Package != "fd-find" {
		t.Fatalf("unexpected crates package mapping: %#v", packages[1])
	}
	if packages[2].Ecosystem != "npm" || packages[2].Package != "pnpm" {
		t.Fatalf("unexpected npm package mapping: %#v", packages[2])
	}
}

func TestAnnotateSecurityPackagePathUsesConservativeBinaryCandidates(t *testing.T) {
	binDir := t.TempDir()
	binaryPath := filepath.Join(binDir, "pnpm")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cargoBinaryPath := filepath.Join(binDir, "fd")
	if err := os.WriteFile(cargoBinaryPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	packages := annotateSecurityPackagePath([]securityPackage{
		{Ecosystem: "npm", Package: "pnpm"},
		{Ecosystem: "npm", Package: "@scope/tool"},
		{Ecosystem: "crates.io", Package: "fd-find"},
	}, []npmPosture{
		{Package: "pnpm", Binaries: []string{"pn", "pnpm"}},
		{Package: "@scope/tool", Binaries: []string{"scope-tool"}},
	})
	if packages[0].BinaryName != "pnpm" || packages[0].PathState != "on-path" || packages[0].BinaryPath == "" {
		t.Fatalf("expected npm package on PATH, got %#v", packages[0])
	}
	if packages[1].PathState != "not-found" || packages[1].BinaryName != "scope-tool" {
		t.Fatalf("expected scoped npm registry binary to be used, got %#v", packages[1])
	}
	if packages[2].PathState != "on-path" || packages[2].BinaryName != "fd" || packages[2].BinaryPath == "" {
		t.Fatalf("expected cargo binary to be found conservatively, got %#v", packages[2])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func TestSecurityScopeReportsSkippedAutomaticMappingGaps(t *testing.T) {
	items := []plan.Item{
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "npm:missing-version", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "github:owner/tool", Version: "1.0.0", Kind: "tool", Category: "github"},
		{Provider: "mise", Name: "aqua:owner/tool", Version: "1.0.0", Kind: "tool", Category: "aqua"},
		{Provider: "brew", Name: "jq", Version: "1.8.1", Kind: "brew"},
		{Provider: "brew", Name: "github.copilot", Kind: "vscode"},
	}
	packages, skipped := securityScopeFromItems(items)
	if len(packages) != 1 || packages[0].Package != "pnpm" {
		t.Fatalf("unexpected packages: %#v", packages)
	}
	reasons := map[string]int{}
	for _, entry := range skipped {
		reasons[entry.Reason] += entry.Count
	}
	if reasons["missing version for high-confidence ecosystem"] != 1 {
		t.Fatalf("expected missing version skip, got %#v", skipped)
	}
	if reasons["unsupported mise backend ecosystem"] != 1 {
		t.Fatalf("expected unsupported mise backend skip, got %#v", skipped)
	}
	if reasons["homebrew requires curated advisory mapping"] != 1 {
		t.Fatalf("expected homebrew mapping skip, got %#v", skipped)
	}
	for _, entry := range skipped {
		if entry.Reason == "homebrew requires curated advisory mapping" && !containsString(entry.Examples, "jq") {
			t.Fatalf("expected skipped examples to include jq, got %#v", entry)
		}
	}
	if reasons["vscode extensions require marketplace advisory mapping"] != 1 {
		t.Fatalf("expected vscode mapping skip, got %#v", skipped)
	}
}

func TestGitHubRepoFromMiseName(t *testing.T) {
	repo, ok := githubRepoFromMiseName("github:owner/tool@1.2.3")
	if !ok || repo != "owner/tool" {
		t.Fatalf("unexpected repo parse: %q %v", repo, ok)
	}
	if _, ok := githubRepoFromMiseName("github:owner/tool;rm"); ok {
		t.Fatal("expected unsafe repository name to be rejected")
	}
}

func TestGitHubTokenPrefersEnvironment(t *testing.T) {
	t.Setenv("UPDEV_GITHUB_TOKEN", "updev-token")
	t.Setenv("GITHUB_API_TOKEN", "github-api-token")
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("GH_TOKEN", "gh-token")
	if got := githubToken(); got != "updev-token" {
		t.Fatalf("expected UPDEV_GITHUB_TOKEN to win, got %q", got)
	}
	t.Setenv("UPDEV_GITHUB_TOKEN", "")
	if got := githubToken(); got != "github-api-token" {
		t.Fatalf("expected GITHUB_API_TOKEN to win after UPDEV_GITHUB_TOKEN, got %q", got)
	}
}

func TestFetchNPMMetadataUsesSecurityMetadataCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/left-pad" {
			t.Fatalf("unexpected npm registry path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "name": "left-pad",
  "description": "cached package",
  "dist-tags": {"latest": "1.0.0"},
  "versions": {"1.0.0": {"version": "1.0.0"}}
}`))
	}))
	defer server.Close()
	first, err := fetchNPMMetadata(context.Background(), server.Client(), server.URL, "left-pad")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetchNPMMetadata(context.Background(), server.Client(), server.URL, "left-pad")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("expected second metadata fetch to use cache, got %d requests", requests)
	}
	if first.Name != "left-pad" || second.Description != "cached package" {
		t.Fatalf("unexpected cached metadata: %#v %#v", first, second)
	}
}

func TestFetchGitHubRepositoryUsesMetadataCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/repos/owner/tool" {
			t.Fatalf("unexpected GitHub path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "full_name": "owner/tool",
  "html_url": "https://github.com/owner/tool",
  "default_branch": "main",
  "stargazers_count": 42
}`))
	}))
	defer server.Close()
	first, err := fetchGitHubRepository(context.Background(), server.Client(), server.URL, "owner/tool")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetchGitHubRepository(context.Background(), server.Client(), server.URL, "owner/tool")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("expected second GitHub fetch to use cache, got %d requests", requests)
	}
	if first.FullName != "owner/tool" || second.StargazersCount != 42 {
		t.Fatalf("unexpected cached GitHub repo: %#v %#v", first, second)
	}
}

func TestGitHubPosturesFromItemsReportsArchivedRepos(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "full_name": "owner/tool",
  "html_url": "https://github.com/owner/tool",
  "default_branch": "main",
  "archived": true,
  "pushed_at": "2026-05-20T00:00:00Z",
  "updated_at": "2026-05-21T00:00:00Z",
  "open_issues_count": 7,
  "stargazers_count": 42
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "github:owner/tool", Version: "1.0.0", Kind: "tool", Category: "github"},
		{Provider: "mise", Name: "github:owner/tool", Version: "1.0.0", Kind: "tool", Category: "github"},
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
	}
	postures, err := githubPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/owner/tool" {
		t.Fatalf("unexpected GitHub path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one unique repo posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || postures[0].Reason != "repository is archived" {
		t.Fatalf("expected archived repo review, got %#v", postures[0])
	}
	if !strings.Contains(postures[0].Remediation, "archived") {
		t.Fatalf("expected GitHub posture remediation, got %#v", postures[0])
	}
	if postures[0].StargazersCount != 42 || postures[0].OpenIssuesCount != 7 {
		t.Fatalf("expected GitHub metadata, got %#v", postures[0])
	}
}

func TestGitHubPostureReviewsDisabledSecurityFeatures(t *testing.T) {
	posture := githubPostureFromRepo("mise", "github:owner/tool", "owner/tool", githubRepository{
		FullName: "owner/tool",
		SecurityAndAnalysis: githubSecurityAndAnalysis{
			DependabotSecurityUpdates: githubSecurityFeature{Status: "disabled"},
			SecretScanning:            githubSecurityFeature{Status: "enabled"},
		},
	})
	if posture.Decision != "review" || posture.Reason != "Dependabot security updates are disabled" {
		t.Fatalf("expected disabled dependabot review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "Dependabot") {
		t.Fatalf("expected dependabot remediation, got %#v", posture)
	}
	if posture.SecretScanning != "enabled" || posture.DependabotSecurityUpdates != "disabled" {
		t.Fatalf("expected security feature evidence, got %#v", posture)
	}
}

func TestGitHubPostureReviewsNewHomebrewTapRepository(t *testing.T) {
	t.Setenv("UPDEV_HOMEBREW_MIN_TAP_AGE_DAYS", "30")
	posture := githubPostureFromRepo("brew", "tap:vendor/tap", "vendor/homebrew-tap", githubRepository{
		FullName:  "vendor/homebrew-tap",
		HTMLURL:   "https://github.com/vendor/homebrew-tap",
		CreatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	})
	if posture.Decision != "review" || !strings.Contains(posture.Reason, "newly created") {
		t.Fatalf("expected new tap repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "minimum age") {
		t.Fatalf("expected new tap remediation, got %#v", posture)
	}
	if posture.RepositoryAgeDays != 2 || posture.MinRepositoryAgeDays != 30 || !containsString(posture.Evidence, "GitHub repository age") {
		t.Fatalf("expected tap repository age evidence, got %#v", posture)
	}
}

func TestGitHubPosturesFromItemsKeepsUnavailableReposAuditable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "github:owner/missing", Version: "1.0.0", Kind: "tool", Category: "github"},
	}
	postures, err := githubPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err == nil {
		t.Fatal("expected GitHub metadata warning error")
	}
	if len(postures) != 1 {
		t.Fatalf("expected unavailable repo posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || !strings.Contains(postures[0].Reason, "metadata unavailable") {
		t.Fatalf("expected unavailable repo review posture, got %#v", postures[0])
	}
}

func TestGitHubPosturesFromHomebrewUsesMetadataURLs(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "full_name": "owner/tool",
  "html_url": "https://github.com/owner/tool",
  "default_branch": "main",
  "archived": false,
  "pushed_at": "2026-05-20T00:00:00Z",
  "updated_at": "2026-05-21T00:00:00Z",
  "stargazers_count": 123
}`))
	}))
	defer server.Close()
	postures, err := githubPosturesFromHomebrew(context.Background(), server.Client(), server.URL, []homebrewPosture{
		{Kind: "brew", Name: "tool", URL: "https://github.com/owner/tool/archive/refs/tags/v1.0.0.tar.gz"},
		{Kind: "cask", Name: "tool-app", Homepage: "https://github.com/owner/tool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/owner/tool" {
		t.Fatalf("unexpected GitHub path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected deduplicated repo posture, got %#v", postures)
	}
	if postures[0].Provider != "brew" || postures[0].Name != "brew:tool" || postures[0].Repository != "owner/tool" {
		t.Fatalf("unexpected Homebrew GitHub posture: %#v", postures[0])
	}
}

func TestGitHubPosturesFromHomebrewUsesTapURLs(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "full_name": "vendor/homebrew-tap",
  "html_url": "https://github.com/vendor/homebrew-tap",
  "default_branch": "main",
  "archived": false,
  "pushed_at": "2026-05-20T00:00:00Z",
  "updated_at": "2026-05-21T00:00:00Z"
}`))
	}))
	defer server.Close()
	postures, err := githubPosturesFromHomebrew(context.Background(), server.Client(), server.URL, []homebrewPosture{
		homebrewTapPosture(plan.Item{Provider: "brew", Kind: "tap", Name: "vendor/tap"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/vendor/homebrew-tap" {
		t.Fatalf("unexpected GitHub path: %s", gotPath)
	}
	if len(postures) != 1 || postures[0].Name != "tap:vendor/tap" || postures[0].Repository != "vendor/homebrew-tap" {
		t.Fatalf("expected tap GitHub posture, got %#v", postures)
	}
}

func TestHomebrewPosturesFromItemsReportsMetadataRisks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`
brew "jq"
cask "visual-studio-code"
cask "vendor/tap/custom-app"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}},
  "deprecated": true,
  "deprecation_reason": "use yq"
}`))
		case "/cask/visual-studio-code.json":
			_, _ = w.Write([]byte(`{
  "token": "visual-studio-code",
  "name": ["Visual Studio Code"],
  "tap": "homebrew/cask",
  "homepage": "https://code.visualstudio.com/",
  "url": "https://update.code.visualstudio.com/latest/darwin/stable",
  "version": "1.101.0"
}`))
		default:
			t.Fatalf("unexpected Homebrew API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq"},
		{Provider: "brew", Kind: "cask", Name: "visual-studio-code"},
		{Provider: "brew", Kind: "cask", Name: "custom-app"},
		{Provider: "brew", Kind: "tap", Name: "vendor/tap"},
		{Provider: "brew", Kind: "tap", Name: "homebrew/core"},
	}
	postures, err := homebrewPosturesFromItems(context.Background(), server.Client(), server.URL, root, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(postures) != 5 {
		t.Fatalf("expected five Homebrew posture entries, got %#v", postures)
	}
	reasons := map[string]string{}
	for _, posture := range postures {
		reasons[posture.Name] = posture.Reason
	}
	if reasons["jq"] != "use yq" {
		t.Fatalf("expected deprecated formula reason, got %#v", postures)
	}
	if reasons["visual-studio-code"] != "Homebrew cask download host differs from homepage host; vendor provenance review required" {
		t.Fatalf("expected cask review reason, got %#v", postures)
	}
	if reasons["vendor/tap/custom-app"] != "non-official Homebrew tap needs provenance review" {
		t.Fatalf("expected custom tap review reason, got %#v", postures)
	}
	trustCommands := map[string]string{}
	for _, posture := range postures {
		trustCommands[posture.Name] = posture.TrustCommand
	}
	if trustCommands["vendor/tap/custom-app"] != "brew trust --cask vendor/tap/custom-app" ||
		trustCommands["vendor/tap"] != "brew trust --tap vendor/tap" {
		t.Fatalf("expected Homebrew 6 trust commands on non-official entries, got %#v", postures)
	}
	remediations := map[string]string{}
	for _, posture := range postures {
		remediations[posture.Name] = posture.Remediation
	}
	if !strings.Contains(remediations["visual-studio-code"], "visualstudio.com") ||
		!strings.Contains(remediations["visual-studio-code"], "update.code.visualstudio.com") ||
		!strings.Contains(remediations["vendor/tap"], "tap repository") {
		t.Fatalf("expected Homebrew posture remediation, got %#v", postures)
	}
	if reasons["vendor/tap"] != "non-official Homebrew tap needs provenance review" {
		t.Fatalf("expected custom tap review posture, got %#v", postures)
	}
	if reasons["homebrew/core"] != "" {
		t.Fatalf("expected official tap allow posture, got %#v", postures)
	}
	for _, posture := range postures {
		if posture.Name == "vendor/tap" && posture.URL != "https://github.com/vendor/homebrew-tap" {
			t.Fatalf("expected inferred tap GitHub URL, got %#v", posture)
		}
	}
}

func TestHomebrewAdvisoryPackagesFromPosturesMapsGitHubFormulaTags(t *testing.T) {
	packages := homebrewAdvisoryPackagesFromPostures([]homebrewPosture{
		{Kind: "brew", Name: "jq", Version: "1.8.1", URL: "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"},
		{Kind: "brew", Name: "jq", Version: "1.8.1", URL: "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"},
		{Kind: "cask", Name: "tool", URL: "https://github.com/owner/tool/releases/download/v1.0.0/tool.dmg"},
		{Kind: "brew", Name: "nongit", URL: "https://example.com/tool.tar.gz"},
		{Kind: "brew", Name: "pnpm", Version: "11.4.0", URL: "https://registry.npmjs.org/pnpm/-/pnpm-11.4.0.tgz"},
	})
	if len(packages) != 3 {
		t.Fatalf("expected three mapped Homebrew advisory packages, got %#v", packages)
	}
	if packages[0].Provider != "brew" || packages[0].Name != "jq" || packages[0].Ecosystem != "GIT" || packages[0].Package != "https://github.com/jqlang/jq.git" || packages[0].Version != "jq-1.8.1" || packages[0].Confidence != "medium" {
		t.Fatalf("unexpected Homebrew advisory package mapping: %#v", packages[0])
	}
	if packages[1].Provider != "brew" || packages[1].Name != "tool" || packages[1].Ecosystem != "GIT" || packages[1].Package != "https://github.com/owner/tool.git" || packages[1].Version != "v1.0.0" || packages[1].Confidence != "medium" {
		t.Fatalf("unexpected Homebrew cask advisory package mapping: %#v", packages[1])
	}
	if packages[2].Provider != "brew" || packages[2].Name != "pnpm" || packages[2].Ecosystem != "npm" || packages[2].Package != "pnpm" || packages[2].Version != "11.4.0" || packages[2].Confidence != "medium" {
		t.Fatalf("unexpected curated Homebrew advisory package mapping: %#v", packages[2])
	}
}

func TestVSCodePosturesFromItemsReportsMarketplaceRisks(t *testing.T) {
	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request vscodeMarketplaceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Filters) != 1 || len(request.Filters[0].Criteria) != 1 {
			t.Fatalf("unexpected marketplace request: %#v", request)
		}
		extension := request.Filters[0].Criteria[0].Value
		requested = append(requested, extension)
		switch extension {
		case "github.copilot":
			_, _ = w.Write([]byte(`{
  "results": [{
    "extensions": [{
      "publisher": {"publisherName": "github", "isDomainVerified": true},
      "extensionName": "copilot",
      "displayName": "GitHub Copilot",
      "flags": "validated, public",
      "lastUpdated": "2026-05-20T00:00:00Z",
      "publishedDate": "2021-06-29T00:00:00Z",
      "versions": [{"version": "1.388.0", "properties": [
        {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"},
        {"key": "Microsoft.VisualStudio.Services.Links.Source", "value": "https://github.com/github/copilot.vscode"}
      ]}],
      "statistics": [{"statisticName": "install", "value": 1000}, {"statisticName": "averagerating", "value": 4.2}]
    }]
  }]
}`))
		case "unknown.extension":
			_, _ = w.Write([]byte(`{"results":[{"extensions":[]}]}`))
		default:
			t.Fatalf("unexpected extension query: %s", extension)
		}
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "brew", Kind: "vscode", Name: "github.copilot"},
		{Provider: "brew", Kind: "vscode", Name: "unknown.extension"},
		{Provider: "brew", Kind: "cask", Name: "visual-studio-code"},
	}
	postures, err := vscodePosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err == nil {
		t.Fatal("expected missing extension warning error")
	}
	if len(postures) != 2 {
		t.Fatalf("expected two VS Code posture entries, got %#v", postures)
	}
	byName := map[string]vscodePosture{}
	for _, posture := range postures {
		byName[posture.Name] = posture
	}
	if byName["github.copilot"].Decision != "allow" || byName["github.copilot"].Version != "1.388.0" {
		t.Fatalf("expected allow posture for verified extension, got %#v", byName["github.copilot"])
	}
	if !byName["github.copilot"].ExecutesCode || byName["github.copilot"].RepositoryURL == "" {
		t.Fatalf("expected VS Code source metadata, got %#v", byName["github.copilot"])
	}
	if byName["unknown.extension"].Decision != "review" || !strings.Contains(byName["unknown.extension"].Reason, "metadata unavailable") {
		t.Fatalf("expected review posture for missing extension, got %#v", byName["unknown.extension"])
	}
	if len(requested) != 2 {
		t.Fatalf("expected two marketplace requests, got %#v", requested)
	}
}

func TestGitHubPosturesFromVSCodeReportsRepositoryRisks(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "full_name": "github/copilot.vscode",
  "html_url": "https://github.com/github/copilot.vscode",
  "default_branch": "main",
  "archived": true,
  "pushed_at": "2025-01-01T00:00:00Z",
  "updated_at": "2025-01-02T00:00:00Z"
}`))
	}))
	defer server.Close()
	postures := []vscodePosture{
		{Name: "github.copilot", RepositoryURL: "https://github.com/github/copilot.vscode"},
		{Name: "openai.chatgpt"},
	}
	got, err := githubPosturesFromVSCode(context.Background(), server.Client(), server.URL, postures)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/github/copilot.vscode" {
		t.Fatalf("unexpected github path: %s", gotPath)
	}
	if len(got) != 1 {
		t.Fatalf("expected one GitHub posture entry, got %#v", got)
	}
	if got[0].Provider != "brew" || got[0].Name != "vscode:github.copilot" {
		t.Fatalf("unexpected source identity: %#v", got[0])
	}
	if got[0].Decision != "review" || got[0].Reason != "repository is archived" {
		t.Fatalf("expected archived repo review, got %#v", got[0])
	}
}

func TestGitHubPosturesFromRegistryUsesRepositoryURLs(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "full_name": "scope/tool",
  "html_url": "https://github.com/scope/tool",
  "default_branch": "main",
  "archived": true
}`))
	}))
	defer server.Close()
	postures, err := githubPosturesFromRegistry(context.Background(), server.Client(), server.URL,
		[]npmPosture{{Provider: "mise", Name: "npm:@scope/tool", RepositoryURL: "https://github.com/scope/tool"}},
		[]cargoPosture{{Provider: "mise", Name: "cargo:fd-find", RepositoryURL: "https://github.com/scope/tool"}},
		[]pypiPosture{{Provider: "mise", Name: "pipx:frogmouth", ProjectURL: "https://example.com/frogmouth"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/scope/tool" {
		t.Fatalf("unexpected github path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one deduped GitHub posture, got %#v", postures)
	}
	if postures[0].Name != "npm:@scope/tool" || postures[0].Decision != "review" {
		t.Fatalf("expected registry GitHub posture, got %#v", postures[0])
	}
}

func TestNPMPosturesFromItemsReportsRegistryRisks(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "name": "@scope/tool",
  "dist-tags": {"latest": "2.0.0"},
  "time": {"1.0.0": "2026-05-01T00:00:00.000Z", "modified": "2026-05-02T00:00:00.000Z"},
  "repository": {"type": "git", "url": "git+https://github.com/scope/tool.git"},
  "maintainers": [{"name": "alice"}],
  "versions": {
    "1.0.0": {
      "version": "1.0.0",
      "bin": {"scope-tool": "./bin/tool.js"},
      "deprecated": "use 2.x"
    }
  }
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "npm:@scope/tool", Version: "1.0.0", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "npm:@scope/tool", Version: "1.0.0", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
	}
	postures, err := npmPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/@scope/tool" && gotPath != "/@scope%2ftool" {
		t.Fatalf("unexpected npm registry path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one npm posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || !strings.Contains(postures[0].Reason, "deprecated") {
		t.Fatalf("expected deprecated npm review, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/scope/tool" || postures[0].Latest != "2.0.0" || len(postures[0].Binaries) != 1 || postures[0].Binaries[0] != "scope-tool" {
		t.Fatalf("expected npm metadata evidence, got %#v", postures[0])
	}
}

func TestNPMPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := npmPostureFromMetadata("npm:tool", "tool", "1.0.0", npmPackageMetadata{
		DistTags:    map[string]string{"latest": "1.0.0"},
		Maintainers: []npmMaintainer{{Name: "alice"}},
		Versions: map[string]npmVersionInfo{
			"1.0.0": {Version: "1.0.0"},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected npm posture remediation, got %#v", posture)
	}
}

func TestCargoPosturesFromItemsReportsYankedVersions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "crate": {
    "id": "fd-find",
    "max_version": "10.4.2",
    "repository": "https://github.com/sharkdp/fd",
    "updated_at": "2026-05-02T00:00:00Z",
    "downloads": 42
  },
  "versions": [
    {"num": "10.4.2", "yanked": true, "created_at": "2026-05-01T00:00:00Z"}
  ]
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
	}
	postures, err := cargoPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/crates/fd-find" {
		t.Fatalf("unexpected crates.io path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one cargo posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || postures[0].Reason != "installed crate version is yanked" {
		t.Fatalf("expected yanked crate review, got %#v", postures[0])
	}
	if !strings.Contains(postures[0].Remediation, "non-yanked") {
		t.Fatalf("expected cargo posture remediation, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/sharkdp/fd" || postures[0].Latest != "10.4.2" {
		t.Fatalf("expected crates.io metadata evidence, got %#v", postures[0])
	}
}

func TestCargoPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := cargoPostureFromMetadata("cargo:tool", "tool", "1.0.0", cratesIOResponse{
		Crate: cratesIOCrate{ID: "tool", MaxVersion: "1.0.0"},
		Versions: []cratesIOVersion{
			{Num: "1.0.0"},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected cargo posture remediation, got %#v", posture)
	}
}

func TestPyPIPosturesFromItemsReportsYankedVersions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "info": {
    "name": "frogmouth",
    "version": "0.9.2",
    "project_urls": {"Source": "https://github.com/Textualize/frogmouth"}
  },
  "releases": {
    "0.9.2": [{
      "upload_time_iso_8601": "2026-05-01T00:00:00.000Z",
      "yanked": true,
      "yanked_reason": "bad release"
    }]
  }
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "pipx:frogmouth", Version: "0.9.2", Kind: "tool", Category: "pipx"},
		{Provider: "mise", Name: "pipx:frogmouth", Version: "0.9.2", Kind: "tool", Category: "pipx"},
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
	}
	postures, err := pypiPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/frogmouth/json" {
		t.Fatalf("unexpected PyPI path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one PyPI posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || !strings.Contains(postures[0].Reason, "yanked") {
		t.Fatalf("expected yanked PyPI review, got %#v", postures[0])
	}
	if !strings.Contains(postures[0].Remediation, "non-yanked") {
		t.Fatalf("expected PyPI posture remediation, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/Textualize/frogmouth" || postures[0].Latest != "0.9.2" {
		t.Fatalf("expected PyPI metadata evidence, got %#v", postures[0])
	}
}

func TestPyPIPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := pypiPostureFromMetadata("pipx:tool", "tool", "1.0.0", pypiPackageMetadata{
		Info: pypiInfo{Name: "tool", Version: "1.0.0"},
		Releases: map[string][]pypiRelease{
			"1.0.0": {{UploadTimeISO8601: "2026-05-01T00:00:00.000Z"}},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected PyPI posture remediation, got %#v", posture)
	}
}

func TestVSCodePostureReviewsCodeExecutionWithoutRepository(t *testing.T) {
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Code.ExecutesCode", Value: "true"},
			},
		}},
	})
	if posture.Decision != "review" || !strings.Contains(posture.Reason, "executes code") {
		t.Fatalf("expected executes-code repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "trusted source repository") {
		t.Fatalf("expected executes-code remediation, got %#v", posture)
	}
}

func TestVSCodePostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
		}},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected missing repository remediation, got %#v", posture)
	}
}

func TestVSCodePostureReviewsLowInstallCount(t *testing.T) {
	t.Setenv(vscodeMinInstallCountEnvName, "500")
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"},
			},
		}},
		Statistics: []vscodeStatistic{{StatisticName: "install", Value: 42}, {StatisticName: "averagerating", Value: 4.9}},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "install count") {
		t.Fatalf("expected low-install-count review, got %#v", posture)
	}
	if len(posture.Evidence) != 1 || posture.Evidence[0] != "vscode-marketplace popularity" {
		t.Fatalf("expected popularity evidence, got %#v", posture)
	}
}

func TestVSCodePostureReviewsZeroInstallStatistic(t *testing.T) {
	t.Setenv(vscodeMinInstallCountEnvName, "1")
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"},
			},
		}},
		Statistics: []vscodeStatistic{{StatisticName: "install", Value: 0}},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "0 installs") {
		t.Fatalf("expected zero-install statistic review, got %#v", posture)
	}
	if len(posture.Evidence) != 1 || posture.Evidence[0] != "vscode-marketplace popularity" {
		t.Fatalf("expected popularity evidence, got %#v", posture)
	}
}

func TestVSCodePostureReviewsLowAverageRating(t *testing.T) {
	t.Setenv(vscodeMinInstallCountEnvName, "0")
	t.Setenv(vscodeMinAverageRatingEnvName, "3.5")
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"},
			},
		}},
		Statistics: []vscodeStatistic{{StatisticName: "install", Value: 10000}, {StatisticName: "averagerating", Value: 2.8}},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "average rating") {
		t.Fatalf("expected low-rating review, got %#v", posture)
	}
	if len(posture.Evidence) != 1 || posture.Evidence[0] != "vscode-marketplace rating" {
		t.Fatalf("expected rating evidence, got %#v", posture)
	}
}

func TestVSCodePostureReviewsNewExtension(t *testing.T) {
	t.Setenv(vscodeMinInstallCountEnvName, "0")
	t.Setenv(vscodeMinAverageRatingEnvName, "0")
	t.Setenv(vscodeMinExtensionAgeDaysEnvName, "30")
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		PublishedDate: time.Now().UTC().Format(time.RFC3339),
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"},
			},
		}},
		Statistics: []vscodeStatistic{{StatisticName: "install", Value: 10000}, {StatisticName: "averagerating", Value: 4.8}},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "newly published") {
		t.Fatalf("expected new extension review, got %#v", posture)
	}
	if len(posture.Evidence) != 1 || posture.Evidence[0] != "vscode-marketplace age" {
		t.Fatalf("expected age evidence, got %#v", posture)
	}
}

func TestVSCodePostureAllowsMissingPopularityStatistics(t *testing.T) {
	posture := vscodePostureFromMetadata("publisher.extension", vscodeExtension{
		Publisher:     vscodePublisher{PublisherName: "publisher", IsDomainVerified: true},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions: []vscodeVersion{{
			Version: "1.0.0",
			Properties: []vscodeProperty{
				{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"},
			},
		}},
	})
	if posture.Decision != "allow" {
		t.Fatalf("expected missing popularity statistics to remain allowed, got %#v", posture)
	}
}

func TestVSCodeAdvisoryPackagesFromPosturesUsesMarketplaceIdentity(t *testing.T) {
	packages := vscodeAdvisoryPackagesFromPostures([]vscodePosture{
		{Name: "publisher.extension", Version: "1.2.3"},
		{Name: "publisher.extension", Version: "1.2.3"},
		{Name: "publisher.other"},
	})
	if len(packages) != 1 {
		t.Fatalf("expected one VS Code advisory package, got %#v", packages)
	}
	if packages[0].Provider != "brew" || packages[0].Name != "publisher.extension" || packages[0].Ecosystem != "VSCode" || packages[0].Package != "publisher.extension" || packages[0].Version != "1.2.3" || packages[0].Confidence != "high" {
		t.Fatalf("unexpected VS Code advisory package mapping: %#v", packages[0])
	}
}

func TestEnrichVSCodeSafetyAdvisoriesHoldsOSVMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/vulns/GHSA-vscode" {
				t.Fatalf("unexpected OSV detail path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "id": "GHSA-vscode",
  "affected": [{
    "package": {"ecosystem": "VSCode", "name": "publisher.extension"},
    "ranges": [{"events": [{"introduced": "0"}, {"fixed": "1.2.4"}]}]
  }]
}`))
			return
		}
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "VSCode" || request.Queries[0].Package.Name != "publisher.extension" || request.Queries[0].Version != "1.2.3" {
			t.Fatalf("unexpected VS Code safety OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-vscode","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	findings := []safetyFinding{{
		Provider:   "brew",
		Kind:       "vscode",
		Name:       "publisher.extension",
		Version:    "1.2.3",
		Decision:   "allow",
		Confidence: "medium",
	}}
	enriched, err := enrichVSCodeSafetyAdvisories(context.Background(), server.Client(), server.URL, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(enriched) != 1 || enriched[0].Decision != "hold" || !strings.Contains(enriched[0].Reason, "GHSA-vscode") {
		t.Fatalf("expected VS Code OSV hold finding, got %#v", enriched)
	}
	if !containsString(enriched[0].AdvisoryIDs, "GHSA-vscode") || !containsString(enriched[0].FixedVersions, "1.2.4") || !containsString(enriched[0].Evidence, "osv-vscode") {
		t.Fatalf("expected VS Code OSV advisory evidence, got %#v", enriched[0])
	}
	if !strings.Contains(enriched[0].Remediation, "1.2.4") {
		t.Fatalf("expected fixed-version VS Code remediation, got %#v", enriched[0])
	}
}

func TestQueryOSVBatchBuildsFindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/vulns/GHSA-test" {
				t.Fatalf("unexpected OSV detail path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "id": "GHSA-test",
  "aliases": ["CVE-2026-0001"],
  "severity": [{"type": "CVSS_V3", "score": "9.8"}],
  "affected": [{
    "package": {"ecosystem": "npm", "name": "pnpm"},
    "ranges": [{"events": [{"introduced": "0"}, {"fixed": "11.1.3"}]}]
  }]
}`))
			return
		}
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "npm" || request.Queries[0].Package.Name != "pnpm" || request.Queries[0].Version != "11.1.2" {
			t.Fatalf("unexpected OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-test","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	packages := []securityPackage{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high", BinaryName: "pnpm", PathState: "on-path"}}
	findings, err := queryOSVBatch(context.Background(), server.Client(), server.URL, packages)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].VulnID != "GHSA-test" || findings[0].Status != plan.StatusHeld {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if len(findings[0].Aliases) != 1 || findings[0].Aliases[0] != "CVE-2026-0001" || findings[0].Severity != "CVSS_V3:9.8" {
		t.Fatalf("expected enriched OSV detail, got %#v", findings[0])
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "11.1.3" {
		t.Fatalf("expected fixed version evidence, got %#v", findings[0])
	}
	if findings[0].BinaryName != "pnpm" || findings[0].PathState != "on-path" || findings[0].Exposure != "on-path-binary:pnpm" {
		t.Fatalf("expected path evidence to propagate to finding, got %#v", findings[0])
	}
	if reason := securityFindingReason(findings[0]); !strings.Contains(reason, "on-PATH binary exposure") {
		t.Fatalf("expected on-PATH exposure in finding reason, got %q", reason)
	}
	if !strings.Contains(findings[0].Remediation, "11.1.3") || !strings.Contains(findings[0].Remediation, "on-PATH binary") {
		t.Fatalf("expected fixed-version and on-PATH remediation, got %#v", findings[0])
	}
}

func TestQueryGitHubAdvisoriesBuildsFindings(t *testing.T) {
	t.Setenv("UPDEV_GITHUB_TOKEN", "updev-test-token")
	seenTypes := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/advisories" {
			t.Fatalf("unexpected GitHub Advisory request: %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("unexpected Accept header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer updev-test-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		if got := r.URL.Query().Get("ecosystem"); got != "npm" {
			t.Fatalf("unexpected ecosystem query: %q", got)
		}
		if got := r.URL.Query().Get("affects"); got != "pnpm@11.1.2" {
			t.Fatalf("unexpected affects query: %q", got)
		}
		advisoryType := r.URL.Query().Get("type")
		seenTypes[advisoryType] = true
		if advisoryType == "malware" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{
  "ghsa_id": "GHSA-test-gh",
  "cve_id": "CVE-2026-1234",
  "summary": "test advisory",
  "type": "reviewed",
  "severity": "high",
  "html_url": "https://github.com/advisories/GHSA-test-gh",
  "updated_at": "2026-01-03T00:00:00Z",
  "vulnerabilities": [{
    "package": {"ecosystem": "npm", "name": "pnpm"},
    "first_patched_version": "11.1.3",
    "vulnerable_version_range": "<11.1.3"
  }]
}]`))
	}))
	defer server.Close()
	packages := []securityPackage{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high", BinaryName: "pnpm", PathState: "on-path"}}
	findings, err := queryGitHubAdvisories(context.Background(), server.Client(), server.URL, packages)
	if err != nil {
		t.Fatal(err)
	}
	if !seenTypes["reviewed"] || !seenTypes["malware"] {
		t.Fatalf("expected reviewed and malware GitHub Advisory queries, got %#v", seenTypes)
	}
	if len(findings) != 1 || findings[0].VulnID != "GHSA-test-gh" || findings[0].Status != plan.StatusHeld {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if findings[0].Reason != "GitHub Advisory vulnerability match" || findings[0].Severity != "high" || findings[0].URL == "" {
		t.Fatalf("expected GitHub Advisory metadata, got %#v", findings[0])
	}
	if len(findings[0].Aliases) != 1 || findings[0].Aliases[0] != "CVE-2026-1234" {
		t.Fatalf("expected CVE alias, got %#v", findings[0])
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "11.1.3" {
		t.Fatalf("expected fixed version evidence, got %#v", findings[0])
	}
	if !strings.Contains(findings[0].Remediation, "11.1.3") || !strings.Contains(findings[0].Remediation, "on-PATH binary") {
		t.Fatalf("expected fixed-version and on-PATH remediation, got %#v", findings[0])
	}
}

func TestAppendUniqueSecurityFindingsDeduplicatesAliases(t *testing.T) {
	findings := appendUniqueSecurityFindings(
		[]securityFinding{{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "OSV-2026-0001", Aliases: []string{"CVE-2026-1234"}}},
		securityFinding{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "GHSA-test-gh", Aliases: []string{"CVE-2026-1234"}, FixedVersions: []string{"11.1.3"}, Decision: "hold"},
		securityFinding{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "GHSA-other"},
	)
	if len(findings) != 2 {
		t.Fatalf("expected alias duplicate to be removed, got %#v", findings)
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "11.1.3" {
		t.Fatalf("expected duplicate finding evidence to merge, got %#v", findings)
	}
}

func TestSortSecurityFindingsPrioritizesExploitabilityAndExposure(t *testing.T) {
	findings := []securityFinding{
		{Name: "low", VulnID: "GHSA-low", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "binary-not-found:low"},
		{Name: "allowed", VulnID: "GHSA-allow", Decision: "allow", KEV: &kevFinding{CVEID: "CVE-2026-0002"}},
		{Name: "epss", VulnID: "GHSA-epss", Decision: "hold", Severity: "CVSS_V3:4.0", EPSS: &epssFinding{Score: 0.91}},
		{Name: "kev", VulnID: "GHSA-kev", Decision: "hold", Severity: "CVSS_V3:5.0", KEV: &kevFinding{CVEID: "CVE-2026-0001"}},
		{Name: "fixed", VulnID: "GHSA-fixed", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "binary-not-found:fixed", FixedVersions: []string{"1.2.3"}},
		{Name: "onpath", VulnID: "GHSA-onpath", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "on-path-binary:onpath"},
	}
	sortSecurityFindings(findings)
	got := []string{}
	for _, finding := range findings {
		got = append(got, finding.Name)
	}
	want := []string{"kev", "epss", "fixed", "onpath", "low", "allowed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected sorted findings order: got %#v want %#v", got, want)
	}
}

func TestQueryOSVBatchBuildsHomebrewGitFindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/vulns/CVE-2026-9999" {
				t.Fatalf("unexpected OSV detail path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "id": "CVE-2026-9999",
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
			t.Fatalf("unexpected Homebrew OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"CVE-2026-9999","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	packages := []securityPackage{{Provider: "brew", Name: "jq", Package: "https://github.com/jqlang/jq.git", Version: "jq-1.8.1", Ecosystem: "GIT", Confidence: "medium"}}
	findings, err := queryOSVBatch(context.Background(), server.Client(), server.URL, packages)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Provider != "brew" || findings[0].Name != "jq" || findings[0].Package != "https://github.com/jqlang/jq.git" || findings[0].Confidence != "medium" || findings[0].Status != plan.StatusHeld {
		t.Fatalf("unexpected Homebrew GIT finding: %#v", findings)
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "jq-1.8.2" {
		t.Fatalf("expected Homebrew GIT fixed version evidence, got %#v", findings[0])
	}
}

func TestQueryOSVBatchBuildsVSCodeFindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/vulns/GHSA-vscode" {
				t.Fatalf("unexpected OSV detail path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{
  "id": "GHSA-vscode",
  "affected": [{
    "package": {"ecosystem": "VSCode", "name": "publisher.extension"},
    "ranges": [{"events": [{"introduced": "0"}, {"fixed": "1.2.4"}]}]
  }]
}`))
			return
		}
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "VSCode" || request.Queries[0].Package.Name != "publisher.extension" || request.Queries[0].Version != "1.2.3" {
			t.Fatalf("unexpected VS Code OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-vscode","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	packages := []securityPackage{{Provider: "brew", Name: "publisher.extension", Package: "publisher.extension", Version: "1.2.3", Ecosystem: "VSCode", Confidence: "high"}}
	findings, err := queryOSVBatch(context.Background(), server.Client(), server.URL, packages)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Provider != "brew" || findings[0].Name != "publisher.extension" || findings[0].Ecosystem != "VSCode" || findings[0].Confidence != "high" || findings[0].Status != plan.StatusHeld {
		t.Fatalf("unexpected VS Code finding: %#v", findings)
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "1.2.4" {
		t.Fatalf("expected VS Code fixed version evidence, got %#v", findings[0])
	}
}

func TestEnrichFindingsWithKEVBlocksKnownExploitedCVE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
  "vulnerabilities": [
    {
      "cveID": "CVE-2026-0001",
      "vendorProject": "Example",
      "product": "Example Tool",
      "vulnerabilityName": "Example exploited vulnerability",
      "dateAdded": "2026-05-01",
      "dueDate": "2026-05-22",
      "knownRansomwareCampaignUse": "Known",
      "requiredAction": "Apply updates"
    }
  ]
}`))
	}))
	defer server.Close()
	findings := []securityFinding{
		{VulnID: "GHSA-test", Aliases: []string{"CVE-2026-0001"}, Decision: "hold", Status: plan.StatusHeld},
	}
	enriched, err := enrichFindingsWithKEV(context.Background(), server.Client(), server.URL, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(enriched) != 1 || enriched[0].Status != plan.StatusBlocked || enriched[0].Decision != "block" {
		t.Fatalf("expected blocked KEV finding, got %#v", enriched)
	}
	if enriched[0].KEV == nil || enriched[0].KEV.KnownRansomwareCampaignUse != "Known" {
		t.Fatalf("expected KEV metadata, got %#v", enriched[0])
	}
}

func TestSecurityReportPreservesOSVFindingsWhenKEVFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-test"}]}]}`))
		case r.URL.Path == "/vulns/GHSA-test":
			_, _ = w.Write([]byte(`{"id":"GHSA-test","aliases":["CVE-2026-0001"]}`))
		default:
			http.Error(w, "kev unavailable", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_CISA_KEV_URL", server.URL+"/kev")
	opts := securityOptions{format: "json", root: "/repo"}
	packages := []securityPackage{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high"}}
	findings, err := queryOSVBatch(context.Background(), server.Client(), server.URL+"/querybatch", packages)
	if err != nil {
		t.Fatal(err)
	}
	enriched, kevErr := enrichFindingsWithKEV(context.Background(), server.Client(), cisaKEVURL(), findings)
	if kevErr == nil {
		t.Fatal("expected KEV enrichment error")
	}
	if len(enriched) != 1 || enriched[0].Status != plan.StatusHeld {
		t.Fatalf("expected original held finding to remain, got %#v with opts %#v", enriched, opts)
	}
}

func TestEnrichFindingsWithEPSSAddsExploitProbability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("cve"); got != "CVE-2026-0001" {
			t.Fatalf("unexpected EPSS CVE query: %q", got)
		}
		_, _ = w.Write([]byte(`{
  "data": [
    {"cve":"CVE-2026-0001","epss":"0.812340000","percentile":"0.987650000","date":"2026-05-27"}
  ]
}`))
	}))
	defer server.Close()
	findings := []securityFinding{
		{VulnID: "GHSA-test", Aliases: []string{"CVE-2026-0001"}, Decision: "hold", Status: plan.StatusHeld},
	}
	enriched, err := enrichFindingsWithEPSS(context.Background(), server.Client(), server.URL, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(enriched) != 1 || enriched[0].EPSS == nil {
		t.Fatalf("expected EPSS metadata, got %#v", enriched)
	}
	if enriched[0].EPSS.Score != 0.81234 || enriched[0].EPSS.Percentile != 0.98765 || enriched[0].EPSS.Date != "2026-05-27" {
		t.Fatalf("unexpected EPSS metadata: %#v", enriched[0].EPSS)
	}
}
