package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

type updateOptions struct {
	format        string
	root          string
	inventory     string
	dryRun        bool
	security      string
	policy        string
	includeVSCode bool
	tui           bool
	noTUI         bool
}

type updateStep struct {
	Name         string      `json:"name"`
	Command      []string    `json:"command"`
	Status       plan.Status `json:"status"`
	Stdout       string      `json:"stdout,omitempty"`
	Stderr       string      `json:"stderr,omitempty"`
	Reason       string      `json:"reason,omitempty"`
	Skipped      bool        `json:"skipped,omitempty"`
	Updated      []string    `json:"updated,omitempty"`
	SkippedItems []string    `json:"skipped_items,omitempty"`
}

type updateReport struct {
	Status    plan.Status        `json:"status"`
	Root      string             `json:"root"`
	DryRun    bool               `json:"dry_run"`
	Security  string             `json:"security"`
	Policy    *securityPolicyUse `json:"policy,omitempty"`
	Warnings  []string           `json:"warnings,omitempty"`
	Steps     []updateStep       `json:"steps"`
	Safety    []safetyGate       `json:"safety,omitempty"`
	Inventory plan.Report        `json:"inventory"`
	Report    string             `json:"report,omitempty"`
}

type updateReportCacheEntry struct {
	Version   int          `json:"version"`
	Type      string       `json:"type"`
	CreatedAt time.Time    `json:"created_at"`
	Report    updateReport `json:"report"`
}

type updateReportSectionView struct {
	Version   int                     `json:"version"`
	Type      string                  `json:"type"`
	CreatedAt time.Time               `json:"created_at"`
	Section   string                  `json:"section"`
	Status    plan.Status             `json:"status"`
	Filters   map[string]string       `json:"filters,omitempty"`
	Summary   updateReportViewSummary `json:"summary"`
	Report    *updateReport           `json:"report,omitempty"`
	Steps     []updateStep            `json:"steps,omitempty"`
	Safety    []safetyGate            `json:"safety,omitempty"`
	Inventory *plan.Report            `json:"inventory,omitempty"`
}

type updateReportViewSummary struct {
	Steps              int `json:"steps"`
	SkippedSteps       int `json:"skipped_steps,omitempty"`
	HeldSteps          int `json:"held_steps,omitempty"`
	ErrorSteps         int `json:"error_steps,omitempty"`
	SafetyGates        int `json:"safety_gates,omitempty"`
	SafetyAttention    int `json:"safety_attention,omitempty"`
	InventoryItems     int `json:"inventory_items,omitempty"`
	InventoryAttention int `json:"inventory_attention,omitempty"`
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) runner.Result
}

type streamingCommandRunner interface {
	RunStreaming(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result
}

func parseUpdateOptions(args []string) (updateOptions, error) {
	opts := updateOptions{format: "text", root: defaultRoot(), inventory: "fast", security: defaultUpdateSecurityMode(), policy: securityPolicyPath()}
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
		case "--inventory":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--inventory requires a value")
			}
			opts.inventory = args[i+1]
			i++
		case "--fast-list":
			opts.inventory = "fast"
		case "--dry-run", "-n":
			opts.dryRun = true
		case "--security":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--security requires a value")
			}
			opts.security = args[i+1]
			i++
		case "--policy":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--policy requires a value")
			}
			opts.policy = args[i+1]
			i++
		case "--include-vscode":
			opts.includeVSCode = true
		case "--interactive", "--tui":
			opts.tui = true
		case "--no-interactive", "--no-tui":
			opts.noTUI = true
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
	if opts.inventory != "legacy" && opts.inventory != "fast" {
		return opts, fmt.Errorf("unsupported inventory mode: %s", opts.inventory)
	}
	if !validUpdateSecurityMode(opts.security) {
		return opts, fmt.Errorf("unsupported security mode: %s", opts.security)
	}
	return opts, nil
}

func defaultUpdateSecurityMode() string {
	mode := "warn"
	if configured := loadUpdevConfig().Update.Security; configured != nil && validUpdateSecurityMode(*configured) {
		mode = strings.ToLower(strings.TrimSpace(*configured))
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_UPDATE_SECURITY")); value != "" && validUpdateSecurityMode(value) {
		mode = strings.ToLower(value)
	}
	return mode
}

func runUpdate(opts updateOptions, commandRunner commandRunner) int {
	ctx := context.Background()
	policyUse := loadUpdateSecurityPolicy(opts)
	report := updateReport{
		Status:   plan.StatusOK,
		Root:     opts.root,
		DryRun:   opts.dryRun,
		Security: opts.security,
		Policy:   policyUse.View(),
	}
	if len(policyUse.Warnings) > 0 {
		report.Warnings = append(report.Warnings, policyUse.Warnings...)
	}
	safetyProgress := startupProgress{}
	if opts.format == "text" && opts.security != "off" {
		lang := defaultLanguage()
		safetyProgress = newStartupProgress(os.Stdin, os.Stderr, opts.format, safetyProgressMessage(lang))
	}
	safetyProgress.Start()
	report.Safety = collectUpdateSafetyWithPolicy(ctx, commandRunner, opts, policyUse.Policy)
	safetyProgress.Done()
	for _, step := range updateSteps() {
		if opts.format == "text" && !opts.dryRun {
			fmt.Printf(tr("running %s update...\n", "%s update を実行中...\n"), step.Name)
		}
		result := runUpdateStepWithOutput(ctx, commandRunner, step, opts.dryRun, providerHeldBySafety(step.Name, opts, report.Safety), opts.format == "text" && !opts.dryRun)
		if result.Status == plan.StatusError {
			report.Status = plan.StatusError
		} else if result.Status == plan.StatusHeld && report.Status != plan.StatusError {
			report.Status = plan.StatusHeld
		}
		report.Steps = append(report.Steps, result)
	}
	collectGoInventory := opts.format == "json" || opts.dryRun || opts.inventory == "fast"
	if collectGoInventory {
		progress := startupProgress{}
		if opts.format == "text" {
			lang := defaultLanguage()
			progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, inventoryProgressMessage(lang, true))
		}
		progress.Start()
		report.Inventory = collectInventoryWithOptions(ctx, opts.root, runner.Local{}, inventoryOptions{IncludeVSCode: updateIncludesVSCode(opts)})
		progress.Done()
	}
	if report.Status != plan.StatusError && report.Inventory.Status == plan.StatusError {
		report.Status = plan.StatusError
	}
	report.Report = saveLastUpdateReport(report)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printUpdateText(report)
		if shouldRunUpdateHub(opts, os.Stdin, os.Stdout) {
			runUpdateHub(report)
		}
		if !opts.dryRun && opts.inventory == "legacy" {
			fmt.Println("refreshing legacy list...")
			legacyCode := runLegacy([]string{"list"})
			if report.Status == plan.StatusError {
				return 1
			}
			return legacyCode
		}
	}
	return updateExitCode(report.Status)
}

func shouldRunUpdateHub(opts updateOptions, input io.Reader, output io.Writer) bool {
	if opts.inventory == "legacy" {
		return false
	}
	return shouldRunUpdevInteractive(input, output, opts.format, opts.tui, opts.noTUI)
}

func loadUpdateSecurityPolicy(opts updateOptions) securityPolicyLoadResult {
	if opts.security == "off" {
		return securityPolicyLoadResult{}
	}
	return loadSecurityPolicyForReportPath(opts.policy)
}

func updateSteps() []updateStep {
	return []updateStep{
		{
			Name:    "brew",
			Command: []string{"bash", "-lc", "brew update && brew upgrade --greedy && brew cleanup"},
		},
		{
			Name:    "mise",
			Command: []string{"zsh", "-c", "source ~/.zshenv && mise upgrade && mise prune"},
		},
	}
}

func updateExitCode(status plan.Status) int {
	switch status {
	case plan.StatusError:
		return 1
	case plan.StatusBlocked:
		return 3
	case plan.StatusHeld, plan.StatusDrift:
		return 2
	default:
		return 0
	}
}

func runUpdateStep(ctx context.Context, commandRunner commandRunner, step updateStep, dryRun bool) updateStep {
	return runUpdateStepWithHold(ctx, commandRunner, step, dryRun, "")
}

func runUpdateStepWithHold(ctx context.Context, commandRunner commandRunner, step updateStep, dryRun bool, holdReason string) updateStep {
	return runUpdateStepWithOutput(ctx, commandRunner, step, dryRun, holdReason, false)
}

func runUpdateStepWithOutput(ctx context.Context, commandRunner commandRunner, step updateStep, dryRun bool, holdReason string, stream bool) updateStep {
	if stream {
		return runUpdateStepWithWriters(ctx, commandRunner, step, dryRun, holdReason, os.Stdout, os.Stderr)
	}
	return runUpdateStepWithWriters(ctx, commandRunner, step, dryRun, holdReason, nil, nil)
}

