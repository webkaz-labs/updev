package mise

import (
	"sort"
	"strings"
)

func UpgradeCommand(root string, tools []string, minimumReleaseAge string) []string {
	tools = cleanCommandTargets(tools)
	if len(tools) == 0 {
		return nil
	}
	command := []string{"mise", "upgrade", "--yes"}
	if minimumReleaseAge = strings.TrimSpace(minimumReleaseAge); minimumReleaseAge != "" {
		command = append(command, "--minimum-release-age", minimumReleaseAge)
	}
	if root = strings.TrimSpace(root); root != "" {
		command = append(command, "--cd", root)
	}
	command = append(command, tools...)
	return command
}

func UpgradeAllCommand() []string {
	return []string{"mise", "upgrade"}
}

func UpgradeAllCommands() [][]string {
	return [][]string{
		UpgradeAllCommand(),
		PruneCommand(),
	}
}

func UpgradeCommands(root string, tools []string, minimumReleaseAge string) [][]string {
	command := UpgradeCommand(root, tools, minimumReleaseAge)
	if len(command) == 0 {
		return nil
	}
	return [][]string{
		command,
		PruneCommand(),
	}
}

func BumpCommand(root string, tools []string, dryRun bool, yes bool, bypassMinimumReleaseAge bool) []string {
	tools = cleanCommandTargets(tools)
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
	if root = strings.TrimSpace(root); root != "" {
		args = append(args, "--cd", root)
	}
	args = append(args, tools...)
	command := append([]string{"mise"}, args...)
	if bypassMinimumReleaseAge {
		return append([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d"}, command...)
	}
	return command
}

func BumpApplyCommands(root string, tools []string, bypassMinimumReleaseAge bool) [][]string {
	preflight := BumpCommand(root, tools, true, false, bypassMinimumReleaseAge)
	apply := BumpCommand(root, tools, false, true, bypassMinimumReleaseAge)
	commands := [][]string{}
	if len(preflight) > 0 {
		commands = append(commands, preflight)
	}
	if len(apply) > 0 {
		commands = append(commands, apply)
	}
	return commands
}

func PruneCommand() []string {
	return []string{"mise", "prune"}
}

func cleanCommandTargets(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
