package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/inventoryrun"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/support"
	"github.com/webkaz-labs/updev/internal/syncreport"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevconfig"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type options struct {
	format        string
	root          string
	refresh       bool
	includeVSCode bool
	manifestOnly  bool
	dependencies  bool
}

const (
	usageExitCode = 64
	toolName      = "updev"
	toolVersion   = "v0.7.8"
)

const inventoryCacheVersion = inventoryrun.CacheVersion
const supportReportSchemaVersion = support.ReportSchemaVersion

var filterSummaryKeys = []string{"provider", "kind", "category", "status", "query", "limit", "include_vscode"}

var embeddedAgentSkillDoc = fallbackAgentSkillDoc
var embeddedAgentUsageDoc = fallbackAgentUsageDoc
var defaultLanguageValue string
var defaultLanguageOnce sync.Once

type versionReport struct {
	SchemaVersion int    `json:"schema_version"`
	Tool          string `json:"tool"`
	Version       string `json:"version"`
	Major         int    `json:"major"`
	Minor         int    `json:"minor"`
	Patch         int    `json:"patch"`
	Contract      string `json:"contract"`
}

type startupProgress = reviewui.StartupProgress

type updevConfig = updevconfig.Config
type updevSecurityConfig = updevconfig.SecurityConfig
type updevHomebrewSecurityConfig = updevconfig.HomebrewSecurityConfig
type updevMiseSecurityConfig = updevconfig.MiseSecurityConfig
type updevVSCodeSecurityConfig = updevconfig.VSCodeSecurityConfig
type updevProvidersConfig = updevconfig.ProvidersConfig
type updevUpdateConfig = updevconfig.UpdateConfig
type updevMiseBumpUpdateConfig = updevconfig.MiseBumpUpdateConfig
type updevUIConfig = updevconfig.UIConfig
type updevSourcesConfig = updevconfig.SourcesConfig
type updevBrewfileConfig = updevconfig.BrewfileConfig
type updevInventoryConfig = updevconfig.InventoryConfig
type updevInventoryManualConfig = updevconfig.InventoryManualConfig
type updevInventoryAgentConfig = updevconfig.InventoryAgentConfig
type updevInventoryReportConfig = updevconfig.InventoryReportConfig
type updevBackendsConfig = updevconfig.BackendsConfig

func loadUpdevConfig() updevConfig {
	return updevconfig.Load()
}

func updevConfigPath() string {
	return updevconfig.ConfigPath()
}

func truthyEnv(name string) bool {
	return updevconfig.TruthyEnv(name)
}

func boolEnv(name string) (bool, bool) {
	return updevconfig.BoolEnv(name)
}

func parseUpdevConfigTOML(data string) updevConfig {
	return updevconfig.ParseTOML(data)
}

func configuredEnvString(defaultValue string, envName string) string {
	return updevconfig.ConfiguredEnvString(defaultValue, envName)
}

func configuredNonNegativeInt(defaultValue int, configured *int, envName string) int {
	return updevconfig.ConfiguredNonNegativeInt(defaultValue, configured, envName)
}

func configuredNonNegativeFloat(defaultValue float64, configured *float64, envName string) float64 {
	return updevconfig.ConfiguredNonNegativeFloat(defaultValue, configured, envName)
}

func parseBoolValue(value string) (bool, bool) {
	return updevconfig.ParseBoolValue(value)
}

func parseStringArray(value string) []string {
	return updevconfig.ParseStringArray(value)
}

func stripTOMLComment(line string) string {
	return updevconfig.StripTOMLComment(line)
}

func validBrewfileDesiredMode(value string) bool {
	return updevconfig.ValidBrewfileDesiredMode(value)
}

func validBrewfileWriteMode(value string) bool {
	return updevconfig.ValidBrewfileWriteMode(value)
}

func validUpdateSecurityMode(value string) bool {
	return updevconfig.ValidUpdateSecurityMode(value)
}

func validMiseBumpMode(value string) bool {
	return updevconfig.ValidMiseBumpMode(value)
}

func validUIInteractiveMode(value string) bool {
	return updevconfig.ValidUIInteractiveMode(value)
}