func runUpdateStepWithWriters(ctx context.Context, commandRunner commandRunner, step updateStep, dryRun bool, holdReason string, stdout io.Writer, stderr io.Writer) updateStep {
	if holdReason != "" {
		step.Status = plan.StatusHeld
		step.Reason = holdReason
		step.Skipped = true
		step.SkippedItems = append(step.SkippedItems, holdReason)
		return step
	}
	if dryRun {
		step.Status = plan.StatusOK
		return step
	}
	result := runner.Result{}
	if stdout != nil || stderr != nil {
		if streamingRunner, ok := commandRunner.(streamingCommandRunner); ok {
			result = streamingRunner.RunStreaming(ctx, stdout, stderr, step.Command[0], step.Command[1:]...)
		} else {
			result = commandRunner.Run(ctx, step.Command[0], step.Command[1:]...)
		}
	} else {
		result = commandRunner.Run(ctx, step.Command[0], step.Command[1:]...)
	}
	step.Stdout = result.Stdout
	step.Stderr = result.Stderr
	step.Updated, step.SkippedItems = summarizeUpdateStepLog(step)
	if result.Code != 0 || result.Err != nil {
		step.Status = plan.StatusError
		return step
	}
	step.Status = plan.StatusOK
	return step
}

func summarizeUpdateStepLog(step updateStep) ([]string, []string) {
	updated := []string{}
	skipped := []string{}
	for _, raw := range strings.Split(strings.Join([]string{step.Stdout, step.Stderr}, "\n"), "\n") {
		line := normalizeUpdateLogLine(raw)
		if line == "" {
			continue
		}
		if updateLogLineIsProgress(line) {
			continue
		}
		if updateLogLineIsSkipped(line) {
			if updateLogLineIsGenericSkipped(line) {
				continue
			}
			skipped = appendCappedUniqueSummary(skipped, line)
			continue
		}
		if updateLogLineIsUpdated(line) {
			updated = appendCappedUniqueSummary(updated, line)
		}
	}
	return updated, skipped
}

func normalizeUpdateLogLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "==>")
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "🍺")
	line = strings.TrimSpace(line)
	return line
}

func updateLogLineIsUpdated(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(line, " -> ") {
		return updateLogLineHasPackageVersionChange(line)
	}
	for _, prefix := range []string{"upgraded ", "installing ", "installed ", "updated ", "pruning ", "pruned "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func updateLogLineHasPackageVersionChange(line string) bool {
	before, after, ok := strings.Cut(line, " -> ")
	if !ok || strings.TrimSpace(after) == "" {
		return false
	}
	fields := strings.Fields(before)
	if len(fields) < 2 {
		return false
	}
	return !updateLogTokenLooksVersion(fields[0])
}

func updateLogTokenLooksVersion(token string) bool {
	var hasDigit, hasLetter bool
	for _, r := range token {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	return hasDigit && !hasLetter
}

func updateLogLineIsProgress(line string) bool {
	lower := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(line), "."))
	for _, needle := range []string{
		"adjust how often this is run with",
		"homebrew_auto_update_secs",
		"homebrew_no_auto_update",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	for _, progress := range []string{
		"auto-updating homebrew",
		"updating homebrew",
		"updating homebrew bundle",
		"updating homebrew/bundle",
	} {
		if lower == progress {
			return true
		}
	}
	if strings.HasPrefix(lower, "upgrading ") {
		return true
	}
	return false
}

func updateLogLineIsSkipped(line string) bool {
	lower := strings.ToLower(line)
	for _, needle := range []string{
		"already up-to-date",
		"already installed",
		"nothing to do",
		"no outdated",
		"not upgrading",
		"skipping",
		"skipped",
		"kept current",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func updateLogLineIsGenericSkipped(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, generic := range []string{
		"already up-to-date.",
		"already up-to-date",
		"nothing to do",
	} {
		if lower == generic {
			return true
		}
	}
	return false
}

func appendCappedUniqueSummary(values []string, value string) []string {
	if len(values) >= 12 {
		return values
	}
	value = truncate(oneLine(value), 160)
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func printUpdateText(report updateReport) {
	printUpdateTextTo(os.Stdout, report)
}

func printUpdateTextTo(w io.Writer, report updateReport) {
	color := textui.ColorEnabled()
	printUpdateHeaderTo(w, "updev update", report.Status, color)
	printUpdateBodyTo(w, report, color)
}

func printUpdateHeaderTo(w io.Writer, title string, status plan.Status, color bool) {
	titleStatus := status
	if titleStatus == "" {
		titleStatus = plan.StatusOK
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(title, color), textui.StyleStatus(string(titleStatus), color))
}

func printUpdateBodyTo(w io.Writer, report updateReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	if report.DryRun {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("mode:", "モード:"), color), textui.StyleRequested("dry-run", color))
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("security:", "セキュリティ:"), color), styleSecurityMode(report.Security, color))
	if report.Policy != nil && report.Policy.Loaded {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("policy:", "ポリシー:"), color), report.Policy.Path, textui.StyleDim("("+securityPolicyUseSummary(report.Policy)+")", color))
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, textui.StyleWarning(tr("warnings:", "警告:"), color))
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	if summary := updateSafetySummaryText(report); summary != "" {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("safety summary:", "安全性サマリー:"), color), summary)
	}
	if summary := updateStepSummaryText(report.Steps); summary != "" {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("update summary:", "更新サマリー:"), color), summary)
	}
	printUpdateOutcomeSummary(w, report, color)
	if report.Report != "" {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("report:", "レポート:"), color), report.Report)
	}
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("updates", "更新"), color))
	printUpdateStepsTable(w, report.Steps, color, updateStepTableLabels{
		Name:    tr("name", "名前"),
		Status:  tr("status", "状態"),
		Skipped: tr("skipped", "skip"),
		Detail:  tr("detail", "詳細"),
	})
	for _, step := range report.Steps {
		if step.Status == plan.StatusError && step.Stderr != "" {
			fmt.Fprintf(w, "    %s\n", truncate(oneLine(step.Stderr), 120))
		}
		if step.Reason != "" {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel(tr("reason:", "理由:"), color), truncate(oneLine(step.Reason), 120))
		}
	}
	printUpdateSafetyDashboard(w, report, color)
	printUpdateInventoryDashboard(w, report.Inventory, color)
}

type updateStepTableLabels struct {
	Name    string
	Status  string
	Skipped string
	Detail  string
}

func printUpdateStepsTable(w io.Writer, steps []updateStep, color bool, labels updateStepTableLabels) {
	rows := make([][]string, 0, len(steps))
	hasDetail := false
	details := make([]string, 0, len(steps))
	for _, step := range steps {
		detail := updateStepHumanDetail(step)
		if detail != "" {
			hasDetail = true
		}
		details = append(details, detail)
	}
	for index, step := range steps {
		row := []string{
			textui.StyleName(step.Name, color),
			textui.StyleStatus(string(step.Status), color),
			styleSkipped(step.Skipped, color),
		}
		if hasDetail {
			row = append(row, details[index])
		}
		rows = append(rows, row)
	}
	columns := []textui.Column{
		{Header: labels.Name, Min: 8, Max: 12},
		{Header: labels.Status, Min: 8, Max: 10},
		{Header: labels.Skipped, Min: 7, Max: 7},
	}
	if hasDetail {
		columns = append(columns, textui.Column{Header: labels.Detail, Min: 0, Max: 72})
	}
	textui.PrintTable(w, columns, rows, color)
}

func updateStepHumanDetail(step updateStep) string {
	if step.Reason != "" {
		return truncate(oneLine(step.Reason), 72)
	}
	if len(step.Updated) > 0 {
		return fmt.Sprintf(tr("%d updated", "更新 %d件"), len(step.Updated))
	}
	if len(step.SkippedItems) > 0 {
		return fmt.Sprintf(tr("%d deferred", "見送り %d件"), len(step.SkippedItems))
	}
	if step.Status == plan.StatusError {
		return truncate(firstNonEmpty(oneLine(step.Stderr), oneLine(step.Stdout)), 72)
	}
	return ""
}

func saveLastUpdateReport(report updateReport) string {
	dir := updevReportCacheDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	report.Report = ""
	entry := updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Now(),
		Report:    report,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	lastPath := filepath.Join(dir, "last-update.json")
	if err := os.WriteFile(lastPath, data, 0o600); err != nil {
		return ""
	}
	stampedPath := filepath.Join(dir, "update-"+entry.CreatedAt.Format("20060102T150405")+".json")
	_ = os.WriteFile(stampedPath, data, 0o600)
	return lastPath
}

func loadLastUpdateReport() (updateReportCacheEntry, bool) {
	dir := updevReportCacheDir()
	if dir == "" {
		return updateReportCacheEntry{}, false
	}
	path := filepath.Join(dir, "last-update.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return updateReportCacheEntry{}, false
	}
	var entry updateReportCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return updateReportCacheEntry{}, false
	}
	if entry.Version != 1 || entry.Type != "update" {
		return updateReportCacheEntry{}, false
	}
	entry.Report.Report = path
	return entry, true
}

type lastReportOptions struct {
	format   string
	section  string
	provider string
	status   string
	query    string
	details  bool
}

