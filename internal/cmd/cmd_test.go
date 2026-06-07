package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/snapshot"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("UPDEV_LANG", "en")
	_ = os.Setenv("NO_COLOR", "1")
	if wd, err := os.Getwd(); err == nil {
		path := filepath.Clean(filepath.Join(wd, "..", "..", "mise.toml"))
		if _, err := os.Stat(path); err == nil {
			_ = os.Setenv("MISE_TRUSTED_CONFIG_PATHS", path)
		}
	}
	os.Exit(m.Run())
}

type fakeCommandRunner struct {
	mu      sync.Mutex
	result  runner.Result
	results map[string]runner.Result
	paths   map[string]error
	calls   [][]string
}

func (fake *fakeCommandRunner) LookPath(name string) (string, error) {
	if fake.paths != nil {
		if err, ok := fake.paths[name]; ok {
			if err != nil {
				return "", err
			}
			return "/fake/bin/" + name, nil
		}
	}
	return "/fake/bin/" + name, nil
}

func (fake *fakeCommandRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	call := append([]string{name}, args...)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls = append(fake.calls, call)
	if fake.results != nil {
		if result, ok := fake.results[strings.Join(call, "\x00")]; ok {
			return result
		}
	}
	return fake.result
}

func (fake *fakeCommandRunner) RunStreaming(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result {
	result := fake.Run(ctx, name, args...)
	if stdout != nil && result.Stdout != "" {
		_, _ = io.WriteString(stdout, result.Stdout)
	}
	if stderr != nil && result.Stderr != "" {
		_, _ = io.WriteString(stderr, result.Stderr)
	}
	return result
}

func testBackendCompatibleAssetName(prefix string) string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "darwin"
	}
	archName := runtime.GOARCH
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	return prefix + "-" + osName + "-" + archName + ".tar.gz"
}

type deadlineRecordingRunner struct {
	calls       int
	sawDeadline bool
	result      runner.Result
}

func (recording *deadlineRecordingRunner) Run(ctx context.Context, _ string, _ ...string) runner.Result {
	recording.calls++
	if _, ok := ctx.Deadline(); ok {
		recording.sawDeadline = true
	}
	return recording.result
}

