package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/packageapply"
	"github.com/webkaz-labs/updev/internal/packageexecutor"
	"github.com/webkaz-labs/updev/internal/packageparity"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevconfig"
)

type applyOptions struct {
	target   string
	format   string
	root     string
	dryRun   bool
	safeOnly bool
	policy   string
}

type brewfileApplyReport struct {
	Status     plan.Status              `json:"status"`
	Root       string                   `json:"root"`
	DryRun     bool                     `json:"dry_run"`
	SafeOnly   bool                     `json:"safe_only"`
	Policy     *securityPolicyUse       `json:"policy,omitempty"`
	Candidates []brewfileApplyCandidate `json:"candidates"`
	Summary    brewfileApplySummary     `json:"summary"`
	Warnings   []string                 `json:"warnings,omitempty"`
}

type brewfileApplySummary struct {
	Candidates int `json:"candidates"`
	Allow      int `json:"allow,omitempty"`
	Review     int `json:"review,omitempty"`
	Hold       int `json:"hold,omitempty"`
	Block      int `json:"block,omitempty"`
	Applied    int `json:"applied,omitempty"`
	Failed     int `json:"failed,omitempty"`
}

type brewfileApplyCandidate struct {
	Identity      string      `json:"identity"`
	Provider      string      `json:"provider"`
	Kind          string      `json:"kind"`
	Name          string      `json:"name"`
	Category      string      `json:"category,omitempty"`
	DesiredSource string      `json:"desired_source"`
	Executor      string      `json:"executor"`
	Status        plan.Status `json:"status"`
	Decision      string      `json:"decision"`
	Reason        string      `json:"reason"`
	ReasonCode    string      `json:"reason_code,omitempty"`
	Remediation   string      `json:"remediation,omitempty"`
	Command       []string    `json:"command,omitempty"`
	Evidence      []string    `json:"evidence,omitempty"`
	Stdout        string      `json:"stdout,omitempty"`
	Stderr        string      `json:"stderr,omitempty"`
}

func parseApplyOptions(args []string) (applyOptions, error) {
	opts := applyOptions{target: "brewfile", format: "text", root: defaultRoot(), policy: securityPolicyPath()}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opts.target = args[0]
		args = args[1:]
	}
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
		case "--policy":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--policy requires a value")
			}
			opts.policy = args[i+1]
			i++
		case "--dry-run", "-n":
			opts.dryRun = true
		case "--safe-only":
			opts.safeOnly = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.target != "brewfile" {
		return opts, fmt.Errorf("unsupported apply target: %s", opts.target)
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runApply(opts applyOptions, commandRunner runner.Runner) int {
	ctx := context.Background()
	policyUse := loadSecurityPolicyForReportPath(opts.policy)
	report := buildBrewfileApplyReport(ctx, opts, commandRunner, policyUse)
	if !opts.dryRun {
		report = applyBrewfileSafeCandidates(ctx, opts, commandRunner, report)
	}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
		return updateExitCode(report.Status)
	}
	printBrewfileApplyText(os.Stdout, report, textui.ColorEnabled())
	return updateExitCode(report.Status)
}

