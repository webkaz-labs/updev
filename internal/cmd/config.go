package cmd

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

type updevConfig struct {
	Security  updevSecurityConfig
	Providers updevProvidersConfig
	Update    updevUpdateConfig
	UI        updevUIConfig
	Sources   updevSourcesConfig
	Brewfile  updevBrewfileConfig
	Inventory updevInventoryConfig
	Backends  updevBackendsConfig
}

type updevSecurityConfig struct {
	Homebrew updevHomebrewSecurityConfig
	Mise     updevMiseSecurityConfig
	VSCode   updevVSCodeSecurityConfig
}

type updevHomebrewSecurityConfig struct {
	MinReleaseAgeDays      *int
	MinTapAgeDays          *int
	OutdatedTimeoutSeconds *int
}

type updevMiseSecurityConfig struct {
	MinReleaseAgeDays *int
}

type updevVSCodeSecurityConfig struct {
	MinInstallCount     *float64
	MinAverageRating    *float64
	MinExtensionAgeDays *int
	MinUpdateAgeDays    *int
}

type updevProvidersConfig struct {
	IncludeVSCode *bool
}

type updevUpdateConfig struct {
	Security *string
	MiseBump updevMiseBumpUpdateConfig
}

type updevMiseBumpUpdateConfig struct {
	Mode *string
}

type updevUIConfig struct {
	Language               *string
	Interactive            *string
	Progress               *bool
	DescriptionTranslation *string
}

type updevSourcesConfig struct {
	Root *string
}

type updevBrewfileConfig struct {
	Desired   *string
	WriteMode *string
}

type updevInventoryConfig struct {
	StateDir  *string
	Overrides *string
	Manual    updevInventoryManualConfig
	Agent     updevInventoryAgentConfig
	Reports   []updevInventoryReportConfig
}

type updevInventoryManualConfig struct {
	Sources        []string
	MarkdownCompat *bool
}

type updevInventoryAgentConfig struct {
	Enabled *bool
	Command []string
	Batch   *bool
}

type updevInventoryReportConfig struct {
	Name      string
	Providers []string
	Format    string
	Path      string
}

type updevBackendsConfig struct {
	PreferenceOrder []string
}

func loadUpdevConfig() updevConfig {
	path := updevConfigPath()
	if path == "" {
		return updevConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updevConfig{}
	}
	return parseUpdevConfigTOML(string(data))
}

func updevConfigPath() string {
	return updevpath.ConfigFile()
}

func truthyEnv(name string) bool {
	value, _ := boolEnv(name)
	return value
}

func boolEnv(name string) (bool, bool) {
	return parseBoolValue(os.Getenv(name))
}

func parseUpdevConfigTOML(data string) updevConfig {
	config := updevConfig{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(collapseTOMLMultilineArrays(data)))
	for scanner.Scan() {
		line := stripTOMLComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")))
			if section == "inventory.reports" {
				config.Inventory.Reports = append(config.Inventory.Reports, updevInventoryReportConfig{})
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		stringValue := strings.Trim(value, "\"'")
		switch section {
		case "security.homebrew":
			switch key {
			case "min_release_age_days":
				config.Security.Homebrew.MinReleaseAgeDays = parseNonNegativeIntPtr(stringValue)
			case "min_tap_age_days":
				config.Security.Homebrew.MinTapAgeDays = parseNonNegativeIntPtr(stringValue)
			case "outdated_timeout_seconds":
				config.Security.Homebrew.OutdatedTimeoutSeconds = parseNonNegativeIntPtr(stringValue)
			}
		case "security.mise":
			switch key {
			case "min_release_age_days":
				config.Security.Mise.MinReleaseAgeDays = parseNonNegativeIntPtr(stringValue)
			}
		case "security.vscode":
			switch key {
			case "min_install_count":
				config.Security.VSCode.MinInstallCount = parseNonNegativeFloatPtr(stringValue)
			case "min_average_rating":
				config.Security.VSCode.MinAverageRating = parseNonNegativeFloatPtr(stringValue)
			case "min_extension_age_days":
				config.Security.VSCode.MinExtensionAgeDays = parseNonNegativeIntPtr(stringValue)
			case "min_update_age_days":
				config.Security.VSCode.MinUpdateAgeDays = parseNonNegativeIntPtr(stringValue)
			}
		case "providers":
			if key == "include_vscode" {
				config.Providers.IncludeVSCode = parseBoolPtr(stringValue)
			}
		case "update":
			if key == "security" && validUpdateSecurityMode(stringValue) {
				normalized := stringValue
				config.Update.Security = &normalized
			}
		case "update.mise_bump":
			if key == "mode" && validMiseBumpMode(stringValue) {
				normalized := strings.ToLower(strings.TrimSpace(stringValue))
				config.Update.MiseBump.Mode = &normalized
			}
		case "ui":
			switch key {
			case "language":
				if stringValue != "" {
					normalized := strings.ToLower(stringValue)
					config.UI.Language = &normalized
				}
			case "interactive":
				if validUIInteractiveMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.UI.Interactive = &normalized
				}
			case "progress":
				config.UI.Progress = parseBoolPtr(stringValue)
			case "description_translation":
				if validDescriptionTranslationMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.UI.DescriptionTranslation = &normalized
				}
			}
		case "sources":
			if key == "root" {
				config.Sources.Root = parseNonEmptyStringPtr(stringValue)
			}
		case "brewfile":
			switch key {
			case "desired":
				if validBrewfileDesiredMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.Brewfile.Desired = &normalized
				}
			case "write_mode":
				if validBrewfileWriteMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.Brewfile.WriteMode = &normalized
				}
			}
		case "inventory":
			switch key {
			case "state_dir":
				config.Inventory.StateDir = parseNonEmptyStringPtr(stringValue)
			case "overrides":
				config.Inventory.Overrides = parseNonEmptyStringPtr(stringValue)
			}
		case "inventory.manual":
			switch key {
			case "sources":
				config.Inventory.Manual.Sources = parseStringArray(value)
			case "markdown_compat":
				config.Inventory.Manual.MarkdownCompat = parseBoolPtr(stringValue)
			}
		case "inventory.agent":
			switch key {
			case "enabled":
				config.Inventory.Agent.Enabled = parseBoolPtr(stringValue)
			case "command":
				config.Inventory.Agent.Command = parseStringArray(value)
			case "batch":
				config.Inventory.Agent.Batch = parseBoolPtr(stringValue)
			}
		case "backends":
			switch key {
			case "preference_order":
				config.Backends.PreferenceOrder = parseStringArray(value)
			}
		case "inventory.reports":
			if len(config.Inventory.Reports) == 0 {
				config.Inventory.Reports = append(config.Inventory.Reports, updevInventoryReportConfig{})
			}
			report := &config.Inventory.Reports[len(config.Inventory.Reports)-1]
			switch key {
			case "name":
				report.Name = stringValue
			case "providers":
				report.Providers = parseStringArray(value)
			case "format":
				report.Format = stringValue
			case "path":
				report.Path = stringValue
			}
		}
	}
	return config
}

func validBrewfileDesiredMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "home", "root", "template", "disabled":
		return true
	default:
		return false
	}
}

func validBrewfileWriteMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "disabled", "direct", "template", "chezmoi-template":
		return true
	default:
		return false
	}
}

func collapseTOMLMultilineArrays(data string) string {
	lines := []string{}
	var pending strings.Builder
	for _, raw := range strings.Split(data, "\n") {
		line := stripTOMLComment(strings.TrimSpace(raw))
		if pending.Len() > 0 {
			if line != "" {
				pending.WriteByte(' ')
				pending.WriteString(line)
			}
			if strings.Contains(line, "]") {
				lines = append(lines, pending.String())
				pending.Reset()
			}
			continue
		}
		if strings.Contains(line, "=") && strings.Contains(line, "[") && !strings.Contains(line, "]") {
			pending.WriteString(line)
			continue
		}
		lines = append(lines, raw)
	}
	if pending.Len() > 0 {
		lines = append(lines, pending.String())
	}
	return strings.Join(lines, "\n")
}

func parseNonEmptyStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func configuredEnvString(defaultValue string, envName string) string {
	if parsed := parseNonEmptyStringPtr(os.Getenv(envName)); parsed != nil {
		return *parsed
	}
	return defaultValue
}

func parseNonNegativeIntPtr(value string) *int {
	value = strings.TrimSpace(value)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

func configuredNonNegativeInt(defaultValue int, configured *int, envName string) int {
	value := defaultValue
	if configured != nil && *configured >= 0 {
		value = *configured
	}
	if envName == "" {
		return value
	}
	if parsed := parseNonNegativeIntPtr(os.Getenv(envName)); parsed != nil {
		value = *parsed
	}
	return value
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil
	}
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		parsed := strings.Trim(strings.TrimSpace(part), "\"'")
		if parsed != "" {
			out = append(out, parsed)
		}
	}
	return out
}

func parseNonNegativeFloatPtr(value string) *float64 {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func configuredNonNegativeFloat(defaultValue float64, configured *float64, envName string) float64 {
	value := defaultValue
	if configured != nil && *configured >= 0 {
		value = *configured
	}
	if envName == "" {
		return value
	}
	if parsed := parseNonNegativeFloatPtr(os.Getenv(envName)); parsed != nil {
		value = *parsed
	}
	return value
}

func parseBoolPtr(value string) *bool {
	parsed, ok := parseBoolValue(value)
	if !ok {
		return nil
	}
	return &parsed
}

func parseBoolValue(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func validUpdateSecurityMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "warn", "strict":
		return true
	default:
		return false
	}
}

func validMiseBumpMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "manual", "safe", "auto":
		return true
	default:
		return false
	}
}

func validUIInteractiveMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "on", "off":
		return true
	default:
		return false
	}
}

func stripTOMLComment(line string) string {
	inSingle := false
	inDouble := false
	for index, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(line[:index])
			}
		}
	}
	return line
}
