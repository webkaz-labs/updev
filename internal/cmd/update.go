package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/mise"
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
	miseBumpMode  string
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

type envCommandRunner interface {
	RunWithEnv(ctx context.Context, env []string, name string, args ...string) runner.Result
}

type envStreamingCommandRunner interface {
	RunStreamingWithEnv(ctx context.Context, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result
}

func parseUpdateOptions(args []string) (updateOptions, error) {
	opts := updateOptions{format: "text", root: defaultRoot(), inventory: "fast", security: defaultUpdateSecurityMode(), miseBumpMode: defaultMiseBumpMode(), policy: securityPolicyPath()}
	plain := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--plain":
			plain = true
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
	if plain {
		opts.format = "text"
		opts.noTUI = true
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
	mode := "strict"
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
	streamProviderLogs := shouldStreamUpdateProviderLogs(opts)
	for _, step := range updateSteps() {
		if refreshedStep, refreshedGate, ok := runStrictBrewRefreshIfNoCandidates(ctx, commandRunner, step, opts, policyUse.Policy, report.Safety, streamProviderLogs); ok {
			report.Safety = replaceUpdateSafetyGate(report.Safety, refreshedGate)
			if refreshedStep.Status == plan.StatusError {
				report.Status = plan.StatusError
			} else if refreshedStep.Status == plan.StatusHeld && report.Status != plan.StatusError {
				report.Status = plan.StatusHeld
			} else if refreshedStep.Status == plan.StatusDrift && report.Status == plan.StatusOK {
				report.Status = plan.StatusDrift
			}
			report.Steps = append(report.Steps, refreshedStep)
			continue
		}
		step, holdReason := updateStepWithStrictSafety(step, opts, report.Safety)
		if streamProviderLogs {
			fmt.Fprintf(updateProviderProgressWriter(), tr("running %s update...\n", "%s update を実行中...\n"), step.Name)
		}
		result := runUpdateStepWithOutput(ctx, commandRunner, step, opts.dryRun, holdReason, streamProviderLogs)
		if result.Status == plan.StatusError {
			report.Status = plan.StatusError
		} else if result.Status == plan.StatusHeld && report.Status != plan.StatusError {
			report.Status = plan.StatusHeld
		} else if result.Status == plan.StatusDrift && report.Status == plan.StatusOK {
			report.Status = plan.StatusDrift
		}
		report.Steps = append(report.Steps, result)
	}
	if bumpStep, ok := runMiseBumpUpdateStep(ctx, commandRunner, opts, report.Safety, streamProviderLogs); ok {
		if bumpStep.Status == plan.StatusError {
			report.Status = plan.StatusError
		} else if bumpStep.Status == plan.StatusHeld && report.Status != plan.StatusError {
			report.Status = plan.StatusHeld
		} else if bumpStep.Status == plan.StatusDrift && report.Status == plan.StatusOK {
			report.Status = plan.StatusDrift
		}
		report.Steps = append(report.Steps, bumpStep)
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
	if opts.format == "text" && !opts.dryRun && report.Inventory.Status != plan.StatusError && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		translationOpts := listOptions{format: "text", root: opts.root, autoTranslate: true}
		if shouldAutoUpdateListTranslations(translationOpts) {
			progress := newStartupProgress(os.Stdin, os.Stderr, opts.format, descriptionTranslationProgressMessage(defaultLanguage()))
			inventory := buildListReport(inventoryResult{Report: report.Inventory}, translationOpts)
			progress.Start()
			_ = maybeUpdateListTranslations(translationOpts, inventory)
			progress.Done()
		}
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
		if shouldRunUpdateHub(opts, os.Stdin, os.Stdout) {
			runUpdateHub(report)
		} else {
			printUpdateText(report)
			warnInteractiveUnavailable(os.Stdin, os.Stdout, opts.format, opts.tui, opts.noTUI)
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
		return runUpdateStepWithWriters(ctx, commandRunner, step, dryRun, holdReason, updateProviderStdoutWriter(), os.Stderr)
	}
	return runUpdateStepWithWriters(ctx, commandRunner, step, dryRun, holdReason, nil, nil)
}

func shouldStreamUpdateProviderLogs(opts updateOptions) bool {
	return opts.format == "text" && !opts.dryRun
}

func updateProviderProgressWriter() io.Writer {
	return os.Stderr
}

func updateProviderStdoutWriter() io.Writer {
	return updateProviderStdoutWriterForTerminal(isTerminal(os.Stdin), isTerminal(os.Stdout))
}

func updateProviderStdoutWriterForTerminal(stdinTTY bool, stdoutTTY bool) io.Writer {
	if stdinTTY && stdoutTTY {
		return os.Stderr
	}
	return os.Stdout
}

func runUpdateStepWithWriters(ctx context.Context, commandRunner commandRunner, step updateStep, dryRun bool, holdReason string, stdout io.Writer, stderr io.Writer) updateStep {
	if holdReason != "" {
		step.Status = plan.StatusHeld
		step.Reason = holdReason
		step.Skipped = true
		step.SkippedItems = append(step.SkippedItems, holdReason)
		return step
	}
	preSkipped := append([]string(nil), step.SkippedItems...)
	preReason := step.Reason
	if dryRun {
		step.Status = plan.StatusOK
		if len(preSkipped) > 0 {
			step.Status = plan.StatusHeld
			step.Skipped = true
			step.Reason = firstNonEmpty(preReason, tr("strict safety would apply safe candidates and hold unsafe candidates", "strict safety は safe 候補だけを適用し unsafe 候補を hold します"))
		}
		return step
	}
	result := runner.Result{}
	if stdout != nil || stderr != nil {
		if step.Name == "mise" {
			result = runMiseCommand(ctx, commandRunner, stdout, stderr, step.Command[0], step.Command[1:]...)
		} else if streamingRunner, ok := commandRunner.(streamingCommandRunner); ok {
			result = streamingRunner.RunStreaming(ctx, stdout, stderr, step.Command[0], step.Command[1:]...)
		} else {
			result = commandRunner.Run(ctx, step.Command[0], step.Command[1:]...)
		}
	} else if step.Name == "mise" {
		result = runMiseCommand(ctx, commandRunner, nil, nil, step.Command[0], step.Command[1:]...)
	} else {
		result = commandRunner.Run(ctx, step.Command[0], step.Command[1:]...)
	}
	step.Stdout = result.Stdout
	step.Stderr = result.Stderr
	updated, skipped := summarizeUpdateStepLog(step)
	step.Updated = updated
	step.SkippedItems = append(preSkipped, skipped...)
	if result.Code != 0 || result.Err != nil {
		step.Status = plan.StatusError
		return step
	}
	if len(preSkipped) > 0 {
		step.Status = plan.StatusHeld
		step.Skipped = len(step.Updated) == 0
		step.Reason = firstNonEmpty(preReason, tr("strict safety applied safe candidates and held unsafe candidates", "strict safety は safe 候補を適用し unsafe 候補を hold しました"))
		return step
	}
	step.Status = plan.StatusOK
	return step
}

func runStrictBrewRefreshIfNoCandidates(ctx context.Context, commandRunner commandRunner, step updateStep, opts updateOptions, policy securityPolicy, gates []safetyGate, stream bool) (updateStep, safetyGate, bool) {
	if step.Name != "brew" || opts.security != "strict" || opts.dryRun {
		return updateStep{}, safetyGate{}, false
	}
	gate, ok := updateSafetyGateForProvider("brew", gates)
	if !ok || gate.Status != plan.StatusOK || len(gate.Findings) > 0 {
		return updateStep{}, safetyGate{}, false
	}
	refreshStep := updateStep{
		Name:    "brew",
		Command: []string{"bash", "-lc", "brew update"},
		Reason:  "strict safety refreshed Homebrew metadata before rechecking package candidates",
	}
	if stream {
		fmt.Fprintf(updateProviderProgressWriter(), tr("running %s update...\n", "%s update を実行中...\n"), refreshStep.Name)
	}
	refreshStep = runUpdateStepWithOutput(ctx, commandRunner, refreshStep, false, "", stream)
	if refreshStep.Status == plan.StatusError {
		return refreshStep, gate, true
	}
	refreshedGate := collectBrewUpdateSafetyFreshWithPolicy(ctx, commandRunner, opts.root, policy)
	if refreshedGate.Status == plan.StatusError {
		refreshStep.Status = plan.StatusError
		refreshStep.Reason = "Homebrew safety gate failed after metadata refresh: " + refreshedGate.Error
		return refreshStep, refreshedGate, true
	}
	if len(refreshedGate.Findings) == 0 {
		refreshStep.Status = plan.StatusOK
		refreshStep.Reason = "strict safety refreshed Homebrew metadata; no package candidates found"
		return refreshStep, refreshedGate, true
	}
	scoped, holdReason := updateStepWithStrictSafety(updateSteps()[0], opts, []safetyGate{refreshedGate})
	if holdReason != "" {
		scoped = runUpdateStepWithOutput(ctx, commandRunner, scoped, false, holdReason, stream)
		scoped.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(refreshStep.Stdout, scoped.Stdout), "\n"))
		scoped.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(refreshStep.Stderr, scoped.Stderr), "\n"))
		return scoped, refreshedGate, true
	}
	scoped = runUpdateStepWithOutput(ctx, commandRunner, scoped, false, "", stream)
	scoped.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(refreshStep.Stdout, scoped.Stdout), "\n"))
	scoped.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(refreshStep.Stderr, scoped.Stderr), "\n"))
	return scoped, refreshedGate, true
}

func replaceUpdateSafetyGate(gates []safetyGate, replacement safetyGate) []safetyGate {
	out := append([]safetyGate(nil), gates...)
	for index, gate := range out {
		if gate.Provider == replacement.Provider {
			out[index] = replacement
			return out
		}
	}
	return append(out, replacement)
}

func updateStepWithStrictSafety(step updateStep, opts updateOptions, gates []safetyGate) (updateStep, string) {
	if opts.security != "strict" {
		return step, ""
	}
	gate, ok := updateSafetyGateForProvider(step.Name, gates)
	if !ok {
		return step, ""
	}
	if gate.Status == plan.StatusError {
		return step, "security=strict held update because safety gate failed: " + gate.Error
	}
	safe, unsafe := splitUpdateSafetyFindings(gate.Findings)
	if step.Name == "brew" && len(safe) == 0 && len(unsafe) == 0 {
		step.Command = []string{"bash", "-lc", "brew update"}
		step.Reason = "strict safety refreshes Homebrew metadata only before rechecking package candidates"
		return step, ""
	}
	if len(safe) == 0 && gate.Status == plan.StatusHeld {
		if step.Name == "brew" && len(unsafe) > 0 {
			step.Command = []string{"bash", "-lc", "brew update"}
			step.Reason = fmt.Sprintf("strict safety refreshed Homebrew metadata and held %d Homebrew candidates requiring review", len(unsafe))
			step.SkippedItems = updateSafetySkippedSummaries(unsafe)
			return step, ""
		}
		return step, "security=strict held update because safety gate requires review"
	}
	if len(safe) == 0 {
		return step, ""
	}
	switch step.Name {
	case "mise":
		command := scopedMiseUpgradeCommand(opts.root, safe)
		if len(command) == 0 {
			return step, "security=strict held mise update because no scoped safe candidates were found"
		}
		step.Command = command
		if len(unsafe) > 0 {
			step.Reason = fmt.Sprintf("strict safety will apply %d safe mise candidates and hold %d unsafe candidates", len(safe), len(unsafe))
		}
	case "brew":
		command := scopedBrewUpgradeCommand(safe)
		if len(command) == 0 {
			return step, "security=strict held brew update because no scoped safe candidates were found"
		}
		step.Command = command
		if len(unsafe) > 0 {
			step.Reason = fmt.Sprintf("strict safety will apply %d safe Homebrew candidates and hold %d unsafe candidates; Homebrew cannot generally install an older intermediate release", len(safe), len(unsafe))
		}
	default:
		if gate.Status == plan.StatusHeld {
			return step, "security=strict held update because safety gate requires review"
		}
		return step, ""
	}
	step.SkippedItems = updateSafetySkippedSummaries(unsafe)
	return step, ""
}