func TestParseUpdateOptions(t *testing.T) {
	opts, err := parseUpdateOptions([]string{"--dry-run", "--format", "json", "--root", "/tmp/root", "--security", "strict", "--policy", "/tmp/policy.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.dryRun || opts.format != "json" || opts.root != "/tmp/root" || opts.security != "strict" || opts.policy != "/tmp/policy.json" {
		t.Fatalf("unexpected options: %+v", opts)
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
	if report.Major != 0 || report.Minor != 5 || report.Patch != 7 || report.Contract != "pre_stable" {
		t.Fatalf("unexpected version semantics: %#v", report)
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

func TestUsageErrorsReturn64(t *testing.T) {
	tests := []struct {
		name string
		run  func() int
	}{
		{name: "global option", run: func() int { return Run([]string{"--config"}) }},
		{name: "update parse", run: func() int { return Run([]string{"update", "--format", "xml"}) }},
		{name: "list parse", run: func() int { return Run([]string{"list", "--limit", "-1"}) }},
		{name: "legacy usage", run: func() int { return Run([]string{"legacy"}) }},
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

func TestParseReadOnlyOptionsManifestOnly(t *testing.T) {
	opts, err := parseOptions([]string{"--root", "/repo", "--manifest-only", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.root != "/repo" || !opts.manifestOnly || opts.format != "json" {
		t.Fatalf("unexpected options: %+v", opts)
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
	var out bytes.Buffer
	progress := startupProgress{enabled: true, w: &out, message: inventoryProgressMessage("ja", true)}
	progress.Start()
	progress.Done()
	if got := out.String(); !strings.Contains(got, "更新中") || !strings.Contains(got, "\033[2K") {
		t.Fatalf("expected localized progress write and clear, got %q", got)
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

func TestParseSyncOptionsRefresh(t *testing.T) {
	opts, err := parseSyncOptions([]string{"--refresh", "--root", "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.refresh || opts.root != "/repo" {
		t.Fatalf("unexpected sync options: %#v", opts)
	}
}

func TestShouldUseHomeBrewfileOnlyForDefaultRoot(t *testing.T) {
	t.Setenv("CHEZMOI_SOURCE_DIR", "/repo")
	if !shouldUseHomeBrewfile("/repo/") {
		t.Fatal("expected default root to use rendered home Brewfile")
	}
	if shouldUseHomeBrewfile("/tmp/repo") {
		t.Fatal("expected alternate root to use source Brewfile.tmpl")
	}
}

func TestSyncEntriesFromInventoryUsesV1Reasons(t *testing.T) {
	report := plan.Report{
		Providers: []plan.ProviderSummary{{Name: "brew", Unavailable: true}},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusMissing},
			{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusExtra},
			{Provider: "mise", Kind: "tool", Name: "go", Status: plan.StatusOK},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 3 {
		t.Fatalf("expected unavailable, missing, and extra entries, got %#v", entries)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Reason] = true
	}
	for _, reason := range []string{"unavailable", "missing", "extra"} {
		if !got[reason] {
			t.Fatalf("expected reason %s in %#v", reason, entries)
		}
	}
}

func TestSyncEntriesClassifyProviderMismatchAndActions(t *testing.T) {
	report := plan.Report{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "node", Status: plan.StatusMissing, Desired: true},
			{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusExtra, Live: true},
			{Provider: "brew", Kind: "cask", Name: "orphan-app", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	byName := map[string]syncEntry{}
	for _, entry := range entries {
		byName[entry.Provider+"/"+entry.Name] = entry
	}
	if entry := byName["brew/node"]; entry.Reason != "provider-mismatch" || entry.Action != "choose-provider" || entry.RelatedProvider != "mise" {
		t.Fatalf("expected brew node provider mismatch with related mise entry, got %#v", entry)
	}
	if entry := byName["mise/node"]; entry.Reason != "provider-mismatch" || entry.Action != "choose-provider" || entry.RelatedProvider != "brew" {
		t.Fatalf("expected mise node provider mismatch with related brew entry, got %#v", entry)
	}
	if entry := byName["brew/orphan-app"]; entry.Reason != "extra" || entry.Action != "adopt-remove-or-manual" || entry.Detail == "" {
		t.Fatalf("expected extra action guidance, got %#v", entry)
	}
}

func TestSyncEntriesClassifyManualCaskAsSkipped(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(`# macOS 手動管理アプリ

## ベンダー独自更新

| アプリ | 入手先 | 用途 |
|--------|--------|------|
| Evernote | 公式 | ノート |
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusDrift,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one skipped manual entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Reason != "skipped" || entry.Action != "manual-local-only" || !strings.Contains(entry.Detail, "docs/apps.md") {
		t.Fatalf("expected manual cask to be skipped with guidance, got %#v", entry)
	}
	if got := syncReportStatus(report, entries); got != plan.StatusOK {
		t.Fatalf("expected all-skipped sync report to stay ok, got %s", got)
	}
}

func TestProfileScopedExtraBecomesProfileMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{- if has "work" .profiles }}
# work - baseline macOS Homebrew entries, also included by personal profile
cask "ghostty"
{{- end }}
{{- if has "personal" .profiles }}
# personal - private-use desired entries
cask "warp"
{{- end }}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusDrift,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true},
		},
	}
	annotateProfileScopedExtras(&report, root)
	if got := report.Items[0].Category; got != "personal" {
		t.Fatalf("expected personal category, got %q in %#v", got, report.Items[0])
	}
	if !itemHasProfileMismatch(report.Items[0]) {
		t.Fatalf("expected profile mismatch detail, got %#v", report.Items[0])
	}
	if got := inventoryItemStatusLabel(report.Items[0]); got != "profile-mismatch" {
		t.Fatalf("expected profile-mismatch status label, got %q", got)
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one sync entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Reason != "profile-mismatch" || entry.Action != "switch-profile-or-remove" || entry.Category != "personal" {
		t.Fatalf("expected profile mismatch guidance, got %#v", entry)
	}
}

func TestMiseManifestHygieneAnnotatesInventory(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
go = "1.26.3"
"github:jnsahaj/lumen" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Name: "go", Status: plan.StatusOK},
		},
	}
	annotateMiseManifestIssues(&report, root)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift status for manifest issue, got %#v", report)
	}
	var issue plan.Item
	for _, item := range report.Items {
		if item.Provider == "mise" && item.Kind == "manifest" {
			issue = item
			break
		}
	}
	if issue.Name != "github:jnsahaj/lumen" || issue.Category != "github" || issue.Status != plan.StatusBlocked || !strings.Contains(issue.Detail, "latest is not allowed") {
		t.Fatalf("expected blocked mise manifest issue, got %#v", report.Items)
	}
	annotateMiseManifestIssues(&report, root)
	count := 0
	for _, item := range report.Items {
		if item.Provider == "mise" && item.Kind == "manifest" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected idempotent manifest annotation, got %#v", report.Items)
	}
}

func TestMiseManifestFixDryRunResolvesLatest(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(miseDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[tools]
"github:owner/tool" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	results := addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
		strings.Join([]string{"mise", "latest", "github:owner/tool"}, "\x00"): {Stdout: "1.2.3\n"},
	})
	results[strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00")] = runner.Result{Stdout: `{"minimum_release_age":{"value":"3d","type":"string","source":"/fake/mise/config.toml"}}`}
	fake := &fakeCommandRunner{results: results}
	report := buildMiseManifestFixReport(context.Background(), miseManifestFixOptions{root: root}, fake)
	if report.Status != plan.StatusDrift || !report.DryRun || len(report.Actions) != 1 {
		t.Fatalf("expected dry-run drift action, got %#v", report)
	}
	if !boolValue(report.MiseMinimumReleaseAge.Active) || report.MiseMinimumReleaseAge.Value != "3d" {
		t.Fatalf("expected active mise minimum_release_age evidence, got %#v", report.MiseMinimumReleaseAge)
	}
	if action := report.Actions[0]; action.Status != plan.StatusDrift || action.Resolved != "1.2.3" || action.Current != "latest" || !action.AgePolicyActive || action.AgePolicyValue != "3d" {
		t.Fatalf("unexpected fix action: %#v", action)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "1.2.3") {
		t.Fatalf("dry-run should not rewrite config: %s", data)
	}
}

func TestMiseManifestFixApplyRewritesLatest(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(miseDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[tools]
"github:owner/tool" = "latest" # keep comment
"npm:demo" = { version = "latest", os = ["macos"] }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(projectDir, "mise.toml")
	if err := os.WriteFile(projectPath, []byte(`[tools]
"aqua:owner/project" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "latest", "github:owner/tool"}, "\x00"):  {Stdout: "1.2.3\n"},
		strings.Join([]string{"mise", "latest", "npm:demo"}, "\x00"):           {Stdout: "4.5.6\n"},
		strings.Join([]string{"mise", "latest", "aqua:owner/project"}, "\x00"): {Stdout: "7.8.9\n"},
	}}
	report := buildMiseManifestFixReport(context.Background(), miseManifestFixOptions{root: root, apply: true}, fake)
	if report.Status != plan.StatusOK || report.DryRun || len(report.Actions) != 3 {
		t.Fatalf("expected applied ok report, got %#v", report)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"github:owner/tool" = "1.2.3" # keep comment`, `version = "4.5.6"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected rewritten config to contain %q:\n%s", want, text)
		}
	}
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectData), `"aqua:owner/project" = "7.8.9"`) {
		t.Fatalf("expected rewritten project config:\n%s", projectData)
	}
}

func TestSyncEntriesGiveTapSpecificGuidance(t *testing.T) {
	report := plan.Report{
		Items: []plan.Item{
			{Provider: "brew", Kind: "tap", Name: "webkaz/tap", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one tap entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Action != "adopt-or-untap" || !strings.Contains(entry.Detail, "brew untap webkaz/tap") {
		t.Fatalf("expected tap-specific untap guidance, got %#v", entry)
	}
}

func TestPrintSyncTextShowsCategoryMeaning(t *testing.T) {
	report := syncReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Entries: []syncEntry{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "orphan-app",
			Category: "personal",
			Reason:   "extra",
			Status:   plan.StatusExtra,
			Action:   "adopt-remove-or-manual",
			Detail:   "installed entry is unmanaged",
		}},
	}
	var out bytes.Buffer
	printSyncText(&out, report, false)
	text := out.String()
	for _, want := range []string{"categories: personal=1", "personal-only additions on top of work", "action", "adopt-remove-or-manual", "details"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected sync text to include %q:\n%s", want, text)
		}
	}
}

func TestPrintReadOnlyTextShowsCacheAndNoChanges(t *testing.T) {
	result := inventoryResult{
		Cached:    true,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "brew", Supported: true, Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "app", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	var out bytes.Buffer
	printReadOnlyText(&out, "status", result)
	text := out.String()
	for _, want := range []string{"updev status ok", "cache:", "categories", "personal=1", "changes", "no changes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected read-only text to include %q:\n%s", want, text)
		}
	}
}

func TestPrintReadOnlyTextUsesRichListCategoryCounts(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	rows := `{
  "mise": {
    "rows": [
      ["node", "24.15.0", "-", "inactive", "-", "Node runtime"],
      ["node", "24.16.0", "lts", "active", "/tmp/config.toml", "Node runtime"]
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "rows_cache.json"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	var out bytes.Buffer
	printReadOnlyText(&out, "status", result)
	text := out.String()
	for _, want := range []string{"categories", "runtime=2", "other categories are provider/backend groups", "changes", "no changes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected read-only text to include %q:\n%s", want, text)
		}
	}
}

func TestMutationReportAddsBrewfileEntryWithSnapshotAndDiff(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.SchemaVersion != mutationReportSchemaVersion || report.Status != plan.StatusOK || !report.Changed || report.Snapshot == nil || report.Diff == "" {
		t.Fatalf("expected successful mutation with snapshot and diff, got %#v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, "Brewfile.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `brew "jq"`) {
		t.Fatalf("expected Brewfile entry, got %s", data)
	}
}

func TestMutationReportHoldsAmbiguousBareName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action: "add",
		root:   root,
		name:   "jq",
	})
	if report.Status != plan.StatusHeld || report.Changed || report.Snapshot != nil {
		t.Fatalf("expected ambiguous bare name to hold without snapshot or write, got %#v", report)
	}
	if len(report.Candidates) < 2 {
		t.Fatalf("expected provider candidates, got %#v", report.Candidates)
	}
}

func TestMutationReportSurfacesValidationError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	fakeMise := filepath.Join(binDir, "mise")
	if err := os.WriteFile(fakeMise, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const scannerOverflowTokenBytes = 70 * 1024 // bufio.Scanner default token limit is 64 KiB.
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"+strings.Repeat("a", scannerOverflowTokenBytes)+" = \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.Status != plan.StatusError || report.Validation.Status != plan.StatusError {
		t.Fatalf("expected validation failure to surface in report status, got %#v", report)
	}
	if !report.Changed || report.Snapshot == nil || report.RollbackCommand == "" {
		t.Fatalf("expected successful write to remain rollbackable despite validation failure, got %#v", report)
	}
}

func TestRollbackReportRestoresLatestSnapshot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "Brewfile.tmpl")
	if err := os.WriteFile(path, []byte("brew \"git\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Create(root, []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("brew \"jq\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildRollbackReport(rollbackOptions{root: root})
	if report.Status != plan.StatusOK || report.Token == "" || len(report.RestoredFiles) != 1 {
		t.Fatalf("expected latest rollback to succeed, got %#v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "brew \"git\"\n" {
		t.Fatalf("expected restored manifest, got %q", data)
	}
}

func TestBackendPlanReportsReadOnlyConvergenceFindings(t *testing.T) {
	compatibleAsset := testBackendCompatibleAssetName("demo")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`brew "git"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "ripgrep"
brew "rtk"
brew "git"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
ripgrep = "15.1.0"
"cargo:fd-find" = { version = "10.4.2", os = ["macos/x64"] }
"aqua:sharkdp/fd" = { version = "10.4.2", os = ["macos/arm64", "linux"] }
"cargo:git-delta" = { version = "0.19.2", os = ["macos/x64"] }
"aqua:dandavison/delta" = { version = "0.19.2", os = ["macos/x64", "linux"] }
"cargo:broot" = "1.56.4"
"cargo:demo-tool" = "0.1.0"
"cargo:local-build" = "0.2.0"
"npm:@scope/demo-cli" = "2.0.0"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{
		paths: map[string]error{
			"rg":          nil,
			"rtk":         nil,
			"fd":          fmt.Errorf("missing"),
			"delta":       nil,
			"demo-tool":   nil,
			"local-build": nil,
			"demo-cli":    nil,
		},
		results: map[string]runner.Result{
			"brew\x00info\x00--json=v2\x00git\x00rtk":                                          {Stdout: `{"formulae":[{"name":"rtk","urls":{"stable":{"url":"https://github.com/rtk-ai/rtk/archive/refs/tags/v0.42.1.tar.gz"},"head":{"url":"https://github.com/rtk-ai/rtk.git"}}}],"casks":[]}`},
			"cargo\x00info\x00demo-tool":                                                       {Stdout: "demo-tool # CLI\nrepository: https://github.com/example/demo-tool\n"},
			"cargo\x00info\x00local-build":                                                     {Stdout: "local-build # CLI\nrepository: https://github.com/example/local-build\n"},
			"npm\x00view\x00@scope/demo-cli\x00repository\x00homepage\x00--json":               {Stdout: `{"repository":{"url":"git+https://github.com/example/demo-cli.git"},"homepage":"https://example.com"}`},
			"gh\x00api\x00repos/Canop/broot/releases/latest\x00--jq\x00.assets[].name":         {Stdout: compatibleAsset},
			"gh\x00api\x00repos/rtk-ai/rtk/releases/latest\x00--jq\x00.assets[].name":          {Stdout: compatibleAsset},
			"gh\x00api\x00repos/example/demo-tool/releases/latest\x00--jq\x00.assets[].name":   {Stdout: compatibleAsset},
			"gh\x00api\x00repos/example/local-build/releases/latest\x00--jq\x00.assets[].name": {},
			"gh\x00api\x00repos/example/demo-cli/releases/latest\x00--jq\x00.assets[].name":    {Stdout: compatibleAsset},
		},
	}
	report := buildBackendPlanReportWithRunner(context.Background(), backendOptions{command: "plan", root: root}, fake)
	if report.SchemaVersion != backendPlanReportSchemaVersion || report.Status != plan.StatusDrift {
		t.Fatalf("expected drift backend plan report, got %#v", report)
	}
	types := map[string]bool{}
	for _, finding := range report.Findings {
		types[finding.Type] = true
		switch finding.Name {
		case "ripgrep":
			if finding.CommandStatus != "on-path" || !containsString(finding.CommandNames, "rg") || finding.RecommendedTier != "mise/core" || finding.PreferenceRank != 1 {
				t.Fatalf("expected ripgrep command verification, got %#v", finding)
			}
		case "rtk":
			if finding.Type != "homebrew-to-mise-candidate" || finding.RecommendedName != "github:rtk-ai/rtk" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || finding.CommandStatus != "on-path" || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" {
				t.Fatalf("expected rtk GitHub backend candidate from Homebrew metadata with platform evidence, got %#v", finding)
			}
		case "cargo:fd-find":
			if finding.CommandStatus != "missing" || !containsString(finding.CurrentOS, "macos/x64") || !containsString(finding.RecommendedOS, "macos/arm64") || finding.RecommendedTier != "mise/aqua" || finding.PreferenceRank != 2 {
				t.Fatalf("expected fd command and OS-condition evidence, got %#v", finding)
			}
		case "cargo:git-delta":
			if finding.RecommendedName != "aqua:dandavison/delta" || !finding.RewriteAllowed || !containsString(finding.CurrentOS, "macos/x64") || !containsString(finding.RecommendedOS, "macos/x64") {
				t.Fatalf("expected delta recommendation to be safely removable with covered OS evidence, got %#v", finding)
			}
		case "cargo:broot":
			if finding.RecommendedName != "github:Canop/broot" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 {
				t.Fatalf("expected broot GitHub backend recommendation, got %#v", finding)
			}
		case "cargo:demo-tool":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/demo-tool" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" || !containsString(finding.ReleaseAssetMatches, compatibleAsset) {
				t.Fatalf("expected cargo metadata GitHub backend candidate with platform evidence, got %#v", finding)
			}
		case "cargo:local-build":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/local-build" || finding.ReleaseAssetStatus != "no-assets" || finding.Confidence != "low" || !strings.Contains(finding.Action, "local cargo build") {
				t.Fatalf("expected cargo metadata candidate without assets to preserve local build, got %#v", finding)
			}
		case "npm:@scope/demo-cli":
			if finding.Type != "mise-backend-candidate" || finding.RecommendedName != "github:example/demo-cli" || finding.RecommendedTier != "mise/github" || finding.PreferenceRank != 3 || !containsString(finding.CommandNames, "demo-cli") || finding.RecommendationKind != "candidate" || finding.ReleaseAssetStatus != "compatible" {
				t.Fatalf("expected npm metadata GitHub backend candidate with platform evidence, got %#v", finding)
			}
		}
	}
	if !types["homebrew-to-mise"] || !types["homebrew-to-mise-candidate"] || !types["mise-backend-rewrite"] || !types["mise-backend-candidate"] {
		t.Fatalf("expected brew and mise convergence findings, got %#v", report.Findings)
	}
	rows := backendDetailRows(report)
	if len(rows) != len(report.Findings) || !strings.Contains(strings.Join(rows[0].Metadata, " "), "command status:") || !strings.Contains(strings.Join(rows[0].Metadata, " "), "preference:") || !strings.Contains(strings.Join(rows[0].Metadata, " "), "recommendation kind:") {
		t.Fatalf("expected backend detail rows to expose command evidence, got %#v", rows)
	}
	sections := backendToolSections(report)
	sectionTitles := []string{}
	for _, section := range sections {
		sectionTitles = append(sectionTitles, section.Title)
	}
	for _, want := range []string{"backend / homebrew-to-mise", "backend / mise-backend-rewrite", "backend / mise-backend-candidate"} {
		if !containsString(sectionTitles, want) {
			t.Fatalf("expected backend grouped table sections to include %q, got %#v", want, sectionTitles)
		}
	}
	actionRows := map[string]detailBrowserRow{}
	for _, row := range rows {
		actionRows[row.Title] = row
	}
	if len(actionRows["cargo:broot -> github:Canop/broot"].Actions) != 1 {
		t.Fatalf("expected simple mise rewrite to expose an action, got %#v", actionRows["cargo:broot -> github:Canop/broot"].Actions)
	}
	if len(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions) != 1 || !strings.Contains(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions[0].Value, "remove-mise") {
		t.Fatalf("expected covered duplicate backend to expose remove action, got %#v", actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions)
	}
	if len(actionRows["ripgrep -> ripgrep"].Actions) != 1 || !strings.Contains(actionRows["ripgrep -> ripgrep"].Actions[0].Value, "remove-brew") {
		t.Fatalf("expected duplicated Homebrew/mise ownership to expose Brewfile removal action, got %#v", actionRows["ripgrep -> ripgrep"].Actions)
	}
	if len(actionRows["cargo:demo-tool -> github:example/demo-tool"].Actions) != 0 {
		t.Fatalf("expected metadata-inferred rewrite to remain review-only, got %#v", actionRows["cargo:demo-tool -> github:example/demo-tool"].Actions)
	}
	if len(actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Actions) != 0 {
		t.Fatalf("expected already-desired rewrite to remain review-only, got %#v", actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Actions)
	}
	if !strings.Contains(strings.Join(actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Metadata, " "), "review-only") {
		t.Fatalf("expected uncovered OS conditions to explain review-only applyability, got %#v", actionRows["cargo:fd-find -> aqua:sharkdp/fd"].Metadata)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["cargo:broot -> github:Canop/broot"].Actions[0].Value); !ok || action != "rewrite-mise" || current != "cargo:broot" || recommended != "github:Canop/broot" {
		t.Fatalf("unexpected backend action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["cargo:git-delta -> aqua:dandavison/delta"].Actions[0].Value); !ok || action != "remove-mise" || current != "cargo:git-delta" || recommended != "aqua:dandavison/delta" {
		t.Fatalf("unexpected backend remove action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
	if action, current, recommended, ok := parseBackendDetailAction(actionRows["ripgrep -> ripgrep"].Actions[0].Value); !ok || action != "remove-brew" || current != "brew:ripgrep" || recommended != "ripgrep" {
		t.Fatalf("unexpected backend Brewfile removal action parse: action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}
}

func TestBackendPreferenceTiersHonorConfiguredOrder(t *testing.T) {
	config := updevConfig{Backends: updevBackendsConfig{PreferenceOrder: []string{"store/native", "mise/github", "linux/apt"}}}
	tiers := backendPreferenceTiersWithConfig(config)
	if len(tiers) < 5 {
		t.Fatalf("expected configured and default tiers, got %#v", tiers)
	}
	for index, tier := range tiers {
		if tier.Rank != index+1 {
			t.Fatalf("expected ranks to be recomputed, got %#v", tiers)
		}
	}
	if tiers[0].Label != "store/native" || tiers[0].Provider != "mas" {
		t.Fatalf("expected store/native first with default provider mapping, got %#v", tiers[0])
	}
	if tiers[1].Label != "mise/github" || tiers[1].Provider != "mise" || tiers[1].Backend != "github" {
		t.Fatalf("expected mise/github second, got %#v", tiers[1])
	}
	if tiers[2].Label != "linux/apt" || tiers[2].Provider != "linux" || tiers[2].Backend != "apt" {
		t.Fatalf("expected unknown provider/backend label to be preserved, got %#v", tiers[2])
	}
	if tiers[3].Label != "mise/core" {
		t.Fatalf("expected unspecified defaults to remain after configured tiers, got %#v", tiers[:5])
	}
}

func TestBackendPreferenceTiersExcludeDeprecatedMiseBackendsByDefault(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	tiers := backendPreferenceTiersWithConfig(updevConfig{})
	for _, tier := range tiers {
		switch tier.Label {
		case "mise/ubi", "mise/vfox", "mise/asdf":
			t.Fatalf("expected deprecated or legacy backend to stay out of defaults, got %#v", tier)
		}
	}
	ubi := backendPreferenceTierFor("mise", "ubi:owner/repo")
	if ubi.Label != "mise/ubi" || ubi.Rank != 90 {
		t.Fatalf("expected existing ubi backend to remain recognized as deprecated, got %#v", ubi)
	}
	configured := backendPreferenceTiersWithConfig(updevConfig{Backends: updevBackendsConfig{PreferenceOrder: []string{"mise/asdf"}}})
	if configured[0].Label != "mise/asdf" || configured[0].Rank != 1 {
		t.Fatalf("expected explicit deprecated backend override to be honored, got %#v", configured[0])
	}
}

func TestBackendDoctorOnlyReportsMiseBackendFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "ripgrep"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
"cargo:fd-find" = "10.4.2"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildBackendPlanReportWithRunner(context.Background(), backendOptions{command: "doctor", root: root}, &fakeCommandRunner{paths: map[string]error{"fd": nil}})
	if len(report.Findings) != 1 || report.Findings[0].Type != "mise-backend-rewrite" {
		t.Fatalf("expected doctor to report only mise backend rewrites, got %#v", report.Findings)
	}
	if report.Findings[0].CommandStatus != "on-path" {
		t.Fatalf("expected doctor to verify candidate command, got %#v", report.Findings[0])
	}
}

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
			"brew\x00--version":                 {Stdout: "Homebrew 4.5.0"},
			"brew\x00outdated\x00--json=v2":     {Stdout: `{"formulae":[],"casks":[]}`},
			"mise\x00--version":                 {Stdout: "2026.5.18"},
			"mise\x00ls\x00--current\x00--json": {Stdout: `{}`},
		}),
	}
	report := buildDependencyContractReport(context.Background(), dependencyOptions{command: "dependencies", root: "/repo"}, fake)
	if report.SchemaVersion != dependencyContractReportSchemaVersion || report.Status != plan.StatusOK {
		t.Fatalf("expected ok dependency contract report, got %#v", report)
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

func TestDependencyContractReportAllowsInactiveMiseMinimumReleaseAge(t *testing.T) {
	results := addMiseMinimumReleaseAgeFakeResults(map[string]runner.Result{
		"brew\x00--version":                 {Stdout: "Homebrew 4.5.0"},
		"brew\x00outdated\x00--json=v2":     {Stdout: `{"formulae":[],"casks":[]}`},
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
			"brew\x00--version":                 {Stdout: "Homebrew 4.5.0"},
			"brew\x00outdated\x00--json=v2":     {Stdout: `{"formulae":[],"casks":[]}`},
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
			"brew\x00--version":                 {Stdout: "Homebrew 4.5.0"},
			"brew\x00outdated\x00--json=v2":     {Stdout: `{"formulae":[]}`},
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
			"brew\x00--version":                 {Stdout: "Homebrew 4.5.0"},
			"brew\x00outdated\x00--json=v2":     {Stdout: `{"formulae":[],"casks":[]}`},
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

func TestSimpleUnifiedDiffKeepsDeletionFocused(t *testing.T) {
	diff := simpleUnifiedDiff("Brewfile.tmpl", "{{ if has \"personal\" .profiles }}\nbrew \"jq\"\n{{ end }}\n", "{{ if has \"personal\" .profiles }}\n{{ end }}\n")
	if strings.Contains(diff, "+{{ end }}") {
		t.Fatalf("expected deletion-only diff to keep common closing line, got:\n%s", diff)
	}
	if !strings.Contains(diff, "-brew \"jq\"") {
		t.Fatalf("expected deleted brew line, got:\n%s", diff)
	}
}

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

[inventory]
state_dir = "~/.local/state/updev/inventory"
overrides = ".config/updev/inventory-overrides.toml"

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
	if config.Inventory.StateDir == nil || *config.Inventory.StateDir != "~/.local/state/updev/inventory" {
		t.Fatalf("expected inventory state dir, got %#v", config.Inventory)
	}
	if config.Inventory.Overrides == nil || *config.Inventory.Overrides != ".config/updev/inventory-overrides.toml" {
		t.Fatalf("expected inventory overrides path, got %#v", config.Inventory)
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
Upgrading usage
3.3.0 -> 3.4.0
Upgrading mise
2026.5.16 -> 2026.5.18
Upgrading cursor
3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	want := []string{
		"usage 3.3.0 -> 3.4.0",
		"mise 2026.5.16 -> 2026.5.18",
		"cursor 3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768",
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

func TestSecurityPostureStatusTreatsReviewAsHeld(t *testing.T) {
	got := securityPostureStatus(plan.StatusOK, []githubPosture{
		{Provider: "mise", Name: "github:owner/tool", Repository: "owner/tool", Decision: "review", Reason: "repository is archived"},
	}, nil, nil, nil, nil, nil)
	if got != plan.StatusHeld {
		t.Fatalf("expected review posture to hold scan status, got %s", got)
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

func TestPrintSecurityTextIncludesNativeAuditAttention(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Sources: []string{"provider-native-audit"},
		Audits: []nativeAudit{{
			Ecosystem: "npm",
			Tool:      "npm",
			Status:    plan.StatusUnavailable,
			Decision:  "review",
			Reason:    "npm audit does not support globals",
			Error:     "EAUDITGLOBAL",
		}},
	}, false)
	got := buffer.String()
	if !strings.Contains(got, "native audits") || !strings.Contains(got, "unavailable") || !strings.Contains(got, "globals") || !strings.Contains(got, "EAUDITGLOBAL") {
		t.Fatalf("expected native audit attention in text output, got %q", got)
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

func TestRunNPMNativeAuditReportsGlobalUnsupported(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"error":{"code":"EAUDITGLOBAL","summary":"npm audit does not support globals"}}`,
	}}
	audit := runNPMNativeAudit(context.Background(), fake)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Reason, "globals") {
		t.Fatalf("expected unsupported npm global audit, got %#v", audit)
	}
}

func TestRunNPMNativeAuditReportsVulnerabilities(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runNPMNativeAudit(context.Background(), fake)
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held npm audit, got %#v", audit)
	}
}

func TestRunNPMLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "package-lock.json")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runNPMLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "npm" || audit.Target != lockfile {
		t.Fatalf("expected npm project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held npm lockfile audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--prefix") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected npm --prefix audit call, got %#v", fake.calls)
	}
}

func TestRunPNPMLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "pnpm-lock.yaml")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runPNPMLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "pnpm" || audit.Target != lockfile {
		t.Fatalf("expected pnpm project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held pnpm audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--dir") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pnpm --dir audit call, got %#v", fake.calls)
	}
}

func TestRunBunLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "bun.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"advisories":[{"id":"GHSA-test"}]}`,
	}}
	audit := runBunLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "bun" || audit.Target != lockfile {
		t.Fatalf("expected bun project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 1 {
		t.Fatalf("expected held bun audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--cwd") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected bun --cwd audit call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsOnlyMatchingEcosystem(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	root := t.TempDir()
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "npm"}}, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 0 {
		t.Fatalf("expected npm native audit to be skipped by ecosystem filter, got %#v", audits)
	}
	audits = nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "npm"}}, securityOptions{root: root, ecosystem: "npm"})
	if len(audits) != 1 || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected npm native audit, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesRunsProjectLockfileAudits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bun.lock"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "npm"})
	if len(audits) != 3 {
		t.Fatalf("expected npm, pnpm, and bun project audits, got %#v", audits)
	}
	if audits[0].Tool != "npm" || audits[0].Target != filepath.Join(root, "package-lock.json") {
		t.Fatalf("expected npm lockfile audit, got %#v", audits)
	}
	if audits[1].Tool != "pnpm" || audits[1].Target != filepath.Join(root, "pnpm-lock.yaml") {
		t.Fatalf("expected pnpm lockfile audit, got %#v", audits)
	}
	if audits[2].Tool != "bun" || audits[2].Target != filepath.Join(root, "bun.lock") {
		t.Fatalf("expected bun lockfile audit, got %#v", audits)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("expected three project audit calls, got %#v", fake.calls)
	}
	focusedFake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	focusedAudits := nativeAuditsFromPackages(context.Background(), focusedFake, nil, securityOptions{root: root, ecosystem: "npm", provider: "brew"})
	if len(focusedAudits) != 0 || len(focusedFake.calls) != 0 {
		t.Fatalf("expected brew provider filter to skip project audits, got audits=%#v calls=%#v", focusedAudits, focusedFake.calls)
	}
	projectFake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	projectAudits := nativeAuditsFromPackages(context.Background(), projectFake, nil, securityOptions{root: root, ecosystem: "npm", provider: "project"})
	if len(projectAudits) != 3 || len(projectFake.calls) != 3 {
		t.Fatalf("expected project provider filter to run project audits, got audits=%#v calls=%#v", projectAudits, projectFake.calls)
	}
}

func TestRunCargoNativeAuditReportsMissingCommand(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   101,
		Stderr: "error: no such command: `audit`",
	}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "no such command") {
		t.Fatalf("expected unavailable cargo audit, got %#v", audit)
	}
}

func TestRunCargoNativeAuditReportsVulnerabilities(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"vulnerabilities":{"found":true,"count":2,"list":[{},{}]}}`,
	}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}})
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held cargo audit, got %#v", audit)
	}
}

func TestRunCargoNativeAuditReportsMissingBinaryContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CARGO_HOME", filepath.Join(home, ".cargo"))
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", Package: "fd-find"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || len(fake.calls) != 0 {
		t.Fatalf("expected unavailable cargo audit without running command, got %#v calls=%#v", audit, fake.calls)
	}
}

func TestRunCargoNativeAuditUsesCargoHomeBinary(t *testing.T) {
	home := t.TempDir()
	cargoBin := filepath.Join(home, "cargo-home", "bin")
	if err := os.MkdirAll(cargoBin, 0o700); err != nil {
		t.Fatal(err)
	}
	fdPath := filepath.Join(cargoBin, "fd")
	if err := os.WriteFile(fdPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(home, "empty-home"))
	t.Setenv("CARGO_HOME", filepath.Join(home, "cargo-home"))
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", Package: "fd-find", BinaryName: "fd,fd-find", PathState: "not-found"}})
	if audit.Status != plan.StatusOK || len(fake.calls) != 1 {
		t.Fatalf("expected cargo audit to use CARGO_HOME binary, got %#v calls=%#v", audit, fake.calls)
	}
	if !containsString(fake.calls[0], fdPath) {
		t.Fatalf("expected cargo audit command to include %s, got %#v", fdPath, fake.calls[0])
	}
}

func TestRunCargoProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"vulnerabilities":{"found":true,"count":2,"list":[{},{}]}}`,
	}}
	audit := runCargoProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "crates.io" || audit.Tool != "cargo-audit" || audit.Target != lockfile {
		t.Fatalf("expected Cargo project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held Cargo project audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "bash" || !containsString(fake.calls[0], root) {
		t.Fatalf("expected bash-wrapped cargo audit call, got %#v", fake.calls)
	}
}

func TestRunCargoProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   101,
		Stderr: "error: no such command: `audit`",
	}}
	audit := runCargoProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "no such command") {
		t.Fatalf("expected unavailable Cargo project audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsCargoEcosystem(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}}, securityOptions{ecosystem: "crates.io"})
	if len(audits) != 1 || audits[0].Ecosystem != "crates.io" || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected cargo native audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "bin") || !containsString(fake.calls[0], "/tmp/fd") {
		t.Fatalf("expected cargo audit bin call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsCargoProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	if err := os.WriteFile(lockfile, []byte("[[package]]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "crates.io"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "cargo-audit" || audits[0].Target != lockfile {
		t.Fatalf("expected Cargo project native audit, got %#v", audits)
	}
}

func TestRunPyPINativeAuditReportsMissingCommand(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable pip-audit, got %#v", audit)
	}
}

func TestRunPyPINativeAuditReportsVulnerabilities(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	sitePackages := makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"frogmouth","version":"0.9.2","vulns":[{"id":"PYSEC-test"},{"id":"GHSA-test"}]}]}`,
	}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held pip-audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--path") || !containsString(fake.calls[0], sitePackages) {
		t.Fatalf("expected pip-audit path call, got %#v", fake.calls)
	}
}

func TestRunPyPINativeAuditReportsMissingPipxContext(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || len(fake.calls) != 0 {
		t.Fatalf("expected unavailable pip-audit without paths, got %#v calls=%#v", audit, fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPyPIEcosystem(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}}, securityOptions{root: t.TempDir(), ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Ecosystem != "PyPI" || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected PyPI native audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "pip-audit" {
		t.Fatalf("expected pip-audit call, got %#v", fake.calls)
	}
}

func TestRunNativeAuditTasksPreservesTaskOrder(t *testing.T) {
	audits := runNativeAuditTasks([]func() nativeAudit{
		func() nativeAudit {
			time.Sleep(5 * time.Millisecond)
			return nativeAudit{Tool: "slow"}
		},
		func() nativeAudit {
			return nativeAudit{Tool: "fast"}
		},
	})
	if len(audits) != 2 || audits[0].Tool != "slow" || audits[1].Tool != "fast" {
		t.Fatalf("expected native audit task order to be stable, got %#v", audits)
	}
}

func TestRunPythonProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	sitePackages := filepath.Join(t.TempDir(), ".venv", "lib", "python3.12", "site-packages")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonProjectNativeAudit(context.Background(), fake, sitePackages)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != sitePackages {
		t.Fatalf("expected Python project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python project audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--path") || !containsString(fake.calls[0], sitePackages) {
		t.Fatalf("expected pip-audit --path call, got %#v", fake.calls)
	}
}

func TestRunPythonRequirementsNativeAuditReportsVulnerabilities(t *testing.T) {
	requirements := filepath.Join(t.TempDir(), "requirements.txt")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonRequirementsNativeAudit(context.Background(), fake, requirements)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != requirements {
		t.Fatalf("expected Python requirements audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python requirements audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--requirement") || !containsString(fake.calls[0], requirements) {
		t.Fatalf("expected pip-audit --requirement call, got %#v", fake.calls)
	}
}

func TestRunPythonLockedProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	pyproject := filepath.Join(root, "pyproject.toml")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonLockedProjectNativeAudit(context.Background(), fake, root, pyproject)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != pyproject {
		t.Fatalf("expected Python locked project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python locked project audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--locked") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pip-audit --locked project call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPythonProjectAudit(t *testing.T) {
	root := t.TempDir()
	sitePackages := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(sitePackages, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Target != sitePackages {
		t.Fatalf("expected Python project native audit, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesRunsPythonRequirementsAudits(t *testing.T) {
	root := t.TempDir()
	requirements := filepath.Join(root, "requirements.txt")
	devRequirements := filepath.Join(root, "requirements", "dev.txt")
	if err := os.WriteFile(requirements, []byte("requests==2.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(devRequirements), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devRequirements, []byte("pytest==8.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 2 {
		t.Fatalf("expected two Python requirements audits, got %#v", audits)
	}
	if audits[0].Target != requirements || audits[1].Target != devRequirements {
		t.Fatalf("expected sorted requirements audit targets, got %#v", audits)
	}
	if len(fake.calls) != 2 || !containsString(fake.calls[0], "--requirement") || !containsString(fake.calls[1], "--requirement") {
		t.Fatalf("expected pip-audit requirement calls, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPythonLockedProjectAudit(t *testing.T) {
	root := t.TempDir()
	pyproject := filepath.Join(root, "pyproject.toml")
	if err := os.WriteFile(pyproject, []byte("[project]\nname = \"demo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Target != pyproject {
		t.Fatalf("expected Python locked project audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--locked") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pip-audit --locked project call, got %#v", fake.calls)
	}
}

func TestProjectPythonLockedAuditTargetUsesPylock(t *testing.T) {
	root := t.TempDir()
	pylock := filepath.Join(root, "pylock.dev.toml")
	if err := os.WriteFile(pylock, []byte("[[packages]]\nname = \"demo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := projectPythonLockedAuditTarget(root); got != pylock {
		t.Fatalf("expected pylock target, got %q", got)
	}
}

func TestRunGoProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 3,
		Stdout: `{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3"}}
{"finding":{"osv":"GO-2026-0001"}}
{"finding":{"osv":"GO-2026-0002"}}`,
	}}
	audit := runGoProjectNativeAudit(context.Background(), fake, root, module)
	if audit.Provider != "project" || audit.Ecosystem != "Go" || audit.Tool != "govulncheck" || audit.Target != module {
		t.Fatalf("expected Go project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held govulncheck audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "govulncheck" || !containsString(fake.calls[0], "-format=json") || !containsString(fake.calls[0], filepath.Join(root, "...")) {
		t.Fatalf("expected govulncheck JSON call, got %#v", fake.calls)
	}
}

func TestRunGoProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runGoProjectNativeAudit(context.Background(), fake, root, module)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable govulncheck audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsGoProjectAudit(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	if err := os.WriteFile(module, []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "go"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "govulncheck" || audits[0].Target != module {
		t.Fatalf("expected Go project native audit, got %#v", audits)
	}
}

func TestRunComposerProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"advisories":{"vendor/package":[{"advisoryId":"PKSA-test-1"},{"advisoryId":"PKSA-test-2"}]}}`,
	}}
	audit := runComposerProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "Packagist" || audit.Tool != "composer" || audit.Target != lockfile {
		t.Fatalf("expected Composer project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held Composer audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "composer" || !containsString(fake.calls[0], "--working-dir") || !containsString(fake.calls[0], root) || !containsString(fake.calls[0], "--locked") {
		t.Fatalf("expected composer audit call, got %#v", fake.calls)
	}
}

func TestRunComposerProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runComposerProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable Composer audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsComposerProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	if err := os.WriteFile(lockfile, []byte(`{"packages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "packagist"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "composer" || audits[0].Target != lockfile {
		t.Fatalf("expected Composer project native audit, got %#v", audits)
	}
}

func TestRunBundlerProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	lockfile := filepath.Join(t.TempDir(), "Gemfile.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"results":[{"gem":"rails","advisory":{"id":"CVE-test-1"}},{"gem":"rack","advisory":{"id":"CVE-test-2"}}]}`,
	}}
	audit := runBundlerProjectNativeAudit(context.Background(), fake, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "RubyGems" || audit.Tool != "bundle-audit" || audit.Target != lockfile {
		t.Fatalf("expected Bundler project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held bundle-audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "bundle-audit" || !containsString(fake.calls[0], "--gemfile") || !containsString(fake.calls[0], lockfile) {
		t.Fatalf("expected bundle-audit JSON call, got %#v", fake.calls)
	}
}

func TestRunBundlerProjectNativeAuditReportsMissingCommand(t *testing.T) {
	lockfile := filepath.Join(t.TempDir(), "Gemfile.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runBundlerProjectNativeAudit(context.Background(), fake, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable Bundler audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsBundlerProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Gemfile.lock")
	if err := os.WriteFile(lockfile, []byte("GEM\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "rubygems"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "bundle-audit" || audits[0].Target != lockfile {
		t.Fatalf("expected Bundler project native audit, got %#v", audits)
	}
}

func TestRunDotnetProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Demo.sln")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"projects":[{"frameworks":[{"topLevelPackages":[{"vulnerabilities":[{"severity":"High"}]}],"transitivePackages":[{"vulnerabilities":[{"severity":"Moderate"},{"severity":"Low"}]}]}]}]}`,
	}}
	audit := runDotnetProjectNativeAudit(context.Background(), fake, target)
	if audit.Provider != "project" || audit.Ecosystem != "NuGet" || audit.Tool != "dotnet" || audit.Target != target {
		t.Fatalf("expected .NET project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 3 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 3 {
		t.Fatalf("expected held dotnet audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "dotnet" || !containsString(fake.calls[0], "--include-transitive") || !containsString(fake.calls[0], "--vulnerable") || !containsString(fake.calls[0], target) {
		t.Fatalf("expected dotnet package list call, got %#v", fake.calls)
	}
}

func TestRunDotnetProjectNativeAuditReportsMissingCommand(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Demo.csproj")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runDotnetProjectNativeAudit(context.Background(), fake, target)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable dotnet audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsDotnetProjectAudit(t *testing.T) {
	root := t.TempDir()
	solution := filepath.Join(root, "Demo.sln")
	project := filepath.Join(root, "Demo.csproj")
	if err := os.WriteFile(solution, []byte("Microsoft Visual Studio Solution File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("<Project />\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "nuget"})
	if len(audits) != 2 || audits[0].Target != project || audits[1].Target != solution {
		t.Fatalf("expected sorted .NET project native audits, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesReportsMavenProjectAuditUnavailable(t *testing.T) {
	root := t.TempDir()
	pom := filepath.Join(root, "pom.xml")
	gradle := filepath.Join(root, "build.gradle.kts")
	if err := os.WriteFile(pom, []byte("<project />\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gradle, []byte("plugins {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "maven"})
	if len(audits) != 2 || audits[0].Provider != "project" || audits[0].Ecosystem != "Maven" || audits[0].Status != plan.StatusUnavailable || audits[0].Decision != "review" {
		t.Fatalf("expected unavailable Maven project audits, got %#v", audits)
	}
	if audits[0].Target != gradle || audits[1].Target != pom {
		t.Fatalf("expected sorted Maven targets, got %#v", audits)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("expected Maven unavailable audit not to run a command, got %#v", fake.calls)
	}
}

func TestPipxAuditPathsRejectUnsafePackageName(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	paths := pipxAuditPaths([]securityPackage{{Ecosystem: "PyPI", Package: "../frogmouth", Version: "0.9.2"}})
	if len(paths) != 0 {
		t.Fatalf("expected unsafe package name to be rejected, got %#v", paths)
	}
	paths = pipxAuditPaths([]securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "../0.9.2"}})
	if len(paths) != 0 {
		t.Fatalf("expected unsafe package version to be rejected, got %#v", paths)
	}
}

func makePipxSitePackages(t *testing.T, miseDir string, pkg string, version string) string {
	t.Helper()
	path := filepath.Join(miseDir, "installs", "pipx-"+pkg, version, pkg, "lib", "python3.13", "site-packages")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("GH_TOKEN", "gh-token")
	if got := githubToken(); got != "updev-token" {
		t.Fatalf("expected UPDEV_GITHUB_TOKEN to win, got %q", got)
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

func TestJoinCommandQuotesShellFragments(t *testing.T) {
	got := joinCommand([]string{"bash", "-lc", "brew update && brew upgrade"})
	want := `bash -lc "brew update && brew upgrade"`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestListUsesGoImplementationByDefault(t *testing.T) {
	if shouldDelegate([]string{"list"}) {
		t.Fatal("expected updev list to use Go rich list")
	}
	if shouldDelegate([]string{"list", "--fast"}) {
		t.Fatal("expected updev list --fast to use Go inventory")
	}
}

func TestBuildListReportFiltersItems(t *testing.T) {
	result := inventoryResult{
		Cached:    true,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Report: plan.Report{
			Status: plan.StatusDrift,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "brew", Desired: 1, Live: 2, Extra: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Status: plan.StatusOK, Desired: true, Live: true},
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
				{Provider: "brew", Kind: "cask", Category: "personal", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: profileMismatchDetail("personal")},
			},
		},
	}
	report := buildListReport(result, listOptions{provider: "brew", status: "extra", query: "jq"})
	if !report.Cached || report.CacheAge == "" {
		t.Fatalf("expected cache metadata, got %#v", report)
	}
	if report.Limit != 0 {
		t.Fatalf("expected default limit to stay unlimited, got %d", report.Limit)
	}
	if len(report.Items) != 1 || report.Items[0].Name != "jq" {
		t.Fatalf("unexpected filtered items: %#v", report.Items)
	}
	attention := buildListReport(result, listOptions{status: "attention"})
	if len(attention.Items) != 2 || !listReportHasItem(attention, "jq") || !listReportHasItem(attention, "warp") {
		t.Fatalf("expected attention filter to include drift items, got %#v", attention.Items)
	}
	profile := buildListReport(result, listOptions{status: "profile-mismatch"})
	if len(profile.Items) != 1 || profile.Items[0].Name != "warp" || profile.Filters["status"] != "profile-mismatch" {
		t.Fatalf("expected profile-mismatch filter to include scoped drift, got %#v", profile)
	}
	category := buildListReport(result, listOptions{category: "personal"})
	if len(category.Items) != 2 || !listReportHasItem(category, "visual-studio-code") || !listReportHasItem(category, "warp") || category.Filters["category"] != "personal" {
		t.Fatalf("expected category filter to include personal cask, got %#v", category)
	}
}

func listReportHasItem(report listReport, name string) bool {
	for _, item := range report.Items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func TestBuildListReportEnrichesMiseAndShowsMiseVersionRows(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	rows := `{
  "mise": {
    "rows": [
      ["node", "24.15.0", "-", "inactive", "-", "Node runtime"],
      ["node", "24.16.0", "lts", "active", "/tmp/config.toml", "Node runtime"]
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "rows_cache.json"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	tsv := "mise:node\tNode runtime\tNode.js runtime\n"
	if err := os.WriteFile(filepath.Join(cacheDir, "desc_ja.tsv"), []byte(tsv), 0o600); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	report := buildListReport(result, listOptions{provider: "mise", query: "node"})
	if len(report.Items) != 1 || report.Items[0].Version != "24.16.0" || report.Items[0].Detail != "Node.js runtime" {
		t.Fatalf("expected active mise row to enrich item, got %#v", report.Items)
	}
	if len(report.Sections) != 1 || report.Sections[0].Title != "mise / runtime" {
		t.Fatalf("expected mise runtime section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 2 {
		t.Fatalf("expected active and inactive mise rows, got %#v", report.Sections[0].Rows)
	}
	active := report.Sections[0].Rows[0]
	if active.Name != "node" || active.Version != "24.16.0" || active.State != "active" || active.Wanted != "lts" {
		t.Fatalf("unexpected active row: %#v", active)
	}
	inactive := report.Sections[0].Rows[1]
	if inactive.Name != "node" || inactive.Version != "24.15.0" || inactive.State != "inactive" || inactive.Wanted != "-" {
		t.Fatalf("unexpected inactive row: %#v", inactive)
	}
	pending := pendingTranslations(report, loadLegacyCache(), true)
	if pending["mise:node"] != "Node runtime" {
		t.Fatalf("expected mise row cache description to feed translation, got %#v", pending)
	}
}

func TestBuildListReportFiltersMiseSectionsByToolStatus(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	rows := `{
  "mise": {
    "rows": [
      ["node", "24.15.0", "-", "inactive", "-", "Node runtime"],
      ["node", "24.16.0", "lts", "active", "/tmp/config.toml", "Node runtime"]
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "rows_cache.json"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   "/repo",
			Providers: []plan.ProviderSummary{
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	active := buildListReport(result, listOptions{provider: "mise", status: "active"})
	if len(active.Sections) != 1 || len(active.Sections[0].Rows) != 1 || active.Sections[0].Rows[0].State != "active" {
		t.Fatalf("expected only active mise row, got %#v", active.Sections)
	}
	inactive := buildListReport(result, listOptions{provider: "mise", status: "inactive"})
	if len(inactive.Sections) != 1 || len(inactive.Sections[0].Rows) != 1 || inactive.Sections[0].Rows[0].State != "inactive" {
		t.Fatalf("expected only inactive mise row, got %#v", inactive.Sections)
	}
	installed := buildListReport(result, listOptions{provider: "mise", status: "installed"})
	if len(installed.Sections) != 1 || len(installed.Sections[0].Rows) != 2 {
		t.Fatalf("expected both installed mise rows, got %#v", installed.Sections)
	}
}

func TestBuildListReportAddsManualAppsOnlyWhenRequested(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## Adobe Creative Cloud で管理

- Adobe Photoshop 2025

## Mac App Store で管理

| アプリ | 用途 |
|--------|------|
| Final Cut Pro, iMovie | Apple 純正 動画/音楽 |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}

	defaultReport := buildListReport(result, listOptions{root: root})
	if len(defaultReport.Sections) != 0 || len(defaultReport.Providers) != 0 {
		t.Fatalf("expected manual apps to stay out of the default report, got sections=%#v providers=%#v", defaultReport.Sections, defaultReport.Providers)
	}

	report := buildListReport(result, listOptions{root: root, provider: "manual", query: "final"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/app-store" {
		t.Fatalf("expected filtered manual app-store section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].Name != "Final Cut Pro" || report.Sections[0].Rows[0].State != "manual" {
		t.Fatalf("unexpected manual rows: %#v", report.Sections[0].Rows)
	}
	if len(report.Providers) != 1 || report.Providers[0].Name != "manual" || report.Providers[0].Desired != 1 || report.Providers[0].Live != 0 {
		t.Fatalf("expected synthetic manual provider summary, got %#v", report.Providers)
	}
	if counts := listKindCounts(report); counts["manual"] != 1 {
		t.Fatalf("expected manual kind count, got %#v", counts)
	}
	if counts := listCategoryCounts(report); counts["manual"] != 1 {
		t.Fatalf("expected manual category count, got %#v", counts)
	}
	if !listManualOnly(listOptions{provider: "manual"}) {
		t.Fatal("expected manual provider filter to avoid live provider collection")
	}
	if listManualOnly(listOptions{provider: "manual", includeVSCode: true}) {
		t.Fatal("did not expect mixed manual/vscode list to skip provider collection")
	}
}

func TestManualInventoryUsesConfiguredOverridesAndAliases(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory]\noverrides = \"inventory-overrides.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	overrides := `[[manual.apps]]
name = "Example App"
aliases = ["example-cask", "Example.app"]
category = "Vendor"
detail = "vendor updater"
managed_by = "vendor"
`
	if err := os.WriteFile(filepath.Join(root, "inventory-overrides.toml"), []byte(overrides), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", query: "example"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/vendor" {
		t.Fatalf("expected manual override section, got %#v", report.Sections)
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].State != "vendor" || report.Sections[0].Rows[0].Detail != "vendor updater" {
		t.Fatalf("unexpected override row: %#v", report.Sections[0].Rows)
	}
	if _, ok := manualAppMatch(manualAppIndex(root), "example-cask"); !ok {
		t.Fatalf("expected alias to reconcile manual/cask identity")
	}
}

func TestManualInventoryScansApplicationBundles(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo Display",
		"CFBundleName":               "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "1.2.3",
	})

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", status: "installed", query: "demo"})
	if len(report.Sections) != 1 || report.Sections[0].Name != "manual/installed-apps" {
		t.Fatalf("expected installed app section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Demo Display" || row.Version != "1.2.3" || row.State != "installed" {
		t.Fatalf("unexpected scanned app row: %#v", row)
	}
	for _, want := range []string{"source: app bundle", "path: ", "bundle_id: com.example.demo", "version: 1.2.3"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected scanned app detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Name != "manual" || report.Providers[0].Desired != 0 || report.Providers[0].Live != 1 {
		t.Fatalf("expected scanned app to count as live-only manual inventory, got %#v", report.Providers)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only installed app review candidate, got %#v", report.ReviewCandidates)
	}
	candidate := report.ReviewCandidates[0]
	if candidate.Evidence[0].Scanner != "macos_app_bundle" || candidate.Evidence[0].BundleID != "com.example.demo" || candidate.SuggestedOverride.Name != "Demo Display" || candidate.Confidence != "medium" || candidate.RemediationCode != "manual_inventory_override" {
		t.Fatalf("unexpected review candidate evidence: %#v", candidate)
	}
}

func TestManualInventoryMarksMacAppStoreReceipts(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "Applications", "StoreDemo.app")
	writeFakeAppBundle(t, appPath, map[string]string{
		"CFBundleDisplayName":        "Store Demo",
		"CFBundleIdentifier":         "com.example.store-demo",
		"CFBundleShortVersionString": "5.0.0",
	})
	writeFakeMASReceipt(t, appPath)

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", status: "installed", query: "store"})
	if len(report.Sections) != 1 || len(report.Sections[0].Rows) != 1 {
		t.Fatalf("expected one MAS receipt app row, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if !strings.Contains(row.Detail, "source: mac app store receipt") {
		t.Fatalf("expected MAS receipt source in detail, got %q", row.Detail)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].SuggestedOverride.ManagedBy != "mas" || report.ReviewCandidates[0].Confidence != "high" {
		t.Fatalf("expected MAS receipt candidate to suggest managed_by=mas, got %#v", report.ReviewCandidates)
	}
}

func TestManualInventoryParsesMASListEvidence(t *testing.T) {
	apps := parseManualMASList("803453959 Slack (4.45.69)\n409201541 Pages (14.4)\n")
	if len(apps) != 2 || apps[0].Name != "Pages" || apps[0].ID != "409201541" || apps[0].Version != "14.4" || apps[1].Name != "Slack" {
		t.Fatalf("unexpected mas list parse: %#v", apps)
	}

	sections := reconcileManualAppSections(manualMASListSectionsFromOutput("409201541 Pages (14.4)\n"))
	candidates := manualReviewCandidates(sections)
	if len(candidates) != 1 {
		t.Fatalf("expected MAS-only app review candidate, got %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.SuggestedOverride.ManagedBy != "mas" || candidate.Confidence != "high" || candidate.Evidence[0].Scanner != "mas_list" || candidate.Evidence[0].MASID != "409201541" {
		t.Fatalf("unexpected MAS-only candidate: %#v", candidate)
	}
}

func TestManualInventoryMergesMASListWithAppBundleEvidence(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Pages.app"), map[string]string{
		"CFBundleDisplayName":        "Pages",
		"CFBundleIdentifier":         "com.apple.iWork.Pages",
		"CFBundleShortVersionString": "14.4",
	})

	sections := append(manualScannedAppSections(root), manualMASListSectionsFromOutput("409201541 Pages (14.4)\n")...)
	sections = reconcileManualAppSections(sections)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected merged MAS/app-bundle evidence, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if row.State != "installed" || !strings.Contains(row.Detail, "source: app bundle") || !strings.Contains(row.Detail, "source: mas list") || !strings.Contains(row.Detail, "mas_id: 409201541") {
		t.Fatalf("unexpected merged MAS/app-bundle row: %#v", row)
	}
	candidates := manualReviewCandidates(sections)
	if len(candidates) != 1 || candidates[0].SuggestedOverride.ManagedBy != "mas" || candidates[0].Evidence[0].MASID != "409201541" {
		t.Fatalf("expected merged MAS candidate to preserve MAS evidence, got %#v", candidates)
	}
}

func TestManualInventoryReconcilesDocumentedAndInstalledApps(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte("## 業務・専門アプリ（Cask なし）\n\n- Pencil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Pencil.app"), map[string]string{
		"CFBundleDisplayName":        "Pencil",
		"CFBundleIdentifier":         "dev.pencil.desktop",
		"CFBundleShortVersionString": "1.1.17",
	})

	report := buildListReport(inventoryResult{Report: plan.Report{Status: plan.StatusOK, Root: root}}, listOptions{root: root, provider: "manual", query: "Pencil"})
	if len(report.Sections) != 1 {
		t.Fatalf("expected documented and installed app to reconcile into one section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Pencil" || row.Version != "1.1.17" || row.State != "managed" {
		t.Fatalf("expected reconciled managed row, got %#v", row)
	}
	for _, want := range []string{"業務・専門アプリ", "source: app bundle", "bundle_id: dev.pencil.desktop"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected reconciled detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Desired != 1 || report.Providers[0].Live != 1 {
		t.Fatalf("expected reconciled app to count as desired and live, got %#v", report.Providers)
	}
	if len(report.ReviewCandidates) != 0 {
		t.Fatalf("did not expect review candidate for reconciled app, got %#v", report.ReviewCandidates)
	}
}

func TestManualInventoryReconcilesDocumentedAppsWithHomebrewCasks(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte("## ベンダー独自更新\n\n- Evernote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusOK, Desired: true, Live: true, Category: "personal"},
		},
	}}

	report := buildListReport(result, listOptions{root: root, provider: "manual", query: "Evernote"})
	if len(report.Sections) != 1 {
		t.Fatalf("expected documented and cask app to reconcile into one section, got %#v", report.Sections)
	}
	row := report.Sections[0].Rows[0]
	if row.Name != "Evernote" || row.State != "brew" {
		t.Fatalf("expected reconciled brew row, got %#v", row)
	}
	for _, want := range []string{"ベンダー独自更新", "source: homebrew cask", "cask: evernote", "category: personal"} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected cask reconciliation detail to contain %q, got %q", want, row.Detail)
		}
	}
	if len(report.Providers) != 1 || report.Providers[0].Desired != 1 || report.Providers[0].Live != 1 {
		t.Fatalf("expected reconciled cask to count as desired and live, got %#v", report.Providers)
	}
}

func TestManualInventoryHidesHomebrewOnlyCasksByDefault(t *testing.T) {
	root := t.TempDir()
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "firefox", Status: plan.StatusOK, Desired: true, Live: true, Category: "personal"},
		},
	}}

	defaultReport := buildListReport(result, listOptions{root: root, provider: "manual"})
	if len(defaultReport.Sections) != 0 {
		t.Fatalf("expected Homebrew-only cask evidence to be hidden by default, got %#v", defaultReport.Sections)
	}
	statusReport := buildListReport(result, listOptions{root: root, provider: "manual", status: "brew"})
	if len(statusReport.Sections) != 1 || statusReport.Sections[0].Rows[0].Name != "Firefox" {
		t.Fatalf("expected explicit brew status filter to show cask evidence, got %#v", statusReport.Sections)
	}
	queryReport := buildListReport(result, listOptions{root: root, provider: "manual", query: "firefox"})
	if len(queryReport.Sections) != 1 || queryReport.Sections[0].Rows[0].State != "brew" {
		t.Fatalf("expected query filter to show cask evidence, got %#v", queryReport.Sections)
	}
}

func TestHomebrewTapDocsStayBrewEvidenceNotManualDesired(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## Intel Mac 向け自前ビルド（Homebrew Tap で自動配布）

| アプリ | リポジトリ | Cask 名 | ビルド方式 |
|--------|-----------|---------|-----------|
| Codex Monitor | [webkaz/codexmonitor-intel-builds](https://github.com/webkaz/codexmonitor-intel-builds) | ` + "`codexmonitor-intel`" + ` | Tauri クロスコンパイル |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Codex Monitor.app"), map[string]string{
		"CFBundleDisplayName":        "Codex Monitor",
		"CFBundleIdentifier":         "com.example.codexmonitor",
		"CFBundleShortVersionString": "0.7.67",
	})
	result := inventoryResult{Report: plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "codexmonitor-intel",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
	}}

	normal := buildListReport(result, listOptions{root: root, query: "codexmonitor"})
	if len(normal.Items) != 1 || !strings.Contains(normal.Items[0].Detail, "source: homebrew tap docs") || !strings.Contains(normal.Items[0].Detail, "cask: codexmonitor-intel") {
		t.Fatalf("expected normal brew inventory row to carry Homebrew Tap docs evidence, got %#v", normal.Items)
	}

	manual := buildListReport(result, listOptions{root: root, provider: "manual", query: "codexmonitor"})
	if len(manual.Sections) != 1 || manual.Sections[0].Name != "manual/installed-apps" {
		t.Fatalf("expected Homebrew Tap docs to reconcile into installed evidence, got %#v", manual.Sections)
	}
	row := manual.Sections[0].Rows[0]
	if row.Name != "Codex Monitor" || row.State != "brew" || !strings.Contains(row.Detail, "source: homebrew tap docs") || !strings.Contains(row.Detail, "source: homebrew cask") {
		t.Fatalf("expected reconciled brew evidence row, got %#v", row)
	}
	if strings.Count(row.Detail, "cask: codexmonitor-intel") != 1 {
		t.Fatalf("expected cask evidence to be deduplicated, got %q", row.Detail)
	}
	if len(row.Actions) == 0 || !toolRowHasRouteAction(row, listHubActionManual) {
		t.Fatalf("expected reconciled row to preserve manual review routing action, got %#v", row.Actions)
	}
	if len(manual.Providers) != 1 || manual.Providers[0].Desired != 0 || manual.Providers[0].Live != 1 {
		t.Fatalf("expected Homebrew Tap evidence not to count as manual desired state, got %#v", manual.Providers)
	}
	if len(manual.ReviewCandidates) != 0 {
		t.Fatalf("did not expect manual review candidate for Homebrew-managed cask evidence, got %#v", manual.ReviewCandidates)
	}
}

func TestManualLiveCaskInventoryItemsUsesBoundedBrewProbe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Homebrew cask probe is macOS-only")
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "firefox\nvisual-studio-code\n"}}
	items := manualLiveCaskInventoryItems(defaultRoot(), fake)
	if len(items) != 2 || items[0].Provider != "brew" || items[0].Kind != "cask" || items[0].Name != "firefox" || items[0].Status != plan.StatusExtra || !items[0].Live {
		t.Fatalf("expected live Homebrew cask evidence, got %#v", items)
	}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], " ") != "brew list --cask -1" {
		t.Fatalf("expected bounded brew cask list probe, got %#v", fake.calls)
	}
	if got := manualLiveCaskInventoryItems(t.TempDir(), fake); got != nil {
		t.Fatalf("expected non-default root to skip live brew probe, got %#v", got)
	}
}

func TestInventoryRenderBuildsManualAppsPreview(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryRenderReport(inventoryRenderOptions{root: root, report: "manual-apps", format: "json"})
	if report.SchemaVersion != 1 || report.Report != "manual-apps" || report.Path != filepath.Join(root, "docs", "apps.md") {
		t.Fatalf("unexpected render report metadata: %#v", report)
	}
	for _, want := range []string{"Generated preview", "## Installed apps", "| Demo | installed | 2.0.0 |"} {
		if !strings.Contains(report.Content, want) {
			t.Fatalf("expected rendered markdown to contain %q:\n%s", want, report.Content)
		}
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only review candidate in render report, got %#v", report.ReviewCandidates)
	}
}

func TestInventoryScanBuildsManualAppEvidence(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryScanReport(inventoryScanOptions{root: root, provider: "manual", format: "json"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected scan report metadata: %#v", report)
	}
	if report.Summary.Live != 1 || report.Summary.Desired != 0 {
		t.Fatalf("expected live-only manual summary, got %#v", report.Summary)
	}
	if len(report.Sections) != 1 || len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].Name != "Demo" {
		t.Fatalf("expected scanned app section, got %#v", report.Sections)
	}
	if len(report.ReviewCandidates) != 1 || report.ReviewCandidates[0].Evidence[0].BundleID != "com.example.demo" {
		t.Fatalf("expected review candidate with bundle evidence, got %#v", report.ReviewCandidates)
	}
	if code := inventoryScanExitCode(report); code != 2 {
		t.Fatalf("expected review-needed scan exit code, got %d", code)
	}
}

func TestInventoryScanRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryScanOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestInventoryRenderRejectsUnsupportedReport(t *testing.T) {
	if _, err := parseInventoryRenderOptions([]string{"--report", "unknown"}); err == nil {
		t.Fatal("expected unsupported report error")
	}
}

func TestInventoryReviewBuildsManualOverridePreview(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected review report metadata: %#v", report)
	}
	if report.OverridesPath != filepath.Join(root, ".config", "updev", "inventory-overrides.toml") {
		t.Fatalf("unexpected default overrides path: %q", report.OverridesPath)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only manual app candidate, got %#v", report.Candidates)
	}
	for _, want := range []string{
		"[[manual.apps]]",
		`name = "Demo"`,
		`aliases = ["Demo.app", "com.example.demo"]`,
		`managed_by = "manual"`,
		`# reason_code = "manual_app_live_only"`,
		`# remediation_code = "manual_inventory_override"`,
		`# confidence = "medium"`,
		`bundle_id="com.example.demo"`,
		`version="2.0.0"`,
	} {
		if !strings.Contains(report.OverridePreview, want) {
			t.Fatalf("expected override preview to contain %q:\n%s", want, report.OverridePreview)
		}
	}
	if code := inventoryReviewExitCode(report); code != 2 {
		t.Fatalf("expected review-needed exit code, got %d", code)
	}
}

func TestInventoryReviewUsesConfiguredOverridePath(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory]\noverrides = \"state/manual-overrides.toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.OverridesPath != filepath.Join(root, "state", "manual-overrides.toml") {
		t.Fatalf("expected configured overrides path, got %q", report.OverridesPath)
	}
	if report.Status != plan.StatusOK || len(report.Candidates) != 0 || report.OverridePreview != "" {
		t.Fatalf("expected empty review to stay ok, got %#v", report)
	}
}

func TestInventoryReviewAcceptsManualOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName": "Demo",
		"CFBundleIdentifier":  "com.example.demo",
	})

	opts := inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "demo", format: "json"}
	report := buildInventoryReviewReport(opts)
	if len(report.Candidates) != 1 {
		t.Fatalf("expected one candidate before accept, got %#v", report.Candidates)
	}
	if _, _, err := applyInventoryReviewAction(opts, report); err != nil {
		t.Fatalf("accept action failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".config", "updev", "inventory-overrides.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name = "Demo"`, `aliases = ["Demo.app", "com.example.demo"]`, `managed_by = "manual"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected accepted override to contain %q:\n%s", want, string(data))
		}
	}
	after := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if after.Status != plan.StatusOK || len(after.Candidates) != 0 {
		t.Fatalf("expected accepted override to suppress review candidate, got %#v", after)
	}
}

func TestInventoryReviewIgnoreAddsLocalOnlyOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Local Helper.app"), map[string]string{
		"CFBundleDisplayName": "Local Helper",
		"CFBundleIdentifier":  "com.example.local-helper",
	})

	opts := inventoryReviewOptions{root: root, provider: "manual", action: "ignore", query: "helper", format: "json"}
	report := buildInventoryReviewReport(opts)
	if _, _, err := applyInventoryReviewAction(opts, report); err != nil {
		t.Fatalf("ignore action failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".config", "updev", "inventory-overrides.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`category = "Ignored"`, `lifecycle = "local-only"`, `local-only app ignored by manual inventory review`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected ignored override to contain %q:\n%s", want, string(data))
		}
	}
	sections := manualAppSectionsForInventoryCommand(root)
	if len(sections) != 1 || len(sections[0].Rows) != 1 || sections[0].Rows[0].State != "local-only" {
		t.Fatalf("expected local-only override row, got %#v", sections)
	}
	summary := manualProviderSummary(sections)
	if summary.Desired != 0 || summary.Live != 1 {
		t.Fatalf("expected local-only app to count as live-only, got %#v", summary)
	}
	localOnlyWithoutLive := manualProviderSummary(manualOverrideSections([]manualAppOverride{{Name: "Ghost", Lifecycle: "local-only"}}))
	if localOnlyWithoutLive.Desired != 0 || localOnlyWithoutLive.Live != 0 {
		t.Fatalf("expected local-only override without live evidence to stay out of desired/live counts, got %#v", localOnlyWithoutLive)
	}
	after := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if after.Status != plan.StatusOK || len(after.Candidates) != 0 {
		t.Fatalf("expected ignored override to suppress review candidate, got %#v", after)
	}
}

