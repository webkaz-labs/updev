package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/packageexecutor"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/support"
)

func TestParseUpdateOptions(t *testing.T) {
	opts, err := parseUpdateOptions([]string{"--dry-run", "--format", "json", "--root", "/tmp/root", "--security", " STRICT ", "--policy", "/tmp/policy.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.dryRun || opts.format != "json" || opts.root != "/tmp/root" || opts.security != "strict" || opts.policy != "/tmp/policy.json" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseApplyOptionsAcceptsBrewfileSurface(t *testing.T) {
	opts, err := parseApplyOptions([]string{"brewfile", "--safe-only", "--dry-run", "--format", "json", "--root", "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.target != "brewfile" || !opts.safeOnly || !opts.dryRun || opts.format != "json" || opts.root != "/repo" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if _, err := parseApplyOptions([]string{"all"}); err == nil {
		t.Fatal("expected unsupported apply target to fail")
	}
}

func TestBrewfileApplyMissingItemsOnlyIncludesMissingHomebrewDesired(t *testing.T) {
	items := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq", Desired: true, Live: false, Status: plan.StatusMissing},
		{Provider: "brew", Kind: "cask", Name: "firefox", Desired: true, Live: false, Status: plan.StatusMissing},
		{Provider: "brew", Kind: "vscode", Name: "publisher.extension", Desired: true, Live: false, Status: plan.StatusMissing},
		{Provider: "brew", Kind: "brew", Name: "git", Desired: true, Live: true, Status: plan.StatusOK},
		{Provider: "mise", Kind: "tool", Name: "node", Desired: true, Live: false, Status: plan.StatusMissing},
	}
	got := brewfileApplyMissingItems(items)
	names := []string{}
	for _, item := range got {
		names = append(names, item.Kind+":"+item.Name)
	}
	want := []string{"brew:jq", "cask:firefox"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("missing items = %#v, want %#v", names, want)
	}
}

func TestBrewfileApplyCandidatesUseItemScopedCommands(t *testing.T) {
	items := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq", Category: "dev", Desired: true, Status: plan.StatusMissing},
		{Provider: "brew", Kind: "tap", Name: "webkaz/tap", Desired: true, Status: plan.StatusMissing},
	}
	findings := []safetyFinding{
		{Provider: "brew", Kind: "brew", Name: "jq", Decision: "allow", Reason: "ok"},
		{Provider: "brew", Kind: "tap", Name: "webkaz/tap", Decision: "review", Reason: "needs review"},
	}
	executors := map[string]packageexecutor.Item{
		"brew/formula/jq": {
			Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq",
			DesiredSource: packageexecutor.SourceBrewfile, Executor: packageexecutor.ExecutorNative, Status: plan.StatusOK,
		},
		"brew/tap/webkaz/tap": {
			Identity: "brew/tap/webkaz/tap", Provider: "brew", Kind: "tap", Name: "webkaz/tap",
			DesiredSource: packageexecutor.SourceBrewfile, Executor: packageexecutor.ExecutorNative, Status: plan.StatusOK,
		},
	}
	got := brewfileApplyCandidatesFromFindings("/repo", items, findings, executors)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %#v", got)
	}
	if got[0].Decision != "allow" || got[0].Status != plan.StatusDrift {
		t.Fatalf("unexpected safe candidate: %#v", got[0])
	}
	wantCommand := []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "jq"}
	if !reflect.DeepEqual(got[0].Command, wantCommand) {
		t.Fatalf("safe command = %#v, want %#v", got[0].Command, wantCommand)
	}
	if got[1].Decision != "review" || got[1].Status != plan.StatusHeld {
		t.Fatalf("unexpected review candidate: %#v", got[1])
	}
	if got[1].Command != nil {
		t.Fatalf("review candidate must not expose an executable command: %#v", got[1])
	}
}