func buildBrewfileApplyReport(ctx context.Context, opts applyOptions, commandRunner runner.Runner, policyUse securityPolicyLoadResult) brewfileApplyReport {
	report := brewfileApplyReport{
		Status:     plan.StatusOK,
		Root:       opts.root,
		DryRun:     opts.dryRun,
		SafeOnly:   opts.safeOnly,
		Policy:     policyUse.View(),
		Candidates: []brewfileApplyCandidate{},
		Warnings:   append([]string(nil), policyUse.Warnings...),
	}
	brewfilePath := packageParityBrewfilePath(opts.root)
	snapshot, err := packageparity.Read(ctx, opts.root, brewfilePath, commandRunner)
	if err != nil {
		report.Status = plan.StatusError
		report.Warnings = append(report.Warnings, "resolved Homebrew desired state failed: "+err.Error())
		return report
	}
	additionalDesired := brew.DesiredFromBootstrapPackages(snapshot.PackageSet, snapshot.Taps)
	missing, err := brewfileApplyMissingDesired(ctx, opts.root, commandRunner, additionalDesired)
	if err != nil {
		report.Status = plan.StatusError
		report.Warnings = append(report.Warnings, "resolved Homebrew desired/live comparison failed: "+err.Error())
		return report
	}
	if len(missing) == 0 {
		return report
	}
	metadataPath := updevconfig.PackageMetadataPath(loadUpdevConfig())
	platform := packageexecutor.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	executorReport, err := buildPackageExecutorReportFromSnapshot(snapshot, metadataPath, platform, localPackageExecutorCapabilities(commandRunner, platform))
	if err != nil {
		report.Status = plan.StatusError
		report.Warnings = append(report.Warnings, "package executor plan failed: "+err.Error())
		return report
	}
	findings, warnings := brewfileApplySafetyFindings(ctx, commandRunner, missing, policyUse.Policy)
	report.Warnings = append(report.Warnings, warnings...)
	candidates := brewfileApplyCandidatesFromFindings(opts.root, missing, findings, packageExecutorItemsByIdentity(executorReport.Items))
	report.Candidates = candidates
	report.Summary = brewfileApplySummaryFromCandidates(candidates)
	report.Status = brewfileApplyStatus(candidates, opts.dryRun)
	return report
}

func brewfileApplyMissingDesired(ctx context.Context, root string, commandRunner runner.Runner, additionalDesired []plan.Item) ([]plan.Item, error) {
	provider := brew.Provider{Root: root, Runner: commandRunner, IncludeVSCode: false, UseHomeDesired: shouldUseHomeBrewfile(root), AdditionalDesired: additionalDesired}
	desired, err := provider.Desired(ctx)
	if err != nil {
		return nil, err
	}
	live, err := provider.Live(ctx)
	if err != nil {
		return nil, err
	}
	liveKeys := map[string]bool{}
	for _, item := range live {
		liveKeys[item.Kind+"\x00"+item.Name] = true
	}
	items := make([]plan.Item, 0, len(desired))
	for _, item := range desired {
		if item.Provider != "brew" {
			continue
		}
		item.Desired = true
		item.Live = liveKeys[item.Kind+"\x00"+item.Name]
		if item.Live {
			item.Status = plan.StatusOK
		} else {
			item.Status = plan.StatusMissing
		}
		items = append(items, item)
	}
	return brewfileApplyMissingItems(items), nil
}

func brewfileApplyMissingItems(items []plan.Item) []plan.Item {
	out := []plan.Item{}
	for _, item := range items {
		if item.Provider != "brew" || item.Status != plan.StatusMissing || !item.Desired || item.Live {
			continue
		}
		switch item.Kind {
		case "brew", "cask", "tap":
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func brewfileApplySafetyFindings(ctx context.Context, commandRunner commandRunner, items []plan.Item, policy securityPolicy) ([]safetyFinding, []string) {
	findings := make([]safetyFinding, 0, len(items))
	minReleaseAge := minHomebrewReleaseAge()
	for _, item := range items {
		finding := brewfileApplyBaseFinding(item)
		switch item.Kind {
		case "tap":
			finding = brewfileApplyTapFinding(finding)
		case "brew", "cask":
			finding = brewfileApplyPackageFinding(ctx, finding, minReleaseAge)
		default:
			finding.Decision = "block"
			setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewMetadataUnavailable, item.Kind, item.Name, "unsupported Brewfile apply kind", nil))
			finding.Remediation = "remove this entry from the Homebrew apply plan"
		}
		findings = append(findings, finding)
	}
	findings, trustWarnings := applyHomebrewTrustStateToBrewFindings(ctx, commandRunner, findings)
	return applySecurityPolicyToSafetyFindings(policy, findings), trustWarnings
}

func brewfileApplyBaseFinding(item plan.Item) safetyFinding {
	entry := brew.ManifestEntry{
		Kind:    item.Kind,
		Name:    brew.NormalizePackageName(item.Kind, item.Name),
		RawName: item.Name,
		Tap:     brew.TapName(item.Kind, item.Name),
	}
	trustKind := item.Kind
	if trustKind == "brew" {
		trustKind = "formula"
	}
	source := "Brewfile desired state"
	evidence := "rendered Brewfile missing desired item"
	if brew.IsMiseBootstrapDesired(item) {
		source = "mise bootstrap package desired state"
		evidence = "resolved mise bootstrap package is missing from Homebrew"
	}
	finding := safetyFinding{
		Provider:    "brew",
		Kind:        item.Kind,
		Name:        entry.Name,
		Decision:    "review",
		Source:      source,
		Tap:         entry.Tap,
		Confidence:  "low",
		Evidence:    []string{evidence},
		TrustTarget: entry.RawName,
	}
	if entry.Tap != "" && !brew.IsOfficialTap(entry.Tap) {
		finding.TrustStatus = "needs-review"
		finding.TrustCommandArgv = brew.TrustCommandArgv(trustKind, entry.RawName)
		finding.TrustCommand = brew.JoinCommand(finding.TrustCommandArgv)
	}
	setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewMetadataUnavailable, item.Kind, item.Name, "Homebrew apply candidate requires metadata and provenance review", nil))
	finding.Remediation = "review metadata, release age, and provenance before installing"
	return finding
}