func TestInventoryReviewDetectsExistingOverrideByAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	path := filepath.Join(root, ".config", "updev", "inventory-overrides.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[[manual.apps]]
name = "Existing Demo"
aliases = ["com.example.demo"]
managed_by = "manual"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := manualReviewCandidate{
		Name: "Demo",
		SuggestedOverride: manualReviewOverrideFields{
			Name:    "Demo",
			Aliases: []string{"Demo.app", "com.example.demo"},
		},
	}
	existing := matchingManualOverride(root, candidate)
	if existing.Name != "Existing Demo" {
		t.Fatalf("expected existing override alias match, got %#v", existing)
	}
	report := inventoryReviewReport{Candidates: []manualReviewCandidate{candidate}}
	if _, _, err := applyInventoryReviewAction(inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "demo", format: "json"}, report); err == nil {
		t.Fatal("expected duplicate override to block accept append")
	}
}

func TestInventoryReviewListsUpdatesAndRemovesOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	path := filepath.Join(root, ".config", "updev", "inventory-overrides.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`# keep header
[[manual.apps]]
name = "Demo"
aliases = ["com.example.demo"]
managed_by = "manual"
detail = "before"

# keep neighbor comment
[[manual.apps]]
name = "Keep"
managed_by = "manual"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	list := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "list", query: "demo", format: "json"})
	if len(list.Overrides) != 1 || list.Overrides[0].Name != "Demo" {
		t.Fatalf("expected override list to filter by query, got %#v", list.Overrides)
	}
	t.Setenv("EDITOR", "true")
	updateOpts := inventoryReviewOptions{root: root, provider: "manual", action: "update", query: "demo", format: "json"}
	updateReport := buildInventoryReviewReport(updateOpts)
	changed, err := applyManualOverrideManagementAction(updateOpts, updateReport.Overrides)
	if err != nil || changed.Name != "Demo" {
		t.Fatalf("update action failed: changed=%#v err=%v", changed, err)
	}
	removeOpts := inventoryReviewOptions{root: root, provider: "manual", action: "remove", query: "demo", format: "json"}
	removeReport := buildInventoryReviewReport(removeOpts)
	changed, err = applyManualOverrideManagementAction(removeOpts, removeReport.Overrides)
	if err != nil || changed.Name != "Demo" {
		t.Fatalf("remove action failed: changed=%#v err=%v", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep header", "# keep neighbor comment", `name = "Keep"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected remove to preserve %q in untouched content:\n%s", want, string(data))
		}
	}
	after := loadManualAppOverrides(root)
	if len(after) != 1 || after[0].Name != "Keep" {
		t.Fatalf("expected remove action to leave untouched override, got %#v", after)
	}
}

