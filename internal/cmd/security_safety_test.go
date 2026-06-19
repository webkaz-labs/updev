package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/nativeaudit"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/registryaudit"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/updatereason"
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

func TestCollectUpdateSafetyTasksPreservesTaskOrder(t *testing.T) {
	gates := collectUpdateSafetyTasks([]updateSafetyTask{
		{provider: "first", collect: func() safetyGate {
			time.Sleep(5 * time.Millisecond)
			return safetyGate{}
		}},
		{provider: "second", collect: func() safetyGate {
			return safetyGate{Provider: "explicit-second"}
		}},
	})
	if len(gates) != 2 {
		t.Fatalf("expected two gates, got %#v", gates)
	}
	if gates[0].Provider != "first" || gates[1].Provider != "explicit-second" {
		t.Fatalf("unexpected gate order/providers: %#v", gates)
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
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "HOMEBREW_NO_AUTO_UPDATE=1") || !containsString(fake.calls[0], "HOMEBREW_NO_INSTALL_FROM_API=1") || !containsString(fake.calls[0], "brew") || !containsString(fake.calls[0], "--greedy") {
		t.Fatalf("expected brew command, got %+v", fake.calls)
	}
}

func TestRunBrewOutdatedAlwaysUsesLocalTapMetadata(t *testing.T) {
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {Stdout: `{"formulae":[],"casks":[]}`},
	}}
	result := runBrewOutdatedJSON(context.Background(), fake)
	if result.Stdout == "" || len(fake.calls) != 1 {
		t.Fatalf("expected no-install API command, result=%#v calls=%#v", result, fake.calls)
	}
	if !containsString(fake.calls[0], "HOMEBREW_NO_INSTALL_FROM_API=1") {
		t.Fatalf("expected no-install API env, calls=%#v", fake.calls)
	}
}

