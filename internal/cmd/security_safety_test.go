package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func TestParseSecurityGateOptions(t *testing.T) {
	opts, err := parseSecurityGateOptions([]string{"--provider", "brew", "--format", "json", "--root", "/tmp/root", "--policy", "/tmp/policy.json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.provider != "brew" || opts.format != "json" || opts.root != "/tmp/root" || opts.policy != "/tmp/policy.json" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityGateOptions([]string{"--provider", "npm"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	opts, err = parseSecurityGateOptions([]string{"--provider", "vscode"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.provider != "vscode" {
		t.Fatalf("expected vscode provider, got %+v", opts)
	}
	opts, err = parseSecurityGateOptions([]string{"--provider", "mise"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.provider != "mise" {
		t.Fatalf("expected mise provider, got %+v", opts)
	}
	opts, err = parseSecurityGateOptions([]string{"--provider", "all"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.provider != "all" {
		t.Fatalf("expected all provider, got %+v", opts)
	}
}

func TestParseSecurityReviewOptionsAcceptsCandidateFilters(t *testing.T) {
	opts, err := parseSecurityReviewOptions([]string{"--provider", "all", "--decision", "hold", "--kind", "osv-scanner", "--name", "GHSA", "--scanner", "none"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.provider != "all" || opts.decision != "hold" || opts.kind != "osv-scanner" || opts.name != "GHSA" || opts.scanner != "none" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityReviewOptions([]string{"--decision", "maybe"}); err == nil {
		t.Fatal("expected unsupported decision error")
	}
}

func TestBuildSecurityGateReportRunsBrewSafetyOnly(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: t.TempDir(), provider: "brew"}, fake)
	if report.Status != plan.StatusOK {
		t.Fatalf("expected ok gate, got %#v", report)
	}
	if len(report.Gates) != 1 || report.Gates[0].Provider != "brew" {
		t.Fatalf("expected brew gate, got %#v", report.Gates)
	}
	if len(fake.calls) != 2 || !containsString(fake.calls[1], "HOMEBREW_NO_AUTO_UPDATE=1") || containsString(fake.calls[1], "HOMEBREW_NO_INSTALL_FROM_API=1") || !containsString(fake.calls[1], "brew") {
		t.Fatalf("expected brew command, got %+v", fake.calls)
	}
}

func TestRunBrewOutdatedUsesNoInstallFromAPIOnlyWhenCoreTapExists(t *testing.T) {
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"brew", "tap"}, "\x00"): {Stdout: "homebrew/core\nwebkaz/tap\n"},
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "outdated", "--json=v2"}, "\x00"): {Stdout: `{"formulae":[],"casks":[]}`},
	}}
	result := runBrewOutdatedJSON(context.Background(), fake)
	if result.Stdout == "" || len(fake.calls) != 2 {
		t.Fatalf("expected tap probe and no-install API command, result=%#v calls=%#v", result, fake.calls)
	}
	if !containsString(fake.calls[1], "HOMEBREW_NO_INSTALL_FROM_API=1") {
		t.Fatalf("expected no-install API env when core tap exists, calls=%#v", fake.calls)
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

func TestBuildSecurityGateReportRunsAllSafety(t *testing.T) {
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
	  "versions": [{"version": "1.0.0", "properties": [
	    {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"}
	  ]}]
	}]}]}`))
	}))
	defer server.Close()
	osvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer osvServer.Close()
	t.Setenv("UPDEV_VSCODE_MARKETPLACE_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", osvServer.URL)
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "all", includeVSCode: true}, &fakeCommandRunner{result: runner.Result{Stdout: "{}"}})
	if report.Status != plan.StatusHeld || len(report.Gates) != 3 || report.Gates[0].Provider != "brew" || report.Gates[1].Provider != "mise" || report.Gates[2].Provider != "vscode" {
		t.Fatalf("expected combined brew/mise/vscode held gate, got %#v", report)
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

func TestBuildSecurityGateReportRunsMiseSafety(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`}}
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "mise"}, fake)
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected mise gate held, got %#v", report)
	}
	if len(report.Gates) != 1 || report.Gates[0].Provider != "mise" {
		t.Fatalf("expected mise gate, got %#v", report.Gates)
	}
	if len(report.Gates[0].Findings) != 1 || report.Gates[0].Findings[0].Name != "github:openai/codex" || report.Gates[0].Findings[0].Decision != "review" {
		t.Fatalf("expected mise review finding, got %#v", report.Gates[0].Findings)
	}
}

func TestVSCodeInstalledVersionsErrorUsesExitStatus(t *testing.T) {
	got := vscodeInstalledVersionsError(runner.Result{Code: 127})
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
		strings.Join([]string{"brew", "outdated", "--json=v2"}, "\x00"):                {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"code", "--list-extensions", "--show-versions"}, "\x00"): {Stdout: "publisher.extension@0.9.0\n"},
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
		strings.Join([]string{"brew", "outdated", "--json=v2"}, "\x00"):                {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"code", "--list-extensions", "--show-versions"}, "\x00"): {Stdout: "publisher.extension@0.9.0\n"},
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

func TestCollectUpdateSafetyCachesBrewOutdatedError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "example"`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Code: 1, Stderr: "temporary brew api failure"}}
	first := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	second := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if first.Status != plan.StatusError || second.Status != plan.StatusError {
		t.Fatalf("expected cached brew errors, got first=%#v second=%#v", first, second)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("expected second call to use cached brew outdated error, calls=%#v", fake.calls)
	}
	if !strings.Contains(second.Error, "cached Homebrew outdated unavailable") {
		t.Fatalf("expected cached error message, got %q", second.Error)
	}
}

func TestCollectUpdateSafetyParsesBrewOutdatedJSONWithNonZeroExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "example"`), 0o600); err != nil {
		t.Fatal(err)
	}
	apiServer := httptest.NewServer(http.NotFoundHandler())
	defer apiServer.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", apiServer.URL)
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"formulae":[{"name":"example","installed_versions":["1.0.0"],"current_version":"1.0.1"}],"casks":[]}`,
		Stderr: "brew outdated found outdated packages",
	}}
	gate := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected parsed non-zero brew outdated JSON to produce held gate, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Name != "example" {
		t.Fatalf("expected parsed finding from non-zero brew outdated JSON, got %#v", gate.Findings)
	}
	if !containsSubstring(gate.Warnings, "non-zero but JSON output was parsed") {
		t.Fatalf("expected non-zero parse warning, got %#v", gate.Warnings)
	}
}

func TestCollectUpdateSafetyCachesBrewOutdatedSuccessWithDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "example"`), 0o600); err != nil {
		t.Fatal(err)
	}
	recording := &deadlineRecordingRunner{result: runner.Result{Stdout: `{"formulae":[],"casks":[]}`}}
	first := collectBrewUpdateSafetyWithPolicy(context.Background(), recording, root, securityPolicy{})
	second := collectBrewUpdateSafetyWithPolicy(context.Background(), recording, root, securityPolicy{})
	if first.Status != plan.StatusOK || second.Status != plan.StatusOK {
		t.Fatalf("expected ok brew safety gates, got first=%#v second=%#v", first, second)
	}
	if recording.calls != 2 {
		t.Fatalf("expected second call to use cached brew outdated success, calls=%d", recording.calls)
	}
	if !recording.sawDeadline {
		t.Fatal("expected brew outdated command to run with a deadline")
	}
}

func TestMinHomebrewReleaseAgeEnvOverridesTOMLConfig(t *testing.T) {
	t.Setenv("UPDEV_HOMEBREW_MIN_RELEASE_AGE_DAYS", "1")
	config := parseUpdevConfigTOML("[security.homebrew]\nmin_release_age_days = 5\n")
	if got := minHomebrewReleaseAgeWithConfig(config); got != 24*time.Hour {
		t.Fatalf("expected env release-age override, got %s", got)
	}
}

func TestInventoryCachePathUsesConfiguredStateDir(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory]\nstate_dir = \"state/inventory\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	want := filepath.Join(root, "state", "inventory", "inventory-v1.json")
	if got := inventoryCachePath(root); got != want {
		t.Fatalf("expected configured inventory cache path %q, got %q", want, got)
	}
}

func TestTOMLConfigFeedsThresholdDefaults(t *testing.T) {
	config := parseUpdevConfigTOML(`
[security.homebrew]
min_release_age_days = 5
min_tap_age_days = 21

[security.vscode]
min_install_count = 2500
min_average_rating = 3.5
min_extension_age_days = 30
min_update_age_days = 7
`)
	if got := minHomebrewReleaseAgeWithConfig(config); got != 5*24*time.Hour {
		t.Fatalf("expected Homebrew release age from config, got %s", got)
	}
	if got := minHomebrewTapRepositoryAgeWithConfig(config); got != 21*24*time.Hour {
		t.Fatalf("expected Homebrew tap age from config, got %s", got)
	}
	if got := minVSCodeInstallCountWithConfig(config); got != 2500 {
		t.Fatalf("expected VS Code install threshold from config, got %v", got)
	}
	if got := minVSCodeAverageRatingWithConfig(config); got != 3.5 {
		t.Fatalf("expected VS Code rating threshold from config, got %v", got)
	}
	if got := minVSCodeExtensionAgeWithConfig(config); got != 30*24*time.Hour {
		t.Fatalf("expected VS Code extension age from config, got %s", got)
	}
	if got := minVSCodeUpdateAgeWithConfig(config); got != 7*24*time.Hour {
		t.Fatalf("expected VS Code update age from config, got %s", got)
	}
}

