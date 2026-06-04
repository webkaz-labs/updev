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
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
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
	toolVersion   = "v0.5.4"
)

type versionReport struct {
	SchemaVersion int    `json:"schema_version"`
	Tool          string `json:"tool"`
	Version       string `json:"version"`
	Major         int    `json:"major"`
	Minor         int    `json:"minor"`
	Patch         int    `json:"patch"`
	Contract      string `json:"contract"`
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
	case "inventory", "list", "ls":
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
	case "help":
		printUsage()
		return 0
	default:
		return runLegacy(append([]string{command}, args...))
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
	annotateMiseManifestIssues(&report, opts.root)
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
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("cache:", "キャッシュ:"), color), textui.StyleCount(friendlyAge(time.Since(result.CreatedAt))+" old", color), textui.StyleDim(tr("(use --refresh for a fresh read)", "(再取得は --refresh)"), color))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, textui.StyleHeading("providers", color))
	rows := make([][]string, 0, len(report.Providers))
	for _, provider := range report.Providers {
		status := "ok"
		if provider.Unavailable {
			status = "unavailable"
		} else if provider.Error != "" {
			status = "error"
		} else if provider.Missing > 0 || provider.Extra > 0 {
			status = "drift"
		}
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
	if len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  updev [--config file] [--no-color] ...
  updev                         # daily update workflow, then compact review dashboard
  updev -h | --help             # help
  updev update [--dry-run] [--security warn|strict|off] [--include-vscode] [--policy file] [--inventory fast|legacy] [--interactive|--no-interactive] [--format text|json]
  updev sync [--format text|json]
  updev add [--provider brew|mise] [--kind brew|cask|tap|vscode|tool] [--category work|personal] [--version version] <name>
  updev remove [--provider brew|mise] [--kind brew|cask|tap|vscode|tool] <name>
  updev edit [--provider brew|mise]
  updev rollback [--token token] [--format text|json]
  updev fix mise [--dry-run|--apply] [--format text|json]
  updev backends <doctor|plan> [--format text|json]
  updev doctor dependencies [--format text|json]
  updev list                    # Go rich list with descriptions/translations/cache
  updev last [--section summary|updates|security|inventory|logs|full] [--details] [--format text|json]
  updev inventory [--refresh] [--include-vscode] [--provider name] [--kind kind] [--status status] [--query text] [--limit n] [--details] [--interactive|--no-interactive] [--format text|json]
  updev inventory scan [--provider manual] [--format text|json]
  updev inventory plan [--provider manual] [--action action] [--query text] [--limit n] [--format text|json]
  updev inventory check [--provider manual] [--action action] [--query text] [--limit n] [--format text|json]
  updev inventory review [--provider manual] [--action action] [--query text] [--format text|json]
  updev inventory render [--report manual-apps] [--format text|json]
  updev list --fast ...         # accepted alias for inventory-style options
  updev ls ...                  # alias for list
  updev st ...                  # alias for status
  updev ck ...                  # alias for check
  updev security scan [--refresh] [--include-vscode] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--policy file] [--format text|json]
  updev security review [--refresh] [--include-vscode] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--decision allow|review|hold|block] [--kind name] [--name text] [--policy file] [--format text|json]
  updev security gate [--provider brew|vscode|all] [--include-vscode] [--policy file] [--format text|json]
  updev security policy [--path file] [--format text|json]
  updev security policy cleanup [--apply] [--path file] [--format text|json]
  updev status [--refresh] [--include-vscode] [--format text|json]
  updev check [--refresh] [--include-vscode] [--manifest-only] [--dependencies] [--format text|json]
  updev plan [--refresh] [--include-vscode] [--format text|json]
  updev version [--format text|json]
  updev --version | -v
  updev brewfile [--root path] <add|remove|has|check|sync> ...
  updev legacy <command> ...    # explicit escape hatch for Python legacy commands

Use "updev legacy <command>" only for explicitly named legacy comparison or
compatibility paths.`)
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
	if root := os.Getenv("CHEZMOI_SOURCE_DIR"); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share", "chezmoi")
}