func defaultLanguage() string {
	defaultLanguageOnce.Do(func() {
		configured := ""
		if value := loadUpdevConfig().UI.Language; value != nil {
			configured = *value
		}
		defaultLanguageValue = i18n.DefaultLanguage(configured)
	})
	return defaultLanguageValue
}

func tr(en string, ja string) string {
	return i18n.Pick(defaultLanguage(), en, ja)
}

func newStartupProgress(input io.Reader, w io.Writer, format string, message string) startupProgress {
	return reviewui.NewStartupProgress(shouldShowStartupProgress(input, w, format), w, message)
}

func shouldShowStartupProgress(input io.Reader, w io.Writer, format string) bool {
	if value, ok := boolEnv("UPDEV_PROGRESS"); ok {
		if !value {
			return false
		}
	} else if configured := loadUpdevConfig().UI.Progress; configured != nil && !*configured {
		return false
	}
	if format != "text" {
		return false
	}
	return isTerminal(input) && isTerminal(w)
}

func inventoryProgressMessage(lang string, refresh bool) string {
	return reviewui.InventoryProgressMessage(lang, refresh)
}

func safetyProgressMessage(lang string) string {
	return reviewui.SafetyProgressMessage(lang)
}

func descriptionTranslationProgressMessage(lang string) string {
	return reviewui.DescriptionTranslationProgressMessage(lang)
}

func reviewPlanProgressMessage(lang string) string {
	return reviewui.ReviewPlanProgressMessage(lang)
}

type inventoryOptions struct {
	IncludeVSCode bool
}

type inventoryCacheEntry = inventoryrun.CacheEntry
type inventoryResult = inventoryrun.Result
type supportOptions = support.Options
type supportReport = support.Report

func collectInventory(ctx context.Context, root string, local runner.Local) plan.Report {
	return collectInventoryWithOptions(ctx, root, local, inventoryOptions{IncludeVSCode: includeVSCodeExtensionsByDefault()})
}

func collectInventoryWithOptions(ctx context.Context, root string, local runner.Local, opts inventoryOptions) plan.Report {
	return inventoryrun.Collect(ctx, root, local, inventoryRunOptions(root, opts))
}

func collectInventoryCached(ctx context.Context, root string, refresh bool, maxAge time.Duration) inventoryResult {
	return collectInventoryCachedWithOptions(ctx, root, refresh, maxAge, inventoryOptions{IncludeVSCode: includeVSCodeExtensionsByDefault()})
}

func collectInventoryCachedWithOptions(ctx context.Context, root string, refresh bool, maxAge time.Duration, opts inventoryOptions) inventoryResult {
	return inventoryrun.CollectCached(ctx, root, refresh, maxAge, inventoryRunOptions(root, opts))
}

func loadInventoryCache(root string, maxAge time.Duration, opts inventoryOptions) (inventoryCacheEntry, bool) {
	return inventoryrun.LoadCache(root, maxAge, inventoryRunOptions(root, opts))
}

func saveInventoryCache(entry inventoryCacheEntry) {
	inventoryrun.SaveCache(entry, loadUpdevConfig().Inventory.StateDir)
}

func inventoryCachePath(root string) string {
	return inventoryrun.CachePath(root, loadUpdevConfig().Inventory.StateDir)
}

func inventoryRunOptions(root string, opts inventoryOptions) inventoryrun.Options {
	return inventoryrun.Options{
		IncludeVSCode:        opts.IncludeVSCode,
		UseHomeBrewfile:      shouldUseHomeBrewfile(root),
		UseNativeMiseDesired: shouldUseNativeMiseDesired(root),
		StateDir:             loadUpdevConfig().Inventory.StateDir,
	}
}

func shouldUseHomeBrewfile(root string) bool {
	mode := "auto"
	if configured := loadUpdevConfig().Brewfile.Desired; configured != nil {
		mode = strings.ToLower(strings.TrimSpace(*configured))
	}
	switch mode {
	case "home":
		return true
	case "root", "template", "disabled":
		return false
	default:
		return filepath.Clean(root) == filepath.Clean(defaultRoot())
	}
}

func shouldUseNativeMiseDesired(root string) bool {
	cleanedRoot := filepath.Clean(root)
	cleanedDefault := filepath.Clean(defaultRoot())
	return cleanedRoot == cleanedDefault || strings.HasPrefix(cleanedRoot, cleanedDefault+string(os.PathSeparator))
}