func TestBrewUpdateSafetyUsesGreedyOutdatedCandidates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`cask "wezterm@nightly"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {
			Stdout: `{"formulae":[],"casks":[{"name":"wezterm@nightly","installed_versions":"latest","current_version":"latest"}]}`,
		},
	}}
	gate := collectBrewUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if len(fake.calls) < 1 || !containsString(fake.calls[0], "--greedy") {
		t.Fatalf("expected greedy brew outdated probe, calls=%#v", fake.calls)
	}
	if gate.Status != plan.StatusHeld || len(gate.Findings) != 1 || gate.Findings[0].Kind != "cask" || gate.Findings[0].Name != "wezterm@nightly" {
		t.Fatalf("expected greedy cask candidate to be gated, got %#v", gate)
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

func TestBuildSecurityGateReportReportsMiseMinimumReleaseAgeWithoutCandidates(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
	}}
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "mise"}, fake)
	if report.Status != plan.StatusOK || len(report.Gates) != 1 || report.Gates[0].Provider != "mise" {
		t.Fatalf("expected ok mise gate, got %#v", report)
	}
	if len(report.Gates[0].Findings) != 0 {
		t.Fatalf("expected no pending mise findings, got %#v", report.Gates[0].Findings)
	}
	if !containsSubstring(report.Gates[0].Evidence, "mise minimum_release_age active: 3d from /fake/mise/config.toml") {
		t.Fatalf("expected minimum_release_age evidence without candidates, got %#v", report.Gates[0].Evidence)
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
	if len(fake.calls) != 1 {
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
	t.Setenv("UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS", "45")
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
	if recording.calls != 1 {
		t.Fatalf("expected second call to use cached brew outdated success, calls=%d", recording.calls)
	}
	if !recording.sawDeadline {
		t.Fatal("expected brew outdated command to run with a deadline")
	}
	if got := brewOutdatedTimeout(); got != 45*time.Second {
		t.Fatalf("expected configurable brew outdated timeout, got %s", got)
	}
}

func TestBrewOutdatedTimeoutFallsBackForNonPositiveEnv(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
	t.Setenv("UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS", "0")
	if got := brewOutdatedTimeout(); got != 60*time.Second {
		t.Fatalf("expected zero timeout to fall back to default, got %s", got)
	}
	t.Setenv("UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS", "-1")
	if got := brewOutdatedTimeout(); got != 60*time.Second {
		t.Fatalf("expected negative timeout to fall back to default, got %s", got)
	}
}

func TestMiseCommandsInjectGitHubTokenWithoutLeakingCommand(t *testing.T) {
	t.Setenv("UPDEV_GITHUB_TOKEN", "updev-test-token")
	root := t.TempDir()
	recording := &envRecordingRunner{fakeCommandRunner: fakeCommandRunner{result: runner.Result{Stdout: `{}`}}}
	_ = runMiseOutdatedJSON(context.Background(), recording, root)
	if len(recording.envCalls) != 1 || !containsString(recording.envCalls[0], "MISE_GITHUB_TOKEN=updev-test-token") {
		t.Fatalf("expected mise outdated to receive MISE_GITHUB_TOKEN env, got %#v", recording.envCalls)
	}
	if strings.Contains(strings.Join(recording.calls[0], " "), "updev-test-token") {
		t.Fatalf("token leaked into recorded command: %#v", recording.calls[0])
	}

	recording = &envRecordingRunner{fakeCommandRunner: fakeCommandRunner{result: runner.Result{Stdout: "All tools are up to date"}}}
	step := updateSteps()[1]
	_ = runUpdateStepWithHold(context.Background(), recording, step, false, "")
	if len(recording.envCalls) != 2 || !containsString(recording.envCalls[0], "MISE_GITHUB_TOKEN=updev-test-token") || !containsString(recording.envCalls[1], "MISE_GITHUB_TOKEN=updev-test-token") {
		t.Fatalf("expected mise upgrade step to receive MISE_GITHUB_TOKEN env, got %#v", recording.envCalls)
	}
	if strings.Contains(strings.Join(recording.calls[0], " "), "updev-test-token") {
		t.Fatalf("token leaked into recorded update command: %#v", recording.calls[0])
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

[security.mise]
min_release_age_days = 4

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
	if got := minMiseReleaseAgeWithConfig(config); got != 4*24*time.Hour {
		t.Fatalf("expected mise release age from config, got %s", got)
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

[update.mise_bump]
mode = "safe"
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
	if opts.miseBumpMode != "safe" {
		t.Fatalf("expected mise bump mode from config, got %+v", opts)
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
	t.Setenv("UPDEV_MISE_BUMP_MODE", "auto")
	opts, err = parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.miseBumpMode != "auto" {
		t.Fatalf("expected env mise bump override, got %+v", opts)
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
			Provider:          "project",
			Tool:              "maven-native-audit",
			Target:            "pom.xml",
			Decision:          "review",
			Reason:            "Maven project audit unavailable",
			UnavailableReason: nativeaudit.FailureUnsupportedTarget,
			Error:             "no configured provider-native Maven vulnerability audit",
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
	if nativeCandidate.Name != "maven-native-audit" || !strings.Contains(nativeCandidate.Prompt, "pom.xml") || !strings.Contains(nativeCandidate.Prompt, "no configured provider-native") || !strings.Contains(nativeCandidate.Prompt, "issue:unsupported-target") {
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
			ReasonCode:        securityreason.CandidateReleaseTooNew,
			ReasonArgs:        map[string]string{"age_days": "0", "min_age_days": "3"},
			Remediation:       "wait until the release reaches the minimum age or allow temporarily by policy after review",
		}},
	}})
	got := buffer.String()
	if !strings.Contains(got, "候補リリースが新しすぎます") || !strings.Contains(got, "リリースが最小経過日数に達するまで") || strings.Contains(got, "candidate release is too new") {
		t.Fatalf("expected localized safety gate text, got %q", got)
	}
}

func TestLocalizedSafetyFindingReasonFallsBackFromProse(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	got := localizedSafetyFindingReason(safetyFinding{Reason: "candidate release is too new: age 1 days, minimum 3 days"})
	if !strings.Contains(got, "経過 1日") || strings.Contains(got, "candidate release") {
		t.Fatalf("expected inferred localized safety reason, got %q", got)
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
	want := "1 provider gates, 1 held providers, 4 findings (1 allow, 1 review, 1 hold, 1 unknown)"
	if got != want {
		t.Fatalf("expected safety summary %q, got %q", want, got)
	}
}

func TestJapaneseSummaryTextUsesDistinctCountLabels(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	updateGot := updateStepSummaryText([]updateStep{
		{Name: "brew", Status: plan.StatusHeld, Skipped: true, SkippedItems: []string{"security held"}},
		{Name: "mise", Status: plan.StatusOK, Updated: []string{"node 22 -> 24"}},
	})
	updateWant := "provider step 2件, 更新項目 1件, 見送り項目 1件, 保留step 1件, skip step 1件"
	if updateGot != updateWant {
		t.Fatalf("expected Japanese update summary %q, got %q", updateWant, updateGot)
	}

	safetyGot := safetySummaryText([]safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{
			{Decision: "allow"},
			{Decision: "hold"},
		},
	}})
	safetyWant := "provider確認 1件, 保留provider 1件, 検出項目 2件 (allow 1件, hold 1件)"
	if safetyGot != safetyWant {
		t.Fatalf("expected Japanese safety summary %q, got %q", safetyWant, safetyGot)
	}
}

func TestLocalizedUpdateStepReasonCoversStrictRefreshAndMiseBumpDrift(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	cases := []struct {
		reason string
		want   string
	}{
		{
			reason: "strict safety refreshed Homebrew metadata; no package candidates found",
			want:   "strict safety のため Homebrew metadata を更新しました。更新対象の package 候補はありません",
		},
		{
			reason: "strict safety refreshed Homebrew metadata before rechecking package candidates",
			want:   "strict safety のため Homebrew metadata を更新し、package 候補を再確認しました",
		},
		{
			reason: "strict safety refreshes Homebrew metadata only before rechecking package candidates",
			want:   "strict safety のため Homebrew metadata の更新だけを実行し、package 候補を再確認します",
		},
		{
			reason: "security=strict held mise update because no scoped safe candidates were found",
			want:   "security=strict のため mise 更新を保留しました: 適用できる scoped safe 候補がありません",
		},
		{
			reason: "strict safety will apply 2 safe mise candidates and hold 3 unsafe candidates",
			want:   "strict safety は mise の safe 候補 2件だけを適用し、unsafe 候補 3件を保留します",
		},
		{
			reason: "strict safety will apply 1 safe Homebrew candidates and hold 4 unsafe candidates; Homebrew cannot generally install an older intermediate release",
			want:   "strict safety は Homebrew の safe 候補 1件だけを適用し、unsafe 候補 4件を保留します。Homebrew は通常、古い中間 version を指定して install できません",
		},
		{
			reason: "mise bump candidates available; mode=manual requires item review",
			want:   "mise bump 候補があります。mode=manual のため item ごとの確認が必要です",
		},
		{
			reason: "mise bump candidates require review",
			want:   "mise bump 候補の確認が必要です",
		},
		{
			reason: "mise bump candidates require review; no safe auto candidates",
			want:   "mise bump 候補の確認が必要です。自動適用できる safe 候補はありません",
		},
		{
			reason: "mise bump candidates available; 2 safe candidates can be applied after confirmation",
			want:   "mise bump 候補があります。確認後に safe 候補 2件を適用できます",
		},
		{
			reason: "mise bump auto would apply 2 safe candidates",
			want:   "mise bump auto は safe 候補 2件を適用します",
		},
		{
			reason: "mise bump auto would apply 2 safe candidates; 3 candidates require review",
			want:   "mise bump auto は safe 候補 2件を適用し、3件は確認待ちにします",
		},
		{
			reason: "mise bump candidate set changed before apply: planned candidate github:ogulcancelik/herdr is no longer reported by mise outdated --bump",
			want:   "mise bump の候補が適用直前に変わったため保留しました: 予定していた候補 github:ogulcancelik/herdr は現在の mise outdated --bump に出ていません",
		},
		{
			reason: "mise bump candidate set changed before preview: planned candidate go changed from 1.26.3 to 1.26.4",
			want:   "mise bump の候補が preview 直前に変わりました: 予定していた候補 go は 1.26.3 から 1.26.4 に変わりました",
		},
		{
			reason: "mise bump auto found only dependency-blocked candidates",
			want:   "mise bump auto で見つかった候補は dependency 不足で block されたものだけです",
		},
		{
			reason: "mise bump dry-run preflight failed: dependency missing",
			want:   "mise bump の dry-run preflight が失敗しました: dependency missing",
		},
		{
			reason: "mise bump failed: install failed",
			want:   "mise bump が失敗しました: install failed",
		},
		{
			reason: "mise bump applied 2 safe candidates; 3 candidates require review",
			want:   "mise bump は safe 候補 2件を適用し、3件は確認待ちです",
		},
		{
			reason: "security policy reran scoped mise-bump update for mise-bump/tool github:ogulcancelik/herdr",
			want:   "security policy に従い、mise-bump の scoped update を再実行しました: mise-bump/tool github:ogulcancelik/herdr",
		},
	}
	for _, tt := range cases {
		if got := localizedUpdateStepReason(tt.reason); got != tt.want {
			t.Fatalf("expected localized reason %q, got %q", tt.want, got)
		}
	}
}

func TestLocalizedUpdateStepReasonForStepUsesReasonCode(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	step := updateStep{}
	setUpdateStepReason(&step, updatereason.MiseBumpCandidateChangedApplyReason("planned candidate go changed from 1.26.3 to 1.26.4"))
	got := localizedUpdateStepReasonForStep(step)
	want := "mise bump の候補が適用直前に変わったため保留しました: 予定していた候補 go は 1.26.3 から 1.26.4 に変わりました"
	if got != want {
		t.Fatalf("expected localized reason %q, got %q", want, got)
	}
}

func TestSetUpdateStepReasonOverwritesStaleCode(t *testing.T) {
	step := updateStep{}
	setUpdateStepReason(&step, updatereason.StrictBrewRefreshDoneReason())
	setUpdateStepReason(&step, updatereason.StrictBrewRefreshFailedReason("api unavailable"))
	if step.ReasonCode != updatereason.StrictBrewRefreshFailed || step.ReasonArgs["error"] != "api unavailable" {
		t.Fatalf("expected refresh failure reason to replace stale code, got %#v", step)
	}
}

func TestLocalizedUpdateStepReasonForStepUsesRefreshFailureCode(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	step := updateStep{}
	setUpdateStepReason(&step, updatereason.StrictBrewRefreshDoneReason())
	setUpdateStepReason(&step, updatereason.StrictBrewRefreshFailedReason("brew update failed"))
	got := localizedUpdateStepReasonForStep(step)
	want := "Homebrew metadata 更新後の safety gate が失敗しました: brew update failed"
	if got != want {
		t.Fatalf("expected localized reason %q, got %q", want, got)
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
		repo, tag, ok := githubrepo.RepoTagFromURL(tt.raw)
		if !ok || repo != tt.repo || tag != tt.tag {
			t.Fatalf("unexpected github repo/tag for %s: repo=%q tag=%q ok=%v", tt.raw, repo, tag, ok)
		}
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

func TestBrewOutdatedCachedErrorRejectsProviderLogNoise(t *testing.T) {
	if brewOutdatedCachedErrorIsReusable("==> Auto-updating Homebrew... Adjust how often this is run with $HOMEBREW_AUTO_UPDATE_SECS.") {
		t.Fatal("expected Homebrew auto-update log cache to be ignored")
	}
	if brewOutdatedCachedErrorIsReusable("==> Tapping homebrew/core\nCloning into '/usr/local/Homebrew/Library/Taps/homebrew/homebrew-core'...\nUpdating files: 100%") {
		t.Fatal("expected Homebrew tap clone log cache to be ignored")
	}
	if !brewOutdatedCachedErrorIsReusable("brew outdated --json=v2 --greedy timed out after 15s") {
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
	if findings[1].TrustTarget != "muxy-app/tap/muxy" || findings[1].TrustCommand != "brew trust --cask muxy-app/tap/muxy" {
		t.Fatalf("expected item-scoped Homebrew trust metadata, got %#v", findings[1])
	}
	if got := strings.Join(findings[1].TrustCommandArgv, "\x00"); got != "brew\x00trust\x00--cask\x00muxy-app/tap/muxy" {
		t.Fatalf("expected structured Homebrew trust argv, got %#v", findings[1].TrustCommandArgv)
	}
	if !strings.Contains(findings[1].Remediation, "brew trust --cask muxy-app/tap/muxy") {
		t.Fatalf("expected tap trust remediation, got %#v", findings[1])
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
		Evidence:          []string{"brew outdated --json=v2 --greedy"},
		Confidence:        "low",
	}
	metadata := homebrewMetadata{
		Name:     []string{"jq"},
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
	if enriched.ReasonCode != securityreason.HomebrewOfficialFormula || enriched.ReasonArgs["name"] != "jq" {
		t.Fatalf("expected structured official formula reason, got %#v", enriched)
	}
}

func TestApplyHomebrewSafetyMetadataReviewsDeprecatedFormula(t *testing.T) {
	finding := safetyFinding{Provider: "brew", Kind: "brew", Name: "oldtool", Decision: "unknown", Confidence: "low"}
	metadata := homebrewMetadata{
		Name:              []string{"oldtool"},
		Tap:               "homebrew/core",
		Deprecated:        true,
		DeprecationReason: "use newtool",
	}
	enriched := applyHomebrewSafetyMetadata(finding, metadata)
	if enriched.Decision != "review" || enriched.Reason != "use newtool" || enriched.Confidence != "medium" {
		t.Fatalf("expected deprecated formula review, got %#v", enriched)
	}
	if enriched.ReasonCode != securityreason.HomebrewEntryDeprecated || enriched.ReasonArgs["reason_text"] != "use newtool" {
		t.Fatalf("expected structured deprecated formula reason, got %#v", enriched)
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
	if enriched[0].ReasonCode != securityreason.HomebrewMetadataUnavailable || enriched[0].ReasonArgs["error"] == "" {
		t.Fatalf("expected structured metadata failure reason, got %#v", enriched[0])
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
	if gate.Findings[0].ReasonCode != securityreason.CandidateReleaseTooNew || gate.Findings[0].ReasonArgs["age_days"] != "0" {
		t.Fatalf("expected structured release-age reason, got %#v", gate.Findings[0])
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

func TestCollectMiseSafetyAllowsOldGitHubCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected mise safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].ReleaseDate == "" {
		t.Fatalf("expected GitHub mise allow finding with release date, got %#v", gate.Findings)
	}
	if gate.Findings[0].RepositoryURL != "https://github.com/openai/codex" {
		t.Fatalf("expected GitHub repository evidence, got %#v", gate.Findings[0])
	}
}

func TestCollectMiseSafetyReportsNativeMinimumReleaseAgeEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected mise safety gate ok, got %#v", gate)
	}
	if !containsString(gate.Evidence, "mise minimum_release_age active: 3d from ~/.config/mise/config.toml") {
		t.Fatalf("expected native mise minimum_release_age evidence, got %#v", gate.Evidence)
	}
}

func TestCollectMiseSafetyReportsNativeHeldCandidateFromBatchComparison(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected mise safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].Source != miseNativeReleaseAgeSource {
		t.Fatalf("expected native release-age hold finding, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d") {
		t.Fatalf("expected age-disabled evidence, got %#v", gate.Findings[0].Evidence)
	}
	if gate.Findings[0].ReleaseDate == "" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected release-age enrichment on native hold, got %#v", gate.Findings[0])
	}
}

func TestCollectMiseSafetyReportsNativeHeldNewerCandidateFromLatestDiff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.60.1", "/repos/openai/codex/git/ref/tags/0.60.1",
			"/repos/openai/codex/releases/tags/codex-0.60.1", "/repos/openai/codex/git/ref/tags/codex-0.60.1":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.60.1":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.60.1"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected mise safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 2 {
		t.Fatalf("expected normal candidate and native hold finding, got %#v", gate.Findings)
	}
	if gate.Findings[0].Decision != "allow" || gate.Findings[1].Decision != "hold" || gate.Findings[1].CurrentVersion != "0.61.0" {
		t.Fatalf("expected allowed age-gated candidate plus held newer native candidate, got %#v", gate.Findings)
	}
	if !strings.Contains(gate.Findings[1].Reason, "normal age-gated candidate is 0.60.1") {
		t.Fatalf("expected native hold reason to mention normal candidate, got %#v", gate.Findings[1])
	}
}

func TestCollectMiseBumpSafetyAllowsSafePinnedCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "0")
	root := t.TempDir()
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
	}}
	gate := collectMiseBumpSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Provider != miseBumpProvider || gate.Status != plan.StatusOK {
		t.Fatalf("expected ok mise-bump gate, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].Source != miseBumpSource {
		t.Fatalf("expected allowed bump finding, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "mise outdated --json --bump") {
		t.Fatalf("expected bump evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectMiseBumpSafetyReviewsMajorPinnedCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "0")
	root := t.TempDir()
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"1.0.0","latest":"1.0.0"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
	}}
	gate := collectMiseBumpSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected major bump to hold gate, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "review" || !strings.Contains(gate.Findings[0].Reason, "major version") {
		t.Fatalf("expected major bump review finding, got %#v", gate.Findings)
	}
}

func TestCollectMiseBumpSafetyHoldsRateLimitedDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stderr: "mise WARN GitHub rate limit exceeded. Resets at 2026-06-11 20:49:57 +09:00",
	}}
	gate := collectMiseBumpSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld || gate.Error != "" {
		t.Fatalf("expected rate-limited mise-bump discovery to be held without gate error, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "review" || gate.Findings[0].Name != "candidate-discovery" {
		t.Fatalf("expected generic review finding for unavailable bump discovery, got %#v", gate.Findings)
	}
	if !strings.Contains(gate.Findings[0].Reason, "GitHub rate limit exceeded") {
		t.Fatalf("expected rate limit reason in finding, got %#v", gate.Findings[0])
	}
}

func TestParseMiseBumpOutdatedUsesMiseBumpField(t *testing.T) {
	findings, err := parseMiseBumpOutdated(`{
  "node": {"requested":"lts","current":"24.16.0","bump":null,"latest":"26.3.0"},
  "go": {"requested":"1.26","current":"1.26.3","bump":"1.27","latest":"1.27.0"},
  "python": {"requested":"3","current":"3.14.5","bump":"3.15","latest":"3.15.0"}
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected only rows with mise bump values, got %#v", findings)
	}
	if findings[0].Name != "go" || findings[0].CurrentVersion != "1.27" || findings[0].Version != "1.26" {
		t.Fatalf("expected go bump value to be used, got %#v", findings[0])
	}
	if findings[1].Name != "python" || findings[1].CurrentVersion != "3.15" || findings[1].Version != "3" {
		t.Fatalf("expected python bump value to be used, got %#v", findings[1])
	}
}

