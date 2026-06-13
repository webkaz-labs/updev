package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type homebrewTrustTarget = brew.TrustTarget
type homebrewTrustState = brew.TrustState

func homebrewTapTrustDependencyCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:     "brew",
		Feature:  "tap-trust",
		Required: false,
		Command:  brew.TrustJSONCommand(),
		Status:   plan.StatusOK,
	}
	if _, err := commandRunner.LookPath("brew"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation("brew", true)
		return check
	}
	targets, err := homebrewTrustTargetsFromRoot(root)
	if err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "could not inspect Brewfile trust targets: " + err.Error()
		check.Remediation = "verify the configured root and Brewfile path before relying on Homebrew tap trust diagnostics"
		return check
	}
	if len(targets) == 0 {
		check.Value = "no non-official Brewfile trust targets"
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, check.Command[0], check.Command[1:]...)
	state, err := parseHomebrewTrustState(result.Stdout)
	if err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "brew trust --json=v1 output is not valid JSON"
		if detail := dependencyCommandError(result); detail != "" {
			check.Reason += ": " + detail
		}
		check.Remediation = "upgrade or repair Homebrew tap trust support; updev expects brew trust --json=v1"
		return check
	}
	targets = applyHomebrewTrustState(targets, state)
	check.TrustTargets = targets
	trusted, untrusted := homebrewTrustTargetCounts(targets)
	check.Value = fmt.Sprintf("%d trusted, %d untrusted, %d targets", trusted, untrusted, len(targets))
	if result.Code != 0 || result.Err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "brew trust --json=v1 returned non-zero but JSON output was parsed"
		check.Remediation = "repair Homebrew trust metadata access; updev used the parsed trust JSON but provider diagnostics should exit cleanly"
		return check
	}
	if untrusted > 0 {
		check.Status = plan.StatusDrift
		check.Reason = "untrusted Homebrew tap targets: " + strings.Join(homebrewUntrustedTargetNames(targets, 4), ", ")
		check.Remediation = "review source provenance, then prefer item-scoped brew trust commands"
		if command := firstHomebrewUntrustedTrustCommand(targets); command != "" {
			check.Remediation += " such as `" + command + "`"
		}
		check.Remediation += "; trust whole taps only when you accept all current and future entries"
	}
	return check
}

func homebrewTrustTargetsFromRoot(root string) ([]homebrewTrustTarget, error) {
	path := homebrewTrustBrewfilePath(root)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return parseHomebrewTrustTargets(file, path)
}

func homebrewTrustBrewfilePath(root string) string {
	return updevpath.RootBrewfileTemplate(root)
}

func parseHomebrewTrustTargets(reader io.Reader, source string) ([]homebrewTrustTarget, error) {
	return brew.ParseTrustTargets(reader, source)
}

func parseHomebrewTrustState(raw string) (homebrewTrustState, error) {
	return brew.ParseTrustState(raw)
}

func applyHomebrewTrustState(targets []homebrewTrustTarget, state homebrewTrustState) []homebrewTrustTarget {
	return brew.ApplyTrustState(targets, state)
}

func homebrewTrustTargetCounts(targets []homebrewTrustTarget) (int, int) {
	return brew.TrustTargetCounts(targets)
}

func homebrewUntrustedTargetNames(targets []homebrewTrustTarget, limit int) []string {
	return brew.UntrustedTargetNames(targets, limit)
}

func firstHomebrewUntrustedTrustCommand(targets []homebrewTrustTarget) string {
	return brew.FirstUntrustedTrustCommand(targets)
}

func isHomebrewTrustSecurityAction(action string) bool {
	switch action {
	case securityActionBrewTrustFormula, securityActionBrewTrustCask, securityActionBrewTrustTap:
		return true
	default:
		return false
	}
}

func homebrewTrustDetailAction(gate safetyGate, finding safetyFinding) (detailBrowserAction, bool) {
	provider := firstNonEmpty(finding.Provider, gate.Provider)
	if provider != "brew" || strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
		return detailBrowserAction{}, false
	}
	action, kind, target, command, ok := homebrewTrustActionParts(finding)
	if !ok {
		return detailBrowserAction{}, false
	}
	label := tr("trust package", "package を trust")
	description := fmt.Sprintf(tr("run item-scoped %s after reviewing the package provenance", "package の出所確認後、対象 item だけ %s を実行します"), command)
	if action == securityActionBrewTrustTap {
		label = tr("trust whole tap", "tap 全体を trust")
		description = fmt.Sprintf(tr("run %s only if the whole tap and future entries are accepted", "tap 全体と今後の entry も受け入れる場合だけ %s を実行します"), command)
	}
	return detailBrowserAction{
		Value:       securityDetailActionValue(action, "brew", kind, target),
		Label:       label,
		Description: description,
	}, true
}