func TestUpdevConfigControlsDefaultUpdateAndProviderModes(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	opts, err := parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.security != "strict" {
		t.Fatalf("expected strict update security by default, got %+v", opts)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[providers]
include_vscode = true

[update]
security = "strict"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", path)
	opts, err = parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.security != "strict" {
		t.Fatalf("expected update security from config, got %+v", opts)
	}
	if !includeVSCodeExtensionsByDefault() {
		t.Fatal("expected VS Code include default from config")
	}
	t.Setenv(includeVSCodeEnvName, "0")
	if includeVSCodeExtensionsByDefault() {
		t.Fatal("expected env override to disable VS Code include default")
	}
	t.Setenv("UPDEV_UPDATE_SECURITY", "off")
	opts, err = parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.security != "off" {
		t.Fatalf("expected env update security override, got %+v", opts)
	}
}

func TestRunUpdateStepDryRunDoesNotExecute(t *testing.T) {
	fake := &fakeCommandRunner{}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], true)
	if step.Status != plan.StatusOK {
		t.Fatalf("expected dry-run step to be ok, got %s", step.Status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("dry-run executed commands: %+v", fake.calls)
	}
}

func TestRunUpdateStepReportsError(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Code: 2, Stderr: "failed", Err: os.ErrPermission}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if step.Status != plan.StatusError {
		t.Fatalf("expected error status, got %s", step.Status)
	}
	if step.Stderr != "failed" {
		t.Fatalf("expected stderr to be preserved, got %q", step.Stderr)
	}
}

func TestRunUpdateStepSummarizesProviderLogs(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Upgrading jq
jq 1.7 -> 1.8.1
Already up-to-date.`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.Updated) != 1 || step.Updated[0] != "jq 1.7 -> 1.8.1" {
		t.Fatalf("expected updated items from provider logs, got %#v", step.Updated)
	}
	if len(step.SkippedItems) != 0 {
		t.Fatalf("generic skipped provider logs should not become outcome rows, got %#v", step.SkippedItems)
	}
}

func TestRunUpdateStepDeduplicatesHomebrewUpgradeProgress(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Upgrading 3 outdated packages:
usage 3.3.0 -> 3.4.0
mise 2026.5.16 -> 2026.5.18
cursor 3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768
mole 1.41.0 -> 1.42.0 (3.5MB)
Upgraded 4 outdated packages
Upgrading usage
3.3.0 -> 3.4.0
Upgrading mise
2026.5.16 -> 2026.5.18
Upgrading cursor
3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768
Upgrading mole
1.41.0 -> 1.42.0`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	want := []string{
		"usage 3.3.0 -> 3.4.0",
		"mise 2026.5.16 -> 2026.5.18",
		"cursor 3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768",
		"mole 1.41.0 -> 1.42.0",
	}
	if strings.Join(step.Updated, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected only package version summary rows, got %#v", step.Updated)
	}
}

func TestUpdateOutcomeRowsSplitItemAndVersionDetail(t *testing.T) {
	report := updateReport{Steps: []updateStep{{
		Name:    "brew",
		Status:  plan.StatusOK,
		Updated: []string{"usage 3.3.0 -> 3.4.0", "Updated 2 taps (homebrew/core and homebrew/cask)."},
	}}}
	rows := updateOutcomeRows(report, 10, false)
	if len(rows) != 2 {
		t.Fatalf("expected two outcome rows, got %#v", rows)
	}
	if rows[0][2] != "usage" || rows[0][3] != "3.3.0 -> 3.4.0" {
		t.Fatalf("expected item/detail split, got %#v", rows[0])
	}
	if rows[1][2] != "Homebrew taps" || !strings.Contains(rows[1][3], "Updated 2 taps") {
		t.Fatalf("expected Homebrew tap update row, got %#v", rows[1])
	}
}

func TestRunUpdateStepNormalizesHomebrewSkippedWarnings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "Warning: Skipping oven-sh/bun because it is not trusted. Run `brew trust oven-sh/bun`."}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.SkippedItems) != 1 || step.SkippedItems[0] != "oven-sh/bun skipped: because it is not trusted. Run `brew trust oven-sh/bun`." {
		t.Fatalf("expected normalized skipped item, got %#v", step.SkippedItems)
	}
	rows := updateOutcomeRows(updateReport{Steps: []updateStep{step}}, 10, false)
	if len(rows) != 1 || rows[0][2] != "oven-sh/bun" || !strings.Contains(rows[0][3], "not trusted") {
		t.Fatalf("expected skipped outcome row to use item name, got %#v", rows)
	}
}

func TestLastUpdateReportNormalizesCachedOutcomes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cached := updateReport{Steps: []updateStep{{
		Name: "brew",
		Updated: []string{
			"mole 1.41.0 -> 1.42.0 (3.5MB)",
			"Upgraded 3 outdated packages",
			"mole 1.41.0 -> 1.42.0",
			"Updated Homebrew from 2920e720fa to 6a64c5ef91.",
		},
		SkippedItems: []string{
			"Warning: Skipping oven-sh/bun because it is not trusted. Run `brew trust oven-sh/bun`.",
			"Already up-to-date.",
		},
	}}}
	_ = saveLastUpdateReport(cached)
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected cached update report to load")
	}
	report := filterUpdateReport(entry.Report, lastReportOptions{})
	step := report.Steps[0]
	wantUpdated := []string{
		"mole 1.41.0 -> 1.42.0",
		"Updated Homebrew from 2920e720fa to 6a64c5ef91.",
	}
	if strings.Join(step.Updated, "\n") != strings.Join(wantUpdated, "\n") {
		t.Fatalf("expected cached update outcomes to be normalized, got %#v", step.Updated)
	}
	if len(step.SkippedItems) != 1 || step.SkippedItems[0] != "oven-sh/bun skipped: because it is not trusted. Run `brew trust oven-sh/bun`." {
		t.Fatalf("expected cached skipped outcomes to be normalized, got %#v", step.SkippedItems)
	}
	rows := updateOutcomeRows(report, 10, false)
	if len(rows) != 3 || rows[0][2] != "mole" || rows[2][2] != "oven-sh/bun" {
		t.Fatalf("expected normalized cached outcome rows, got %#v", rows)
	}
}

func TestRunUpdateStepIgnoresGenericHomebrewProgressLogs(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Updating Homebrew...
Auto-updating Homebrew...
Adjust how often this is run with $HOMEBREW_AUTO_UPDATE_SECS or disable with $HOMEBREW_NO_AUTO_UPDATE.
Already up-to-date.`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.Updated) != 0 || len(step.SkippedItems) != 0 {
		t.Fatalf("generic Homebrew progress logs should not become outcome rows, got updated=%#v skipped=%#v", step.Updated, step.SkippedItems)
	}
}

func TestRunUpdateStepStreamsProviderLogsWhenRequested(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "brew stdout\n", Stderr: "brew stderr\n"}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	step := updateSteps()[0]
	result := runUpdateStepWithWriters(context.Background(), fake, step, false, "", &stdout, &stderr)
	if result.Status != plan.StatusOK {
		t.Fatalf("expected ok step, got %#v", result)
	}
	if !strings.Contains(stdout.String(), "brew stdout") || !strings.Contains(stderr.String(), "brew stderr") {
		t.Fatalf("expected provider logs to stream, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestShouldStreamUpdateProviderLogs(t *testing.T) {
	plainOpts, err := parseUpdateOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		opts updateOptions
		want bool
	}{
		{
			name: "text update streams provider logs",
			opts: updateOptions{format: "text"},
			want: true,
		},
		{
			name: "interactive text update still streams provider logs",
			opts: updateOptions{format: "text", tui: true},
			want: true,
		},
		{
			name: "plain text update still streams provider logs",
			opts: plainOpts,
			want: true,
		},
		{
			name: "dry run keeps deterministic output",
			opts: updateOptions{format: "text", dryRun: true, tui: true},
			want: false,
		},
		{
			name: "json keeps deterministic output",
			opts: updateOptions{format: "json"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStreamUpdateProviderLogs(tt.opts); got != tt.want {
				t.Fatalf("shouldStreamUpdateProviderLogs()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestRunUpdateStepCanBeHeldByStrictSafety(t *testing.T) {
	fake := &fakeCommandRunner{}
	step := runUpdateStepWithHold(context.Background(), fake, updateSteps()[0], false, "security=strict held update")
	if step.Status != plan.StatusHeld || !step.Skipped {
		t.Fatalf("expected held skipped step, got %#v", step)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("held step executed commands: %+v", fake.calls)
	}
}

func TestRunUpdateStrictSafetyHoldsTooNewBrewCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
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
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"brew\x00tap": runner.Result{Stdout: "homebrew/core\n"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict"}, fake)
	if code != 2 {
		t.Fatalf("expected held update exit code 2, got %d", code)
	}
	for _, call := range fake.calls {
		if strings.Join(call, " ") == "bash -lc brew update && brew upgrade --greedy && brew cleanup" {
			t.Fatalf("strict safety hold executed brew upgrade: %#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	if entry.Report.Status != plan.StatusHeld || len(entry.Report.Safety) != 2 || entry.Report.Safety[0].Status != plan.StatusHeld {
		t.Fatalf("expected held safety report, got %#v", entry.Report)
	}
	if len(entry.Report.Safety[0].Findings) != 1 || entry.Report.Safety[0].Findings[0].Decision != "hold" {
		t.Fatalf("expected too-new hold finding, got %#v", entry.Report.Safety[0].Findings)
	}
}

func TestRunUpdateStrictSafetyHoldsMiseCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"brew\x00tap": runner.Result{Stdout: ""},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2":                                   {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"):                            {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`},
		strings.Join([]string{"bash", "-lc", "brew update && brew upgrade --greedy && brew cleanup"}, "\x00"): {Stdout: "Already up-to-date."},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict"}, fake)
	if code != 2 {
		t.Fatalf("expected held update exit code 2, got %d", code)
	}
	for _, call := range fake.calls {
		if strings.Join(call, " ") == "zsh -c source ~/.zshenv && mise upgrade && mise prune" {
			t.Fatalf("strict safety hold executed mise upgrade: %#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	if entry.Report.Status != plan.StatusHeld || len(entry.Report.Safety) != 2 || entry.Report.Safety[1].Status != plan.StatusHeld {
		t.Fatalf("expected held mise safety report, got %#v", entry.Report)
	}
	if len(entry.Report.Safety[1].Findings) != 1 || entry.Report.Safety[1].Findings[0].Provider != "mise" || entry.Report.Safety[1].Findings[0].Decision != "review" {
		t.Fatalf("expected mise review finding, got %#v", entry.Report.Safety[1].Findings)
	}
}

func TestUpdateStepSummaryTextReportsSkippedHeldSteps(t *testing.T) {
	got := updateStepSummaryText([]updateStep{
		{Name: "brew", Status: plan.StatusHeld, Skipped: true, SkippedItems: []string{"security held"}},
		{Name: "mise", Status: plan.StatusOK, Updated: []string{"node 22 -> 24"}},
	})
	if got != "2 steps, 1 updated, 1 deferred, 1 held, 1 skipped" {
		t.Fatalf("unexpected update step summary: %q", got)
	}
}

func TestPrintUpdateTextIncludesSkippedStepStatus(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Report:   "/tmp/last-update.json",
		Security: "strict",
		Steps: []updateStep{
			{Name: "brew", Command: []string{"brew", "upgrade"}, Status: plan.StatusHeld, Skipped: true, Reason: "security=strict held update because safety gate requires review"},
			{Name: "mise", Command: []string{"mise", "upgrade"}, Status: plan.StatusOK, Updated: []string{"node 22.0.0 -> 22.1.0"}},
		},
	})
	got := buffer.String()
	for _, want := range []string{"update summary: 2 steps, 1 updated, 1 deferred, 1 held, 1 skipped", "update outcome", "node", "22.0.0 -> 22.1.0", "skipped", "brew", "yes", "reason: security=strict held update"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected update text to include %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "brew upgrade") || strings.Contains(got, "mise upgrade") || strings.Contains(got, "command") {
		t.Fatalf("expected human update text to hide raw commands, got %q", got)
	}
}