func brewfileApplyTapFinding(finding safetyFinding) safetyFinding {
	if brew.IsOfficialTap(finding.Name) {
		finding.Decision = "allow"
		finding.Confidence = "medium"
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewOfficialFormula, "tap", finding.Name, "official Homebrew tap", nil))
		finding.Remediation = ""
		return finding
	}
	finding.Tap = finding.Name
	setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewNonOfficialTap, "tap", finding.Name, "non-official Homebrew tap needs provenance review before install", nil))
	finding.Remediation = "review the tap repository; if accepted, prefer Homebrew trust or a temporary allow policy"
	finding.TrustStatus = "needs-review"
	finding.TrustCommandArgv = []string{"brew", "trust", "--tap", finding.Name}
	finding.TrustCommand = brew.JoinCommand(finding.TrustCommandArgv)
	return finding
}

func brewfileApplyPackageFinding(ctx context.Context, finding safetyFinding, minReleaseAge time.Duration) safetyFinding {
	if finding.Tap != "" && !brew.IsOfficialTap(finding.Tap) {
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewNonOfficialTap, finding.Kind, finding.Name, "non-official Homebrew tap needs provenance review before install", nil))
		finding.Remediation = "review the tap repository; if accepted, prefer item-scoped Homebrew trust or a temporary allow policy"
		return finding
	}
	metadata, err := fetchHomebrewMetadata(ctx, http.DefaultClient, homebrewAPIURL(), finding.Kind, finding.Name)
	if err != nil {
		setSafetyFindingReason(&finding, securityreason.HomebrewPostureReason(securityreason.HomebrewMetadataUnavailable, finding.Kind, finding.Name, "Homebrew metadata unavailable before install: "+err.Error(), map[string]string{"error": err.Error()}))
		finding.Remediation = "retry after Homebrew metadata is reachable; otherwise review manually or allow by policy with reason and expiry"
		return finding
	}
	finding = applyHomebrewSafetyMetadata(finding, metadata)
	finding = applyHomebrewReleaseAge(ctx, http.DefaultClient, githubAPIURL(), finding, minReleaseAge)
	return finding
}