func TestCollectMiseSafetyHoldsTooNewGitHubCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected mise safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].ReleaseAgeDays != 0 {
		t.Fatalf("expected too-new GitHub mise hold, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "GitHub inferred release metadata") {
		t.Fatalf("expected GitHub release evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectMiseSafetyAllowsOldNPMCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pnpm" {
			t.Fatalf("unexpected npm path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "name": "pnpm",
  "repository": {"type": "git", "url": "git+https://github.com/pnpm/pnpm.git"},
  "maintainers": [{"name": "maintainer"}],
  "dist-tags": {"latest": "11.0.0"},
  "time": {"11.0.0": "2026-05-20T00:00:00.000Z", "modified": "2026-05-20T00:00:00.000Z"},
  "versions": {"11.0.0": {"version": "11.0.0", "repository": {"type": "git", "url": "git+https://github.com/pnpm/pnpm.git"}}}
}`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_NPM_REGISTRY_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"npm:pnpm":{"requested":"10.0.0","current":"10.0.0","latest":"11.0.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected mise npm safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].Kind != "npm" {
		t.Fatalf("expected npm allow finding, got %#v", gate.Findings)
	}
	if gate.Findings[0].RepositoryURL != "https://github.com/pnpm/pnpm" || gate.Findings[0].PublishedDate == "" {
		t.Fatalf("expected npm provenance and publish evidence, got %#v", gate.Findings[0])
	}
}

func TestCollectMiseSafetyAllowsOldCargoCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/crates/broot" {
			t.Fatalf("unexpected crates.io path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "crate": {"id": "broot", "max_version": "2.0.0", "repository": "https://github.com/Canop/broot", "updated_at": "2026-05-20T00:00:00Z", "downloads": 1000},
  "versions": [{"num": "2.0.0", "yanked": false, "created_at": "2026-05-20T00:00:00Z"}]
}`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_CRATES_IO_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"cargo:broot":{"requested":"1.0.0","current":"1.0.0","latest":"2.0.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected mise cargo safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].Kind != "cargo" {
		t.Fatalf("expected cargo allow finding, got %#v", gate.Findings)
	}
}

func TestCollectMiseSafetyHoldsTooNewPipxCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/black/json" {
			t.Fatalf("unexpected PyPI path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
  "info": {"name": "black", "version": "26.1.0", "project_urls": {"Source": "https://github.com/psf/black"}},
  "releases": {"26.1.0": [{"upload_time_iso_8601": "` + time.Now().UTC().Format(time.RFC3339Nano) + `", "yanked": false}]}
}`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_PYPI_API_URL", server.URL)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"pipx:black":{"requested":"25.1.0","current":"25.1.0","latest":"26.1.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected mise pipx safety gate held, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "hold" || gate.Findings[0].Kind != "pipx" {
		t.Fatalf("expected too-new pipx hold finding, got %#v", gate.Findings)
	}
}

func TestCollectMiseSafetyUsesRegistryAquaGitHubBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/astral-sh/uv/releases/tags/0.11.19", "/repos/astral-sh/uv/git/ref/tags/0.11.19":
			http.NotFound(w, r)
		case "/repos/astral-sh/uv/releases/tags/v0.11.19":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"uv":{"requested":"0.11.14","current":"0.11.14","latest":"0.11.19"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "registry", "--json"}, "\x00"): {
			Stdout: `[{"short":"uv","backends":["aqua:astral-sh/uv","pipx:uv"]}]`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected registry-backed mise safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].RepositoryURL != "https://github.com/astral-sh/uv" {
		t.Fatalf("expected uv to be allowed via mise registry aqua GitHub metadata, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "mise registry backend aqua:astral-sh/uv") {
		t.Fatalf("expected mise registry backend evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectMiseSafetyUsesExplicitAquaGitHubBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/modem-dev/hunk/releases/tags/0.14.1", "/repos/modem-dev/hunk/git/ref/tags/0.14.1":
			http.NotFound(w, r)
		case "/repos/modem-dev/hunk/releases/tags/v0.14.1":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"aqua:modem-dev/hunk":{"requested":"0.14.0","current":"0.14.0","latest":"0.14.1"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected explicit aqua-backed mise safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].RepositoryURL != "https://github.com/modem-dev/hunk" {
		t.Fatalf("expected hunk to be allowed via aqua GitHub metadata, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "mise aqua backend modem-dev/hunk") {
		t.Fatalf("expected mise aqua backend evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectMiseSafetyUsesCoreGitHubReleaseTags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/golang/go/releases/tags/go1.26.4":
			_, _ = w.Write([]byte(`{"published_at":"2026-05-20T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"go":{"requested":"1.26.3","current":"1.26.3","latest":"1.26.4"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "registry", "--json"}, "\x00"): {
			Stdout: `[{"short":"go","backends":["core:go"]}]`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected core-backed mise safety gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].RepositoryURL != "https://github.com/golang/go" {
		t.Fatalf("expected go to be allowed via mise core GitHub metadata, got %#v", gate.Findings)
	}
	if !containsString(gate.Findings[0].Evidence, "mise core backend go") {
		t.Fatalf("expected mise core backend evidence, got %#v", gate.Findings[0].Evidence)
	}
}

func TestCollectMiseSafetyUsesVfoxProviderMetadataRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release-notes" {
			t.Fatalf("unexpected vendor release notes path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`<h2 id="57200_2026-05-20">572.0.0 (2026-05-20)</h2>`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_PROVIDER_METADATA_URL_GOOGLE_CLOUD_CLI", server.URL+"/release-notes")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"vfox:gcloud":{"requested":"568.0.0","current":"568.0.0","latest":"572.0.0"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "registry", "--json"}, "\x00"): {
			Stdout: `[{"short":"gcloud","backends":["vfox:mise-plugins/vfox-gcloud","asdf:mise-plugins/mise-gcloud"]}]`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusOK {
		t.Fatalf("expected vfox provider metadata gate ok, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "allow" || gate.Findings[0].ReleaseDate == "" {
		t.Fatalf("expected gcloud to be allowed from provider metadata, got %#v", gate.Findings)
	}
	if gate.Findings[0].URL != server.URL+"/release-notes" || gate.Findings[0].SupportURL == "" {
		t.Fatalf("expected vendor source URLs, got %#v", gate.Findings[0])
	}
	for _, want := range []string{"mise registry backend vfox:mise-plugins/vfox-gcloud", "provider metadata google-cloud-cli", "Google Cloud CLI release notes"} {
		if !containsString(gate.Findings[0].Evidence, want) {
			t.Fatalf("expected evidence %q, got %#v", want, gate.Findings[0].Evidence)
		}
	}
}

func TestCollectMiseSafetyReviewsVfoxWhenProviderMetadataMissingVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<h2>571.0.0 (2026-05-20)</h2>`))
	}))
	defer server.Close()
	t.Setenv("UPDEV_PROVIDER_METADATA_URL_GOOGLE_CLOUD_CLI", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{"minimum_release_age":{"value":"3d","source":"~/.config/mise/config.toml"}}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{"vfox:gcloud":{"requested":"568.0.0","current":"568.0.0","latest":"572.0.0"}}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "registry", "--json"}, "\x00"): {
			Stdout: `[{"short":"gcloud","backends":["vfox:mise-plugins/vfox-gcloud"]}]`,
		},
	}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected missing provider metadata version to hold gate, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "review" || !strings.Contains(gate.Findings[0].Reason, "version 572.0.0 was not found") {
		t.Fatalf("expected provider metadata review finding, got %#v", gate.Findings)
	}
}

func TestCollectMiseSafetyReviewsUnsupportedBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"vfox:gcloud":{"requested":"568.0.0","current":"568.0.0","latest":"572.0.0"}}`}}
	gate := collectMiseUpdateSafetyWithPolicy(context.Background(), fake, root, securityPolicy{})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected unsupported mise backend to hold gate, got %#v", gate)
	}
	if len(gate.Findings) != 1 || gate.Findings[0].Decision != "review" || !strings.Contains(gate.Findings[0].Reason, "unsupported or opaque") {
		t.Fatalf("expected unsupported backend review, got %#v", gate.Findings)
	}
	if gate.Findings[0].ReasonCode != securityreason.MiseOpaqueBackend {
		t.Fatalf("expected structured opaque backend reason, got %#v", gate.Findings[0])
	}
}

