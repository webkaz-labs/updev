package securityadvisory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
)

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
		var request OSVBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "npm" || request.Queries[0].Package.Name != "pnpm" || request.Queries[0].Version != "11.1.2" {
			t.Fatalf("unexpected OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-test","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	packages := []Package{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high", BinaryName: "pnpm", PathState: "on-path"}}
	findings, err := QueryOSVBatch(context.Background(), server.Client(), server.URL, packages)
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
	if reason := Reason(findings[0]); !strings.Contains(reason, "on-PATH binary exposure") {
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
	packages := []Package{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high", BinaryName: "pnpm", PathState: "on-path"}}
	findings, err := QueryGitHubAdvisories(context.Background(), server.Client(), server.URL, "updev-test-token", packages)
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
	findings := AppendUniqueFindings(
		[]Finding{{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "OSV-2026-0001", Aliases: []string{"CVE-2026-1234"}}},
		Finding{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "GHSA-test-gh", Aliases: []string{"CVE-2026-1234"}, FixedVersions: []string{"11.1.3"}, Decision: "hold"},
		Finding{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Ecosystem: "npm", Package: "pnpm", VulnID: "GHSA-other"},
	)
	if len(findings) != 2 {
		t.Fatalf("expected alias duplicate to be removed, got %#v", findings)
	}
	if len(findings[0].FixedVersions) != 1 || findings[0].FixedVersions[0] != "11.1.3" {
		t.Fatalf("expected duplicate finding evidence to merge, got %#v", findings)
	}
}

func TestSortSecurityFindingsPrioritizesExploitabilityAndExposure(t *testing.T) {
	findings := []Finding{
		{Name: "low", VulnID: "GHSA-low", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "binary-not-found:low"},
		{Name: "allowed", VulnID: "GHSA-allow", Decision: "allow", KEV: &KEVFinding{CVEID: "CVE-2026-0002"}},
		{Name: "epss", VulnID: "GHSA-epss", Decision: "hold", Severity: "CVSS_V3:4.0", EPSS: &EPSSFinding{Score: 0.91}},
		{Name: "kev", VulnID: "GHSA-kev", Decision: "hold", Severity: "CVSS_V3:5.0", KEV: &KEVFinding{CVEID: "CVE-2026-0001"}},
		{Name: "fixed", VulnID: "GHSA-fixed", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "binary-not-found:fixed", FixedVersions: []string{"1.2.3"}},
		{Name: "onpath", VulnID: "GHSA-onpath", Decision: "hold", Severity: "CVSS_V3:9.8", Exposure: "on-path-binary:onpath"},
	}
	SortFindings(findings)
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
		var request OSVBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Queries) != 1 || request.Queries[0].Package.Ecosystem != "GIT" || request.Queries[0].Package.Name != "https://github.com/jqlang/jq.git" || request.Queries[0].Version != "jq-1.8.1" {
			t.Fatalf("unexpected Homebrew OSV request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"CVE-2026-9999","modified":"2026-01-02T00:00:00Z"}]}]}`))
	}))
	defer server.Close()
	packages := []Package{{Provider: "brew", Name: "jq", Package: "https://github.com/jqlang/jq.git", Version: "jq-1.8.1", Ecosystem: "GIT", Confidence: "medium"}}
	findings, err := QueryOSVBatch(context.Background(), server.Client(), server.URL, packages)
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
	findings := []Finding{
		{VulnID: "GHSA-test", Aliases: []string{"CVE-2026-0001"}, Decision: "hold", Status: plan.StatusHeld},
	}
	enriched, err := EnrichWithKEV(context.Background(), server.Client(), server.URL, findings)
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
	packages := []Package{{Provider: "mise", Name: "npm:pnpm", Package: "pnpm", Version: "11.1.2", Ecosystem: "npm", Confidence: "high"}}
	findings, err := QueryOSVBatch(context.Background(), server.Client(), server.URL+"/querybatch", packages)
	if err != nil {
		t.Fatal(err)
	}
	enriched, kevErr := EnrichWithKEV(context.Background(), server.Client(), server.URL+"/kev", findings)
	if kevErr == nil {
		t.Fatal("expected KEV enrichment error")
	}
	if len(enriched) != 1 || enriched[0].Status != plan.StatusHeld {
		t.Fatalf("expected original held finding to remain, got %#v", enriched)
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
	findings := []Finding{
		{VulnID: "GHSA-test", Aliases: []string{"CVE-2026-0001"}, Decision: "hold", Status: plan.StatusHeld},
	}
	enriched, err := EnrichWithEPSS(context.Background(), server.Client(), server.URL, findings)
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
