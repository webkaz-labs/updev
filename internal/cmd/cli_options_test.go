package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/support"
)

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
	if report.Major != 0 || report.Minor != 7 || report.Patch != 0 || report.Contract != "pre_stable" {
		t.Fatalf("unexpected version semantics: %#v", report)
	}
}

func TestBuildSupportReportFiltersSupportLabels(t *testing.T) {
	report := buildSupportReport(supportOptions{format: "json", surface: "provider", label: support.LabelExperimental})
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
	if opts.surface != "command" || opts.label != "compatibility" || opts.format != "json" {
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
	printSupportText(&builder, buildSupportReport(supportOptions{format: "text", surface: "provider"}), false)
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
