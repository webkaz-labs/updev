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

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/packageexecutor"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func addRequiredMiseFakeResults(results map[string]runner.Result) map[string]runner.Result {
	if results == nil {
		results = map[string]runner.Result{}
	}
	results["mise\x00latest\x00--help"] = runner.Result{Stdout: "Usage: mise latest [OPTIONS] <TOOL@VERSION>\n      --minimum-release-age <MINIMUM_RELEASE_AGE>"}
	results["mise\x00settings\x00ls\x00--json-extended"] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	results["mise\x00settings\x00ls\x00--json-extended\x00--cd\x00/repo"] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	results["mise\x00bootstrap\x00status\x00--json\x00--cd\x00/repo"] = runner.Result{Stdout: `{"packages":{},"tools":[]}`}
	results["mise\x00bootstrap\x00plan\x00--json\x00--cd\x00/repo"] = runner.Result{Stdout: `{"resources":[],"summary":{"changes":0}}`}
	results["mise\x00bootstrap\x00packages\x00apply\x00--dry-run\x00--cd\x00/repo"] = runner.Result{Stdout: "No package changes"}
	results["mise\x00config\x00ls\x00--json\x00--cd\x00/repo"] = runner.Result{Stdout: `[{"path":"/fake/mise/config.toml","tools":["go"]},{"path":"/repo/mise.team-blue.toml","tools":["node"]}]`}
	return results
}

func TestBuildPackageParityReportUsesRenderedBrewfileAndResolvedMiseSources(t *testing.T) {
	root := t.TempDir()
	brewfilePath := filepath.Join(root, "Brewfile")
	misePath := filepath.Join(root, "mise.toml")
	if err := os.WriteFile(brewfilePath, []byte("brew \"jq\"\ncask \"firefox\"\ntap \"example/tools\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(misePath, []byte("[bootstrap.brew.taps]\n\"example/tools\" = \"https://github.com/example/homebrew-tools.git\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"mise\x00config\x00ls\x00--json\x00--cd\x00" + root: {Stdout: fmt.Sprintf(`[{"path":%q,"tools":[]}]`, misePath)},
		"mise\x00bootstrap\x00status\x00--json\x00--cd\x00" + root: {Stdout: `{
			"packages": {
				"brew": {"available":false,"reason":"unsupported architecture","packages":[{"package":"jq","requested_version":"latest","state":"skipped"}]},
				"brew-cask": {"available":true,"packages":[{"package":"firefox","requested_version":"latest","state":"installed","installed_version":"153.0.3"}]}
			},
			"tools": []
		}`},
	}}
	report, err := buildPackageParityReport(context.Background(), root, brewfilePath, fake)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != plan.StatusOK || report.Summary.Matched != 3 || len(report.Items) != 3 {
		t.Fatalf("expected formula/cask/tap parity, got %#v", report)
	}
}