func updateSafetyGateForProvider(provider string, gates []safetyGate) (safetyGate, bool) {
	for _, gate := range gates {
		if gate.Provider == provider {
			return gate, true
		}
	}
	return safetyGate{}, false
}

func splitUpdateSafetyFindings(findings []safetyFinding) ([]safetyFinding, []safetyFinding) {
	safe := []safetyFinding{}
	unsafe := []safetyFinding{}
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
			safe = append(safe, finding)
			continue
		}
		unsafe = append(unsafe, finding)
	}
	return safe, unsafe
}

func scopedMiseUpgradeCommand(root string, findings []safetyFinding) []string {
	tools := updateSafetyFindingNames(findings)
	if len(tools) == 0 {
		return nil
	}
	miseCommand := []string{"mise", "upgrade", "--yes"}
	if age := miseMinimumReleaseAgeFlagValue(); age != "" {
		miseCommand = append(miseCommand, "--minimum-release-age", age)
	}
	if strings.TrimSpace(root) != "" {
		miseCommand = append(miseCommand, "--cd", root)
	}
	miseCommand = append(miseCommand, tools...)
	return []string{"zsh", "-c", "source ~/.zshenv && " + joinCommand(miseCommand) + " && mise prune"}
}

func miseGitHubTokenEnv() []string {
	if strings.TrimSpace(os.Getenv("MISE_GITHUB_TOKEN")) != "" {
		return nil
	}
	token := githubToken()
	if token == "" {
		return nil
	}
	return []string{"MISE_GITHUB_TOKEN=" + token}
}

func runMiseCommand(ctx context.Context, commandRunner commandRunner, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result {
	env := miseGitHubTokenEnv()
	if stdout != nil || stderr != nil {
		if envRunner, ok := commandRunner.(envStreamingCommandRunner); ok {
			return envRunner.RunStreamingWithEnv(ctx, env, stdout, stderr, name, args...)
		}
		if streamingRunner, ok := commandRunner.(streamingCommandRunner); ok {
			return streamingRunner.RunStreaming(ctx, stdout, stderr, name, args...)
		}
	}
	if envRunner, ok := commandRunner.(envCommandRunner); ok {
		return envRunner.RunWithEnv(ctx, env, name, args...)
	}
	return commandRunner.Run(ctx, name, args...)
}

func miseMinimumReleaseAgeFlagValue() string {
	days := int(minMiseReleaseAge().Hours() / 24)
	if days <= 0 {
		return ""
	}
	return fmt.Sprintf("%dd", days)
}

func scopedBrewUpgradeCommand(findings []safetyFinding) []string {
	names := updateSafetyFindingNames(findings)
	if len(names) == 0 {
		return nil
	}
	upgrade := append([]string{"brew", "upgrade", "--greedy"}, names...)
	return []string{"bash", "-lc", "HOMEBREW_NO_AUTO_UPDATE=1 " + joinCommand(upgrade) + " && HOMEBREW_NO_AUTO_UPDATE=1 brew cleanup && brew update"}
}

func updateSafetyFindingNames(findings []safetyFinding) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, finding := range findings {
		name := strings.TrimSpace(finding.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func updateSafetySkippedSummaries(findings []safetyFinding) []string {
	out := []string{}
	for _, finding := range findings {
		summary := updateSafetyFindingSummary(finding)
		if summary != "" {
			out = append(out, summary)
		}
	}
	return out
}

func updateSafetyFindingSummary(finding safetyFinding) string {
	name := strings.TrimSpace(finding.Name)
	if name == "" {
		name = strings.TrimSpace(finding.Kind)
	}
	if name == "" {
		name = strings.TrimSpace(finding.Provider)
	}
	version := strings.TrimSpace(firstNonEmpty(finding.CurrentVersion, finding.Version))
	detail := strings.TrimSpace(localizedSafetyReasonWithReleaseAge(finding))
	if version != "" {
		name += " -> " + version
	}
	decision := strings.TrimSpace(finding.Decision)
	if decision != "" {
		name += " " + decision
	}
	if detail != "" {
		name += ": " + detail
	}
	return strings.TrimSpace(name)
}

func runMiseBumpUpdateStep(ctx context.Context, commandRunner commandRunner, opts updateOptions, gates []safetyGate, stream bool) (updateStep, bool) {
	mode := strings.ToLower(strings.TrimSpace(opts.miseBumpMode))
	if mode == "" || mode == "off" {
		return updateStep{}, false
	}
	gate, ok := miseBumpGate(gates)
	if !ok || len(gate.Findings) == 0 {
		return updateStep{}, false
	}
	safe := safeMiseBumpFindings(gate)
	unsafe := unsafeMiseBumpFindings(gate)
	step := updateStep{
		Name:    miseBumpProvider,
		Command: miseBumpCommandForFindings(opts.root, false, false, safe),
	}
	for _, finding := range unsafe {
		step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding))
	}
	switch mode {
	case "manual":
		step.Status = plan.StatusDrift
		step.Skipped = true
		step.Reason = fmt.Sprintf("mise bump candidates available; mode=manual requires item review")
		for _, finding := range safe {
			step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding))
		}
		if len(safe) == 0 && len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			step.Reason = "mise bump candidates require review"
		}
		return step, true
	case "safe":
		step.Status = plan.StatusDrift
		step.Skipped = true
		step.Reason = fmt.Sprintf("mise bump candidates available; %d safe candidates can be applied after confirmation", len(safe))
		for _, finding := range safe {
			step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding))
		}
		if len(safe) == 0 && len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			step.Reason = "mise bump candidates require review"
		}
		return step, true
	case "auto":
	default:
		return updateStep{}, false
	}
	if len(safe) == 0 {
		step.Status = plan.StatusHeld
		step.Skipped = true
		step.Reason = "mise bump candidates require review; no safe auto candidates"
		return step, true
	}
	if opts.dryRun {
		step.Status = plan.StatusDrift
		step.Reason = fmt.Sprintf("mise bump auto would apply %d safe candidates", len(safe))
		for _, finding := range safe {
			step.Updated = append(step.Updated, "would bump "+miseBumpFindingSummary(finding))
		}
		if len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			step.Reason = fmt.Sprintf("mise bump auto would apply %d safe candidates; %d candidates require review", len(safe), len(unsafe))
		}
		return step, true
	}
	if err := validateMiseBumpPlannedCandidates(ctx, commandRunner, opts.root, safe); err != nil {
		step.Status = plan.StatusHeld
		step.Skipped = true
		step.Reason = "mise bump candidate set changed before apply: " + err.Error()
		return step, true
	}
	reviewCount := len(unsafe)
	preflight := runMiseBumpCommand(ctx, commandRunner, opts.root, true, false, safe, nil, nil)
	step.Stdout = preflight.Stdout
	step.Stderr = preflight.Stderr
	blocked, remaining := splitMiseBumpDependencyBlockedFindings(safe, preflight.Stdout+"\n"+preflight.Stderr)
	if len(blocked) > 0 {
		reviewCount += len(blocked)
		for _, finding := range blocked {
			step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding)+" review: mise dependency is not in the current install set")
		}
		safe = remaining
		step.Command = miseBumpCommandForFindings(opts.root, true, false, safe)
		if len(safe) == 0 {
			step.Status = plan.StatusHeld
			step.Skipped = true
			step.Reason = "mise bump auto found only dependency-blocked candidates"
			return step, true
		}
		preflight = runMiseBumpCommand(ctx, commandRunner, opts.root, true, false, safe, nil, nil)
		step.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(step.Stdout, preflight.Stdout), "\n"))
		step.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(step.Stderr, preflight.Stderr), "\n"))
	}
	if preflight.Code != 0 || preflight.Err != nil {
		step.Status = plan.StatusHeld
		step.Skipped = true
		step.Reason = "mise bump dry-run preflight failed: " + miseOutdatedResultDetail(preflight, "preflight failed")
		return step, true
	}
	var stdout io.Writer
	var stderr io.Writer
	if stream {
		stdout = updateProviderStdoutWriter()
		stderr = os.Stderr
		fmt.Fprintf(updateProviderProgressWriter(), tr("running %s update...\n", "%s update を実行中...\n"), step.Name)
	}
	result := runMiseBumpCommand(ctx, commandRunner, opts.root, false, true, safe, stdout, stderr)
	step.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(preflight.Stdout, result.Stdout), "\n"))
	step.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(preflight.Stderr, result.Stderr), "\n"))
	step.Command = miseBumpCommandForFindings(opts.root, false, true, safe)
	if result.Code != 0 || result.Err != nil {
		updated, skipped := summarizeUpdateStepLog(updateStep{Stdout: result.Stdout, Stderr: result.Stderr})
		step.Updated = appendUniqueUpdateSummaries(step.Updated, updated...)
		step.SkippedItems = appendUniqueSkippedSummaries(step.SkippedItems, skipped...)
		step.Status = plan.StatusError
		step.Reason = "mise bump failed: " + miseOutdatedResultDetail(result, "mise upgrade --bump failed")
		return step, true
	}
	if reviewCount > 0 {
		step.Status = plan.StatusHeld
		step.Reason = fmt.Sprintf("mise bump applied %d safe candidates; %d candidates require review", len(safe), reviewCount)
	} else {
		step.Status = plan.StatusOK
	}
	for _, finding := range safe {
		step.Updated = append(step.Updated, miseBumpFindingSummary(finding))
	}
	return step, true
}

func runMiseBumpCommand(ctx context.Context, commandRunner commandRunner, root string, dryRun bool, yes bool, findings []safetyFinding, stdout io.Writer, stderr io.Writer) runner.Result {
	command := miseBumpCommandForFindings(root, dryRun, yes, findings)
	if len(command) == 0 {
		return runner.Result{}
	}
	cleanup := func() {}
	if miseBumpNeedsSanitizedNPMUserConfig(findings) {
		wrapped, wrappedCleanup, err := miseBumpCommandWithSanitizedNPMUserConfig(command)
		if err != nil {
			return runner.Result{Stderr: err.Error(), Err: err, Code: 1}
		}
		command = wrapped
		cleanup = wrappedCleanup
	}
	defer cleanup()
	return runMiseCommand(ctx, commandRunner, stdout, stderr, command[0], command[1:]...)
}

func miseBumpCommandForFindings(root string, dryRun bool, yes bool, findings []safetyFinding) []string {
	command := miseBumpCommand(root, dryRun, yes, findings)
	if len(command) == 0 || !miseBumpNeedsReleaseAgeBypass(findings) {
		return command
	}
	return append([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d"}, command...)
}

func miseBumpNeedsReleaseAgeBypass(findings []safetyFinding) bool {
	for _, finding := range findings {
		if !strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(finding.Confidence), "policy") || safetyFindingHasEvidence(finding, "security-policy") {
			return true
		}
	}
	return false
}

func safetyFindingHasEvidence(finding safetyFinding, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range finding.Evidence {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func miseBumpNeedsSanitizedNPMUserConfig(findings []safetyFinding) bool {
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Kind), "npm") || strings.HasPrefix(strings.TrimSpace(finding.Name), "npm:") {
			return true
		}
	}
	return false
}

func miseBumpCommandWithSanitizedNPMUserConfig(command []string) ([]string, func(), error) {
	path, cleanup, err := sanitizedNPMUserConfigForMiseBump()
	if err != nil {
		return nil, func() {}, err
	}
	wrapped := append([]string{
		"env",
		"-u", "NPM_CONFIG_MIN_RELEASE_AGE",
		"-u", "npm_config_min_release_age",
		"-u", "NPM_CONFIG_MINIMUM_RELEASE_AGE",
		"-u", "npm_config_minimum_release_age",
		"NPM_CONFIG_USERCONFIG=" + path,
	}, command...)
	return wrapped, cleanup, nil
}

func sanitizedNPMUserConfigForMiseBump() (string, func(), error) {
	file, err := os.CreateTemp("", "updev-mise-npmrc-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary npm config for mise bump: %w", err)
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	content := sanitizedNPMUserConfigContentForMiseBump()
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary npm config for mise bump: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close temporary npm config for mise bump: %w", err)
	}
	return file.Name(), cleanup, nil
}