func TestMinMiseReleaseAgeEnvOverridesTOMLConfig(t *testing.T) {
	config := updevConfig{}
	days := 5
	config.Security.Mise.MinReleaseAgeDays = &days
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "1")
	if got := minMiseReleaseAgeWithConfig(config); got != 24*time.Hour {
		t.Fatalf("expected env min release age override, got %s", got)
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
	repo, ok := githubrepo.RepoFromMiseName("github:owner/tool@1.2.3")
	if !ok || repo != "owner/tool" {
		t.Fatalf("unexpected repo parse: %q %v", repo, ok)
	}
	if _, ok := githubrepo.RepoFromMiseName("github:owner/tool;rm"); ok {
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
	first, err := registryaudit.FetchNPMMetadata(context.Background(), server.Client(), server.URL, "left-pad")
	if err != nil {
		t.Fatal(err)
	}
	second, err := registryaudit.FetchNPMMetadata(context.Background(), server.Client(), server.URL, "left-pad")
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
	posture := githubrepo.PostureFromRepository("mise", "github:owner/tool", "owner/tool", githubRepository{
		FullName: "owner/tool",
		SecurityAndAnalysis: githubSecurityAndAnalysis{
			DependabotSecurityUpdates: githubSecurityFeature{Status: "disabled"},
			SecretScanning:            githubSecurityFeature{Status: "enabled"},
		},
	}, minHomebrewTapRepositoryAge())
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
	posture := githubrepo.PostureFromRepository("brew", "tap:vendor/tap", "vendor/homebrew-tap", githubRepository{
		FullName:  "vendor/homebrew-tap",
		HTMLURL:   "https://github.com/vendor/homebrew-tap",
		CreatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	}, minHomebrewTapRepositoryAge())
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
	trustArgv := map[string][]string{}
	for _, posture := range postures {
		trustCommands[posture.Name] = posture.TrustCommand
		trustArgv[posture.Name] = posture.TrustCommandArgv
	}
	if trustCommands["vendor/tap/custom-app"] != "brew trust --cask vendor/tap/custom-app" ||
		trustCommands["vendor/tap"] != "brew trust --tap vendor/tap" {
		t.Fatalf("expected Homebrew 6 trust commands on non-official entries, got %#v", postures)
	}
	if strings.Join(trustArgv["vendor/tap/custom-app"], "\x00") != "brew\x00trust\x00--cask\x00vendor/tap/custom-app" ||
		strings.Join(trustArgv["vendor/tap"], "\x00") != "brew\x00trust\x00--tap\x00vendor/tap" {
		t.Fatalf("expected structured Homebrew 6 trust argv on non-official entries, got %#v", postures)
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
	postures, err := githubPosturesFromRegistry(
		context.Background(), server.Client(), server.URL,
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

func TestRunUpdateAutoMiseBumpRunsScopedSafeCandidates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "0")
	root := t.TempDir()
	safeBumpJSON := `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {
			Stdout: `{"formulae":[],"casks":[]}`,
		},
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: safeBumpJSON,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"brew", "update"}, "\x00"): {
			Stdout: "Already up-to-date.",
		},
		strings.Join([]string{"mise", "upgrade"}, "\x00"): {
			Stdout: "All tools are up to date",
		},
		strings.Join([]string{"mise", "prune"}, "\x00"): {Stdout: "mise pruned configuration links"},
		preflightKey: {Stdout: "Would bump github:openai/codex"},
		applyKey:     {Stdout: "github:openai/codex 0.60.0 -> 0.60.1"},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict", miseBumpMode: "auto", noTUI: true}, fake)
	if code != 0 {
		t.Fatalf("expected successful auto bump update, got %d", code)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(preflightKey, "\x00")) {
		t.Fatalf("expected scoped dry-run bump command, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected scoped apply bump command, calls=%#v", fake.calls)
	}
	for _, call := range fake.calls {
		if len(call) == 3 && call[0] == "mise" && call[1] == "upgrade" && call[2] == "--bump" {
			t.Fatalf("unscoped mise bump must not run, calls=%#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	var bumpStep updateStep
	for _, step := range entry.Report.Steps {
		if step.Name == miseBumpProvider {
			bumpStep = step
			break
		}
	}
	if bumpStep.Status != plan.StatusOK || len(bumpStep.Updated) != 1 || !strings.Contains(bumpStep.Updated[0], "github:openai/codex") {
		t.Fatalf("expected successful bump step in report, got %#v", bumpStep)
	}
	if !updateStepHasCommands(bumpStep, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"},
		{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"},
	}) {
		t.Fatalf("expected bump report to include preflight and apply commands, got %#v", bumpStep.Commands)
	}
}

func TestRunMiseBumpAutoDryRunShowsWouldUpdateCandidates(t *testing.T) {
	step, ok := runMiseBumpUpdateStep(context.Background(), &fakeCommandRunner{}, updateOptions{root: "/repo", security: "strict", miseBumpMode: "auto", dryRun: true}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:          "mise",
			Kind:              "tool",
			Name:              "github:openai/codex",
			InstalledVersions: []string{"0.60.0"},
			CurrentVersion:    "0.60.1",
			Decision:          "allow",
			Source:            miseBumpSource,
		}},
	}}, false)
	if !ok || len(step.Updated) != 1 || len(step.SkippedItems) != 0 || !strings.HasPrefix(step.Updated[0], "would bump ") {
		t.Fatalf("expected dry-run auto bump to expose would-update row, ok=%v step=%#v", ok, step)
	}
	rows := updateOutcomeRows(updateReport{DryRun: true, Steps: []updateStep{step}}, 10, false)
	if len(rows) != 1 || rows[0][0] != "would" || !strings.Contains(rows[0][2], "github:openai/codex") {
		t.Fatalf("expected dry-run outcome to render as would-update, got %#v", rows)
	}
}

func TestRunMiseBumpOffModeDoesNotCreateUpdateStep(t *testing.T) {
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "off"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:          "mise",
			Kind:              "tool",
			Name:              "github:openai/codex",
			InstalledVersions: []string{"0.60.0"},
			CurrentVersion:    "0.60.1",
			Decision:          "allow",
			Source:            miseBumpSource,
		}},
	}}, false)
	if ok || step.Name != "" || len(fake.calls) != 0 {
		t.Fatalf("expected off mode to skip mise-bump step without command calls, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
}

func TestRunMiseBumpManualModeKeepsAllCandidatesReviewOnly(t *testing.T) {
	safe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	unsafe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "npm:@google/gemini-cli",
		InstalledVersions: []string{"0.42.0"},
		CurrentVersion:    "0.46.0",
		Decision:          "review",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "manual"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{safe, unsafe},
	}}, false)
	if !ok || step.Status != plan.StatusDrift || !step.Skipped || len(step.Updated) != 0 || len(step.SkippedItems) != 2 || len(fake.calls) != 0 {
		t.Fatalf("expected manual mode to expose all candidates without applying, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	if !strings.Contains(strings.Join(step.SkippedItems, "\n"), "github:openai/codex") || !strings.Contains(strings.Join(step.SkippedItems, "\n"), "npm:@google/gemini-cli") {
		t.Fatalf("expected manual mode skipped items to include safe and review candidates, got %#v", step.SkippedItems)
	}
}

func TestRunMiseBumpSafeModeKeepsSafeCandidatesConfirmationOnly(t *testing.T) {
	safe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "safe"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{safe},
	}}, false)
	if !ok || step.Status != plan.StatusDrift || !step.Skipped || len(step.Updated) != 0 || len(step.SkippedItems) != 1 || len(fake.calls) != 0 {
		t.Fatalf("expected safe mode to expose confirmation-only batch without applying, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	if !strings.Contains(step.Reason, "can be applied after confirmation") {
		t.Fatalf("expected safe mode reason to describe confirmation boundary, got %q", step.Reason)
	}
}

func TestRunMiseBumpAutoSkipsDependencyBlockedCandidate(t *testing.T) {
	root := t.TempDir()
	codex := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	broot := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "cargo:broot",
		InstalledVersions: []string{"1.56.0"},
		CurrentVersion:    "1.57.0",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	firstPreflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot", "github:openai/codex"}, "\x00")
	secondPreflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{
		results: map[string]runner.Result{
			validateKey:        {Stdout: `{"cargo:broot":{"requested":"1.56.0","current":"1.56.0","bump":"1.57.0","latest":"1.57.0"},"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`},
			firstPreflightKey:  {Stderr: "mise WARN tool 'cargo:broot@1.57.0': depends on 'rust' which is not in the current install set"},
			secondPreflightKey: {Stdout: "Would bump github:openai/codex"},
			applyKey:           {Stdout: "github:openai/codex 0.60.0 -> 0.60.1"},
		},
	}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{codex, broot},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:openai/codex") || len(step.SkippedItems) != 1 || !strings.Contains(step.SkippedItems[0], "cargo:broot") {
		t.Fatalf("expected dependency-blocked candidate to be skipped while remaining safe candidate applies, ok=%v step=%#v", ok, step)
	}
	if fakeCommandWasCalled(fake.calls, strings.Split(strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "cargo:broot", "github:openai/codex"}, "\x00"), "\x00")) {
		t.Fatalf("dependency-blocked candidate must not be passed to apply command, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected remaining safe candidate to be applied, calls=%#v", fake.calls)
	}
	if !updateStepHasCommands(step, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot", "github:openai/codex"},
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"},
		{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"},
	}) {
		t.Fatalf("expected dependency-blocked bump report to include both preflights and apply command, got %#v", step.Commands)
	}
}