func parseLastReportOptions(args []string) (lastReportOptions, error) {
	opts := lastReportOptions{format: "text", section: "summary"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--section":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--section requires a value")
			}
			opts.section = args[i+1]
			i++
		case "--provider":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--provider requires a value")
			}
			opts.provider = args[i+1]
			i++
		case "--status":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--status requires a value")
			}
			opts.status = args[i+1]
			i++
		case "--query":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--query requires a value")
			}
			opts.query = args[i+1]
			i++
		case "--details":
			opts.details = true
		case "--help", "-h":
			return opts, fmt.Errorf("usage: updev last [--section summary|updates|security|inventory|logs|full] [--provider name] [--status status|attention] [--query text] [--details] [--format text|json]")
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	opts.section = strings.ToLower(strings.TrimSpace(opts.section))
	switch opts.section {
	case "summary", "updates", "security", "inventory", "logs", "full":
	default:
		return opts, fmt.Errorf("unsupported section: %s", opts.section)
	}
	return opts, nil
}

func runLastReport(opts lastReportOptions) int {
	entry, ok := loadLastUpdateReport()
	if !ok {
		fmt.Fprintln(os.Stderr, "updev: no cached update report found")
		return 1
	}
	view := buildUpdateReportSectionView(entry, opts)
	if opts.format == "json" {
		if code := encodeJSON(view); code != 0 {
			return code
		}
		return updateExitCode(entry.Report.Status)
	}
	printLastReportText(os.Stdout, entry, opts)
	return updateExitCode(entry.Report.Status)
}

func printLastReportText(w io.Writer, entry updateReportCacheEntry, opts lastReportOptions) {
	color := textui.ColorEnabled()
	printUpdateHeaderTo(w, "updev last", entry.Report.Status, color)
	if !entry.CreatedAt.IsZero() {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("created:", "作成:"), color), entry.CreatedAt.Format(time.RFC3339))
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("age:", "経過:"), color), friendlyAge(time.Since(entry.CreatedAt)))
	}
	report := filterUpdateReport(entry.Report, opts)
	if opts.section != "" && opts.section != "summary" {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("section:", "section:"), color), textui.StyleRequested(opts.section, color), textui.StyleStatus(string(updateSectionStatus(report, opts.section)), color))
	}
	if filters := lastReportFilterMap(opts); len(filters) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("filters:", "フィルター:"), color), textui.StyleRequested(filterSummary(filters), color))
	}
	switch opts.section {
	case "updates":
		printLastUpdateSteps(w, report, opts.details, color)
	case "security":
		printLastSecuritySection(w, report, opts.details, color)
	case "inventory":
		printLastInventorySection(w, report, opts, color)
	case "logs":
		printLastUpdateLogs(w, report, color)
	case "full":
		printUpdateBodyTo(w, report, color)
	default:
		printUpdateBodyTo(w, report, color)
	}
}

func updevReportCacheDir() string {
	dir := updevCacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "reports")
}

func buildUpdateReportSectionView(entry updateReportCacheEntry, opts lastReportOptions) updateReportSectionView {
	report := filterUpdateReport(entry.Report, opts)
	view := updateReportSectionView{
		Version:   entry.Version,
		Type:      entry.Type,
		CreatedAt: entry.CreatedAt,
		Section:   opts.section,
		Status:    updateSectionStatus(report, opts.section),
		Filters:   lastReportFilterMap(opts),
		Summary:   updateReportSummary(report),
	}
	switch opts.section {
	case "updates", "logs":
		view.Steps = report.Steps
	case "security":
		view.Safety = report.Safety
	case "inventory":
		inventory := report.Inventory
		view.Inventory = &inventory
	case "full":
		view.Report = &report
	default:
		view.Report = &report
	}
	return view
}

func updateSectionStatus(report updateReport, section string) plan.Status {
	switch section {
	case "updates", "logs":
		return updateStepsStatus(report.Steps)
	case "security":
		return safetyGatesStatus(report.Safety)
	case "inventory":
		if report.Inventory.Status != "" {
			return report.Inventory.Status
		}
		return plan.StatusOK
	default:
		if report.Status != "" {
			return report.Status
		}
		return plan.StatusOK
	}
}

func updateStepsStatus(steps []updateStep) plan.Status {
	status := plan.StatusOK
	for _, step := range steps {
		switch step.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusBlocked:
			status = plan.StatusBlocked
		case plan.StatusHeld:
			if status != plan.StatusBlocked {
				status = plan.StatusHeld
			}
		case plan.StatusDrift:
			if status == plan.StatusOK {
				status = plan.StatusDrift
			}
		}
	}
	return status
}

func safetyGatesStatus(gates []safetyGate) plan.Status {
	status := plan.StatusOK
	for _, gate := range gates {
		switch gate.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusBlocked:
			status = plan.StatusBlocked
		case plan.StatusHeld:
			if status != plan.StatusBlocked {
				status = plan.StatusHeld
			}
		}
	}
	return status
}

func updateReportSummary(report updateReport) updateReportViewSummary {
	summary := updateReportViewSummary{
		Steps:          len(report.Steps),
		SafetyGates:    len(report.Safety),
		InventoryItems: len(report.Inventory.Items),
	}
	for _, step := range report.Steps {
		if step.Skipped {
			summary.SkippedSteps++
		}
		switch step.Status {
		case plan.StatusHeld:
			summary.HeldSteps++
		case plan.StatusError:
			summary.ErrorSteps++
		}
	}
	for _, gate := range report.Safety {
		if gate.Status == plan.StatusError || gate.Status == plan.StatusHeld || gate.Status == plan.StatusBlocked {
			summary.SafetyAttention++
			continue
		}
		for _, finding := range gate.Findings {
			if !strings.EqualFold(finding.Decision, "allow") {
				summary.SafetyAttention++
				break
			}
		}
	}
	for _, item := range report.Inventory.Items {
		if isAttentionStatus(item.Status) {
			summary.InventoryAttention++
		}
	}
	return summary
}

func lastReportFilterMap(opts lastReportOptions) map[string]string {
	filters := map[string]string{}
	if opts.provider != "" {
		filters["provider"] = opts.provider
	}
	if opts.status != "" {
		filters["status"] = opts.status
	}
	if opts.query != "" {
		filters["query"] = opts.query
	}
	if len(filters) == 0 {
		return nil
	}
	return filters
}

func filterUpdateReport(report updateReport, opts lastReportOptions) updateReport {
	report.Steps = filterUpdateSteps(report.Steps, opts)
	report.Safety = filterSafetyGates(report.Safety, opts)
	report.Inventory = filterPlanReport(report.Inventory, opts)
	return report
}

func filterUpdateSteps(steps []updateStep, opts lastReportOptions) []updateStep {
	if opts.provider == "" && opts.status == "" && opts.query == "" {
		return steps
	}
	out := make([]updateStep, 0, len(steps))
	for _, step := range steps {
		if opts.provider != "" && !strings.EqualFold(step.Name, opts.provider) {
			continue
		}
		if opts.status != "" && !statusMatches(step.Status, opts.status) {
			continue
		}
		if opts.query != "" && !updateStepMatchesQuery(step, opts.query) {
			continue
		}
		out = append(out, step)
	}
	return out
}

func updateStepMatchesQuery(step updateStep, query string) bool {
	needle := strings.ToLower(query)
	haystack := strings.ToLower(strings.Join([]string{
		step.Name,
		string(step.Status),
		joinCommand(step.Command),
		step.Reason,
		step.Stdout,
		step.Stderr,
	}, " "))
	return strings.Contains(haystack, needle)
}

func filterSafetyGates(gates []safetyGate, opts lastReportOptions) []safetyGate {
	if opts.provider == "" && opts.status == "" && opts.query == "" {
		return gates
	}
	out := make([]safetyGate, 0, len(gates))
	for _, gate := range gates {
		if opts.provider != "" && !strings.EqualFold(gate.Provider, opts.provider) {
			continue
		}
		filtered := gate
		filtered.Findings = filterSafetyFindings(gate.Findings, opts)
		gateMatches := safetyGateMatchesFilters(gate, opts)
		if !gateMatches && len(filtered.Findings) == 0 {
			continue
		}
		out = append(out, filtered)
	}
	return out
}

func filterSafetyFindings(findings []safetyFinding, opts lastReportOptions) []safetyFinding {
	if opts.status == "" && opts.query == "" {
		return findings
	}
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if opts.status != "" && !safetyFindingStatusMatches(finding, opts.status) {
			continue
		}
		if opts.query != "" && !safetyFindingMatchesQuery(finding, opts.query) {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func safetyGateMatchesFilters(gate safetyGate, opts lastReportOptions) bool {
	if opts.status != "" && !statusMatches(gate.Status, opts.status) {
		return false
	}
	if opts.query != "" && !safetyGateMatchesQuery(gate, opts.query) {
		return false
	}
	return true
}

func safetyFindingStatusMatches(finding safetyFinding, filter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filter))
	switch normalized {
	case "attention", "problem", "problems":
		return !strings.EqualFold(finding.Decision, "allow")
	default:
		return strings.EqualFold(finding.Decision, normalized)
	}
}