func TestBrewfileApplyCandidatesUseMiseAndFailClosedExecutors(t *testing.T) {
	items := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq", Desired: true, Status: plan.StatusMissing},
		{Provider: "brew", Kind: "cask", Name: "firefox", Desired: true, Status: plan.StatusMissing},
	}
	findings := []safetyFinding{
		{Provider: "brew", Kind: "brew", Name: "jq", Decision: "allow", Reason: "ok"},
		{Provider: "brew", Kind: "cask", Name: "firefox", Decision: "allow", Reason: "ok"},
	}
	executors := map[string]packageexecutor.Item{
		"brew/formula/jq": {
			Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq",
			DesiredSource: packageexecutor.SourceBoth, Manager: "brew", ManagerPackage: "jq",
			Executor: packageexecutor.ExecutorMise, Status: plan.StatusOK,
		},
		"brew/cask/firefox": {
			Identity: "brew/cask/firefox", Provider: "brew", Kind: "cask", Name: "firefox",
			DesiredSource: packageexecutor.SourceBoth, Executor: packageexecutor.ExecutorUnsupported,
			Status: plan.StatusBlocked, ReasonCode: "intentional-duplicate-required", Reason: "duplicate",
		},
	}
	got := brewfileApplyCandidatesFromFindings("/repo", items, findings, executors)
	wantMise := []string{"mise", "bootstrap", "packages", "apply", "--yes", "--cd", "/repo", "brew:jq"}
	if len(got) != 2 || got[0].Executor != packageexecutor.ExecutorMise || !reflect.DeepEqual(got[0].Command, wantMise) {
		t.Fatalf("expected exact mise command, got %#v", got)
	}
	if got[1].Decision != "block" || got[1].Command != nil || got[1].ReasonCode != "intentional-duplicate-required" {
		t.Fatalf("expected unsupported executor to fail closed, got %#v", got[1])
	}
}

func TestBrewfileApplyMiseDesiredUsesSelectedNativeExecutor(t *testing.T) {
	item := plan.Item{Provider: "brew", Kind: "brew", Name: "btop", Detail: brew.MiseBootstrapDesiredDetail, Desired: true, Status: plan.StatusMissing}
	finding := brewfileApplyBaseFinding(item)
	if finding.Source != "mise bootstrap package desired state" || !strings.Contains(strings.Join(finding.Evidence, " "), "resolved mise") {
		t.Fatalf("expected mise desired evidence, got %#v", finding)
	}
	finding.Decision = "allow"
	executors := map[string]packageexecutor.Item{
		"brew/formula/btop": {
			Identity: "brew/formula/btop", Provider: "brew", Kind: "formula", Name: "btop",
			DesiredSource: packageexecutor.SourceMise, Manager: "brew", ManagerPackage: "btop",
			Executor: packageexecutor.ExecutorNative, Status: plan.StatusOK,
		},
	}
	candidates := brewfileApplyCandidatesFromFindings("/repo", []plan.Item{item}, []safetyFinding{finding}, executors)
	want := []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "btop"}
	if len(candidates) != 1 || candidates[0].DesiredSource != packageexecutor.SourceMise || candidates[0].Executor != packageexecutor.ExecutorNative || !reflect.DeepEqual(candidates[0].Command, want) {
		t.Fatalf("unexpected migrated package candidate: %#v", candidates)
	}
}

func TestBrewfileApplyStatusDistinguishesDryRunAndHeld(t *testing.T) {
	safe := []brewfileApplyCandidate{{Decision: "allow", Status: plan.StatusDrift}}
	if got := brewfileApplyStatus(safe, true); got != plan.StatusDrift {
		t.Fatalf("safe dry-run status = %s", got)
	}
	applied := []brewfileApplyCandidate{{Decision: "allow", Status: plan.StatusOK}}
	if got := brewfileApplyStatus(applied, false); got != plan.StatusOK {
		t.Fatalf("applied status = %s", got)
	}
	mixed := []brewfileApplyCandidate{
		{Decision: "allow", Status: plan.StatusDrift},
		{Decision: "review", Status: plan.StatusHeld},
	}
	if got := brewfileApplyStatus(mixed, true); got != plan.StatusHeld {
		t.Fatalf("mixed status = %s", got)
	}
}

func TestApplyBrewfileSafeCandidatesRunsOnlyAllowedExactCommand(t *testing.T) {
	runner := &fakeCommandRunner{}
	report := brewfileApplyReport{Candidates: []brewfileApplyCandidate{
		{
			Identity: "brew/formula/jq", Provider: "brew", Kind: "brew", Name: "jq",
			Executor: packageexecutor.ExecutorNative, Decision: "allow", Status: plan.StatusDrift,
			Command: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "jq"},
		},
		{
			Identity: "brew/cask/firefox", Provider: "brew", Kind: "cask", Name: "firefox",
			Executor: packageexecutor.ExecutorNative, Decision: "review", Status: plan.StatusHeld,
		},
	}}
	got := applyBrewfileSafeCandidates(context.Background(), applyOptions{format: "json"}, runner, report)
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], report.Candidates[0].Command) {
		t.Fatalf("expected only the exact allow command to run, calls=%#v", runner.calls)
	}
	if got.Candidates[0].Status != plan.StatusOK || got.Candidates[1].Status != plan.StatusHeld || got.Status != plan.StatusHeld {
		t.Fatalf("unexpected apply result: %#v", got)
	}
}