func TestRunMiseBumpAutoHoldsWhenAllCandidatesAreDependencyBlocked(t *testing.T) {
	root := t.TempDir()
	broot := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "cargo:broot",
		InstalledVersions: []string{"1.56.0"},
		CurrentVersion:    "1.57.0",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot"}, "\x00")
	fake := &fakeCommandRunner{
		results: map[string]runner.Result{
			validateKey:  {Stdout: `{"cargo:broot":{"requested":"1.56.0","current":"1.56.0","bump":"1.57.0","latest":"1.57.0"}}`},
			preflightKey: {Stderr: "mise WARN tool 'cargo:broot@1.57.0': depends on 'rust' which is not in the current install set"},
		},
	}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{broot},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || !step.Skipped || !strings.Contains(step.Reason, "dependency-blocked") {
		t.Fatalf("expected dependency-blocked only candidates to hold, ok=%v step=%#v", ok, step)
	}
	if !updateStepHasCommands(step, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot"},
	}) {
		t.Fatalf("expected only the executed preflight command in report, got %#v", step.Commands)
	}
}

func TestSanitizedNPMUserConfigForMiseBumpKeepsRegistryAndDropsReleaseAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "npm")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("registry=https://registry.npmjs.org/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "npmrc"), []byte("@webkaz-labs:registry=https://npm.pkg.github.com\n//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\nmin-release-age=3\nminimum_release_age=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := sanitizedNPMUserConfigContentForMiseBump()
	for _, want := range []string{
		"registry=https://registry.npmjs.org/",
		"@webkaz-labs:registry=https://npm.pkg.github.com",
		"//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected sanitized npm config to keep %q, got:\n%s", want, content)
		}
	}
	if strings.Contains(content, "min-release-age") || strings.Contains(content, "minimum_release_age") {
		t.Fatalf("expected sanitized npm config to drop release-age settings, got:\n%s", content)
	}
}