func safetyGateMatchesQuery(gate safetyGate, query string) bool {
	needle := strings.ToLower(query)
	haystack := strings.ToLower(strings.Join([]string{
		gate.Provider,
		string(gate.Status),
		gate.Error,
		strings.Join(gate.Warnings, " "),
		safetyGatePrimaryReason(gate),
	}, " "))
	return strings.Contains(haystack, needle)
}

func safetyFindingMatchesQuery(finding safetyFinding, query string) bool {
	needle := strings.ToLower(query)
	haystack := strings.ToLower(strings.Join([]string{
		finding.Provider,
		finding.Kind,
		finding.Name,
		finding.Decision,
		finding.Reason,
		finding.Remediation,
		strings.Join(finding.Evidence, " "),
		finding.Source,
		finding.Tap,
		finding.Publisher,
		finding.RepositoryURL,
		finding.SupportURL,
		finding.Homepage,
		finding.URL,
		finding.Confidence,
		strings.Join(finding.AdvisoryIDs, " "),
		strings.Join(finding.FixedVersions, " "),
	}, " "))
	return strings.Contains(haystack, needle)
}

func filterPlanReport(report plan.Report, opts lastReportOptions) plan.Report {
	report.Providers = filterPlanProviders(report.Providers, opts)
	report.Items = filterPlanItems(report.Items, opts)
	return report
}

func filterPlanProviders(providers []plan.ProviderSummary, opts lastReportOptions) []plan.ProviderSummary {
	if opts.provider == "" && opts.status == "" && opts.query == "" {
		return providers
	}
	out := make([]plan.ProviderSummary, 0, len(providers))
	for _, provider := range providers {
		if opts.provider != "" && !strings.EqualFold(provider.Name, opts.provider) {
			continue
		}
		if opts.status != "" && !statusMatches(plan.Status(providerStatus(provider)), opts.status) {
			continue
		}
		if opts.query != "" && !strings.Contains(strings.ToLower(provider.Name+" "+provider.Error), strings.ToLower(opts.query)) {
			continue
		}
		out = append(out, provider)
	}
	return out
}

func filterPlanItems(items []plan.Item, opts lastReportOptions) []plan.Item {
	if opts.provider == "" && opts.status == "" && opts.query == "" {
		return items
	}
	return filterItems(items, listOptions{provider: opts.provider, status: opts.status, query: opts.query})
}

func styleSkipped(skipped bool, color bool) string {
	if skipped {
		return textui.StyleBool(true, color)
	}
	return textui.StyleDim("-", color)
}

func styleSecurityMode(mode string, color bool) string {
	switch mode {
	case "strict":
		return textui.StyleWarning(mode, color)
	case "off":
		return textui.StyleDim(mode, color)
	default:
		return textui.StyleRequested(mode, color)
	}
}

func updateStepSummaryText(steps []updateStep) string {
	if len(steps) == 0 {
		return ""
	}
	var held, errors, skipped int
	var updatedItems, skippedItems int
	for _, step := range steps {
		switch step.Status {
		case plan.StatusHeld:
			held++
		case plan.StatusError:
			errors++
		}
		if step.Skipped {
			skipped++
		}
		updatedItems += len(step.Updated)
		skippedItems += updateStepDeferredCount(step)
	}
	parts := []string{fmt.Sprintf(tr("%d steps", "step %d件"), len(steps))}
	if updatedItems > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d updated", "更新 %d件"), updatedItems))
	}
	if skippedItems > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d deferred", "見送り %d件"), skippedItems))
	}
	if held > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d held", "held %d件"), held))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d skipped", "skip %d件"), skipped))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d error", "error %d件"), errors))
	}
	return strings.Join(parts, ", ")
}

func printUpdateOutcomeSummary(w io.Writer, report updateReport, color bool) {
	rows := updateOutcomeRows(report, 10, color)
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("update outcome", "更新結果"), color))
	textui.PrintTable(w, []textui.Column{
		{Header: tr("type", "種別"), Min: 8, Max: 10},
		{Header: "provider", Min: 8, Max: 10},
		{Header: tr("item", "項目"), Min: 18, Max: 38},
		{Header: tr("detail", "詳細"), Min: 0, Max: 72},
	}, rows, color)
}