func TestRunBrewfileApplyCommandPreservesStreamingPath(t *testing.T) {
	recording := &brewfileApplyStreamingRunner{}
	command := []string{"mise", "bootstrap", "packages", "apply", "--yes", "--cd", "/repo", "brew:jq"}
	result := runBrewfileApplyCommand(context.Background(), recording, command, true)
	if result.Err != nil || recording.streamingCalls != 1 || len(recording.calls) != 1 || !reflect.DeepEqual(recording.calls[0], command) {
		t.Fatalf("expected exact command through streaming runner, result=%#v calls=%#v streaming=%d", result, recording.calls, recording.streamingCalls)
	}
}

type brewfileApplyStreamingRunner struct {
	fakeCommandRunner
	streamingCalls int
}

func (recording *brewfileApplyStreamingRunner) RunStreaming(ctx context.Context, _ io.Writer, _ io.Writer, name string, args ...string) runner.Result {
	recording.streamingCalls++
	return recording.fakeCommandRunner.Run(ctx, name, args...)
}

func TestBuildBrewfileApplyReportConsumesExecutorPlan(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configPath := filepath.Join(configHome, "updev", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[brewfile]\ndesired = \"root\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte("tap \"homebrew/core\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	misePath := filepath.Join(root, "mise.toml")
	if err := os.WriteFile(misePath, []byte("[bootstrap.packages]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, err := json.Marshal([]map[string]any{{"path": misePath, "tools": []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "config", "ls", "--json", "--cd", root}, "\x00"):        {Stdout: string(sources)},
		strings.Join([]string{"mise", "bootstrap", "status", "--json", "--cd", root}, "\x00"): {Stdout: `{"packages":{},"tools":[]}`},
	}}
	report := buildBrewfileApplyReport(context.Background(), applyOptions{
		target: "brewfile", format: "json", root: root, dryRun: true, safeOnly: true,
	}, fake, securityPolicyLoadResult{})
	if report.Status != plan.StatusDrift || len(report.Candidates) != 1 {
		t.Fatalf("expected one safe missing candidate, got %#v", report)
	}
	candidate := report.Candidates[0]
	wantCommand := []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "tap", "homebrew/core"}
	if candidate.Identity != "brew/tap/homebrew/core" || candidate.Executor != packageexecutor.ExecutorNative || candidate.Decision != "allow" || !reflect.DeepEqual(candidate.Command, wantCommand) {
		t.Fatalf("unexpected executor-aware candidate: %#v", candidate)
	}
}

