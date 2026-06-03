package cmd

import (
	"os"
	"strings"
)

const includeVSCodeEnvName = "UPDEV_INCLUDE_VSCODE"

func includeVSCodeExtensionsByDefault() bool {
	if value, ok := boolEnv(includeVSCodeEnvName); ok {
		return value
	}
	if configured := loadUpdevConfig().Providers.IncludeVSCode; configured != nil {
		return *configured
	}
	return false
}

func truthyEnv(name string) bool {
	value, _ := boolEnv(name)
	return value
}

func boolEnv(name string) (bool, bool) {
	return parseBoolValue(os.Getenv(name))
}

func providerFilterIsVSCode(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "vscode")
}

func kindFilterIsVSCode(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "vscode")
}
