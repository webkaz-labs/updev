package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
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

func stringPtr(value string) *string {
	return &value
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
	if got := defaultRoot(); got != nested {
		t.Fatalf("expected cwd default without parent marker climb, got %q want %q", got, nested)
	}
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
	if report.Major != 0 || report.Minor != 5 || report.Patch != 8 || report.Contract != "pre_stable" {
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

func TestPlainLastInventoryDetailsStayCachedReportOnly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{
			Status: plan.StatusOK,
			Items: []plan.Item{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "ripgrep",
				Version:  "15.1.0",
				Status:   plan.StatusOK,
				Detail:   "search tool",
				Desired:  true,
				Live:     true,
			}},
		},
	}
	var out bytes.Buffer
	printLastInventorySection(&out, report, lastReportOptions{section: "inventory", provider: "brew", query: "ripgrep", details: true}, false)
	text := out.String()
	if strings.Contains(text, "backend evidence:") || strings.Contains(text, "backend 整理") {
		t.Fatalf("expected plain last inventory details to avoid backend evidence injection:\n%s", text)
	}
	if !strings.Contains(text, "ripgrep") {
		t.Fatalf("expected cached inventory row:\n%s", text)
	}
}

func TestDryRunUpdateReportDoesNotReplaceLastUpdateEvidence(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	realPath := saveLastUpdateReport(updateReport{
		Status: plan.StatusHeld,
		Root:   "/repo",
		Steps: []updateStep{{
			Name:    "brew",
			Status:  plan.StatusOK,
			Updated: []string{"jq 1.7 -> 1.8.1"},
		}},
	})
	dryPath := saveLastUpdateReport(updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		DryRun: true,
		Steps:  []updateStep{{Name: "brew", Status: plan.StatusOK}},
	})
	if realPath == "" || dryPath == "" || realPath == dryPath {
		t.Fatalf("expected separate real and dry-run report paths, real=%q dry=%q", realPath, dryPath)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected real last update report to remain loadable")
	}
	if entry.Report.DryRun || entry.Report.Status != plan.StatusHeld || len(entry.Report.Steps) != 1 || len(entry.Report.Steps[0].Updated) != 1 {
		t.Fatalf("expected dry-run report not to replace real update evidence, got %#v", entry.Report)
	}
	if !strings.Contains(dryPath, "last-dry-run.json") {
		t.Fatalf("expected dry-run report path, got %q", dryPath)
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
	t.Setenv("UPDEV_ROOT", "")
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
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
	enableManualMarkdownCompat(t, root)
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

func enableManualMarkdownCompat(t *testing.T, root string) {
	t.Helper()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nmarkdown_compat = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
}

func enableBrewfileWriteMode(t *testing.T, root string, mode string) {
	t.Helper()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[brewfile]\nwrite_mode = \""+mode+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
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

func TestListUsesStaleInventoryCacheForFastInitialDisplay(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
	root := t.TempDir()
	entry := inventoryCacheEntry{
		Version:   inventoryCacheVersion,
		Root:      root,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Report: plan.Report{
			Status: plan.StatusOK,
			Root:   root,
			Providers: []plan.ProviderSummary{
				{Name: "mise", Supported: true, Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "mise", Kind: "tool", Category: "runtime", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	saveInventoryCache(entry)
	result := collectInventoryCachedWithOptions(context.Background(), root, false, 0, inventoryOptions{})
	if !result.Cached {
		t.Fatal("expected stale inventory cache to be reused for list initial display")
	}
	if got := result.Report.Root; got != root {
		t.Fatalf("unexpected cached root: %q", got)
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
	enableBrewfileWriteMode(t, root, "template")
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

func TestMutationReportHoldsDefaultBrewfileWriteWithoutOptIn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("UPDEV_ROOT", root)
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
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
	if report.Status != plan.StatusHeld || report.Changed || report.Snapshot != nil || !strings.Contains(report.Reason, "Brewfile writes are disabled") {
		t.Fatalf("expected default Brewfile write to hold before snapshot, got %#v", report)
	}
}

func TestMutationReportAllowsConfiguredBrewfileWriteMode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("UPDEV_ROOT", root)
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[brewfile]\nwrite_mode = \"template\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
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
	if report.Status != plan.StatusOK || !report.Changed || report.Snapshot == nil {
		t.Fatalf("expected configured Brewfile write to succeed, got %#v", report)
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
	enableBrewfileWriteMode(t, root, "template")
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