func TestBuildBrewfileApplyReportUsesEmptyCandidateArray(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[brewfile]\ndesired = \"root\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	misePath := filepath.Join(root, "mise.toml")
	if err := os.WriteFile(misePath, []byte("[bootstrap.packages]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, err := json.Marshal([]map[string]any{{"path": misePath, "tools": []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "config", "ls", "--json", "--cd", root}, "\x00"):        {Stdout: string(sources)},
		strings.Join([]string{"mise", "bootstrap", "status", "--json", "--cd", root}, "\x00"): {Stdout: `{"packages":{},"tools":[]}`},
	}}
	report := buildBrewfileApplyReport(context.Background(), applyOptions{root: root, dryRun: true}, fake, securityPolicyLoadResult{})
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"candidates":[]`) {
		t.Fatalf("expected deterministic empty candidate array: %s", data)
	}
}

func TestApplyGlobalOptionsSetsConfigAndNoColor(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", "")
	t.Setenv("NO_COLOR", "")
	args, err := applyGlobalOptions([]string{"--config", "/tmp/updev.toml", "check", "--no-color", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "check --format json" {
		t.Fatalf("unexpected stripped args: %#v", args)
	}
	if os.Getenv("UPDEV_CONFIG") != "/tmp/updev.toml" {
		t.Fatalf("expected UPDEV_CONFIG to be set, got %q", os.Getenv("UPDEV_CONFIG"))
	}
	if os.Getenv("NO_COLOR") != "1" {
		t.Fatalf("expected NO_COLOR to be set, got %q", os.Getenv("NO_COLOR"))
	}
	if _, err := applyGlobalOptions([]string{"--config"}); err == nil {
		t.Fatal("expected missing config value error")
	}
}

func TestBuildVersionReport(t *testing.T) {
	report := buildVersionReport()
	if report.SchemaVersion != 1 || report.Tool != toolName || report.Version != toolVersion {
		t.Fatalf("unexpected version report: %#v", report)
	}
	if report.Major != 0 || report.Minor != 7 || report.Patch != 19 || report.Contract != "pre_stable" {
		t.Fatalf("unexpected version semantics: %#v", report)
	}
}

func TestBuildSupportReportFiltersSupportLabels(t *testing.T) {
	report := buildSupportReport(supportOptions{Format: "json", Surface: "provider", Label: support.LabelExperimental})
	if report.SchemaVersion != supportReportSchemaVersion || report.Tool != toolName || report.Version != toolVersion {
		t.Fatalf("unexpected support report metadata: %#v", report)
	}
	if len(report.Entries) == 0 {
		t.Fatal("expected support entries")
	}
	for _, entry := range report.Entries {
		if entry.Surface != "provider" || entry.Label != support.LabelExperimental {
			t.Fatalf("unexpected filtered entry: %#v", entry)
		}
	}
	if report.Summary[support.LabelExperimental] != len(report.Entries) {
		t.Fatalf("unexpected support summary: %#v", report.Summary)
	}
}

func TestParseSupportOptions(t *testing.T) {
	opts, err := parseSupportOptions([]string{"--surface", "command", "--label", "compatibility", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Surface != "command" || opts.Label != "compatibility" || opts.Format != "json" {
		t.Fatalf("unexpected support options: %#v", opts)
	}
	for _, args := range [][]string{
		{"--surface", "package"},
		{"--label", "stable"},
		{"--format", "xml"},
	} {
		if _, err := parseSupportOptions(args); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
}

func TestSupportTextMentionsLabels(t *testing.T) {
	var builder strings.Builder
	printSupportText(&builder, buildSupportReport(supportOptions{Format: "text", Surface: "provider"}), false)
	out := builder.String()
	for _, want := range []string{"updev support", "supported_preview", "experimental", "homebrew", "linux"} {
		if !strings.Contains(out, want) {
			t.Fatalf("support text missing %q:\n%s", want, out)
		}
	}
}

func TestParseToolVersion(t *testing.T) {
	major, minor, patch := parseToolVersion("v1.2.3")
	if major != 1 || minor != 2 || patch != 3 {
		t.Fatalf("unexpected parsed version: %d.%d.%d", major, minor, patch)
	}
}

func TestVersionAliases(t *testing.T) {
	for _, alias := range []string{"--version", "-v"} {
		if !isVersionAlias(alias) {
			t.Fatalf("expected %s to be a version alias", alias)
		}
	}
	if isVersionAlias("--verbose") {
		t.Fatal("--verbose must not be a version alias")
	}
}

func TestCommandAliases(t *testing.T) {
	if normalizeListCommand("ls") != "list" {
		t.Fatal("expected ls to normalize to list")
	}
	if normalizeReadOnlyCommand("st") != "status" {
		t.Fatal("expected st to normalize to status")
	}
	if normalizeReadOnlyCommand("ck") != "check" {
		t.Fatal("expected ck to normalize to check")
	}
}

func TestAgentDocsRenderFromInjectedCanonicalDocs(t *testing.T) {
	previousSkill := embeddedAgentSkillDoc
	previousUsage := embeddedAgentUsageDoc
	defer func() {
		embeddedAgentSkillDoc = previousSkill
		embeddedAgentUsageDoc = previousUsage
	}()
	SetAgentDocs("# Skill\n\nUse this.", "# Usage\n\nRead-only first.")
	if got := renderAgentSkillDoc(false); !strings.Contains(got, "# Skill") || strings.Contains(got, "# Usage") {
		t.Fatalf("expected short skill doc only, got %q", got)
	}
	if got := renderAgentSkillDoc(true); !strings.Contains(got, "# Skill") || !strings.Contains(got, "# Usage") || !strings.Contains(got, "---") {
		t.Fatalf("expected full skill doc with usage, got %q", got)
	}
	if got := renderAgentUsageDoc(); !strings.Contains(got, "Read-only first.") {
		t.Fatalf("expected usage doc, got %q", got)
	}
	if got := runAgentSkill([]string{"--unknown"}); got != usageExitCode {
		t.Fatalf("expected usage exit for unknown skill option, got %d", got)
	}
}

func TestUsageErrorsReturn64(t *testing.T) {
	tests := []struct {
		name string
		run  func() int
	}{
		{name: "global option", run: func() int { return Run([]string{"--config"}) }},
		{name: "update parse", run: func() int { return Run([]string{"update", "--format", "xml"}) }},
		{name: "list parse", run: func() int { return Run([]string{"list", "--limit", "-1"}) }},
		{name: "unknown command", run: func() int { return Run([]string{"listt"}) }},
		{name: "legacy usage", run: func() int { return Run([]string{"legacy"}) }},
		{name: "skill parse", run: func() int { return runAgentSkill([]string{"--bad"}) }},
		{name: "help agent parse", run: func() int { return runAgentHelp([]string{"--bad"}) }},
		{name: "backends usage", run: func() int { return runBackends(nil) }},
		{name: "backends parse", run: func() int { return runBackends([]string{"plan", "--format", "xml"}) }},
		{name: "doctor usage", run: func() int { return runDoctor(nil) }},
		{name: "fix usage", run: func() int { return runFix(nil) }},
		{name: "security usage", run: func() int { return runSecurity(nil) }},
		{name: "security parse", run: func() int { return runSecurity([]string{"scan", "--scanner", "unknown"}) }},
		{name: "edit parse", run: func() int { return runEdit([]string{"--format", "xml"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.run(); got != usageExitCode {
				t.Fatalf("expected usage exit code %d, got %d", usageExitCode, got)
			}
		})
	}
}

func TestRunConsumesValueFlagsBeforeDispatchError(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name: "update",
			args: []string{
				"update",
				"--format", "json",
				"--root", "/tmp/update-root",
				"--inventory", "fast",
				"--security", "off",
				"--policy", "/tmp/update-policy.json",
				"--sentinel",
			},
			wantStderr: "unknown option: --sentinel",
		},
		{
			name: "list",
			args: []string{
				"list",
				"--format", "json",
				"--root", "/tmp/list-root",
				"--provider", "mise",
				"--kind", "tool",
				"--category", "runtime",
				"--status", "active",
				"--query", "node",
				"--limit", "5",
				"--sentinel",
			},
			wantStderr: "flag provided but not defined: -sentinel",
		},
		{
			name: "last",
			args: []string{
				"last",
				"--format", "json",
				"--section", "inventory",
				"--provider", "brew",
				"--status", "attention",
				"--query", "jq",
				"--sentinel",
			},
			wantStderr: "unknown option: --sentinel",
		},
		{
			name: "apply",
			args: []string{
				"apply", "brewfile",
				"--format", "json",
				"--root", "/tmp/apply-root",
				"--policy", "/tmp/apply-policy.json",
				"--sentinel",
			},
			wantStderr: "unknown option: --sentinel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
			code, stderr := captureRunStderr(t, func() int { return Run(tt.args) })
			if code != usageExitCode {
				t.Fatalf("expected usage exit code %d, got %d", usageExitCode, code)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestRunConsumesGlobalConfigValue(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", os.Getenv("UPDEV_CONFIG"))
	configPath := filepath.Join(t.TempDir(), "updev.toml")
	code, stderr := captureRunStderr(t, func() int {
		return Run([]string{"--config", configPath, "update", "--sentinel"})
	})
	if code != usageExitCode || !strings.Contains(stderr, "unknown option: --sentinel") {
		t.Fatalf("expected update parser sentinel after global option, code=%d stderr=%q", code, stderr)
	}
	if got := os.Getenv("UPDEV_CONFIG"); got != configPath {
		t.Fatalf("global --config value was not consumed: got %q want %q", got, configPath)
	}
}

func captureRunStderr(t *testing.T, run func() int) (int, string) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = write
	defer func() {
		os.Stderr = original
		_ = write.Close()
		_ = read.Close()
	}()
	code := run()
	os.Stderr = original
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return code, string(output)
}

func TestPrimaryV1CommandsDoNotDelegateToLegacy(t *testing.T) {
	for _, args := range [][]string{
		{"sync"},
		{"add", "demo"},
		{"remove", "demo"},
		{"edit", "--provider", "brew"},
		{"rollback"},
		{"backends", "plan"},
	} {
		if shouldDelegate(args) {
			t.Fatalf("expected %v to stay in Go", args)
		}
	}
	if shouldDelegate([]string{"--print-explicit-formulas"}) {
		t.Fatal("expected explicit formula helper to stay in Go")
	}
}

func TestParseReadOnlyOptionsManifestOnly(t *testing.T) {
	opts, err := parseOptions([]string{"--root", "/repo", "--manifest-only", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.root != "/repo" || !opts.manifestOnly || opts.format != "json" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestParseUpdateOptionsInteractiveFlags(t *testing.T) {
	opts, err := parseUpdateOptions([]string{"--interactive", "--no-interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.tui || !opts.noTUI {
		t.Fatalf("expected interactive flags, got %+v", opts)
	}
	opts, err = parseUpdateOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "text" || !opts.noTUI {
		t.Fatalf("expected --plain to force text and disable TUI, got %+v", opts)
	}
}

func TestParseLastReportOptions(t *testing.T) {
	opts, err := parseLastReportOptions([]string{"--section", "inventory", "--provider", "brew", "--status", "attention", "--query", "jq", "--details", "--interactive", "--no-interactive", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.section != "inventory" || opts.provider != "brew" || opts.status != "attention" || opts.query != "jq" || !opts.details || !opts.tui || !opts.noTUI || opts.format != "json" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseLastReportOptions([]string{"--section", "unknown"}); err == nil {
		t.Fatal("expected unsupported section to fail")
	}
	opts, err = parseLastReportOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "text" || !opts.noTUI {
		t.Fatalf("expected --plain to force text and disable TUI, got %+v", opts)
	}
}

func TestParseListOptionsDetailsAndLimit(t *testing.T) {
	opts, err := parseListOptions([]string{"--details", "--limit", "5", "--category", "runtime", "--interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.details || opts.limit != 5 || opts.category != "runtime" || !opts.tui {
		t.Fatalf("unexpected list options: %+v", opts)
	}
	if _, err := parseListOptions([]string{"--limit", "-1"}); err == nil {
		t.Fatal("expected negative limit to fail")
	}
	opts, err = parseListOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.format != "text" || !opts.noTUI {
		t.Fatalf("expected --plain to force text and disable TUI, got %+v", opts)
	}
}

func TestParseReadOnlyOptionsRefreshAndVSCode(t *testing.T) {
	opts, err := parseOptions([]string{"--refresh", "--include-vscode", "--format", "json", "--root", "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.refresh || !opts.includeVSCode || opts.format != "json" || opts.root != "/repo" {
		t.Fatalf("unexpected read-only options: %+v", opts)
	}
}

func TestParseSyncOptionsRefresh(t *testing.T) {
	opts, err := parseSyncOptions([]string{"--refresh", "--root", "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.refresh || opts.root != "/repo" {
		t.Fatalf("unexpected sync options: %#v", opts)
	}
}

func TestConfiguredRootResolvesRelativeToConfigFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".config", "updev")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	t.Setenv("UPDEV_CONFIG", configPath)
	want := filepath.Join(configDir, "..", "..", "dotfiles")
	got := configuredRoot(updevConfig{Sources: updevSourcesConfig{Root: stringPtr("../../dotfiles")}})
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("expected config-relative root, got %q want %q", got, want)
	}
}

func TestConfiguredRootIgnoresAuto(t *testing.T) {
	if got := configuredRoot(updevConfig{Sources: updevSourcesConfig{Root: stringPtr("auto")}}); got != "" {
		t.Fatalf("expected auto root to use default discovery, got %q", got)
	}
}

func TestDefaultRootUsesCWDWithoutMarkerClimb(t *testing.T) {
	t.Setenv("UPDEV_ROOT", "")
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
	t.Setenv("CHEZMOI_SOURCE_DIR", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "ripgrep"`), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "tools", "updev")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	if got := defaultRoot(); !sameFilesystemPath(t, got, nested) {
		t.Fatalf("expected cwd default without parent marker climb, got %q want %q", got, nested)
	}
}