func TestSafetyHumanTextLocalizesReleaseAgeWarnings(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	finding := safetyFinding{
		Provider:          "brew",
		Kind:              "brew",
		Name:              "libomp",
		InstalledVersions: []string{"22.1.6"},
		CurrentVersion:    "22.1.7",
		Decision:          "hold",
		Reason:            "candidate release is too new: age 0 days, minimum 3 days",
		Remediation:       "wait until the release reaches the minimum age or allow temporarily by policy after review",
	}
	report := updateReport{
		Security: "warn",
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{finding},
		}},
	}
	var dashboard bytes.Buffer
	printUpdateSafetyDashboard(&dashboard, report, false)
	if got := dashboard.String(); !strings.Contains(got, "候補リリースが新しすぎます") || strings.Contains(got, "candidate release is too new") {
		t.Fatalf("expected localized safety dashboard reason, got %q", got)
	}
	var details bytes.Buffer
	printSafetyFindingDetails(&details, report.Safety, false)
	if got := details.String(); !strings.Contains(got, "候補リリースが新しすぎます") || !strings.Contains(got, "リリースが最小経過日数に達するまで") {
		t.Fatalf("expected localized safety details, got %q", got)
	}
	row := safetyFindingDetailRow(report.Safety[0], finding)
	metadata := strings.Join(row.Metadata, "\n")
	if !strings.Contains(row.Summary, "候補リリースが新しすぎます") || !strings.Contains(row.Detail, "リリースが最小経過日数に達するまで") || !strings.Contains(metadata, "候補リリースが新しすぎます") {
		t.Fatalf("expected localized safety detail row, row=%#v metadata=%q", row, metadata)
	}
}

func TestSecurityReviewTextLocalizesCandidateReasons(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	report := securityReviewReport{
		Status: plan.StatusHeld,
		Root:   "/repo",
		Candidates: []securityReviewCandidate{{
			Provider:    "github-repo",
			Kind:        "github",
			Name:        "owner/repo",
			Decision:    "review",
			Reason:      "repository is archived",
			Remediation: "replace the archived repository source or add a temporary policy override after review",
			Prompt:      "Review updev security candidate github-repo/github owner/repo.",
		}},
	}
	var buffer bytes.Buffer
	printSecurityReviewText(&buffer, report)
	got := buffer.String()
	if !strings.Contains(got, "reason: repository が archived です") || !strings.Contains(got, "remediation: archived repository source を置き換える") {
		t.Fatalf("expected localized review candidate reason/remediation, got %q", got)
	}
	if strings.Contains(got, "reason: repository is archived") || strings.Contains(got, "remediation: replace the archived repository source") {
		t.Fatalf("expected review candidate text to avoid English reason/remediation, got %q", got)
	}
}

func withDefaultLanguageForTest(t *testing.T, lang string) {
	t.Helper()
	old, hadOld := os.LookupEnv("UPDEV_LANG")
	_ = os.Setenv("UPDEV_LANG", lang)
	defaultLanguageOnce = sync.Once{}
	defaultLanguageValue = ""
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv("UPDEV_LANG", old)
		} else {
			_ = os.Unsetenv("UPDEV_LANG")
		}
		defaultLanguageOnce = sync.Once{}
		defaultLanguageValue = ""
	})
}

func TestPrintUpdateTextOmitsEmptyDetailColumn(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusOK,
		Root:     "/repo",
		Security: "off",
		Steps: []updateStep{
			{Name: "brew", Status: plan.StatusOK},
			{Name: "mise", Status: plan.StatusOK},
		},
	})
	got := buffer.String()
	if strings.Contains(got, "detail") || strings.Contains(got, "詳細") {
		t.Fatalf("expected empty detail column to be omitted, got %q", got)
	}
}

func TestUpdateOutcomeRowsPreferGateProviderForSecurityFindings(t *testing.T) {
	rows := updateOutcomeRows(updateReport{
		Safety: []safetyGate{{
			Provider: "vscode",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider:          "brew",
				Kind:              "vscode",
				Name:              "publisher.extension",
				InstalledVersions: []string{"1.0.0"},
				CurrentVersion:    "1.1.0",
				Decision:          "hold",
			}},
		}},
	}, 10, false)
	if len(rows) != 1 || rows[0][1] != "vscode" || rows[0][3] != "1.0.0 -> 1.1.0" {
		t.Fatalf("expected vscode provider and concise version detail, got %#v", rows)
	}
}

func TestUpdateOutcomeRowsColorUpdatedItems(t *testing.T) {
	rows := updateOutcomeRows(updateReport{
		Steps: []updateStep{{
			Name:    "mise",
			Status:  plan.StatusOK,
			Updated: []string{"node 22.0.0 -> 24.0.0"},
		}},
	}, 10, true)
	if len(rows) != 1 {
		t.Fatalf("expected updated row, got %#v", rows)
	}
	for _, column := range []int{0, 2, 3} {
		if !strings.Contains(rows[0][column], "\033[32m") {
			t.Fatalf("expected updated column %d to be green, got %#v", column, rows[0])
		}
	}
}

func TestPrintUpdateTextShowsWarnModeSafetyAsWarnings(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusOK,
		Root:     "/repo",
		Security: "warn",
		Steps: []updateStep{{
			Name:    "brew",
			Status:  plan.StatusOK,
			Updated: []string{"mise 2026.5.16 -> 2026.5.18"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Summary:  &safetySummary{Findings: 1, Hold: 1},
			Findings: []safetyFinding{{
				Provider:          "brew",
				Kind:              "brew",
				Name:              "mise",
				InstalledVersions: []string{"2026.5.16"},
				CurrentVersion:    "2026.5.18",
				Decision:          "hold",
				Reason:            "candidate release is too new",
			}},
		}},
	})
	got := buffer.String()
	for _, want := range []string{"safety summary: 1 gates, 1 warnings", "warning", "mise", "2026.5.16 -> 2026.5.18"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected warn-mode update text to include %q, got %q", want, got)
		}
	}
	for _, unwanted := range []string{"held", "hold"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("expected warn-mode update text to avoid %q wording, got %q", unwanted, got)
		}
	}
}

func TestPrintUpdateTextUsesCompactInventoryDashboard(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusDrift,
		Root:     "/repo",
		Security: "off",
		Steps: []updateStep{
			{Name: "brew", Command: []string{"brew", "upgrade"}, Status: plan.StatusOK},
		},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Providers: []plan.ProviderSummary{
				{Name: "brew", Desired: 1, Live: 2, Extra: 1},
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true, Detail: "JSON processor"},
				{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: profileMismatchDetail("personal")},
				{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusOK, Desired: true, Live: true, Detail: "Node runtime"},
			},
		},
	})
	got := buffer.String()
	for _, want := range []string{"inventory drift", "top inventory items", "jq", "profile", "profile-mismatch"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected compact update dashboard to include %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "updev last --format json") || strings.Contains(got, "\nnext\n") {
		t.Fatalf("expected human update dashboard to avoid follow-up command lists, got %q", got)
	}
	if strings.Contains(got, "brew / brew") || strings.Contains(got, "Node runtime") {
		t.Fatalf("expected update dashboard to avoid full inventory tables, got %q", got)
	}
}