func updateOutcomeRows(report updateReport, limit int, color bool) [][]string {
	rows := [][]string{}
	for _, step := range report.Steps {
		for _, item := range step.Updated {
			name, detail := updateOutcomeUpdatedItemParts(item)
			rows = append(rows, []string{
				textui.StyleStatus("updated", color),
				textui.StyleName(step.Name, color),
				textui.StyleVersion(truncate(name, 38), color),
				textui.StyleVersion(truncate(oneLine(detail), 72), color),
			})
			if len(rows) >= limit {
				return rows
			}
		}
		for _, item := range step.SkippedItems {
			rows = append(rows, []string{
				textui.StyleStatus("skipped", color),
				textui.StyleName(step.Name, color),
				truncate(firstNonEmpty(step.Name, "step"), 38),
				truncate(oneLine(item), 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
		if step.Skipped && len(step.SkippedItems) == 0 && step.Reason != "" {
			rows = append(rows, []string{
				textui.StyleStatus("skipped", color),
				textui.StyleName(step.Name, color),
				truncate(firstNonEmpty(step.Name, "step"), 38),
				truncate(oneLine(step.Reason), 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
	}
	for _, gate := range report.Safety {
		if gate.Status == plan.StatusError && gate.Error != "" {
			rows = append(rows, []string{
				textui.StyleStatus("attention", color),
				textui.StyleName(gate.Provider, color),
				truncate(gate.Provider+" safety", 38),
				truncate(oneLine(gate.Error), 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
		for _, finding := range gate.Findings {
			if strings.EqualFold(finding.Decision, "allow") {
				continue
			}
			item := strings.TrimSpace(finding.Kind + "/" + finding.Name)
			decision := updateSafetyDisplayDecision(report, finding.Decision)
			rows = append(rows, []string{
				textui.StyleStatus(decision, color),
				textui.StyleName(firstNonEmpty(gate.Provider, finding.Provider), color),
				truncate(item, 38),
				truncate(firstNonEmpty(updateOutcomeFindingDetail(finding), finding.Reason), 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
	}
	return rows
}

func updateOutcomeUpdatedItemParts(item string) (string, string) {
	item = oneLine(strings.TrimSpace(item))
	if item == "" {
		return "", ""
	}
	lower := strings.ToLower(item)
	if strings.HasPrefix(lower, "updated ") && (strings.Contains(lower, " tap ") || strings.Contains(lower, " taps ")) {
		return "Homebrew taps", item
	}
	if strings.HasPrefix(lower, "updated homebrew") {
		return "Homebrew", item
	}
	if before, after, ok := strings.Cut(item, " -> "); ok {
		fields := strings.Fields(before)
		if len(fields) >= 2 {
			name := strings.Join(fields[:len(fields)-1], " ")
			from := fields[len(fields)-1]
			return name, from + " -> " + strings.TrimSpace(after)
		}
	}
	return item, ""
}

func updateOutcomeFindingDetail(finding safetyFinding) string {
	if len(finding.InstalledVersions) > 0 && finding.CurrentVersion != "" {
		return strings.Join(finding.InstalledVersions, ",") + " -> " + finding.CurrentVersion
	}
	return versionText(finding)
}

func updateStepDeferredCount(step updateStep) int {
	if len(step.SkippedItems) > 0 {
		return len(step.SkippedItems)
	}
	if step.Skipped && step.Reason != "" {
		return 1
	}
	return 0
}

func printUpdateSafetyDashboard(w io.Writer, report updateReport, color bool) {
	if len(report.Safety) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("security attention", "セキュリティ注意項目"), color))
	rows := [][]string{}
	for _, gate := range report.Safety {
		status := updateSafetyDisplayStatus(report, gate.Status)
		rows = append(rows, []string{
			textui.StyleName(gate.Provider, color),
			textui.StyleStatus(status, color),
			updateSafetyGateSummaryCompact(report, gate),
			truncate(oneLine(localizedSafetyReason(safetyGatePrimaryReason(gate))), 72),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "provider", Min: 8, Max: 12},
		{Header: tr("status", "状態"), Min: 7, Max: 10},
		{Header: tr("summary", "サマリー"), Min: 10, Max: 34},
		{Header: tr("top reason", "主な理由"), Min: 0, Max: 72},
	}, rows, color)
	if attention := safetyAttentionRows(report.Safety, 8, color, report.Security); len(attention) > 0 {
		fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("top security items", "主なセキュリティ項目"), color))
		textui.PrintTable(w, []textui.Column{
			{Header: "decision", Min: 8, Max: 8},
			{Header: "provider", Min: 8, Max: 10},
			{Header: tr("item", "項目"), Min: 16, Max: 36},
			{Header: "version", Min: 7, Max: 18},
			{Header: tr("reason", "理由"), Min: 0, Max: 72},
		}, attention, color)
	}
}

func updateSafetySummaryText(report updateReport) string {
	if report.Security != "warn" {
		return safetySummaryText(report.Safety)
	}
	if len(report.Safety) == 0 {
		return ""
	}
	gates := 0
	warnings := 0
	errors := 0
	for _, gate := range report.Safety {
		gates++
		if gate.Status == plan.StatusError {
			errors++
		}
		for _, finding := range gate.Findings {
			if !strings.EqualFold(finding.Decision, "allow") {
				warnings++
			}
		}
	}
	parts := []string{fmt.Sprintf(tr("%d gates", "gate %d件"), gates)}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d error", "error %d件"), errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d warnings", "warning %d件"), warnings))
	}
	return strings.Join(parts, ", ")
}

func updateSafetyDisplayStatus(report updateReport, status plan.Status) string {
	if status == "" {
		status = plan.StatusOK
	}
	if report.Security == "warn" && (status == plan.StatusHeld || status == plan.StatusBlocked) {
		return "warn"
	}
	return string(status)
}

func updateSafetyDisplayDecision(report updateReport, decision string) string {
	normalized := strings.ToLower(strings.TrimSpace(decision))
	if normalized == "" {
		normalized = "unknown"
	}
	if report.Security == "warn" && normalized != "allow" {
		return "warning"
	}
	return normalized
}

func updateSafetyGateSummaryCompact(report updateReport, gate safetyGate) string {
	if report.Security != "warn" {
		return safetyGateSummaryCompact(gate)
	}
	warnings := 0
	for _, finding := range gate.Findings {
		if !strings.EqualFold(finding.Decision, "allow") {
			warnings++
		}
	}
	if warnings > 0 {
		return fmt.Sprintf(tr("%d warnings", "warning %d件"), warnings)
	}
	return safetyGateSummaryCompact(gate)
}

func safetyGateSummaryCompact(gate safetyGate) string {
	if gate.Error != "" && len(gate.Findings) == 0 {
		return "error"
	}
	if gate.Summary != nil {
		parts := []string{}
		if gate.Summary.Block > 0 {
			parts = append(parts, fmt.Sprintf("%d block", gate.Summary.Block))
		}
		if gate.Summary.Hold > 0 {
			parts = append(parts, fmt.Sprintf("%d hold", gate.Summary.Hold))
		}
		if gate.Summary.Review > 0 {
			parts = append(parts, fmt.Sprintf("%d review", gate.Summary.Review))
		}
		if gate.Summary.Unknown > 0 {
			parts = append(parts, fmt.Sprintf("%d unknown", gate.Summary.Unknown))
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
		if gate.Summary.Allow > 0 {
			return fmt.Sprintf("%d allow", gate.Summary.Allow)
		}
	}
	if len(gate.Findings) > 0 {
		return fmt.Sprintf("%d findings", len(gate.Findings))
	}
	return "ok"
}

func safetyGatePrimaryReason(gate safetyGate) string {
	if gate.Error != "" {
		return gate.Error
	}
	for _, warning := range gate.Warnings {
		if warning != "" {
			return "warning: " + warning
		}
	}
	for _, finding := range gate.Findings {
		if strings.EqualFold(finding.Decision, "allow") {
			continue
		}
		if finding.Reason != "" {
			return finding.Reason
		}
	}
	return ""
}

func safetyAttentionRows(gates []safetyGate, limit int, color bool, securityMode ...string) [][]string {
	rows := [][]string{}
	report := updateReport{}
	if len(securityMode) > 0 {
		report.Security = securityMode[0]
	}
	for _, decision := range []string{"block", "hold", "review", "unknown"} {
		for _, gate := range gates {
			for _, finding := range gate.Findings {
				findingDecision := strings.ToLower(strings.TrimSpace(finding.Decision))
				if findingDecision == "" {
					findingDecision = "unknown"
				}
				if findingDecision != decision {
					continue
				}
				displayDecision := findingDecision
				if report.Security != "" {
					displayDecision = updateSafetyDisplayDecision(report, findingDecision)
				}
				version := strings.Join(finding.InstalledVersions, ",")
				if finding.CurrentVersion != "" {
					version += " -> " + finding.CurrentVersion
				}
				rows = append(rows, []string{
					textui.StyleStatus(displayDecision, color),
					textui.StyleName(gate.Provider, color),
					finding.Kind + "/" + finding.Name,
					version,
					truncate(oneLine(localizedSafetyReason(finding.Reason)), 72),
				})
				if len(rows) >= limit {
					return rows
				}
			}
		}
	}
	return rows
}

func printUpdateInventoryDashboard(w io.Writer, inventory plan.Report, color bool) {
	if len(inventory.Providers) == 0 && len(inventory.Items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s %s\n", textui.StyleHeading("inventory", color), textui.StyleStatus(string(inventory.Status), color))
	providerRows := [][]string{}
	for _, provider := range inventory.Providers {
		status := providerStatus(provider)
		if status == "ok" {
			continue
		}
		profileMismatches := profileMismatchCount(inventory.Items, provider.Name)
		providerRows = append(providerRows, []string{
			textui.StyleName(provider.Name, color),
			styleDriftCount(provider.Missing, color),
			styleDriftCount(provider.Extra, color),
			styleDriftCount(profileMismatches, color),
			textui.StyleStatus(status, color),
		})
	}
	if len(providerRows) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no provider drift", "provider drift はありません"), color))
	} else {
		textui.PrintTable(w, []textui.Column{
			{Header: "provider", Min: 8, Max: 12},
			{Header: "missing", Min: 7, Max: 8},
			{Header: "extra", Min: 5, Max: 8},
			{Header: "profile", Min: 7, Max: 8},
			{Header: tr("status", "状態"), Min: 7, Max: 12},
		}, providerRows, color)
	}
	itemRows := inventoryAttentionRows(inventory.Items, 10, color)
	if len(itemRows) > 0 {
		fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("top inventory items", "主な inventory 項目"), color))
		textui.PrintTable(w, []textui.Column{
			{Header: tr("status", "状態"), Min: 7, Max: 9},
			{Header: "provider", Min: 8, Max: 10},
			{Header: "kind", Min: 8, Max: 10},
			{Header: tr("name", "名前"), Min: 16, Max: 36},
			{Header: tr("detail", "詳細"), Min: 0, Max: 56},
		}, itemRows, color)
	}
}

func inventoryAttentionRows(items []plan.Item, limit int, color bool) [][]string {
	rows := [][]string{}
	for _, status := range attentionStatusOrder() {
		for _, item := range items {
			if item.Status != status {
				continue
			}
			rows = append(rows, []string{
				textui.StyleStatus(inventoryItemStatusLabel(item), color),
				textui.StyleName(item.Provider, color),
				item.Kind,
				item.Name,
				item.Detail,
			})
			if len(rows) >= limit {
				return rows
			}
		}
	}
	return rows
}

func profileMismatchCount(items []plan.Item, provider string) int {
	count := 0
	for _, item := range items {
		if strings.EqualFold(item.Provider, provider) && itemHasProfileMismatch(item) {
			count++
		}
	}
	return count
}

func printUpdateNextActions(w io.Writer, report updateReport, color bool) {
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading(tr("next", "次の操作"), color))
	actions := []string{
		tr("updev last                              # review this compact dashboard again", "updev last                              # この compact dashboard を再表示"),
		tr("updev last --section inventory --details # inspect full inventory detail", "updev last --section inventory --details # inventory 詳細を確認"),
		tr("updev last --section logs --details      # inspect update logs", "updev last --section logs --details      # update log を確認"),
		tr("updev last --format json                 # inspect the cached report as JSON", "updev last --format json                 # cached report を JSON で確認"),
		tr("updev list --status attention            # inspect inventory attention items", "updev list --status attention            # inventory 注意項目を確認"),
	}
	if hasSafetyAttention(report.Safety) {
		actions = append(actions, tr("updev last --section security --details  # inspect security evidence", "updev last --section security --details  # security evidence を確認"))
		actions = append(actions, tr("updev security review                    # inspect security candidates", "updev security review                    # security 候補を確認"))
	}
	for _, action := range actions {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(action, color))
	}
}

const (
	updateHubActionDashboard          = "dashboard"
	updateHubActionInventoryAll       = "inventory-all"
	updateHubActionInventoryAttention = "inventory-attention"
	updateHubActionInventoryDetails   = "inventory-details"
	updateHubActionManualPlan         = "manual-plan"
	updateHubActionBackends           = "backends"
	updateHubActionUpdatesFilter      = "updates-filter"
	updateHubActionSecurity           = "security"
	updateHubActionSecurityFilter     = "security-filter"
	updateHubActionLogs               = "logs"
	updateHubActionFull               = "full"
	updateHubActionJSON               = "json"
)

func runUpdateHub(report updateReport) {
	defaultAction := updateHubActionInventoryAll
	manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: report.Root, provider: manualProviderName})
	backendPlan := buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: report.Root})
	if manualPlan.AttentionCount > 0 {
		defaultAction = updateHubActionManualPlan
	} else if len(backendPlan.Findings) > 0 {
		defaultAction = updateHubActionBackends
	}
	detailStates := map[string]detailBrowserState{}
	for {
		action, err := runUpdevSelect("updev update", tr("Choose what to inspect from the cached update report.", "cached update report から確認する項目を選択します。"), updateHubChoices(report, manualPlan, backendPlan, defaultAction), defaultAction)
		if err != nil {
			return
		}
		if action == updevActionExit {
			return
		}
		defaultAction = action
		color := textui.ColorEnabled()
		switch action {
		case updateHubActionDashboard:
			printUpdateText(report)
		case updateHubActionInventoryAll:
			inventory := buildListReport(inventoryResult{Report: report.Inventory}, listOptions{})
			action, handled := runListFilteredBrowser("updev installed inventory", inventory, detailStates, color)
			if !handled {
				printLastInventorySection(os.Stdout, report, lastReportOptions{section: "inventory", details: true}, color)
				break
			}
			if action == updevActionExit {
				return
			}
			if action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
			}
			continue
		case updateHubActionInventoryAttention:
			printLastInventorySection(os.Stdout, report, lastReportOptions{section: "inventory", status: "attention", details: false}, color)
		case updateHubActionInventoryDetails:
			stateKey := "inventory-details"
			state, err := runDetailBrowserWithState("updev inventory details", updateInventoryDetailRows(report), detailStates[stateKey], color)
			if err != nil {
				printLastInventorySection(os.Stdout, report, lastReportOptions{section: "inventory", status: "attention", details: true}, color)
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
				continue
			}
			continue
		case updateHubActionManualPlan:
			printInventoryPlanText(os.Stdout, manualPlan)
		case updateHubActionBackends:
			stateKey := "backends"
			state, err := runDetailBrowserWithState("updev backend convergence", backendDetailRows(backendPlan), detailStates[stateKey], color)
			if err != nil {
				printBackendPlanText(os.Stdout, backendPlan, color)
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
				continue
			}
			continue
		case updateHubActionUpdatesFilter:
			opts, ok := selectUpdateStepFilter(report)
			if !ok {
				continue
			}
			filtered := filterUpdateReport(report, opts)
			printLastUpdateSteps(os.Stdout, filtered, true, color)
		case updateHubActionSecurity:
			stateKey := "security"
			state, err := runDetailBrowserWithState("updev security details", updateSecurityDetailRows(report), detailStates[stateKey], color)
			if err != nil {
				printLastSecuritySection(os.Stdout, report, true, color)
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
				continue
			}
			continue
		case updateHubActionSecurityFilter:
			opts, ok := selectUpdateSecurityFilter(report)
			if !ok {
				continue
			}
			filtered := filterUpdateReport(report, opts)
			stateKey := "security-filter:" + filterSummary(lastReportFilterMap(opts))
			state, err := runDetailBrowserWithState("updev security filter", updateSecurityDetailRowsForFilter(filtered, opts), detailStates[stateKey], color)
			if err != nil {
				printLastSecuritySection(os.Stdout, filtered, true, color)
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
				continue
			}
			continue
		case updateHubActionLogs:
			stateKey := "logs"
			state, err := runDetailBrowserWithState("updev update logs", updateLogDetailRows(report), detailStates[stateKey], color)
			if err != nil {
				printLastUpdateLogs(os.Stdout, report, color)
				break
			}
			detailStates[stateKey] = state
			if state.Action == updevActionExit {
				return
			}
			if state.Action == updevActionHome {
				defaultAction = updateHubActionInventoryAll
				continue
			}
			continue
		case updateHubActionFull:
			printUpdateBodyTo(os.Stdout, report, color)
		case updateHubActionJSON:
			entry := updateReportCacheEntry{Version: 1, Type: "update", CreatedAt: time.Now(), Report: report}
			if cached, ok := loadLastUpdateReport(); ok {
				entry = cached
			}
			_ = encodeJSON(buildUpdateReportSectionView(entry, lastReportOptions{section: "full"}))
		}
		next, err := runPostSectionNavigation()
		if err != nil || next == updevActionExit {
			return
		}
		if next == updevActionHome {
			defaultAction = updateHubActionInventoryAll
		}
	}
}