func TestParsePackageExecutorOptionsSeparatesTTYAndJSON(t *testing.T) {
	opts, err := parsePackageExecutorOptions([]string{"--root", "/repo", "--interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.root != "/repo" || !opts.interactive || opts.format != "text" {
		t.Fatalf("unexpected package executor options: %#v", opts)
	}
	if _, err := parsePackageExecutorOptions([]string{"--interactive", "--format", "json"}); err == nil {
		t.Fatal("expected interactive JSON conflict")
	}
	if _, err := parsePackageParityOptions([]string{"--interactive"}); err == nil {
		t.Fatal("expected package parity to reject interactive mode")
	}
}

func TestPackageExecutorRowsRenderInSharedDetailBrowser(t *testing.T) {
	report := packageexecutor.Report{Items: []packageexecutor.Item{{
		Identity:      "brew/formula/jq",
		Kind:          "formula",
		Name:          "jq",
		DesiredSource: packageexecutor.SourceBrewfile,
		Executor:      packageexecutor.ExecutorNative,
		Status:        plan.StatusOK,
		ReasonCode:    "brewfile-native-authority",
		Reason:        "Brewfile-only desired state stays on the native item-scoped provider",
	}}}
	model := newDetailBrowserModel("updev package executor plan", packageexecutor.DetailRows(report, "ja"), detailBrowserState{}, false)
	view := model.View().Content
	for _, want := range []string{"updev package executor plan", "executor", "jq", "Brewfileをnative適用"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected executor TUI to include %q:\n%s", want, view)
		}
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if view := model.View().Content; !strings.Contains(view, "identity: brew/formula/jq") {
		t.Fatalf("expected expanded executor evidence to include canonical identity:\n%s", view)
	}
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
		results: addRequiredMiseFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.8.2"},
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
	foundBrewJSON := false
	for _, check := range report.Checks {
		if check.Tool == "brew" && check.Feature == "outdated-json-v2" {
			foundBrewJSON = true
			joined := strings.Join(check.Command, "\x00")
			if !strings.Contains(joined, "HOMEBREW_NO_AUTO_UPDATE=1") || strings.Contains(joined, "HOMEBREW_NO_INSTALL_FROM_API=1") {
				t.Fatalf("expected Homebrew JSON contract command to suppress only auto-update, got %#v", check.Command)
			}
		}
	}
	if !foundBrewJSON {
		t.Fatalf("expected Homebrew JSON contract check, got %#v", report.Checks)
	}
	foundPolicy := false
	foundBootstrapStatus := false
	foundBootstrapPlan := false
	foundBootstrapPackagesDryRun := false
	foundConfigSources := false
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
		if check.Tool == "mise" && check.Feature == "bootstrap-status-json" {
			foundBootstrapStatus = true
			if check.Status != plan.StatusOK || check.Value != "packages=0" || !strings.Contains(strings.Join(check.Command, "\x00"), "--cd\x00/repo") {
				t.Fatalf("expected supported bootstrap status contract, got %#v", check)
			}
		}
		if check.Tool == "mise" && check.Feature == "bootstrap-plan-json" {
			foundBootstrapPlan = true
			if check.Status != plan.StatusOK || !strings.Contains(strings.Join(check.Command, "\x00"), "--cd\x00/repo") {
				t.Fatalf("expected supported bootstrap plan contract, got %#v", check)
			}
		}
		if check.Tool == "mise" && check.Feature == "bootstrap-packages-dry-run" {
			foundBootstrapPackagesDryRun = true
			if check.Status != plan.StatusOK || check.Value != "dry-run supported" || !strings.Contains(strings.Join(check.Command, "\x00"), "--dry-run\x00--cd\x00/repo") {
				t.Fatalf("expected supported bootstrap packages dry-run contract, got %#v", check)
			}
		}
		if check.Tool == "mise" && check.Feature == "config-sources-json" {
			foundConfigSources = true
			if check.Status != plan.StatusOK || check.Value != "sources=2, environment_sources=1" || check.Source != "/fake/mise/config.toml (reported_order=1, tools=1); /repo/mise.team-blue.toml (reported_order=2, environment=team-blue, tools=1)" {
				t.Fatalf("expected normalized mise config source contract, got %#v", check)
			}
		}
	}
	if !foundPolicy {
		t.Fatalf("expected mise minimum-release-age check, got %#v", report.Checks)
	}
	if !foundBootstrapStatus || !foundBootstrapPlan || !foundBootstrapPackagesDryRun || !foundConfigSources {
		t.Fatalf("expected mise bootstrap capability checks, got %#v", report.Checks)
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

func TestDependencyContractReportDetectsUnsupportedMiseBootstrap(t *testing.T) {
	results := addRequiredMiseFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"mise\x00--version":                 {Stdout: "2026.7.7"},
		"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
	})
	results["mise\x00bootstrap\x00status\x00--json\x00--cd\x00/repo"] = runner.Result{Stderr: "unknown command: bootstrap", Code: 2, Err: fmt.Errorf("exit status 2")}
	results["mise\x00bootstrap\x00plan\x00--json\x00--cd\x00/repo"] = runner.Result{Stderr: "unknown command: bootstrap", Code: 2, Err: fmt.Errorf("exit status 2")}
	results["mise\x00bootstrap\x00packages\x00apply\x00--dry-run\x00--cd\x00/repo"] = runner.Result{Stderr: "unknown command: bootstrap", Code: 2, Err: fmt.Errorf("exit status 2")}
	fake := dependencyContractFakeRunner(results)

	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusError {
		t.Fatalf("expected unsupported required bootstrap contract to fail, got %#v", report)
	}
	for _, feature := range []string{"bootstrap-status-json", "bootstrap-plan-json", "bootstrap-packages-dry-run"} {
		check := dependencyCheckByFeature(t, report.Checks, "mise", feature)
		if check.Status != plan.StatusError || !strings.Contains(check.Reason, "unknown command") {
			t.Fatalf("expected unsupported %s evidence, got %#v", feature, check)
		}
	}
}