func TestNPMUserConfigCandidatePathsPreferExplicitConfig(t *testing.T) {
	home := t.TempDir()
	explicit := filepath.Join(home, "custom-npmrc")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("NPM_CONFIG_USERCONFIG", explicit)

	got := npmUserConfigCandidatePaths()
	if len(got) != 1 || got[0] != explicit {
		t.Fatalf("expected explicit npm userconfig to be the only candidate, got %#v", got)
	}
}

func TestNPMUserConfigCandidatePathsUseHomeAndXDGConfig(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, ".config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("NPM_CONFIG_USERCONFIG", "")
	t.Setenv("npm_config_userconfig", "")

	want := []string{
		filepath.Join(home, ".npmrc"),
		filepath.Join(configHome, "npm", "npmrc"),
	}
	got := npmUserConfigCandidatePaths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected npm config candidates %#v, got %#v", want, got)
	}
}

func TestRunMiseBumpAutoWrapsNPMBackendWithSanitizedUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "npm")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "npmrc"), []byte("registry=https://registry.npmjs.org/\nmin-release-age=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "npm",
		Name:              "npm:agent-browser",
		InstalledVersions: []string{"0.27.0"},
		CurrentVersion:    "0.27.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey: {Stdout: `{"npm:agent-browser":{"requested":"0.27.0","current":"0.27.0","bump":"0.27.1","latest":"0.27.1"}}`},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusOK || len(step.Updated) != 1 {
		t.Fatalf("expected npm bump to apply with sanitized npm config, ok=%v step=%#v", ok, step)
	}
	envCalls := [][]string{}
	for _, call := range fake.calls {
		if len(call) > 2 && call[0] == "env" && npmUserConfigAssignmentFromCommand(call) != "" {
			envCalls = append(envCalls, call)
		}
	}
	if len(envCalls) != 2 {
		t.Fatalf("expected preflight and apply to use sanitized npm userconfig, calls=%#v", fake.calls)
	}
	for _, call := range envCalls {
		if !containsString(call, "mise") || !containsString(call, "npm:agent-browser") {
			t.Fatalf("expected sanitized npm env call to wrap scoped mise bump, got %#v", call)
		}
		if !containsString(call, "-u") || !containsString(call, "NPM_CONFIG_MIN_RELEASE_AGE") || !containsString(call, "npm_config_min_release_age") {
			t.Fatalf("expected sanitized npm env call to unset release-age env vars, got %#v", call)
		}
		path := strings.TrimPrefix(npmUserConfigAssignmentFromCommand(call), "NPM_CONFIG_USERCONFIG=")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected temporary npm config to be cleaned up, path=%q err=%v", path, err)
		}
	}
}