func sanitizedNPMUserConfigContentForMiseBump() string {
	lines := []string{}
	for _, path := range npmUserConfigCandidatePaths() {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			if npmConfigLineSetsReleaseAge(line) {
				continue
			}
			lines = append(lines, line)
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func npmUserConfigCandidatePaths() []string {
	seen := map[string]bool{}
	paths := []string{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if configured := firstNonEmpty(os.Getenv("NPM_CONFIG_USERCONFIG"), os.Getenv("npm_config_userconfig")); configured != "" {
		add(configured)
		return paths
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		add(filepath.Join(home, ".npmrc"))
	}
	xdgConfig := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdgConfig == "" {
		if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
			xdgConfig = filepath.Join(home, ".config")
		}
	}
	if xdgConfig != "" {
		add(filepath.Join(xdgConfig, "npm", "npmrc"))
	}
	return paths
}

func npmConfigLineSetsReleaseAge(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return false
	}
	key := trimmed
	if before, _, ok := strings.Cut(trimmed, "="); ok {
		key = before
	}
	key = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(key, "_", "-")))
	return key == "min-release-age" || key == "minimum-release-age"
}

func miseBumpCommand(root string, dryRun bool, yes bool, findings []safetyFinding) []string {
	tools := miseBumpToolNames(findings)
	if len(tools) == 0 {
		return nil
	}
	args := []string{"upgrade"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--bump")
	if yes {
		args = append(args, "--yes")
	}
	if strings.TrimSpace(root) != "" {
		args = append(args, "--cd", root)
	}
	args = append(args, tools...)
	return append([]string{"mise"}, args...)
}

func miseBumpToolNames(findings []safetyFinding) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, finding := range findings {
		name := strings.TrimSpace(finding.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func splitMiseBumpDependencyBlockedFindings(findings []safetyFinding, output string) ([]safetyFinding, []safetyFinding) {
	blockedNames := miseBumpDependencyBlockedNames(output)
	if len(blockedNames) == 0 {
		return nil, findings
	}
	blocked := []safetyFinding{}
	remaining := []safetyFinding{}
	for _, finding := range findings {
		if blockedNames[strings.ToLower(strings.TrimSpace(finding.Name))] {
			blocked = append(blocked, finding)
			continue
		}
		remaining = append(remaining, finding)
	}
	return blocked, remaining
}

func miseBumpDependencyBlockedNames(output string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "depends on") || !strings.Contains(line, "not in the current install set") {
			continue
		}
		_, after, ok := strings.Cut(line, "tool '")
		if !ok {
			continue
		}
		spec, _, ok := strings.Cut(after, "'")
		if !ok {
			continue
		}
		name := miseBumpToolNameFromVersionedSpec(spec)
		if name != "" {
			out[strings.ToLower(name)] = true
		}
	}
	return out
}

func miseBumpToolNameFromVersionedSpec(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return spec
	}
	return strings.TrimSpace(spec[:at])
}

func miseBumpFindingSummary(finding safetyFinding) string {
	from := firstNonEmpty(strings.Join(finding.InstalledVersions, ","), finding.Version)
	to := miseCandidateVersion(finding)
	if from != "" && to != "" {
		return fmt.Sprintf("%s %s -> %s", finding.Name, from, to)
	}
	if to != "" {
		return fmt.Sprintf("%s -> %s", finding.Name, to)
	}
	return finding.Name
}

func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
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
			skipped = appendCappedUniqueSummary(skipped, normalizeSkippedSummaryItem(line))
			continue
		}
		if updateLogLineIsUpdated(line) {
			updated = appendCappedUniqueUpdateSummary(updated, line)
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
	if strings.HasPrefix(lower, "upgraded ") && strings.Contains(lower, " outdated package") {
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

func appendCappedUniqueUpdateSummary(values []string, value string) []string {
	value = normalizeUpdateSummaryItem(value)
	if value == "" || len(values) >= 12 {
		return values
	}
	key := updateSummaryItemKey(value)
	for index, existing := range values {
		if existing == value || (key != "" && updateSummaryItemKey(existing) == key) {
			if len(value) < len(existing) {
				values[index] = value
			}
			return values
		}
	}
	return append(values, value)
}

func appendUniqueUpdateSummaries(values []string, more ...string) []string {
	for _, value := range more {
		values = appendCappedUniqueUpdateSummary(values, value)
	}
	return values
}

func appendUniqueSkippedSummaries(values []string, more ...string) []string {
	for _, value := range more {
		values = appendCappedUniqueSummary(values, value)
	}
	return values
}

func normalizeUpdateSummaryItem(value string) string {
	value = truncate(oneLine(value), 160)
	if before, after, ok := strings.Cut(value, " -> "); ok {
		after = strings.TrimSpace(after)
		if left, _, ok := strings.Cut(after, " ("); ok {
			after = strings.TrimSpace(left)
		}
		return strings.TrimSpace(before) + " -> " + after
	}
	return value
}

func normalizeSkippedSummaryItem(value string) string {
	value = truncate(oneLine(value), 220)
	if name, detail, ok := parseHomebrewSkippingWarning(value); ok {
		return name + " skipped: " + detail
	}
	return value
}

func parseHomebrewSkippingWarning(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"warning: skipping ", "skipping "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(prefix):])
		if rest == "" {
			return "", "", false
		}
		name := rest
		detail := trimmed
		if before, after, ok := strings.Cut(rest, " because "); ok {
			name = strings.TrimSpace(before)
			detail = "because " + strings.TrimSpace(after)
		}
		name = strings.Trim(name, "`\"'")
		if name == "" {
			return "", "", false
		}
		return name, detail, true
	}
	return "", "", false
}

func updateSummaryItemKey(value string) string {
	name, detail := updateOutcomeUpdatedItemParts(value)
	if name == "" || detail == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(name))
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
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel(tr("reason:", "理由:"), color), truncate(oneLine(localizedUpdateStepReason(step.Reason)), 120))
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
		return truncate(oneLine(localizedUpdateStepReason(step.Reason)), 72)
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
	lastName := "last-update.json"
	if report.DryRun {
		lastName = "last-dry-run.json"
	}
	lastPath := filepath.Join(dir, lastName)
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
	entry.Report = normalizeCachedUpdateReport(entry.Report)
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
	tui      bool
	noTUI    bool
}

func parseLastReportOptions(args []string) (lastReportOptions, error) {
	opts := lastReportOptions{format: "text", section: "summary"}
	plain := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--plain":
			plain = true
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
		case "--interactive", "--tui":
			opts.tui = true
		case "--no-interactive", "--no-tui":
			opts.noTUI = true
		case "--help", "-h":
			return opts, fmt.Errorf("usage: updev last [--section summary|updates|security|inventory|logs|full] [--provider name] [--status status|attention] [--query text] [--details] [--interactive|--no-interactive|--plain] [--format text|json]")
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if plain {
		opts.format = "text"
		opts.noTUI = true
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
	if shouldRunLastReportHub(opts, os.Stdin, os.Stdout) {
		runUpdateHubWithDefault(entry.Report, lastReportHubDefaultAction(opts.section))
	} else {
		printLastReportText(os.Stdout, entry, opts)
		warnInteractiveUnavailable(os.Stdin, os.Stdout, opts.format, opts.tui, opts.noTUI)
	}
	return updateExitCode(entry.Report.Status)
}

func shouldRunLastReportHub(opts lastReportOptions, input io.Reader, output io.Writer) bool {
	return shouldRunUpdevInteractive(input, output, opts.format, opts.tui, opts.noTUI)
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
	report.Steps = normalizeUpdateStepOutcomes(report.Steps)
	report.Steps = filterUpdateSteps(report.Steps, opts)
	report.Safety = filterSafetyGates(report.Safety, opts)
	report.Inventory = filterPlanReport(report.Inventory, opts)
	return report
}

func normalizeUpdateStepOutcomes(steps []updateStep) []updateStep {
	out := make([]updateStep, 0, len(steps))
	for _, step := range steps {
		normalizedUpdated := []string{}
		for _, item := range step.Updated {
			item = normalizeUpdateSummaryItem(item)
			if item == "" || updateLogLineIsProgress(item) {
				continue
			}
			normalizedUpdated = appendCappedUniqueUpdateSummary(normalizedUpdated, item)
		}
		normalizedSkipped := []string{}
		for _, item := range step.SkippedItems {
			item = normalizeSkippedSummaryItem(item)
			if item == "" || updateLogLineIsGenericSkipped(item) {
				continue
			}
			normalizedSkipped = appendCappedUniqueSummary(normalizedSkipped, item)
		}
		step.Updated = normalizedUpdated
		step.SkippedItems = normalizedSkipped
		out = append(out, step)
	}
	return out
}

func normalizeCachedUpdateReport(report updateReport) updateReport {
	report.Steps = normalizeUpdateStepOutcomes(report.Steps)
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
		if opts.query != "" {
			filteredStep, ok := filterUpdateStepByQuery(step, opts.query)
			if !ok {
				continue
			}
			step = filteredStep
		}
		out = append(out, step)
	}
	return out
}

func filterUpdateStepByQuery(step updateStep, query string) (updateStep, bool) {
	matchingUpdated := filterUpdateStepItemsByQuery(step.Updated, query)
	matchingSkipped := filterUpdateStepItemsByQuery(step.SkippedItems, query)
	if len(matchingUpdated) > 0 || len(matchingSkipped) > 0 {
		step.Updated = matchingUpdated
		step.SkippedItems = matchingSkipped
		return step, true
	}
	if updateStepMatchesQuery(step, query) {
		return step, true
	}
	return updateStep{}, false
}

func filterUpdateStepItemsByQuery(items []string, query string) []string {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), needle) {
			out = append(out, item)
		}
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
		strings.Join(step.Updated, " "),
		strings.Join(step.SkippedItems, " "),
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
		strings.TrimSpace(finding.Kind + "/" + finding.Name),
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
	if opts.provider == "" && opts.status == "" {
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
	parts := []string{fmt.Sprintf(tr("%d provider steps", "provider step %d件"), len(steps))}
	if updatedItems > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d updated items", "更新項目 %d件"), updatedItems))
	}
	if skippedItems > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d deferred items", "見送り項目 %d件"), skippedItems))
	}
	if held > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d held steps", "保留step %d件"), held))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d skipped steps", "skip step %d件"), skipped))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf(tr("%d error steps", "error step %d件"), errors))
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
			outcome := "updated"
			if report.DryRun && strings.HasPrefix(strings.TrimSpace(item), "would bump ") {
				outcome = "would"
				item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "would bump "))
			}
			name, detail := updateOutcomeUpdatedItemParts(item)
			rows = append(rows, []string{
				textui.StyleStatus(outcome, color),
				textui.StyleName(step.Name, color),
				textui.StyleVersion(truncate(name, 38), color),
				textui.StyleVersion(truncate(oneLine(detail), 72), color),
			})
			if len(rows) >= limit {
				return rows
			}
		}
		for _, item := range step.SkippedItems {
			name, detail := updateOutcomeSkippedItemParts(step, item)
			rows = append(rows, []string{
				textui.StyleStatus("skipped", color),
				textui.StyleName(step.Name, color),
				truncate(name, 38),
				truncate(oneLine(detail), 72),
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
				truncate(oneLine(localizedUpdateStepReason(step.Reason)), 72),
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
				truncate(firstNonEmpty(updateOutcomeFindingDetail(finding), localizedSafetyReasonWithReleaseAge(finding)), 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
	}
	return rows
}

func updateOutcomeSkippedItemParts(step updateStep, item string) (string, string) {
	item = oneLine(strings.TrimSpace(item))
	if item == "" {
		return firstNonEmpty(step.Name, "step"), ""
	}
	if name, detail, ok := strings.Cut(item, " skipped: "); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name), strings.TrimSpace(detail)
	}
	if localized := localizedUpdateStepReason(item); localized != item {
		return firstNonEmpty(step.Name, "step"), localized
	}
	if name, detail := updateOutcomeUpdatedItemParts(strings.TrimPrefix(item, "would bump ")); name != "" && strings.TrimSpace(name) != strings.TrimSpace(item) {
		return name, detail
	}
	return firstNonEmpty(step.Name, "step"), item
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
		if len(fields) == 1 {
			return strings.TrimSpace(before), strings.TrimSpace(after)
		}
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
					truncate(oneLine(localizedSafetyReasonWithReleaseAge(finding)), 72),
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

const securityDetailActionPrefix = "security-policy"
const miseBumpDetailActionPrefix = "mise-bump"

func runUpdateHub(report updateReport) {
	runUpdateHubWithDefault(report, "")
}

func buildUpdateHubReviewPlans(root string) (inventoryPlanReport, backendPlanReport) {
	progress := newStartupProgress(os.Stdin, os.Stderr, "text", reviewPlanProgressMessage(defaultLanguage()))
	progress.Start()
	var manualPlan inventoryPlanReport
	var backendPlan backendPlanReport
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		manualPlan = buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: manualProviderName})
	}()
	go func() {
		defer wg.Done()
		backendPlan = buildBackendPlanReport(context.Background(), backendOptions{command: "plan", root: root})
	}()
	wg.Wait()
	progress.Done()
	return manualPlan, backendPlan
}