func updateHubChoices(report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, defaultAction string) []updevChoice {
	choices := []updevChoice{
		{Value: updateHubActionInventoryAll, Label: tr("Installed inventory", "インストール済み一覧"), Description: tr("Review all installed and desired inventory rows with grouping, filters, and expansion.", "すべての installed/desired inventory 行を group / filter / 展開付きで確認します。")},
		{Value: updateHubActionInventoryAttention, Label: tr("Attention items", "注意項目"), Description: tr("Show missing, extra, drift, held, blocked, error, and unavailable inventory rows.", "missing / extra / drift / held / blocked / error / unavailable の inventory 行を表示します。")},
		{Value: updateHubActionInventoryDetails, Label: tr("Inventory details", "inventory 詳細"), Description: tr("Expand attention inventory descriptions and versions.", "注意が必要な inventory の説明と version を展開します。")},
		{Value: updateHubActionUpdatesFilter, Label: tr("Updates filter", "updates filter"), Description: tr("Filter update steps by provider, status, or query.", "update step を provider / status / query で絞り込みます。")},
	}
	if attention := manualPlan.AttentionCount; attention > 0 {
		choices = append(choices, updevChoice{Value: updateHubActionManualPlan, Label: tr("Manual review plan", "手動アプリ確認"), Description: fmt.Sprintf(tr("Review %d manual/vendor app candidates and provider adoption suggestions.", "%d 件の手動/vendor app 候補と provider 採用提案を確認します。"), attention)})
	}
	if findings := len(backendPlan.Findings); findings > 0 {
		choices = append(choices, updevChoice{Value: updateHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: fmt.Sprintf(tr("Review %d provider/backend recommendations before platform expansion.", "%d 件の provider/backend 推奨を確認します。"), findings)})
	}
	if len(report.Safety) > 0 {
		choices = append(choices, updevChoice{Value: updateHubActionSecurity, Label: tr("Security", "security"), Description: tr("Show held/review security evidence and remediation.", "held/review の security evidence と remediation を表示します。")})
		choices = append(choices, updevChoice{Value: updateHubActionSecurityFilter, Label: tr("Security filter", "security filter"), Description: tr("Filter security evidence by provider, decision, or query.", "security evidence を provider / decision / query で絞り込みます。")})
	}
	choices = append(choices,
		updevChoice{Value: updateHubActionLogs, Label: tr("Update logs", "update logs"), Description: tr("Show captured stdout, stderr, and skipped reasons.", "stdout / stderr / skipped reason を表示します。")},
		updevChoice{Value: updateHubActionDashboard, Label: tr("Dashboard", "dashboard"), Description: tr("Reprint the compact update dashboard.", "compact update dashboard を再表示します。")},
		updevChoice{Value: updateHubActionFull, Label: tr("Full text report", "full text report"), Description: tr("Print the full cached report in text form.", "cached report 全体を text で表示します。")},
		updevChoice{Value: updateHubActionJSON, Label: tr("JSON export", "JSON export"), Description: tr("Print the cached report as JSON.", "cached report を JSON で表示します。")},
		updevChoice{Value: updevActionExit, Label: tr("Exit", "終了"), Description: tr("Leave the selector.", "selector を終了します。")},
	)
	for index := range choices {
		if choices[index].Value == defaultAction {
			choices[index].Selected = true
			break
		}
	}
	return choices
}

const (
	updateFilterActionProvider = "provider"
	updateFilterActionStatus   = "status"
	updateFilterActionDecision = "decision"
	updateFilterActionQuery    = "query"
)

func selectUpdateStepFilter(report updateReport) (lastReportOptions, bool) {
	action, err := runUpdevSelect("updev update filter", "Choose an update-step facet.", []updevChoice{
		{Value: updevActionBack, Label: "Back", Description: "Return to the update hub.", Selected: true},
		{Value: updateFilterActionProvider, Label: "Provider", Description: "Filter by update step provider."},
		{Value: updateFilterActionStatus, Label: "Status", Description: "Filter by ok, held, blocked, error, drift, or attention."},
		{Value: updateFilterActionQuery, Label: "Query", Description: "Search command, reason, stdout, and stderr."},
	}, updevActionBack)
	if err != nil || action == updevActionBack {
		return lastReportOptions{}, false
	}
	opts := lastReportOptions{section: "updates"}
	switch action {
	case updateFilterActionProvider:
		value, ok := selectUpdateStepProvider(report)
		if !ok {
			return lastReportOptions{}, false
		}
		opts.provider = value
	case updateFilterActionStatus:
		value, ok := selectUpdateStatus("updev update status", updateStepStatusCounts(report.Steps))
		if !ok {
			return lastReportOptions{}, false
		}
		opts.status = value
	case updateFilterActionQuery:
		value, ok := selectUpdateQuery("updev update query", "Search update commands, reasons, stdout, and stderr.")
		if !ok {
			return lastReportOptions{}, false
		}
		opts.query = value
	}
	return opts, true
}

