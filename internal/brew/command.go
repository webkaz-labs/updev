package brew

import (
	"sort"
	"strconv"
	"strings"
)

func UpgradeGreedyNoAutoUpdateCommand(names []string) []string {
	names = cleanCommandTargets(names)
	if len(names) == 0 {
		return nil
	}
	return append([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy"}, names...)
}

func UpgradeGreedyCommand(names ...string) []string {
	command := []string{"brew", "upgrade", "--greedy"}
	command = append(command, cleanCommandTargets(names)...)
	return command
}

func UpgradeGreedyCommands() [][]string {
	return [][]string{
		UpdateCommand(),
		UpgradeGreedyCommand(),
		CleanupCommand(),
	}
}

func UpgradeGreedyNoAutoUpdateCommands(names []string) [][]string {
	command := UpgradeGreedyNoAutoUpdateCommand(names)
	if len(command) == 0 {
		return nil
	}
	return [][]string{
		command,
		CleanupNoAutoUpdateCommand(),
		UpdateCommand(),
	}
}

func CleanupCommand() []string {
	return []string{"brew", "cleanup"}
}

func CleanupNoAutoUpdateCommand() []string {
	return []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "cleanup"}
}

func UpdateCommand() []string {
	return []string{"brew", "update"}
}

func TrustJSONCommand() []string {
	return []string{"env", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "trust", "--json=v1"}
}

func TrustCommandArgv(kind string, target string) []string {
	kind = strings.TrimSpace(kind)
	target = strings.TrimSpace(target)
	if kind == "" || target == "" {
		return nil
	}
	return []string{"brew", "trust", "--" + kind, target}
}

func JoinCommand(command []string) string {
	parts := []string{}
	for _, arg := range command {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if strings.ContainsAny(arg, " \t\n\"'\\$`") {
			parts = append(parts, strconv.Quote(arg))
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
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