func buildInventoryPlanForHub(root string) inventoryPlanReport {
	progress := newStartupProgress(os.Stdin, os.Stderr, "text", reviewPlanProgressMessage(defaultLanguage()))
	progress.Start()
	report := buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: manualProviderName})
	progress.Done()
	return report
}

func runUpdateHubWithDefault(report updateReport, preferredAction string) {
	manualPlan := inventoryPlanReport{}
	backendPlan := backendPlanReport{}
	manualLoading := true
	backendLoading := true
	for {
		defaultAction := updateHubDefaultAction(manualPlan, backendPlan, preferredAction, report)
		result, err := runUpdateHubRouter(report, manualPlan, manualLoading, backendPlan, backendLoading, preferredAction, defaultAction, textui.ColorEnabled())
		if err != nil {
			printUpdateText(report)
			return
		}
		if result.ManualReady {
			manualPlan = result.ManualPlan
			manualLoading = false
		}
		if result.BackendReady {
			backendPlan = result.BackendPlan
			backendLoading = false
		}
		nextAction, done := handleUpdateHubExternalAction(&report, &manualPlan, &backendPlan, result.Action)
		if done {
			return
		}
		preferredAction = nextAction
	}
}

func runUpdateSummaryRouteDetail(report updateReport, route updateSummaryRoute, backendPlan backendPlanReport, detailStates map[string]detailBrowserState, color bool) bool {
	opts := lastReportOptions{provider: route.Provider, query: route.Query}
	filtered := filterUpdateReport(report, opts)
	suffix := updateSummaryRouteTitleSuffix(route)
	switch route.Base {
	case updateHubActionLogs:
		stateKey := "summary:logs:" + filterSummary(lastReportFilterMap(opts))
		state, err := runDetailBrowserWithState("updev update logs"+suffix, updateLogDetailRows(filtered), detailStates[stateKey], color)
		if err != nil {
			printLastUpdateLogs(os.Stdout, filtered, color)
			return false
		}
		detailStates[stateKey] = state
		return state.Action == updevActionExit
	case updateHubActionSecurity:
		stateKey := "summary:security:" + filterSummary(lastReportFilterMap(opts))
		state, err := runDetailBrowserWithState("updev security details"+suffix, updateSecurityDetailRows(filtered), detailStates[stateKey], color)
		if err != nil {
			printLastSecuritySection(os.Stdout, filtered, true, color)
			return false
		}
		detailStates[stateKey] = state
		return state.Action == updevActionExit
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: filtered.Inventory}, listOptions{provider: route.Provider, query: route.Query})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, backendPlan)
		action, handled := runListFilteredBrowser("updev installed inventory"+suffix, inventory, detailStates, color)
		if !handled {
			printLastInventorySection(os.Stdout, filtered, lastReportOptions{section: "inventory", provider: route.Provider, query: route.Query, details: true}, color)
			return false
		}
		return action == updevActionExit
	case updateHubActionInventoryDetails:
		stateKey := "summary:inventory-details:" + filterSummary(lastReportFilterMap(opts))
		state, err := runDetailBrowserWithState("updev inventory details"+suffix, updateInventoryDetailRowsWithBackend(filtered, backendPlan), detailStates[stateKey], color)
		if err != nil {
			printLastInventorySection(os.Stdout, filtered, lastReportOptions{section: "inventory", provider: route.Provider, query: route.Query, details: true}, color)
			return false
		}
		detailStates[stateKey] = state
		return state.Action == updevActionExit
	default:
		return false
	}
}

func updateSummaryRouteTitleSuffix(route updateSummaryRoute) string {
	parts := []string{}
	if route.Provider != "" {
		parts = append(parts, "provider="+route.Provider)
	}
	if route.Query != "" {
		parts = append(parts, "query="+route.Query)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func runUpdateFullReportBrowser(report updateReport, state detailBrowserState, color bool) (detailBrowserState, error) {
	if state.Expanded == nil {
		state.Expanded = map[int]bool{0: true}
	} else if len(state.Expanded) == 0 {
		state.Expanded[0] = true
	}
	return runDetailBrowserWithState("updev full report", updateFullReportRows(report), state, color)
}

func updateFullReportRows(report updateReport) []detailBrowserRow {
	var body strings.Builder
	printUpdateBodyTo(&body, report, false)
	return []detailBrowserRow{{
		Title:   "full report",
		Status:  string(report.Status),
		Summary: tr("cached update report", "cached update report"),
		Detail:  body.String(),
	}}
}

func updateHubTitle(report updateReport) string {
	status := report.Status
	if status == "" {
		status = plan.StatusOK
	}
	return fmt.Sprintf("updev update %s", status)
}

func lastReportHubDefaultAction(section string) string {
	switch strings.ToLower(strings.TrimSpace(section)) {
	case "updates":
		return updateHubActionUpdatesFilter
	case "security":
		return updateHubActionSecurity
	case "inventory":
		return updateHubActionInventoryAll
	case "logs":
		return updateHubActionLogs
	case "full":
		return updateHubActionFull
	default:
		return ""
	}
}

func updateHubActionAvailable(action string, choices []updevChoice) bool {
	if action == "" {
		return false
	}
	for _, choice := range choices {
		if choice.Value == action {
			return true
		}
	}
	return false
}

func handleManualPlanDetailAction(root string, value string) bool {
	action, target, ok := parseManualPlanDetailAction(value)
	if !ok {
		return false
	}
	switch action {
	case "accept", "edit", "ignore":
		if !confirmManualPlanWriteAction(action, target) {
			return true
		}
		_ = applyConfirmedManualPlanDetailAction(root, action, target)
	case "enrich", "enrich-batch":
		if !confirmManualPlanWriteAction(action, target) {
			return true
		}
		_ = applyConfirmedManualPlanDetailAction(root, action, target)
	case "accept-draft", "edit-draft", "ignore-draft":
		if !confirmManualPlanWriteAction(action, target) {
			return true
		}
		if err := applyManualStructuredDraftAction(root, action, target); err != nil {
			fmt.Fprintf(os.Stderr, "manual draft action failed: %v\n", err)
		}
	case "review-cask":
		runManualPlanProviderReview("brew", "info", "--cask", target)
	case "review-mas":
		runManualPlanProviderReview("mas", "info", target)
	case "open-vendor":
		runManualPlanProviderReview("open", target)
	default:
		return false
	}
	return true
}

func manualPlanDetailActionRequiresConfirmation(action string) bool {
	switch action {
	case "accept", "edit", "ignore", "enrich", "enrich-batch", "accept-draft", "edit-draft", "ignore-draft":
		return true
	default:
		return false
	}
}

func applyConfirmedManualPlanDetailAction(root string, action string, target string) bool {
	if !manualPlanDetailActionRequiresConfirmation(action) {
		return false
	}
	opts := inventoryReviewOptions{
		action:   action,
		format:   "text",
		provider: manualProviderName,
		query:    target,
		root:     root,
	}
	if action == "enrich-batch" && target == "*" {
		opts.query = ""
	}
	report := buildInventoryReviewReport(opts)
	if _, _, _, err := applyInventoryReviewAction(opts, report); err != nil {
		fmt.Fprintf(os.Stderr, "manual review action failed: %v\n", err)
	}
	return true
}

func confirmManualPlanWriteAction(action string, target string) bool {
	selected, err := runUpdevSelect("manual inventory action", fmt.Sprintf("Apply %s to %s?", action, target), []updevChoice{
		{Value: "apply", Label: "Apply", Description: "Write the selected manual inventory override.", Selected: true},
		{Value: updevActionBack, Label: "Back", Description: "Return without writing."},
	}, "apply")
	return err == nil && selected == "apply"
}

func handleBackendDetailAction(root string, value string) bool {
	action, current, recommended, ok := parseBackendDetailAction(value)
	if !ok {
		return false
	}
	switch action {
	case "remove-brew":
		kind, name, ok := strings.Cut(current, ":")
		if !ok || kind == "" || name == "" {
			fmt.Fprintf(os.Stderr, "backend Brewfile removal failed: invalid target %q\n", current)
			return true
		}
		if !confirmBackendBrewfileRemoveAction(kind, name, recommended) {
			return true
		}
		_ = applyConfirmedBackendDetailAction(root, action, current, recommended)
	case "rewrite-mise":
		if !confirmBackendWriteAction(current, recommended) {
			return true
		}
		_ = applyConfirmedBackendDetailAction(root, action, current, recommended)
	case "remove-mise":
		if !confirmBackendRemoveAction(current, recommended) {
			return true
		}
		_ = applyConfirmedBackendDetailAction(root, action, current, recommended)
	default:
		return false
	}
	return true
}

func backendDetailActionRequiresConfirmation(action string) bool {
	switch action {
	case "remove-brew", "rewrite-mise", "remove-mise":
		return true
	default:
		return false
	}
}

func applyConfirmedBackendDetailAction(root string, action string, current string, recommended string) bool {
	switch action {
	case "remove-brew":
		kind, name, ok := strings.Cut(current, ":")
		if !ok || kind == "" || name == "" {
			fmt.Fprintf(os.Stderr, "backend Brewfile removal failed: invalid target %q\n", current)
			return true
		}
		changed, err := brewfile.RemoveEntry(root, kind, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backend Brewfile removal failed: %v\n", err)
			return true
		}
		if changed {
			fmt.Printf("%s %s %q (mise %s owns it)\n", textui.StyleLabel("removed:", textui.ColorEnabled()), kind, name, recommended)
		} else {
			fmt.Printf("%s %s %q\n", textui.StyleDim("no change:", textui.ColorEnabled()), kind, name)
		}
	case "rewrite-mise":
		changed, err := mise.RenameTool(root, current, recommended)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backend rewrite failed: %v\n", err)
			return true
		}
		if changed {
			fmt.Printf("%s %s -> %s\n", textui.StyleLabel("rewritten:", textui.ColorEnabled()), current, recommended)
		} else {
			fmt.Printf("%s %s -> %s\n", textui.StyleDim("no change:", textui.ColorEnabled()), current, recommended)
		}
	case "remove-mise":
		changed, err := mise.RemoveTool(root, current)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backend removal failed: %v\n", err)
			return true
		}
		if changed {
			fmt.Printf("%s %s (%s covers it)\n", textui.StyleLabel("removed:", textui.ColorEnabled()), current, recommended)
		} else {
			fmt.Printf("%s %s\n", textui.StyleDim("no change:", textui.ColorEnabled()), current)
		}
	default:
		return false
	}
	return true
}