func TestSaveAndLoadLastUpdateReport(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	path := saveLastUpdateReport(updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Security: "strict",
		Steps: []updateStep{
			{Name: "brew", Status: plan.StatusHeld, Skipped: true},
		},
	})
	if path == "" {
		t.Fatal("expected cached report path")
	}
	if !strings.HasPrefix(path, filepath.Join(cacheHome, "updev", "reports")) {
		t.Fatalf("expected report under XDG cache, got %q", path)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected cached report to load")
	}
	if entry.Report.Status != plan.StatusHeld || entry.Report.Root != "/repo" || entry.Report.Report != path {
		t.Fatalf("unexpected cached report: %#v", entry)
	}
}

func TestPrintLastReportTextDoesNotRepeatUpdateHeader(t *testing.T) {
	var buffer bytes.Buffer
	printLastReportText(&buffer, updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status:   plan.StatusOK,
			Root:     "/repo",
			Security: "off",
			Steps: []updateStep{
				{Name: "brew", Status: plan.StatusOK},
			},
		},
	}, lastReportOptions{section: "summary"})
	got := buffer.String()
	if !strings.Contains(got, "updev last ok") || strings.Contains(got, "updev update") {
		t.Fatalf("expected last report to reuse update body without update header, got %q", got)
	}
}

func TestBuildUpdateReportSectionViewFiltersInventory(t *testing.T) {
	entry := updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status: plan.StatusDrift,
			Inventory: plan.Report{
				Status: plan.StatusDrift,
				Providers: []plan.ProviderSummary{
					{Name: "brew", Desired: 1, Live: 2, Extra: 1},
					{Name: "mise", Desired: 1, Live: 1},
				},
				Items: []plan.Item{
					{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
					{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
				},
			},
		},
	}
	view := buildUpdateReportSectionView(entry, lastReportOptions{section: "inventory", provider: "brew", status: "attention"})
	if view.Section != "inventory" || view.Inventory == nil {
		t.Fatalf("expected inventory view, got %#v", view)
	}
	if view.Status != plan.StatusDrift {
		t.Fatalf("expected inventory section status to come from inventory, got %s", view.Status)
	}
	if len(view.Inventory.Items) != 1 || view.Inventory.Items[0].Name != "jq" {
		t.Fatalf("expected filtered attention inventory item, got %#v", view.Inventory.Items)
	}
	if view.Summary.InventoryAttention != 1 || view.Filters["provider"] != "brew" {
		t.Fatalf("unexpected view summary/filters: %#v", view)
	}
}

func TestFilterPlanProvidersKeepsProviderWhenOnlyItemQueryIsSet(t *testing.T) {
	providers := []plan.ProviderSummary{{Name: "brew", Desired: 1, Live: 1}}
	filtered := filterPlanProviders(providers, lastReportOptions{provider: "brew", query: "ripgrep"})
	if len(filtered) != 1 || filtered[0].Name != "brew" {
		t.Fatalf("expected provider filter to survive item query, got %#v", filtered)
	}
}

func TestPrintLastReportSecurityDetails(t *testing.T) {
	var buffer bytes.Buffer
	printLastReportText(&buffer, updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status: plan.StatusHeld,
			Root:   "/repo",
			Safety: []safetyGate{{
				Provider: "brew",
				Status:   plan.StatusHeld,
				Findings: []safetyFinding{{
					Provider:    "brew",
					Kind:        "cask",
					Name:        "demo",
					Decision:    "hold",
					Reason:      "needs provenance review",
					Remediation: "verify upstream",
					Evidence:    []string{"unsigned cask"},
				}},
			}},
		},
	}, lastReportOptions{section: "security", status: "attention", details: true})
	got := buffer.String()
	for _, want := range []string{"section: security", "security details", "brew/cask demo", "needs provenance review", "unsigned cask"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected security detail output to include %q, got %q", want, got)
		}
	}
}