func npmUserConfigAssignmentFromCommand(command []string) string {
	for _, arg := range command {
		if strings.HasPrefix(arg, "NPM_CONFIG_USERCONFIG=") {
			return arg
		}
	}
	return ""
}

func TestRunMiseBumpAutoKeepsPartialUpdatesOnApplyError(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey:  {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`},
		preflightKey: {Stdout: "Would bump github:openai/codex"},
		applyKey:     {Stdout: "github:openai/codex 0.60.0 -> 0.60.1", Stderr: "mise failed after partial update", Code: 1},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusError || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:openai/codex") {
		t.Fatalf("expected partial update evidence to survive apply error, ok=%v step=%#v", ok, step)
	}
}

func TestRunMiseBumpAutoHoldsWhenCandidateChangesBeforeApply(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.2","latest":"0.60.2"}}`,
		},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || !strings.Contains(step.Reason, "candidate set changed") {
		t.Fatalf("expected changed candidate set to hold auto bump, ok=%v step=%#v", ok, step)
	}
	if step.ReasonCode != updatereason.MiseBumpCandidateChangedApply || !strings.Contains(step.ReasonArgs["detail"], "github:openai/codex") {
		t.Fatalf("expected structured candidate-change reason, got %#v", step)
	}
	for _, call := range fake.calls {
		if len(call) >= 2 && call[0] == "mise" && call[1] == "upgrade" {
			t.Fatalf("changed candidate set must not execute upgrade, calls=%#v", fake.calls)
		}
	}
}