func TestManualPlanTextDetailsExposeActionableHints(t *testing.T) {
	items := []manualPlanItem{{
		Name:           "Vendor App",
		Action:         "open-vendor",
		ReviewURL:      "https://example.com/vendor",
		InstallHint:    "open the vendor URL for review only",
		CommandPreview: []string{`open "https://example.com/vendor"`},
	}}
	var out bytes.Buffer
	printManualPlanTextDetails(&out, items, false)
	text := out.String()
	for _, want := range []string{"review details", "https://example.com/vendor", "open the vendor URL", `open "https://example.com/vendor"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected manual plan details to contain %q:\n%s", want, text)
		}
	}
}

func TestInventoryReviewActionRequiresSingleCandidate(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "One.app"), map[string]string{"CFBundleDisplayName": "One"})
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Two.app"), map[string]string{"CFBundleDisplayName": "Two"})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "accept", format: "json"})
	if _, _, err := applyInventoryReviewAction(inventoryReviewOptions{root: root, provider: "manual", action: "accept", format: "json"}, report); err == nil {
		t.Fatal("expected write action to require a single matching candidate")
	}
	filtered := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", action: "accept", query: "one", format: "json"})
	if len(filtered.Candidates) != 1 || filtered.Candidates[0].Name != "One" {
		t.Fatalf("expected query to select one candidate, got %#v", filtered.Candidates)
	}
}

func TestInventoryReviewReconcilesCachedHomebrewCasks(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Evernote.app"), map[string]string{
		"CFBundleDisplayName": "Evernote",
		"CFBundleIdentifier":  "com.evernote.Evernote",
	})
	saveInventoryCache(inventoryCacheEntry{
		Version:       inventoryCacheVersion,
		Root:          root,
		IncludeVSCode: false,
		CreatedAt:     time.Now(),
		Report: plan.Report{
			Root: root,
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	})

	report := buildInventoryReviewReport(inventoryReviewOptions{root: root, provider: "manual", format: "text"})
	if report.Status != plan.StatusOK || len(report.Candidates) != 0 {
		t.Fatalf("expected cached cask evidence to suppress live-only manual candidate, got %#v", report)
	}
}

func TestInventoryReviewRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryReviewOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestInventoryPlanBuildsManualReviewActions(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Demo.app"), map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "2.0.0",
	})

	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: "manual", format: "json"})
	if report.SchemaVersion != 1 || report.Provider != "manual" || report.Status != plan.StatusDrift {
		t.Fatalf("unexpected plan metadata: %#v", report)
	}
	if len(report.Items) != 1 || report.Items[0].Action != "needs-review" || report.Items[0].ReasonCode != "manual_app_live_only" {
		t.Fatalf("expected live-only manual review action, got %#v", report.Items)
	}
	if report.Items[0].SuggestedProvider != "manual" || len(report.Items[0].CommandPreview) != 1 || !strings.Contains(report.Items[0].InstallHint, "accept, edit, or ignore") {
		t.Fatalf("expected review guidance fields, got %#v", report.Items[0])
	}
	if report.ActionCounts["needs-review"] != 1 || report.AttentionCount != 1 || len(report.NextSteps) == 0 {
		t.Fatalf("expected action counts, attention count, and next steps, got %#v %d %#v", report.ActionCounts, report.AttentionCount, report.NextSteps)
	}
	if code := inventoryPlanExitCode(report); code != 2 {
		t.Fatalf("expected review-needed plan exit code, got %d", code)
	}
}

func TestInventoryPlanSuggestsMASAdoption(t *testing.T) {
	root := t.TempDir()
	writeFakeAppBundle(t, filepath.Join(root, "Applications", "Store.app"), map[string]string{
		"CFBundleDisplayName": "Store",
		"CFBundleIdentifier":  "com.example.store",
	})
	receipt := filepath.Join(root, "Applications", "Store.app", "Contents", "_MASReceipt")
	if err := os.MkdirAll(receipt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(receipt, "receipt"), []byte("receipt"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: "manual", format: "json"})
	if len(report.Items) != 1 || report.Items[0].Action != "adopt-mas" || report.Items[0].Confidence != "high" {
		t.Fatalf("expected MAS adoption suggestion, got %#v", report.Items)
	}
	if report.Items[0].SuggestedOverride == nil || report.Items[0].SuggestedOverride.ManagedBy != "mas" {
		t.Fatalf("expected suggested MAS override, got %#v", report.Items[0].SuggestedOverride)
	}
	if report.Items[0].SuggestedProvider != "mas" || !strings.Contains(report.Items[0].InstallHint, "Mac App Store") {
		t.Fatalf("expected MAS provider guidance, got %#v", report.Items[0])
	}
	if len(report.Items[0].CommandPreview) != 1 || !strings.Contains(report.Items[0].CommandPreview[0], `mas search "Store"`) {
		t.Fatalf("expected MAS search preview for receipt-only evidence, got %#v", report.Items[0].CommandPreview)
	}
}

func TestManualInventoryReconcilesCaskByApplicationPathBasename(t *testing.T) {
	sections := reconcileManualAppSections([]toolSection{
		{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows: []toolRow{{
				Name:    "Code",
				Version: "1.123.0",
				State:   "installed",
				Detail:  "source: app bundle; path: /Applications/Visual Studio Code.app",
			}},
		},
		{
			Name:  "manual/homebrew-casks",
			Title: "manual / Homebrew casks",
			Rows: []toolRow{{
				Name:   "Visual Studio Code",
				State:  "brew",
				Detail: "source: homebrew cask; cask: visual-studio-code; status: ok",
			}},
		},
	})
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one reconciled installed section, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if row.Name != "Code" || row.State != "brew" || !strings.Contains(row.Detail, "cask: visual-studio-code") {
		t.Fatalf("expected app bundle row to merge Homebrew cask evidence, got %#v", row)
	}
	if !manualRowHiddenByDefault(sections[0], row, listOptions{provider: "manual"}) {
		t.Fatalf("expected default manual view to hide brew-owned installed app row, got %#v", row)
	}
}

func TestInventoryPlanAddsBrewAndVendorGuidance(t *testing.T) {
	sections := []toolSection{
		{
			Name:  "manual/homebrew-casks",
			Title: "manual / Homebrew casks",
			Rows: []toolRow{{
				Name:   "Firefox",
				State:  "brew",
				Detail: "source: homebrew cask; cask: firefox; status: ok; category: personal",
			}},
		},
		{
			Name:  "manual/vendor",
			Title: "manual / Vendor",
			Rows: []toolRow{{
				Name:   "Vendor Tool",
				State:  "manual",
				Detail: "source: vendor; 入手先: https://vendor.example.com/tool",
			}},
		},
	}
	items := manualPlanItems(sections)
	if len(items) != 2 {
		t.Fatalf("expected two manual plan items, got %#v", items)
	}
	byName := map[string]manualPlanItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	brew := byName["Firefox"]
	if brew.Action != "adopt-brew" || brew.SuggestedProvider != "brew" || len(brew.CommandPreview) != 1 || !strings.Contains(brew.CommandPreview[0], "brew info --cask") {
		t.Fatalf("expected brew adoption guidance, got %#v", brew)
	}
	if brew.SuggestedOverride == nil || brew.SuggestedOverride.ManagedBy != "brew" || !containsString(brew.SuggestedOverride.Aliases, "firefox") {
		t.Fatalf("expected brew suggested override, got %#v", brew.SuggestedOverride)
	}
	masItems := manualPlanItems(manualMASListSectionsFromOutput("123456789 Store Tool (1.2.3)\n"))
	if len(masItems) != 1 || masItems[0].Action != "adopt-mas" || len(masItems[0].CommandPreview) != 1 || masItems[0].CommandPreview[0] != `mas lookup "123456789"` {
		t.Fatalf("expected MAS id adoption guidance, got %#v", masItems)
	}
	vendor := byName["Vendor Tool"]
	if vendor.Action != "open-vendor" || vendor.SuggestedProvider != "vendor" || vendor.ReviewURL != "https://vendor.example.com/tool" || len(vendor.CommandPreview) != 1 || !strings.HasPrefix(vendor.CommandPreview[0], "open ") {
		t.Fatalf("expected gated vendor guidance, got %#v", vendor)
	}
	if vendor.SuggestedOverride == nil || vendor.SuggestedOverride.ManagedBy != "vendor" {
		t.Fatalf("expected vendor suggested override, got %#v", vendor.SuggestedOverride)
	}
	brewRow := manualPlanDetailRow(brew)
	if len(brewRow.Actions) != 1 || !strings.Contains(brewRow.Actions[0].Value, "review-cask") {
		t.Fatalf("expected brew plan row to expose review-cask action, got %#v", brewRow.Actions)
	}
	if action, target, ok := parseManualPlanDetailAction(brewRow.Actions[0].Value); !ok || action != "review-cask" || target != "firefox" {
		t.Fatalf("unexpected parsed manual plan action: action=%q target=%q ok=%v", action, target, ok)
	}
	planSections := manualPlanToolSections(inventoryPlanReport{Items: []manualPlanItem{brew, vendor}})
	if len(planSections) != 2 || planSections[0].Title != "manual / adopt-brew" || planSections[1].Title != "manual / open-vendor" {
		t.Fatalf("expected manual plan grouped table sections by action, got %#v", planSections)
	}
	if len(planSections[0].Rows[0].Actions) != len(brewRow.Actions) || !strings.Contains(planSections[0].Rows[0].Detail, "summary:") {
		t.Fatalf("expected manual grouped row to preserve actions and rich detail, got %#v", planSections[0].Rows[0])
	}
}

func TestInventoryPlanSuggestsIgnoreLocalForUserApplications(t *testing.T) {
	section := toolSection{
		Name:  "manual/installed-apps",
		Title: "manual / Installed apps",
		Rows: []toolRow{{
			Name:   "Local Helper",
			State:  "installed",
			Detail: "source: app bundle; path: /Users/demo/Applications/Local Helper.app",
		}},
	}
	items := manualPlanItems([]toolSection{section})
	if len(items) != 1 || items[0].Action != "ignore-local" || items[0].ReasonCode != "manual_app_user_local" {
		t.Fatalf("expected user-local app ignore candidate, got %#v", items)
	}
	row := manualPlanDetailRow(items[0])
	if len(row.Actions) != 3 {
		t.Fatalf("expected installed manual row to expose accept/ignore/edit actions, got %#v", row.Actions)
	}
}

func TestInventoryPlanFiltersActionAndQuery(t *testing.T) {
	items := []manualPlanItem{
		{Name: "Demo", Action: "needs-review", Detail: "one"},
		{Name: "Pencil", Action: "open-vendor", Detail: "pencil.dev"},
	}
	got := filterManualPlanItems(items, inventoryPlanOptions{action: "open-vendor", query: "pencil"})
	if len(got) != 1 || got[0].Name != "Pencil" {
		t.Fatalf("expected filtered plan item, got %#v", got)
	}
	candidates := filterManualReviewCandidatesForPlan([]manualReviewCandidate{{Name: "Demo"}, {Name: "Pencil"}}, got)
	if len(candidates) != 1 || candidates[0].Name != "Pencil" {
		t.Fatalf("expected review candidates to follow filtered plan items, got %#v", candidates)
	}
}

func TestInventoryPlanRejectsUnsupportedProvider(t *testing.T) {
	if _, err := parseInventoryPlanOptions([]string{"--provider", "brew"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func writeFakeAppBundle(t *testing.T, path string, values map[string]string) {
	t.Helper()
	contents := filepath.Join(path, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	var plist strings.Builder
	plist.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
`)
	for key, value := range values {
		plist.WriteString("<key>")
		plist.WriteString(key)
		plist.WriteString("</key><string>")
		plist.WriteString(value)
		plist.WriteString("</string>\n")
	}
	plist.WriteString("</dict></plist>\n")
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFakeMASReceipt(t *testing.T, appPath string) {
	t.Helper()
	receiptDir := filepath.Join(appPath, "Contents", "_MASReceipt")
	if err := os.MkdirAll(receiptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(receiptDir, "receipt"), []byte("fake receipt"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildListReportUsesManualInventoryForCaskDriftGuidance(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "updev")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheDir))
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apps := `# macOS 手動管理アプリ

## ベンダー独自更新

| アプリ | 入手先 | 用途 |
|--------|--------|------|
| Evernote | 公式 | ノート |
`
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(apps), 0o644); err != nil {
		t.Fatal(err)
	}
	result := inventoryResult{
		Report: plan.Report{
			Status: plan.StatusDrift,
			Root:   root,
			Items: []plan.Item{
				{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusExtra, Live: true},
			},
		},
	}
	report := buildListReport(result, listOptions{root: root})
	if len(report.Items) != 1 || !strings.Contains(report.Items[0].Detail, "manual-local-only") {
		t.Fatalf("expected manual inventory guidance for extra cask, got %#v", report.Items)
	}
}

func TestPrintListTextUsesMiseSectionsInsteadOfDuplicateInventoryItems(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "mise", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Version: "24.16.0", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Sections: []toolSection{
			{
				Name:  "mise/runtime",
				Title: "mise / runtime",
				Rows: []toolRow{
					{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				},
			},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if strings.Contains(text, "mise / tool / runtime") {
		t.Fatalf("expected mise inventory item table to be suppressed when rich section exists:\n%s", text)
	}
	if !strings.Contains(text, "summary:") || !strings.Contains(text, "wanted") || !strings.Contains(text, "24.16.0") || !strings.Contains(text, "lts") {
		t.Fatalf("expected rich mise section with version and requested version:\n%s", text)
	}
}

func TestPrintListTextKeepsMiseDriftItemsWithRichSections(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "mise", Desired: 2, Live: 1, Missing: 1},
		},
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Version: "24.16.0", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Category: "github", Name: "github:openai/tunnel-client", Status: plan.StatusMissing, Desired: true, Detail: "defined in mise but not installed"},
		},
		Sections: []toolSection{
			{
				Name:  "mise/runtime",
				Title: "mise / runtime",
				Rows: []toolRow{
					{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				},
			},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if strings.Contains(text, "mise / tool / runtime") {
		t.Fatalf("expected ok mise inventory rows to stay suppressed when rich section exists:\n%s", text)
	}
	for _, want := range []string{"missing=1", "mise / tool / github", "github:openai/tunnel-client", "defined in mise but not installed", "mise / runtime", "24.16.0", "lts"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list text to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextShowsAttentionSummary(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 2, Extra: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
			{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: profileMismatchDetail("personal")},
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"summary:", "1 provider attention", "extra=1", "profile-mismatch=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list summary to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextShowsCategoryMeaning(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 1},
			{Name: "mise", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node", State: "active"}},
		}},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"categories", "personal=1", "runtime=1", "meaning:", "personal-only additions on top of work", "other categories are provider/backend groups"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected category summary to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextDetailsExpandsDescriptions(t *testing.T) {
	report := listReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Details: true,
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 1, Live: 1},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "demo", Status: plan.StatusOK, Desired: true, Live: true, Detail: "A deliberately long package description that should be available in the detail section."},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"details", "brew/brew demo", "A deliberately long package description"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list detail output to include %q:\n%s", want, text)
		}
	}
}

func TestPrintListTextSurfacesReviewEvidence(t *testing.T) {
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "cask", "demo-app"), "brew held: release age gate")
	listEvidenceAdd(evidence.Security, listEvidenceExactKey("brew", "cask", "demo-app"), "brew/cask demo-app: hold")
	listEvidenceAdd(evidence.Backends, listEvidenceExactKey("mise", "tool", "cargo:fd-find"), "aqua prebuilt CLI is preferred")
	report := listReport{
		Status:  plan.StatusHeld,
		Root:    "/repo",
		Details: true,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo-app",
			Status:   plan.StatusHeld,
			Desired:  true,
			Live:     true,
		}},
		Sections: []toolSection{{
			Name:  "mise/cargo",
			Title: "mise / cargo",
			Rows: []toolRow{{
				Name:   "cargo:fd-find",
				State:  "active",
				Detail: "fd via cargo",
			}},
		}},
		Evidence: evidence,
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	for _, want := range []string{"update evidence:", "security evidence:", "backend evidence:", "brew held: release age gate", "aqua prebuilt CLI is preferred"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected list text to include review evidence %q:\n%s", want, text)
		}
	}
}

func TestListTableShowsUpdatedBadgeWithVersionDelta(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "jq"), "brew updated: jq 1.7 -> 1.8.1")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "jq",
			Version:  "1.8.1",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶up 1.7→1.8.1" {
		t.Fatalf("expected updated badge with version delta, got %#v", row)
	}
}

func TestListTableOmitsSymbolicUpdateDelta(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "brew", "wezterm@nightly"), "brew updated: wezterm@nightly latest -> latest")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "wezterm@nightly",
			Version:  "latest",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶up" {
		t.Fatalf("expected symbolic update badge without version delta, got %#v", row)
	}
}

func TestListTableShowsHeldBadgeForDeferredUpdateEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	listEvidenceAdd(evidence.Updates, listEvidenceExactKey("brew", "cask", "demo-app"), "brew held: release age gate")
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo-app",
			Status:   plan.StatusHeld,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶hold" {
		t.Fatalf("expected held update badge, got %#v", row)
	}
}

func TestListTableShowsHeldBadgeForBrewSecurityHoldEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	for _, key := range listEvidenceFindingKeys(safetyGate{Provider: "brew"}, safetyFinding{
		Provider: "brew",
		Kind:     "brew",
		Name:     "jq",
		Decision: "hold",
		Reason:   "candidate release is too new",
	}) {
		listEvidenceAdd(evidence.Security, key, "brew/brew jq: hold")
	}
	report := listReport{
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "jq",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := reviewui.StyledRow(sections[0].Rows[0], false, false)
	if len(row) != 5 || row[3] != "▶hold" {
		t.Fatalf("expected security hold badge, got %#v", row)
	}
	if !toolRowHasRouteAction(sections[0].Rows[0], listHubActionSecurity) {
		t.Fatalf("expected security route action for held brew finding, got %#v", sections[0].Rows[0].Actions)
	}
}

func TestListDetailRowsIncludeItemsAndToolSections(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Limit:  1,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true, Detail: "JSON processor"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows: []toolRow{
				{Name: "node", Version: "24.16.0", Wanted: "lts", State: "active", Detail: "Node runtime"},
				{Name: "python", Version: "3.13.0", Wanted: "latest", State: "inactive", Detail: "Python runtime"},
			},
		}},
	}
	rows := listDetailRows(report)
	if len(rows) != 2 {
		t.Fatalf("expected item plus limited tool detail rows, got %#v", rows)
	}
	if rows[0].Title != "brew/brew jq" || rows[1].Title != "mise / runtime node" {
		t.Fatalf("unexpected detail row titles: %#v", rows)
	}
	if !strings.Contains(strings.Join(rows[1].Metadata, " "), "wanted: lts") {
		t.Fatalf("expected mise wanted version metadata, got %#v", rows[1])
	}
	if !strings.Contains(rows[0].Detail, "description: JSON processor") || !strings.Contains(rows[0].Detail, "identity: brew / brew / jq") || !strings.Contains(rows[0].Detail, "status: ok - desired and installed") {
		t.Fatalf("expected inventory item detail to explain status and management state, got %#v", rows[0])
	}
	if !strings.Contains(strings.Join(rows[0].Metadata, " "), "name: jq") {
		t.Fatalf("expected inventory item metadata to include item identity, got %#v", rows[0])
	}
	renderedInventoryDetail := strings.Join(detailBrowserExpandedLinesStyled(rows[0], 80, true), "\n")
	for _, want := range []string{"\033[1m\033[35mdetails", "\033[36mdescription:", "\033[36midentity:", "\033[36mstatus:", "\033[1m\033[35mevidence", "\033[36mprovider:"} {
		if !strings.Contains(renderedInventoryDetail, want) {
			t.Fatalf("expected rendered inventory detail to contain %q:\n%q", want, renderedInventoryDetail)
		}
	}
	if len(rows[0].Actions) != 0 {
		t.Fatalf("did not expect backend route without backend evidence, got %#v", rows[0].Actions)
	}
}

func TestListTableSectionsConvertBrewItemsToExpandableRows(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Category: "work", Name: "jq", Version: "1.8.1", Status: plan.StatusOK, Desired: true, Live: true, Detail: "JSON processor"},
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "visual-studio-code", Version: "1.100.0", Status: plan.StatusExtra, Live: true, Detail: "Editor"},
		},
	}
	sections := listTableSections(report)
	if len(sections) != 2 || sections[0].Title != "brew / brew / work" || sections[1].Title != "brew / cask / personal" {
		t.Fatalf("expected brew item sections, got %#v", sections)
	}
	if sections[0].Rows[0].Name != "jq" || sections[0].Rows[0].State != "ok" || !strings.Contains(sections[0].Rows[0].Detail, "description: JSON processor") || !strings.Contains(sections[0].Rows[0].Detail, "identity: brew / brew / jq") {
		t.Fatalf("expected brew item detail to include rich inventory context, got %#v", sections[0].Rows[0])
	}
	if len(sections[0].Rows[0].Actions) != 0 {
		t.Fatalf("did not expect backend route without backend evidence, got %#v", sections[0].Rows[0].Actions)
	}
	model := newToolTableBrowserModel("updev list brew", sections, detailBrowserState{}, false)
	view := model.View().Content
	for _, want := range []string{"brew / brew / work", "jq", "JSON processor", "brew / cask / personal", "visual-studio-code"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected brew table browser to include %q:\n%s", want, view)
		}
	}
	model.ToggleSelected()
	view = model.View().Content
	for _, want := range []string{"detail", "description: JSON processor", "identity: brew / brew / jq", "status: ok - desired and installed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded installed inventory row to include %q:\n%s", want, view)
		}
	}
	for _, duplicate := range []string{"metadata", "version: 1.8.1", "state: ok"} {
		if strings.Contains(view, duplicate) {
			t.Fatalf("did not expect expanded installed inventory row to repeat table metadata %q:\n%s", duplicate, view)
		}
	}
}