func TestUpdateDetailRowsExposeInventorySecurityAndLogs(t *testing.T) {
	report := updateReport{
		Status: plan.StatusHeld,
		Steps: []updateStep{{
			Name:         "brew",
			Command:      []string{"brew", "upgrade"},
			Status:       plan.StatusHeld,
			Reason:       "security review required",
			Stdout:       "kept current version",
			Updated:      []string{"jq 1.7 -> 1.8.1"},
			SkippedItems: []string{"demo held by policy"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider:      "brew",
				Kind:          "cask",
				Name:          "demo",
				Decision:      "hold",
				Reason:        "needs provenance review",
				Remediation:   "verify upstream",
				Evidence:      []string{"unsigned cask"},
				FixedVersions: []string{"1.2.3"},
			}, {
				Provider: "brew",
				Kind:     "brew",
				Name:     "safe",
				Decision: "allow",
				Reason:   "already trusted",
			}},
		}},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Items: []plan.Item{
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true, Detail: "extra package"},
				{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	if rows := updateInventoryDetailRows(report); len(rows) != 1 || rows[0].Title != "brew/brew jq" {
		t.Fatalf("expected attention inventory detail row, got %#v", rows)
	}
	securityRows := updateSecurityDetailRows(report)
	if len(securityRows) != 1 || !strings.Contains(strings.Join(securityRows[0].Metadata, " "), "fixed: 1.2.3") || !strings.Contains(strings.Join(securityRows[0].Metadata, " "), "decision: hold") {
		t.Fatalf("expected security finding metadata, got %#v", securityRows)
	}
	if len(securityRows[0].Actions) != 5 || !strings.Contains(securityRows[0].Actions[0].Value, securityDetailActionPrefix) {
		t.Fatalf("expected held security row to expose policy actions, got %#v", securityRows[0].Actions)
	}
	allowRows := updateSecurityDetailRowsForFilter(report, lastReportOptions{section: "security", status: "allow"})
	if len(allowRows) != 2 || allowRows[1].Title != "brew/brew safe" {
		t.Fatalf("expected explicit allow filter to show allow findings, got %#v", allowRows)
	}
	logRows := updateLogDetailRows(report)
	if len(logRows) != 3 {
		t.Fatalf("expected updated item, deferred item, and provider log rows, got %#v", logRows)
	}
	if logRows[0].Status != "updated" || logRows[0].Summary != "jq 1.7 -> 1.8.1" {
		t.Fatalf("expected first update detail row to be item-level updated row, got %#v", logRows[0])
	}
	if logRows[1].Status != "held" || logRows[1].Summary != "demo held by policy" || len(logRows[1].Actions) != 1 {
		t.Fatalf("expected second update detail row to be held item row with security action, got %#v", logRows[1])
	}
	providerLog := logRows[2]
	logMetadata := strings.Join(providerLog.Metadata, " ")
	if !strings.Contains(logMetadata, "stdout: kept current version") || !strings.Contains(logMetadata, "updated: jq") || !strings.Contains(logMetadata, "deferred: demo held") {
		t.Fatalf("expected update provider log metadata, got %#v", providerLog)
	}
	if providerLog.Summary != "security review required" {
		t.Fatalf("expected reason to remain update log summary, got %#v", providerLog)
	}
	if len(providerLog.Actions) != 1 || providerLog.Actions[0].Value != updateHubActionSecurity {
		t.Fatalf("expected held update log row to link to security detail actions, got %#v", providerLog.Actions)
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(securityRows[0].Actions[0].Value); !ok || action != "allow-7d-rerun" || provider != "brew" || kind != "cask" || name != "demo" {
		t.Fatalf("unexpected security detail action parse: action=%q provider=%q kind=%q name=%q ok=%v", action, provider, kind, name, ok)
	}
	nonRerunnable := securityDetailActions(safetyGate{Provider: "github-repo"}, safetyFinding{Kind: "repo", Name: "owner/tool", Decision: "hold"})
	if len(nonRerunnable) != 3 || strings.Contains(nonRerunnable[0].Value, "rerun") {
		t.Fatalf("expected non-update provider security actions to omit rerun, got %#v", nonRerunnable)
	}
	if action, _, _, _, ok := parseSecurityDetailAction(nonRerunnable[0].Value); !ok || action != "allow-custom" {
		t.Fatalf("expected custom allow to be the first non-rerunnable action, got %#v", nonRerunnable)
	}
}

func TestUpdateLogDetailRowsDistinguishSkippedErrorAndPreserveLogLines(t *testing.T) {
	rows := updateLogDetailRows(updateReport{Steps: []updateStep{
		{
			Name:    "brew",
			Command: []string{"brew", "upgrade"},
			Status:  plan.StatusOK,
			Skipped: true,
			Reason:  "dry run",
			Stdout:  "line one\nline two",
		},
		{
			Name:    "mise",
			Command: []string{"mise", "upgrade"},
			Status:  plan.StatusError,
			Stderr:  "error one\nerror two",
		},
	}})
	if len(rows) != 2 {
		t.Fatalf("expected skipped and error provider log rows, got %#v", rows)
	}
	if rows[0].Status != "skipped" || !strings.Contains(strings.Join(rows[0].Metadata, " "), "skipped: true") {
		t.Fatalf("expected skipped provider row to expose skipped state, got %#v", rows[0])
	}
	if rows[1].Status != string(plan.StatusError) || rows[1].Summary != "error one error two" {
		t.Fatalf("expected error provider row to summarize stderr, got %#v", rows[1])
	}
	expanded := strings.Join(detailBrowserExpandedLinesWithWidth(rows[0], 80), "\n")
	if !strings.Contains(expanded, "stdout: line one") || !strings.Contains(expanded, "line two") {
		t.Fatalf("expected expanded update log to preserve stdout newlines, got %q", expanded)
	}
}

func TestUpdateHubChoicesExposeNavigationTargets(t *testing.T) {
	manualPlan := inventoryPlanReport{ActionCounts: map[string]int{"adopt-brew": 2}, AttentionCount: 2}
	backendPlan := backendPlanReport{Findings: []backendFinding{{Name: "ripgrep", RecommendedName: "ripgrep"}}}
	choices := updateHubChoices(updateReport{Safety: []safetyGate{{Provider: "brew", Status: plan.StatusHeld}}}, manualPlan, backendPlan, updateHubActionManualPlan)
	values := map[string]bool{}
	for _, choice := range choices {
		values[choice.Value] = true
	}
	for _, want := range []string{updateHubActionInventoryAll, updateHubActionInventoryAttention, updateHubActionInventoryDetails, updateHubActionManualPlan, updateHubActionBackends, updateHubActionUpdatesFilter, updateHubActionSecurity, updateHubActionSecurityFilter, updateHubActionLogs, updateHubActionJSON, updevActionExit} {
		if !values[want] {
			t.Fatalf("expected update hub choice %q in %#v", want, choices)
		}
	}
	selected := ""
	for _, choice := range choices {
		if choice.Selected {
			selected = choice.Value
		}
	}
	if selected != updateHubActionManualPlan {
		t.Fatalf("expected manual plan to be selected when review actions exist, got %#v", choices)
	}
	if manualPlan.AttentionCount != 2 {
		t.Fatalf("expected manual plan attention count to include adoption actions")
	}
}

func TestUpdateDashboardDetailRowsExposeHubActions(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Report:   "/tmp/last-update.json",
		Security: "strict",
		Steps: []updateStep{{
			Name:         "brew",
			Status:       plan.StatusHeld,
			Updated:      []string{"jq 1.7 -> 1.8.1"},
			SkippedItems: []string{"demo held"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "cask",
				Name:     "demo",
				Decision: "hold",
				Reason:   "review provenance",
			}},
		}},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Items: []plan.Item{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "jq",
				Status:   plan.StatusExtra,
			}},
			Providers: []plan.ProviderSummary{{Name: "brew", Desired: 1, Live: 2}},
		},
	}
	manualPlan := inventoryPlanReport{ActionCounts: map[string]int{"needs-review": 1}, AttentionCount: 1}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{Type: "mise-backend-rewrite"}}}
	rows := updateDashboardDetailRows(report, manualPlan, backendPlan)
	byTitle := map[string]detailBrowserRow{}
	for _, row := range rows {
		byTitle[row.Title] = row
	}
	for _, tc := range []struct {
		title string
		want  string
	}{
		{"Update steps", updateHubActionUpdatesFilter},
		{"Security", updateHubActionSecurity},
		{"Inventory", updateHubActionInventoryAll},
		{"Manual review", updateHubActionManualPlan},
		{"Backend convergence", updateHubActionBackends},
		{"Full report", updateHubActionFull},
	} {
		row := byTitle[tc.title]
		if len(row.Actions) == 0 || row.Actions[0].Value != tc.want {
			t.Fatalf("expected dashboard row %q to expose action %q, got %#v", tc.title, tc.want, row.Actions)
		}
	}
	backendMetadata := strings.Join(byTitle["Backend convergence"].Metadata, " ")
	if !strings.Contains(backendMetadata, "mise/github") || !strings.Contains(backendMetadata, "safe actions:") {
		t.Fatalf("expected backend preference metadata to include mise/github, got %#v", byTitle["Backend convergence"].Metadata)
	}
	dashboardView := newDetailBrowserModel("updev dashboard", rows, detailBrowserState{}, false).View().Content
	if !strings.Contains(dashboardView, "focused actions: a/1=filter updates") {
		t.Fatalf("expected dashboard view to expose focused row action hint:\n%s", dashboardView)
	}
	summaryModel := newUpdateSummaryBrowserModel(updateHubTitle(report), report, manualPlan, backendPlan, detailBrowserState{}, updateHubActionLogs, false)
	summaryModel.Height = 80
	dashboardView = summaryModel.View().Content
	for _, want := range []string{"updev update held", "root: /repo", "security: strict", "safety summary:", "update summary:", "focused actions:", "a/1=open update details", "updates", "security attention", "inventory drift", "review actions"} {
		if !strings.Contains(dashboardView, want) {
			t.Fatalf("expected update hub view to contain %q:\n%s", want, dashboardView)
		}
	}
	summaryActions := updateSummaryActionsByText(summaryModel.Lines)
	for _, tc := range []struct {
		contains string
		want     string
	}{
		{"updates", updateHubActionLogs},
		{"security attention", updateHubActionSecurity},
		{"inventory drift", updateHubActionInventoryAll},
		{"top inventory items", updateHubActionInventoryDetails},
		{"report:", updateHubActionFull},
		{"manual review", updateHubActionManualPlan},
		{"backend convergence", updateHubActionBackends},
	} {
		if got := summaryActions[tc.contains]; got != tc.want {
			t.Fatalf("expected summary row containing %q to route to %q, got %q", tc.contains, tc.want, got)
		}
	}
	if route, ok := firstUpdateSummaryRoute(summaryModel.Lines, "brew"); !ok || route.Base != updateHubActionLogs || route.Provider != "brew" {
		t.Fatalf("expected brew update row to route to provider-filtered logs, route=%+v ok=%v", route, ok)
	}
	reviewFocused := newUpdateSummaryBrowserModel(updateHubTitle(report), report, manualPlan, backendPlan, detailBrowserState{}, updateHubActionManualPlan, false)
	reviewFocused.Height = 80
	reviewView := reviewFocused.View().Content
	if !strings.Contains(reviewView, "action") || !strings.Contains(reviewView, "summary") {
		t.Fatalf("expected review actions to render as a small table:\n%s", reviewView)
	}
	if strings.Contains(reviewView, "[Enter: open manual review]") {
		t.Fatalf("expected review action row to avoid inline Enter badge:\n%s", reviewView)
	}
	coloredSummaryModel := newUpdateSummaryBrowserModel(updateHubTitle(report), report, manualPlan, backendPlan, detailBrowserState{}, updateHubActionLogs, true)
	coloredSummaryModel.Height = 80
	coloredSummaryView := coloredSummaryModel.View().Content
	if !strings.Contains(coloredSummaryView, "\033[1m\033[35mupdates") || !strings.Contains(coloredSummaryView, "\033[1m\033[35mreview actions") {
		t.Fatalf("expected summary section titles to be visually styled:\n%q", coloredSummaryView)
	}
	if !updateHubActionExists(updateHubActionBackends) || updateHubActionExists("unknown") {
		t.Fatalf("unexpected update hub action existence result")
	}
	for _, tc := range []struct {
		action string
		index  int
		title  string
	}{
		{updateHubActionManualPlan, 3, "Manual review"},
		{updateHubActionBackends, 4, "Backend convergence"},
		{updateHubActionSecurity, 1, "Security"},
		{updateHubActionInventoryDetails, 2, "Inventory"},
		{updateHubActionUpdatesFilter, 0, "Update steps"},
	} {
		index := updateDashboardRowIndexForAction(tc.action)
		if index != tc.index || rows[index].Title != tc.title {
			t.Fatalf("expected dashboard action %q to focus row %d/%q, got %d/%q", tc.action, tc.index, tc.title, index, rows[index].Title)
		}
	}
	for _, tc := range []struct {
		listAction string
		want       string
	}{
		{listHubActionManual, updateHubActionManualPlan},
		{listHubActionBackends, updateHubActionBackends},
		{listHubActionUpdates, updateHubActionLogs},
		{listHubActionSecurity, updateHubActionSecurity},
		{"unknown", ""},
	} {
		if got := updateHubActionFromListAction(tc.listAction); got != tc.want {
			t.Fatalf("list action %q mapped to %q, want %q", tc.listAction, got, tc.want)
		}
	}
	if got := initialUpdateHubAction("", updateHubActionManualPlan); got != updateHubActionDashboard {
		t.Fatalf("expected bare update hub to open summary first, got %q", got)
	}
	if got := initialUpdateHubAction(updateHubActionSecurity, updateHubActionSecurity); got != updateHubActionSecurity {
		t.Fatalf("expected explicit preferred section to open directly, got %q", got)
	}
}

func updateSummaryActionsByText(lines []updateSummaryLine) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		plain := strings.ToLower(line.Text)
		for _, key := range []string{"updates", "security attention", "inventory drift", "top inventory items", "report:", "manual review", "backend convergence"} {
			if strings.Contains(plain, key) && line.Action != "" {
				action := line.Action
				if route, ok := parseUpdateSummaryRoute(action); ok {
					action = route.Base
				}
				out[key] = action
			}
		}
	}
	return out
}

func firstUpdateSummaryRoute(lines []updateSummaryLine, contains string) (updateSummaryRoute, bool) {
	for _, line := range lines {
		if !strings.Contains(strings.ToLower(line.Text), strings.ToLower(contains)) {
			continue
		}
		if route, ok := parseUpdateSummaryRoute(line.Action); ok {
			return route, true
		}
	}
	return updateSummaryRoute{}, false
}

func TestUpdateFacetCounts(t *testing.T) {
	steps := []updateStep{
		{Name: "brew", Status: plan.StatusOK},
		{Name: "brew", Status: plan.StatusHeld},
		{Name: "mise", Status: plan.StatusError},
	}
	if counts := updateStepProviderCounts(steps); counts["brew"] != 2 || counts["mise"] != 1 {
		t.Fatalf("unexpected update provider counts: %#v", counts)
	}
	if counts := updateStepStatusCounts(steps); counts["ok"] != 1 || counts["held"] != 1 || counts["error"] != 1 || counts["attention"] != 2 {
		t.Fatalf("unexpected update status counts: %#v", counts)
	}
	gates := []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusError,
		Error:    "brew metadata unavailable",
	}, {
		Provider: "vscode",
		Status:   plan.StatusHeld,
		Warnings: []string{"publisher unverified"},
		Findings: []safetyFinding{
			{Decision: "hold"},
			{Decision: "review"},
			{Decision: "allow"},
		},
	}}
	if counts := safetyProviderCounts(gates); counts["brew"] != 1 || counts["vscode"] != 4 {
		t.Fatalf("unexpected safety provider counts: %#v", counts)
	}
	if counts := safetyDecisionCounts(gates); counts["error"] != 1 || counts["hold"] != 1 || counts["review"] != 1 || counts["allow"] != 1 || counts["attention"] != 4 {
		t.Fatalf("unexpected safety decision counts: %#v", counts)
	}
}

