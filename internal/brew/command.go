package brew

import (
	"strconv"
	"strings"
)

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