func resolveUpdevConfigPath(root string, path string) string {
	return updevpath.Resolve(root, path)
}

func sortReport(report *plan.Report) {
	inventoryrun.SortReport(report)
}

func parseSupportOptions(args []string) (supportOptions, error) {
	opts := supportOptions{Format: "text", Surface: "all"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.Format = args[i+1]
			i++
		case "--surface":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--surface requires a value")
			}
			opts.Surface = args[i+1]
			i++
		case "--label":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--label requires a value")
			}
			opts.Label = args[i+1]
			i++
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.Format != "text" && opts.Format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.Format)
	}
	if !support.ValidSurface(opts.Surface) {
		return opts, fmt.Errorf("unsupported support surface: %s", opts.Surface)
	}
	if !support.ValidLabel(opts.Label) {
		return opts, fmt.Errorf("unsupported support label: %s", opts.Label)
	}
	return opts, nil
}

func runSupport(opts supportOptions) int {
	report := buildSupportReport(opts)
	if opts.Format == "json" {
		return encodeJSON(report)
	}
	printSupportText(os.Stdout, report, textui.ColorEnabled())
	return 0
}

func buildSupportReport(opts supportOptions) supportReport {
	return support.BuildReport(toolName, toolVersion, opts)
}

func printSupportText(w io.Writer, report supportReport, color bool) {
	support.PrintText(w, report, color)
}

func securityScanProgressMessage(lang string) string {
	return reviewui.SecurityScanProgressMessage(lang)
}

func securityReviewProgressMessage(lang string) string {
	return reviewui.SecurityReviewProgressMessage(lang)
}

func syncProgressMessage(lang string, refresh bool) string {
	return reviewui.SyncProgressMessage(lang, refresh)
}

func mutationProgressMessage(lang string, action string) string {
	return reviewui.MutationProgressMessage(lang, action)
}

func Run(args []string) int {
	var err error
	args, err = applyGlobalOptions(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	if len(args) > 0 && args[0] == "--print-explicit-formulas" {
		root, _, err := parseRootOption(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runPrintExplicitFormulae(root, runner.Local{})
	}
	if len(args) > 0 && shouldDelegate(args) {
		return runLegacy(args)
	}
	if len(args) > 0 && isHelpAlias(args[0]) {
		printUsage()
		return 0
	}
	if len(args) > 0 && isVersionAlias(args[0]) {
		return runVersion(args[1:])
	}
	command := "update"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "update":
		opts, err := parseUpdateOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runUpdate(opts, runner.Local{})
	case "sync":
		opts, err := parseSyncOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSync(opts)
	case "add", "remove":
		opts, err := parseMutationOptions(command, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runMutation(opts)
	case "edit":
		return runEdit(args)
	case "rollback":
		opts, err := parseRollbackOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runRollback(opts)
	case "backends":
		return runBackends(args)
	case "doctor":
		return runDoctor(args)
	case "support":
		opts, err := parseSupportOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSupport(opts)
	case "fix":
		return runFix(args)
	case "security":
		return runSecurity(args)
	case "last", "report":
		opts, err := parseLastReportOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runLastReport(opts)
	case "inventory", "list", "ls", "hub":
		if command == "inventory" && len(args) > 0 && args[0] == "scan" {
			opts, err := parseInventoryScanOptions(args[1:])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return usageExitCode
			}
			return runInventoryScan(opts)
		}
		if command == "inventory" && len(args) > 0 && args[0] == "render" {
			opts, err := parseInventoryRenderOptions(args[1:])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return usageExitCode
			}
			return runInventoryRender(opts)
		}
		if command == "inventory" && len(args) > 0 && (args[0] == "plan" || args[0] == "check") {
			opts, err := parseInventoryPlanOptions(args[1:])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return usageExitCode
			}
			return runInventoryPlan(opts)
		}
		if command == "inventory" && len(args) > 0 && args[0] == "review" {
			opts, err := parseInventoryReviewOptions(args[1:])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return usageExitCode
			}
			return runInventoryReview(opts)
		}
		listCommand := command
		fastList := hasFastListFlag(args)
		args = stripFastListFlag(args)
		opts, err := parseListOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		if normalizeListCommand(listCommand) == "list" && fastList {
			opts.title = "updev list --fast"
		} else if normalizeListCommand(listCommand) == "list" {
			opts.title = "updev list"
		} else if command == "hub" {
			opts.title = "updev hub"
			opts.hub = true
			opts.tui = true
		}
		return runList(opts)
	case "", "status", "st", "check", "ck", "plan":
		command = normalizeReadOnlyCommand(command)
		opts, err := parseOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runReadOnly(command, opts)
	case "brewfile":
		root, brewfileArgs, err := parseRootOption(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return brewfile.Run(ctx, root, brewfileArgs)
	case "legacy":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: updev legacy <legacy-command> ...")
			return usageExitCode
		}
		return runLegacy(args)
	case "version":
		return runVersion(args)
	case "skill":
		return runAgentSkill(args)
	case "help":
		if len(args) > 0 && args[0] == "agent" {
			return runAgentHelp(args[1:])
		}
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "updev: unknown command %q\n", command)
		printUsage()
		return usageExitCode
	}
}

