package vscode

import (
	"context"
	"fmt"
	"strings"

	"github.com/webkaz-labs/updev/internal/runner"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) runner.Result
}

func InstalledVersionsCommand() []string {
	return []string{"code", "--list-extensions", "--show-versions"}
}

func InstalledVersions(ctx context.Context, commandRunner CommandRunner) (map[string]string, string) {
	command := InstalledVersionsCommand()
	result := commandRunner.Run(ctx, command[0], command[1:]...)
	if result.Code != 0 || result.Err != nil {
		return nil, InstalledVersionsError(result)
	}
	return ParseInstalledVersions(result.Stdout), ""
}

func InstalledVersionsError(result runner.Result) string {
	if detail := firstNonEmpty(result.Stderr, result.Stdout); detail != "" {
		return detail
	}
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Code != 0 {
		return fmt.Sprintf("code exited with status %d", result.Code)
	}
	return "unknown error"
}

func ParseInstalledVersions(raw string) map[string]string {
	versions := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, version, ok := strings.Cut(line, "@")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" {
			continue
		}
		versions[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(version)
	}
	return versions
}
