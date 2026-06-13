package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func addMiseMinimumReleaseAgeFakeResults(results map[string]runner.Result) map[string]runner.Result {
	if results == nil {
		results = map[string]runner.Result{}
	}
	results["mise\x00latest\x00--help"] = runner.Result{Stdout: "Usage: mise latest [OPTIONS] <TOOL@VERSION>\n      --minimum-release-age <MINIMUM_RELEASE_AGE>"}
	results["mise\x00settings\x00ls\x00--json-extended"] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	results["mise\x00settings\x00ls\x00--json-extended\x00--cd\x00/repo"] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	return results
}

func TestDependencyContractReportChecksRequiredJSONContracts(t *testing.T) {
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"codex":       fmt.Errorf("missing"),
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.5.18"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.SchemaVersion != dependencyContractReportSchemaVersion || report.Status != plan.StatusOK {
		t.Fatalf("expected ok dependency contract report, got %#v", report)
	}
	if report.CompatibilityLedger.SchemaVersion != 1 || report.CompatibilityLedger.Root != "/repo" || len(report.CompatibilityLedger.Entries) == 0 {
		t.Fatalf("expected compatibility ledger entries, got %#v", report.CompatibilityLedger)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected dependency checks")
	}
	for _, check := range report.Checks {
		if check.Required && check.Status != plan.StatusOK {
			t.Fatalf("expected required checks to pass, got %#v", check)
		}
	}
	foundPolicy := false
	for _, check := range report.Checks {
		if check.Tool == "mise" && check.Feature == "minimum-release-age" {
			foundPolicy = true
			if check.Active == nil || !*check.Active || check.Value != "3d" || check.Source != "/fake/mise/config.toml" {
				t.Fatalf("expected active mise minimum_release_age evidence, got %#v", check)
			}
			if check.CommandShapeSupported == nil || !*check.CommandShapeSupported {
				t.Fatalf("expected mise latest command shape support, got %#v", check)
			}
		}
	}
	if !foundPolicy {
		t.Fatalf("expected mise minimum-release-age check, got %#v", report.Checks)
	}
	foundCodex := false
	for _, check := range report.Checks {
		if check.Tool == "codex" && check.Feature == "description-translation" {
			foundCodex = true
			if check.Status != plan.StatusUnavailable || check.Required {
				t.Fatalf("expected optional unavailable codex translation check, got %#v", check)
			}
			if check.Value != "" || !strings.Contains(check.Reason, "Codex CLI not found") {
				t.Fatalf("expected unavailable codex detail to use reason rather than mode value, got %#v", check)
			}
		}
	}
	if !foundCodex {
		t.Fatalf("expected codex translation check, got %#v", report.Checks)
	}
}

func TestDependencyCompatibilityLedgerMarksRequiredDriftUnsupported(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	ledger := buildDependencyCompatibilityLedger("/repo", []dependencyContractCheck{
		{Tool: "brew", Feature: "outdated-json-v2", Required: true, Status: plan.StatusDrift, Version: "Homebrew 6.0.0", Command: []string{"brew", "outdated", "--json=v2"}, Remediation: "update parser"},
		{Tool: "osv-scanner", Feature: "cli-version", Required: false, Status: plan.StatusUnavailable, Reason: "missing"},
	}, now)
	if ledger.GeneratedAt != "2026-06-14T01:02:03Z" || len(ledger.Entries) != 2 {
		t.Fatalf("unexpected ledger: %#v", ledger)
	}
	if ledger.Entries[0].Supported || ledger.Entries[0].SupportLabel != "supported_preview" || !strings.Contains(ledger.Entries[0].Evidence, "Homebrew 6.0.0") {
		t.Fatalf("expected required drift to be unsupported with evidence, got %#v", ledger.Entries[0])
	}
	if !ledger.Entries[1].Supported || ledger.Entries[1].SupportLabel != "experimental" {
		t.Fatalf("expected optional unavailable scanner to stay supported, got %#v", ledger.Entries[1])
	}
}

func TestWriteDependencyCompatibilityLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compatibility-ledger.json")
	ledger := dependencyCompatibilityLedger{SchemaVersion: 1, GeneratedAt: "2026-06-14T01:02:03Z", Root: "/repo", Entries: []dependencyCompatibilityEntry{{Tool: "mise", Feature: "current-json", Status: plan.StatusOK, Supported: true}}}
	if err := writeDependencyCompatibilityLedger(path, ledger); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"support_label"`) {
		t.Fatalf("expected support_label to be part of the JSON contract, got %s", data)
	}
	var got dependencyCompatibilityLedger
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Root != "/repo" || len(got.Entries) != 1 || got.Entries[0].Tool != "mise" {
		t.Fatalf("unexpected written ledger: %#v", got)
	}
}

func TestDependencyContractReportAllowsInactiveMiseMinimumReleaseAge(t *testing.T) {
	results := addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
		"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"mise\x00--version":                 {Stdout: "2026.5.18"},
		"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
	})
	results["mise\x00settings\x00ls\x00--json-extended\x00--cd\x00/repo"] = runner.Result{Stdout: `{}`}
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: results,
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusOK {
		t.Fatalf("expected inactive mise minimum_release_age to remain ok, got %#v", report)
	}
	for _, check := range report.Checks {
		if check.Tool == "mise" && check.Feature == "minimum-release-age" {
			if check.Active == nil || *check.Active || check.Value != "" || !strings.Contains(check.Reason, "not configured") {
				t.Fatalf("expected inactive mise minimum_release_age evidence, got %#v", check)
			}
			return
		}
	}
	t.Fatalf("expected mise minimum-release-age check, got %#v", report.Checks)
}

func TestDependencyContractReportDetectsHomebrewTapTrustDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`
tap "vendor/tap"
brew "vendor/tap/tool"
cask "vendor/tap/app"
tap "homebrew/core"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	results := addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
		"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00trust\x00--json=v1":    {Stdout: `{"taps":[],"formulae":["vendor/tap/tool"],"casks":[],"commands":[]}`},
		"mise\x00--version":                                            {Stdout: "2026.5.18"},
		"mise\x00ls\x00--current\x00--json":                            {Stdout: `{}`},
		"mise\x00settings\x00ls\x00--json-extended\x00--cd\x00" + root: {Stdout: `{}`},
	})
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: results,
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: root}, fake)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected tap trust drift, got %#v", report)
	}
	for _, check := range report.Checks {
		if check.Tool == "brew" && check.Feature == "tap-trust" {
			if check.Status != plan.StatusDrift || !strings.Contains(check.Value, "1 trusted, 2 untrusted, 3 targets") {
				t.Fatalf("expected trust summary drift, got %#v", check)
			}
			if !strings.Contains(check.Reason, "cask vendor/tap/app") || !strings.Contains(check.Reason, "tap vendor/tap") {
				t.Fatalf("expected untrusted targets in reason, got %#v", check)
			}
			if !strings.Contains(check.Remediation, "item-scoped brew trust") {
				t.Fatalf("expected item-scoped remediation, got %#v", check)
			}
			return
		}
	}
	t.Fatalf("expected Homebrew tap trust check, got %#v", report.Checks)
}

func TestHomebrewTrustStateTreatsWholeTapAsTrusted(t *testing.T) {
	targets, err := parseHomebrewTrustTargets(strings.NewReader(`
brew "vendor/tap/tool"
cask "vendor/tap/app"
`), "Brewfile.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	targets = applyHomebrewTrustState(targets, homebrewTrustState{Taps: []string{"vendor/tap"}})
	trusted, untrusted := homebrewTrustTargetCounts(targets)
	if trusted != 2 || untrusted != 0 {
		t.Fatalf("expected whole tap trust to cover formula and cask targets, got trusted=%d untrusted=%d targets=%#v", trusted, untrusted, targets)
	}
	for _, target := range targets {
		if target.TrustSource != "tap" {
			t.Fatalf("expected tap trust source, got %#v", targets)
		}
		if len(target.TrustCommandArgv) == 0 || target.TrustCommand != joinCommand(target.TrustCommandArgv) {
			t.Fatalf("expected trust target to keep compatible string and structured argv, got %#v", target)
		}
	}
}

func TestDependencyContractReportLeavesMiseAgePolicyUnknownOnProbeDrift(t *testing.T) {
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.5.18"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
			"mise\x00latest\x00--help":          {Code: 1, Err: fmt.Errorf("boom")},
		},
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift dependency contract report, got %#v", report)
	}
	for _, check := range report.Checks {
		if check.Tool == "mise" && check.Feature == "minimum-release-age" {
			if check.Active != nil || check.CommandShapeSupported != nil || !strings.Contains(check.Reason, "could not verify") {
				t.Fatalf("expected unknown mise minimum_release_age evidence after probe drift, got %#v", check)
			}
			return
		}
	}
	t.Fatalf("expected mise minimum-release-age check, got %#v", report.Checks)
}

func TestDependencyContractReportDetectsBrewJSONDrift(t *testing.T) {
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[]}`},
			"mise\x00--version":                 {Stdout: "2026.5.18"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift dependency contract report, got %#v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.Tool == "brew" && check.Feature == "outdated-json-v2" {
			found = true
			if check.Status != plan.StatusDrift || len(check.MissingField) != 1 || check.MissingField[0] != "casks" {
				t.Fatalf("expected brew casks field drift, got %#v", check)
			}
		}
	}
	if !found {
		t.Fatalf("expected brew JSON check, got %#v", report.Checks)
	}
}

func TestDependencyContractReportDetectsMiseJSONRootDrift(t *testing.T) {
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"brew":        nil,
			"mise":        nil,
			"osv-scanner": fmt.Errorf("missing"),
			"gitleaks":    fmt.Errorf("missing"),
			"zizmor":      fmt.Errorf("missing"),
			"trivy":       fmt.Errorf("missing"),
			"grype":       fmt.Errorf("missing"),
		},
		results: addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.5.18"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `[]`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift dependency contract report, got %#v", report)
	}
	for _, check := range report.Checks {
		if check.Tool == "mise" && check.Feature == "current-json" && !strings.Contains(check.Reason, "root") {
			t.Fatalf("expected mise root-object drift, got %#v", check)
		}
	}
}