func TestUpdateExitCodeTreatsHeldAsNonSuccess(t *testing.T) {
	if got := updateExitCode(plan.StatusHeld); got != 2 {
		t.Fatalf("expected held exit code 2, got %d", got)
	}
	if got := updateExitCode(plan.StatusBlocked); got != 3 {
		t.Fatalf("expected blocked exit code 3, got %d", got)
	}
	if got := updateExitCode(plan.StatusError); got != 1 {
		t.Fatalf("expected error exit code 1, got %d", got)
	}
	if got := updateExitCode(plan.StatusOK); got != 0 {
		t.Fatalf("expected ok exit code 0, got %d", got)
	}
}

func TestParseBrewOutdatedBuildsSafetyFindings(t *testing.T) {
	raw := `{
  "formulae": [
    {"name": "jq", "installed_versions": ["1.7"], "current_version": "1.8.1"}
  ],
  "casks": [
    {"name": "firefox", "installed_versions": "150.0", "current_version": "151.0"}
  ]
}`
	findings, err := parseBrewOutdated(raw, brewSafetyManifest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected two findings, got %#v", findings)
	}
	if findings[0].Kind != "brew" || findings[0].Decision != "unknown" || findings[0].CurrentVersion != "1.8.1" {
		t.Fatalf("unexpected formula finding: %#v", findings[0])
	}
	if findings[1].Kind != "cask" || findings[1].Decision != "review" || findings[1].InstalledVersions[0] != "150.0" {
		t.Fatalf("unexpected cask finding: %#v", findings[1])
	}
}

func TestParseBrewOutdatedIgnoresAutoUpdatePrefix(t *testing.T) {
	raw := `==> Auto-updating Homebrew...
Adjust how often this is run with $HOMEBREW_AUTO_UPDATE_SECS.
{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`
	findings, err := parseBrewOutdated(raw, brewSafetyManifest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Name != "jq" {
		t.Fatalf("expected JSON payload to be parsed after prefix logs, got %#v", findings)
	}
}

func TestParseMiseOutdatedBuildsReviewFindings(t *testing.T) {
	findings, err := parseMiseOutdated(`{
  "github:openai/codex": {"requested": "0.60.0", "current": "0.60.0", "latest": "0.61.0"},
  "node": {"requested": "24.0.0", "current": "24.0.0", "latest": "24.1.0"}
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected two mise findings, got %#v", findings)
	}
	if findings[0].Provider != "mise" || findings[0].Kind != "tool" || findings[0].Name != "github:openai/codex" || findings[0].Decision != "review" {
		t.Fatalf("unexpected first mise finding: %#v", findings[0])
	}
	if findings[0].InstalledVersions[0] != "0.60.0" || findings[0].CurrentVersion != "0.61.0" || findings[0].Version != "0.60.0" {
		t.Fatalf("expected mise versions to be preserved, got %#v", findings[0])
	}
}

func TestBrewOutdatedCachedErrorRejectsProviderLogNoise(t *testing.T) {
	if brewOutdatedCachedErrorIsReusable("==> Auto-updating Homebrew... Adjust how often this is run with $HOMEBREW_AUTO_UPDATE_SECS.") {
		t.Fatal("expected Homebrew auto-update log cache to be ignored")
	}
	if brewOutdatedCachedErrorIsReusable("==> Tapping homebrew/core\nCloning into '/usr/local/Homebrew/Library/Taps/homebrew/homebrew-core'...\nUpdating files: 100%") {
		t.Fatal("expected Homebrew tap clone log cache to be ignored")
	}
	if !brewOutdatedCachedErrorIsReusable("brew outdated --json=v2 timed out after 15s") {
		t.Fatal("expected real unavailable cache to be reusable")
	}
}

func TestBrewSafetyManifestAddsProvenanceFindings(t *testing.T) {
	manifest, err := parseBrewSafetyManifest(strings.NewReader(`
brew "jq"
cask "muxy-app/tap/muxy"
cask "https://example.com/custom-app.rb"
`), "/tmp/Brewfile")
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "formulae": [
    {"name": "jq", "installed_versions": ["1.7"], "current_version": "1.8.1"}
  ],
  "casks": [
    {"name": "muxy", "installed_versions": "1.0", "current_version": "1.1"}
  ]
}`
	findings, err := parseBrewOutdated(raw, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if findings[0].Name != "jq" || findings[0].Decision != "unknown" {
		t.Fatalf("expected core formula to remain release-age unknown, got %#v", findings[0])
	}
	if findings[1].Name != "muxy" || findings[1].Decision != "review" || findings[1].Tap != "muxy-app/tap" {
		t.Fatalf("expected custom tap cask review finding, got %#v", findings[1])
	}
	if !strings.Contains(findings[1].Remediation, "tap repository") {
		t.Fatalf("expected tap remediation, got %#v", findings[1])
	}
	warnings := manifestWarnings(manifest)
	if len(warnings) != 1 || warnings[0].Name != "https://example.com/custom-app.rb" || warnings[0].Decision != "review" {
		t.Fatalf("expected URL cask manifest warning, got %#v", warnings)
	}
	if !strings.Contains(warnings[0].Remediation, "cask source URL") {
		t.Fatalf("expected URL cask remediation, got %#v", warnings[0])
	}
}

func TestApplyHomebrewSafetyMetadataAllowsOfficialFormula(t *testing.T) {
	finding := safetyFinding{
		Provider:          "brew",
		Kind:              "brew",
		Name:              "jq",
		InstalledVersions: []string{"1.7"},
		CurrentVersion:    "1.8.1",
		Decision:          "unknown",
		Reason:            "release-age and provenance evidence are not available in the first Go safety slice",
		Evidence:          []string{"brew outdated --json=v2"},
		Confidence:        "low",
	}
	metadata := homebrewMetadata{
		Name:     flexStringSlice{"jq"},
		Tap:      "homebrew/core",
		Homepage: "https://jqlang.github.io/jq/",
		Versions: homebrewVersions{Stable: "1.8.1"},
		URLs:     homebrewURLs{Stable: homebrewURL{URL: "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}},
		Autobump: true,
	}
	enriched := applyHomebrewSafetyMetadata(finding, metadata)
	if enriched.Decision != "allow" || enriched.Confidence != "medium" {
		t.Fatalf("expected official formula to be allowed, got %#v", enriched)
	}
	if enriched.Tap != "homebrew/core" || enriched.Version != "1.8.1" || enriched.URL == "" {
		t.Fatalf("expected metadata evidence, got %#v", enriched)
	}
	if !containsString(enriched.Evidence, "formulae.brew.sh metadata") {
		t.Fatalf("expected metadata evidence source, got %#v", enriched.Evidence)
	}
}

func TestApplyHomebrewSafetyMetadataReviewsDeprecatedFormula(t *testing.T) {
	finding := safetyFinding{Provider: "brew", Kind: "brew", Name: "oldtool", Decision: "unknown", Confidence: "low"}
	metadata := homebrewMetadata{
		Name:              flexStringSlice{"oldtool"},
		Tap:               "homebrew/core",
		Deprecated:        true,
		DeprecationReason: "use newtool",
	}
	enriched := applyHomebrewSafetyMetadata(finding, metadata)
	if enriched.Decision != "review" || enriched.Reason != "use newtool" || enriched.Confidence != "medium" {
		t.Fatalf("expected deprecated formula review, got %#v", enriched)
	}
}

func TestEnrichBrewSafetyFindingsKeepsMetadataFailuresAuditable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	findings := []safetyFinding{
		{Provider: "brew", Kind: "brew", Name: "missing", Decision: "unknown", Confidence: "low"},
	}
	enriched := enrichBrewSafetyFindings(context.Background(), server.Client(), server.URL, findings, minHomebrewReleaseAge())
	if len(enriched) != 1 {
		t.Fatalf("expected one finding, got %#v", enriched)
	}
	if enriched[0].Decision != "review" || !strings.Contains(enriched[0].Reason, "metadata unavailable") {
		t.Fatalf("expected metadata failure review, got %#v", enriched[0])
	}
}

func TestCollectBrewSafetyWarnsWhenManifestUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected safety gate ok, got %#v", gate)
	}
	if len(gate.Warnings) != 1 || !strings.Contains(gate.Warnings[0], "manifest unavailable") {
		t.Fatalf("expected manifest warning, got %#v", gate.Warnings)
	}
}

func TestCollectBrewSafetyAllowsOfficialFormulaWithMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
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
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].Confidence != "medium" {
		t.Fatalf("expected formula allow finding, got %#v", gate.Findings)
	}
	if gate.Summary == nil || gate.Summary.Allow != 1 || gate.Summary.Findings != 1 {
		t.Fatalf("expected safety gate summary, got %#v", gate.Summary)
	}
	if gate.Findings[0].Remediation != "" {
		t.Fatalf("expected allowed formula to clear remediation, got %#v", gate.Findings[0])
	}
	if gate.Findings[0].ReleaseDate == "" || gate.Findings[0].MinReleaseAgeDays != 3 {
		t.Fatalf("expected release-age evidence, got %#v", gate.Findings[0])
	}
}

func TestCollectBrewSafetyHoldsTooNewGitHubFormulaRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
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
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected too-new formula hold finding, got %#v", gate.Findings)
	}
	if !strings.Contains(gate.Findings[0].Remediation, "minimum age") {
		t.Fatalf("expected release-age remediation, got %#v", gate.Findings[0])
	}
}