func TestManualSectionRowsRouteToManualReview(t *testing.T) {
	sections := manualCaskSections([]plan.Item{{Provider: "brew", Kind: "cask", Name: "demo-app", Status: plan.StatusOK}})
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one manual cask row, got %#v", sections)
	}
	if len(sections[0].Rows[0].Actions) != 1 || !toolRowHasRouteAction(sections[0].Rows[0], listHubActionManual) {
		t.Fatalf("expected manual cask row to route to manual review, got %#v", sections[0].Rows[0].Actions)
	}
	detail := toolDetailRow(sections[0], sections[0].Rows[0])
	if len(detail.Actions) != 1 || !detailRowHasRouteAction(detail, listHubActionManual) || !strings.Contains(strings.Join(detail.Metadata, " "), "next action:") {
		t.Fatalf("expected manual detail row to preserve review routing action and evidence, got %#v", detail)
	}
}

func TestListFilteredActionRoutesDomainActionsBackToHub(t *testing.T) {
	defaultAction := listHubActionProvider
	pendingAction := ""
	handled, exit := handleListFilteredAction(listHubActionBackends, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionBackends || pendingAction != listHubActionBackends {
		t.Fatalf("expected backend row action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}

	handled, exit = handleListFilteredAction(updevActionHome, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionFull {
		t.Fatalf("expected home action to reset default action, handled=%v exit=%v default=%q", handled, exit, defaultAction)
	}

	handled, exit = handleListFilteredAction(updevActionExit, true, &defaultAction, &pendingAction)
	if !handled || !exit {
		t.Fatalf("expected exit action to exit, handled=%v exit=%v", handled, exit)
	}
}

func TestListFilteredActionRoutesInventoryToggleActions(t *testing.T) {
	defaultAction := listHubActionFull
	pendingAction := ""
	handled, exit := handleListFilteredAction(listHubActionManual, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionManual || pendingAction != listHubActionManual {
		t.Fatalf("expected manual toggle action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}

	handled, exit = handleListFilteredAction(listHubActionFull, true, &defaultAction, &pendingAction)
	if !handled || exit || defaultAction != listHubActionFull || pendingAction != listHubActionFull {
		t.Fatalf("expected installed toggle action to become pending hub action, handled=%v exit=%v default=%q pending=%q", handled, exit, defaultAction, pendingAction)
	}
}

func TestListTableBrowserViewToggleLabelsAreOptIn(t *testing.T) {
	defaultLabels := tableBrowserLabels()
	if strings.Contains(defaultLabels.ControlsHelp, "Tab") {
		t.Fatalf("default table browser labels should not mention view toggle: %q", defaultLabels.ControlsHelp)
	}

	toggleLabels := tableBrowserLabelsWithViewToggle()
	if !strings.Contains(toggleLabels.ControlsHelp, "Tab") {
		t.Fatalf("expected list toggle controls to mention Tab: %q", toggleLabels.ControlsHelp)
	}
	if !strings.Contains(strings.Join(toggleLabels.HelpLines, "\n"), "installed") {
		t.Fatalf("expected list toggle help to mention installed/manual switch: %#v", toggleLabels.HelpLines)
	}
}

func TestListRowsRouteToCachedUpdateAndSecurityEvidence(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	saveLastUpdateReport(updateReport{
		Root: root,
		Steps: []updateStep{{
			Name:         "brew",
			Status:       plan.StatusHeld,
			Reason:       "security review required",
			Updated:      []string{"jq"},
			SkippedItems: []string{"ripgrep"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "ripgrep",
				Decision: "hold",
				Reason:   "new release is inside minimum age",
			}},
		}},
	})
	report := buildListReport(inventoryResult{Report: plan.Report{
		Root: root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}}, listOptions{root: root})
	sections := listTableSections(report)
	rowsByName := map[string]toolRow{}
	for _, section := range sections {
		for _, row := range section.Rows {
			rowsByName[section.Name+"/"+row.Name] = row
		}
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/jq"], listHubActionUpdates) || strings.Contains(rowsByName["brew/brew/jq"].Detail, "security evidence:") {
		t.Fatalf("expected jq row to route only to update evidence, got %#v", rowsByName["brew/brew/jq"])
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionUpdates) || !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionSecurity) || !strings.Contains(rowsByName["brew/brew/ripgrep"].Detail, "security evidence:") {
		t.Fatalf("expected ripgrep row to route to update and security evidence, got %#v", rowsByName["brew/brew/ripgrep"])
	}
	if toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionUpdates) || toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionSecurity) {
		t.Fatalf("did not expect brew evidence to attach to same-name mise row, got %#v", rowsByName["mise/tool/ripgrep"])
	}
	details := listDetailRows(report)
	byTitle := map[string]detailBrowserRow{}
	for _, row := range details {
		byTitle[row.Title] = row
	}
	rg := byTitle["brew/brew ripgrep"]
	if !detailRowHasRouteAction(rg, listHubActionSecurity) || !strings.Contains(strings.Join(rg.Metadata, " "), "security evidence:") {
		t.Fatalf("expected ripgrep detail row to expose security route evidence, got %#v", rg)
	}
}

func TestListRowsShowHoldBadgeForStrictSecurityReviewEvidence(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	saveLastUpdateReport(updateReport{
		Status:   plan.StatusHeld,
		Root:     root,
		Security: "strict",
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "cask",
				Name:     "firefox",
				Decision: "review",
				Reason:   "Homebrew cask needs vendor provenance review before update",
			}},
		}},
	})
	report := buildListReport(inventoryResult{Report: plan.Report{
		Root: root,
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "firefox",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
	}}, listOptions{root: root})
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one list row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	rendered := reviewui.StyledRow(row, false, false)
	if len(rendered) != 5 || rendered[3] != "▶hold" {
		t.Fatalf("expected strict review evidence to render as held, got %#v", rendered)
	}
	if !strings.Contains(row.Detail, "security evidence:") || !strings.Contains(row.Detail, "held (decision: review)") {
		t.Fatalf("expected detail to preserve strict-held review decision, got %q", row.Detail)
	}
}

func TestListRowsExposeBackendRouteOnlyWithMatchingEvidence(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "mise", Kind: "tool", Name: "ripgrep", Status: plan.StatusOK, Desired: true, Live: true},
		},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
			Type:            "homebrew-to-mise",
			Provider:        "brew",
			Kind:            "brew",
			Name:            "ripgrep",
			RecommendedName: "ripgrep",
			RecommendedSpec: "15.1.0",
			Reason:          "ripgrep is already a stable mise-managed CLI",
		}}}),
	}
	sections := listTableSections(report)
	rowsByName := map[string]toolRow{}
	for _, section := range sections {
		for _, row := range section.Rows {
			rowsByName[section.Name+"/"+row.Name] = row
		}
	}
	if toolRowHasRouteAction(rowsByName["brew/brew/jq"], listHubActionBackends) {
		t.Fatalf("did not expect unrelated jq row to expose backend route: %#v", rowsByName["brew/brew/jq"])
	}
	if !toolRowHasRouteAction(rowsByName["brew/brew/ripgrep"], listHubActionBackends) || !strings.Contains(rowsByName["brew/brew/ripgrep"].Detail, "backend evidence:") {
		t.Fatalf("expected ripgrep row to expose focused backend route and evidence: %#v", rowsByName["brew/brew/ripgrep"])
	}
	if !toolRowHasRouteAction(rowsByName["mise/tool/ripgrep"], listHubActionBackends) || !strings.Contains(rowsByName["mise/tool/ripgrep"].Detail, "backend evidence:") {
		t.Fatalf("expected recommended mise row to expose backend route and evidence: %#v", rowsByName["mise/tool/ripgrep"])
	}
}

func TestListSectionsExposeBackendRoutesForRichToolRows(t *testing.T) {
	report := listReport{
		Sections: []toolSection{{
			Name:  "mise/cargo",
			Title: "mise / cargo",
			Rows: []toolRow{{
				Name:    "cargo:fd-find",
				Version: "10.4.2",
				State:   "active",
				Detail:  "fd via cargo",
			}},
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlanReport{Findings: []backendFinding{{
			Type:                "mise-backend-rewrite",
			Provider:            "mise",
			Kind:                "tool",
			Name:                "cargo:fd-find",
			Current:             "cargo:fd-find",
			RecommendedProvider: "mise",
			RecommendedName:     "aqua:sharkdp/fd",
			CommandNames:        []string{"fd"},
			Reason:              "aqua prebuilt CLI is preferred over a cargo global build",
		}}}),
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one enriched section row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if !toolRowHasRouteAction(row, listHubActionBackends) || !strings.Contains(row.Detail, "backend evidence:") {
		t.Fatalf("expected rich mise section row to expose backend route and evidence, got %#v", row)
	}
}

func TestManualRowsExposeUpdateAndSecurityRoutesFromCaskEvidence(t *testing.T) {
	evidence := listEvidenceIndex{Updates: map[string][]string{}, Security: map[string][]string{}, Backends: map[string][]string{}}
	for _, key := range listEvidenceUpdateItemKeys("brew", "cask demo-app") {
		listEvidenceAdd(evidence.Updates, key, "brew held: release age gate")
	}
	for _, key := range listEvidenceFindingKeys(safetyGate{Provider: "brew"}, safetyFinding{Provider: "brew", Kind: "cask", Name: "demo-app", Decision: "hold", Reason: "release too new"}) {
		listEvidenceAdd(evidence.Security, key, "brew/cask demo-app: hold")
	}
	report := listReport{
		Sections: []toolSection{{
			Name:  "manual/installed-apps",
			Title: "manual / Installed apps",
			Rows: []toolRow{{
				Name:   "Demo App",
				State:  "brew",
				Detail: "source: app bundle; cask: demo-app",
			}},
		}},
		Evidence: evidence,
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one manual section row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	if !toolRowHasRouteAction(row, listHubActionUpdates) || !toolRowHasRouteAction(row, listHubActionSecurity) {
		t.Fatalf("expected manual cask evidence row to expose update and security routes, got %#v", row.Actions)
	}
	if !strings.Contains(row.Detail, "update evidence:") || !strings.Contains(row.Detail, "security evidence:") {
		t.Fatalf("expected manual cask evidence row detail to include update and security evidence, got %q", row.Detail)
	}
}

func TestBackendDetailRowsForListRouteFocusesMatchingItem(t *testing.T) {
	report := backendPlanReport{Findings: []backendFinding{
		{Type: "homebrew-to-mise", Provider: "brew", Kind: "brew", Name: "ripgrep", RecommendedName: "ripgrep", RecommendedSpec: "15.1.0", Reason: "ripgrep can move"},
		{Type: "homebrew-to-mise", Provider: "brew", Kind: "brew", Name: "jq", RecommendedName: "jq", RecommendedSpec: "1.8.1", Reason: "jq can move"},
	}}
	rows := backendDetailRowsForListRoute(report, listRouteAction{Domain: listHubActionBackends, Provider: "brew", Kind: "brew", Name: "ripgrep"})
	if len(rows) != 1 || !strings.Contains(rows[0].Title, "ripgrep") {
		t.Fatalf("expected focused ripgrep backend row, got %#v", rows)
	}
}

func toolRowHasAction(row toolRow, value string) bool {
	for _, action := range row.Actions {
		if action.Value == value {
			return true
		}
	}
	return false
}

func toolRowHasRouteAction(row toolRow, domain string) bool {
	for _, action := range row.Actions {
		if route, ok := parseListRouteAction(action.Value); ok && route.Domain == domain {
			return true
		}
	}
	return false
}

func detailRowHasAction(row detailBrowserRow, value string) bool {
	for _, action := range row.Actions {
		if action.Value == value {
			return true
		}
	}
	return false
}

func detailRowHasRouteAction(row detailBrowserRow, domain string) bool {
	for _, action := range row.Actions {
		if route, ok := parseListRouteAction(action.Value); ok && route.Domain == domain {
			return true
		}
	}
	return false
}

func TestStyledToolRowColorsProviderStatuses(t *testing.T) {
	ok := styledToolRow(toolRow{Name: "jq", Version: "1.8.1", State: "ok", Detail: "JSON processor"}, false, true)
	if !strings.Contains(ok[1], "\033[32m") || !strings.Contains(ok[2], "\033[32m") || !strings.Contains(ok[4], "\033[36m") {
		t.Fatalf("expected ok row to color version/status/detail, got %#v", ok)
	}
	if strings.Contains(strings.Join(ok, " "), "\033[2m") {
		t.Fatalf("did not expect ok row to be dimmed, got %#v", ok)
	}

	extra := styledToolRow(toolRow{Name: "visual-studio-code", Version: "1.100.0", State: "extra", Detail: "installed but not desired"}, false, true)
	if !strings.Contains(extra[2], "\033[33m") || !strings.Contains(extra[4], "\033[33m") {
		t.Fatalf("expected extra status/detail to be warning-colored, got %#v", extra)
	}

	inactive := styledToolRow(toolRow{Name: "python", Version: "3.13.0", Wanted: "latest", State: "inactive", Detail: "Python runtime"}, true, true)
	if !strings.Contains(inactive[1], "\033[2m") || !strings.Contains(inactive[2], "\033[2m") || !strings.Contains(inactive[5], "\033[2m") {
		t.Fatalf("expected inactive version/wanted/detail to stay dimmed, got %#v", inactive)
	}
}

func TestInventoryItemHelpersColorAttentionRows(t *testing.T) {
	extraVersion := styleInventoryItemVersion("2026-05-14", plan.StatusExtra, true)
	extraDetail := styleInventoryItemDetail("installed but not desired", plan.StatusExtra, true)
	if !strings.Contains(extraVersion, "\033[33m") || !strings.Contains(extraDetail, "\033[33m") {
		t.Fatalf("expected extra inventory row fields to be warning-colored, got version=%q detail=%q", extraVersion, extraDetail)
	}
	okDetail := styleInventoryItemDetail("JSON processor", plan.StatusOK, true)
	if !strings.Contains(okDetail, "\033[36m") {
		t.Fatalf("expected ok detail to be label-colored, got %q", okDetail)
	}
}

func TestDetailBrowserModelTogglesAndRendersExpandedDetail(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Status: "ok", Summary: "short", Detail: "full detail"},
		{Title: "two", Status: "held", Summary: "summary", Detail: "second detail", Metadata: []string{"updated: jq; git", "applyability: review-only"}, Actions: []detailBrowserAction{{Value: "demo", Label: "review", Description: "inspect evidence"}}},
	}, detailBrowserState{}, false)
	model.move(1)
	if model.State.Selected != 1 {
		t.Fatalf("expected selected row 1, got %d", model.State.Selected)
	}
	model.toggleSelected()
	if !model.State.Expanded[1] {
		t.Fatalf("expected selected row to be expanded: %#v", model.State)
	}
	view := model.View()
	if !view.AltScreen {
		t.Fatal("expected detail browser to use alt screen for stable mouse coordinates")
	}
	for _, want := range []string{"Enter/Space expand", "> - held two", "[actions:1]", "[updated:2]", "[review-only]", "focused actions: a/1=review", "details", "evidence", "actions", "action 1 [press a or 1]: review", "detail: second detail", "mouse=off"} {
		if !strings.Contains(view.Content, want) {
			t.Fatalf("expected detail browser view to contain %q:\n%s", want, view.Content)
		}
	}
	coloredLines := strings.Join(detailBrowserExpandedLinesStyled(detailBrowserRow{
		Detail:   "second detail",
		Metadata: []string{"updated: jq; git"},
		Actions:  []detailBrowserAction{{Value: "demo", Label: "review", Description: "inspect evidence"}},
	}, 80, true), "\n")
	for _, want := range []string{"\033[1m\033[35mdetails", "\033[36mdetail:", "\033[1m\033[35mevidence", "\033[36mupdated:", "\033[1m\033[35mactions", "\033[36maction 1 [press a or 1]:", "\033[32mreview"} {
		if !strings.Contains(coloredLines, want) {
			t.Fatalf("expected colored detail lines to contain %q:\n%q", want, coloredLines)
		}
	}
}

func TestDetailBrowserKeepsExpandedDetailVisibleNearBottom(t *testing.T) {
	rows := []detailBrowserRow{}
	for i := 0; i < 12; i++ {
		detail := "detail"
		if i == 10 {
			detail = "description: expanded row\nidentity: manual / app / bottom\nstatus: needs-review\nnext action: open manual review"
		}
		rows = append(rows, detailBrowserRow{Title: fmt.Sprintf("row-%02d", i), Status: "ok", Detail: detail})
	}
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	model.Height = 14
	model.move(10)
	model.toggleSelected()
	view := model.View().Content
	for _, want := range []string{"row-10", "description: expanded row", "identity: manual / app / bottom", "next action: open manual review"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded bottom detail row to stay visible with %q:\n%s", want, view)
		}
	}
}

func TestDetailBrowserActionKeysReturnFocusedRowAction(t *testing.T) {
	rows := []detailBrowserRow{{
		Title:   "action row",
		Status:  "held",
		Summary: "needs action",
		Actions: []detailBrowserAction{
			{Value: "first", Label: "first action"},
			{Value: "second", Label: "second action"},
		},
	}}
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(detailBrowserModel)
	if model.State.Action != "first" {
		t.Fatalf("expected a to select first row action, got %#v", model.State)
	}

	model = newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))
	model = updated.(detailBrowserModel)
	if model.State.Action != "second" {
		t.Fatalf("expected 2 to select second row action, got %#v", model.State)
	}
}

func TestDetailBrowserFocusedActionHintFitsTerminalWidth(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{{
		Title:  "long actions",
		Status: "held",
		Actions: []detailBrowserAction{
			{Value: "one", Label: "a deliberately long action label that should not overflow the header"},
		},
	}}, detailBrowserState{}, false)
	model.Width = 36
	view := model.View().Content
	if !strings.Contains(view, "focused actions:") || !strings.Contains(view, "…") {
		t.Fatalf("expected focused action hint to be truncated to terminal width:\n%s", view)
	}
}

func TestDetailBrowserReservesFocusedActionHintLine(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{{
		Title:   "backend review",
		Status:  "drift",
		Summary: "needs action",
		Actions: []detailBrowserAction{{
			Value: "backend",
			Label: "open backend review",
		}},
	}, {
		Title:   "already aligned",
		Status:  "ok",
		Summary: "no action",
	}}, detailBrowserState{}, false)
	firstView := model.View().Content
	model.move(1)
	secondView := model.View().Content
	if !strings.Contains(firstView, "focused actions: a/1=open backend review") {
		t.Fatalf("expected first focused row to show action hint:\n%s", firstView)
	}
	if strings.Contains(secondView, "focused actions:") {
		t.Fatalf("expected second focused row to have no action hint:\n%s", secondView)
	}
	if firstIndex, secondIndex := detailViewLineIndex(firstView, "drift backend review"), detailViewLineIndex(secondView, "drift backend review"); firstIndex != secondIndex {
		t.Fatalf("expected detail rows to stay stable, got first=%d second=%d\nfirst:\n%s\nsecond:\n%s", firstIndex, secondIndex, firstView, secondView)
	}
	if firstLines, secondLines := strings.Count(firstView, "\n"), strings.Count(secondView, "\n"); firstLines != secondLines {
		t.Fatalf("expected view line count to stay stable, got first=%d second=%d\nfirst:\n%s\nsecond:\n%s", firstLines, secondLines, firstView, secondView)
	}
}

