package cmd

import (
	"bytes"
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

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updatelog"
	"github.com/webkaz-labs/updev/internal/updatereason"
	"github.com/webkaz-labs/updev/internal/updevpath"

	tea "charm.land/bubbletea/v2"
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
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Commands     []updateCommand   `json:"commands,omitempty"`
	Status       plan.Status       `json:"status"`
	Stdout       string            `json:"stdout,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	ReasonCode   string            `json:"reason_code,omitempty"`
	ReasonArgs   map[string]string `json:"reason_args,omitempty"`
	Skipped      bool              `json:"skipped,omitempty"`
	Updated      []string          `json:"updated,omitempty"`
	SkippedItems []string          `json:"skipped_items,omitempty"`
}

type updateCommand struct {
	Command []string `json:"command"`
}

func setUpdateStepReason(step *updateStep, reason updatereason.Reason) {
	if step == nil {
		return
	}
	step.Reason = reason.Text
	step.ReasonCode = reason.Code
	step.ReasonArgs = reason.Args
}

func setUpdateStepReasonText(step *updateStep, reason string) {
	setUpdateStepReason(step, updatereason.Infer(reason))
}

func localizedUpdateStepReasonForStep(step updateStep) string {
	reason := updatereason.Reason{
		Code: step.ReasonCode,
		Text: step.Reason,
		Args: step.ReasonArgs,
	}
	if reason.Code == "" {
		reason = updatereason.Infer(step.Reason)
	}
	if defaultLanguage() == "ja" {
		switch reason.Code {
		case updatereason.MiseBumpCandidateChangedApply, updatereason.MiseBumpCandidateChangedPreview:
			reason.Args = cloneStringMap(reason.Args)
			reason.Args["detail"] = localizedMiseBumpCandidateChange(reason.Args["detail"])
		}
		localized := updatereason.LocalizeJapanese(reason)
		if localized != "" {
			return localized
		}
	}
	return reason.Text
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
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
			Name:     "brew",
			Command:  brew.UpgradeGreedyCommand(),
			Commands: updateCommandsFromArgv(brew.UpgradeGreedyCommands()),
		},
		{
			Name:     "mise",
			Command:  mise.UpgradeAllCommand(),
			Commands: updateCommandsFromArgv(mise.UpgradeAllCommands()),
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
		return runUpdateStepWithWriters(updateStepRunOptions{
			Context:    ctx,
			Runner:     commandRunner,
			Step:       step,
			DryRun:     dryRun,
			HoldReason: holdReason,
			Stdout:     updateProviderStdoutWriter(),
			Stderr:     os.Stderr,
		})
	}
	return runUpdateStepWithWriters(updateStepRunOptions{
		Context:    ctx,
		Runner:     commandRunner,
		Step:       step,
		DryRun:     dryRun,
		HoldReason: holdReason,
	})
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

type updateStepRunOptions struct {
	Context    context.Context
	Runner     commandRunner
	Step       updateStep
	DryRun     bool
	HoldReason string
	Stdout     io.Writer
	Stderr     io.Writer
}

func runUpdateStepWithWriters(options updateStepRunOptions) updateStep {
	step := options.Step
	if options.HoldReason != "" {
		step.Status = plan.StatusHeld
		setUpdateStepReasonText(&step, options.HoldReason)
		step.Skipped = true
		step.SkippedItems = append(step.SkippedItems, options.HoldReason)
		return step
	}
	preSkipped := append([]string(nil), step.SkippedItems...)
	preReason := step.Reason
	if options.DryRun {
		step.Status = plan.StatusOK
		if len(preSkipped) > 0 {
			step.Status = plan.StatusHeld
			step.Skipped = true
			if preReason == "" {
				setUpdateStepReason(&step, updatereason.StrictDryRunPartialReason())
			}
		}
		return step
	}
	result := runUpdateStepCommands(options.Context, options.Runner, step, options.Stdout, options.Stderr)
	step.Stdout = result.Stdout
	step.Stderr = result.Stderr
	summary := updatelog.Summarize(step.Stdout, step.Stderr)
	step.Updated = summary.Updated
	step.SkippedItems = append(preSkipped, summary.Skipped...)
	if result.Code != 0 || result.Err != nil {
		step.Status = plan.StatusError
		return step
	}
	if len(preSkipped) > 0 {
		step.Status = plan.StatusHeld
		step.Skipped = len(step.Updated) == 0
		if preReason == "" {
			setUpdateStepReason(&step, updatereason.StrictAppliedPartialReason())
		}
		return step
	}
	step.Status = plan.StatusOK
	return step
}

func runUpdateStepCommands(ctx context.Context, commandRunner commandRunner, step updateStep, stdout io.Writer, stderr io.Writer) runner.Result {
	commands := updateStepCommands(step)
	var combined runner.Result
	for _, command := range commands {
		result := runUpdateStepCommand(ctx, commandRunner, step, command.Command, stdout, stderr)
		combined.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(combined.Stdout, result.Stdout), "\n"))
		combined.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(combined.Stderr, result.Stderr), "\n"))
		if result.Code != 0 || result.Err != nil {
			combined.Code = result.Code
			combined.Err = result.Err
			return combined
		}
	}
	return combined
}

func updateStepCommands(step updateStep) []updateCommand {
	if len(step.Commands) > 0 {
		return step.Commands
	}
	if len(step.Command) == 0 {
		return nil
	}
	return []updateCommand{{Command: step.Command}}
}

func runUpdateStepCommand(ctx context.Context, commandRunner commandRunner, step updateStep, command []string, stdout io.Writer, stderr io.Writer) runner.Result {
	if len(command) == 0 {
		return runner.Result{}
	}
	if step.Name == "mise" {
		return runMiseCommand(ctx, commandRunner, stdout, stderr, command[0], command[1:]...)
	}
	if stdout != nil || stderr != nil {
		if streamingRunner, ok := commandRunner.(streamingCommandRunner); ok {
			return streamingRunner.RunStreaming(ctx, stdout, stderr, command[0], command[1:]...)
		}
	}
	return commandRunner.Run(ctx, command[0], command[1:]...)
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
		Command: brew.UpdateCommand(),
		Commands: []updateCommand{
			{Command: brew.UpdateCommand()},
		},
	}
	setUpdateStepReason(&refreshStep, updatereason.StrictBrewRefreshDoneReason())
	if stream {
		fmt.Fprintf(updateProviderProgressWriter(), tr("running %s update...\n", "%s update を実行中...\n"), refreshStep.Name)
	}
	refreshStep = runUpdateStepWithOutput(ctx, commandRunner, refreshStep, false, "", stream)
	if refreshStep.Status == plan.StatusError {
		setUpdateStepReason(&refreshStep, updatereason.StrictBrewRefreshFailedReason(miseOutdatedResultDetail(runner.Result{Stdout: refreshStep.Stdout, Stderr: refreshStep.Stderr}, "brew update failed")))
		return refreshStep, gate, true
	}
	refreshedGate := collectBrewUpdateSafetyFreshWithPolicy(ctx, commandRunner, opts.root, policy)
	if refreshedGate.Status == plan.StatusError {
		refreshStep.Status = plan.StatusError
		setUpdateStepReason(&refreshStep, updatereason.StrictBrewRefreshFailedReason(refreshedGate.Error))
		return refreshStep, refreshedGate, true
	}
	if len(refreshedGate.Findings) == 0 {
		refreshStep.Status = plan.StatusOK
		setUpdateStepReason(&refreshStep, updatereason.StrictBrewNoCandidatesReason())
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
		reason := updatereason.StrictGateFailedReason(gate.Error)
		return step, reason.Text
	}
	safe, unsafe := splitUpdateSafetyFindings(gate.Findings)
	if step.Name == "brew" && len(safe) == 0 && len(unsafe) == 0 {
		step.Command = brew.UpdateCommand()
		step.Commands = []updateCommand{{Command: brew.UpdateCommand()}}
		setUpdateStepReason(&step, updatereason.StrictBrewRefreshOnlyReason())
		return step, ""
	}
	if len(safe) == 0 && gate.Status == plan.StatusHeld {
		if step.Name == "brew" && len(unsafe) > 0 {
			step.Command = brew.UpdateCommand()
			step.Commands = []updateCommand{{Command: brew.UpdateCommand()}}
			setUpdateStepReason(&step, updatereason.StrictBrewHeldReason(len(unsafe)))
			step.SkippedItems = updateSafetySkippedSummaries(unsafe)
			return step, ""
		}
		return step, updatereason.StrictGateReviewReason().Text
	}
	if len(safe) == 0 {
		return step, ""
	}
	switch step.Name {
	case "mise":
		command := scopedMiseUpgradeCommand(opts.root, safe)
		if len(command) == 0 {
			return step, updatereason.StrictMiseNoSafeReason().Text
		}
		step.Command = command
		step.Commands = scopedMiseUpgradeCommands(opts.root, safe)
		if len(unsafe) > 0 {
			setUpdateStepReason(&step, updatereason.StrictMisePartialReason(len(safe), len(unsafe)))
		}
	case "brew":
		command := scopedBrewUpgradeCommand(safe)
		if len(command) == 0 {
			return step, updatereason.StrictBrewNoSafeReason().Text
		}
		step.Command = command
		step.Commands = scopedBrewUpgradeCommands(safe)
		if len(unsafe) > 0 {
			setUpdateStepReason(&step, updatereason.StrictBrewPartialReason(len(safe), len(unsafe)))
		}
	default:
		if gate.Status == plan.StatusHeld {
			return step, updatereason.StrictGateReviewReason().Text
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
	return mise.UpgradeCommand(root, tools, miseMinimumReleaseAgeFlagValue())
}

func scopedMiseUpgradeCommands(root string, findings []safetyFinding) []updateCommand {
	return updateCommandsFromArgv(mise.UpgradeCommands(root, updateSafetyFindingNames(findings), miseMinimumReleaseAgeFlagValue()))
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
	return brew.UpgradeGreedyNoAutoUpdateCommand(updateSafetyFindingNames(findings))
}

func scopedBrewUpgradeCommands(findings []safetyFinding) []updateCommand {
	return updateCommandsFromArgv(brew.UpgradeGreedyNoAutoUpdateCommands(updateSafetyFindingNames(findings)))
}

func updateCommandsFromArgv(commands [][]string) []updateCommand {
	out := []updateCommand{}
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		out = append(out, updateCommand{Command: command})
	}
	return out
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
		setUpdateStepReason(&step, updatereason.MiseBumpManualReason())
		for _, finding := range safe {
			step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding))
		}
		if len(safe) == 0 && len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			setUpdateStepReason(&step, updatereason.MiseBumpReviewReason())
		}
		return step, true
	case "safe":
		step.Status = plan.StatusDrift
		step.Skipped = true
		setUpdateStepReason(&step, updatereason.MiseBumpSafeManualReason(len(safe)))
		for _, finding := range safe {
			step.SkippedItems = append(step.SkippedItems, miseBumpFindingSummary(finding))
		}
		if len(safe) == 0 && len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			setUpdateStepReason(&step, updatereason.MiseBumpReviewReason())
		}
		return step, true
	case "auto":
	default:
		return updateStep{}, false
	}
	if len(safe) == 0 {
		step.Status = plan.StatusHeld
		step.Skipped = true
		setUpdateStepReason(&step, updatereason.MiseBumpReviewNoSafeAutoReason())
		return step, true
	}
	if opts.dryRun {
		step.Status = plan.StatusDrift
		setUpdateStepReason(&step, updatereason.MiseBumpAutoWouldApplyReason(len(safe)))
		for _, finding := range safe {
			step.Updated = append(step.Updated, "would bump "+miseBumpFindingSummary(finding))
		}
		if len(unsafe) > 0 {
			step.Status = plan.StatusHeld
			setUpdateStepReason(&step, updatereason.MiseBumpAutoWouldApplyWithReviewReason(len(safe), len(unsafe)))
		}
		return step, true
	}
	if err := validateMiseBumpPlannedCandidates(ctx, commandRunner, opts.root, safe); err != nil {
		step.Status = plan.StatusHeld
		step.Skipped = true
		setUpdateStepReason(&step, updatereason.MiseBumpCandidateChangedApplyReason(err.Error()))
		return step, true
	}
	reviewCount := len(unsafe)
	step.Command = miseBumpCommandForFindings(opts.root, true, false, safe)
	step.Commands = append(step.Commands, updateCommand{Command: step.Command})
	preflight := runMiseBumpCommand(miseBumpRunOptions{
		Context:  ctx,
		Runner:   commandRunner,
		Root:     opts.root,
		DryRun:   true,
		Findings: safe,
	})
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
			setUpdateStepReason(&step, updatereason.MiseBumpDependencyBlockedOnlyReason())
			return step, true
		}
		step.Commands = append(step.Commands, updateCommand{Command: step.Command})
		preflight = runMiseBumpCommand(miseBumpRunOptions{
			Context:  ctx,
			Runner:   commandRunner,
			Root:     opts.root,
			DryRun:   true,
			Findings: safe,
		})
		step.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(step.Stdout, preflight.Stdout), "\n"))
		step.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(step.Stderr, preflight.Stderr), "\n"))
	}
	if preflight.Code != 0 || preflight.Err != nil {
		step.Status = plan.StatusHeld
		step.Skipped = true
		setUpdateStepReason(&step, updatereason.MiseBumpPreflightFailedReason(miseOutdatedResultDetail(preflight, "preflight failed")))
		return step, true
	}
	var stdout io.Writer
	var stderr io.Writer
	if stream {
		stdout = updateProviderStdoutWriter()
		stderr = os.Stderr
		fmt.Fprintf(updateProviderProgressWriter(), tr("running %s update...\n", "%s update を実行中...\n"), step.Name)
	}
	result := runMiseBumpCommand(miseBumpRunOptions{
		Context:  ctx,
		Runner:   commandRunner,
		Root:     opts.root,
		Yes:      true,
		Findings: safe,
		Stdout:   stdout,
		Stderr:   stderr,
	})
	step.Stdout = strings.TrimSpace(strings.Join(nonEmptyStrings(preflight.Stdout, result.Stdout), "\n"))
	step.Stderr = strings.TrimSpace(strings.Join(nonEmptyStrings(preflight.Stderr, result.Stderr), "\n"))
	step.Command = miseBumpCommandForFindings(opts.root, false, true, safe)
	step.Commands = append(step.Commands, updateCommand{Command: step.Command})
	if result.Code != 0 || result.Err != nil {
		summary := updatelog.Summarize(result.Stdout, result.Stderr)
		step.Updated = updatelog.AppendUniqueUpdated(step.Updated, summary.Updated...)
		step.SkippedItems = updatelog.AppendUniqueSkipped(step.SkippedItems, summary.Skipped...)
		step.Status = plan.StatusError
		setUpdateStepReason(&step, updatereason.MiseBumpFailedReason(miseOutdatedResultDetail(result, "mise upgrade --bump failed")))
		return step, true
	}
	if reviewCount > 0 {
		step.Status = plan.StatusHeld
		setUpdateStepReason(&step, updatereason.MiseBumpAppliedWithReviewReason(len(safe), reviewCount))
	} else {
		step.Status = plan.StatusOK
	}
	for _, finding := range safe {
		step.Updated = append(step.Updated, miseBumpFindingSummary(finding))
	}
	return step, true
}

type miseBumpRunOptions struct {
	Context  context.Context
	Runner   commandRunner
	Root     string
	DryRun   bool
	Yes      bool
	Findings []safetyFinding
	Stdout   io.Writer
	Stderr   io.Writer
}

func runMiseBumpCommand(options miseBumpRunOptions) runner.Result {
	command := miseBumpCommandForFindings(options.Root, options.DryRun, options.Yes, options.Findings)
	if len(command) == 0 {
		return runner.Result{}
	}
	cleanup := func() {}
	if miseBumpNeedsSanitizedNPMUserConfig(options.Findings) {
		wrapped, wrappedCleanup, err := miseBumpCommandWithSanitizedNPMUserConfig(command)
		if err != nil {
			return runner.Result{Stderr: err.Error(), Err: err, Code: 1}
		}
		command = wrapped
		cleanup = wrappedCleanup
	}
	defer cleanup()
	return runMiseCommand(options.Context, options.Runner, options.Stdout, options.Stderr, command[0], command[1:]...)
}

func miseBumpCommandForFindings(root string, dryRun bool, yes bool, findings []safetyFinding) []string {
	return mise.BumpCommand(root, miseBumpToolNames(findings), dryRun, yes, miseBumpNeedsReleaseAgeBypass(findings))
}

func miseBumpApplyCommands(root string, findings []safetyFinding) []updateCommand {
	return updateCommandsFromArgv(mise.BumpApplyCommands(root, miseBumpToolNames(findings), miseBumpNeedsReleaseAgeBypass(findings)))
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
	if home := updevpath.HomeDir(); home != "" {
		add(filepath.Join(home, ".npmrc"))
	}
	if xdgConfig := updevpath.ConfigHome(); xdgConfig != "" {
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
			fmt.Fprintf(w, "    %s %s\n", textui.StyleLabel(tr("reason:", "理由:"), color), truncate(oneLine(localizedUpdateStepReasonForStep(step)), 120))
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
		return truncate(oneLine(localizedUpdateStepReasonForStep(step)), 72)
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
	dir := updevpath.ReportCacheDir()
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
	dir := updevpath.ReportCacheDir()
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
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("age:", "経過:"), color), textui.FriendlyAge(time.Since(entry.CreatedAt)))
	}
	report := filterUpdateReport(entry.Report, opts)
	if opts.section != "" && opts.section != "summary" {
		fmt.Fprintf(w, "%s %s %s\n", textui.StyleLabel(tr("section:", "section:"), color), textui.StyleRequested(opts.section, color), textui.StyleStatus(string(updateSectionStatus(report, opts.section)), color))
	}
	if filters := lastReportFilterMap(opts); len(filters) > 0 {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("filters:", "フィルター:"), color), textui.StyleRequested(textui.FilterSummary(filters, filterSummaryKeys...), color))
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
		if plan.IsAttentionStatus(item.Status) {
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
			item = updatelog.NormalizeUpdatedItem(item)
			if item == "" || updatelog.IsProgressLine(item) {
				continue
			}
			normalizedUpdated = updatelog.AppendUniqueUpdated(normalizedUpdated, item)
		}
		normalizedSkipped := []string{}
		for _, item := range step.SkippedItems {
			item = updatelog.NormalizeSkippedItem(item)
			if item == "" || updatelog.IsGenericSkippedLine(item) {
				continue
			}
			normalizedSkipped = updatelog.AppendUniqueSkipped(normalizedSkipped, item)
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
		if opts.status != "" && !plan.StatusMatches(step.Status, opts.status) {
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
		updateStepCommandText(step),
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
	if opts.status != "" && !plan.StatusMatches(gate.Status, opts.status) {
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
		if opts.status != "" && !plan.StatusMatches(plan.ProviderStatus(provider), opts.status) {
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
			name, detail := updatelog.UpdatedItemParts(item)
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
				truncate(compactUpdateSummaryDetail(detail), 72),
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
				truncate(compactUpdateSummaryDetail(localizedUpdateStepReasonForStep(step)), 72),
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
			detail := compactUpdateOutcomeFindingDetail(finding)
			if decision == "warning" {
				detail = strings.ReplaceAll(detail, "hold", "warning")
			}
			rows = append(rows, []string{
				textui.StyleStatus(decision, color),
				textui.StyleName(firstNonEmpty(gate.Provider, finding.Provider), color),
				truncate(item, 38),
				truncate(detail, 72),
			})
			if len(rows) >= limit {
				return rows
			}
		}
	}
	return rows
}

func compactUpdateSummaryDetail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if text := compactKnownListEvidenceText(value); text != "" {
		return text
	}
	return oneLine(value)
}

func compactUpdateOutcomeFindingDetail(finding safetyFinding) string {
	if text := compactKnownListEvidenceText(localizedSafetyReasonWithReleaseAge(finding)); text != "" {
		return text
	}
	return firstNonEmpty(updateOutcomeFindingDetail(finding), localizedSafetyReasonWithReleaseAge(finding))
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
	if name, detail := updatelog.UpdatedItemParts(strings.TrimPrefix(item, "would bump ")); name != "" && strings.TrimSpace(name) != strings.TrimSpace(item) {
		return name, detail
	}
	return firstNonEmpty(step.Name, "step"), item
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
				reason := compactSecurityItemReason(finding)
				if displayDecision == "warning" {
					reason = strings.ReplaceAll(reason, "hold", "warning")
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
					truncate(reason, 72),
				})
				if len(rows) >= limit {
					return rows
				}
			}
		}
	}
	return rows
}

func compactSecurityItemReason(finding safetyFinding) string {
	reason := localizedSafetyReasonWithReleaseAge(finding)
	if text := compactKnownListEvidenceText(reason); text != "" {
		return text
	}
	return oneLine(reason)
}

func printUpdateInventoryDashboard(w io.Writer, inventory plan.Report, color bool) {
	if len(inventory.Providers) == 0 && len(inventory.Items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s %s\n", textui.StyleHeading("inventory", color), textui.StyleStatus(string(inventory.Status), color))
	providerRows := [][]string{}
	for _, provider := range inventory.Providers {
		status := string(plan.ProviderStatus(provider))
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
	for _, status := range plan.AttentionStatusOrder() {
		for _, item := range items {
			if item.Status != status {
				continue
			}
			rows = append(rows, []string{
				textui.StyleStatus(inventoryannotate.ItemStatusLabel(item), color),
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
		if strings.EqualFold(item.Provider, provider) && inventoryannotate.ItemHasProfileMismatch(item) {
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

const (
	securityDetailActionPrefix = "security-policy"
	miseBumpDetailActionPrefix = "mise-bump"
)

const (
	securityActionBrewTrustFormula = "brew-trust-formula"
	securityActionBrewTrustCask    = "brew-trust-cask"
	securityActionBrewTrustTap     = "brew-trust-tap"
)

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
	opts := lastReportOptions{provider: route.Provider, status: route.Status, query: route.Query}
	filtered := filterUpdateReport(report, opts)
	suffix := updateSummaryRouteTitleSuffix(route)
	switch route.Base {
	case updateHubActionLogs:
		stateKey := "summary:logs:" + textui.FilterSummary(lastReportFilterMap(opts), filterSummaryKeys...)
		state, err := runDetailBrowserWithState("updev update logs"+suffix, updateLogDetailRows(filtered), focusedRouteDetailState(), color)
		if err != nil {
			printLastUpdateLogs(os.Stdout, filtered, color)
			return false
		}
		detailStates[stateKey] = state
		return state.Action == updevActionExit
	case updateHubActionSecurity:
		stateKey := "summary:security:" + textui.FilterSummary(lastReportFilterMap(opts), filterSummaryKeys...)
		state, err := runDetailBrowserWithState("updev security details"+suffix, updateSecurityDetailRowsForFilter(filtered, opts), focusedRouteDetailState(), color)
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
		stateKey := "summary:inventory-details:" + textui.FilterSummary(lastReportFilterMap(opts), filterSummaryKeys...)
		state, err := runDetailBrowserWithState("updev inventory details"+suffix, updateInventoryDetailRowsWithBackend(filtered, backendPlan), focusedRouteDetailState(), color)
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
	if route.Status != "" {
		parts = append(parts, "status="+route.Status)
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
	if isHomebrewTrustSecurityAction(action) {
		if !confirmHomebrewTrustAction(action, kind, name) {
			return true
		}
		return applyConfirmedHomebrewTrustDetailAction(report, action, kind, name, true)
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
	result := runMiseBumpCommand(miseBumpRunOptions{
		Context:  context.Background(),
		Runner:   runner.Local{},
		Root:     report.Root,
		Yes:      true,
		Findings: findings,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	})
	step := updateStep{
		Name:     miseBumpProvider,
		Command:  miseBumpCommandForFindings(report.Root, false, true, findings),
		Commands: miseBumpApplyCommands(report.Root, findings),
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}
	if result.Code != 0 || result.Err != nil {
		step.Status = plan.StatusError
		setUpdateStepReason(&step, updatereason.MiseBumpFailedReason(miseOutdatedResultDetail(result, "mise upgrade --bump failed")))
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
	command := miseBumpCommandForFindings(root, true, false, findings)
	fmt.Printf("%s %s\n", textui.StyleLabel("preview:", textui.ColorEnabled()), joinCommand(command))
	if err := validateMiseBumpPlannedCandidates(context.Background(), runner.Local{}, root, findings); err != nil {
		fmt.Fprintf(os.Stderr, "mise bump candidate set changed before preview: %s\n", err)
		return false
	}
	preflight := runMiseBumpCommand(miseBumpRunOptions{
		Context:  context.Background(),
		Runner:   runner.Local{},
		Root:     root,
		DryRun:   true,
		Findings: findings,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	})
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
	case "allow-7d", "allow-7d-rerun", "allow-custom", "allow-custom-rerun", "hold",
		securityActionBrewTrustFormula, securityActionBrewTrustCask, securityActionBrewTrustTap:
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
	case "allow-custom", "allow-custom-rerun":
		return "allow", "", "", true
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
	if isHomebrewTrustSecurityAction(action) {
		return applyConfirmedHomebrewTrustDetailAction(report, action, kind, name, printResult)
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
	if holdReason != "" || len(updateStepCommands(scoped)) == 0 {
		return updateStep{}, false
	}
	setUpdateStepReason(&scoped, updatereason.SecurityPolicyScopedRerunReason(provider, kind, name))
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
	if trustAction, ok := homebrewTrustDetailAction(gate, finding); ok {
		actions = append(actions, trustAction)
	}
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
	actions = append(
		actions,
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
	choices = append(
		choices,
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
		if plan.IsAttentionStatus(item.Status) || inventoryannotate.ItemHasProfileMismatch(item) {
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
		if plan.IsAttentionStatus(step.Status) {
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
	metadata = appendDetailMeta(metadata, "reason_code", finding.ReasonCode)
	metadata = appendDetailMeta(metadata, "remediation", remediation)
	metadata = appendDetailMeta(metadata, "source", finding.Source)
	metadata = appendDetailMeta(metadata, "tap", finding.Tap)
	metadata = appendDetailMeta(metadata, "publisher", finding.Publisher)
	metadata = appendDetailMeta(metadata, "repository", finding.RepositoryURL)
	metadata = appendDetailMeta(metadata, "support", finding.SupportURL)
	metadata = appendDetailMeta(metadata, "homepage", finding.Homepage)
	metadata = appendDetailMeta(metadata, "url", finding.URL)
	metadata = appendDetailMeta(metadata, "homepage host", finding.HomepageHost)
	metadata = appendDetailMeta(metadata, "download host", finding.URLHost)
	metadata = appendDetailMeta(metadata, "trust", finding.TrustStatus)
	metadata = appendDetailMeta(metadata, "trust target", finding.TrustTarget)
	metadata = appendDetailMeta(metadata, "trust command", finding.TrustCommand)
	metadata = appendDetailMeta(metadata, "trust command argv", joinCommand(finding.TrustCommandArgv))
	metadata = appendDetailMeta(metadata, "release", finding.ReleaseDate)
	metadata = appendDetailMeta(metadata, "release age", safetyFindingReleaseAgeSummary(finding))
	metadata = appendDetailMeta(metadata, "published", finding.PublishedDate)
	metadata = appendDetailMeta(metadata, "last updated", finding.LastUpdated)
	metadata = appendDetailMeta(metadata, "cache", safetyFindingCacheEvidenceSummary(finding))
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

func updateStepCommandText(step updateStep) string {
	commands := updateStepCommands(step)
	if len(commands) == 0 {
		return ""
	}
	out := []string{}
	for _, command := range commands {
		if len(command.Command) == 0 {
			continue
		}
		out = append(out, joinCommand(command.Command))
	}
	return strings.Join(out, " ; ")
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
			"command: " + updateStepCommandText(step),
		}
		if step.Skipped {
			metadata = append(metadata, "skipped: true")
		}
		if step.Reason != "" {
			metadata = append(metadata, "reason: "+localizedUpdateStepReasonForStep(step))
			if step.ReasonCode != "" {
				metadata = append(metadata, "reason_code: "+step.ReasonCode)
			}
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
			Detail:   firstNonEmpty(localizedUpdateStepReasonForStep(step), step.Stdout, step.Stderr),
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
				"command: " + updateStepCommandText(step),
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
				"command: " + updateStepCommandText(step),
			},
			Actions: updateStepItemDetailActions(step, item),
		})
	}
	return rows
}

func updateStepItemDetailActions(step updateStep, item string) []detailBrowserAction {
	actions := []detailBrowserAction{}
	query := updateStepItemRouteQuery(item)
	if query == "" {
		return updateStepDetailActions(step)
	}
	if step.Name == miseBumpProvider {
		actions = append(actions, detailBrowserAction{
			Value:       updateSummaryRoute{Base: updateHubActionSecurity, Provider: step.Name, Query: query}.Encode(),
			Label:       tr("open this bump candidate", "この bump 候補を開く"),
			Description: tr("open security evidence for this mise bump item", "この mise bump item のセキュリティ根拠を開きます"),
		})
	} else if step.Status == plan.StatusHeld || strings.Contains(strings.ToLower(step.Reason), "security") {
		actions = append(actions, detailBrowserAction{
			Value:       updateSummaryRoute{Base: updateHubActionSecurity, Provider: step.Name, Query: query}.Encode(),
			Label:       tr("open this security review", "このセキュリティ確認を開く"),
			Description: tr("inspect held security evidence for this item", "この item の保留セキュリティ根拠を確認します"),
		})
	}
	if len(actions) == 0 {
		return updateStepDetailActions(step)
	}
	return actions
}

func updateStepItemRouteQuery(item string) string {
	fields := strings.Fields(strings.TrimSpace(item))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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
		return localizedUpdateStepReasonForStep(step)
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
			printDetailLine(w, "homepage host", finding.HomepageHost, color)
			printDetailLine(w, "download host", finding.URLHost, color)
			printDetailLine(w, "release", finding.ReleaseDate, color)
			printDetailLine(w, "release age", safetyFindingReleaseAgeSummary(finding), color)
			printDetailLine(w, "published", finding.PublishedDate, color)
			printDetailLine(w, "last updated", finding.LastUpdated, color)
			printDetailLine(w, "cache", safetyFindingCacheEvidenceSummary(finding), color)
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

func safetyFindingReleaseAgeSummary(finding safetyFinding) string {
	if finding.ReleaseAgeDays == 0 && finding.MinReleaseAgeDays == 0 {
		return ""
	}
	if defaultLanguage() == "ja" {
		if finding.MinReleaseAgeDays > 0 {
			return fmt.Sprintf("経過 %d日 / 最小 %d日", finding.ReleaseAgeDays, finding.MinReleaseAgeDays)
		}
		return fmt.Sprintf("経過 %d日", finding.ReleaseAgeDays)
	}
	if finding.MinReleaseAgeDays > 0 {
		return fmt.Sprintf("%d days old / minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays)
	}
	return fmt.Sprintf("%d days old", finding.ReleaseAgeDays)
}

func safetyFindingCacheEvidenceSummary(finding safetyFinding) string {
	const prefix = "updev update safety cache:"
	values := []string{}
	for _, evidence := range finding.Evidence {
		evidence = strings.TrimSpace(evidence)
		if !strings.HasPrefix(strings.ToLower(evidence), prefix) {
			continue
		}
		value := strings.TrimSpace(evidence[len(prefix):])
		if value == "" {
			continue
		}
		if defaultLanguage() == "ja" {
			value = strings.TrimSuffix(value, " old") + " 経過"
		}
		values = append(values, value)
	}
	return strings.Join(values, "; ")
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
		printDetailLine(w, "reason", localizedUpdateStepReasonForStep(step), color)
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

func localizedNativeAuditReason(audit nativeAudit) string {
	reason := securityreason.Reason{
		Code: audit.ReasonCode,
		Text: audit.Reason,
		Args: audit.ReasonArgs,
	}
	if reason.Code == "" {
		reason = securityreason.Infer(audit.Reason)
		if reason.Code == securityreason.NativeAuditVulnerability {
			reason.Args = cloneStringMap(reason.Args)
			reason.Args["tool"] = firstNonEmpty(audit.Tool, reason.Args["tool"])
			reason.Args["ecosystem"] = firstNonEmpty(audit.Ecosystem, reason.Args["ecosystem"])
			reason.Args["target"] = firstNonEmpty(audit.Target, reason.Args["target"])
		}
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		if localized := securityreason.LocalizeJapanese(reason); localized != "" {
			return localized
		}
	}
	return localizedSecurityReason(audit.Reason)
}

func localizedGitHubPostureReason(posture githubPosture) string {
	reason := securityreason.Reason{Code: posture.ReasonCode, Text: posture.Reason, Args: posture.ReasonArgs}
	if reason.Code == "" {
		reason = securityreason.Infer(posture.Reason)
	}
	if reason.Code != "" {
		reason.Args = cloneStringMap(reason.Args)
		reason.Args["repository"] = firstNonEmpty(reason.Args["repository"], posture.Repository)
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		if localized := securityreason.LocalizeJapanese(reason); localized != "" {
			return localized
		}
	}
	return localizedSecurityReason(posture.Reason)
}

func localizedHomebrewPostureReason(posture homebrewPosture) string {
	reason := securityreason.Reason{Code: posture.ReasonCode, Text: posture.Reason, Args: posture.ReasonArgs}
	if reason.Code == "" {
		reason = securityreason.Infer(posture.Reason)
	}
	if reason.Code != "" {
		reason.Args = cloneStringMap(reason.Args)
		delete(reason.Args, "kind")
		delete(reason.Args, "name")
		reason.Args["homepage_host"] = firstNonEmpty(reason.Args["homepage_host"], posture.HomepageHost)
		reason.Args["url_host"] = firstNonEmpty(reason.Args["url_host"], posture.URLHost)
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		if localized := securityreason.LocalizeJapanese(reason); localized != "" {
			return localized
		}
	}
	return localizedSecurityReason(posture.Reason)
}

func localizedVSCodePostureReason(posture vscodePosture) string {
	reason := securityreason.Reason{Code: posture.ReasonCode, Text: posture.Reason, Args: posture.ReasonArgs}
	if reason.Code == "" {
		reason = securityreason.Infer(posture.Reason)
	}
	if reason.Code != "" {
		reason.Args = cloneStringMap(reason.Args)
		delete(reason.Args, "extension")
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		if localized := securityreason.LocalizeJapanese(reason); localized != "" {
			return localized
		}
	}
	return localizedSecurityReason(posture.Reason)
}

func localizedNPMPostureReason(posture npmPosture) string {
	return localizedRegistryPostureReason(posture.Reason, posture.ReasonCode, posture.ReasonArgs, "npm", posture.Package, posture.Version)
}

func localizedCargoPostureReason(posture cargoPosture) string {
	return localizedRegistryPostureReason(posture.Reason, posture.ReasonCode, posture.ReasonArgs, "crates.io", posture.Crate, posture.Version)
}

func localizedPyPIPostureReason(posture pypiPosture) string {
	return localizedRegistryPostureReason(posture.Reason, posture.ReasonCode, posture.ReasonArgs, "PyPI", posture.Package, posture.Version)
}

func localizedRegistryPostureReason(text string, code string, args map[string]string, registry string, packageName string, version string) string {
	reason := securityreason.Reason{Code: code, Text: text, Args: args}
	if reason.Code == "" {
		reason = securityreason.Infer(text)
	}
	if reason.Code != "" {
		reason.Args = cloneStringMap(reason.Args)
		reason.Args["registry"] = firstNonEmpty(reason.Args["registry"], registry)
		reason.Args["package"] = firstNonEmpty(reason.Args["package"], packageName)
		reason.Args["version"] = firstNonEmpty(reason.Args["version"], version)
	}
	if defaultLanguage() == "ja" && reason.Code != "" {
		if localized := securityreason.LocalizeJapanese(reason); localized != "" {
			return localized
		}
	}
	return localizedSecurityReason(text)
}

func localizedUpdateStepReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || defaultLanguage() != "ja" {
		return reason
	}
	inferred := updatereason.Infer(reason)
	if inferred.Code != "" {
		switch inferred.Code {
		case updatereason.MiseBumpCandidateChangedApply, updatereason.MiseBumpCandidateChangedPreview:
			inferred.Args = cloneStringMap(inferred.Args)
			inferred.Args["detail"] = localizedMiseBumpCandidateChange(inferred.Args["detail"])
		}
		if localized := updatereason.LocalizeJapanese(inferred); localized != "" {
			return localized
		}
	}
	return reason
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
	reason := strings.TrimSpace(localizedSafetyFindingReason(finding))
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

type (
	updateSummaryLine     = reviewui.ActionSummaryLine
	updateSummaryLineKind = reviewui.ActionSummaryLineKind
)

const (
	updateSummaryLineNormal      = reviewui.ActionSummaryLineNormal
	updateSummaryLineSection     = reviewui.ActionSummaryLineSection
	updateSummaryLineTableHeader = reviewui.ActionSummaryLineTableHeader
	updateSummaryLineMeta        = reviewui.ActionSummaryLineMeta
)

type updateSummaryRoute struct {
	Base     string
	Provider string
	Query    string
	Status   string
}

const updateSummaryRoutePrefix = "summary-route"

type updateSummaryBrowserModel = reviewui.ActionSummaryModel

type updateSummaryBrowserOptions struct {
	Title          string
	Report         updateReport
	ManualPlan     inventoryPlanReport
	ManualLoading  bool
	BackendPlan    backendPlanReport
	BackendLoading bool
	State          reviewui.State
	FocusAction    string
	Color          bool
}

func newUpdateSummaryBrowserModel(options updateSummaryBrowserOptions) updateSummaryBrowserModel {
	return newActionSummaryBrowserModel(
		options.Title,
		updateSummaryBrowserLinesWithLoading(
			options.Report,
			options.ManualPlan,
			options.ManualLoading,
			options.BackendPlan,
			options.BackendLoading,
			options.Color,
		),
		options.State,
		options.FocusAction,
		options.Color,
	)
}

func newActionSummaryBrowserModel(title string, lines []updateSummaryLine, state reviewui.State, focusAction string, color bool) updateSummaryBrowserModel {
	labels := reviewui.ActionSummaryLabels{
		HelpMove:      tr("Up/Down or j/k: move between selectable summary rows", "↑↓ または j/k: 選択可能な summary 行を移動"),
		HelpOpen:      tr("Enter, Space, or a: open the selected detail", "Enter / Space / a: 選択した詳細を開く"),
		HelpExit:      tr("b, q, Esc, or Ctrl-C: exit", "b / q / Esc / Ctrl-C: 終了"),
		Controls:      tr("Up/Down/j/k move, Enter open selected summary, Space/a open, ? help, q exit", "↑↓/j/k 移動、Enter で選択 summary を開く、Space/a も開く、? help、q 終了"),
		FocusedPrefix: tr("focused actions:", "選択中の操作:"),
		EnterFormat:   tr("Enter: %s", "Enter: %s"),
	}
	actions := reviewui.ActionSummaryActions{Exit: updevActionExit}
	focusMatcher := func(lineAction string, focusAction string) bool {
		route, ok := parseUpdateSummaryRoute(lineAction)
		return ok && route.Base == focusAction
	}
	return reviewui.NewActionSummaryModel(reviewui.ActionSummaryOptions{
		Title:        title,
		Lines:        lines,
		State:        state,
		FocusAction:  focusAction,
		Labels:       labels,
		Actions:      actions,
		FocusMatcher: focusMatcher,
		Color:        color,
	})
}

func updateSummaryBrowserLines(report updateReport, manualPlan inventoryPlanReport, backendPlan backendPlanReport, color bool) []updateSummaryLine {
	return updateSummaryBrowserLinesWithLoading(report, manualPlan, false, backendPlan, false, color)
}

func updateSummaryBrowserLinesWithLoading(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, color bool) []updateSummaryLine {
	var styled bytes.Buffer
	var plain bytes.Buffer
	printUpdateBodyTo(&styled, report, color)
	printUpdateBodyTo(&plain, report, false)
	styledLines := strings.Split(strings.TrimRight(styled.String(), "\n"), "\n")
	plainLines := strings.Split(strings.TrimRight(plain.String(), "\n"), "\n")
	out := make([]updateSummaryLine, 0, len(styledLines)+6)
	section := ""
	for index, styledLine := range styledLines {
		plainLine := ""
		if index < len(plainLines) {
			plainLine = plainLines[index]
		}
		trimmed := strings.TrimSpace(plainLine)
		action := ""
		label := ""
		allowTableRoute := true
		kind := updateSummaryLineNormal
		switch trimmed {
		case tr("updates", "更新"):
			section = "updates"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionLogs
			label = tr("open update details", "更新詳細を開く")
		case tr("update outcome", "更新結果"):
			section = "outcome"
			kind = updateSummaryLineSection
			styledLine = trimmed
		case tr("security attention", "セキュリティ注意項目"), tr("top security items", "主なセキュリティ項目"):
			section = "security"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionSecurity
			label = tr("open security details", "security 詳細を開く")
		case tr("top inventory items", "主な inventory 項目"):
			section = "inventory-items"
			kind = updateSummaryLineSection
			styledLine = trimmed
			action = updateHubActionInventoryDetails
			label = tr("open inventory details", "inventory 詳細を開く")
		default:
			if strings.HasPrefix(trimmed, "inventory ") {
				section = "inventory"
				kind = updateSummaryLineSection
				styledLine = trimmed
				action = updateSummaryRoute{Base: updateHubActionInventoryAll, Status: "attention"}.Encode()
				label = tr("open inventory attention", "inventory 注意行を開く")
			}
		}
		if action == "" {
			switch {
			case strings.HasPrefix(trimmed, tr("safety summary:", "安全性サマリー:")):
				section = "security"
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("update summary:", "更新サマリー:")):
				section = "updates"
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("report:", "レポート:")):
				action = updateHubActionFull
				label = tr("open full report", "full report を開く")
				kind = updateSummaryLineMeta
				allowTableRoute = false
			case strings.HasPrefix(trimmed, tr("reason:", "理由:")):
				kind = updateSummaryLineMeta
				allowTableRoute = false
			}
		}
		if kind == updateSummaryLineNormal && isUpdateSummaryTableHeaderLine(trimmed) {
			kind = updateSummaryLineTableHeader
			styledLine = plainLine
		}
		if action == "" && allowTableRoute && isUpdateSummaryTableDataLine(trimmed) {
			if route, routeLabel, ok := updateSummaryRouteForTableLine(section, trimmed); ok {
				action = route.Encode()
				label = routeLabel
				styledLine = strings.TrimPrefix(styledLine, "  ")
			}
		}
		out = append(out, updateSummaryLine{
			Text:            styledLine,
			Action:          action,
			Label:           label,
			HideInlineBadge: kind == updateSummaryLineSection,
			Kind:            kind,
		})
	}
	reviewLines := updateSummaryReviewActionLinesWithLoading(manualPlan, manualLoading, backendPlan, backendLoading, color)
	if len(reviewLines) > 0 {
		out = append(out, updateSummaryLine{})
		out = append(out, updateSummaryLine{Text: tr("review actions", "確認アクション"), Kind: updateSummaryLineSection})
		out = append(out, updateSummaryLine{Text: updateSummaryReviewHeaderLine(false), Kind: updateSummaryLineTableHeader})
		out = append(out, reviewLines...)
	}
	return out
}

func updateSummaryRouteForTableLine(section string, line string) (updateSummaryRoute, string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return updateSummaryRoute{}, "", false
	}
	switch section {
	case "updates":
		provider := fields[0]
		return updateSummaryRoute{Base: updateHubActionLogs, Provider: provider}, fmt.Sprintf(tr("open %s update details", "%s の更新詳細を開く"), provider), true
	case "outcome":
		if len(fields) < 3 {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[1]
		query := fields[2]
		if updateOutcomeTypeRoutesToSecurity(fields[0]) {
			return updateSummaryRoute{Base: updateHubActionSecurity, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s security details", "%s の security 詳細を開く"), provider), true
		}
		return updateSummaryRoute{Base: updateHubActionLogs, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s update details", "%s の更新詳細を開く"), provider), true
	case "security":
		providerIndex := 0
		hasDecision := false
		if strings.EqualFold(fields[0], "hold") || strings.EqualFold(fields[0], "review") || strings.EqualFold(fields[0], "allow") || strings.EqualFold(fields[0], "block") {
			providerIndex = 1
			hasDecision = true
		}
		if providerIndex >= len(fields) {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[providerIndex]
		query := ""
		if hasDecision && providerIndex+1 < len(fields) {
			query = fields[providerIndex+1]
		}
		return updateSummaryRoute{Base: updateHubActionSecurity, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s security details", "%s の security 詳細を開く"), provider), true
	case "inventory":
		provider := fields[0]
		return updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: provider, Status: "attention"}, fmt.Sprintf(tr("open %s inventory attention", "%s の inventory 注意行を開く"), provider), true
	case "inventory-items":
		if len(fields) < 4 {
			return updateSummaryRoute{}, "", false
		}
		provider := fields[1]
		query := fields[3]
		return updateSummaryRoute{Base: updateHubActionInventoryDetails, Provider: provider, Query: query}, fmt.Sprintf(tr("open %s inventory details", "%s の inventory 詳細を開く"), provider), true
	default:
		return updateSummaryRoute{}, "", false
	}
}

func updateOutcomeTypeRoutesToSecurity(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "attention", "block", "hold", "review", "unknown", "warning":
		return true
	default:
		return false
	}
}

func (r updateSummaryRoute) Encode() string {
	return strings.Join([]string{updateSummaryRoutePrefix, r.Base, r.Provider, r.Query, r.Status}, "\t")
}

func parseUpdateSummaryRoute(value string) (updateSummaryRoute, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 4 && len(parts) != 5 {
		return updateSummaryRoute{}, false
	}
	if parts[0] != updateSummaryRoutePrefix {
		return updateSummaryRoute{}, false
	}
	route := updateSummaryRoute{Base: parts[1], Provider: parts[2], Query: parts[3]}
	if len(parts) == 5 {
		route.Status = parts[4]
	}
	return route, true
}

func updateSummaryReviewHeaderLine(color bool) string {
	return "  " + strings.Join([]string{
		textui.StyleHeading(textui.PadRight(tr("action", "操作"), 24), color),
		textui.StyleHeading(tr("description", "説明"), color),
	}, " ")
}

func updateSummaryReviewActionLines(manualPlan inventoryPlanReport, backendPlan backendPlanReport, color bool) []updateSummaryLine {
	return updateSummaryReviewActionLinesWithLoading(manualPlan, false, backendPlan, false, color)
}

func updateSummaryReviewActionLinesWithLoading(manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, color bool) []updateSummaryLine {
	lines := []updateSummaryLine{}
	if manualLoading {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("manual review", "手動アプリ確認"), tr("loading - preparing manual/vendor app candidates", "loading - 手動/vendor app 候補を準備中"), color),
			Action:          updateHubActionManualPlan,
			Label:           tr("open manual review", "手動アプリ確認を開く"),
			HideInlineBadge: true,
		})
	} else if manualPlan.AttentionCount > 0 {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("manual review", "手動アプリ確認"), fmt.Sprintf("%s - %s", manualPlanStatus(manualPlan), updateDashboardManualPlanSummary(manualPlan)), color),
			Action:          updateHubActionManualPlan,
			Label:           tr("open manual review", "手動アプリ確認を開く"),
			HideInlineBadge: true,
		})
	}
	if backendLoading {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("backend convergence", "backend 整理"), tr("loading - preparing backend evidence", "loading - backend evidence を準備中"), color),
			Action:          updateHubActionBackends,
			Label:           tr("open backend convergence", "backend 整理を開く"),
			HideInlineBadge: true,
		})
	} else if len(backendPlan.Findings) > 0 {
		lines = append(lines, updateSummaryLine{
			Text:            updateSummaryReviewRowLine(tr("backend convergence", "backend 整理"), fmt.Sprintf("%s - %s", backendPlan.Status, updateDashboardBackendSummary(backendPlan)), color),
			Action:          updateHubActionBackends,
			Label:           tr("open backend convergence", "backend 整理を開く"),
			HideInlineBadge: true,
		})
	}
	return lines
}

func updateSummaryReviewRowLine(name string, description string, color bool) string {
	return strings.Join([]string{
		textui.PadRight(name, 24),
		description,
	}, " ")
}

func isUpdateSummaryTableDataLine(line string) bool {
	if line == "" {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	first := strings.ToLower(fields[0])
	switch first {
	case "name", "名前", "provider", "decision", "status", "状態", "type", "種別", "missing", "extra":
		return false
	}
	return len(fields) >= 2
}

func isUpdateSummaryTableHeaderLine(line string) bool {
	if line == "" {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "name", "名前", "provider", "decision", "status", "状態", "type", "種別", "missing", "extra", "action", "操作":
		return len(fields) >= 2
	default:
		return false
	}
}

func browserSectionHeadingText(text string, color bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return textui.StyleSection(trimmed, color)
}

type updateHubRouterResult struct {
	Action       string
	ManualPlan   inventoryPlanReport
	ManualReady  bool
	BackendPlan  backendPlanReport
	BackendReady bool
}

type updateHubPlanBuilders struct {
	Manual  func(context.Context, string) inventoryPlanReport
	Backend func(context.Context, string) backendPlanReport
}

type updateHubManualPlanMsg struct {
	Report inventoryPlanReport
}

type updateHubBackendPlanMsg struct {
	Report backendPlanReport
}

type updateHubFilterAction struct {
	Section string
	Facet   string
	Value   string
}

type updateHubQueryAction struct {
	Section string
}

type updateHubRouterScreen string

const (
	updateHubRouterDashboard updateHubRouterScreen = "dashboard"
	updateHubRouterDetail    updateHubRouterScreen = "detail"
	updateHubRouterTable     updateHubRouterScreen = "table"
	updateHubRouterInput     updateHubRouterScreen = "input"
	updateHubRouterConfirm   updateHubRouterScreen = "confirm"
)

type updateHubRouterModel struct {
	ctx            context.Context
	planBuilders   updateHubPlanBuilders
	report         updateReport
	manualPlan     inventoryPlanReport
	manualLoading  bool
	backendPlan    backendPlanReport
	backendLoading bool
	defaultAction  string
	detailStates   map[string]detailBrowserState
	color          bool
	width          int
	height         int

	screen       updateHubRouterScreen
	stateKey     string
	returnAction string
	finalAction  string
	writeFlow    reviewui.WriteFlow

	dashboard updateSummaryBrowserModel
	detail    detailBrowserModel
	table     toolTableBrowserModel
	input     textInputBrowserModel
	confirm   confirmBrowserModel
}

func runUpdateHubRouter(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) (updateHubRouterResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newUpdateHubRouterModelWithContext(ctx, defaultUpdateHubPlanBuilders(), report, manualPlan, manualLoading, backendPlan, backendLoading, preferredAction, defaultAction, color)
	final, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil {
		return updateHubRouterResult{}, err
	}
	if result, ok := final.(updateHubRouterModel); ok {
		return updateHubRouterResult{
			Action:       result.finalAction,
			ManualPlan:   result.manualPlan,
			ManualReady:  !result.manualLoading,
			BackendPlan:  result.backendPlan,
			BackendReady: !result.backendLoading,
		}, nil
	}
	return updateHubRouterResult{}, nil
}

func newUpdateHubRouterModel(report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) updateHubRouterModel {
	return newUpdateHubRouterModelWithContext(context.Background(), defaultUpdateHubPlanBuilders(), report, manualPlan, manualLoading, backendPlan, backendLoading, preferredAction, defaultAction, color)
}

func newUpdateHubRouterModelWithContext(ctx context.Context, builders updateHubPlanBuilders, report updateReport, manualPlan inventoryPlanReport, manualLoading bool, backendPlan backendPlanReport, backendLoading bool, preferredAction string, defaultAction string, color bool) updateHubRouterModel {
	if ctx == nil {
		ctx = context.Background()
	}
	defaultBuilders := defaultUpdateHubPlanBuilders()
	if builders.Manual == nil {
		builders.Manual = defaultBuilders.Manual
	}
	if builders.Backend == nil {
		builders.Backend = defaultBuilders.Backend
	}
	model := updateHubRouterModel{
		ctx:            ctx,
		planBuilders:   builders,
		report:         report,
		manualPlan:     manualPlan,
		manualLoading:  manualLoading,
		backendPlan:    backendPlan,
		backendLoading: backendLoading,
		defaultAction:  defaultAction,
		detailStates:   reviewui.EnsureStateCache(nil),
		color:          color,
	}
	action := initialUpdateHubAction(preferredAction, defaultAction)
	if action == "" {
		action = updateHubActionDashboard
	}
	model.showAction(action, updateHubActionDashboard)
	return model
}

func defaultUpdateHubPlanBuilders() updateHubPlanBuilders {
	return updateHubPlanBuilders{
		Manual:  buildUpdateHubManualPlanWithContext,
		Backend: buildUpdateHubBackendPlanWithContext,
	}
}

func buildUpdateHubManualPlanWithContext(ctx context.Context, root string) inventoryPlanReport {
	select {
	case <-ctx.Done():
		return canceledUpdateHubManualPlan(root)
	default:
	}
	return buildInventoryPlanReport(inventoryPlanOptions{root: root, provider: manualProviderName})
}

func buildUpdateHubBackendPlanWithContext(ctx context.Context, root string) backendPlanReport {
	select {
	case <-ctx.Done():
		return canceledUpdateHubBackendPlan(root)
	default:
	}
	return buildBackendPlanReport(ctx, backendOptions{command: "plan", root: root})
}

func canceledUpdateHubManualPlan(root string) inventoryPlanReport {
	return inventoryPlanReport{
		SchemaVersion:  1,
		Status:         plan.StatusHeld,
		Root:           root,
		Provider:       manualProviderName,
		ActionCounts:   map[string]int{},
		AttentionCount: 0,
		NextSteps: []string{
			tr("manual review loading was canceled before completion", "手動アプリ確認の準備は完了前にキャンセルされました"),
		},
	}
}

func canceledUpdateHubBackendPlan(root string) backendPlanReport {
	return backendPlanReport{
		SchemaVersion: backendPlanReportSchemaVersion,
		Status:        plan.StatusHeld,
		Command:       "plan",
		Root:          root,
		Warnings: []string{
			tr("backend evidence loading was canceled before completion", "backend evidence の準備は完了前にキャンセルされました"),
		},
	}
}

func (m updateHubRouterModel) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.manualLoading {
		root := m.report.Root
		ctx := m.ctx
		buildManual := m.planBuilders.Manual
		cmds = append(cmds, func() tea.Msg {
			return updateHubManualPlanMsg{Report: buildManual(ctx, root)}
		})
	}
	if m.backendLoading {
		root := m.report.Root
		ctx := m.ctx
		buildBackend := m.planBuilders.Backend
		cmds = append(cmds, func() tea.Msg {
			return updateHubBackendPlanMsg{Report: buildBackend(ctx, root)}
		})
	}
	return tea.Batch(cmds...)
}

func (m updateHubRouterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateHubManualPlanMsg:
		m.manualPlan = msg.Report
		m.manualLoading = false
		m.refreshCurrentScreen()
		return m, nil
	case updateHubBackendPlanMsg:
		m.backendPlan = msg.Report
		m.backendLoading = false
		m.refreshCurrentScreen()
		return m, nil
	}
	switch m.screen {
	case updateHubRouterDashboard:
		updated, _ := m.dashboard.Update(msg)
		if dashboard, ok := updated.(updateSummaryBrowserModel); ok {
			action := reviewui.TakeActionAndRemember(m.detailStates, m.stateKey, &dashboard.State)
			m.dashboard = dashboard
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterDetail:
		updated, _ := m.detail.Update(msg)
		if detail, ok := updated.(detailBrowserModel); ok {
			action := reviewui.TakeActionAndRemember(m.detailStates, m.stateKey, &detail.State)
			m.detail = detail
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterTable:
		updated, _ := m.table.Update(msg)
		if table, ok := updated.(toolTableBrowserModel); ok {
			action := reviewui.TakeActionAndRemember(m.detailStates, m.stateKey, &table.State)
			m.table = table
			if action != "" {
				return m.handleAction(action)
			}
		}
	case updateHubRouterInput:
		updated, _ := m.input.Update(msg)
		if input, ok := updated.(textInputBrowserModel); ok {
			m.input = input
			if input.Action != "" {
				return m.handleInputAction(input)
			}
		}
	case updateHubRouterConfirm:
		updated, _ := m.confirm.Update(msg)
		if confirm, ok := updated.(confirmBrowserModel); ok {
			m.confirm = confirm
			if confirm.Action != "" {
				return m.handleConfirmAction(confirm)
			}
		}
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}
	return m, nil
}

func (m *updateHubRouterModel) refreshCurrentScreen() {
	if strings.HasPrefix(m.stateKey, "route:") {
		return
	}
	if route, ok := parseUpdateSummaryRouteStateKey(m.stateKey); ok {
		m.showUpdateSummaryRoute(route)
		return
	}
	switch m.stateKey {
	case "dashboard":
		m.showDashboard(updateHubActionDashboard)
	case "inventory-all":
		m.showAction(updateHubActionInventoryAll, m.returnAction)
	case "inventory-details":
		m.showAction(updateHubActionInventoryDetails, m.returnAction)
	case listHubActionManual:
		m.showAction(listHubActionManual, m.returnAction)
	case "manual-plan":
		m.showAction(updateHubActionManualPlan, m.returnAction)
	case "backends":
		m.showAction(updateHubActionBackends, m.returnAction)
	}
}

func (m updateHubRouterModel) View() tea.View {
	switch m.screen {
	case updateHubRouterDashboard:
		return m.dashboard.View()
	case updateHubRouterDetail:
		return m.detail.View()
	case updateHubRouterTable:
		return m.table.View()
	case updateHubRouterInput:
		return m.input.View()
	case updateHubRouterConfirm:
		return m.confirm.View()
	default:
		view := tea.NewView("")
		view.AltScreen = true
		return view
	}
}

func (m updateHubRouterModel) handleAction(action string) (tea.Model, tea.Cmd) {
	switch {
	case action == updevActionExit:
		m.finalAction = action
		return m, tea.Quit
	case action == updevActionBack:
		if m.screen == updateHubRouterDashboard {
			m.finalAction = updevActionExit
			return m, tea.Quit
		}
		if m.returnAction == updateHubActionDashboard {
			m.showDashboard("")
			m.dashboard.TopAnchor = true
			m.dashboard.State.Offset = 0
			reviewui.RememberState(m.detailStates, m.stateKey, m.dashboard.State)
			return m, nil
		}
		if strings.HasPrefix(m.stateKey, "filter-result:") && m.returnAction != "" {
			m.showReturnAction(m.returnAction)
			return m, nil
		}
		m.showReturnAction(m.returnAction)
		return m, nil
	case action == updevActionHome:
		m.showDashboard(updateHubActionDashboard)
		return m, nil
	}
	if _, ok := routedDetailWriteActionSpec(action); ok {
		m.showWriteAction(action)
		return m, nil
	}
	if updateHubExternalAction(action) {
		m.finalAction = action
		return m, tea.Quit
	}
	if route, ok := parseUpdateSummaryRoute(action); ok {
		m.defaultAction = route.Base
		returnAction := updateHubActionDashboard
		if m.screen != updateHubRouterDashboard {
			returnAction = m.currentAction()
		}
		m.showUpdateSummaryRouteWithReturn(route, returnAction)
		return m, nil
	}
	if filter, ok := parseUpdateHubFilterAction(action); ok {
		m.showUpdateFilterResult(filter)
		return m, nil
	}
	if query, ok := parseUpdateHubQueryAction(action); ok {
		m.showUpdateQueryInput(query.Section)
		return m, nil
	}
	if route, ok := parseListRouteAction(action); ok {
		m.showListRouteDetail(route)
		return m, nil
	}
	if action == listHubActionManual {
		m.showAction(listHubActionManual, updateHubActionInventoryAll)
		return m, nil
	}
	if routed := updateHubActionFromListAction(action); routed != "" {
		m.showAction(routed, updateHubActionDashboard)
		return m, nil
	}
	if updateHubActionExists(action) {
		m.defaultAction = action
		m.showAction(action, updateHubActionDashboard)
		return m, nil
	}
	return m, nil
}

func updateHubExternalAction(action string) bool {
	if action == updateHubActionInventoryAttention || action == updateHubActionJSON {
		return true
	}
	if detailAction, _, ok := parseManualPlanDetailAction(action); ok && (detailAction == "edit" || !manualPlanDetailActionRequiresConfirmation(detailAction)) {
		return true
	}
	if detailAction, _, _, ok := parseBackendDetailAction(action); ok && !backendDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	if detailAction, _, _, _, ok := parseSecurityDetailAction(action); ok && !securityDetailActionRequiresConfirmation(detailAction) {
		return true
	}
	return false
}

func (m *updateHubRouterModel) showAction(action string, returnAction string) {
	if action == "" {
		action = updateHubActionDashboard
	}
	switch action {
	case updateHubActionDashboard:
		m.showDashboard(updateHubActionDashboard)
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: m.report.Inventory}, listOptions{})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		m.showListFiltered("updev installed inventory", inventory, "inventory-all", returnAction, listHubActionManual, listHubActionManual)
	case listHubActionManual:
		inventory := buildListReport(inventoryResult{Report: m.report.Inventory}, listOptions{})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		manualReport := derivedListReport(inventory, listOptions{provider: manualProviderName})
		m.showListFiltered("updev list manual", manualReport, listHubActionManual, updateHubActionInventoryAll, updateHubActionInventoryAll, updateHubActionInventoryAll)
	case updateHubActionInventoryDetails:
		m.showDetail("updev inventory details", updateInventoryDetailRowsWithBackend(m.report, m.backendPlan), "inventory-details", returnAction)
	case updateHubActionUpdatesFilter:
		m.showUpdateFilterMenu(updateHubActionUpdatesFilter, returnAction)
	case updateHubActionSecurityFilter:
		m.showUpdateFilterMenu(updateHubActionSecurityFilter, returnAction)
	case updateHubActionManualPlan:
		m.showTable("updev manual review plan", manualPlanToolSections(m.manualPlan), "manual-plan", returnAction)
	case updateHubActionBackends:
		m.showTable("updev backend convergence", backendToolSections(m.backendPlan), "backends", returnAction)
	case updateHubActionSecurity:
		m.showDetail("updev security details", updateSecurityDetailRows(m.report), "security", returnAction)
	case updateHubActionLogs:
		m.showDetail("updev update logs", updateLogDetailRows(m.report), "logs", returnAction)
	case updateHubActionFull:
		m.showDetail("updev full report", updateFullReportRows(m.report), "full", returnAction)
	default:
		m.showDashboard(m.defaultAction)
	}
}

const (
	updateHubFilterActionPrefix = "update-filter"
	updateHubQueryActionPrefix  = "update-query"
)

func updateHubFilterActionValue(section string, facet string, value string) string {
	return strings.Join([]string{updateHubFilterActionPrefix, section, facet, value}, "\t")
}

func updateHubQueryActionValue(section string) string {
	return strings.Join([]string{updateHubQueryActionPrefix, section}, "\t")
}

func parseUpdateHubFilterAction(value string) (updateHubFilterAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 4 || parts[0] != updateHubFilterActionPrefix {
		return updateHubFilterAction{}, false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return updateHubFilterAction{}, false
	}
	return updateHubFilterAction{Section: parts[1], Facet: parts[2], Value: parts[3]}, true
}

func parseUpdateHubQueryAction(value string) (updateHubQueryAction, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 2 || parts[0] != updateHubQueryActionPrefix || strings.TrimSpace(parts[1]) == "" {
		return updateHubQueryAction{}, false
	}
	return updateHubQueryAction{Section: parts[1]}, true
}

func (m *updateHubRouterModel) showUpdateFilterMenu(action string, returnAction string) {
	title := "updev update filter"
	stateKey := "filter-menu:updates"
	rows := updateFilterRows("updates", updateFilterActionProvider, updateStepProviderCounts(m.report.Steps))
	rows = append(rows, updateFilterRows("updates", updateFilterActionStatus, updateStepStatusCounts(m.report.Steps))...)
	if action == updateHubActionSecurityFilter {
		title = "updev security filter"
		stateKey = "filter-menu:security"
		rows = updateFilterRows("security", updateFilterActionProvider, safetyProviderCounts(m.report.Safety))
		rows = append(rows, updateFilterRows("security", updateFilterActionDecision, safetyDecisionCounts(m.report.Safety))...)
	}
	section := "updates"
	if action == updateHubActionSecurityFilter {
		section = "security"
	}
	rows = append(rows, updateQueryFilterRow(section))
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no filter values", "filter 値がありません"),
			Detail:  tr("The selected report section has no available filter values.", "選択した report section に利用可能な filter 値がありません。"),
		}}
	}
	m.showDetail(title, rows, stateKey, returnAction)
	m.detail.PrimaryEnterAction = true
}

func updateFilterRows(section string, facet string, counts map[string]int) []detailBrowserRow {
	rows := make([]detailBrowserRow, 0, len(counts))
	for _, value := range sortedMapKeys(counts) {
		rows = append(rows, detailBrowserRow{
			Title:   facet + ": " + value,
			Status:  value,
			Summary: fmt.Sprintf("%d rows", counts[value]),
			Detail:  tr("Open filtered update evidence for this value.", "この値で絞り込んだ update evidence を開きます。"),
			Actions: []detailBrowserAction{{
				Value:       updateHubFilterActionValue(section, facet, value),
				Label:       tr("open filter", "filter を開く"),
				Description: tr("show filtered evidence", "絞り込んだ evidence を表示します"),
			}},
		})
	}
	return rows
}

func updateQueryFilterRow(section string) detailBrowserRow {
	return detailBrowserRow{
		Title:   tr("query search", "query 検索"),
		Status:  "query",
		Summary: tr("search by text", "文字列で検索"),
		Detail:  tr("Search the selected evidence with a free text query.", "選択した evidence を自由入力の query で検索します。"),
		Actions: []detailBrowserAction{{
			Value:       updateHubQueryActionValue(section),
			Label:       tr("type query", "query を入力"),
			Description: tr("open query input", "query 入力を開きます"),
		}},
	}
}

func (m *updateHubRouterModel) showUpdateFilterResult(filter updateHubFilterAction) {
	opts := lastReportOptions{}
	switch filter.Section {
	case "security":
		opts.section = "security"
	default:
		opts.section = "updates"
	}
	switch filter.Facet {
	case updateFilterActionProvider:
		opts.provider = filter.Value
	case updateFilterActionStatus, updateFilterActionDecision:
		opts.status = filter.Value
	case updateFilterActionQuery:
		opts.query = filter.Value
	}
	filtered := filterUpdateReport(m.report, opts)
	stateKey := "filter-result:" + filter.Section + ":" + filter.Facet + ":" + filter.Value
	title := "updev update filter: " + filter.Value
	returnAction := updateHubActionUpdatesFilter
	rows := updateLogDetailRows(filtered)
	if filter.Section == "security" {
		title = "updev security filter: " + filter.Value
		returnAction = updateHubActionSecurityFilter
		rows = updateSecurityDetailRowsForFilter(filtered, opts)
	}
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *updateHubRouterModel) showUpdateQueryInput(section string) {
	title := "updev update query"
	description := tr("Search update commands, reasons, stdout, and stderr. Empty input returns to the filter menu.", "update command / reason / stdout / stderr を検索します。空入力なら filter menu に戻ります。")
	returnAction := updateHubActionUpdatesFilter
	if section == "security" {
		title = "updev security query"
		description = tr("Search security reasons, evidence, remediation, advisory IDs, and URLs. Empty input returns to the filter menu.", "security reason / evidence / remediation / advisory ID / URL を検索します。空入力なら filter menu に戻ります。")
		returnAction = updateHubActionSecurityFilter
	}
	m.screen = updateHubRouterInput
	m.stateKey = "query-input:" + section
	m.returnAction = returnAction
	m.input = newTextInputBrowserModel(title, description, "brew, hold, provenance, ...", "", m.color)
}

func (m updateHubRouterModel) handleInputAction(input textInputBrowserModel) (tea.Model, tea.Cmd) {
	switch input.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		if reviewui.IsWriteStateKey(m.stateKey) {
			m.showReturnAction(m.writeFlow.ReturnAction)
			return m, nil
		}
		m.showReturnAction(m.returnAction)
		return m, nil
	case "submit":
		if reviewui.IsWriteReasonStateKey(m.stateKey) {
			if !m.writeFlow.AcceptReason(input.Value) {
				m.showReturnAction(m.writeFlow.ReturnAction)
				return m, nil
			}
			m.showWriteExpiryInput()
			return m, nil
		}
		if reviewui.IsWriteExpiryStateKey(m.stateKey) {
			if !m.writeFlow.AcceptExpiry(input.Value, time.Now(), validateSecurityPolicyAllowExpiry) {
				m.showReturnAction(m.writeFlow.ReturnAction)
				return m, nil
			}
			m.showWriteConfirm()
			return m, nil
		}
		section := strings.TrimPrefix(m.stateKey, "query-input:")
		query := strings.TrimSpace(input.Value)
		if query == "" {
			m.showReturnAction(m.returnAction)
			return m, nil
		}
		m.showUpdateFilterResult(updateHubFilterAction{Section: section, Facet: updateFilterActionQuery, Value: query})
		return m, nil
	default:
		return m, nil
	}
}

func (m *updateHubRouterModel) showWriteAction(action string) {
	spec, ok := routedDetailWriteActionSpec(action)
	if !ok {
		return
	}
	m.writeFlow = reviewui.NewWriteFlow(action, m.currentAction(), updateHubActionDashboard, spec)
	if spec.NeedsReason {
		m.showWriteReasonInput(spec)
		return
	}
	m.showWriteConfirm()
}

func (m *updateHubRouterModel) showWriteReasonInput(spec detailWriteActionSpec) {
	model := newTextInputBrowserModel(spec.Title, spec.Description, spec.DefaultReason, spec.DefaultReason, m.color)
	model.Label = tr("reason:", "reason:")
	m.screen = updateHubRouterInput
	m.stateKey = reviewui.WriteReasonStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.input = model
}

func (m *updateHubRouterModel) showWriteExpiryInput() {
	_, _, _, _, ok := parseSecurityDetailAction(m.writeFlow.Action)
	if !ok {
		m.showWriteConfirm()
		return
	}
	defaultExpiry := m.writeFlow.DefaultExpiry(time.Now())
	model := newTextInputBrowserModel(
		tr("security allow expiry", "security allow 期限"),
		tr("Enter the YYYY-MM-DD expiry for this temporary allow rule.", "一時 allow rule の期限を YYYY-MM-DD で入力します。"),
		defaultExpiry,
		defaultExpiry,
		m.color,
	)
	model.Label = tr("expires:", "expires:")
	m.screen = updateHubRouterInput
	m.stateKey = reviewui.WriteExpiryStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.input = model
}

func (m *updateHubRouterModel) showWriteConfirm() {
	spec, ok := routedDetailWriteActionSpec(m.writeFlow.Action)
	if !ok {
		m.showReturnAction(m.writeFlow.ReturnAction)
		return
	}
	spec.Description = m.writeFlow.ConfirmDescription(spec, tr("expires: ", "期限: "), tr("reason: ", "理由: "))
	m.screen = updateHubRouterConfirm
	m.stateKey = reviewui.WriteConfirmStateKey(m.writeFlow.Action)
	m.returnAction = m.writeFlow.ReturnAction
	m.confirm = newConfirmBrowserModel(spec.Title, spec.Prompt, spec.Description, m.color)
}

func (m updateHubRouterModel) handleConfirmAction(confirm confirmBrowserModel) (tea.Model, tea.Cmd) {
	switch confirm.Action {
	case updevActionExit:
		m.finalAction = updevActionExit
		return m, tea.Quit
	case updevActionBack:
		m.showReturnAction(m.writeFlow.ReturnAction)
		return m, nil
	case "apply":
		_ = applyRoutedDetailWriteAction(m.report.Root, &m.report, m.writeFlow.Action, m.writeFlow.Reason, m.writeFlow.Expires)
		m.refreshPlansAfterWriteAction()
		m.showReturnAction(m.writeFlow.ReturnAction)
		return m, nil
	default:
		return m, nil
	}
}

func (m *updateHubRouterModel) refreshPlansAfterWriteAction() {
	if action, _, ok := parseManualPlanDetailAction(m.writeFlow.Action); ok && manualPlanDetailActionRequiresConfirmation(action) {
		m.manualPlan = buildInventoryPlanForHub(m.report.Root)
		m.manualLoading = false
	}
	if action, _, _, ok := parseBackendDetailAction(m.writeFlow.Action); ok && backendDetailActionRequiresConfirmation(action) {
		m.backendPlan = buildBackendPlanForHub(m.report.Root)
		m.backendLoading = false
	}
	if action, _, _, _, ok := parseSecurityDetailAction(m.writeFlow.Action); ok && securityDetailActionRequiresConfirmation(action) {
		m.report.Report = saveLastUpdateReport(m.report)
	}
}

func (m *updateHubRouterModel) showDashboard(focusAction string) {
	stateKey := "dashboard"
	state, hasState := m.detailStates[stateKey]
	if !hasState && (focusAction == "" || focusAction == updateHubActionDashboard) {
		focusAction = m.initialDashboardFocusAction()
	} else if hasState && focusAction == updateHubActionDashboard {
		state.Offset = 0
		state.Action = ""
		focusAction = m.initialDashboardFocusAction()
	}
	model := newUpdateSummaryBrowserModel(updateSummaryBrowserOptions{
		Title:          updateHubTitle(m.report),
		Report:         m.report,
		ManualPlan:     m.manualPlan,
		ManualLoading:  m.manualLoading,
		BackendPlan:    m.backendPlan,
		BackendLoading: m.backendLoading,
		State:          state,
		FocusAction:    focusAction,
		Color:          m.color,
	})
	m.applyDashboardSize(&model)
	m.screen = updateHubRouterDashboard
	m.stateKey = stateKey
	m.returnAction = updateHubActionDashboard
	m.dashboard = model
}

func (m updateHubRouterModel) initialDashboardFocusAction() string {
	return updateHubActionLogs
}

func (m *updateHubRouterModel) showUpdateSummaryRoute(route updateSummaryRoute) {
	m.showUpdateSummaryRouteWithReturn(route, updateHubActionDashboard)
}

func (m *updateHubRouterModel) showUpdateSummaryRouteWithReturn(route updateSummaryRoute, returnAction string) {
	opts := lastReportOptions{provider: route.Provider, status: route.Status, query: route.Query}
	filtered := filterUpdateReport(m.report, opts)
	suffix := updateSummaryRouteTitleSuffix(route)
	stateKey := updateSummaryRouteStateKey(route)
	switch route.Base {
	case updateHubActionLogs:
		m.showFocusedDetail("updev update logs"+suffix, updateLogDetailRows(filtered), stateKey, returnAction)
	case updateHubActionSecurity:
		m.showFocusedDetail("updev security details"+suffix, updateSecurityDetailRowsForFilter(filtered, opts), stateKey, returnAction)
	case updateHubActionInventoryAll:
		inventory := buildListReport(inventoryResult{Report: filtered.Inventory}, listOptions{provider: route.Provider, status: route.Status, query: route.Query})
		inventory.Evidence = addBackendListEvidence(inventory.Evidence, m.backendPlan)
		m.showListFiltered("updev installed inventory"+suffix, inventory, stateKey, returnAction, listHubActionManual, listHubActionManual)
	case updateHubActionInventoryDetails:
		m.showFocusedDetail("updev inventory details"+suffix, updateInventoryDetailRowsWithBackend(filtered, m.backendPlan), stateKey, returnAction)
	default:
		m.showDashboard(route.Base)
	}
}

func (m *updateHubRouterModel) showReturnAction(action string) {
	if route, ok := parseUpdateSummaryRouteStateKey(action); ok {
		m.showUpdateSummaryRoute(route)
		return
	}
	m.showAction(action, updateHubActionDashboard)
}

func updateSummaryRouteStateKey(route updateSummaryRoute) string {
	return "summary:" + route.Encode()
}

func parseUpdateSummaryRouteStateKey(stateKey string) (updateSummaryRoute, bool) {
	encoded, ok := strings.CutPrefix(stateKey, "summary:")
	if !ok {
		return updateSummaryRoute{}, false
	}
	return parseUpdateSummaryRoute(encoded)
}

func (m *updateHubRouterModel) showListRouteDetail(route listRouteAction) {
	stateKey := "route:" + route.Domain + ":" + route.Provider + ":" + route.Kind + ":" + route.Name
	rows := m.listRouteRows(route)
	if len(rows) == 0 {
		rows = []detailBrowserRow{emptyRouteDetailRow(route)}
	}
	m.showFocusedDetail(routeDetailTitle(route), rows, stateKey, m.currentAction())
}

func (m updateHubRouterModel) listRouteRows(route listRouteAction) []detailBrowserRow {
	switch route.Domain {
	case listHubActionManual:
		manualPlan := buildInventoryPlanReport(inventoryPlanOptions{root: m.report.Root, provider: manualProviderName, query: route.Name})
		return manualPlanDetailRows(manualPlan)
	case listHubActionBackends:
		return backendDetailRowsForListRoute(m.backendPlan, route)
	case listHubActionUpdates:
		filtered := filterUpdateReport(m.report, lastReportOptions{section: "logs", provider: route.Provider, query: route.Name})
		return updateLogDetailRows(filtered)
	case listHubActionSecurity:
		opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Name}
		filtered := filterUpdateReport(m.report, opts)
		return updateSecurityDetailRowsForFilter(filtered, opts)
	default:
		return nil
	}
}

func (m *updateHubRouterModel) showListFiltered(title string, report listReport, stateKey string, returnAction string, nextAction string, previousAction string) {
	title = listTitleWithEvidenceSummary(title, report)
	sections := listTableSections(report)
	if toolTableRowCount(sections) > 0 || nextAction != "" || previousAction != "" {
		actions := tableBrowserActions()
		labels := tableBrowserLabels()
		if nextAction != "" || previousAction != "" {
			actions = tableBrowserActionsWithViewToggle(nextAction, previousAction)
			labels = tableBrowserLabelsWithViewToggle()
		}
		m.showTableWithActions(title, sections, stateKey, returnAction, actions, labels)
		return
	}
	rows := listDetailRows(report)
	if len(rows) == 0 {
		rows = []detailBrowserRow{{
			Title:   title,
			Status:  string(plan.StatusOK),
			Summary: tr("no matching rows", "該当する行はありません"),
			Detail:  tr("The selected inventory filter has no rows.", "選択した inventory filter に一致する行はありません。"),
		}}
	}
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *updateHubRouterModel) showDetail(title string, rows []detailBrowserRow, stateKey string, returnAction string) {
	model := newDetailBrowserModel(title, rows, reviewui.CachedState(m.detailStates, stateKey), m.color)
	m.applyDetailSize(&model)
	m.screen = updateHubRouterDetail
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.detail = model
}

func (m *updateHubRouterModel) showFocusedDetail(title string, rows []detailBrowserRow, stateKey string, returnAction string) {
	m.detailStates[stateKey] = focusedRouteDetailState()
	m.showDetail(title, rows, stateKey, returnAction)
}

func (m *updateHubRouterModel) showTable(title string, sections []toolSection, stateKey string, returnAction string) {
	m.showTableWithActions(title, sections, stateKey, returnAction, tableBrowserActions(), tableBrowserLabels())
}

func (m *updateHubRouterModel) showTableWithActions(title string, sections []toolSection, stateKey string, returnAction string, actions reviewui.BrowserActions, labels reviewui.TableBrowserLabels) {
	title = m.loadingTitle(title, stateKey)
	model := newToolTableBrowserModelWithActions(title, sections, reviewui.CachedState(m.detailStates, stateKey), actions, labels, m.color)
	m.applyTableSize(&model)
	m.screen = updateHubRouterTable
	m.stateKey = stateKey
	m.returnAction = returnAction
	m.table = model
}

func (m updateHubRouterModel) loadingTitle(title string, stateKey string) string {
	switch {
	case stateKey == "manual-plan" && m.manualLoading:
		return title + " " + tr("(manual review loading)", "(manual review 準備中)")
	case stateKey == "backends" && m.backendLoading:
		return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
	case (stateKey == "inventory-all" || stateKey == "inventory-details") && m.backendLoading:
		return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
	default:
		if route, ok := parseUpdateSummaryRouteStateKey(stateKey); ok && m.backendLoading && (route.Base == updateHubActionInventoryAll || route.Base == updateHubActionInventoryDetails) {
			return title + " " + tr("(backend evidence loading)", "(backend evidence 準備中)")
		}
		return title
	}
}

func (m updateHubRouterModel) currentAction() string {
	if _, ok := parseUpdateSummaryRouteStateKey(m.stateKey); ok {
		return m.stateKey
	}
	switch m.stateKey {
	case "inventory-all":
		return updateHubActionInventoryAll
	case "inventory-details":
		return updateHubActionInventoryDetails
	case "manual-plan":
		return updateHubActionManualPlan
	case "backends":
		return updateHubActionBackends
	case "security":
		return updateHubActionSecurity
	case "logs":
		return updateHubActionLogs
	case "full":
		return updateHubActionFull
	default:
		if m.returnAction != "" {
			return m.returnAction
		}
		return updateHubActionDashboard
	}
}

func (m updateHubRouterModel) applyDashboardSize(model *updateSummaryBrowserModel) {
	model.Width = m.width
	model.Height = m.height
	if model.TopAnchor {
		model.State.Offset = 0
		return
	}
	model.EnsureSelectedVisible()
}

func (m updateHubRouterModel) applyDetailSize(model *detailBrowserModel) {
	model.Width = m.width
	model.Height = m.height
	model.EnsureSelectedVisible()
}

func (m updateHubRouterModel) applyTableSize(model *toolTableBrowserModel) {
	model.Width = m.width
	model.Height = m.height
}

func updateHubDefaultAction(manualPlan inventoryPlanReport, backendPlan backendPlanReport, preferredAction string, report updateReport) string {
	defaultAction := updateHubActionInventoryAll
	if manualPlan.AttentionCount > 0 {
		defaultAction = updateHubActionManualPlan
	} else if len(backendPlan.Findings) > 0 {
		defaultAction = updateHubActionBackends
	}
	if updateHubActionExists(preferredAction) {
		defaultAction = preferredAction
	} else if updateHubActionAvailable(preferredAction, updateHubChoices(report, manualPlan, backendPlan, defaultAction)) {
		defaultAction = preferredAction
	}
	return defaultAction
}

func handleUpdateHubExternalAction(report *updateReport, manualPlan *inventoryPlanReport, backendPlan *backendPlanReport, action string) (string, bool) {
	switch {
	case action == "" || action == updevActionExit:
		return "", true
	case action == updateHubActionInventoryAttention:
		printLastInventorySection(os.Stdout, *report, lastReportOptions{section: "inventory", status: "attention", details: false}, textui.ColorEnabled())
		return updateHubActionDashboard, false
	case action == updateHubActionUpdatesFilter:
		opts, ok := selectUpdateStepFilter(*report)
		if !ok {
			return updateHubActionDashboard, false
		}
		filtered := filterUpdateReport(*report, opts)
		state, err := runDetailBrowserWithState("updev update steps", updateLogDetailRows(filtered), detailBrowserState{}, textui.ColorEnabled())
		if err != nil {
			fmt.Fprintf(os.Stderr, "updev update steps: %v\n", err)
			return updateHubActionDashboard, false
		}
		if state.Action == updevActionExit {
			return "", true
		}
		return updateHubActionDashboard, false
	case action == updateHubActionSecurityFilter:
		opts, ok := selectUpdateSecurityFilter(*report)
		if !ok {
			return updateHubActionDashboard, false
		}
		filtered := filterUpdateReport(*report, opts)
		state, err := runDetailBrowserWithState("updev security filter", updateSecurityDetailRowsForFilter(filtered, opts), detailBrowserState{}, textui.ColorEnabled())
		if err != nil {
			fmt.Fprintf(os.Stderr, "updev security filter: %v\n", err)
			return updateHubActionDashboard, false
		}
		if state.Action == updevActionExit {
			return "", true
		}
		return updateHubActionDashboard, false
	case action == updateHubActionJSON:
		entry := updateReportCacheEntry{Version: 1, Type: "update", CreatedAt: time.Now(), Report: *report}
		if cached, ok := loadLastUpdateReport(); ok {
			entry = cached
		}
		_ = encodeJSON(buildUpdateReportSectionView(entry, lastReportOptions{section: "full"}))
		return "", true
	}
	if handleManualPlanDetailAction(report.Root, action) {
		*manualPlan = buildInventoryPlanForHub(report.Root)
		return updateHubActionManualPlan, false
	}
	if handleBackendDetailAction(report.Root, action) {
		*backendPlan = buildBackendPlanForHub(report.Root)
		return updateHubActionBackends, false
	}
	if handleMiseBumpDetailAction(report, action) {
		return updateHubActionSecurity, false
	}
	if handleSecurityDetailAction(report, action) {
		return updateHubActionSecurity, false
	}
	return updateHubActionDashboard, false
}