func selectUpdateSecurityFilter(report updateReport) (lastReportOptions, bool) {
	action, err := runUpdevSelect("updev security filter", "Choose a security facet.", []updevChoice{
		{Value: updevActionBack, Label: "Back", Description: "Return to the update hub.", Selected: true},
		{Value: updateFilterActionProvider, Label: "Provider", Description: "Filter by security provider."},
		{Value: updateFilterActionDecision, Label: "Decision", Description: "Filter by hold, review, block, unknown, allow, or attention."},
		{Value: updateFilterActionQuery, Label: "Query", Description: "Search evidence, reason, remediation, and metadata."},
	}, updevActionBack)
	if err != nil || action == updevActionBack {
		return lastReportOptions{}, false
	}
	opts := lastReportOptions{section: "security"}
	switch action {
	case updateFilterActionProvider:
		value, ok := selectSecurityProvider(report)
		if !ok {
			return lastReportOptions{}, false
		}
		opts.provider = value
	case updateFilterActionDecision:
		value, ok := selectUpdateStatus("updev security decision", safetyDecisionCounts(report.Safety))
		if !ok {
			return lastReportOptions{}, false
		}
		opts.status = value
	case updateFilterActionQuery:
		value, ok := selectUpdateQuery("updev security query", "Search security reasons, evidence, remediation, advisory IDs, and URLs.")
		if !ok {
			return lastReportOptions{}, false
		}
		opts.query = value
	}
	return opts, true
}

func selectUpdateStepProvider(report updateReport) (string, bool) {
	counts := updateStepProviderCounts(report.Steps)
	return selectCountFacet("updev update provider", "Choose an update provider.", counts)
}

func selectSecurityProvider(report updateReport) (string, bool) {
	counts := safetyProviderCounts(report.Safety)
	return selectCountFacet("updev security provider", "Choose a security provider.", counts)
}

func selectUpdateStatus(title string, counts map[string]int) (string, bool) {
	return selectCountFacet(title, "Choose a status or decision filter.", counts)
}

func selectUpdateQuery(title string, description string) (string, bool) {
	value, err := runUpdevInput(title, description+" Empty input returns to the update hub.", "brew, hold, provenance, ...", "")
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}

func selectCountFacet(title string, description string, counts map[string]int) (string, bool) {
	choices := []updevChoice{{Value: updevActionBack, Label: "Back", Description: "Return to the update hub.", Selected: true}}
	for _, value := range sortedMapKeys(counts) {
		choices = append(choices, updevChoice{
			Value:       value,
			Label:       value,
			Description: fmt.Sprintf("%d rows", counts[value]),
		})
	}
	selected, err := runUpdevSelect(title, description, choices, updevActionBack)
	if err != nil || selected == updevActionBack {
		return "", false
	}
	return selected, true
}

func updateStepProviderCounts(steps []updateStep) map[string]int {
	counts := map[string]int{}
	for _, step := range steps {
		if step.Name != "" {
			counts[step.Name]++
		}
	}
	return counts
}

func updateStepStatusCounts(steps []updateStep) map[string]int {
	counts := map[string]int{}
	attention := 0
	for _, step := range steps {
		status := string(step.Status)
		if status == "" {
			status = string(plan.StatusOK)
		}
		counts[status]++
		if isAttentionStatus(step.Status) {
			attention++
		}
	}
	if attention > 0 {
		counts["attention"] = attention
	}
	return counts
}

func safetyProviderCounts(gates []safetyGate) map[string]int {
	counts := map[string]int{}
	for _, gate := range gates {
		if gate.Provider == "" {
			continue
		}
		count := len(gate.Findings) + len(gate.Warnings)
		if gate.Error != "" {
			count++
		}
		if count == 0 {
			count = 1
		}
		counts[gate.Provider] += count
	}
	return counts
}

func safetyDecisionCounts(gates []safetyGate) map[string]int {
	counts := map[string]int{}
	attention := 0
	for _, gate := range gates {
		if gate.Error != "" {
			counts["error"]++
			attention++
		}
		if len(gate.Warnings) > 0 {
			attention += len(gate.Warnings)
		}
		for _, finding := range gate.Findings {
			decision := firstNonEmpty(finding.Decision, "unknown")
			counts[decision]++
			if !strings.EqualFold(decision, "allow") {
				attention++
			}
		}
	}
	if attention > 0 {
		counts["attention"] = attention
	}
	return counts
}

func printLastUpdateSteps(w io.Writer, report updateReport, details bool, color bool) {
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("updates", color))
	if len(report.Steps) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim("no matching update steps", color))
		return
	}
	printUpdateStepsTable(w, report.Steps, color, updateStepTableLabels{
		Name:    "name",
		Status:  "status",
		Skipped: "skipped",
		Detail:  "detail",
	})
	if details {
		printLastUpdateLogs(w, report, color)
	}
}

func printLastSecuritySection(w io.Writer, report updateReport, details bool, color bool) {
	if len(report.Safety) == 0 {
		fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("security attention", color))
		fmt.Fprintf(w, "  %s\n", textui.StyleDim("no matching security gates", color))
		return
	}
	printUpdateSafetyDashboard(w, report, color)
	if details {
		printSafetyFindingDetails(w, report.Safety, color)
	}
}

func updateInventoryDetailRows(report updateReport) []detailBrowserRow {
	inventory := buildListReport(inventoryResult{Report: report.Inventory}, listOptions{
		status: "attention",
		limit:  20,
	})
	return listDetailRows(inventory)
}

func updateSecurityDetailRows(report updateReport) []detailBrowserRow {
	return updateSecurityDetailRowsWithAllow(report, false)
}

func updateSecurityDetailRowsForFilter(report updateReport, opts lastReportOptions) []detailBrowserRow {
	return updateSecurityDetailRowsWithAllow(report, strings.EqualFold(opts.status, "allow"))
}

func updateSecurityDetailRowsWithAllow(report updateReport, includeAllow bool) []detailBrowserRow {
	rows := []detailBrowserRow{}
	for _, gate := range report.Safety {
		if strings.TrimSpace(gate.Error) != "" {
			rows = append(rows, detailBrowserRow{
				Title:   gate.Provider + " scanner",
				Status:  string(plan.StatusError),
				Summary: gate.Error,
				Detail:  gate.Error,
			})
		}
		for _, warning := range gate.Warnings {
			rows = append(rows, detailBrowserRow{
				Title:   gate.Provider + " warning",
				Status:  string(plan.StatusHeld),
				Summary: warning,
				Detail:  warning,
			})
		}
		for _, finding := range gate.Findings {
			if !includeAllow && strings.EqualFold(finding.Decision, "allow") {
				continue
			}
			rows = append(rows, safetyFindingDetailRow(gate, finding))
		}
	}
	return rows
}

func safetyFindingDetailRow(gate safetyGate, finding safetyFinding) detailBrowserRow {
	reason := localizedSafetyReason(finding.Reason)
	remediation := localizedSafetyRemediation(finding.Remediation)
	metadata := []string{
		"provider: " + firstNonEmpty(finding.Provider, gate.Provider),
		"kind: " + finding.Kind,
	}
	metadata = appendDetailMeta(metadata, "reason", reason)
	metadata = appendDetailMeta(metadata, "remediation", remediation)
	metadata = appendDetailMeta(metadata, "source", finding.Source)
	metadata = appendDetailMeta(metadata, "tap", finding.Tap)
	metadata = appendDetailMeta(metadata, "publisher", finding.Publisher)
	metadata = appendDetailMeta(metadata, "repository", finding.RepositoryURL)
	metadata = appendDetailMeta(metadata, "support", finding.SupportURL)
	metadata = appendDetailMeta(metadata, "homepage", finding.Homepage)
	metadata = appendDetailMeta(metadata, "url", finding.URL)
	metadata = appendDetailMeta(metadata, "release", finding.ReleaseDate)
	if versions := versionText(finding); versions != "" {
		metadata = appendDetailMeta(metadata, "version", versions)
	}
	if len(finding.AdvisoryIDs) > 0 {
		metadata = appendDetailMeta(metadata, "advisories", strings.Join(finding.AdvisoryIDs, ", "))
	}
	if len(finding.FixedVersions) > 0 {
		metadata = appendDetailMeta(metadata, "fixed", strings.Join(finding.FixedVersions, ", "))
	}
	if len(finding.Evidence) > 0 {
		metadata = appendDetailMeta(metadata, "evidence", strings.Join(finding.Evidence, "; "))
	}
	return detailBrowserRow{
		Title:    gate.Provider + "/" + finding.Kind + " " + finding.Name,
		Status:   firstNonEmpty(finding.Decision, string(gate.Status)),
		Summary:  firstNonEmpty(reason, remediation, strings.Join(finding.Evidence, "; ")),
		Detail:   firstNonEmpty(remediation, reason, strings.Join(finding.Evidence, "; ")),
		Metadata: metadata,
	}
}

func appendDetailMeta(metadata []string, label string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return metadata
	}
	return append(metadata, label+": "+value)
}

func updateLogDetailRows(report updateReport) []detailBrowserRow {
	rows := []detailBrowserRow{}
	for _, step := range report.Steps {
		if step.Reason == "" && step.Stdout == "" && step.Stderr == "" {
			continue
		}
		metadata := []string{"command: " + joinCommand(step.Command)}
		if step.Reason != "" {
			metadata = append(metadata, "reason: "+step.Reason)
		}
		if step.Stdout != "" {
			metadata = append(metadata, "stdout: "+step.Stdout)
		}
		if step.Stderr != "" {
			metadata = append(metadata, "stderr: "+step.Stderr)
		}
		rows = append(rows, detailBrowserRow{
			Title:    step.Name,
			Status:   string(step.Status),
			Summary:  firstNonEmpty(step.Reason, oneLine(step.Stdout), oneLine(step.Stderr)),
			Detail:   firstNonEmpty(step.Reason, step.Stdout, step.Stderr),
			Metadata: metadata,
		})
	}
	return rows
}

