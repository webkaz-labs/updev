package cmd

import (
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
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/vscode"
)

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
	if posture.ReasonCode != securityreason.VSCodeExtensionTooNew || posture.ReasonArgs["min_age_days"] != "30" {
		t.Fatalf("expected structured extension age reason, got %#v", posture)
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
	if posture.ReasonCode != securityreason.VSCodeMarketplaceAllowed || posture.ReasonArgs["extension"] != "publisher.extension" {
		t.Fatalf("expected structured allow reason, got %#v", posture)
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
	if enriched[0].ReasonCode != securityreason.VSCodeAdvisoryMatch || enriched[0].ReasonArgs["advisory_ids"] != "GHSA-vscode" {
		t.Fatalf("expected structured VS Code advisory reason, got %#v", enriched[0])
	}
	if !strings.Contains(enriched[0].Remediation, "1.2.4") {
		t.Fatalf("expected fixed-version VS Code remediation, got %#v", enriched[0])
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

func TestBuildSecurityGateReportRunsVSCodeSafety(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"extensions":[{
	  "publisher": {"publisherName": "publisher", "isDomainVerified": true},
	  "extensionName": "extension",
	  "displayName": "Extension",
	  "flags": "validated, public",
	  "lastUpdated": "2026-05-01T00:00:00Z",
	  "publishedDate": "2026-04-01T00:00:00Z",
	  "versions": [{"version": "1.0.0", "properties": [
	    {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"},
	    {"key": "Microsoft.VisualStudio.Services.Links.Support", "value": "https://example.com/support"}
	  ]}],
	  "statistics": [
	    {"statisticName": "install", "value": 1234},
	    {"statisticName": "averagerating", "value": 4.5}
	  ]
	}]}]}`))
	}))
	defer server.Close()
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer osvServer.Close()
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", osvServer.URL)
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "vscode"}, &fakeCommandRunner{
		result: runner.Result{Stdout: "publisher.extension@0.9.0\n"},
	})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected vscode gate held, got %#v", report)
	}
	if len(report.Gates) != 1 || report.Gates[0].Provider != "vscode" {
		t.Fatalf("expected vscode gate, got %#v", report.Gates)
	}
	if len(report.Gates[0].Findings) != 1 || report.Gates[0].Findings[0].Kind != "vscode" || report.Gates[0].Findings[0].Decision != "review" {
		t.Fatalf("expected vscode review finding, got %#v", report.Gates[0].Findings)
	}
	if !report.Gates[0].Findings[0].ExecutesCode || report.Gates[0].Findings[0].Publisher != "publisher" || report.Gates[0].Findings[0].SupportURL == "" {
		t.Fatalf("expected vscode posture details, got %#v", report.Gates[0].Findings[0])
	}
	if len(report.Gates[0].Findings[0].InstalledVersions) != 1 || report.Gates[0].Findings[0].InstalledVersions[0] != "0.9.0" || report.Gates[0].Findings[0].CurrentVersion != "1.0.0" {
		t.Fatalf("expected vscode installed/current versions, got %#v", report.Gates[0].Findings[0])
	}
	if report.Gates[0].Findings[0].PublisherVerified == nil || !*report.Gates[0].Findings[0].PublisherVerified || report.Gates[0].Findings[0].InstallCount != 1234 || report.Gates[0].Findings[0].AverageRating != 4.5 || report.Gates[0].Findings[0].LastUpdated == "" || report.Gates[0].Findings[0].PublishedDate == "" {
		t.Fatalf("expected vscode provenance details, got %#v", report.Gates[0].Findings[0])
	}
	if report.Gates[0].Summary == nil || report.Gates[0].Summary.Review != 1 {
		t.Fatalf("expected vscode gate summary, got %#v", report.Gates[0].Summary)
	}
	if !strings.Contains(report.Gates[0].Findings[0].Remediation, "Marketplace") {
		t.Fatalf("expected vscode remediation, got %#v", report.Gates[0].Findings[0])
	}
}

func TestBuildSecurityGateReportAllExcludesVSCodeByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "all"}, &fakeCommandRunner{result: runner.Result{Stdout: "{}"}})
	if len(report.Gates) != 2 || report.Gates[0].Provider != "brew" || report.Gates[1].Provider != "mise" {
		t.Fatalf("expected brew and mise gates by default, got %#v", report.Gates)
	}
}

func TestVSCodeInstalledVersionsErrorUsesExitStatus(t *testing.T) {
	got := vscode.InstalledVersionsError(runner.Result{Code: 127})
	if got != "code exited with status 127" {
		t.Fatalf("expected exit status detail, got %q", got)
	}
}

func TestCollectUpdateSafetyIncludesVSCodeWhenBrewfileDeclaresExtensions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	homebrewServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer homebrewServer.Close()
	marketplaceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"extensions":[{
	  "publisher": {"publisherName": "publisher", "isDomainVerified": true},
	  "extensionName": "extension",
	  "displayName": "Extension",
	  "flags": "validated, public",
	  "lastUpdated": "2026-05-01T00:00:00Z",
	  "publishedDate": "2026-04-01T00:00:00Z",
	  "versions": [{"version": "1.0.0", "properties": [
	    {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"}
	  ]}]
	}]}]}`))
	}))
	defer marketplaceServer.Close()
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer osvServer.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", homebrewServer.URL)
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", marketplaceServer.URL)
	t.Setenv("UPDEV_OSV_API_URL", osvServer.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"code", "--list-extensions", "--show-versions"}, "\x00"):                                  {Stdout: "publisher.extension@0.9.0\n"},
	}}
	gates := collectUpdateSafetyWithPolicy(context.Background(), fake, updateOptions{root: root, security: "strict", includeVSCode: true}, securityPolicy{})
	if len(gates) != 3 || gates[0].Provider != "brew" || gates[1].Provider != "mise" || gates[2].Provider != "vscode" {
		t.Fatalf("expected brew, mise, and vscode update safety gates, got %#v", gates)
	}
	if gates[2].Status != plan.StatusHeld || len(gates[2].Findings) != 1 || gates[2].Findings[0].Kind != "vscode" {
		t.Fatalf("expected held vscode update safety finding, got %#v", gates[2])
	}
}