func brewfileApplyCandidatesFromFindings(root string, items []plan.Item, findings []safetyFinding, executors map[string]packageexecutor.Item) []brewfileApplyCandidate {
	findingsByKey := map[string]safetyFinding{}
	for _, finding := range findings {
		findingsByKey[finding.Kind+"\x00"+finding.Name] = finding
	}
	candidates := make([]brewfileApplyCandidate, 0, len(items))
	for _, item := range items {
		name := brew.NormalizePackageName(item.Kind, item.Name)
		finding := findingsByKey[item.Kind+"\x00"+name]
		decision := strings.ToLower(strings.TrimSpace(finding.Decision))
		if decision == "" {
			decision = "review"
		}
		identity, _, _, ok := packageparity.CanonicalBrewfileItem(item)
		executorItem, executorFound := executors[identity]
		reason := localizedSafetyFindingReason(finding)
		reasonCode := finding.ReasonCode
		remediation := finding.Remediation
		executor := packageexecutor.ExecutorUnsupported
		desiredSource := packageexecutor.SourceBrewfile
		var command []string
		if !ok || !executorFound {
			decision = "block"
			reasonCode = "package-executor-missing"
			reason = tr("package executor decision is missing", "package executor の判定がありません")
			remediation = tr("rerun package executor diagnostics before applying", "package executor 診断を再実行してから適用してください")
		} else {
			executor = executorItem.Executor
			desiredSource = executorItem.DesiredSource
			if executorItem.Status != plan.StatusOK || executor == packageexecutor.ExecutorUnsupported {
				decision = "block"
				reasonCode = executorItem.ReasonCode
				reason = packageexecutor.ReasonText(executorItem, defaultLanguage())
				remediation = tr("resolve the executor diagnostic before applying", "executor の診断を解消してから適用してください")
			} else if decision == "allow" {
				var commandErr error
				command, commandErr = packageapply.InstallCommand(root, executorItem)
				if commandErr != nil {
					decision = "block"
					reasonCode = "package-executor-command-unavailable"
					reason = tr("selected executor cannot build an item-scoped command: ", "選択された executor で item-scoped command を生成できません: ") + commandErr.Error()
					remediation = tr("repair the selected executor before applying", "選択された executor を修復してから適用してください")
				}
			}
		}
		status := plan.StatusHeld
		if decision == "allow" {
			status = plan.StatusDrift
		}
		candidates = append(candidates, brewfileApplyCandidate{
			Identity:      identity,
			Provider:      "brew",
			Kind:          item.Kind,
			Name:          name,
			Category:      item.Category,
			DesiredSource: desiredSource,
			Executor:      executor,
			Status:        status,
			Decision:      decision,
			Reason:        reason,
			ReasonCode:    reasonCode,
			Remediation:   remediation,
			Command:       command,
			Evidence:      append([]string(nil), finding.Evidence...),
		})
	}
	return candidates
}

func packageExecutorItemsByIdentity(items []packageexecutor.Item) map[string]packageexecutor.Item {
	result := make(map[string]packageexecutor.Item, len(items))
	for _, item := range items {
		result[item.Identity] = item
	}
	return result
}

func brewfileApplySummaryFromCandidates(candidates []brewfileApplyCandidate) brewfileApplySummary {
	summary := brewfileApplySummary{Candidates: len(candidates)}
	for _, candidate := range candidates {
		switch candidate.Decision {
		case "allow":
			summary.Allow++
		case "review":
			summary.Review++
		case "hold":
			summary.Hold++
		case "block":
			summary.Block++
		}
		if candidate.Status == plan.StatusOK {
			summary.Applied++
		}
		if candidate.Status == plan.StatusError {
			summary.Failed++
		}
	}
	return summary
}

func brewfileApplyStatus(candidates []brewfileApplyCandidate, dryRun bool) plan.Status {
	if len(candidates) == 0 {
		return plan.StatusOK
	}
	hasUnsafe := false
	hasPendingSafe := false
	hasError := false
	for _, candidate := range candidates {
		if candidate.Status == plan.StatusError {
			hasError = true
		}
		if candidate.Decision == "allow" {
			if candidate.Status != plan.StatusOK {
				hasPendingSafe = true
			}
		} else {
			hasUnsafe = true
		}
	}
	switch {
	case hasError:
		return plan.StatusError
	case hasUnsafe:
		return plan.StatusHeld
	case dryRun && hasPendingSafe:
		return plan.StatusDrift
	case hasPendingSafe:
		return plan.StatusDrift
	default:
		return plan.StatusOK
	}
}