func printSafetyFindingDetails(w io.Writer, gates []safetyGate, color bool) {
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("security details", color))
	wrote := false
	for _, gate := range gates {
		if gate.Error != "" {
			wrote = true
			fmt.Fprintf(w, "%s %s\n", textui.StyleName(gate.Provider, color), textui.StyleError(gate.Error, color))
		}
		for _, warning := range gate.Warnings {
			wrote = true
			fmt.Fprintf(w, "%s %s\n", textui.StyleName(gate.Provider, color), textui.StyleWarning("warning: "+warning, color))
		}
		for _, finding := range gate.Findings {
			wrote = true
			fmt.Fprintf(w, "%s %s %s\n", textui.StyleStatus(finding.Decision, color), textui.StyleName(gate.Provider+"/"+finding.Kind+" "+finding.Name, color), versionText(finding))
			printDetailLine(w, "reason", localizedSafetyReason(finding.Reason), color)
			printDetailLine(w, "remediation", localizedSafetyRemediation(finding.Remediation), color)
			printDetailLine(w, "source", finding.Source, color)
			printDetailLine(w, "tap", finding.Tap, color)
			printDetailLine(w, "publisher", finding.Publisher, color)
			printDetailLine(w, "repository", finding.RepositoryURL, color)
			printDetailLine(w, "support", finding.SupportURL, color)
			printDetailLine(w, "homepage", finding.Homepage, color)
			printDetailLine(w, "url", finding.URL, color)
			printDetailLine(w, "release", finding.ReleaseDate, color)
			if len(finding.AdvisoryIDs) > 0 {
				printDetailLine(w, "advisories", strings.Join(finding.AdvisoryIDs, ", "), color)
			}
			if len(finding.FixedVersions) > 0 {
				printDetailLine(w, "fixed", strings.Join(finding.FixedVersions, ", "), color)
			}
			if len(finding.Evidence) > 0 {
				printDetailLine(w, "evidence", strings.Join(finding.Evidence, "; "), color)
			}
		}
	}
	if !wrote {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim("no matching security details", color))
	}
}

func versionText(finding safetyFinding) string {
	parts := []string{}
	if len(finding.InstalledVersions) > 0 {
		parts = append(parts, strings.Join(finding.InstalledVersions, ","))
	}
	if finding.CurrentVersion != "" {
		parts = append(parts, "-> "+finding.CurrentVersion)
	}
	if finding.Version != "" {
		parts = append(parts, finding.Version)
	}
	return strings.Join(parts, " ")
}

func printLastInventorySection(w io.Writer, report updateReport, opts lastReportOptions, color bool) {
	if opts.details {
		inventory := buildListReport(inventoryResult{Report: report.Inventory}, listOptionsFromLastReport(opts))
		printListText(w, inventory, "inventory details", color)
		return
	}
	printUpdateInventoryDashboard(w, report.Inventory, color)
}

func listOptionsFromLastReport(opts lastReportOptions) listOptions {
	return listOptions{
		provider: opts.provider,
		status:   opts.status,
		query:    opts.query,
		details:  opts.details,
	}
}

func printLastUpdateLogs(w io.Writer, report updateReport, color bool) {
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("update logs", color))
	wrote := false
	for _, step := range report.Steps {
		if step.Reason == "" && step.Stdout == "" && step.Stderr == "" {
			continue
		}
		wrote = true
		fmt.Fprintf(w, "%s %s\n", textui.StyleName(step.Name, color), textui.StyleStatus(string(step.Status), color))
		printDetailLine(w, "reason", step.Reason, color)
		printDetailBlock(w, "stdout", step.Stdout, color)
		printDetailBlock(w, "stderr", step.Stderr, color)
	}
	if !wrote {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim("no logs captured", color))
	}
}

func printDetailLine(w io.Writer, label string, value string, color bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(w, "  %s %s\n", textui.StyleLabel(label+":", color), value)
}

func localizedSafetyReason(reason string) string {
	return localizedSecurityReason(reason)
}

func localizedSafetyReasonForLang(lang string, reason string) string {
	return localizedSecurityReasonForLang(lang, reason)
}

func localizedSecurityReason(reason string) string {
	return localizedSecurityReasonForLang(defaultLanguage(), reason)
}

func localizedSecurityReasonForLang(lang string, reason string) string {
	return i18n.LocalizedSecurityReason(lang, reason)
}

func localizedSafetyRemediation(remediation string) string {
	return localizedSecurityRemediation(remediation)
}

func localizedSafetyRemediationForLang(lang string, remediation string) string {
	return localizedSecurityRemediationForLang(lang, remediation)
}

func localizedSecurityRemediation(remediation string) string {
	return localizedSecurityRemediationForLang(defaultLanguage(), remediation)
}

func localizedSecurityRemediationForLang(lang string, remediation string) string {
	return i18n.LocalizedSecurityRemediation(lang, remediation)
}

func printDetailBlock(w io.Writer, label string, value string, color bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(w, "  %s\n", textui.StyleLabel(label+":", color))
	for _, line := range strings.Split(value, "\n") {
		fmt.Fprintf(w, "    %s\n", line)
	}
}

func hasSafetyAttention(gates []safetyGate) bool {
	for _, gate := range gates {
		if gate.Status == plan.StatusError || gate.Status == plan.StatusHeld || gate.Status == plan.StatusBlocked {
			return true
		}
		for _, finding := range gate.Findings {
			if !strings.EqualFold(finding.Decision, "allow") {
				return true
			}
		}
	}
	return false
}

func printSafetyText(gates []safetyGate) {
	printSafetyTextTo(os.Stdout, gates)
}

func safetySummaryText(gates []safetyGate) string {
	if len(gates) == 0 {
		return ""
	}
	gateCounts := map[plan.Status]int{}
	decisionCounts := map[string]int{}
	findings := 0
	for _, gate := range gates {
		status := gate.Status
		if status == "" {
			status = plan.StatusOK
		}
		gateCounts[status]++
		for _, finding := range gate.Findings {
			findings++
			decision := strings.ToLower(strings.TrimSpace(finding.Decision))
			if decision == "" {
				decision = "unknown"
			}
			decisionCounts[decision]++
		}
	}
	parts := []string{fmt.Sprintf(tr("%d gates", "gate %d件"), len(gates))}
	for _, status := range []plan.Status{plan.StatusHeld, plan.StatusBlocked, plan.StatusError} {
		if count := gateCounts[status]; count > 0 {
			parts = append(parts, fmt.Sprintf(tr("%d %s gates", "gate %d件 %s"), count, status))
		}
	}
	if findings > 0 {
		decisionParts := []string{}
		for _, decision := range []string{"allow", "review", "hold", "block", "unknown"} {
			if count := decisionCounts[decision]; count > 0 {
				decisionParts = append(decisionParts, fmt.Sprintf(tr("%d %s", "%d %s"), count, decision))
			}
		}
		parts = append(parts, fmt.Sprintf(tr("%d findings (%s)", "finding %d件 (%s)"), findings, strings.Join(decisionParts, ", ")))
	}
	return strings.Join(parts, ", ")
}

func printSafetyTextTo(w io.Writer, gates []safetyGate) {
	if len(gates) == 0 {
		return
	}
	color := textui.ColorEnabled()
	fmt.Fprintf(w, "\n%s\n", textui.StyleHeading("safety", color))
	for _, gate := range gates {
		fmt.Fprintf(w, "  %s %s\n", textui.StyleName(gate.Provider, color), textui.StyleStatus(string(gate.Status), color))
		for _, warning := range gate.Warnings {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleWarning("warning:", color), truncate(oneLine(warning), 120))
		}
		if gate.Error != "" {
			fmt.Fprintf(w, "    %s %s\n", textui.StyleError("error:", color), truncate(oneLine(gate.Error), 120))
			continue
		}
		for _, finding := range gate.Findings {
			fmt.Fprintf(w, "    %-6s %-32s %-8s %s -> %s\n", finding.Kind, truncate(finding.Name, 32), textui.StyleStatus(finding.Decision, color), strings.Join(finding.InstalledVersions, ","), finding.CurrentVersion)
			fmt.Fprintf(w, "      %s\n", localizedSafetyReason(finding.Reason))
			if finding.Remediation != "" {
				fmt.Fprintf(w, "      %s %s\n", textui.StyleLabel("next:", color), truncate(oneLine(localizedSafetyRemediation(finding.Remediation)), 120))
			}
		}
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func joinCommand(command []string) string {
	parts := make([]string, 0, len(command))
	for _, part := range command {
		if strings.ContainsAny(part, " \t\n&|;()<>$`\"'") {
			parts = append(parts, strconv.Quote(part))
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