func homebrewTrustActionParts(finding safetyFinding) (string, string, string, string, bool) {
	kind := strings.ToLower(strings.TrimSpace(firstNonEmpty(finding.Kind, "item")))
	target := firstNonEmpty(finding.TrustTarget, finding.Name)
	if !validHomebrewTrustTarget(target) {
		return "", "", "", "", false
	}
	action := ""
	trustKind := ""
	switch kind {
	case "brew", "formula":
		if strings.TrimSpace(finding.Tap) == "" || isOfficialBrewTap(finding.Tap) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustFormula
		trustKind = "formula"
	case "cask":
		if strings.TrimSpace(finding.Tap) == "" || isOfficialBrewTap(finding.Tap) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustCask
		trustKind = "cask"
	case "tap":
		if isOfficialBrewTap(target) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustTap
		trustKind = "tap"
	default:
		return "", "", "", "", false
	}
	command := ""
	if args, ok := homebrewTrustCommandForSecurityAction(action, target); ok {
		command = joinCommand(args)
	}
	if command == "" {
		command = joinCommand(finding.TrustCommandArgv)
	}
	if command == "" {
		command = strings.TrimSpace(finding.TrustCommand)
	}
	if command == "" {
		return "", "", "", "", false
	}
	return action, trustKind, target, command, true
}

func validHomebrewTrustTarget(target string) bool {
	return brew.ValidTrustTarget(target)
}

func homebrewTrustCommandForSecurityAction(action string, target string) ([]string, bool) {
	if !validHomebrewTrustTarget(target) {
		return nil, false
	}
	trustKind := ""
	switch action {
	case securityActionBrewTrustFormula:
		trustKind = "formula"
	case securityActionBrewTrustCask:
		trustKind = "cask"
	case securityActionBrewTrustTap:
		trustKind = "tap"
	default:
		return nil, false
	}
	command := brew.TrustCommandArgv(trustKind, target)
	return command, len(command) > 0
}

func confirmHomebrewTrustAction(action string, kind string, target string) bool {
	command, ok := homebrewTrustCommandForSecurityAction(action, target)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported Homebrew trust target: %s/%s\n", kind, target)
		return false
	}
	description := tr("Trust only this Homebrew package after reviewing its source.", "出所を確認したうえで、この Homebrew package だけを trust します。")
	if action == securityActionBrewTrustTap {
		description = tr("Trust the whole tap only when you accept all current and future entries.", "現在と今後の entry すべてを受け入れる場合だけ、tap 全体を trust します。")
	}
	selected, err := runUpdevSelect("homebrew trust action", fmt.Sprintf(tr("Run %s?", "%s を実行しますか?"), joinCommand(command)), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: description, Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func applyConfirmedHomebrewTrustDetailAction(report *updateReport, action string, kind string, target string, printResult bool) bool {
	command, ok := homebrewTrustCommandForSecurityAction(action, target)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported Homebrew trust target: %s/%s\n", kind, target)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	local := runner.Local{}
	var result runner.Result
	if printResult {
		fmt.Printf("%s %s\n", textui.StyleLabel("running:", textui.ColorEnabled()), joinCommand(command))
		result = local.RunStreaming(ctx, os.Stdout, os.Stderr, command[0], command[1:]...)
	} else {
		result = local.Run(ctx, command[0], command[1:]...)
	}
	if result.Err != nil || result.Code != 0 {
		if printResult {
			fmt.Fprintf(os.Stderr, "homebrew trust failed: %s\n", brewOutdatedResultDetail(result, "brew trust failed"))
		}
		return true
	}
	if report != nil {
		for index := range report.Safety {
			if report.Safety[index].Provider == "brew" {
				report.Safety[index].Evidence = appendEvidence(report.Safety[index].Evidence, "Homebrew trust command applied: "+joinCommand(command))
				break
			}
		}
		report.Report = saveLastUpdateReport(*report)
	}
	if printResult {
		fmt.Printf("%s %s\n", textui.StyleLabel("trusted:", textui.ColorEnabled()), joinCommand(command))
	}
	return true
}
