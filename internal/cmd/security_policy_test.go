package cmd

import (
	"bytes"
	"context"
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

func TestParseSecurityOptionsAcceptsPolicyPath(t *testing.T) {
	opts, err := parseSecurityOptions([]string{"--provider", "mise", "--ecosystem", "npm", "--policy", "/tmp/policy.json", "--scanner", "none"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.policy != "/tmp/policy.json" || opts.provider != "mise" || opts.ecosystem != "npm" || opts.scanner != "none" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityOptions([]string{"--scanner", "unknown"}); err == nil {
		t.Fatal("expected unsupported scanner error")
	}
	if _, err := parseSecurityOptions([]string{"--scanner", "auto,gitleaks"}); err == nil {
		t.Fatal("expected combined auto scanner error")
	}
}

func TestParseSecurityPolicyOptionsAdd(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"add", "--path", "/tmp/policy.json", "--provider", "brew", "--kind", "cask", "--name", "firefox", "--decision", "allow", "--reason", "trusted vendor", "--expires", "2099-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "add" || opts.path != "/tmp/policy.json" || opts.rule.Provider != "brew" || opts.rule.Kind != "cask" || opts.rule.Name != "firefox" || opts.rule.Decision != "allow" || opts.rule.Reason != "trusted vendor" || opts.rule.Expires != "2099-01-01" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseSecurityPolicyOptionsTTL(t *testing.T) {
	before := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	opts, err := parseSecurityPolicyOptions([]string{"add", "--name", "firefox", "--decision", "allow", "--reason", "trusted vendor", "--ttl-days", "7"})
	after := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if opts.rule.Expires != before && opts.rule.Expires != after {
		t.Fatalf("expected ttl-derived expiry around %s/%s, got %+v", before, after, opts)
	}
	if !opts.set["expires"] {
		t.Fatalf("expected ttl-days to mark expires as set, got %+v", opts.set)
	}
	if _, err := parseSecurityPolicyOptions([]string{"add", "--name", "firefox", "--decision", "allow", "--reason", "trusted vendor", "--expires", "2099-01-01", "--ttl-days", "7"}); err == nil {
		t.Fatal("expected expires and ttl-days conflict")
	}
	if _, err := parseSecurityPolicyOptions([]string{"list", "--ttl-days", "7"}); err == nil {
		t.Fatal("expected ttl-days to be rejected for list")
	}
}

func TestParseSecurityPolicyOptionsDecisionAction(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"allow", "--path", "/tmp/policy.json", "--provider", "npm", "--name", "pnpm", "--reason", "reviewed vendor", "--ttl-days", "7"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "add" || opts.rule.Decision != "allow" || opts.rule.Provider != "npm" || opts.rule.Name != "pnpm" || opts.rule.Expires == "" {
		t.Fatalf("unexpected decision-action options: %+v", opts)
	}
	if _, err := parseSecurityPolicyOptions([]string{"review", "--name", "pnpm", "--decision", "block", "--reason", "conflict"}); err == nil {
		t.Fatal("expected decision override conflict")
	}
}

func TestParseSecurityPolicyOptionsRemove(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"remove", "--path", "/tmp/policy.json", "--index", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "remove" || opts.path != "/tmp/policy.json" || opts.index != 2 {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseSecurityPolicyOptionsCleanup(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"cleanup", "--path", "/tmp/policy.json", "--apply"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "cleanup" || opts.path != "/tmp/policy.json" || !opts.apply {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityPolicyOptions([]string{"list", "--apply"}); err == nil {
		t.Fatal("expected cleanup-only apply error")
	}
}

func TestParseSecurityPolicyOptionsUpdateTracksSetFields(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"update", "--path", "/tmp/policy.json", "--index", "2", "--reason", "renewed review"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "update" || opts.index != 2 || opts.rule.Reason != "renewed review" || !opts.set["reason"] || opts.set["name"] {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseSecurityPolicyOptionsRenew(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"renew", "--path", "/tmp/policy.json", "--index", "2", "--ttl-days", "30", "--reason", "reviewed again"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "update" || opts.index != 2 || opts.rule.Expires == "" || opts.rule.Reason != "reviewed again" || !opts.set["expires"] || !opts.set["reason"] {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityPolicyOptions([]string{"renew", "--index", "2"}); err == nil {
		t.Fatal("expected renew without ttl-days to fail")
	}
}

func TestParseSecurityPolicyOptionsStateFilter(t *testing.T) {
	opts, err := parseSecurityPolicyOptions([]string{"list", "--state", "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.action != "list" || opts.state != "expired" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseSecurityPolicyOptions([]string{"list", "--state", "stale"}); err == nil {
		t.Fatal("expected unsupported state error")
	}
	if _, err := parseSecurityPolicyOptions([]string{"list", "--decision", "skip"}); err == nil {
		t.Fatal("expected unsupported decision error")
	}
}

func TestBuildSecurityGateReportUsesExplicitPolicy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"provider":"brew","kind":"cask","name":"firefox","decision":"allow","reason":"trusted"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"casks":[{"name":"firefox","installed_versions":["150.0"],"current_version":"151.0"}]}`}}
	report := buildSecurityGateReport(context.Background(), securityGateOptions{root: root, provider: "brew", policy: path}, fake)
	if report.Status != plan.StatusOK {
		t.Fatalf("expected explicit policy to allow gate, got %#v", report)
	}
	if report.Policy == nil || !report.Policy.Loaded || report.Policy.RuleCount != 1 {
		t.Fatalf("expected loaded explicit policy metadata, got %#v", report.Policy)
	}
	if len(report.Gates) != 1 || len(report.Gates[0].Findings) != 1 || report.Gates[0].Findings[0].Decision != "allow" {
		t.Fatalf("expected policy-applied gate finding, got %#v", report.Gates)
	}
}

func TestCollectUpdateSafetyUsesExplicitPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"provider":"brew","kind":"cask","name":"firefox","decision":"allow","reason":"trusted"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	policyUse := loadSecurityPolicyForReportPath(path)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"brew", "tap"}, "\x00"):                                                       {Stdout: ""},
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "outdated", "--json=v2"}, "\x00"): {Stdout: `{"casks":[{"name":"firefox","installed_versions":["150.0"],"current_version":"151.0"}]}`},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"):                          {Stdout: `{}`},
	}}
	gates := collectUpdateSafetyWithPolicy(context.Background(), fake, updateOptions{root: root, security: "strict"}, policyUse.Policy)
	if len(gates) != 2 || gates[0].Status != plan.StatusOK || gates[1].Status != plan.StatusOK {
		t.Fatalf("expected explicit policy to allow update safety, got %#v", gates)
	}
	if len(gates[0].Findings) != 1 || gates[0].Findings[0].Decision != "allow" {
		t.Fatalf("expected policy-applied update finding, got %#v", gates[0].Findings)
	}
}

func TestLoadUpdateSecurityPolicySkipsWhenSecurityOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadUpdateSecurityPolicy(updateOptions{security: "off", policy: path})
	if result.Path != "" || len(result.Warnings) != 0 || result.View() != nil {
		t.Fatalf("expected security off to skip policy load, got %#v", result)
	}
	result = loadUpdateSecurityPolicy(updateOptions{security: "warn", policy: path})
	if result.Path == "" || len(result.Warnings) != 1 || result.View() == nil {
		t.Fatalf("expected security warn to load policy diagnostics, got %#v", result)
	}
}

func TestParseUpdevConfigTOMLReadsPolicySurface(t *testing.T) {
	config := parseUpdevConfigTOML(`
[security.homebrew]
min_release_age_days = 5 # wait a work week
min_tap_age_days = 21

[security.vscode]
min_install_count = 2500
min_average_rating = 3.5
min_extension_age_days = 30
min_update_age_days = 7

[providers]
include_vscode = true

[update]
security = "strict"

[ui]
language = "ja"
interactive = "off"
progress = false
description_translation = "manual"

[sources]
root = "~/src/dotfiles"

[brewfile]
desired = "root"
write_mode = "chezmoi-template"

[inventory]
state_dir = "~/.local/state/updev/inventory"
overrides = ".config/updev/inventory-overrides.toml"

[inventory.manual]
sources = ["~/.config/updev/manual-apps.toml", "docs/apps.md"]
markdown_compat = true

[inventory.agent]
enabled = true
command = ["codex", "exec"]
batch = true

[backends]
preference_order = ["store/native", "mise/github", "linux/apt"]

[[inventory.reports]]
name = "manual-apps"
providers = ["manual", "mas", "vendor"]
format = "markdown"
path = "docs/apps.md"
`)
	if config.Security.Homebrew.MinReleaseAgeDays == nil || *config.Security.Homebrew.MinReleaseAgeDays != 5 {
		t.Fatalf("expected Homebrew release-age setting, got %#v", config)
	}
	if config.Security.Homebrew.MinTapAgeDays == nil || *config.Security.Homebrew.MinTapAgeDays != 21 {
		t.Fatalf("expected Homebrew tap-age setting, got %#v", config)
	}
	if config.Security.VSCode.MinInstallCount == nil || *config.Security.VSCode.MinInstallCount != 2500 {
		t.Fatalf("expected VS Code install-count setting, got %#v", config)
	}
	if config.Security.VSCode.MinAverageRating == nil || *config.Security.VSCode.MinAverageRating != 3.5 {
		t.Fatalf("expected VS Code rating setting, got %#v", config)
	}
	if config.Security.VSCode.MinExtensionAgeDays == nil || *config.Security.VSCode.MinExtensionAgeDays != 30 {
		t.Fatalf("expected VS Code extension-age setting, got %#v", config)
	}
	if config.Security.VSCode.MinUpdateAgeDays == nil || *config.Security.VSCode.MinUpdateAgeDays != 7 {
		t.Fatalf("expected VS Code update-age setting, got %#v", config)
	}
	if config.Providers.IncludeVSCode == nil || !*config.Providers.IncludeVSCode {
		t.Fatalf("expected VS Code provider opt-in setting, got %#v", config)
	}
	if config.Update.Security == nil || *config.Update.Security != "strict" {
		t.Fatalf("expected update security setting, got %#v", config)
	}
	if config.UI.Language == nil || *config.UI.Language != "ja" || config.UI.Interactive == nil || *config.UI.Interactive != "off" || config.UI.Progress == nil || *config.UI.Progress || config.UI.DescriptionTranslation == nil || *config.UI.DescriptionTranslation != "manual" {
		t.Fatalf("expected UI settings, got %#v", config)
	}
	if config.Sources.Root == nil || *config.Sources.Root != "~/src/dotfiles" {
		t.Fatalf("expected source root setting, got %#v", config.Sources)
	}
	if config.Brewfile.Desired == nil || *config.Brewfile.Desired != "root" || config.Brewfile.WriteMode == nil || *config.Brewfile.WriteMode != "chezmoi-template" {
		t.Fatalf("expected Brewfile settings, got %#v", config.Brewfile)
	}
	if config.Inventory.StateDir == nil || *config.Inventory.StateDir != "~/.local/state/updev/inventory" {
		t.Fatalf("expected inventory state dir, got %#v", config.Inventory)
	}
	if config.Inventory.Overrides == nil || *config.Inventory.Overrides != ".config/updev/inventory-overrides.toml" {
		t.Fatalf("expected inventory overrides path, got %#v", config.Inventory)
	}
	if got := strings.Join(config.Inventory.Manual.Sources, ","); got != "~/.config/updev/manual-apps.toml,docs/apps.md" || config.Inventory.Manual.MarkdownCompat == nil || !*config.Inventory.Manual.MarkdownCompat {
		t.Fatalf("expected manual inventory settings, got %#v", config.Inventory.Manual)
	}
	if config.Inventory.Agent.Enabled == nil || !*config.Inventory.Agent.Enabled || strings.Join(config.Inventory.Agent.Command, ",") != "codex,exec" || config.Inventory.Agent.Batch == nil || !*config.Inventory.Agent.Batch {
		t.Fatalf("expected inventory agent settings, got %#v", config.Inventory.Agent)
	}
	if len(config.Inventory.Reports) != 1 || config.Inventory.Reports[0].Name != "manual-apps" || config.Inventory.Reports[0].Format != "markdown" || config.Inventory.Reports[0].Path != "docs/apps.md" {
		t.Fatalf("expected inventory report config, got %#v", config.Inventory.Reports)
	}
	if got := strings.Join(config.Inventory.Reports[0].Providers, ","); got != "manual,mas,vendor" {
		t.Fatalf("unexpected report providers: %q", got)
	}
	if got := strings.Join(config.Backends.PreferenceOrder, ","); got != "store/native,mise/github,linux/apt" {
		t.Fatalf("unexpected backend preference order: %q", got)
	}
}

func TestValidateSecurityPolicyAllowExpiry(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.Local)
	for _, value := range []string{"2026-06-05", "2026-06-06", " 2026-06-06 "} {
		got, err := validateSecurityPolicyAllowExpiry(value, now)
		if err != nil || got == "" {
			t.Fatalf("expected expiry %q to be accepted, got %q err=%v", value, got, err)
		}
	}
	for _, value := range []string{"2026-06-04", "2026/06/06", ""} {
		if got, err := validateSecurityPolicyAllowExpiry(value, now); err == nil {
			t.Fatalf("expected expiry %q to be rejected, got %q", value, got)
		}
	}
}

func TestRefreshUpdateReportSecurityPolicyAppliesDetailDecision(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	report := updateReport{
		Policy: &securityPolicyUse{Path: policyPath},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Kind:        "cask",
				Name:        "demo",
				Decision:    "hold",
				Reason:      "needs review",
				Remediation: "review vendor",
			}},
		}},
	}
	err := addSecurityPolicyRule(policyPath, securityPolicyRule{
		Provider: "brew",
		Kind:     "cask",
		Name:     "demo",
		Decision: "allow",
		Reason:   "trusted after review",
		Expires:  "2099-01-01",
	})
	if err != nil {
		t.Fatalf("add security policy rule: %v", err)
	}
	refreshUpdateReportSecurityPolicy(&report, policyPath)
	if report.Policy == nil || report.Policy.RuleCount != 1 {
		t.Fatalf("expected policy view to refresh, got %#v", report.Policy)
	}
	if report.Safety[0].Status != plan.StatusOK || report.Safety[0].Findings[0].Decision != "allow" || report.Safety[0].Summary.Allow != 1 {
		t.Fatalf("expected safety finding to be allowed after policy refresh, got %#v", report.Safety[0])
	}
}

func TestSecurityPolicyOverridesSafetyFindings(t *testing.T) {
	findings := []safetyFinding{
		{Provider: "brew", Kind: "cask", Name: "firefox", Decision: "review", Reason: "needs review", Evidence: []string{"test"}},
		{Provider: "brew", Kind: "brew", Name: "jq", Decision: "allow", Reason: "ok"},
	}
	policy := securityPolicy{Rules: []securityPolicyRule{
		{Provider: "brew", Kind: "cask", Name: "firefox", Decision: "allow", Reason: "trusted vendor cask", Expires: "2099-01-01"},
		{Provider: "brew", Kind: "brew", Name: "jq", Decision: "block", Reason: "deny test", Expires: "2000-01-01"},
	}}
	got := applySecurityPolicyToSafetyFindings(policy, findings)
	if got[0].Decision != "allow" || got[0].Reason != "trusted vendor cask" || got[0].Confidence != "policy" {
		t.Fatalf("expected active policy override, got %#v", got[0])
	}
	if got[0].Remediation != "" {
		t.Fatalf("expected allow policy to clear remediation, got %#v", got[0])
	}
	if !containsString(got[0].Evidence, "security-policy") {
		t.Fatalf("expected policy evidence, got %#v", got[0].Evidence)
	}
	if got[1].Decision != "allow" {
		t.Fatalf("expected expired policy to be ignored, got %#v", got[1])
	}
}

func TestSecurityPolicyOverridesSecurityFindings(t *testing.T) {
	findings := []securityFinding{
		{Provider: "mise", Name: "npm:pnpm", Ecosystem: "npm", Package: "pnpm", Version: "11.1.2", VulnID: "GHSA-test", Decision: "hold", Confidence: "high", Status: plan.StatusHeld},
		{Provider: "mise", Name: "cargo:fd-find", Ecosystem: "crates.io", Package: "fd-find", Version: "10.4.2", VulnID: "RUSTSEC-test", Decision: "hold", Confidence: "high", Status: plan.StatusHeld},
		{Provider: "mise", Name: "pipx:frogmouth", Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2", VulnID: "PYSEC-test", Decision: "hold", Confidence: "high", Status: plan.StatusHeld},
	}
	policy := securityPolicy{Rules: []securityPolicyRule{
		{Provider: "npm", Name: "pnpm", Decision: "allow", Reason: "accepted local false positive"},
		{Provider: "cargo", Name: "fd-find", Decision: "block", Reason: "deny test"},
		{Provider: "pypi", Name: "frogmouth", Decision: "review", Reason: "manual review"},
	}}
	got := applySecurityPolicyToFindings(policy, findings)
	if got[0].Decision != "allow" || got[0].Status != plan.StatusOK || got[0].Confidence != "policy" {
		t.Fatalf("expected npm finding allow override, got %#v", got[0])
	}
	if got[0].Reason != "accepted local false positive" {
		t.Fatalf("expected policy reason, got %#v", got[0])
	}
	if got[0].Remediation != "" {
		t.Fatalf("expected allow policy to clear advisory remediation, got %#v", got[0])
	}
	if got[1].Decision != "block" || got[1].Status != plan.StatusBlocked {
		t.Fatalf("expected cargo finding block override, got %#v", got[1])
	}
	if !strings.Contains(got[1].Remediation, "local security policy") {
		t.Fatalf("expected non-allow policy remediation, got %#v", got[1])
	}
	if got[2].Decision != "review" || got[2].Status != plan.StatusHeld {
		t.Fatalf("expected PyPI finding review override to remain held, got %#v", got[2])
	}
	if reason := securityFindingReason(got[0]); reason != "accepted local false positive" {
		t.Fatalf("expected policy reason in text output helper, got %q", reason)
	}
}

func TestSecurityPolicyOverridesScanPostures(t *testing.T) {
	policy := securityPolicy{Rules: []securityPolicyRule{
		{Provider: "github-repo", Name: "owner/tool", Decision: "allow", Reason: "trusted archived upstream"},
		{Provider: "brew", Kind: "cask", Name: "firefox", Decision: "allow", Reason: "trusted vendor cask"},
		{Provider: "brew", Kind: "vscode", Name: "github.copilot", Decision: "block", Reason: "disabled locally"},
	}}
	githubPostures := applySecurityPolicyToGitHubPostures(policy, []githubPosture{
		{Provider: "mise", Name: "github:owner/tool", Repository: "owner/tool", Decision: "review", Reason: "repository is archived"},
	})
	homebrewPostures := applySecurityPolicyToHomebrewPostures(policy, []homebrewPosture{
		{Provider: "brew", Kind: "cask", Name: "firefox", Decision: "review", Reason: "needs provenance review"},
	})
	vscodePostures := applySecurityPolicyToVSCodePostures(policy, []vscodePosture{
		{Provider: "brew", Kind: "vscode", Name: "github.copilot", Decision: "allow", Reason: "ok"},
	})
	if githubPostures[0].Decision != "allow" || githubPostures[0].Confidence != "policy" || githubPostures[0].Remediation != "" {
		t.Fatalf("expected github posture policy override, got %#v", githubPostures[0])
	}
	if homebrewPostures[0].Decision != "allow" || homebrewPostures[0].Remediation != "" || !containsString(homebrewPostures[0].Evidence, "security-policy") {
		t.Fatalf("expected homebrew posture policy override, got %#v", homebrewPostures[0])
	}
	if vscodePostures[0].Decision != "block" || vscodePostures[0].Reason != "disabled locally" || !strings.Contains(vscodePostures[0].Remediation, "local security policy") {
		t.Fatalf("expected vscode posture policy override, got %#v", vscodePostures[0])
	}
	if got := securityPostureStatus(plan.StatusOK, githubPostures, homebrewPostures, vscodePostures, nil, nil, nil); got != plan.StatusBlocked {
		t.Fatalf("expected blocked posture status, got %s", got)
	}
}

func TestPrintSecurityUsageIncludesPolicyCommand(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityUsage(&buffer)
	got := buffer.String()
	for _, want := range []string{"<scan|review|gate|policy>", "security scan", "security review", "security gate", "security policy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected security usage to include %q, got %q", want, got)
		}
	}
}

func TestPrintSecurityPolicyTextIncludesExpiresAndFlags(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityPolicyText(&buffer, securityPolicyReport{
		Status: plan.StatusOK,
		Path:   "/tmp/security-policy.json",
		Summary: &securityPolicySummary{
			RuleCount:     3,
			ActiveRules:   2,
			FilteredRules: 2,
		},
		Rules: []securityPolicyRuleView{
			{Line: 3, Name: "pnpm", Decision: "allow", Expires: "2099-01-01", Active: true, Reason: "temporary exception"},
			{Name: "*", Decision: "allow", Active: true, MissingReason: true, MissingExpiry: true, Broad: true, Remediation: "add an expiry"},
		},
	})
	got := buffer.String()
	if !strings.Contains(got, "2 shown") || !strings.Contains(got, "line") || !strings.Contains(got, " 3 ") || !strings.Contains(got, "expires") || !strings.Contains(got, "2099-01-01") || !strings.Contains(got, "missing-reason,missing-expiry,broad") || !strings.Contains(got, "#") || !strings.Contains(got, "policy cleanup") || !strings.Contains(got, "add an expiry") {
		t.Fatalf("expected policy text to include expiry and flags, got %q", got)
	}
}

func TestPrintSecurityTextIncludesLoadedPolicy(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Sources: []string{"osv"},
		Policy: &securityPolicyUse{
			Path:            "/tmp/security-policy.json",
			Loaded:          true,
			RuleCount:       2,
			ActiveRules:     1,
			InvalidRules:    1,
			MissingReasons:  1,
			MissingExpiries: 1,
			BroadRules:      1,
		},
	}, false)
	got := buffer.String()
	if !strings.Contains(got, "policy: /tmp/security-policy.json (2 rules, 1 active, 1 invalid, 1 missing reason, 1 missing expiry, 1 broad)") {
		t.Fatalf("expected text scan output to include loaded policy, got %q", got)
	}
}

func TestApplySecurityPolicyToScannersAllowsSpecificFinding(t *testing.T) {
	scanners := []scannerEvidence{{
		Tool:     "gitleaks",
		Status:   plan.StatusHeld,
		Decision: "hold",
		Reason:   "gitleaks reported possible secrets",
		Findings: []scannerFinding{{
			Kind:     "secret",
			RuleID:   "generic-api-key",
			File:     ".env.example",
			Decision: "hold",
			Reason:   "gitleaks reported possible secret",
		}},
	}}
	policy := securityPolicy{Rules: []securityPolicyRule{{
		Provider: "scanner",
		Kind:     "gitleaks",
		Name:     "generic-api-key",
		Decision: "allow",
		Reason:   "example file contains redacted placeholder",
		Expires:  "2099-01-01",
	}}}
	got := applySecurityPolicyToScanners(policy, scanners)
	if len(got) != 1 || got[0].Status != plan.StatusOK || got[0].Decision != "allow" {
		t.Fatalf("expected scanner policy allow to clear hold, got %#v", got)
	}
	if got[0].Findings[0].Decision != "allow" || got[0].Findings[0].Confidence != "policy" || got[0].Findings[0].Remediation != "" || !containsString(got[0].Findings[0].Evidence, "security-policy") {
		t.Fatalf("expected finding policy metadata, got %#v", got[0].Findings[0])
	}
	if hasScannerFindings(got) {
		t.Fatalf("expected allowed scanner finding not to require attention, got %#v", got)
	}
}

func TestLoadSecurityPolicyFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"provider":"brew","kind":"cask","name":"firefox","decision":"allow"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_SECURITY_POLICY", path)
	policy := loadSecurityPolicy()
	if len(policy.Rules) != 1 || policy.Rules[0].Name != "firefox" {
		t.Fatalf("expected policy file to load, got %#v", policy)
	}
}

func TestBuildSecurityPolicyReportMarksInvalidAndExpiredRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"ok"},
  {"provider":"cargo","name":"fd-find","decision":"reject"},
  {"provider":"pypi","name":"frogmouth","decision":"review","expires":"2000-01-01"},
  {"provider":"npm","name":"bad-expiry","decision":"allow","expires":"tomorrow"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, format: "json"})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report for invalid rule, got %#v", report)
	}
	if len(report.Rules) != 4 {
		t.Fatalf("expected four rules, got %#v", report.Rules)
	}
	if report.Rules[0].Line != 2 || report.Rules[3].Line != 5 {
		t.Fatalf("expected policy rule line numbers, got %#v", report.Rules)
	}
	if !report.Rules[0].Active || report.Rules[0].State != "active" || report.Rules[1].Active || report.Rules[1].State != "invalid" || !report.Rules[1].Invalid || !strings.Contains(report.Rules[1].Remediation, "invalid") || !report.Rules[2].Expired || report.Rules[2].State != "expired" || !strings.Contains(report.Rules[2].Remediation, "expired") || !report.Rules[3].Invalid {
		t.Fatalf("unexpected policy rule states: %#v", report.Rules)
	}
	if report.Summary == nil || report.Summary.ActiveRules != 1 || report.Summary.ExpiredRules != 1 || report.Summary.InvalidRules != 2 {
		t.Fatalf("unexpected policy summary: %#v", report.Summary)
	}
}

func TestBuildSecurityPolicyReportFiltersRulesByState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"ok"},
  {"provider":"npm","name":"old","decision":"review","reason":"old","expires":"2000-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, state: "expired"})
	if report.Summary == nil || report.Summary.RuleCount != 2 || report.Summary.ExpiredRules != 1 {
		t.Fatalf("expected full summary despite filter, got %#v", report.Summary)
	}
	if report.Summary.FilteredRules != 1 {
		t.Fatalf("expected filtered rule count, got %#v", report.Summary)
	}
	if len(report.Rules) != 1 || report.Rules[0].Name != "old" {
		t.Fatalf("expected only expired rule, got %#v", report.Rules)
	}
}

func TestBuildSecurityPolicyReportFiltersRulesByFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"ok","expires":"2099-01-01"},
  {"provider":"scanner","kind":"gitleaks","name":"generic-api-key","decision":"review","reason":"example secret"},
  {"provider":"brew","kind":"cask","name":"firefox","decision":"allow","reason":"trusted","expires":"2099-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{
		path: path,
		rule: securityPolicyRule{
			Provider: "scanner",
			Kind:     "gitleaks",
			Decision: "review",
		},
		set: map[string]bool{"provider": true, "kind": true, "decision": true},
	})
	if report.Summary == nil || report.Summary.RuleCount != 3 || report.Summary.ActiveRules != 3 {
		t.Fatalf("expected full summary despite field filters, got %#v", report.Summary)
	}
	if len(report.Rules) != 1 || report.Rules[0].Name != "generic-api-key" {
		t.Fatalf("expected filtered scanner policy rule, got %#v", report.Rules)
	}
}

func TestBuildSecurityPolicyReportFiltersRulesNeedingCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"ok","decision":"review","reason":"ok","expires":"2099-01-01"},
  {"provider":"npm","name":"missing-reason","decision":"allow","expires":"2099-01-01"},
  {"provider":"npm","name":"missing-expiry","decision":"allow","reason":"temporary"},
  {"provider":"","name":"*","decision":"review","reason":"broad"},
  {"provider":"npm","name":"expired","decision":"review","reason":"old","expires":"2000-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, state: "needs-cleanup"})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report for cleanup rules, got %#v", report)
	}
	if report.Summary == nil || report.Summary.RuleCount != 5 || report.Summary.MissingReasons != 1 || report.Summary.MissingExpiries != 2 || report.Summary.BroadRules != 1 || report.Summary.ExpiredRules != 1 {
		t.Fatalf("expected full cleanup summary, got %#v", report.Summary)
	}
	names := []string{}
	for _, rule := range report.Rules {
		names = append(names, rule.Name)
	}
	if strings.Join(names, ",") != "missing-reason,missing-expiry,*,expired" {
		t.Fatalf("expected cleanup rules only, got %#v", report.Rules)
	}
}

func TestBuildSecurityPolicyReportPlansCleanupWithoutApplying(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"review","reason":"active","expires":"2099-01-01"},
  {"provider":"npm","name":"pnpm","decision":"hold","reason":"duplicate","expires":"2099-01-01"},
  {"provider":"pypi","name":"old","decision":"review","reason":"expired","expires":"2000-01-01"},
  {"name":"*","decision":"review","reason":"broad","expires":"2099-01-01"},
  {"provider":"cargo","name":"fd-find","decision":"review","reason":"shadowed","expires":"2099-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{action: "cleanup", path: path})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held dry-run cleanup report, got %#v", report)
	}
	if len(report.Cleanup) != 3 || report.Cleanup[0].Index != 2 || report.Cleanup[1].Index != 3 || report.Cleanup[2].Index != 5 {
		t.Fatalf("expected duplicate, expired, and shadowed cleanup plan, got %#v", report.Cleanup)
	}
	if len(report.Rules) != 4 {
		t.Fatalf("expected cleanup action to show needs-cleanup rules, got %#v", report.Rules)
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 5 {
		t.Fatalf("expected dry-run cleanup not to mutate policy, got %#v", policy)
	}
}

func TestBuildSecurityPolicyReportAppliesCleanupRemovals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"review","reason":"active","expires":"2099-01-01"},
  {"provider":"npm","name":"pnpm","decision":"hold","reason":"duplicate","expires":"2099-01-01"},
  {"provider":"pypi","name":"old","decision":"review","reason":"expired","expires":"2000-01-01"},
  {"name":"*","decision":"review","reason":"broad","expires":"2099-01-01"},
  {"provider":"cargo","name":"fd-find","decision":"review","reason":"shadowed","expires":"2099-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{action: "cleanup", path: path, apply: true})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report because broad rule remains, got %#v", report)
	}
	if len(report.Cleanup) != 3 || !report.Cleanup[0].Applied || !strings.Contains(report.Cleanup[0].Command, "security policy remove") {
		t.Fatalf("expected applied cleanup plan, got %#v", report.Cleanup)
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, rule := range policy.Rules {
		names = append(names, rule.Name)
	}
	if strings.Join(names, ",") != "pnpm,*" {
		t.Fatalf("expected only active and broad rules to remain, got %#v", policy.Rules)
	}
}

func TestBuildSecurityPolicyReportCleanupHonorsFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"review","reason":"active","expires":"2099-01-01"},
  {"provider":"npm","name":"pnpm","decision":"hold","reason":"duplicate","expires":"2099-01-01"},
  {"provider":"pypi","name":"old","decision":"review","reason":"expired","expires":"2000-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{
		action: "cleanup",
		path:   path,
		apply:  true,
		rule:   securityPolicyRule{Provider: "pypi"},
		set:    map[string]bool{"provider": true},
	})
	if len(report.Cleanup) != 1 || report.Cleanup[0].Provider != "pypi" {
		t.Fatalf("expected filtered pypi cleanup only, got %#v", report.Cleanup)
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 2 || policy.Rules[1].Provider != "npm" {
		t.Fatalf("expected npm duplicate to remain outside filtered cleanup, got %#v", policy.Rules)
	}
}

func TestBuildSecurityPolicyReportAddsRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	report := buildSecurityPolicyReport(securityPolicyOptions{
		action: "add",
		path:   path,
		rule: securityPolicyRule{
			Provider: "brew",
			Kind:     "cask",
			Name:     "firefox",
			Decision: "allow",
			Reason:   "trusted vendor",
			Expires:  "2099-01-01",
		},
	})
	if report.Status != plan.StatusOK {
		t.Fatalf("expected add report ok, got %#v", report)
	}
	if len(report.Rules) != 1 || report.Rules[0].Name != "firefox" || report.Rules[0].Line == 0 {
		t.Fatalf("expected added rule with line number, got %#v", report.Rules)
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 1 || policy.Rules[0].Reason != "trusted vendor" {
		t.Fatalf("expected persisted policy rule, got %#v", policy)
	}
}

func TestBuildSecurityPolicyReportRejectsUnsafeAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	report := buildSecurityPolicyReport(securityPolicyOptions{
		action: "add",
		path:   path,
		rule: securityPolicyRule{
			Name:     "firefox",
			Decision: "allow",
			Reason:   "trusted vendor",
		},
	})
	if report.Status != plan.StatusError || !strings.Contains(report.Error, "--expires") {
		t.Fatalf("expected missing expiry error, got %#v", report)
	}
}

func TestBuildSecurityPolicyReportRemovesRuleByIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"brew","kind":"cask","name":"firefox","decision":"allow","reason":"trusted","expires":"2099-01-01"},
  {"provider":"npm","name":"pnpm","decision":"review","reason":"temporary review","expires":"2099-01-01"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{action: "remove", path: path, index: 1})
	if report.Status != plan.StatusOK {
		t.Fatalf("expected remove report ok, got %#v", report)
	}
	if len(report.Rules) != 1 || report.Rules[0].Name != "pnpm" {
		t.Fatalf("expected remaining pnpm rule, got %#v", report.Rules)
	}
	policy, err := readSecurityPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != 1 || policy.Rules[0].Name != "pnpm" {
		t.Fatalf("expected persisted remove, got %#v", policy)
	}
}

func TestBuildSecurityPolicyReportRejectsOutOfRangeRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{action: "remove", path: path, index: 1})
	if report.Status != plan.StatusError || !strings.Contains(report.Error, "out of range") {
		t.Fatalf("expected out of range error, got %#v", report)
	}
}

func TestBuildSecurityPolicyReportUpdatesRuleByIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"brew","kind":"cask","name":"firefox","decision":"review","reason":"initial review"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{
		action: "update",
		path:   path,
		index:  1,
		rule: securityPolicyRule{
			Decision: "allow",
			Reason:   "trusted after review",
			Expires:  "2099-01-01",
		},
		set: map[string]bool{"decision": true, "reason": true, "expires": true},
	})
	if report.Status != plan.StatusOK {
		t.Fatalf("expected update report ok, got %#v", report)
	}
	if len(report.Rules) != 1 || report.Rules[0].Decision != "allow" || report.Rules[0].Reason != "trusted after review" || report.Rules[0].Name != "firefox" {
		t.Fatalf("expected updated rule preserving name, got %#v", report.Rules)
	}
}

func TestBuildSecurityPolicyReportRejectsUnsafeUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"brew","kind":"cask","name":"firefox","decision":"review","reason":"initial review"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{
		action: "update",
		path:   path,
		index:  1,
		rule: securityPolicyRule{
			Decision: "allow",
		},
		set: map[string]bool{"decision": true},
	})
	if report.Status != plan.StatusError || !strings.Contains(report.Error, "--expires") {
		t.Fatalf("expected missing expiry error, got %#v", report)
	}
}

func TestBuildSecurityPolicyReportMarksDuplicateRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"first"},
  {"provider":" npm ","name":" PNPM ","decision":"block","reason":"duplicate"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, format: "json"})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report for duplicate rule, got %#v", report)
	}
	if len(report.Rules) != 2 || !report.Rules[0].Active || report.Rules[0].State != "active" || !report.Rules[1].Duplicate || report.Rules[1].State != "duplicate" || report.Rules[1].Active {
		t.Fatalf("unexpected duplicate policy rule states: %#v", report.Rules)
	}
	if report.Summary == nil || report.Summary.ActiveRules != 1 || report.Summary.DuplicateRules != 1 {
		t.Fatalf("unexpected duplicate policy summary: %#v", report.Summary)
	}
}

func TestBuildSecurityPolicyReportHoldsBroadAndIncompleteRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"scoped exception"},
  {"name":"*","decision":"review","reason":"temporary global hold"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, format: "json"})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report for broad or incomplete rules, got %#v", report)
	}
	if len(report.Rules) != 2 || report.Rules[0].Broad || !report.Rules[1].Broad || !strings.Contains(report.Rules[1].Remediation, "narrow") {
		t.Fatalf("unexpected broad policy rule states: %#v", report.Rules)
	}
	if report.Summary == nil || report.Summary.BroadRules != 1 {
		t.Fatalf("unexpected broad policy summary: %#v", report.Summary)
	}
}

func TestBuildSecurityPolicyReportMarksShadowedRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"name":"*","decision":"review","reason":"temporary global hold"},
  {"provider":"npm","name":"pnpm","decision":"allow","reason":"specific exception"},
  {"provider":"npm","name":"pnpm","decision":"block","reason":"specific block"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildSecurityPolicyReport(securityPolicyOptions{path: path, format: "json"})
	if report.Status != plan.StatusHeld {
		t.Fatalf("expected held report for shadowed rule, got %#v", report)
	}
	if len(report.Rules) != 3 || report.Rules[0].Index != 1 || !report.Rules[0].Active || !report.Rules[0].Broad || !report.Rules[1].Shadowed || report.Rules[1].ShadowedBy != 1 || report.Rules[1].State != "shadowed" || !strings.Contains(report.Rules[1].Remediation, "rule #1") || report.Rules[1].Active || !report.Rules[2].Shadowed || report.Rules[2].ShadowedBy != 1 || report.Rules[2].Duplicate {
		t.Fatalf("unexpected shadowed policy rule states: %#v", report.Rules)
	}
	if report.Summary == nil || report.Summary.ActiveRules != 1 || report.Summary.ShadowedRules != 2 || report.Summary.BroadRules != 1 {
		t.Fatalf("unexpected shadowed policy summary: %#v", report.Summary)
	}
}

func TestSecurityPolicyMatchingNormalizesFields(t *testing.T) {
	policy := securityPolicy{Rules: []securityPolicyRule{
		{Provider: " npm ", Name: " pnpm ", Decision: " DENY ", Reason: "accepted"},
	}}
	rule, ok := matchingSecurityPolicyRule(policy, "npm", "", "pnpm")
	if !ok || rule.Decision != "block" || rule.Provider != "npm" || rule.Name != "pnpm" {
		t.Fatalf("expected normalized matching policy rule, got %#v ok=%v", rule, ok)
	}
}

func TestLoadSecurityPolicyForReportReturnsWarningOnInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_SECURITY_POLICY", path)
	result := loadSecurityPolicyForReport()
	view := result.View()
	if len(result.Warnings) != 1 || view == nil || view.Loaded || view.Error == "" {
		t.Fatalf("expected invalid policy warning and report view, got %#v %#v", result, view)
	}
}

func TestSecurityPolicyLoadResultReportsDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	if err := os.WriteFile(path, []byte(`{"rules":[
  {"provider":"npm","name":"pnpm","decision":"allow"},
  {"provider":"npm","name":"pnpm","decision":"block"},
  {"provider":"cargo","name":"fd-find","decision":"reject"},
  {"name":"*","decision":"review","reason":"global review"}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadSecurityPolicyForReportPath(path)
	view := result.View()
	if len(result.Warnings) != 5 {
		t.Fatalf("expected invalid, duplicate, missing reason, and broad policy warnings, got %#v", result.Warnings)
	}
	if view == nil || view.ActiveRules != 2 || view.InvalidRules != 1 || view.DuplicateRules != 1 || view.MissingReasons != 1 || view.MissingExpiries != 2 || view.BroadRules != 1 {
		t.Fatalf("expected policy diagnostic counters, got %#v", view)
	}
}