func isVersionAlias(arg string) bool {
	return arg == "--version" || arg == "-v"
}

func isHelpAlias(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func normalizeListCommand(command string) string {
	if command == "ls" {
		return "list"
	}
	return command
}

func normalizeReadOnlyCommand(command string) string {
	switch command {
	case "st":
		return "status"
	case "ck":
		return "check"
	default:
		return command
	}
}

func runVersion(args []string) int {
	format := "text"
	if len(args) > 0 {
		if len(args) == 2 && args[0] == "--format" {
			format = args[1]
		} else {
			fmt.Fprintln(os.Stderr, "usage: updev version [--format text|json]")
			return usageExitCode
		}
	}
	report := buildVersionReport()
	switch format {
	case "json":
		return encodeJSON(report)
	case "text":
		fmt.Printf("%s %s\n", report.Tool, report.Version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unsupported format: %s\n", format)
		return usageExitCode
	}
}

func buildVersionReport() versionReport {
	major, minor, patch := parseToolVersion(toolVersion)
	contract := "stable"
	if major == 0 {
		contract = "pre_stable"
	}
	return versionReport{
		SchemaVersion: 1,
		Tool:          toolName,
		Version:       toolVersion,
		Major:         major,
		Minor:         minor,
		Patch:         patch,
		Contract:      contract,
	}
}

func SetAgentDocs(skill string, usage string) {
	skill = strings.TrimSpace(skill)
	usage = strings.TrimSpace(usage)
	if skill != "" {
		embeddedAgentSkillDoc = skill
	}
	if usage != "" {
		embeddedAgentUsageDoc = usage
	}
}

func runAgentSkill(args []string) int {
	full := false
	for _, arg := range args {
		switch arg {
		case "--full":
			full = true
		case "--help", "-h":
			fmt.Fprintln(os.Stdout, "usage: updev skill [--full]")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "unknown option: %s\n", arg)
			fmt.Fprintln(os.Stderr, "usage: updev skill [--full]")
			return usageExitCode
		}
	}
	fmt.Fprintln(os.Stdout, renderAgentSkillDoc(full))
	return 0
}

func runAgentHelp(args []string) int {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown option: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: updev help agent")
		return usageExitCode
	}
	fmt.Fprintln(os.Stdout, renderAgentUsageDoc())
	return 0
}

func renderAgentSkillDoc(full bool) string {
	if !full {
		return embeddedAgentSkillDoc
	}
	return embeddedAgentSkillDoc + "\n\n---\n\n" + embeddedAgentUsageDoc
}

func renderAgentUsageDoc() string {
	return embeddedAgentUsageDoc
}

const fallbackAgentSkillDoc = `# updev

Use updev to inspect and maintain developer-machine package/tool state,
inventory, and security review queues. Run "updev help agent" for the detailed
agent workflow guide.`

const fallbackAgentUsageDoc = `# updev agent usage

Start with read-only commands, use --format json for machine decisions, and do
not run mutation commands unless the user explicitly asks.`

func parseToolVersion(version string) (int, int, int) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return 0, 0, 0
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])
	return major, minor, patch
}