func TestCollectBrewSafetyUsesTOMLConfigMinReleaseAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[security.homebrew]\nmin_release_age_days = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
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
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected TOML min release age to allow fresh release, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].MinReleaseAgeDays != 0 {
		t.Fatalf("expected TOML min release age evidence, got %#v", gate.Findings)
	}
}

func TestCollectBrewSafetyFallsBackToGitHubTagDate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "demo"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
		switch r.URL.Path {
		case "/formula/demo.json":
			_, _ = w.Write([]byte(`{
  "name": "demo",
  "tap": "homebrew/core",
  "homepage": "https://github.com/example/demo",
  "versions": {"stable": "2.0.0"},
  "urls": {"stable": {"url": "https://github.com/example/demo/releases/download/v2.0.0/demo.tar.gz"}}
}`))
		case "/repos/example/demo/releases/tags/v2.0.0":
			http.NotFound(w, r)
		case "/repos/example/demo/git/ref/tags/v2.0.0":
			_, _ = w.Write([]byte(`{"ref":"refs/tags/v2.0.0","object":{"type":"tag","sha":"tag-sha"}}`))
		case "/repos/example/demo/git/tags/tag-sha":
			_, _ = w.Write([]byte(`{"tagger":{"date":"` + time.Now().UTC().Format(time.RFC3339) + `"},"object":{"type":"commit","sha":"commit-sha"}}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"demo","installed_versions":["1.0.0"],"current_version":"2.0.0"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected tag-date release-age hold, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "GitHub tag metadata") {
		t.Fatalf("expected tag metadata evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectBrewSafetyInfersGitHubFormulaReleaseTag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	releasePaths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq"}}
}`))
		case "/repos/jqlang/jq/releases/tags/1.8.1", "/repos/jqlang/jq/releases/tags/v1.8.1",
			"/repos/jqlang/jq/git/ref/tags/1.8.1", "/repos/jqlang/jq/git/ref/tags/v1.8.1":
			releasePaths = append(releasePaths, r.URL.Path)
			http.NotFound(w, r)
		case "/repos/jqlang/jq/releases/tags/jq-1.8.1":
			releasePaths = append(releasePaths, r.URL.Path)
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected inferred release-age hold, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected inferred too-new formula hold finding, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "GitHub inferred release metadata") {
		t.Fatalf("expected inferred release evidence, got %#v", gate.Findings[0].Evidence)
	}
	if !containsString(releasePaths, "/repos/jqlang/jq/releases/tags/jq-1.8.1") {
		t.Fatalf("expected jq-version tag candidate query, got %#v", releasePaths)
	}
}

func TestCollectBrewSafetyInfersGitHubReleaseFromHomepage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "demo"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
		switch r.URL.Path {
		case "/formula/demo.json":
			_, _ = w.Write([]byte(`{
  "name": "demo",
  "tap": "homebrew/core",
  "homepage": "https://github.com/example/demo",
  "versions": {"stable": "2.0.0"},
  "urls": {"stable": {"url": "https://downloads.example.com/demo-2.0.0.tar.gz"}}
}`))
		case "/repos/example/demo/releases/tags/2.0.0", "/repos/example/demo/git/ref/tags/2.0.0":
			http.NotFound(w, r)
		case "/repos/example/demo/releases/tags/v2.0.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[{"name":"demo","installed_versions":["1.0.0"],"current_version":"2.0.0"}],"casks":[]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected homepage inferred release-age hold, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected homepage inferred too-new formula hold finding, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "GitHub inferred release metadata") {
		t.Fatalf("expected inferred release evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectBrewSafetyHoldsCasksEvenWithMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`cask "firefox"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cask/firefox.json" {
			t.Fatalf("unexpected Homebrew API path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "token": "firefox",
  "name": ["Firefox"],
  "tap": "homebrew/cask",
  "homepage": "https://www.mozilla.org/firefox/",
  "url": "https://download-installer.cdn.mozilla.net/pub/firefox/releases/151.0.2/mac/en-US/Firefox%20151.0.2.dmg",
  "version": "151.0.2"
}`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[],"casks":[{"name":"firefox","installed_versions":"150.0","current_version":"151.0.2"}]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "review" || gate.Findings[0].Confidence != "low" {
		t.Fatalf("expected cask review finding, got %#v", gate.Findings)
	}
	remediation := gate.Findings[0].Remediation
	if !strings.Contains(remediation, "mozilla.org") ||
		!strings.Contains(remediation, "download-installer.cdn.mozilla.net") {
		t.Fatalf("expected cask remediation, got %#v", gate.Findings[0])
	}
	if gate.Findings[0].HomepageHost != "mozilla.org" || gate.Findings[0].URLHost != "download-installer.cdn.mozilla.net" || gate.Findings[0].HostMatched {
		t.Fatalf("expected cask provenance hosts, got %#v", gate.Findings[0])
	}
}

func TestCollectBrewSafetyHoldsTooNewGitHubCaskRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`cask "demo-app"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cask/demo-app.json":
			_, _ = w.Write([]byte(`{
  "token": "demo-app",
  "name": ["Demo App"],
  "tap": "homebrew/cask",
  "homepage": "https://example.com/demo-app/",
  "url": "https://github.com/example/demo-app/releases/download/v2.0.0/demo-app.dmg",
  "version": "2.0.0"
}`))
		case "/repos/example/demo-app/releases/tags/v2.0.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"formulae":[],"casks":[{"name":"demo-app","installed_versions":"1.0.0","current_version":"2.0.0"}]}`}}
	gate := collectBrewSafety(context.Background(), fake, root)
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseDate == "" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected too-new cask release hold finding, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "GitHub release metadata") {
		t.Fatalf("expected cask release-age evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestSecurityReviewCandidatesFromReportBuildsPrompts(t *testing.T) {
	report := securityReport{
		Findings: []securityFinding{{
			Provider:    "mise",
			Name:        "npm:pnpm",
			Package:     "pnpm",
			Version:     "11.1.2",
			Ecosystem:   "npm",
			VulnID:      "GHSA-test",
			Decision:    "hold",
			Reason:      "GitHub Advisory vulnerability match",
			Remediation: "upgrade to a fixed version",
			URL:         "https://github.com/advisories/GHSA-test",
		}},
		VSCode: []vscodePosture{{
			Provider:    "brew",
			Kind:        "vscode",
			Name:        "publisher.extension",
			Version:     "1.0.0",
			Decision:    "review",
			Reason:      "publisher domain is not verified",
			Remediation: "verify publisher identity",
		}},
		Scanners: []scannerEvidence{{
			Tool: "osv-scanner",
			Findings: []scannerFinding{{
				Kind:           "vulnerability",
				Ecosystem:      "npm",
				Package:        "left-pad",
				Version:        "1.0.0",
				DependencyKind: "direct",
				VulnID:         "GHSA-scanner",
				SourcePath:     "package-lock.json",
				Decision:       "hold",
				Reason:         "osv-scanner reported vulnerability in a directly managed package",
				Remediation:    "update left-pad",
			}},
		}},
		Audits: []nativeAudit{{
			Provider: "project",
			Tool:     "maven-native-audit",
			Target:   "pom.xml",
			Decision: "review",
			Reason:   "Maven project audit unavailable",
			Error:    "no configured provider-native Maven vulnerability audit",
		}},
		NPM: []npmPosture{{
			Provider: "mise",
			Kind:     "npm",
			Package:  "safe-package",
			Decision: "allow",
		}},
	}
	candidates := securityReviewCandidatesFromReport(report)
	if len(candidates) != 4 {
		t.Fatalf("expected four review candidates, got %#v", candidates)
	}
	if candidates[0].Decision != "hold" || candidates[0].Name != "pnpm" || !strings.Contains(candidates[0].Prompt, "recommend allow/review/hold/block") {
		t.Fatalf("expected advisory review prompt first, got %#v", candidates)
	}
	if !strings.Contains(candidates[0].PolicyCommand, "updev security policy hold --provider npm --name pnpm") || !strings.Contains(candidates[0].PolicyCommand, "--ttl-days 30") {
		t.Fatalf("expected advisory policy command, got %#v", candidates[0])
	}
	var nativeCandidate securityReviewCandidate
	var scannerCandidate securityReviewCandidate
	var vscodeCandidate securityReviewCandidate
	for _, candidate := range candidates {
		switch {
		case candidate.Provider == "project" && candidate.Kind == "native-audit":
			nativeCandidate = candidate
		case candidate.Provider == "scanner" && candidate.Kind == "osv-scanner":
			scannerCandidate = candidate
		case candidate.Provider == "brew" && candidate.Kind == "vscode":
			vscodeCandidate = candidate
		}
	}
	if nativeCandidate.Name != "maven-native-audit" || !strings.Contains(nativeCandidate.Prompt, "pom.xml") || !strings.Contains(nativeCandidate.Prompt, "no configured provider-native") {
		t.Fatalf("expected native audit review prompt with source and remediation, got %#v", nativeCandidate)
	}
	if nativeCandidate.PolicyCommand != "" {
		t.Fatalf("expected native audit not to produce policy command, got %#v", nativeCandidate)
	}
	if scannerCandidate.Name != "GHSA-scanner" || scannerCandidate.Ecosystem != "npm" || scannerCandidate.Package != "left-pad" || scannerCandidate.DependencyKind != "direct" {
		t.Fatalf("expected scanner review candidate to target scanner tool, got %#v", scannerCandidate)
	}
	if !strings.Contains(scannerCandidate.Prompt, "package left-pad") || !strings.Contains(scannerCandidate.Prompt, "dependency role direct") {
		t.Fatalf("expected scanner prompt package context, got %#v", scannerCandidate)
	}
	if !strings.Contains(scannerCandidate.PolicyCommand, "updev security policy hold --provider scanner --kind osv-scanner --name GHSA-scanner") || !strings.Contains(scannerCandidate.PolicyCommand, "--ttl-days 30") {
		t.Fatalf("expected scanner policy command, got %#v", scannerCandidate)
	}
	if vscodeCandidate.Name != "publisher.extension" || !strings.Contains(vscodeCandidate.Prompt, "publisher domain") {
		t.Fatalf("expected VS Code review prompt, got %#v", candidates)
	}
	if !strings.Contains(vscodeCandidate.PolicyCommand, "updev security policy review --provider brew --kind vscode --name publisher.extension") || !strings.Contains(vscodeCandidate.PolicyCommand, "--ttl-days 30") {
		t.Fatalf("expected VS Code policy command, got %#v", vscodeCandidate)
	}
	summary := securityReviewSummaryFromCandidates(candidates)
	if summary == nil || summary.Candidates != 4 || summary.Decisions["hold"] != 2 || summary.Decisions["review"] != 2 || summary.Providers["mise"] != 1 || summary.Providers["brew"] != 1 || summary.Providers["scanner"] != 1 || summary.Providers["project"] != 1 {
		t.Fatalf("expected review candidate summary, got %#v", summary)
	}
	held := filterSecurityReviewCandidates(candidates, securityReviewOptions{decision: "hold"})
	if len(held) != 2 {
		t.Fatalf("expected two held candidates, got %#v", held)
	}
	scannerFiltered := filterSecurityReviewCandidates(candidates, securityReviewOptions{kind: "osv-scanner", name: "GHSA"})
	if len(scannerFiltered) != 1 || scannerFiltered[0].Name != "GHSA-scanner" {
		t.Fatalf("expected scanner candidate name substring match, got %#v", scannerFiltered)
	}
}

func TestSecurityReviewStatusReflectsFilteredCandidates(t *testing.T) {
	scan := securityReport{Status: plan.StatusHeld}
	if got := securityReviewStatus(scan, nil); got != plan.StatusOK {
		t.Fatalf("expected filtered review with no candidates to be ok, got %s", got)
	}
	if got := securityReviewStatus(scan, []securityReviewCandidate{{Name: "candidate"}}); got != plan.StatusHeld {
		t.Fatalf("expected review candidates to hold review status, got %s", got)
	}
	if got := securityReviewStatus(securityReport{Status: plan.StatusError}, nil); got != plan.StatusError {
		t.Fatalf("expected scan errors to remain errors, got %s", got)
	}
}

func TestPrintSecurityReviewTextIncludesCandidates(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityReviewText(&buffer, securityReviewReport{
		Status:  plan.StatusHeld,
		Root:    "/repo",
		Filters: &securityReviewFilters{Decision: "review", Kind: "cask", Name: "demo"},
		Summary: &securityReviewSummary{Candidates: 1, Decisions: map[string]int{"review": 1}, Providers: map[string]int{"brew": 1}},
		Candidates: []securityReviewCandidate{{
			Provider:      "brew",
			Kind:          "cask",
			Name:          "demo-app",
			Decision:      "review",
			Reason:        "needs provenance review",
			Remediation:   "verify upstream provenance",
			Evidence:      []string{"unsigned cask", "non-official tap"},
			Source:        "Brewfile.tmpl",
			URL:           "https://example.com/demo-app",
			Prompt:        "Review updev security candidate brew/cask demo-app.",
			PolicyCommand: "updev security policy review --provider brew --kind cask --name demo-app --reason \"needs provenance review\"",
		}},
	})
	got := buffer.String()
	for _, want := range []string{"filters: decision=review, kind=cask, name=demo", "review candidates: 1", "decisions: review=1", "providers: brew=1", "brew/cask demo-app", "needs provenance review", "remediation: verify upstream provenance", "source: Brewfile.tmpl", "url: https://example.com/demo-app", "evidence: unsigned cask; non-official tap", "policy:", "prompt:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected review text to include %q, got %q", want, got)
		}
	}
}

func TestPrintSecurityTextIncludesSkippedDetails(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Sources: []string{"osv"},
		Skipped: []securitySkipped{{
			Provider: "brew",
			Kind:     "brew",
			Reason:   "homebrew requires curated advisory mapping",
			Count:    2,
			Examples: []string{"jq", "gh"},
		}},
	}, false)
	got := buffer.String()
	if !strings.Contains(got, "skipped automatic matching") || !strings.Contains(got, "brew") || !strings.Contains(got, "2") || !strings.Contains(got, "jq,gh") || !strings.Contains(got, "curated advisory mapping") {
		t.Fatalf("expected skipped details in text output, got %q", got)
	}
}