func confirmBackendWriteAction(current string, recommended string) bool {
	selected, err := runUpdevSelect("backend convergence action", fmt.Sprintf(tr("Rewrite mise backend %s -> %s?", "mise backend を %s -> %s に書き換えますか?"), current, recommended), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: tr("Rename the mise tool key and preserve its current spec.", "現在の spec を維持したまま mise tool key を rename します。"), Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func confirmBackendRemoveAction(current string, recommended string) bool {
	selected, err := runUpdevSelect("backend convergence action", fmt.Sprintf(tr("Remove old mise backend %s? Preferred entry %s already covers it.", "古い mise backend %s を削除しますか? 優先 entry %s が既にカバーしています。"), current, recommended), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: tr("Remove the current mise tool key because the preferred entry already covers it.", "優先 entry がカバー済みのため、現在の mise tool key を削除します。"), Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func confirmBackendBrewfileRemoveAction(kind string, name string, recommended string) bool {
	selected, err := runUpdevSelect("backend convergence action", fmt.Sprintf(tr("Remove Brewfile %s %q? mise entry %s already exists.", "Brewfile の %s %q を削除しますか? mise entry %s は既に存在します。"), kind, name, recommended), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: tr("Remove desired-state ownership from Brewfile. This does not uninstall the Homebrew package.", "Brewfile の desired-state ownership を削除します。Homebrew package の uninstall は行いません。"), Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func handleSecurityDetailAction(report *updateReport, value string) bool {
	action, provider, kind, name, ok := parseSecurityDetailAction(value)
	if !ok || report == nil {
		return false
	}
	decision := ""
	reason := ""
	expires := ""
	switch action {
	case "allow-7d":
		decision = "allow"
		reason = "accepted from updev detail browser after local review"
		expires = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	case "allow-7d-rerun":
		decision = "allow"
		reason = "accepted from updev detail browser after local review"
		expires = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	case "allow-custom", "allow-custom-rerun":
		decision = "allow"
		var ok bool
		reason, expires, ok = promptSecurityPolicyAllowInputs(provider, kind, name)
		if !ok {
			return true
		}
	case "hold":
		decision = "hold"
		reason = "held from updev detail browser after local review"
	default:
		return false
	}
	if !confirmSecurityPolicyAction(decision, provider, kind, name, expires) {
		return true
	}
	_ = applyConfirmedSecurityDetailAction(report, action, provider, kind, name, reason, expires)
	return true
}

func handleMiseBumpDetailAction(report *updateReport, value string) bool {
	action, name, ok := parseMiseBumpDetailAction(value)
	if !ok || report == nil {
		return false
	}
	findings := []safetyFinding{}
	switch action {
	case "apply":
		finding, ok := findMiseBumpFinding(*report, name)
		if !ok {
			fmt.Fprintf(os.Stderr, "mise bump candidate not found: %s\n", name)
			return true
		}
		findings = []safetyFinding{finding}
	case "apply-batch":
		if gate, ok := miseBumpGate(report.Safety); ok {
			findings = safeMiseBumpFindings(gate)
		}
	default:
		return false
	}
	if len(findings) == 0 {
		fmt.Fprintln(os.Stderr, "no safe mise bump candidates found")
		return true
	}
	for _, finding := range findings {
		if !strings.EqualFold(finding.Decision, "allow") || miseBumpUnsafeVersionReason(finding) != "" {
			fmt.Fprintf(os.Stderr, "mise bump candidate is not safe to apply directly: %s\n", finding.Name)
			return true
		}
	}
	if !confirmMiseBumpWriteAction(report.Root, findings) {
		return true
	}
	if err := validateMiseBumpPlannedCandidates(context.Background(), runner.Local{}, report.Root, findings); err != nil {
		fmt.Fprintf(os.Stderr, "mise bump candidate set changed before apply: %s\n", err)
		return true
	}
	result := runMiseBumpCommand(context.Background(), runner.Local{}, report.Root, false, true, findings, os.Stdout, os.Stderr)
	step := updateStep{
		Name:    miseBumpProvider,
		Command: miseBumpCommand(report.Root, false, true, findings),
		Stdout:  result.Stdout,
		Stderr:  result.Stderr,
	}
	if result.Code != 0 || result.Err != nil {
		step.Status = plan.StatusError
		step.Reason = "mise bump failed: " + miseOutdatedResultDetail(result, "mise upgrade --bump failed")
	} else {
		step.Status = plan.StatusOK
		for _, finding := range findings {
			step.Updated = append(step.Updated, miseBumpFindingSummary(finding))
		}
	}
	replaceOrAppendUpdateStep(report, step)
	refreshMiseBumpGate(report)
	refreshUpdateReportStatus(report)
	report.Report = saveLastUpdateReport(*report)
	return true
}

func parseMiseBumpDetailAction(value string) (string, string, bool) {
	parts := strings.SplitN(value, "\t", 3)
	if len(parts) < 2 || parts[0] != miseBumpDetailActionPrefix {
		return "", "", false
	}
	action := strings.TrimSpace(parts[1])
	target := ""
	if len(parts) == 3 {
		target = strings.TrimSpace(parts[2])
	}
	switch action {
	case "apply":
		return action, target, target != ""
	case "apply-batch":
		return action, "", true
	default:
		return "", "", false
	}
}

func miseBumpDetailActionValue(name string) string {
	return strings.Join([]string{miseBumpDetailActionPrefix, "apply", name}, "\t")
}

func miseBumpBatchDetailActionValue() string {
	return strings.Join([]string{miseBumpDetailActionPrefix, "apply-batch"}, "\t")
}

func findMiseBumpFinding(report updateReport, name string) (safetyFinding, bool) {
	for _, gate := range report.Safety {
		if gate.Provider != miseBumpProvider {
			continue
		}
		for _, finding := range gate.Findings {
			if finding.Name == name {
				return finding, true
			}
		}
	}
	return safetyFinding{}, false
}

func confirmMiseBumpWriteAction(root string, findings []safetyFinding) bool {
	command := miseBumpCommand(root, true, false, findings)
	fmt.Printf("%s %s\n", textui.StyleLabel("preview:", textui.ColorEnabled()), joinCommand(command))
	if err := validateMiseBumpPlannedCandidates(context.Background(), runner.Local{}, root, findings); err != nil {
		fmt.Fprintf(os.Stderr, "mise bump candidate set changed before preview: %s\n", err)
		return false
	}
	preflight := runMiseBumpCommand(context.Background(), runner.Local{}, root, true, false, findings, os.Stdout, os.Stderr)
	if preflight.Code != 0 || preflight.Err != nil {
		fmt.Fprintf(os.Stderr, "mise bump dry-run failed: %s\n", miseOutdatedResultDetail(preflight, "preflight failed"))
		return false
	}
	summary := fmt.Sprintf("%d candidates", len(findings))
	if len(findings) == 1 {
		summary = miseBumpFindingSummary(findings[0])
	}
	selected, err := runUpdevSelect("mise bump action", fmt.Sprintf("Apply mise bump %s?", summary), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: tr("Run the scoped mise upgrade --bump command.", "対象を絞った mise upgrade --bump を実行します。"), Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func replaceOrAppendUpdateStep(report *updateReport, step updateStep) {
	if report == nil {
		return
	}
	for index, existing := range report.Steps {
		if existing.Name == step.Name {
			report.Steps[index] = step
			return
		}
	}
	report.Steps = append(report.Steps, step)
}

func refreshMiseBumpGate(report *updateReport) {
	if report == nil || strings.EqualFold(report.Security, "off") {
		return
	}
	policy := loadSecurityPolicy()
	if report.Policy != nil && strings.TrimSpace(report.Policy.Path) != "" {
		policy = loadSecurityPolicyForReportPath(report.Policy.Path).Policy
	}
	gate := collectMiseBumpSafetyWithPolicy(context.Background(), runner.Local{}, report.Root, policy)
	for index, existing := range report.Safety {
		if existing.Provider == miseBumpProvider {
			report.Safety[index] = gate
			return
		}
	}
	report.Safety = append(report.Safety, gate)
}

func securityDetailActionRequiresConfirmation(action string) bool {
	switch action {
	case "allow-7d", "allow-7d-rerun", "allow-custom", "allow-custom-rerun", "hold":
		return true
	default:
		return false
	}
}

func securityDetailActionRequiresCustomInput(action string) bool {
	switch action {
	case "allow-custom", "allow-custom-rerun":
		return true
	default:
		return false
	}
}

func defaultSecurityDetailActionInputs(action string) (string, string, string, bool) {
	switch action {
	case "allow-7d", "allow-7d-rerun":
		return "allow", "accepted from updev detail browser after local review", time.Now().AddDate(0, 0, 7).Format("2006-01-02"), true
	case "hold":
		return "hold", "held from updev detail browser after local review", "", true
	default:
		return "", "", "", false
	}
}

func applyConfirmedSecurityDetailAction(report *updateReport, action string, provider string, kind string, name string, reason string, expires string) bool {
	return applyConfirmedSecurityDetailActionWithOutput(report, action, provider, kind, name, reason, expires, true, true)
}

func applyConfirmedSecurityDetailActionSilently(report *updateReport, action string, provider string, kind string, name string, reason string, expires string) bool {
	return applyConfirmedSecurityDetailActionWithOutput(report, action, provider, kind, name, reason, expires, false, false)
}

func applyConfirmedSecurityDetailActionWithOutput(report *updateReport, action string, provider string, kind string, name string, reason string, expires string, printResult bool, streamRerun bool) bool {
	if report == nil {
		return false
	}
	decision := ""
	switch action {
	case "allow-7d", "allow-7d-rerun", "allow-custom", "allow-custom-rerun":
		decision = "allow"
	case "hold":
		decision = "hold"
	default:
		return false
	}
	path := securityPolicyPath()
	if report.Policy != nil && strings.TrimSpace(report.Policy.Path) != "" {
		path = report.Policy.Path
	}
	rule := securityPolicyRule{
		Provider: provider,
		Kind:     kind,
		Name:     name,
		Decision: decision,
		Reason:   reason,
		Expires:  expires,
	}
	if err := addSecurityPolicyRule(path, rule); err != nil {
		fmt.Fprintf(os.Stderr, "security policy update failed: %v\n", err)
		return true
	}
	refreshUpdateReportSecurityPolicy(report, path)
	if action == "allow-7d-rerun" || action == "allow-custom-rerun" {
		rerunUpdateProviderFindingWithOutput(report, provider, kind, name, streamRerun)
	}
	report.Report = saveLastUpdateReport(*report)
	if !printResult {
		return true
	}
	if expires != "" {
		fmt.Printf("%s %s/%s %s -> %s until %s\n", textui.StyleLabel("policy:", textui.ColorEnabled()), provider, kind, name, decision, expires)
	} else {
		fmt.Printf("%s %s/%s %s -> %s\n", textui.StyleLabel("policy:", textui.ColorEnabled()), provider, kind, name, decision)
	}
	return true
}

func promptSecurityPolicyAllowInputs(provider string, kind string, name string) (string, string, bool) {
	defaultReason := "accepted from updev detail browser after local review"
	reason, err := runUpdevInput(
		"security allow reason",
		fmt.Sprintf(tr("Reason for allowing %s/%s %s.", "%s/%s %s を許可する理由を入力します。"), provider, kind, name),
		tr("reviewed vendor provenance", "vendor provenance を確認済み"),
		defaultReason,
	)
	if err != nil || strings.TrimSpace(reason) == "" {
		return "", "", false
	}
	defaultExpiry := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	expires, err := runUpdevInput(
		"security allow expiry",
		fmt.Sprintf(tr("Expiry date for %s/%s %s. YYYY-MM-DD is required for allow rules.", "%s/%s %s の期限を入力します。allow rule には YYYY-MM-DD が必要です。"), provider, kind, name),
		defaultExpiry,
		defaultExpiry,
	)
	if err != nil || strings.TrimSpace(expires) == "" {
		return "", "", false
	}
	expires, err = validateSecurityPolicyAllowExpiry(expires, time.Now())
	if err != nil {
		if expires == "" {
			fmt.Fprintln(os.Stderr, "security policy expiry must not be in the past")
		} else {
			fmt.Fprintf(os.Stderr, "security policy expiry must be YYYY-MM-DD: %s\n", expires)
		}
		return "", "", false
	}
	return strings.TrimSpace(reason), strings.TrimSpace(expires), true
}

func validateSecurityPolicyAllowExpiry(expires string, now time.Time) (string, error) {
	trimmed := strings.TrimSpace(expires)
	expiryDate, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return trimmed, err
	}
	today, _ := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if expiryDate.Before(today) {
		return "", fmt.Errorf("expiry is in the past")
	}
	return trimmed, nil
}

func rerunUpdateProviderFindingWithOutput(report *updateReport, provider string, kind string, name string, stream bool) {
	if report == nil {
		return
	}
	step, ok := scopedSecurityRerunStep(*report, provider, kind, name)
	if !ok {
		if stream {
			fmt.Fprintf(os.Stderr, "no scoped update step is available for %s/%s %s\n", provider, kind, name)
		}
		return
	}
	if stream {
		fmt.Printf("%s %s/%s %s\n", textui.StyleLabel("rerunning:", textui.ColorEnabled()), provider, kind, name)
	}
	result := runUpdateStepWithOutput(context.Background(), runner.Local{}, step, false, "", stream)
	replaced := false
	for index, existing := range report.Steps {
		if existing.Name == provider {
			report.Steps[index] = result
			replaced = true
			break
		}
	}
	if !replaced {
		report.Steps = append(report.Steps, result)
	}
	refreshUpdateReportStatus(report)
}

func scopedSecurityRerunStep(report updateReport, provider string, kind string, name string) (updateStep, bool) {
	finding, ok := findSafetyFinding(report, provider, kind, name)
	if !ok || !strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
		return updateStep{}, false
	}
	step, ok := updateStepForProvider(provider)
	if !ok {
		return updateStep{}, false
	}
	scoped, holdReason := updateStepWithStrictSafety(step, updateOptions{root: report.Root, security: "strict"}, []safetyGate{{
		Provider: provider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}})
	if holdReason != "" || len(scoped.Command) == 0 {
		return updateStep{}, false
	}
	scoped.Reason = fmt.Sprintf("security policy reran scoped %s update for %s/%s %s", provider, provider, kind, name)
	return scoped, true
}

func findSafetyFinding(report updateReport, provider string, kind string, name string) (safetyFinding, bool) {
	provider = strings.TrimSpace(provider)
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	for _, gate := range report.Safety {
		for _, finding := range gate.Findings {
			findingProvider := firstNonEmpty(finding.Provider, gate.Provider)
			if strings.EqualFold(strings.TrimSpace(findingProvider), provider) &&
				strings.EqualFold(strings.TrimSpace(finding.Kind), kind) &&
				strings.TrimSpace(finding.Name) == name {
				return finding, true
			}
		}
	}
	return safetyFinding{}, false
}

func updateStepForProvider(provider string) (updateStep, bool) {
	for _, step := range updateSteps() {
		if step.Name == provider {
			return step, true
		}
	}
	return updateStep{}, false
}

func refreshUpdateReportStatus(report *updateReport) {
	if report == nil {
		return
	}
	status := plan.StatusOK
	for _, step := range report.Steps {
		switch step.Status {
		case plan.StatusError:
			report.Status = plan.StatusError
			return
		case plan.StatusHeld:
			status = plan.StatusHeld
		}
	}
	if report.Inventory.Status == plan.StatusError {
		report.Status = plan.StatusError
		return
	}
	if status == plan.StatusOK {
		for _, gate := range report.Safety {
			if gate.Status == plan.StatusError {
				report.Status = plan.StatusError
				return
			}
			if gate.Status == plan.StatusHeld && report.Security == "strict" {
				status = plan.StatusHeld
			}
		}
	}
	report.Status = status
}

func confirmSecurityPolicyAction(decision string, provider string, kind string, name string, expires string) bool {
	description := tr("Write a local security policy rule.", "local security policy rule を書き込みます。")
	if expires != "" {
		description = fmt.Sprintf(tr("Write a local security policy rule that expires on %s.", "%s に期限切れになる local security policy rule を書き込みます。"), expires)
	}
	selected, err := runUpdevSelect("security policy action", fmt.Sprintf(tr("Mark %s/%s %s as %s?", "%s/%s %s を %s にしますか?"), provider, kind, name, decision), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: description, Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func refreshUpdateReportSecurityPolicy(report *updateReport, path string) {
	if report == nil {
		return
	}
	policyResult := loadSecurityPolicyForReportPath(path)
	report.Policy = policyResult.View()
	for index, gate := range report.Safety {
		for findingIndex := range gate.Findings {
			if strings.TrimSpace(gate.Findings[findingIndex].Provider) == "" {
				gate.Findings[findingIndex].Provider = gate.Provider
			}
		}
		gate.Findings = applySecurityPolicyToSafetyFindings(policyResult.Policy, gate.Findings)
		gate.Summary = safetySummaryFromFindings(gate.Findings)
		gate.Status = safetyGateStatusFromFindings(gate)
		report.Safety[index] = gate
	}
}

func safetyGateStatusFromFindings(gate safetyGate) plan.Status {
	if strings.TrimSpace(gate.Error) != "" {
		return plan.StatusError
	}
	for _, finding := range gate.Findings {
		if !strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
			return plan.StatusHeld
		}
	}
	return plan.StatusOK
}

func securityDetailActions(gate safetyGate, finding safetyFinding) []detailBrowserAction {
	if gate.Provider == miseBumpProvider && strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") && miseBumpUnsafeVersionReason(finding) == "" {
		mode := defaultMiseBumpMode()
		if mode == "manual" || mode == "safe" {
			return []detailBrowserAction{{
				Value:       miseBumpDetailActionValue(finding.Name),
				Label:       tr("apply mise bump", "mise bump を適用"),
				Description: tr("preview and run a scoped mise upgrade --bump for this item", "この item だけを対象に mise upgrade --bump を preview 後に実行します"),
			}}
		}
	}
	if strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
		return nil
	}
	provider := firstNonEmpty(finding.Provider, gate.Provider, "unknown")
	kind := firstNonEmpty(finding.Kind, "item")
	if strings.TrimSpace(finding.Name) == "" {
		return nil
	}
	actions := []detailBrowserAction{}
	if _, ok := updateStepForProvider(provider); ok && gate.Provider != miseBumpProvider {
		actions = append(actions, detailBrowserAction{
			Value:       securityDetailActionValue("allow-7d-rerun", provider, kind, finding.Name),
			Label:       tr("allow 7 days and rerun", "7日間許可して再実行"),
			Description: tr("add a temporary allow rule, then rerun only this item", "一時 allow rule を追加し、この item だけを再実行します"),
		})
		actions = append(actions, detailBrowserAction{
			Value:       securityDetailActionValue("allow-custom-rerun", provider, kind, finding.Name),
			Label:       tr("custom allow and rerun", "理由/期限を指定して再実行"),
			Description: tr("enter a reason and expiry, add an allow rule, then rerun only this item", "理由と期限を入力して allow rule を追加し、この item だけを再実行します"),
		})
	}
	actions = append(actions,
		detailBrowserAction{
			Value:       securityDetailActionValue("allow-custom", provider, kind, finding.Name),
			Label:       tr("custom allow", "理由/期限を指定して許可"),
			Description: tr("enter a reason and expiry before adding the local allow rule", "local allow rule 追加前に理由と期限を入力します"),
		},
		detailBrowserAction{
			Value:       securityDetailActionValue("allow-7d", provider, kind, finding.Name),
			Label:       tr("allow for 7 days", "7日間だけ許可"),
			Description: tr("add a temporary local security policy allow rule after review", "確認後に一時的な local security policy allow rule を追加します"),
		},
		detailBrowserAction{
			Value:       securityDetailActionValue("hold", provider, kind, finding.Name),
			Label:       tr("keep held", "hold として記録"),
			Description: tr("add a local security policy hold rule with a review reason", "確認理由付きの local security policy hold rule を追加します"),
		},
	)
	return actions
}

func securityDetailActionValue(action string, provider string, kind string, name string) string {
	return strings.Join([]string{securityDetailActionPrefix, action, provider, kind, name}, "\t")
}

func parseSecurityDetailAction(value string) (string, string, string, string, bool) {
	parts := strings.SplitN(value, "\t", 5)
	if len(parts) != 5 || parts[0] != securityDetailActionPrefix || parts[1] == "" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return "", "", "", "", false
	}
	return parts[1], parts[2], parts[3], parts[4], true
}

func runManualPlanProviderReview(name string, args ...string) {
	command := append([]string{name}, args...)
	fmt.Printf("%s %s\n", textui.StyleLabel("running:", textui.ColorEnabled()), strings.Join(command, " "))
	if _, err := (runner.Local{}).LookPath(name); err != nil {
		fmt.Fprintf(os.Stderr, "%s is not available on PATH\n", name)
		return
	}
	result := (runner.Local{}).RunStreaming(context.Background(), os.Stdout, os.Stderr, name, args...)
	if result.Err != nil {
		fmt.Fprintf(os.Stderr, "%s exited with code %d\n", name, result.Code)
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
		actions := backendPlanActionableCount(backendPlan)
		choices = append(choices, updevChoice{Value: updateHubActionBackends, Label: tr("Backend convergence", "backend 整理"), Description: fmt.Sprintf(tr("Review %d provider/backend recommendations; %d can be applied from details.", "%d 件の provider/backend 推奨を確認します。%d 件は詳細から適用できます。"), findings, actions)})
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

func updateHubActionExists(action string) bool {
	switch action {
	case updateHubActionDashboard,
		updateHubActionInventoryAll,
		updateHubActionInventoryAttention,
		updateHubActionInventoryDetails,
		updateHubActionManualPlan,
		updateHubActionBackends,
		updateHubActionUpdatesFilter,
		updateHubActionSecurity,
		updateHubActionSecurityFilter,
		updateHubActionLogs,
		updateHubActionFull,
		updateHubActionJSON:
		return true
	default:
		return false
	}
}

func updateHubActionFromListAction(action string) string {
	switch action {
	case listHubActionManual:
		return updateHubActionManualPlan
	case listHubActionBackends:
		return updateHubActionBackends
	case listHubActionUpdates:
		return updateHubActionLogs
	case listHubActionSecurity:
		return updateHubActionSecurity
	default:
		return ""
	}
}

func initialUpdateHubAction(preferredAction string, defaultAction string) string {
	if strings.TrimSpace(preferredAction) != "" {
		return defaultAction
	}
	return updateHubActionDashboard
}

func updateDashboardRowIndexForAction(action string) int {
	switch action {
	case updateHubActionSecurity, updateHubActionSecurityFilter:
		return 1
	case updateHubActionInventoryAll, updateHubActionInventoryAttention, updateHubActionInventoryDetails:
		return 2
	case updateHubActionManualPlan:
		return 3
	case updateHubActionBackends:
		return 4
	case updateHubActionFull, updateHubActionJSON:
		return 5
	default:
		return 0
	}
}

func updateDashboardDetailRows(report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport) []detailBrowserRow {
	return []detailBrowserRow{
		{
			Title:   "Update steps",
			Status:  string(report.Status),
			Summary: firstNonEmpty(updateStepSummaryText(report.Steps), "no update steps"),
			Detail:  "Review provider update outcomes, held updates, deferred items, and captured logs.",
			Metadata: []string{
				fmt.Sprintf("steps: %d", len(report.Steps)),
				fmt.Sprintf("updated: %d", updateReportUpdatedItemCount(report)),
				fmt.Sprintf("deferred: %d", updateReportDeferredItemCount(report)),
			},
			Actions: []detailBrowserAction{
				{Value: updateHubActionUpdatesFilter, Label: "filter updates", Description: "choose a provider/status filter for update steps"},
				{Value: updateHubActionLogs, Label: "open logs", Description: "inspect captured stdout and stderr"},
			},
		},
		{
			Title:   "Security",
			Status:  string(updateDashboardSecurityStatus(report)),
			Summary: updateDashboardSecuritySummary(report),
			Detail:  "Review package provenance, release-age, advisory, and scanner decisions.",
			Metadata: []string{
				fmt.Sprintf("gates: %d", len(report.Safety)),
				fmt.Sprintf("attention: %d", updateDashboardSecurityAttention(report)),
				"mode: " + firstNonEmpty(report.Security, "default"),
			},
			Actions: []detailBrowserAction{
				{Value: updateHubActionSecurity, Label: "open security", Description: "inspect security attention rows"},
				{Value: updateHubActionSecurityFilter, Label: "filter security", Description: "choose security status filters"},
			},
		},
		{
			Title:   "Inventory",
			Status:  string(report.Inventory.Status),
			Summary: updateDashboardInventorySummary(report.Inventory),
			Detail:  "Review installed, desired, missing, extra, held, and profile-mismatch inventory rows.",
			Metadata: []string{
				fmt.Sprintf("items: %d", len(report.Inventory.Items)),
				fmt.Sprintf("attention: %d", updateDashboardInventoryAttention(report.Inventory)),
			},
			Actions: []detailBrowserAction{
				{Value: updateHubActionInventoryAll, Label: "open inventory", Description: "browse grouped installed inventory"},
				{Value: updateHubActionInventoryAttention, Label: "attention", Description: "print inventory attention rows"},
				{Value: updateHubActionInventoryDetails, Label: "details", Description: "open inventory attention details"},
			},
		},
		{
			Title:   "Manual review",
			Status:  manualPlanStatus(manualPlan),
			Summary: updateDashboardManualPlanSummary(manualPlan),
			Detail:  "Review manual/vendor app ownership decisions and write explicit overrides only after confirmation.",
			Metadata: []string{
				fmt.Sprintf("items: %d", len(manualPlan.Items)),
				fmt.Sprintf("attention: %d", manualPlan.AttentionCount),
				fmt.Sprintf("review candidates: %d", len(manualPlan.ReviewCandidates)),
			},
			Actions: []detailBrowserAction{
				{Value: updateHubActionManualPlan, Label: "open manual plan", Description: "browse manual app actions"},
			},
		},
		{
			Title:   "Backend convergence",
			Status:  string(backendPlan.Status),
			Summary: updateDashboardBackendSummary(backendPlan),
			Detail:  tr("Review provider preference findings and apply safe mise backend rewrites or covered-entry removals where available.", "provider/backend の優先順を確認し、安全な mise backend rewrite またはカバー済み entry の削除だけを詳細から適用します。"),
			Metadata: []string{
				fmt.Sprintf("findings: %d", len(backendPlan.Findings)),
				fmt.Sprintf("safe actions: %d", backendPlanActionableCount(backendPlan)),
				"preference: " + backendPreferenceOrderText(),
			},
			Actions: []detailBrowserAction{
				{Value: updateHubActionBackends, Label: tr("open backends", "backend 整理を開く"), Description: tr("browse backend findings and safe actions", "backend 推奨と安全な action を確認します")},
			},
		},
		{
			Title:   "Full report",
			Status:  string(report.Status),
			Summary: "Open the full cached report text or JSON.",
			Detail:  "Use this when the focused dashboard rows do not expose enough context.",
			Actions: []detailBrowserAction{
				{Value: updateHubActionFull, Label: "print full", Description: "print the full cached report"},
				{Value: updateHubActionJSON, Label: "json", Description: "print the cached report as JSON"},
			},
		},
	}
}

func manualPlanStatus(report inventoryPlanReport) string {
	if report.AttentionCount > 0 {
		return string(plan.StatusDrift)
	}
	return string(report.Status)
}

func updateDashboardManualPlanSummary(report inventoryPlanReport) string {
	if len(report.ActionCounts) == 0 {
		return "no manual review actions"
	}
	parts := make([]string, 0, len(report.ActionCounts))
	for _, key := range sortedMapKeys(report.ActionCounts) {
		parts = append(parts, fmt.Sprintf("%s=%d", key, report.ActionCounts[key]))
	}
	return strings.Join(parts, ", ")
}

func updateDashboardBackendSummary(report backendPlanReport) string {
	if len(report.Findings) == 0 {
		return "no backend convergence findings"
	}
	counts := map[string]int{}
	for _, finding := range report.Findings {
		counts[finding.Type]++
	}
	parts := make([]string, 0, len(counts))
	for _, key := range sortedMapKeys(counts) {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func updateDashboardInventorySummary(report plan.Report) string {
	if len(report.Providers) == 0 {
		return "no inventory providers"
	}
	parts := []string{fmt.Sprintf("providers=%d", len(report.Providers)), fmt.Sprintf("items=%d", len(report.Items))}
	if attention := updateDashboardInventoryAttention(report); attention > 0 {
		parts = append(parts, fmt.Sprintf("attention=%d", attention))
	}
	return strings.Join(parts, ", ")
}

func updateDashboardInventoryAttention(report plan.Report) int {
	count := 0
	for _, item := range report.Items {
		if isAttentionStatus(item.Status) || itemHasProfileMismatch(item) {
			count++
		}
	}
	return count
}

func updateDashboardSecuritySummary(report updateReport) string {
	if len(report.Safety) == 0 {
		return "no security gates"
	}
	parts := []string{fmt.Sprintf("gates=%d", len(report.Safety))}
	if attention := updateDashboardSecurityAttention(report); attention > 0 {
		parts = append(parts, fmt.Sprintf("attention=%d", attention))
	}
	return strings.Join(parts, ", ")
}

func updateDashboardSecurityStatus(report updateReport) plan.Status {
	if len(report.Safety) == 0 {
		return plan.StatusOK
	}
	status := plan.StatusOK
	for _, gate := range report.Safety {
		if gate.Status == plan.StatusError {
			return plan.StatusError
		}
		if gate.Status == plan.StatusHeld || updateDashboardSecurityGateAttention(gate) > 0 {
			status = plan.StatusHeld
		}
	}
	return status
}

func updateDashboardSecurityAttention(report updateReport) int {
	count := 0
	for _, gate := range report.Safety {
		count += updateDashboardSecurityGateAttention(gate)
	}
	return count
}

func updateDashboardSecurityGateAttention(gate safetyGate) int {
	count := 0
	if gate.Error != "" {
		count++
	}
	count += len(gate.Warnings)
	for _, finding := range gate.Findings {
		if !strings.EqualFold(finding.Decision, "allow") {
			count++
		}
	}
	return count
}

func updateReportUpdatedItemCount(report updateReport) int {
	count := 0
	for _, step := range report.Steps {
		count += len(step.Updated)
	}
	return count
}

func updateReportDeferredItemCount(report updateReport) int {
	count := 0
	for _, step := range report.Steps {
		count += updateStepDeferredCount(step)
	}
	return count
}

func backendPreferenceOrderText() string {
	parts := make([]string, 0, len(backendPreferenceTiers()))
	for _, tier := range backendPreferenceTiers() {
		parts = append(parts, tier.Label)
	}
	return strings.Join(parts, " > ")
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

func updateInventoryDetailRowsWithBackend(report updateReport, backendPlan backendPlanReport) []detailBrowserRow {
	inventory := buildListReport(inventoryResult{Report: report.Inventory}, listOptions{
		status: "attention",
		limit:  20,
	})
	inventory.Evidence = addBackendListEvidence(inventory.Evidence, backendPlan)
	return listDetailRows(inventory)
}

func updateSecurityDetailRows(report updateReport) []detailBrowserRow {
	return updateSecurityDetailRowsWithAllow(report, false)
}

func updateSecurityDetailRowsForFilter(report updateReport, opts lastReportOptions) []detailBrowserRow {
	return updateSecurityDetailRowsWithAllow(report, strings.EqualFold(opts.status, "allow") || strings.TrimSpace(opts.query) != "")
}

func updateSecurityDetailRowsWithAllow(report updateReport, includeAllow bool) []detailBrowserRow {
	rows := []detailBrowserRow{}
	for _, gate := range report.Safety {
		before := len(rows)
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
		if len(rows) == before {
			rows = append(rows, safetyGateDetailRow(report, gate))
		}
	}
	return rows
}

func safetyGateDetailRow(report updateReport, gate safetyGate) detailBrowserRow {
	status := updateSafetyDisplayStatus(report, gate.Status)
	summary := updateSafetyGateSummaryCompact(report, gate)
	reason := localizedSafetyReason(safetyGatePrimaryReason(gate))
	metadata := []string{
		"provider: " + gate.Provider,
		"status: " + status,
		"summary: " + summary,
	}
	if strings.TrimSpace(reason) != "" {
		metadata = append(metadata, "reason: "+reason)
	}
	if len(gate.Warnings) > 0 {
		metadata = append(metadata, "warnings: "+strings.Join(gate.Warnings, "; "))
	}
	return detailBrowserRow{
		Title:    gate.Provider + " security",
		Status:   status,
		Summary:  summary,
		Detail:   firstNonEmpty(reason, summary),
		Metadata: metadata,
	}
}

func safetyFindingDetailRow(gate safetyGate, finding safetyFinding) detailBrowserRow {
	reason := localizedSafetyReasonWithReleaseAge(finding)
	remediation := localizedSafetyRemediation(finding.Remediation)
	metadata := []string{
		"provider: " + firstNonEmpty(finding.Provider, gate.Provider),
		"kind: " + finding.Kind,
		"gate status: " + string(gate.Status),
		"decision: " + firstNonEmpty(finding.Decision, string(gate.Status)),
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
		Actions:  securityDetailActions(gate, finding),
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
		rows = append(rows, updateStepItemDetailRows(step)...)
		if step.Reason == "" && step.Stdout == "" && step.Stderr == "" {
			continue
		}
		status := string(step.Status)
		if step.Skipped && status == string(plan.StatusOK) {
			status = "skipped"
		}
		metadata := []string{
			"provider: " + step.Name,
			"status: " + status,
			"command: " + joinCommand(step.Command),
		}
		if step.Skipped {
			metadata = append(metadata, "skipped: true")
		}
		if step.Reason != "" {
			metadata = append(metadata, "reason: "+localizedUpdateStepReason(step.Reason))
		}
		if len(step.Updated) > 0 {
			metadata = append(metadata, "updated: "+strings.Join(step.Updated, "; "))
			for _, item := range step.Updated {
				metadata = append(metadata, tr("updated item: ", "更新 item: ")+item)
			}
		}
		if len(step.SkippedItems) > 0 {
			metadata = append(metadata, "deferred: "+strings.Join(step.SkippedItems, "; "))
			for _, item := range step.SkippedItems {
				metadata = append(metadata, tr("deferred item: ", "保留 item: ")+item)
			}
		}
		if step.Stdout != "" {
			metadata = append(metadata, "stdout: "+step.Stdout)
		}
		if step.Stderr != "" {
			metadata = append(metadata, "stderr: "+step.Stderr)
		}
		rows = append(rows, detailBrowserRow{
			Title:    step.Name,
			Status:   status,
			Summary:  updateStepDetailSummary(step),
			Detail:   firstNonEmpty(localizedUpdateStepReason(step.Reason), step.Stdout, step.Stderr),
			Metadata: metadata,
			Actions:  updateStepDetailActions(step),
		})
	}
	return rows
}

func updateStepItemDetailRows(step updateStep) []detailBrowserRow {
	rows := []detailBrowserRow{}
	for _, item := range step.Updated {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		rows = append(rows, detailBrowserRow{
			Title:   step.Name + " updated",
			Status:  "updated",
			Summary: item,
			Detail:  tr("This item was reported as updated by the provider log.", "provider log で更新済みとして報告された item です。"),
			Metadata: []string{
				"provider: " + step.Name,
				"result: updated",
				"item: " + item,
				"command: " + joinCommand(step.Command),
			},
		})
	}
	for _, item := range step.SkippedItems {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		status := "deferred"
		if step.Status == plan.StatusHeld {
			status = "held"
		}
		rows = append(rows, detailBrowserRow{
			Title:   step.Name + " " + status,
			Status:  status,
			Summary: item,
			Detail:  firstNonEmpty(step.Reason, tr("This item was deferred or held by the provider log.", "provider log により保留または見送りになった item です。")),
			Metadata: []string{
				"provider: " + step.Name,
				"result: " + status,
				"item: " + item,
				"command: " + joinCommand(step.Command),
			},
			Actions: updateStepDetailActions(step),
		})
	}
	return rows
}

func updateStepDetailActions(step updateStep) []detailBrowserAction {
	actions := []detailBrowserAction{}
	if step.Name == miseBumpProvider {
		actions = append(actions, detailBrowserAction{
			Value:       updateHubActionSecurityFilter,
			Label:       tr("open bump candidates", "bump 候補を開く"),
			Description: tr("filter security evidence to review mise bump candidates", "security evidence の filter から mise bump 候補を確認します"),
		})
		if defaultMiseBumpMode() == "safe" {
			actions = append(actions, detailBrowserAction{
				Value:       miseBumpBatchDetailActionValue(),
				Label:       tr("apply safe bumps", "safe bump を適用"),
				Description: tr("preview and run scoped mise upgrade --bump for all safe candidates", "safe 候補だけを対象に mise upgrade --bump を preview 後に実行します"),
			})
		}
	}
	if step.Status == plan.StatusHeld || strings.Contains(strings.ToLower(step.Reason), "security") {
		actions = append(actions, detailBrowserAction{
			Value:       updateHubActionSecurity,
			Label:       tr("open security review", "security review を開く"),
			Description: tr("inspect held security evidence and policy actions", "保留された security evidence と policy action を確認します"),
		})
	}
	return actions
}

func updateStepDetailSummary(step updateStep) string {
	if step.Reason != "" {
		return localizedUpdateStepReason(step.Reason)
	}
	if len(step.Updated) > 0 && len(step.SkippedItems) > 0 {
		return fmt.Sprintf("%d updated, %d deferred", len(step.Updated), len(step.SkippedItems))
	}
	if len(step.Updated) > 0 {
		return fmt.Sprintf("%d updated: %s", len(step.Updated), step.Updated[0])
	}
	if len(step.SkippedItems) > 0 {
		return fmt.Sprintf("%d deferred: %s", len(step.SkippedItems), step.SkippedItems[0])
	}
	if step.Status == plan.StatusError {
		return firstNonEmpty(oneLine(step.Stderr), oneLine(step.Stdout))
	}
	return firstNonEmpty(oneLine(step.Stdout), oneLine(step.Stderr))
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
			printDetailLine(w, "reason", localizedSafetyReasonWithReleaseAge(finding), color)
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
		// Plain/cached last-report output must stay a deterministic cache read.
		// Do not run manual/backend/security scans here; TTY hubs refresh those
		// domains asynchronously after the first screen is visible.
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
		printDetailLine(w, "reason", localizedUpdateStepReason(step.Reason), color)
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

func localizedUpdateStepReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || defaultLanguage() != "ja" {
		return reason
	}
	if reason == "security=strict held update because safety gate requires review" {
		return "security=strict のため更新を保留しました: safety gate の確認が必要です"
	}
	if suffix, ok := strings.CutPrefix(reason, "security=strict held update because safety gate failed: "); ok {
		return "security=strict のため更新を保留しました: safety gate が失敗しました: " + suffix
	}
	if reason == "strict safety refreshed Homebrew metadata before rechecking package candidates" {
		return "strict safety のため Homebrew metadata を更新し、package 候補を再確認しました"
	}
	if reason == "strict safety refreshes Homebrew metadata only before rechecking package candidates" {
		return "strict safety のため Homebrew metadata の更新だけを実行し、package 候補を再確認します"
	}
	if reason == "strict safety refreshed Homebrew metadata; no package candidates found" {
		return "strict safety のため Homebrew metadata を更新しました。更新対象の package 候補はありません"
	}
	const brewHeldPrefix = "strict safety refreshed Homebrew metadata and held "
	const brewHeldSuffix = " Homebrew candidates requiring review"
	if value, ok := strings.CutPrefix(reason, brewHeldPrefix); ok {
		if count, ok := strings.CutSuffix(value, brewHeldSuffix); ok {
			return fmt.Sprintf("Homebrew metadata を更新し、確認が必要な Homebrew 候補 %s件を保留しました", strings.TrimSpace(count))
		}
	}
	if reason == "security=strict held mise update because no scoped safe candidates were found" {
		return "security=strict のため mise 更新を保留しました: 適用できる scoped safe 候補がありません"
	}
	if reason == "security=strict held brew update because no scoped safe candidates were found" {
		return "security=strict のため brew 更新を保留しました: 適用できる scoped safe 候補がありません"
	}
	if safe, unsafe, ok := parseTwoCountReason(reason, "strict safety will apply ", " safe mise candidates and hold ", " unsafe candidates"); ok {
		return fmt.Sprintf("strict safety は mise の safe 候補 %d件だけを適用し、unsafe 候補 %d件を保留します", safe, unsafe)
	}
	if safe, unsafe, ok := parseTwoCountReason(reason, "strict safety will apply ", " safe Homebrew candidates and hold ", " unsafe candidates; Homebrew cannot generally install an older intermediate release"); ok {
		return fmt.Sprintf("strict safety は Homebrew の safe 候補 %d件だけを適用し、unsafe 候補 %d件を保留します。Homebrew は通常、古い中間 version を指定して install できません", safe, unsafe)
	}
	if reason == "mise bump candidates available; mode=manual requires item review" {
		return "mise bump 候補があります。mode=manual のため item ごとの確認が必要です"
	}
	if reason == "mise bump candidates require review" {
		return "mise bump 候補の確認が必要です"
	}
	if reason == "mise bump candidates require review; no safe auto candidates" {
		return "mise bump 候補の確認が必要です。自動適用できる safe 候補はありません"
	}
	if count, ok := parseOneCountReason(reason, "mise bump candidates available; ", " safe candidates can be applied after confirmation"); ok {
		return fmt.Sprintf("mise bump 候補があります。確認後に safe 候補 %d件を適用できます", count)
	}
	if count, ok := parseOneCountReason(reason, "mise bump auto would apply ", " safe candidates"); ok {
		return fmt.Sprintf("mise bump auto は safe 候補 %d件を適用します", count)
	}
	if safe, review, ok := parseTwoCountReason(reason, "mise bump auto would apply ", " safe candidates; ", " candidates require review"); ok {
		return fmt.Sprintf("mise bump auto は safe 候補 %d件を適用し、%d件は確認待ちにします", safe, review)
	}
	if suffix, ok := strings.CutPrefix(reason, "mise bump candidate set changed before apply: "); ok {
		return "mise bump の候補が適用直前に変わったため保留しました: " + localizedMiseBumpCandidateChange(suffix)
	}
	if suffix, ok := strings.CutPrefix(reason, "mise bump candidate set changed before preview: "); ok {
		return "mise bump の候補が preview 直前に変わりました: " + localizedMiseBumpCandidateChange(suffix)
	}
	if reason == "mise bump auto found only dependency-blocked candidates" {
		return "mise bump auto で見つかった候補は dependency 不足で block されたものだけです"
	}
	if suffix, ok := strings.CutPrefix(reason, "mise bump dry-run preflight failed: "); ok {
		return "mise bump の dry-run preflight が失敗しました: " + suffix
	}
	if suffix, ok := strings.CutPrefix(reason, "mise bump failed: "); ok {
		return "mise bump が失敗しました: " + suffix
	}
	if safe, review, ok := parseTwoCountReason(reason, "mise bump applied ", " safe candidates; ", " candidates require review"); ok {
		return fmt.Sprintf("mise bump は safe 候補 %d件を適用し、%d件は確認待ちです", safe, review)
	}
	if provider, target, ok := parseScopedSecurityPolicyReason(reason); ok {
		return fmt.Sprintf("security policy に従い、%s の scoped update を再実行しました: %s", provider, target)
	}
	return reason
}

func parseOneCountReason(reason string, prefix string, suffix string) (int, bool) {
	value, ok := strings.CutPrefix(reason, prefix)
	if !ok {
		return 0, false
	}
	value, ok = strings.CutSuffix(value, suffix)
	if !ok {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(value))
	return count, err == nil
}

func parseTwoCountReason(reason string, prefix string, middle string, suffix string) (int, int, bool) {
	value, ok := strings.CutPrefix(reason, prefix)
	if !ok {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, middle)
	if !ok {
		return 0, 0, false
	}
	right, ok = strings.CutSuffix(right, suffix)
	if !ok {
		return 0, 0, false
	}
	first, errFirst := strconv.Atoi(strings.TrimSpace(left))
	second, errSecond := strconv.Atoi(strings.TrimSpace(right))
	return first, second, errFirst == nil && errSecond == nil
}

func parseScopedSecurityPolicyReason(reason string) (string, string, bool) {
	value, ok := strings.CutPrefix(reason, "security policy reran scoped ")
	if !ok {
		return "", "", false
	}
	provider, target, ok := strings.Cut(value, " update for ")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(provider), strings.TrimSpace(target), true
}

func localizedMiseBumpCandidateChange(reason string) string {
	reason = strings.TrimSpace(reason)
	const noLongerPrefix = "planned candidate "
	const noLongerSuffix = " is no longer reported by mise outdated --bump"
	if value, ok := strings.CutPrefix(reason, noLongerPrefix); ok {
		if name, ok := strings.CutSuffix(value, noLongerSuffix); ok {
			return fmt.Sprintf("予定していた候補 %s は現在の mise outdated --bump に出ていません", strings.TrimSpace(name))
		}
	}
	if value, ok := strings.CutPrefix(reason, noLongerPrefix); ok {
		parts := strings.Split(value, " changed from ")
		if len(parts) == 2 {
			versions := strings.Split(parts[1], " to ")
			if len(versions) == 2 {
				return fmt.Sprintf("予定していた候補 %s は %s から %s に変わりました", strings.TrimSpace(parts[0]), strings.TrimSpace(versions[0]), strings.TrimSpace(versions[1]))
			}
		}
	}
	return reason
}

func localizedSafetyReasonWithReleaseAge(finding safetyFinding) string {
	reason := strings.TrimSpace(localizedSafetyReason(finding.Reason))
	if wait := releaseAgeHoldAvailabilityText(finding); wait != "" {
		if miseNativeReleaseAgeHoldReason(finding) {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(wait, reason), "。"))
		}
		if strings.Contains(reason, "経過") && strings.Contains(reason, "最小") {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(reason, releaseAgeHoldDateAvailabilityText(finding)), "。"))
		}
		return strings.TrimSpace(strings.Join(nonEmptyStrings(reason, wait), "。"))
	}
	return reason
}

func miseNativeReleaseAgeHoldReason(finding safetyFinding) bool {
	if finding.Source == miseNativeReleaseAgeSource {
		return true
	}
	reason := strings.ToLower(strings.TrimSpace(finding.Reason))
	return strings.Contains(reason, "mise minimum_release_age held")
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
	parts := []string{fmt.Sprintf(tr("%d provider gates", "provider確認 %d件"), len(gates))}
	for _, status := range []plan.Status{plan.StatusHeld, plan.StatusBlocked, plan.StatusError} {
		if count := gateCounts[status]; count > 0 {
			parts = append(parts, safetyGateStatusCountText(status, count))
		}
	}
	if findings > 0 {
		decisionParts := []string{}
		for _, decision := range []string{"allow", "review", "hold", "block", "unknown"} {
			if count := decisionCounts[decision]; count > 0 {
				decisionParts = append(decisionParts, safetyFindingDecisionCountText(decision, count))
			}
		}
		parts = append(parts, fmt.Sprintf(tr("%d findings (%s)", "検出項目 %d件 (%s)"), findings, strings.Join(decisionParts, ", ")))
	}
	return strings.Join(parts, ", ")
}

func safetyGateStatusCountText(status plan.Status, count int) string {
	if defaultLanguage() == "ja" {
		switch status {
		case plan.StatusHeld:
			return fmt.Sprintf("保留provider %d件", count)
		case plan.StatusBlocked:
			return fmt.Sprintf("block provider %d件", count)
		case plan.StatusError:
			return fmt.Sprintf("error provider %d件", count)
		default:
			return fmt.Sprintf("%s provider %d件", status, count)
		}
	}
	switch status {
	case plan.StatusHeld:
		return fmt.Sprintf("%d held providers", count)
	case plan.StatusBlocked:
		return fmt.Sprintf("%d blocked providers", count)
	case plan.StatusError:
		return fmt.Sprintf("%d error providers", count)
	default:
		return fmt.Sprintf("%d %s providers", count, status)
	}
}

func safetyFindingDecisionCountText(decision string, count int) string {
	if defaultLanguage() == "ja" {
		return fmt.Sprintf("%s %d件", decision, count)
	}
	return fmt.Sprintf("%d %s", count, decision)
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
			fmt.Fprintf(w, "      %s\n", localizedSafetyReasonWithReleaseAge(finding))
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
