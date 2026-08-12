package updateplan

import (
	"strings"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/updatereason"
	"github.com/webkaz-labs/updev/internal/updatereport"
)

const (
	BrewProvider = "brew"
	MiseProvider = "mise"
)

type StrictOptions struct {
	Enabled               bool
	Root                  string
	MiseMinimumReleaseAge string
}

type StrictResult struct {
	Step            updatereport.Step
	HoldReason      updatereason.Reason
	SkippedFindings []securitygate.Finding
}

func DefaultSteps() []updatereport.Step {
	return []updatereport.Step{
		{
			Name:     BrewProvider,
			Command:  brew.UpgradeGreedyCommand(),
			Commands: commandsFromArgv(brew.UpgradeGreedyCommands()),
		},
		{
			Name:     MiseProvider,
			Command:  mise.UpgradeAllCommand(),
			Commands: commandsFromArgv(mise.UpgradeAllCommands()),
		},
	}
}

func StepForProvider(provider string) (updatereport.Step, bool) {
	provider = strings.TrimSpace(provider)
	for _, step := range DefaultSteps() {
		if step.Name == provider {
			return step, true
		}
	}
	return updatereport.Step{}, false
}

func ScopeStrict(step updatereport.Step, options StrictOptions, gates []securitygate.Gate) StrictResult {
	result := StrictResult{Step: step}
	if !options.Enabled {
		return result
	}

	gate, ok := gateForProvider(step.Name, gates)
	if !ok {
		return result
	}
	if gate.Status == plan.StatusError {
		reason := updatereason.StrictGateFailedReason(gate.Error)
		setStepReason(&result.Step, reason)
		result.HoldReason = reason
		return result
	}

	safe, unsafe := splitFindings(gate.Findings)
	if step.Name == BrewProvider && len(safe) == 0 && len(unsafe) == 0 {
		result.Step.Command = brew.UpdateCommand()
		result.Step.Commands = commandsFromArgv([][]string{brew.UpdateCommand()})
		setStepReason(&result.Step, updatereason.StrictBrewRefreshOnlyReason())
		return result
	}
	if len(safe) == 0 && gate.Status == plan.StatusHeld {
		if step.Name == BrewProvider && len(unsafe) > 0 {
			result.Step.Command = brew.UpdateCommand()
			result.Step.Commands = commandsFromArgv([][]string{brew.UpdateCommand()})
			setStepReason(&result.Step, updatereason.StrictBrewHeldReason(len(unsafe)))
			result.SkippedFindings = unsafe
			return result
		}
		result.HoldReason = updatereason.StrictGateReviewReason()
		return result
	}
	if len(safe) == 0 {
		return result
	}

	targets := findingNames(safe)
	switch step.Name {
	case MiseProvider:
		result.Step.Command = mise.UpgradeCommand(options.Root, targets, options.MiseMinimumReleaseAge)
		if len(result.Step.Command) == 0 {
			result.HoldReason = updatereason.StrictMiseNoSafeReason()
			return result
		}
		result.Step.Commands = commandsFromArgv(mise.UpgradeCommands(options.Root, targets, options.MiseMinimumReleaseAge))
		if len(unsafe) > 0 {
			setStepReason(&result.Step, updatereason.StrictMisePartialReason(len(safe), len(unsafe)))
			result.SkippedFindings = unsafe
		}
	case BrewProvider:
		result.Step.Command = brew.UpgradeGreedyNoAutoUpdateCommand(targets)
		if len(result.Step.Command) == 0 {
			result.HoldReason = updatereason.StrictBrewNoSafeReason()
			return result
		}
		result.Step.Commands = commandsFromArgv(brew.UpgradeGreedyNoAutoUpdateCommands(targets))
		if len(unsafe) > 0 {
			setStepReason(&result.Step, updatereason.StrictBrewPartialReason(len(safe), len(unsafe)))
			result.SkippedFindings = unsafe
		}
	default:
		if gate.Status == plan.StatusHeld {
			result.HoldReason = updatereason.StrictGateReviewReason()
		}
	}
	return result
}

func IsBrewRefreshOnly(step updatereport.Step) bool {
	if step.Name != BrewProvider {
		return false
	}
	commands := step.Commands
	if len(commands) == 0 && len(step.Command) > 0 {
		commands = []updatereport.Command{{Command: step.Command}}
	}
	if len(commands) != 1 {
		return false
	}
	return equalArgv(commands[0].Command, brew.UpdateCommand())
}

func gateForProvider(provider string, gates []securitygate.Gate) (securitygate.Gate, bool) {
	for _, gate := range gates {
		if gate.Provider == provider {
			return gate, true
		}
	}
	return securitygate.Gate{}, false
}

func splitFindings(findings []securitygate.Finding) ([]securitygate.Finding, []securitygate.Finding) {
	safe := []securitygate.Finding{}
	unsafe := []securitygate.Finding{}
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
			safe = append(safe, finding)
			continue
		}
		unsafe = append(unsafe, finding)
	}
	return safe, unsafe
}

func findingNames(findings []securitygate.Finding) []string {
	values := make([]string, 0, len(findings))
	for _, finding := range findings {
		values = append(values, finding.Name)
	}
	return values
}

func commandsFromArgv(commands [][]string) []updatereport.Command {
	out := []updatereport.Command{}
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		out = append(out, updatereport.Command{Command: command})
	}
	return out
}

func setStepReason(step *updatereport.Step, reason updatereason.Reason) {
	step.Reason = reason.Text
	step.ReasonCode = reason.Code
	step.ReasonArgs = reason.Args
}

func equalArgv(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