func TestPrintSafetyTextIncludesWarningsWithErrors(t *testing.T) {
	var buffer bytes.Buffer
	printSafetyTextTo(&buffer, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusError,
		Warnings: []string{"Homebrew manifest unavailable; provenance checks may be incomplete"},
		Error:    "brew outdated failed",
	}})
	got := buffer.String()
	if !strings.Contains(got, "warning: Homebrew manifest unavailable") || !strings.Contains(got, "error: brew outdated failed") {
		t.Fatalf("expected safety warnings and error, got %q", got)
	}
}

func TestPrintSafetyTextIncludesRemediation(t *testing.T) {
	var buffer bytes.Buffer
	printSafetyTextTo(&buffer, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Kind:        "cask",
			Name:        "firefox",
			Decision:    "review",
			Reason:      "needs review",
			Remediation: "review vendor homepage and download host",
		}},
	}})
	got := buffer.String()
	if !strings.Contains(got, "next: review vendor homepage and download host") {
		t.Fatalf("expected remediation in safety text, got %q", got)
	}
}

func TestPrintSafetyTextLocalizesReleaseAgeWarnings(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	var buffer bytes.Buffer
	printSafetyTextTo(&buffer, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Kind:              "brew",
			Name:              "libomp",
			InstalledVersions: []string{"22.1.6"},
			CurrentVersion:    "22.1.7",
			Decision:          "hold",
			Reason:            "candidate release is too new: age 0 days, minimum 3 days",
			Remediation:       "wait until the release reaches the minimum age or allow temporarily by policy after review",
		}},
	}})
	got := buffer.String()
	if !strings.Contains(got, "候補リリースが新しすぎます") || !strings.Contains(got, "リリースが最小経過日数に達するまで") || strings.Contains(got, "candidate release is too new") {
		t.Fatalf("expected localized safety gate text, got %q", got)
	}
}

func TestSafetySummaryTextReportsHeldAndDecisionCounts(t *testing.T) {
	got := safetySummaryText([]safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{
			{Decision: "allow"},
			{Decision: "review"},
			{Decision: "hold"},
			{},
		},
	}})
	want := "1 gates, 1 held gates, 4 findings (1 allow, 1 review, 1 hold, 1 unknown)"
	if got != want {
		t.Fatalf("expected safety summary %q, got %q", want, got)
	}
}

func TestGitHubRepoTagFromURLSupportsArchiveForms(t *testing.T) {
	tests := []struct {
		raw  string
		repo string
		tag  string
	}{
		{raw: "https://github.com/owner/tool/releases/download/v1.0.0/tool.tar.gz", repo: "owner/tool", tag: "v1.0.0"},
		{raw: "https://github.com/owner/tool/archive/refs/tags/v1.0.0.tar.gz", repo: "owner/tool", tag: "v1.0.0"},
		{raw: "https://github.com/owner/tool/archive/v1.0.0.zip", repo: "owner/tool", tag: "v1.0.0"},
	}
	for _, tt := range tests {
		repo, tag, ok := githubRepoTagFromURL(tt.raw)
		if !ok || repo != tt.repo || tag != tt.tag {
			t.Fatalf("unexpected github repo/tag for %s: repo=%q tag=%q ok=%v", tt.raw, repo, tag, ok)
		}
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
	if finding.ReleaseDate == "" || finding.MinReleaseAgeDays != 7 || !containsString(finding.Evidence, "vscode-marketplace update-age") {
		t.Fatalf("expected update-age evidence, got %#v", finding)
	}
}

func TestSecurityFiltersProviderAndEcosystem(t *testing.T) {
	items := []plan.Item{
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
		{Provider: "brew", Name: "jq", Version: "1.8.1", Kind: "brew"},
	}
	filtered := filterSecurityItems(items, securityOptions{provider: "mise"})
	packages, skipped := securityScopeFromItems(filtered)
	packages = filterSecurityPackages(packages, securityOptions{ecosystem: "npm"})
	if len(packages) != 1 || packages[0].Package != "pnpm" {
		t.Fatalf("expected npm package only, got %#v", packages)
	}
	if len(skipped) != 0 {
		t.Fatalf("provider filter should exclude brew skip entries, got %#v", skipped)
	}
	allItems := filterSecurityItems(items, securityOptions{provider: "all"})
	if len(allItems) != len(items) {
		t.Fatalf("provider all should keep all items, got %#v", allItems)
	}
	if !securityScanIncludesBrewProvider("all") || !securityScanIncludesBrewProvider("brew") || securityScanIncludesBrewProvider("") || securityScanIncludesBrewProvider("mise") {
		t.Fatalf("unexpected brew posture provider selection")
	}
	if !securityScanIncludesProjectProvider("") || !securityScanIncludesProjectProvider("all") || !securityScanIncludesProjectProvider("project") || securityScanIncludesProjectProvider("brew") {
		t.Fatalf("unexpected project provider selection")
	}
	npmItems := filterSecurityItemsForEcosystem(items, "npm")
	if len(npmItems) != 1 || npmItems[0].Name != "npm:pnpm" {
		t.Fatalf("expected npm posture items only, got %#v", npmItems)
	}
}