func TestDependencyContractReportRejectsMiseBelowMinimumWithSupportedContracts(t *testing.T) {
	results := addRequiredMiseFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"mise\x00--version":                 {Stdout: "2026.8.1 macos-x64"},
		"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
	})
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, dependencyContractFakeRunner(results))
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected pre-v%s mise to produce drift, got %#v", minimumMiseVersion, report)
	}
	check := dependencyCheckByFeature(t, report.Checks, "mise", "cli-version")
	if check.Status != plan.StatusDrift || !strings.Contains(check.Reason, minimumMiseVersion) || !strings.Contains(check.Remediation, minimumMiseVersion) {
		t.Fatalf("expected explicit mise version floor evidence, got %#v", check)
	}
	for _, feature := range []string{"bootstrap-status-json", "bootstrap-plan-json", "bootstrap-packages-dry-run"} {
		if check := dependencyCheckByFeature(t, report.Checks, "mise", feature); check.Status != plan.StatusOK {
			t.Fatalf("expected %s probe to pass so only the version floor fails, got %#v", feature, check)
		}
	}
}

func TestMiseVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "2026.8.1 macos-x64", want: false},
		{version: "v2026.8.2", want: true},
		{version: "2026.8.3 linux-arm64", want: true},
		{version: "2027.1.0", want: true},
		{version: "invalid", want: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			if got := miseVersionAtLeast(test.version, minimumMiseVersion); got != test.want {
				t.Fatalf("miseVersionAtLeast(%q, %q) = %t, want %t", test.version, minimumMiseVersion, got, test.want)
			}
		})
	}
}

func TestDependencyContractReportDetectsMissingMiseBootstrap(t *testing.T) {
	fake := &fakeCommandRunner{paths: map[string]error{
		"brew": fmt.Errorf("missing"), "mise": fmt.Errorf("missing"), "codex": fmt.Errorf("missing"),
		"osv-scanner": fmt.Errorf("missing"), "gitleaks": fmt.Errorf("missing"),
		"zizmor": fmt.Errorf("missing"), "trivy": fmt.Errorf("missing"), "grype": fmt.Errorf("missing"),
	}}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	for _, feature := range []string{"bootstrap-status-json", "bootstrap-plan-json", "bootstrap-packages-dry-run"} {
		check := dependencyCheckByFeature(t, report.Checks, "mise", feature)
		if check.Status != plan.StatusUnavailable || !strings.Contains(check.Reason, "not found") {
			t.Fatalf("expected unavailable %s evidence, got %#v", feature, check)
		}
	}
}

func TestDependencyContractReportDetectsMalformedMiseBootstrapJSON(t *testing.T) {
	results := addRequiredMiseFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"mise\x00--version":                 {Stdout: "2026.8.2"},
		"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
	})
	results["mise\x00bootstrap\x00status\x00--json\x00--cd\x00/repo"] = runner.Result{Stdout: `[]`}
	results["mise\x00bootstrap\x00plan\x00--json\x00--cd\x00/repo"] = runner.Result{Stdout: `{"resources":[]}`}
	fake := dependencyContractFakeRunner(results)

	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected malformed bootstrap JSON drift, got %#v", report)
	}
	status := dependencyCheckByFeature(t, report.Checks, "mise", "bootstrap-status-json")
	if status.Status != plan.StatusDrift || !strings.Contains(status.Reason, "root is not an object") {
		t.Fatalf("expected malformed status root evidence, got %#v", status)
	}
	planCheck := dependencyCheckByFeature(t, report.Checks, "mise", "bootstrap-plan-json")
	if planCheck.Status != plan.StatusDrift || len(planCheck.MissingField) != 1 || planCheck.MissingField[0] != "summary" {
		t.Fatalf("expected missing plan summary evidence, got %#v", planCheck)
	}
}

func dependencyContractFakeRunner(results map[string]runner.Result) *fakeCommandRunner {
	return &fakeCommandRunner{
		paths: map[string]error{
			"brew": nil, "mise": nil, "codex": fmt.Errorf("missing"),
			"osv-scanner": fmt.Errorf("missing"), "gitleaks": fmt.Errorf("missing"),
			"zizmor": fmt.Errorf("missing"), "trivy": fmt.Errorf("missing"), "grype": fmt.Errorf("missing"),
		},
		results: results,
	}
}