func applyGlobalOptions(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--config requires a value")
			}
			if err := os.Setenv("UPDEV_CONFIG", args[i+1]); err != nil {
				return out, err
			}
			i++
		case "--no-color":
			if err := os.Setenv("NO_COLOR", "1"); err != nil {
				return out, err
			}
		default:
			out = append(out, args[i])
		}
	}
	return out, nil
}

func parseRootOption(args []string) (string, []string, error) {
	root := defaultRoot()
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--root":
			if i+1 >= len(args) {
				return root, out, fmt.Errorf("--root requires a value")
			}
			root = args[i+1]
			i++
		default:
			out = append(out, args[i])
		}
	}
	return root, out, nil
}

func shouldDelegate(args []string) bool {
	switch args[0] {
	case "--translate-worker":
		return true
	default:
		return false
	}
}

func runPrintExplicitFormulae(root string, commandRunner runner.Runner) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names, err := (brew.Provider{Root: root, Runner: commandRunner}).ExplicitFormulae(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return 0
}

func hasFastListFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--fast" || arg == "--go" {
			return true
		}
	}
	return false
}

func stripFastListFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--fast" || arg == "--go" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func parseOptions(args []string) (options, error) {
	opts := options{format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--refresh", "-r":
			opts.refresh = true
		case "--include-vscode":
			opts.includeVSCode = true
		case "--manifest-only":
			opts.manifestOnly = true
		case "--dependencies":
			opts.dependencies = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runReadOnly(command string, opts options) int {
	if opts.dependencies {
		return runDependencyCheck(dependencyOptions{command: "check", format: opts.format, root: opts.root}, runner.Local{})
	}
	if opts.manifestOnly {
		return runManifestOnlyCheck(command, opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, inventoryProgressMessage(defaultLanguage(), opts.refresh))
	}
	progress.Start()
	result := collectInventoryCachedWithOptions(ctx, opts.root, opts.refresh, inventoryCacheMaxAge, inventoryOptions{IncludeVSCode: opts.includeVSCode || includeVSCodeExtensionsByDefault()})
	progress.Done()
	report := result.Report
	sortReport(&report)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		result.Report = report
		printReadOnlyText(os.Stdout, command, result)
	}
	if report.Status == plan.StatusError {
		return 1
	}
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func runManifestOnlyCheck(command string, opts options) int {
	report := plan.Report{Status: plan.StatusOK, Root: opts.root, Providers: []plan.ProviderSummary{}}
	inventoryannotate.AnnotateMiseManifestIssues(&report, opts.root)
	sortReport(&report)
	result := inventoryResult{Report: report}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printReadOnlyText(os.Stdout, command, result)
	}
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func encodeJSON(value any) int {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func printReadOnlyText(w io.Writer, command string, result inventoryResult) {
	report := result.Report
	color := textui.ColorEnabled()
	title := "updev " + command
	if command == "" {
		title = "updev status"
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(title, color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("root:", color), report.Root)
	if result.Cached {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("cache:", "キャッシュ:"), color), textui.StyleCount(textui.FriendlyAge(time.Since(result.CreatedAt))+" old", color), textui.StyleDim(tr("(use --refresh for a fresh read)", "(再取得は --refresh)"), color))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, textui.StyleHeading("providers", color))
	rows := make([][]string, 0, len(report.Providers))
	for _, provider := range report.Providers {
		status := string(plan.ProviderStatus(provider))
		rows = append(rows, []string{
			textui.StyleName(provider.Name, color),
			textui.StyleCount(fmt.Sprint(provider.Desired), color),
			textui.StyleCount(fmt.Sprint(provider.Live), color),
			styleDriftCount(provider.Missing, color),
			styleDriftCount(provider.Extra, color),
			textui.StyleStatus(status, color),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "name", Min: 6, Max: 12},
		{Header: "desired", Min: 7, Max: 8},
		{Header: "live", Min: 4, Max: 8},
		{Header: "missing", Min: 7, Max: 8},
		{Header: "extra", Min: 5, Max: 8},
		{Header: "status", Min: 6, Max: 12},
	}, rows, color)
	reviewReport := buildListReport(inventoryResult{Report: report}, listOptions{root: report.Root})
	if summary := listCategorySummary(reviewReport, color); summary != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(tr("categories", "categories"), color), summary)
		if meanings := listCategoryMeaningSummary(reviewReport, color); meanings != "" {
			fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("meaning:", "意味:"), color), meanings)
		}
	}
	if len(report.Items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("changes", color))
	changeRows := [][]string{}
	for _, item := range report.Items {
		if item.Status == plan.StatusOK {
			continue
		}
		changeRows = append(changeRows, []string{
			textui.StyleName(item.Provider, color),
			item.Kind,
			item.Name,
			textui.StyleStatus(string(item.Status), color),
		})
	}
	if len(changeRows) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no changes", "変更なし"), color))
		return
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "provider", Min: 8, Max: 12},
		{Header: "kind", Min: 8, Max: 12},
		{Header: "name", Min: 12, Max: 36},
		{Header: "status", Min: 7, Max: 9},
	}, changeRows, color)
}