func TestRunMiseBumpAutoAppliesPolicyAllowedNativeAgeHold(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise-bump",
		Kind:              "tool",
		Name:              "github:ogulcancelik/herdr",
		InstalledVersions: []string{"0.6.8"},
		CurrentVersion:    "0.6.9",
		Decision:          "allow",
		Reason:            "reviewed locally",
		Confidence:        "policy",
		Evidence:          []string{"mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d", "security-policy"},
		Source:            miseNativeReleaseAgeSource,
	}
	validateKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:ogulcancelik/herdr"}, "\x00")
	applyKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--bump", "--yes", "--cd", root, "github:ogulcancelik/herdr"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey:  {Stdout: `{"github:ogulcancelik/herdr":{"requested":"0.6.8","current":"0.6.8","bump":"0.6.9","latest":"0.6.9"}}`},
		preflightKey: {Stdout: "Would bump github:ogulcancelik/herdr"},
		applyKey:     {Stdout: "github:ogulcancelik/herdr 0.6.8 -> 0.6.9"},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusOK || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:ogulcancelik/herdr") {
		t.Fatalf("expected policy-allowed native age hold to apply, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	normalValidateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	if fakeCommandWasCalled(fake.calls, strings.Split(normalValidateKey, "\x00")) {
		t.Fatalf("policy-allowed age hold must validate against age-disabled candidates, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected scoped age-disabled apply command, calls=%#v", fake.calls)
	}
	if !updateStepHasCommands(step, [][]string{
		{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:ogulcancelik/herdr"},
		{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--bump", "--yes", "--cd", root, "github:ogulcancelik/herdr"},
	}) {
		t.Fatalf("expected policy-allowed bump report to include age-disabled preflight/apply commands, got %#v", step.Commands)
	}
}

func TestMiseBumpSafeModeExposesBatchAction(t *testing.T) {
	t.Setenv("UPDEV_MISE_BUMP_MODE", "safe")
	actions := updateStepDetailActions(updateStep{Name: miseBumpProvider, Status: plan.StatusDrift})
	found := false
	for _, action := range actions {
		parsedAction, _, ok := parseMiseBumpDetailAction(action.Value)
		if ok && parsedAction == "apply-batch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected safe mode batch action, got %#v", actions)
	}
}