func dependencyCheckByFeature(t *testing.T, checks []dependencyContractCheck, tool string, feature string) dependencyContractCheck {
	t.Helper()
	for _, check := range checks {
		if check.Tool == tool && check.Feature == feature {
			return check
		}
	}
	t.Fatalf("missing dependency check %s/%s", tool, feature)
	return dependencyContractCheck{}
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

func TestDependencyContractTextShowsSupportLabel(t *testing.T) {
	var builder strings.Builder
	printDependencyContractText(&builder, dependencyContractReport{
		Status: plan.StatusOK,
		Checks: []dependencyContractCheck{{
			Tool:     "mise",
			Feature:  "current-json",
			Required: true,
			Status:   plan.StatusOK,
			Version:  "2026.6.1",
		}},
	}, false)
	out := builder.String()
	if !strings.Contains(out, "support_label") || !strings.Contains(out, "supported_preview") {
		t.Fatalf("expected dependency text to include support label:\n%s", out)
	}
}

func TestDependencyContractReportIncludesBrewWrapperDiagnostics(t *testing.T) {
	t.Setenv("UPDEV_BREW_WRAPPER", "")
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
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
		results: addRequiredMiseFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.8.2"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.Status != plan.StatusOK {
		t.Fatalf("expected optional inactive wrapper diagnostics to keep report ok, got %#v", report)
	}
	foundWrapper := false
	foundWriteBoundary := false
	for _, check := range report.Checks {
		switch {
		case check.Tool == "brew" && check.Feature == "shell-wrapper":
			foundWrapper = true
			if check.Status != plan.StatusUnavailable || check.Active == nil || *check.Active {
				t.Fatalf("expected inactive optional shell wrapper diagnostic, got %#v", check)
			}
		case check.Tool == "brewfile" && check.Feature == "write-boundary":
			foundWriteBoundary = true
			if check.Status != plan.StatusUnavailable || check.Value != "disabled" || check.Active == nil || *check.Active {
				t.Fatalf("expected disabled optional Brewfile write boundary diagnostic, got %#v", check)
			}
		}
	}
	if !foundWrapper || !foundWriteBoundary {
		t.Fatalf("expected wrapper and write-boundary diagnostics, got %#v", report.Checks)
	}
}

func TestDependencyContractReportShowsActiveBrewWrapperAndWriteBoundary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_BREW_WRAPPER", "1")
	enableBrewfileWriteMode(t, root, "template")
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
		results: addRequiredMiseFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                                            {Stdout: "2026.8.2"},
			"mise\x00ls\x00--current\x00--json":                            {Stdout: `{}`},
			"mise\x00settings\x00ls\x00--json-extended\x00--cd\x00" + root: {Stdout: `{}`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: root}, fake)
	for _, check := range report.Checks {
		switch {
		case check.Tool == "brew" && check.Feature == "shell-wrapper":
			if check.Status != plan.StatusOK || check.Active == nil || !*check.Active || check.Value != "1" {
				t.Fatalf("expected active shell wrapper diagnostic, got %#v", check)
			}
		case check.Tool == "brewfile" && check.Feature == "write-boundary":
			if check.Status != plan.StatusOK || check.Active == nil || !*check.Active || check.Value != "template" {
				t.Fatalf("expected active Brewfile write boundary diagnostic, got %#v", check)
			}
		}
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
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	entries, ok := raw["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected raw ledger entries, got %#v", raw["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("expected raw ledger entry object, got %#v", entries[0])
	}
	if _, ok := entry["support_label"]; !ok {
		t.Fatalf("expected support_label key in ledger entry JSON, got %#v", entry)
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
	results := addRequiredMiseFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
		"mise\x00--version":                 {Stdout: "2026.8.2"},
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
	results := addRequiredMiseFakeResults(map[string]runner.Result{
		"brew\x00--version": {Stdout: "Homebrew 6.0.0"},
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2":   {Stdout: `{"formulae":[],"casks":[]}`},
		"env\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00trust\x00--json=v1": {Stdout: `{"taps":[],"formulae":["vendor/tap/tool"],"casks":[],"commands":[]}`},
		"mise\x00--version":                                            {Stdout: "2026.8.2"},
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
			if check.Status != plan.StatusDrift || !strings.Contains(check.Value, "0 trusted taps, 1 untrusted taps, 3 targets (1 trusted, 2 untrusted)") {
				t.Fatalf("expected trust summary drift, got %#v", check)
			}
			if !strings.Contains(check.Reason, "vendor/tap (2 untrusted/3 targets)") {
				t.Fatalf("expected untrusted tap group in reason, got %#v", check)
			}
			if !strings.Contains(check.Remediation, "brew trust --tap vendor/tap") {
				t.Fatalf("expected tap-scoped remediation, got %#v", check)
			}
			if len(check.TrustGroups) != 1 || check.TrustGroups[0].Tap != "vendor/tap" || check.TrustGroups[0].Trusted {
				t.Fatalf("expected untrusted tap group, got %#v", check.TrustGroups)
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
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.8.2"},
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
		results: addRequiredMiseFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[]}`},
			"mise\x00--version":                 {Stdout: "2026.8.2"},
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
		results: addRequiredMiseFakeResults(map[string]runner.Result{
			"brew\x00--version": {Stdout: "Homebrew 4.5.0"},
			"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00brew\x00outdated\x00--json=v2": {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.8.2"},
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