func truncate(text string, width int) string {
	return textui.Truncate(text, width)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  updev [--config file] [--no-color] ...

Human interactive entry points:
  updev                         # [interactive] update workflow, then dashboard on TTY
  updev list                    # [interactive] installed inventory browser on TTY
  updev hub                     # [interactive] full list menu selector on TTY
  updev last                    # [interactive] reopen cached update dashboard on TTY

Agent/script-safe output:
  updev --plain                 # [scriptable] text update summary, never opens TUI
  updev list --plain            # [scriptable] text inventory list, never opens TUI
  updev last --plain            # [scriptable] cached text summary, never opens TUI
  updev <command> --format json # [scriptable] machine-readable output, never opens TUI

Commands:
  updev -h | --help             # help
  updev update [--dry-run] [--security warn|strict|off] [--include-vscode] [--policy file] [--inventory fast|legacy] [--interactive|--no-interactive|--plain] [--format text|json]
  updev sync [--format text|json]
  updev add [--provider brew|mise] [--kind brew|cask|tap|vscode|tool] [--category work|personal] [--version version] <name>
  updev remove [--provider brew|mise] [--kind brew|cask|tap|vscode|tool] <name>
  updev edit [--provider brew|mise]
  updev rollback [--token token] [--format text|json]
  updev fix mise [--dry-run|--apply] [--format text|json]
  updev backends <doctor|plan> [--format text|json]
  updev doctor dependencies [--ledger file] [--format text|json]
  updev doctor support [--surface provider|command|report|inventory_source|all] [--label supported_preview|experimental|compatibility|deferred] [--format text|json]
  updev last [--section summary|updates|security|inventory|logs|full] [--details] [--interactive|--no-interactive|--plain] [--format text|json]
  updev hub [inventory/list options]  # full menu selector for list views
  updev inventory [--refresh] [--include-vscode] [--provider name] [--kind kind] [--status status] [--query text] [--limit n] [--details] [--interactive|--no-interactive|--plain] [--format text|json]
  updev inventory scan [--provider manual] [--format text|json]
  updev inventory plan [--provider manual] [--action action] [--query text] [--limit n] [--format text|json]
  updev inventory check [--provider manual] [--action action] [--query text] [--limit n] [--format text|json]
  updev inventory review [--provider manual] [--action action] [--query text] [--limit n] [--format text|json]
  updev inventory render [--report manual-apps] [--format text|json]
  updev list --fast ...         # accepted alias for inventory-style options
  updev ls ...                  # alias for list
  updev st ...                  # alias for status
  updev ck ...                  # alias for check
  updev security scan [--refresh] [--include-vscode] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--policy file] [--format text|json]
  updev security review [--refresh] [--include-vscode] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--decision allow|review|hold|block] [--kind name] [--name text] [--policy file] [--format text|json]
  updev security gate [--provider brew|mise|vscode|all] [--include-vscode] [--policy file] [--format text|json]
  updev security policy [--path file] [--format text|json]
  updev security policy cleanup [--apply] [--path file] [--format text|json]
  updev status [--refresh] [--include-vscode] [--format text|json]
  updev check [--refresh] [--include-vscode] [--manifest-only] [--dependencies] [--format text|json]
  updev plan [--refresh] [--include-vscode] [--format text|json]
  updev version [--format text|json]
  updev --version | -v
  updev support [--surface provider|command|report|inventory_source|all] [--label supported_preview|experimental|compatibility|deferred] [--format text|json]
  updev skill [--full]
  updev help agent
  updev brewfile [--root path] <add|remove|has|check|sync> ...