func sameFilesystemPath(t *testing.T, left string, right string) bool {
	t.Helper()
	leftEval, leftErr := filepath.EvalSymlinks(left)
	rightEval, rightErr := filepath.EvalSymlinks(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return leftEval == rightEval
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func TestConfiguredEnvStringPrecedence(t *testing.T) {
	t.Setenv("UPDEV_TEST_URL", "")
	if got := configuredEnvString("https://default.example", "UPDEV_TEST_URL"); got != "https://default.example" {
		t.Fatalf("expected default URL, got %q", got)
	}
	t.Setenv("UPDEV_TEST_URL", " https://override.example ")
	if got := configuredEnvString("https://default.example", "UPDEV_TEST_URL"); got != "https://override.example" {
		t.Fatalf("expected trimmed env URL, got %q", got)
	}
	t.Setenv("UPDEV_TEST_URL", " ")
	if got := configuredEnvString("https://default.example", "UPDEV_TEST_URL"); got != "https://default.example" {
		t.Fatalf("expected blank env to keep default URL, got %q", got)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func TestConfiguredNonNegativeIntPrecedence(t *testing.T) {
	t.Setenv("UPDEV_TEST_THRESHOLD", "")
	if got := configuredNonNegativeInt(3, nil, "UPDEV_TEST_THRESHOLD"); got != 3 {
		t.Fatalf("expected default threshold, got %d", got)
	}
	if got := configuredNonNegativeInt(3, intPtr(5), "UPDEV_TEST_THRESHOLD"); got != 5 {
		t.Fatalf("expected config threshold, got %d", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", " 7 ")
	if got := configuredNonNegativeInt(3, intPtr(5), "UPDEV_TEST_THRESHOLD"); got != 7 {
		t.Fatalf("expected env threshold, got %d", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", "-1")
	if got := configuredNonNegativeInt(3, intPtr(5), "UPDEV_TEST_THRESHOLD"); got != 5 {
		t.Fatalf("expected invalid env to keep config threshold, got %d", got)
	}
	if got := configuredNonNegativeInt(3, intPtr(0), "UPDEV_TEST_THRESHOLD"); got != 0 {
		t.Fatalf("expected zero config threshold, got %d", got)
	}
}

func TestConfiguredNonNegativeFloatPrecedence(t *testing.T) {
	t.Setenv("UPDEV_TEST_THRESHOLD", "")
	if got := configuredNonNegativeFloat(2.5, nil, "UPDEV_TEST_THRESHOLD"); got != 2.5 {
		t.Fatalf("expected default threshold, got %f", got)
	}
	if got := configuredNonNegativeFloat(2.5, floatPtr(3.5), "UPDEV_TEST_THRESHOLD"); got != 3.5 {
		t.Fatalf("expected config threshold, got %f", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", " 4.5 ")
	if got := configuredNonNegativeFloat(2.5, floatPtr(3.5), "UPDEV_TEST_THRESHOLD"); got != 4.5 {
		t.Fatalf("expected env threshold, got %f", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", "-1")
	if got := configuredNonNegativeFloat(2.5, floatPtr(3.5), "UPDEV_TEST_THRESHOLD"); got != 3.5 {
		t.Fatalf("expected invalid env to keep config threshold, got %f", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", "NaN")
	if got := configuredNonNegativeFloat(2.5, floatPtr(3.5), "UPDEV_TEST_THRESHOLD"); got != 3.5 {
		t.Fatalf("expected NaN env to keep config threshold, got %f", got)
	}
	t.Setenv("UPDEV_TEST_THRESHOLD", "+Inf")
	if got := configuredNonNegativeFloat(2.5, floatPtr(3.5), "UPDEV_TEST_THRESHOLD"); got != 3.5 {
		t.Fatalf("expected infinite env to keep config threshold, got %f", got)
	}
	if got := configuredNonNegativeFloat(2.5, floatPtr(0), "UPDEV_TEST_THRESHOLD"); got != 0 {
		t.Fatalf("expected zero config threshold, got %f", got)
	}
}

func TestStartupProgressMessagesAreLocalized(t *testing.T) {
	if got := inventoryProgressMessage("ja", false); !strings.Contains(got, "読み込み中") {
		t.Fatalf("expected Japanese loading progress, got %q", got)
	}
	if got := safetyProgressMessage("ja"); !strings.Contains(got, "安全性") {
		t.Fatalf("expected Japanese safety progress, got %q", got)
	}
	if got := descriptionTranslationProgressMessage("ja"); !strings.Contains(got, "翻訳") {
		t.Fatalf("expected Japanese translation progress, got %q", got)
	}
	if got := securityScanProgressMessage("ja"); !strings.Contains(got, "セキュリティ") {
		t.Fatalf("expected Japanese security scan progress, got %q", got)
	}
	if got := syncProgressMessage("ja", true); !strings.Contains(got, "更新中") {
		t.Fatalf("expected Japanese sync progress, got %q", got)
	}
	if got := mutationProgressMessage("ja", "add"); !strings.Contains(got, "検証中") {
		t.Fatalf("expected Japanese mutation progress, got %q", got)
	}
}

func TestShouldRunInteractiveRequiresTextAndTTY(t *testing.T) {
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "text", false, false) {
		t.Fatal("expected non-TTY to skip interactive mode")
	}
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "json", true, false) {
		t.Fatal("expected JSON to skip interactive mode even when forced")
	}
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "text", true, false) {
		t.Fatal("expected explicit interactive mode to still require a TTY")
	}
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "text", true, true) {
		t.Fatal("expected disabled interactive mode to win over force")
	}
	t.Setenv("CI", "1")
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "text", false, false) {
		t.Fatal("expected CI to skip automatic interactive mode")
	}
	if shouldRunUpdevInteractive(strings.NewReader(""), &bytes.Buffer{}, "text", true, false) {
		t.Fatal("expected explicit interactive mode in CI to still require a TTY")
	}
}

func TestShouldRunListHubSkipsExplicitFocusedOutput(t *testing.T) {
	if shouldRunListHub(listOptions{format: "text", provider: "brew"}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected explicit provider filter to skip automatic list hub")
	}
	if shouldRunListHub(listOptions{format: "text", category: "runtime"}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected explicit category filter to skip automatic list hub")
	}
	if shouldRunListHub(listOptions{format: "text", tui: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected explicit interactive list to still require a TTY")
	}
	if shouldRunListHub(listOptions{format: "text", tui: true, provider: "brew"}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected explicit interactive list with filter to still require a TTY")
	}
	if shouldRunUpdateHub(updateOptions{format: "text", tui: true, noTUI: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected no-interactive to disable update hub")
	}
	if shouldRunUpdateHub(updateOptions{format: "json", tui: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected JSON update output to skip interactive hub")
	}
	plainUpdateOpts, err := parseUpdateOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	if shouldRunUpdateHub(plainUpdateOpts, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected --plain update output to skip interactive hub")
	}
	if shouldRunUpdateHub(updateOptions{format: "text", inventory: "legacy", tui: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected legacy inventory comparison to skip update hub")
	}
	if shouldRunLastReportHub(lastReportOptions{format: "text"}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected automatic last report hub to still require a TTY")
	}
	if shouldRunLastReportHub(lastReportOptions{format: "text", tui: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected explicit interactive last report to still require a TTY")
	}
	if shouldRunLastReportHub(lastReportOptions{format: "json", tui: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected JSON last report to skip interactive hub")
	}
	if shouldRunLastReportHub(lastReportOptions{format: "text", tui: true, noTUI: true}, strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("expected no-interactive to disable last report hub")
	}
}

func TestLastReportHubDefaultAction(t *testing.T) {
	tests := map[string]string{
		"updates":   updateHubActionUpdatesFilter,
		"security":  updateHubActionSecurity,
		"inventory": updateHubActionInventoryAll,
		"logs":      updateHubActionLogs,
		"full":      updateHubActionFull,
		"summary":   "",
	}
	for section, want := range tests {
		if got := lastReportHubDefaultAction(section); got != want {
			t.Fatalf("section %q default action = %q, want %q", section, got, want)
		}
	}
}

func TestUpdateHubActionAvailable(t *testing.T) {
	choices := []updevChoice{
		{Value: updateHubActionInventoryAll},
		{Value: updateHubActionSecurity},
	}
	if !updateHubActionAvailable(updateHubActionSecurity, choices) {
		t.Fatal("expected available action to be accepted")
	}
	if updateHubActionAvailable(updateHubActionBackends, choices) {
		t.Fatal("expected unavailable action to be rejected")
	}
	if updateHubActionAvailable("", choices) {
		t.Fatal("expected empty action to be rejected")
	}
}