func detailViewLineIndex(text string, needle string) int {
	for index, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return index
		}
	}
	return -1
}

func TestDogfoodDetailRowsSelectManualBackendSecurityAndDashboardActions(t *testing.T) {
	manualRows := []detailBrowserRow{manualPlanDetailRow(manualPlanItem{
		Name:       "Google Sheets",
		State:      "installed",
		Action:     "needs-review",
		Confidence: "medium",
		NextStep:   "accept, edit, or ignore one explicit override after ownership review",
	})}
	if action := selectedDetailActionForKey(manualRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'})); !strings.HasPrefix(action, manualPlanDetailActionPrefix+"\taccept\tGoogle Sheets") {
		t.Fatalf("expected manual detail row to select accept action, got %q", action)
	}

	backendRows := backendDetailRows(backendPlanReport{Findings: []backendFinding{{
		Type:            "mise-backend-rewrite",
		Name:            "cargo:broot",
		RecommendedName: "github:Canop/broot",
		RewriteAllowed:  true,
	}}})
	if action, current, recommended, ok := parseBackendDetailAction(selectedDetailActionForKey(backendRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))); !ok || action != "rewrite-mise" || current != "cargo:broot" || recommended != "github:Canop/broot" {
		t.Fatalf("expected backend detail row to select rewrite action, action=%q current=%q recommended=%q ok=%v", action, current, recommended, ok)
	}

	securityRows := updateSecurityDetailRows(updateReport{Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{Provider: "brew", Kind: "cask", Name: "demo", Decision: "hold"}},
	}}})
	if action, provider, kind, name, ok := parseSecurityDetailAction(selectedDetailActionForKey(securityRows, tea.KeyPressMsg(tea.Key{Text: "2", Code: '2'}))); !ok || action != "allow-custom-rerun" || provider != "brew" || kind != "cask" || name != "demo" {
		t.Fatalf("expected security detail row to select custom allow rerun action, action=%q provider=%q kind=%q name=%q ok=%v", action, provider, kind, name, ok)
	}

	dashboardRows := updateDashboardDetailRows(updateReport{Status: plan.StatusOK}, inventoryPlanReport{}, backendPlanReport{})
	if action := selectedDetailActionForKey(dashboardRows, tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'})); action != updateHubActionUpdatesFilter {
		t.Fatalf("expected dashboard focused row to select update filter action, got %q", action)
	}
	model := newDetailBrowserModel(updateHubTitle(updateReport{Status: plan.StatusOK}), dashboardRows, detailBrowserState{}, false)
	model.PrimaryEnterAction = true
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if model.State.Action != updateHubActionUpdatesFilter {
		t.Fatalf("expected dashboard Enter to select focused primary action, got %q", model.State.Action)
	}
}

func TestUpdateHubRouterBackReturnsFromDetailToDashboard(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionLogs, updateHubActionLogs, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected router to start in logs detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back to return to dashboard, screen=%q\n%s", model.screen, model.View().Content)
	}
	if !strings.Contains(model.View().Content, "updev update ok") {
		t.Fatalf("expected dashboard view after Back:\n%s", model.View().Content)
	}
}

func TestUpdateHubRouterClearsDashboardActionAfterReturning(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected router to start on dashboard, screen=%q", model.screen)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected dashboard action to open logs detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back from logs to return to dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Down after Back to stay on dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterOpensFullReportWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionFull, updateHubActionFull, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "full" {
		t.Fatalf("expected router to open full report detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev full report") || !strings.Contains(view, "cached update report") {
		t.Fatalf("expected full report detail view:\n%s", view)
	}
}

func TestUpdateHubRouterOpensBackendTableWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "bat",
		RecommendedProvider: "mise",
		RecommendedName:     "bat",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	if model.screen != updateHubRouterTable || model.stateKey != "backends" {
		t.Fatalf("expected router to open backend table, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev backend convergence") || !strings.Contains(view, "bat") {
		t.Fatalf("expected backend table view:\n%s", view)
	}
}

func TestUpdateHubRouterRefreshesReviewPlansAsynchronously(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	initialView := model.View().Content
	if !strings.Contains(initialView, "loading - preparing manual") || !strings.Contains(initialView, "loading - preparing backend") {
		t.Fatalf("expected dashboard to show review plan loading rows:\n%s", initialView)
	}
	manualPlan := inventoryPlanReport{
		Status:         plan.StatusDrift,
		AttentionCount: 1,
		Items:          []manualPlanItem{{Name: "Vendor App", Action: "needs-review"}},
		ActionCounts:   map[string]int{"needs-review": 1},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	updated, _ := model.Update(updateHubManualPlanMsg{Report: manualPlan})
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(updateHubBackendPlanMsg{Report: backendPlan})
	model = updated.(updateHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "loading - preparing") || !strings.Contains(readyView, "needs-review=1") || !strings.Contains(readyView, "homebrew-to-mise=1") {
		t.Fatalf("expected dashboard to refresh review plan rows:\n%s", readyView)
	}
}

func TestUpdateHubRouterUpdateFilterStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" || !model.detail.PrimaryEnterAction {
		t.Fatalf("expected update filter menu inside router, screen=%q state=%q primary=%v", model.screen, model.stateKey, model.detail.PrimaryEnterAction)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:provider:brew" {
		t.Fatalf("expected Enter to open filtered update evidence, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") {
		t.Fatalf("expected filtered update detail to include provider evidence:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from filtered evidence to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}, {
		Name:   "mise",
		Status: plan.StatusOK,
		Stdout: "mise output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("updates"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:updates" {
		t.Fatalf("expected update query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "brew", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:query:brew" {
		t.Fatalf("expected query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") || strings.Contains(view, "mise output") {
		t.Fatalf("expected query-filtered update detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from query result to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterSecurityQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo", Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "danger-app",
			Decision: "hold",
			Reason:   "unique-risk",
		}, {
			Provider: "brew",
			Kind:     "cask",
			Name:     "other-app",
			Decision: "hold",
			Reason:   "other reason",
		}},
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionSecurityFilter, updateHubActionSecurityFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("security"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:security" {
		t.Fatalf("expected security query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "unique-risk", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:security:query:unique-risk" {
		t.Fatalf("expected security query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "danger-app") || strings.Contains(view, "other-app") {
		t.Fatalf("expected query-filtered security detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:security" {
		t.Fatalf("expected Back from security query result to return to security filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterWriteConfirmationStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:            "mise-backend-rewrite",
		Name:            "cargo:broot",
		RecommendedName: "github:Canop/broot",
		RewriteAllowed:  true,
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	action := backendDetailActionValue("rewrite-mise", "cargo:broot", "github:Canop/broot")
	updated, _ := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterConfirm || !strings.HasPrefix(model.stateKey, "write-confirm:") {
		t.Fatalf("expected backend write confirmation inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "Rewrite mise backend") || !strings.Contains(view, "cargo:broot") {
		t.Fatalf("expected backend confirmation view:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != updateHubActionBackends {
		t.Fatalf("expected Back from confirmation to return to backend table, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterManualEditRemainsExternal(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionManualPlan, updateHubActionManualPlan, false)
	action := manualPlanDetailActionValue("edit", "Vendor App")
	updated, cmd := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.finalAction != action || cmd == nil {
		t.Fatalf("expected manual edit to remain an external action, final=%q cmdNil=%v", model.finalAction, cmd == nil)
	}
}

func TestListHubRouterTogglesInstalledAndManualWithoutSubprogram(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "ripgrep",
			Version:  "14.1.1",
			Status:   plan.StatusOK,
		}, {
			Provider: manualProviderName,
			Kind:     "app",
			Name:     "Vendor App",
			Status:   plan.StatusDrift,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionFull, nil, false)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected installed inventory table, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionManual {
		t.Fatalf("expected Tab to switch to manual table inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev list manual") || !strings.Contains(view, "Vendor App") {
		t.Fatalf("expected manual view after Tab:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "shift+tab", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Shift+Tab to switch back to installed inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterInventoryTabSwitchesToManualInventory(t *testing.T) {
	report := updateReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "ripgrep",
			Version:  "14.1.1",
			Status:   plan.StatusOK,
		}, {
			Provider: manualProviderName,
			Kind:     "app",
			Name:     "Vendor App",
			Status:   plan.StatusDrift,
		}}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionInventoryAll, updateHubActionInventoryAll, false)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected installed inventory table, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != listHubActionManual {
		t.Fatalf("expected Tab to switch to manual inventory inside update router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev list manual") || !strings.Contains(view, "Vendor App") {
		t.Fatalf("expected manual inventory view after Tab:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "shift+tab", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected Shift+Tab to switch back to installed inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterBackReturnsToSelectorHub(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo", Items: []plan.Item{{
		Provider: "mise",
		Kind:     "tool",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionFull, nil, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.finalAction != updevActionBack {
		t.Fatalf("expected Back to quit router for selector hub, got %q", model.finalAction)
	}
}

func TestListHubRouterOpensBackendTableWithoutSubprogram(t *testing.T) {
	report := listReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "bat",
		RecommendedProvider: "mise",
		RecommendedName:     "bat",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionBackends, nil, false)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionBackends {
		t.Fatalf("expected backend table, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev backend convergence") || !strings.Contains(view, "bat") {
		t.Fatalf("expected backend table view:\n%s", view)
	}
}

func TestListHubRouterRefreshesBackendEvidenceAsynchronously(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo", Items: []plan.Item{{
		Provider: "brew",
		Kind:     "brew",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, true, updateReport{}, false, listHubActionFull, nil, false)
	initialView := model.View().Content
	if !strings.Contains(initialView, "backend evidence loading") || strings.Contains(initialView, "open backend review") {
		t.Fatalf("expected initial list view to render before backend evidence is ready:\n%s", initialView)
	}
	updated, _ := model.Update(listHubBackendPlanMsg{Report: backendPlan})
	model = updated.(listHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "backend evidence loading") || !strings.Contains(readyView, "open backend review") {
		t.Fatalf("expected backend evidence refresh to add row action:\n%s", readyView)
	}
}

func TestListHubRouterProviderFilterStaysInsideRouter(t *testing.T) {
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Providers: []plan.ProviderSummary{{
			Name:    "brew",
			Desired: 1,
			Live:    1,
		}},
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionProvider, nil, false)
	if model.screen != listHubRouterDetail || model.stateKey != "filter-menu:"+listHubActionProvider || !model.detail.PrimaryEnterAction {
		t.Fatalf("expected provider filter menu inside router, screen=%q state=%q primary=%v", model.screen, model.stateKey, model.detail.PrimaryEnterAction)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != "filter-result:provider:brew" {
		t.Fatalf("expected Enter to open provider-filtered inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "ripgrep") || strings.Contains(view, "node") {
		t.Fatalf("expected provider-filtered view to show brew rows only:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || model.stateKey != "filter-menu:"+listHubActionProvider {
		t.Fatalf("expected Back from filtered rows to return to provider menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterQueryInputStaysInsideRouter(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "unique-rip-tool",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}},
	}
	model := newListHubRouterModel(report, backendPlanReport{}, false, updateReport{}, false, listHubActionQuery, nil, false)
	if model.screen != listHubRouterInput || model.stateKey != listHubActionQuery {
		t.Fatalf("expected list query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "unique-rip", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != "filter-result:query:unique-rip" {
		t.Fatalf("expected query-filtered list inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "unique-rip-tool") || strings.Contains(view, "node") {
		t.Fatalf("expected query-filtered list view:\n%s", view)
	}
}

func TestListHubRouterSecurityCustomAllowInputsStayInsideRouter(t *testing.T) {
	report := listReport{Status: plan.StatusOK, Root: "/repo"}
	lastUpdate := updateReport{Status: plan.StatusHeld, Root: "/repo", Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "demo",
			Decision: "hold",
			Reason:   "review required",
		}},
	}}}
	model := newListHubRouterModel(report, backendPlanReport{}, false, lastUpdate, true, listHubActionSecurity, nil, false)
	action := securityDetailActionValue("allow-custom", "brew", "cask", "demo")
	updated, _ := model.handleAction(action)
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterInput || !strings.HasPrefix(model.stateKey, "write-reason:") {
		t.Fatalf("expected custom allow reason input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "reviewed provenance", Code: tea.KeyExtended}))
	model = updated.(listHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterInput || !strings.HasPrefix(model.stateKey, "write-expiry:") {
		t.Fatalf("expected custom allow expiry input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterConfirm || !strings.HasPrefix(model.stateKey, "write-confirm:") {
		t.Fatalf("expected custom allow confirmation inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "reviewed provenance") || !strings.Contains(view, "expires:") {
		t.Fatalf("expected custom allow confirmation detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || model.stateKey != listHubActionSecurity {
		t.Fatalf("expected Back from confirmation to return to security detail, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterRouteBackReturnsToOriginView(t *testing.T) {
	report := listReport{Status: plan.StatusDrift, Root: "/repo", Items: []plan.Item{{
		Provider: "brew",
		Kind:     "brew",
		Name:     "ripgrep",
		Status:   plan.StatusOK,
	}}}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionFull, nil, false)
	model.showRouteDetail(listRouteAction{Domain: listHubActionBackends, Provider: "brew", Kind: "brew", Name: "ripgrep"})
	if model.screen != listHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Back from route detail to return to origin inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestListHubRouterClearsRowActionAfterReturningToInventory(t *testing.T) {
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlan),
	}
	model := newListHubRouterModel(report, backendPlan, false, updateReport{}, false, listHubActionFull, nil, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected row action to open route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	if !model.detail.State.Expanded[0] {
		t.Fatalf("expected route detail to open expanded on the focused row, state=%#v", model.detail.State)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Back from route detail to return to inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(listHubRouterModel)
	if model.screen != listHubRouterTable || model.stateKey != listHubActionFull {
		t.Fatalf("expected Down after Back to stay in inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestInventoryItemDetailLocalizesJapaneseEvidenceAndActions(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise-candidate",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "git",
		RecommendedProvider: "mise",
		RecommendedName:     "github:git/git",
		Reason:              "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew",
		Action:              "review github:git/git as a candidate only; verify release assets, version mapping, and ownership before changing Homebrew ownership",
	}}}
	report := listReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "git",
			Category: "work",
			Detail:   "keep macOS/system git available",
			Status:   plan.StatusOK,
			Desired:  true,
			Live:     true,
		}},
		Evidence: addBackendListEvidence(listEvidenceIndex{}, backendPlan),
	}
	sections := listTableSections(report)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("expected one inventory row, got %#v", sections)
	}
	row := sections[0].Rows[0]
	for _, want := range []string{
		"説明: macOS/system git を使える状態に保つ",
		"関連 evidence: 1 件の backend evidence",
		"backend evidence: github:git/git は候補としてのみ確認します",
		"次の操作: backend 整理を開く",
	} {
		if !strings.Contains(row.Detail, want) {
			t.Fatalf("expected localized detail to contain %q:\n%s", want, row.Detail)
		}
	}
	expanded := strings.Join(reviewui.ExpandedLines(row, reviewLabels()), "\n")
	if strings.Contains(expanded, "note:") {
		t.Fatalf("did not expect localized key-value detail to fall back to note lines:\n%s", expanded)
	}
}

func TestDetailBrowserDoesNotTreatURLsAsKeyValueLines(t *testing.T) {
	lines := strings.Join(detailBrowserDetailLines("go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html", 120, false), "\n")
	if strings.Contains(lines, "https: //") {
		t.Fatalf("did not expect URL scheme to be split as a key-value line:\n%s", lines)
	}
	if !strings.Contains(lines, "detail: go言語（組み込みプラグイン）。https://mise.jdx.dev/lang/go.html") {
		t.Fatalf("expected URL to remain inside the detail text:\n%s", lines)
	}
}

func selectedDetailActionForKey(rows []detailBrowserRow, key tea.KeyPressMsg) string {
	model := newDetailBrowserModel("details", rows, detailBrowserState{}, false)
	updated, _ := model.Update(key)
	model = updated.(detailBrowserModel)
	return model.State.Action
}

func TestDetailBrowserCollapsedBadgesSummarizeActionsAndEvidence(t *testing.T) {
	row := detailBrowserRow{
		Status:  "held",
		Summary: "security review required",
		Metadata: []string{
			"updated: jq; git",
			"deferred: demo held",
			"decision: hold",
			"release assets: compatible",
			"applyability: applyable: rewrite current mise key",
		},
		Actions: []detailBrowserAction{{Value: "one", Label: "allow"}, {Value: "two", Label: "hold"}},
	}
	got := detailBrowserCollapsedSummary(row)
	for _, want := range []string{"[actions:2]", "[updated:2]", "[deferred:1]", "[decision:hold]", "[assets:compatible]", "[applyable]", "security review required"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected collapsed summary to include %q, got %q", want, got)
		}
	}
}

func TestDetailBrowserMouseClickTogglesOncePerClick(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Status: "ok", Summary: "short", Detail: "full detail"},
	}, detailBrowserState{}, false)
	model.MouseMode = browserMouseClick
	click := model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to detail row")
	}
	updated, _ := model.Update(click())
	model = updated.(detailBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected click press to select without expanding row: %#v", model.State)
	}
	release := model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if release == nil {
		t.Fatal("expected row release to map to detail row")
	}
	updated, _ = model.Update(release())
	model = updated.(detailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected matching release to expand row: %#v", model.State)
	}
	click = model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(click())
	model = updated.(detailBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected second click press not to collapse row: %#v", model.State)
	}
	release = model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = model.Update(release())
	model = updated.(detailBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected second matching release to collapse row: %#v", model.State)
	}
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to detail browser movement")
	}
}

func TestDetailBrowserFiltersRowsInPlace(t *testing.T) {
	model := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "node", Status: "ok", Summary: "runtime", Detail: "javascript"},
		{Title: "jq", Status: "held", Summary: "json", Detail: "processor"},
	}, detailBrowserState{Query: "json"}, false)
	view := model.View().Content
	if strings.Contains(view, "node") || !strings.Contains(view, "jq") || !strings.Contains(view, `filter="json"`) {
		t.Fatalf("expected filtered detail browser rows:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected detail filter to update while typing: %#v\n%s", model.State, model.View().Content)
	}
	if view := model.View().Content; strings.Index(view, "filter: jq") < 0 || strings.Index(view, "filter: jq") > strings.Index(view, "jq") {
		t.Fatalf("expected active detail filter input near the top:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected esc to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear active detail filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "jq", Code: tea.KeyExtended}))
	model = updated.(detailBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "jq" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected slash filter input to apply detail filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(detailBrowserModel)
	if model.State.Query != "" || model.State.Action != "" {
		t.Fatalf("expected back to clear detail filter before leaving: %#v", model.State)
	}
}

func TestToolTableBrowserPreservesGroupedTableAndExpandsDetail(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "A deliberately long Node.js runtime description that should expand below the grouped table row.",
		}},
	}}, detailBrowserState{}, false)
	model.MouseMode = reviewui.MouseClick
	view := model.View().Content
	for _, want := range []string{"mise / runtime", "name", "version", "wanted", "state", "detail", "node", "24.16.0", "lts"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected table browser to preserve grouped table output with %q:\n%s", want, view)
		}
	}
	rendered := model.View()
	if !rendered.AltScreen {
		t.Fatal("expected table browser to use alt screen for stable mouse coordinates")
	}
	click := rendered.OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to table row")
	}
	msg, ok := click().(toolTableMouseMsg)
	if !ok || msg.Index != 0 {
		t.Fatalf("expected row click to target index 0, got %#v", msg)
	}
	model.ToggleSelected()
	view = model.View().Content
	if !strings.Contains(view, "detail: A deliberately long Node.js runtime description") {
		t.Fatalf("expected expanded detail under table row:\n%s", view)
	}
}

func TestToolTableBrowserExpandedActionsAreSelectable(t *testing.T) {
	actions := []reviewui.Action{{
		Value:       "open-backend",
		Label:       "open backend review",
		Description: "inspect backend recommendation",
	}, {
		Value:       "open-update",
		Label:       "open update evidence",
		Description: "inspect update evidence",
	}, {
		Value:       "open-security",
		Label:       "open security review",
		Description: "inspect security evidence",
	}, {
		Value:       "open-manual",
		Label:       "open manual review",
		Description: "inspect manual evidence",
	}, {
		Value:       "open-extra",
		Label:       "open extra review",
		Description: "inspect extra evidence",
	}}
	model := newToolTableBrowserModel("updev list brew", []toolSection{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows: []toolRow{{
			Name:    "ripgrep",
			State:   "ok",
			Detail:  "description: ripgrep",
			Actions: actions,
		}},
	}}, detailBrowserState{}, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	view := model.View().Content
	if !strings.Contains(view, "> action 1 [press a or 1]: open backend review") || !strings.Contains(view, "expanded actions:") {
		t.Fatalf("expected expanded action focus in table browser:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(toolTableBrowserModel)
	view = model.View().Content
	if !strings.Contains(view, "> action 2 [press 2]: open update evidence") {
		t.Fatalf("expected cursor to move to second expanded action:\n%s", view)
	}
	for _, want := range []string{"action 3 [press 3]: open security review", "action 4 [press 4]: open manual review", "action 5 [press 5]: open extra review"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded row to preserve all actions with %q:\n%s", want, view)
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	if model.State.Action != "open-update" {
		t.Fatalf("expected Enter to run focused expanded action, got %#v", model.State)
	}
}

func TestToolTableBrowserFiltersRowsInPlace(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "javascript runtime",
		}, {
			Name:    "go",
			Version: "1.26.3",
			State:   "active",
			Detail:  "language runtime",
		}},
	}}, detailBrowserState{Query: "javascript"}, false)
	view := model.View().Content
	if !strings.Contains(view, "node") || strings.Contains(view, "go") || !strings.Contains(view, `filter="javascript"`) {
		t.Fatalf("expected filtered table browser rows:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || !strings.Contains(model.View().Content, "go") {
		t.Fatalf("expected x to clear table filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "go" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected table filter to update while typing: %#v\n%s", model.State, model.View().Content)
	}
	if view := model.View().Content; strings.Index(view, "filter: go") < 0 || strings.Index(view, "filter: go") > strings.Index(view, "mise / runtime") {
		t.Fatalf("expected active table filter input near the top:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected esc to clear active table filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.Filtering || !strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected x to clear active table filter input: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "go", Code: tea.KeyExtended}))
	model = updated.(toolTableBrowserModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "go" || strings.Contains(model.View().Content, "node") {
		t.Fatalf("expected slash filter input to apply table filter: %#v\n%s", model.State, model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(toolTableBrowserModel)
	if model.State.Query != "" || model.State.Action != "" {
		t.Fatalf("expected back to clear table filter before leaving: %#v", model.State)
	}
}

func TestBrowserModelsExposeNavigationActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{name: "back", key: tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}), want: updevActionBack},
		{name: "home", key: tea.KeyPressMsg(tea.Key{Text: "h", Code: 'h'}), want: updevActionHome},
		{name: "exit", key: tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}), want: updevActionExit},
	} {
		t.Run("detail/"+tc.name, func(t *testing.T) {
			model := newDetailBrowserModel("details", []detailBrowserRow{{Title: "one"}}, detailBrowserState{}, false)
			updated, _ := model.Update(tc.key)
			model = updated.(detailBrowserModel)
			if model.State.Action != tc.want {
				t.Fatalf("expected detail action %q, got %#v", tc.want, model.State)
			}
		})
		t.Run("table/"+tc.name, func(t *testing.T) {
			model := newToolTableBrowserModel("tools", []toolSection{{
				Name:  "mise/core",
				Title: "mise / core",
				Rows:  []toolRow{{Name: "node"}},
			}}, detailBrowserState{}, false)
			updated, _ := model.Update(tc.key)
			model = updated.(toolTableBrowserModel)
			if model.State.Action != tc.want {
				t.Fatalf("expected table action %q, got %#v", tc.want, model.State)
			}
		})
	}
}

func TestToolTableBrowserMouseClickTogglesOncePerClick(t *testing.T) {
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/runtime",
		Title: "mise / runtime",
		Rows: []toolRow{{
			Name:    "node",
			Version: "24.16.0",
			Wanted:  "lts",
			State:   "active",
			Detail:  "detail",
		}},
	}}, detailBrowserState{}, false)
	model.MouseMode = reviewui.MouseClick
	click := model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if click == nil {
		t.Fatal("expected row click to map to table row")
	}
	updated, _ := model.Update(click())
	model = updated.(toolTableBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected click press to select without expanding row: %#v", model.State)
	}
	release := model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	if release == nil {
		t.Fatal("expected row release to map to table row")
	}
	updated, _ = model.Update(release())
	model = updated.(toolTableBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected matching release to expand row: %#v", model.State)
	}
	click = model.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	updated, _ = model.Update(click())
	model = updated.(toolTableBrowserModel)
	if !model.State.Expanded[0] {
		t.Fatalf("expected second click press not to collapse row: %#v", model.State)
	}
	release = model.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	updated, _ = model.Update(release())
	model = updated.(toolTableBrowserModel)
	if model.State.Expanded[0] {
		t.Fatalf("expected second matching release to collapse row: %#v", model.State)
	}
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to table browser movement")
	}
}

func TestBrowserMouseDragDoesNotToggleAndMouseCanBeDisabled(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{
		{Title: "one", Detail: "detail"},
		{Title: "two", Detail: "detail"},
	}, detailBrowserState{}, false)
	if detail.MouseMode != browserMouseOff || detail.View().MouseMode != tea.MouseModeNone || detail.View().OnMouse != nil {
		t.Fatalf("expected mouse support to be off by default")
	}
	updated, _ := detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseWheel || detail.View().OnMouse == nil {
		t.Fatalf("expected first m to enable wheel-only mouse mode")
	}
	if detail.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft})) != nil {
		t.Fatalf("expected wheel-only mode to ignore clicks")
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseClick {
		t.Fatalf("expected second m to enable click mouse mode")
	}
	click := detail.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	updated, _ = detail.Update(click())
	detail = updated.(detailBrowserModel)
	release := detail.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 6, Button: tea.MouseLeft}))
	updated, _ = detail.Update(release())
	detail = updated.(detailBrowserModel)
	if detail.State.Expanded[0] || detail.State.Expanded[1] {
		t.Fatalf("expected drag release on another row not to toggle expansion: %#v", detail.State)
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Text: "m", Code: 'm'}))
	detail = updated.(detailBrowserModel)
	if detail.MouseMode != browserMouseOff || detail.View().MouseMode != tea.MouseModeNone || detail.View().OnMouse != nil {
		t.Fatalf("expected third m to disable detail browser mouse support")
	}

	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "one"}, {Name: "two"}},
	}}, detailBrowserState{}, false)
	table.MouseMode = reviewui.MouseClick
	click = table.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 7, Button: tea.MouseLeft}))
	tableUpdated, _ := table.Update(click())
	table = tableUpdated.(toolTableBrowserModel)
	release = table.View().OnMouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 8, Button: tea.MouseLeft}))
	tableUpdated, _ = table.Update(release())
	table = tableUpdated.(toolTableBrowserModel)
	if table.State.Expanded[0] || table.State.Expanded[1] {
		t.Fatalf("expected table drag release on another row not to toggle expansion: %#v", table.State)
	}
}