Notes:
  TTY text output may open an interactive dashboard/browser unless --plain or --no-interactive is set.
  Use --plain for stable human-readable logs and --format json for machine-readable output.
  Global options: --config file, --no-color, --lang en|ja.
  Configuration is optional; defaults are used when no updev.toml exists.
  Agent guidance is available from updev skill and updev help agent.
  Exit codes: 0 ok, 1 error, 2 drift.

Unknown commands fail fast and show this help.`)
}

func runLegacy(args []string) int {
	legacy := os.Getenv("UPDEV_LEGACY")
	if legacy == "" {
		legacy = filepath.Join(defaultRoot(), "tools", "updev", "legacy", "updev.py")
	}
	if _, err := os.Stat(legacy); err != nil {
		fmt.Fprintf(os.Stderr, "updev: legacy command is not available for %q\n", strings.Join(args, " "))
		return 1
	}
	command := exec.Command(legacy, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func defaultRoot() string {
	return updevpath.DefaultRoot(configuredRoot(loadUpdevConfig()))
}

func configuredRoot(config updevConfig) string {
	if config.Sources.Root == nil {
		return ""
	}
	root := strings.TrimSpace(*config.Sources.Root)
	if root == "" || strings.EqualFold(root, "auto") {
		return ""
	}
	return updevpath.ResolveConfigRelative(root, updevConfigPath())
}

func resolveSourceRootConfigPath(path string) string {
	return updevpath.ResolveConfigRelative(path, updevConfigPath())
}

const syncReportSchemaVersion = syncreport.SchemaVersion

type syncOptions struct {
	format  string
	root    string
	refresh bool
}

type syncReport = syncreport.Report
type syncEntry = syncreport.Entry
type syncGuidance = syncreport.Guidance

func parseSyncOptions(args []string) (syncOptions, error) {
	opts := syncOptions{format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--refresh", "-r":
			opts.refresh = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runSync(opts syncOptions) int {
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, syncProgressMessage(defaultLanguage(), opts.refresh))
	}
	progress.Start()
	report := buildSyncReport(context.Background(), opts)
	progress.Done()
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSyncText(os.Stdout, report, textui.ColorEnabled())
	}
	return updateExitCode(report.Status)
}

func buildSyncReport(ctx context.Context, opts syncOptions) syncReport {
	result := collectInventoryCached(ctx, opts.root, opts.refresh, inventoryCacheMaxAge)
	return syncreport.Build(result.Report, result.Cached, result.CreatedAt, manualLocalOnlyCaskFunc(manualAppIndex(opts.root)), defaultLanguage())
}

func syncEntriesFromInventory(inventory plan.Report) []syncEntry {
	return syncreport.EntriesFromInventory(inventory, manualLocalOnlyCaskFunc(manualAppIndex(inventory.Root)), defaultLanguage())
}

func syncReportStatus(inventory plan.Report, entries []syncEntry) plan.Status {
	return syncreport.Status(inventory, entries)
}

func syncReasonForItem(item plan.Item, related map[string]plan.Item) string {
	return syncreport.ReasonForItem(item, related, nil)
}

func syncReasonForItemWithManual(item plan.Item, related map[string]plan.Item, manualIndex map[string]toolRow) string {
	return syncreport.ReasonForItem(item, related, manualLocalOnlyCaskFunc(manualIndex))
}

func enrichSyncEntry(entry *syncEntry, item plan.Item, related map[string]plan.Item) {
	syncreport.EnrichEntry(entry, item, related, defaultLanguage())
}

func syncGuidanceForItem(reason string, item plan.Item, related map[string]plan.Item) syncGuidance {
	return syncreport.GuidanceForItem(reason, item, related, defaultLanguage())
}

func missingSyncGuidance(item plan.Item) syncGuidance {
	return syncreport.MissingGuidance(item, defaultLanguage())
}

func extraSyncGuidance(item plan.Item) syncGuidance {
	return syncreport.ExtraGuidance(item, defaultLanguage())
}

func syncProviderMismatchIndex(items []plan.Item) map[string]plan.Item {
	return syncreport.ProviderMismatchIndex(items)
}

func syncEntryKey(item plan.Item) string {
	return syncreport.EntryKey(item)
}

func syncIdentityKey(item plan.Item) string {
	return syncreport.IdentityKey(item)
}

func manualLocalOnlyCaskFunc(manualIndex map[string]toolRow) syncreport.ManualLocalOnlyFunc {
	if len(manualIndex) == 0 {
		return nil
	}
	return func(item plan.Item) bool {
		if !strings.EqualFold(item.Provider, "brew") || !strings.EqualFold(item.Kind, "cask") {
			return false
		}
		row, ok := manualAppMatch(manualIndex, item.Name)
		if ok && row.State == "brew" {
			return false
		}
		return ok
	}
}

func printSyncText(w io.Writer, report syncReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.Style("updev sync", "\033[1m", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", tr("root:", "ルート:"), report.Root)
	if report.Cached {
		fmt.Fprintf(w, "%s %s %s\n", tr("cache:", "キャッシュ:"), report.CacheAge+" old", tr("(use --refresh for a fresh read)", "(再取得は --refresh)"))
	}
	fmt.Fprintf(w, "%s %d\n", tr("entries:", "項目:"), len(report.Entries))
	if summary := syncReasonSummary(report.Entries); summary != "" {
		fmt.Fprintf(w, "%s %s\n", tr("summary:", "サマリー:"), summary)
	}
	if summary := syncCategorySummary(report.Entries, color); summary != "" {
		fmt.Fprintf(w, "%s %s\n", tr("categories:", "categories:"), summary)
	}
	if len(report.Entries) == 0 {
		fmt.Fprintf(w, "\n%s\n", tr("in sync", "同期済み"))
		return
	}
	rows := make([][]string, 0, len(report.Entries))
	for _, entry := range report.Entries {
		rows = append(rows, []string{
			textui.StyleName(entry.Provider, color),
			entry.Kind,
			textui.StyleName(entry.Name, color),
			entry.Category,
			textui.StyleStatus(entry.Reason, color),
			textui.StyleLabel(entry.Action, color),
		})
	}
	fmt.Fprintf(w, "\n%s\n", tr("reconcile", "reconcile"))
	textui.PrintTable(w, []textui.Column{
		{Header: "provider", Min: 8, Max: 10},
		{Header: "kind", Min: 7, Max: 10},
		{Header: "name", Min: 18, Max: 36},
		{Header: "category", Min: 8, Max: 10},
		{Header: "reason", Min: 10, Max: 18},
		{Header: "action", Min: 12, Max: 24},
	}, rows, color)
	printSyncEntryDetails(w, report.Entries, color)
	fmt.Fprintf(w, "\n%s\n", tr("next", "次"))
	fmt.Fprintf(w, "  %s\n", tr("review entries, then use updev add/remove or provider-specific commands; sync is read-only by default", "項目を確認してから updev add/remove または provider 固有コマンドを使ってください。sync は既定で read-only です"))
}

func printSyncEntryDetails(w io.Writer, entries []syncEntry, color bool) {
	wrote := false
	for _, entry := range entries {
		if entry.Detail == "" {
			continue
		}
		if !wrote {
			fmt.Fprintf(w, "\n%s\n", tr("details", "詳細"))
			wrote = true
		}
		target := entry.Provider
		if entry.Kind != "" || entry.Name != "" {
			target = strings.Trim(target+"/"+entry.Kind+" "+entry.Name, "/ ")
		}
		detail := entry.Detail
		if entry.RelatedProvider != "" {
			detail += fmt.Sprintf(" (related: %s/%s)", entry.RelatedProvider, entry.RelatedKind)
		}
		fmt.Fprintf(w, "  %s %s\n", textui.StyleName(target, color), textui.StyleDim(detail, color))
	}
}

func syncReasonSummary(entries []syncEntry) string {
	return syncreport.ReasonSummary(entries)
}

func syncCategorySummary(entries []syncEntry, color bool) string {
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.Category != "" {
			counts[entry.Category]++
		}
	}
	keys := sortedMapKeys(counts)
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d (%s)", textui.StyleRequested(key, color), counts[key], categoryDescription(key)))
	}
	return strings.Join(parts, ", ")
}