func TestCollectUpdateSafetyExcludesVSCodeByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"code", "--list-extensions", "--show-versions"}, "\x00"):                                  {Stdout: "publisher.extension@0.9.0\n"},
	}}
	gates := collectUpdateSafetyWithPolicy(context.Background(), fake, updateOptions{root: root, security: "strict"}, securityPolicy{})
	if len(gates) != 2 || gates[0].Provider != "brew" || gates[1].Provider != "mise" {
		t.Fatalf("expected brew and mise update safety gates by default, got %#v", gates)
	}
	for _, call := range fake.calls {
		if len(call) > 0 && call[0] == "code" {
			t.Fatalf("did not expect code command by default, calls=%#v", fake.calls)
		}
	}
}

func TestCollectUpdateSafetyUsesVSCodeCandidateCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	marketplaceCalls := 0
	marketplaceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marketplaceCalls++
		_, _ = w.Write([]byte(`{"results":[{"extensions":[{
	  "publisher": {"publisherName": "publisher", "isDomainVerified": true},
	  "extensionName": "extension",
	  "displayName": "Extension",
	  "flags": "validated, public",
	  "lastUpdated": "2026-05-01T00:00:00Z",
	  "publishedDate": "2026-04-01T00:00:00Z",
	  "versions": [{"version": "1.0.0", "properties": [
	    {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"}
	  ]}]
	}]}]}`))
	}))
	defer marketplaceServer.Close()
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer osvServer.Close()
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", marketplaceServer.URL)
	t.Setenv("UPDEV_OSV_API_URL", osvServer.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "publisher.extension@0.9.0\n"}}
	first := collectVSCodeUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	second := collectVSCodeUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if marketplaceCalls != 1 {
		t.Fatalf("expected marketplace to be called once, got %d", marketplaceCalls)
	}
	if len(first.Findings) != 1 || len(second.Findings) != 1 {
		t.Fatalf("expected one cached vscode finding, got first=%#v second=%#v", first.Findings, second.Findings)
	}
	if !containsSubstring(second.Findings[0].Evidence, "updev update safety cache") {
		t.Fatalf("expected cache evidence on second finding, got %#v", second.Findings[0].Evidence)
	}
}

func TestCollectUpdateSafetyCachesVSCodeMarketplaceError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	marketplaceCalls := 0
	marketplaceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marketplaceCalls++
		http.Error(w, "temporary marketplace failure", http.StatusBadGateway)
	}))
	defer marketplaceServer.Close()
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", marketplaceServer.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "publisher.extension@0.9.0\n"}}
	first := collectVSCodeUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	second := collectVSCodeUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if marketplaceCalls != 1 {
		t.Fatalf("expected marketplace error to be cached, got %d calls", marketplaceCalls)
	}
	if !containsSubstring(first.Warnings, "VS Code marketplace posture failed") {
		t.Fatalf("expected first marketplace warning, got %#v", first.Warnings)
	}
	if !containsSubstring(second.Warnings, "cached VS Code marketplace posture failed") {
		t.Fatalf("expected cached marketplace warning, got %#v", second.Warnings)
	}
}

func TestCollectUpdateSafetySkipsUninstalledVSCodeItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`vscode "publisher.extension"`), 0o600); err != nil {
		t.Fatal(err)
	}
	marketplaceCalls := 0
	marketplaceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marketplaceCalls++
		_, _ = w.Write([]byte(`{"results":[{"extensions":[]}]}`))
	}))
	defer marketplaceServer.Close()
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", marketplaceServer.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "other.extension@1.0.0\n"}}
	gate := collectVSCodeUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK || len(gate.Findings) != 0 {
		t.Fatalf("expected no vscode update candidates, got %#v", gate)
	}
	if marketplaceCalls != 0 {
		t.Fatalf("expected no marketplace call for uninstalled Brewfile extension, got %d", marketplaceCalls)
	}
}

func TestVSCodeSafetyHoldsNewMarketplaceUpdate(t *testing.T) {
	t.Setenv(vscodeMinUpdateAgeDaysEnvName, "7")
	posture := vscodePosture{
		Provider:          "brew",
		Kind:              "vscode",
		Name:              "publisher.extension",
		Version:           "1.2.0",
		LastUpdated:       time.Now().AddDate(0, 0, -2).UTC().Format(time.RFC3339),
		Publisher:         "publisher",
		PublisherVerified: true,
		Decision:          "allow",
		Confidence:        "medium",
		Evidence:          []string{"vscode-marketplace"},
	}
	finding := vscodeSafetyFinding(posture, "1.1.0")
	if finding.Decision != "hold" || !strings.Contains(finding.Reason, "update is too new") {
		t.Fatalf("expected new Marketplace update hold, got %#v", finding)
	}
	if finding.ReasonCode != securityreason.VSCodeExtensionTooNew || finding.ReasonArgs["min_age_days"] != "7" {
		t.Fatalf("expected structured VS Code update-age reason, got %#v", finding)
	}
	if finding.ReleaseDate == "" || finding.MinReleaseAgeDays != 7 || !containsString(finding.Evidence, "vscode-marketplace update-age") {
		t.Fatalf("expected update-age evidence, got %#v", finding)
	}
}