func TestToolTableBrowserScrollsBySelectedRow(t *testing.T) {
	rows := make([]toolRow, 0, 30)
	for i := 0; i < 30; i++ {
		rows = append(rows, toolRow{Name: "tool-" + string(rune('a'+i)), Version: "1.0.0", State: "active", Detail: "detail"})
	}
	model := newToolTableBrowserModel("updev list mise", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  rows,
	}}, detailBrowserState{}, false)
	model.Height = 10
	model.Move(20)
	if model.State.Selected != 20 || model.State.Offset == 0 {
		t.Fatalf("expected scroll offset to follow selected row, got selected=%d offset=%d", model.State.Selected, model.State.Offset)
	}
	before := model.State.Selected
	beforeOffset := model.State.Offset
	model.MouseMode = reviewui.MouseWheel
	wheel := model.View().OnMouse(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if wheel == nil {
		t.Fatal("expected mouse wheel to map to table scroll")
	}
	updated, _ := model.Update(wheel())
	model = updated.(toolTableBrowserModel)
	if model.State.Selected != before || model.State.Offset <= beforeOffset {
		t.Fatalf("expected wheel down to scroll without moving selection, before=%d/%d after=%d/%d", before, beforeOffset, model.State.Selected, model.State.Offset)
	}
	view := model.View().Content
	if strings.Contains(view, "tool-a") || !strings.Contains(view, "tool-u") {
		t.Fatalf("expected scrolled view around selected row:\n%s", view)
	}
}

func TestToolTableBrowserKeepsSelectedRowVisibleAcrossManySections(t *testing.T) {
	sections := []toolSection{}
	for section := 0; section < 10; section++ {
		rows := []toolRow{}
		for row := 0; row < 3; row++ {
			rows = append(rows, toolRow{
				Name:    fmt.Sprintf("tool-%d-%d", section, row),
				Version: "1.0.0",
				State:   "active",
				Detail:  "detail",
			})
		}
		sections = append(sections, toolSection{
			Name:  fmt.Sprintf("mise/section-%d", section),
			Title: fmt.Sprintf("mise / section-%d", section),
			Rows:  rows,
		})
	}
	model := newToolTableBrowserModel("updev list mise", sections, detailBrowserState{}, false)
	model.Height = 10
	model.Move(20)
	if !toolTableVisibleRows(model.VisibleSections(), model.State.Offset, model.VisibleBodyLines(), model.State.Expanded)[model.State.Selected] {
		t.Fatalf("expected selected row to be visible, selected=%d offset=%d\n%s", model.State.Selected, model.State.Offset, model.View().Content)
	}
	if !strings.Contains(model.View().Content, "tool-6-2") {
		t.Fatalf("expected visible content around selected row:\n%s", model.View().Content)
	}
}

func TestToolTableBrowserKeepsExpandedDetailVisibleNearBottom(t *testing.T) {
	rows := []toolRow{}
	for i := 0; i < 12; i++ {
		detail := "detail"
		if i == 10 {
			detail = "description: expanded row\nidentity: brew / brew / bottom\nstatus: ok - desired and installed\nlinked evidence: cached update\nnext action: open update evidence"
		}
		rows = append(rows, toolRow{Name: fmt.Sprintf("tool-%02d", i), State: "ok", Detail: detail})
	}
	model := newToolTableBrowserModel("updev list brew", []toolSection{{
		Name:  "brew/brew",
		Title: "brew / brew",
		Rows:  rows,
	}}, detailBrowserState{}, false)
	model.Height = 14
	model.Move(10)
	model.ToggleSelected()
	view := model.View().Content
	for _, want := range []string{"tool-10", "description: expanded row", "identity: brew / brew / bottom", "next action: open update evidence"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded bottom detail to stay visible with %q:\n%s", want, view)
		}
	}
}

func TestBrowserModelsClearStaleNavigationAction(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{{
		Title: "one",
	}}, detailBrowserState{Action: updevActionExit}, false)
	if detail.State.Action != "" {
		t.Fatalf("expected stale detail browser action to be cleared, got %#v", detail.State)
	}
	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "node"}},
	}}, detailBrowserState{Action: updevActionHome}, false)
	if table.State.Action != "" {
		t.Fatalf("expected stale table browser action to be cleared, got %#v", table.State)
	}
}

func TestBrowserHelpOverlayTogglesInPlace(t *testing.T) {
	detail := newDetailBrowserModel("details", []detailBrowserRow{{Title: "one"}}, detailBrowserState{}, false)
	updated, _ := detail.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	detail = updated.(detailBrowserModel)
	if !detail.Help || !strings.Contains(detail.View().Content, "expand/collapse details") {
		t.Fatalf("expected detail browser help overlay:\n%s", detail.View().Content)
	}
	updated, _ = detail.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	detail = updated.(detailBrowserModel)
	if detail.Help {
		t.Fatalf("expected detail browser help to close")
	}
	table := newToolTableBrowserModel("tools", []toolSection{{
		Name:  "mise/core",
		Title: "mise / core",
		Rows:  []toolRow{{Name: "node"}},
	}}, detailBrowserState{}, false)
	tableUpdated, _ := table.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	table = tableUpdated.(toolTableBrowserModel)
	if !table.Help || !strings.Contains(table.View().Content, "expand/collapse details") {
		t.Fatalf("expected table browser help overlay:\n%s", table.View().Content)
	}
}

func TestPrintListTextHonorsSectionLimit(t *testing.T) {
	report := listReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Limit:  1,
		Providers: []plan.ProviderSummary{
			{Name: "brew", Desired: 2, Live: 2},
		},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "one", Status: plan.StatusOK, Desired: true, Live: true},
			{Provider: "brew", Kind: "brew", Name: "two", Status: plan.StatusOK, Desired: true, Live: true},
		},
	}
	var out bytes.Buffer
	printListText(&out, report, "updev inventory", false)
	text := out.String()
	if !strings.Contains(text, "one") || strings.Contains(text, "two") || !strings.Contains(text, "... 1 more rows") {
		t.Fatalf("expected list limit to show first row and omitted count:\n%s", text)
	}
}

func TestListHubChoicesExposeFiltersAndNavigation(t *testing.T) {
	backendPlan := backendPlanReport{Findings: []backendFinding{{Name: "ripgrep", RecommendedName: "ripgrep"}}}
	choices := listHubChoices(listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra},
		},
	}, backendPlan, updateReport{}, false)
	if choices[0].Value != listHubActionFull || !choices[0].Selected {
		t.Fatalf("expected installed inventory to be the default list hub choice, got %#v", choices[0])
	}
	values := map[string]bool{}
	for _, choice := range choices {
		values[choice.Value] = true
	}
	for _, want := range []string{listHubActionAttention, listHubActionProvider, listHubActionKind, listHubActionCategory, listHubActionStatus, listHubActionQuery, listHubActionManual, listHubActionBackends, listHubActionLimited, listHubActionDetails, listHubActionFull, updevActionExit} {
		if !values[want] {
			t.Fatalf("expected list hub choice %q in %#v", want, choices)
		}
	}
}

func TestShouldRunListHubRouterIncludesQuery(t *testing.T) {
	if !shouldRunListHubRouterAction(listHubActionQuery) {
		t.Fatal("expected list query to run inside the list hub router")
	}
}

func TestDerivedListReportCanOpenManualApps(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "apps.md"), []byte("## Manual\n\n- Demo App\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := listReport{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK},
		},
	}
	manual := derivedListReport(report, listOptions{provider: "manual"})
	if len(manual.Items) != 0 || len(manual.Sections) != 1 || manual.Sections[0].Rows[0].Name != "Demo App" {
		t.Fatalf("expected derived manual report to use original root without live provider collection, got %#v", manual)
	}
}

func TestListInventoryReviewCountIncludesToolSections(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "app"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node"}, {Name: "python"}},
		}},
	}
	if got := listInventoryReviewCount(report); got != 3 {
		t.Fatalf("expected review count to include manifest rows and rich tool rows, got %d", got)
	}
}

func TestListFacetCountsIncludeToolSections(t *testing.T) {
	report := listReport{
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Category: "personal", Name: "app"},
		},
		Sections: []toolSection{{
			Name:  "mise/runtime",
			Title: "mise / runtime",
			Rows:  []toolRow{{Name: "node"}, {Name: "python"}},
		}},
	}
	if counts := listKindCounts(report); counts["cask"] != 1 || counts["tool"] != 2 {
		t.Fatalf("unexpected kind counts: %#v", counts)
	}
	if counts := listCategoryCounts(report); counts["personal"] != 1 || counts["runtime"] != 2 {
		t.Fatalf("unexpected category counts: %#v", counts)
	}
	if rows := listVisibleRowCount(report); rows != 3 {
		t.Fatalf("expected visible row count to include item and tool rows, got %d", rows)
	}
	if desc := categoryDescription("work"); !strings.Contains(desc, "included by personal") {
		t.Fatalf("expected work category description, got %q", desc)
	}
	if desc := categoryDescription("core"); !strings.Contains(desc, "core CLI") {
		t.Fatalf("expected core category description, got %q", desc)
	}
}

func TestListFilteredDetailBrowserRequiresRows(t *testing.T) {
	empty := listReport{}
	handled, exit := runListFilteredDetailBrowser("empty", empty, map[string]detailBrowserState{}, "empty", false)
	if handled || exit {
		t.Fatalf("expected empty report to fall back to text output")
	}
}

func TestDetailBrowserActionKeyIndex(t *testing.T) {
	if index, ok := detailBrowserActionKeyIndex("1"); !ok || index != 0 {
		t.Fatalf("expected key 1 to map to first action, got index=%d ok=%v", index, ok)
	}
	if _, ok := detailBrowserActionKeyIndex("0"); ok {
		t.Fatal("did not expect key 0 to map to an action")
	}
	if _, ok := detailBrowserActionKeyIndex("a"); ok {
		t.Fatal("did not expect key a to map through numeric action helper")
	}
}

func TestDetailBrowserPreservesPreformattedDetailLines(t *testing.T) {
	lines := detailBrowserExpandedLinesWithWidth(detailBrowserRow{
		Detail: "stdout line one\nstdout line two",
		Metadata: []string{
			"stderr: warning one\nwarning two",
		},
	}, 80)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"detail: stdout line one",
		"stdout line two",
		"stderr: warning one",
		"warning two",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected expanded detail to preserve %q in %#v", want, lines)
		}
	}
}

func TestListBrowserStateKeyUsesFilters(t *testing.T) {
	report := listReport{Filters: map[string]string{"provider": "mise", "query": "node"}}
	if got := listBrowserStateKey(report); got != "list:provider=mise query=node" {
		t.Fatalf("unexpected list browser state key: %q", got)
	}
}

func TestParseTranslatedTSV(t *testing.T) {
	pending := map[string]string{"brew:git": "Distributed revision control"}
	got := parseTranslatedTSV([]byte("noise\nBEGIN_TSV\nbrew:git\t分散バージョン管理\nEND_TSV\n"), pending)
	if got["brew:git"] != "分散バージョン管理" {
		t.Fatalf("unexpected translation parse result: %#v", got)
	}
}

func TestListTranslationDisabledByConfigSkipsExplicitRequest(t *testing.T) {
	t.Setenv("UPDEV_DESCRIPTION_TRANSLATION", "off")
	update := maybeUpdateListTranslations(listOptions{format: "text", translateNow: true}, listReport{})
	if !update.Attempted || update.Changed || !strings.Contains(update.Message, "disabled") {
		t.Fatalf("expected disabled translation message, got %#v", update)
	}
}