func applyBrewfileSafeCandidates(ctx context.Context, opts applyOptions, commandRunner runner.Runner, report brewfileApplyReport) brewfileApplyReport {
	for index := range report.Candidates {
		candidate := &report.Candidates[index]
		if candidate.Decision != "allow" {
			continue
		}
		if len(candidate.Command) == 0 {
			candidate.Status = plan.StatusError
			candidate.Stderr = "no item-scoped Homebrew command was generated"
			continue
		}
		if opts.format == "text" {
			fmt.Fprintf(os.Stderr, tr("running %s...\n", "%s を実行中...\n"), brew.JoinCommand(candidate.Command))
		}
		result := runBrewfileApplyCommand(ctx, commandRunner, candidate.Command, opts.format == "text")
		candidate.Stdout = strings.TrimSpace(result.Stdout)
		candidate.Stderr = strings.TrimSpace(result.Stderr)
		if result.Code != 0 || result.Err != nil {
			candidate.Status = plan.StatusError
			continue
		}
		candidate.Status = plan.StatusOK
	}
	report.Summary = brewfileApplySummaryFromCandidates(report.Candidates)
	report.Status = brewfileApplyStatus(report.Candidates, false)
	return report
}

func runBrewfileApplyCommand(ctx context.Context, commandRunner runner.Runner, command []string, stream bool) runner.Result {
	if len(command) == 0 {
		return runner.Result{}
	}
	request := runner.Request{Name: command[0], Args: command[1:]}
	if stream {
		request.Stdout = updateProviderStdoutWriter()
		request.Stderr = os.Stderr
	}
	return runner.Execute(ctx, commandRunner, request)
}

func printBrewfileApplyText(w io.Writer, report brewfileApplyReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev apply brewfile", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	if report.DryRun {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("mode:", "モード:"), color), textui.StyleRequested("dry-run", color))
	}
	if report.Policy != nil && report.Policy.Path != "" {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("policy:", "ポリシー:"), color), report.Policy.Path)
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, textui.StyleWarning(tr("warnings:", "警告:"), color))
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("summary:", "サマリー:"), color), brewfileApplySummaryText(report.Summary, color))
	if len(report.Candidates) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim(tr("no missing Homebrew desired items", "不足している Homebrew desired item はありません"), color))
		return
	}
	rows := make([][]string, 0, len(report.Candidates))
	for _, candidate := range report.Candidates {
		rows = append(rows, []string{
			textui.StyleName(candidate.Name, color),
			candidate.Kind,
			candidate.Executor,
			textui.StyleStatus(candidate.Decision, color),
			textui.StyleStatus(string(candidate.Status), color),
			textui.Truncate(candidate.Reason, 72),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: tr("name", "名前"), Min: 12, Max: 32},
		{Header: "kind", Min: 4, Max: 6},
		{Header: "executor", Min: 8, Max: 11},
		{Header: tr("decision", "判定"), Min: 7, Max: 8},
		{Header: tr("status", "状態"), Min: 6, Max: 8},
		{Header: tr("reason", "理由"), Min: 0, Max: 72},
	}, rows, color)
}

func brewfileApplySummaryText(summary brewfileApplySummary, color bool) string {
	parts := []string{fmt.Sprintf(tr("%d candidates", "候補 %d件"), summary.Candidates)}
	if summary.Allow > 0 {
		parts = append(parts, textui.StyleStatusText(fmt.Sprintf("allow=%d", summary.Allow), "allow", color))
	}
	if summary.Review > 0 {
		parts = append(parts, textui.StyleWarning(fmt.Sprintf("review=%d", summary.Review), color))
	}
	if summary.Hold > 0 {
		parts = append(parts, textui.StyleWarning(fmt.Sprintf("hold=%d", summary.Hold), color))
	}
	if summary.Block > 0 {
		parts = append(parts, textui.StyleStatusText(fmt.Sprintf("block=%d", summary.Block), "block", color))
	}
	if summary.Applied > 0 {
		parts = append(parts, textui.StyleStatusText(fmt.Sprintf(tr("applied=%d", "適用=%d"), summary.Applied), "ok", color))
	}
	if summary.Failed > 0 {
		parts = append(parts, textui.StyleStatusText(fmt.Sprintf("failed=%d", summary.Failed), "error", color))
	}
	return strings.Join(parts, ", ")
}
