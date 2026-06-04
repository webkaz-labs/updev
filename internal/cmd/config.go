package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type updevConfig struct {
	Security  updevSecurityConfig
	Providers updevProvidersConfig
	Update    updevUpdateConfig
	UI        updevUIConfig
	Inventory updevInventoryConfig
}

type updevSecurityConfig struct {
	Homebrew updevHomebrewSecurityConfig
	VSCode   updevVSCodeSecurityConfig
}

type updevHomebrewSecurityConfig struct {
	MinReleaseAgeDays *int
	MinTapAgeDays     *int
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
}

type updevUIConfig struct {
	Language               *string
	Interactive            *string
	Progress               *bool
	DescriptionTranslation *string
}

type updevInventoryConfig struct {
	StateDir  *string
	Overrides *string
	Reports   []updevInventoryReportConfig
}

type updevInventoryReportConfig struct {
	Name      string
	Providers []string
	Format    string
	Path      string
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
	if value := strings.TrimSpace(os.Getenv("UPDEV_CONFIG")); value != "" {
		return value
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "updev", "config.toml")
}

func parseUpdevConfigTOML(data string) updevConfig {
	config := updevConfig{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(data))
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
		case "inventory":
			switch key {
			case "state_dir":
				config.Inventory.StateDir = parseNonEmptyStringPtr(stringValue)
			case "overrides":
				config.Inventory.Overrides = parseNonEmptyStringPtr(stringValue)
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

func parseNonEmptyStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func parseNonNegativeIntPtr(value string) *int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
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
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
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
