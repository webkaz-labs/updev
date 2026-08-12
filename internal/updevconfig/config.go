package updevconfig

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

type Config struct {
	Security      SecurityConfig
	Providers     ProvidersConfig
	Update        UpdateConfig
	UI            UIConfig
	Sources       SourcesConfig
	Brewfile      BrewfileConfig
	ChezmoiHooks  ChezmoiHooksConfig
	MiseBootstrap MiseBootstrapConfig
	Inventory     InventoryConfig
	Backends      BackendsConfig
}

type SecurityConfig struct {
	Homebrew HomebrewSecurityConfig
	Mise     MiseSecurityConfig
	VSCode   VSCodeSecurityConfig
}

type HomebrewSecurityConfig struct {
	MinReleaseAgeDays      *int
	MinTapAgeDays          *int
	OutdatedTimeoutSeconds *int
}

type MiseSecurityConfig struct {
	MinReleaseAgeDays *int
}

type VSCodeSecurityConfig struct {
	MinInstallCount     *float64
	MinAverageRating    *float64
	MinExtensionAgeDays *int
	MinUpdateAgeDays    *int
}

type ProvidersConfig struct {
	IncludeVSCode *bool
}

type UpdateConfig struct {
	Security *string
	MiseBump MiseBumpUpdateConfig
}

type MiseBumpUpdateConfig struct {
	Mode *string
}

type UIConfig struct {
	Language               *string
	Interactive            *string
	Progress               *bool
	DescriptionTranslation *string
}

type SourcesConfig struct {
	Root *string
}

type BrewfileConfig struct {
	Desired   *string
	WriteMode *string
}

type ChezmoiHooksConfig struct {
	Brewfile ChezmoiBrewfileHookConfig
}

type ChezmoiBrewfileHookConfig struct {
	Mode *string
}

type MiseBootstrapConfig struct {
	PackageMetadata *string
}

type InventoryConfig struct {
	StateDir  *string
	Overrides *string
	Manual    InventoryManualConfig
	Agent     InventoryAgentConfig
	Reports   []InventoryReportConfig
}

type InventoryManualConfig struct {
	Sources        []string
	MarkdownCompat *bool
}

type InventoryAgentConfig struct {
	Enabled *bool
	Command []string
	Batch   *bool
}

type InventoryReportConfig struct {
	Name      string
	Providers []string
	Format    string
	Path      string
}

type BackendsConfig struct {
	PreferenceOrder []string
	KeepHomebrew    []string
}

func Load() Config {
	config, _ := LoadWithError()
	return config
}

func LoadWithError() (Config, error) {
	path := ConfigPath()
	if path == "" {
		return Config{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	return ParseTOMLWithError(string(data))
}

func ConfigPath() string {
	return updevpath.ConfigFile()
}

func TruthyEnv(name string) bool {
	value, ok := BoolEnv(name)
	return ok && value
}

func BoolEnv(name string) (bool, bool) {
	return ParseBoolValue(os.Getenv(name))
}

func ParseTOML(data string) Config {
	config, _ := ParseTOMLWithError(data)
	return config
}

func ParseTOMLWithError(data string) (Config, error) {
	config := Config{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(collapseTOMLMultilineArrays(data)))
	for scanner.Scan() {
		line := StripTOMLComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")))
			if section == "inventory.reports" {
				config.Inventory.Reports = append(config.Inventory.Reports, InventoryReportConfig{})
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
			if key == "security" && ValidUpdateSecurityMode(stringValue) {
				normalized := stringValue
				config.Update.Security = &normalized
			}
		case "update.mise_bump":
			if key == "mode" && ValidMiseBumpMode(stringValue) {
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
				if ValidUIInteractiveMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.UI.Interactive = &normalized
				}
			case "progress":
				config.UI.Progress = parseBoolPtr(stringValue)
			case "description_translation":
				if ValidDescriptionTranslationMode(stringValue) {
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
				if ValidBrewfileDesiredMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.Brewfile.Desired = &normalized
				}
			case "write_mode":
				if ValidBrewfileWriteMode(stringValue) {
					normalized := strings.ToLower(stringValue)
					config.Brewfile.WriteMode = &normalized
				}
			}
		case "chezmoi_hooks.brewfile":
			if key == "mode" && ValidChezmoiBrewfileHookMode(stringValue) {
				normalized := strings.ToLower(strings.TrimSpace(stringValue))
				config.ChezmoiHooks.Brewfile.Mode = &normalized
			}
		case "mise_bootstrap":
			if key == "package_metadata" {
				config.MiseBootstrap.PackageMetadata = parseNonEmptyStringPtr(stringValue)
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
				config.Inventory.Manual.Sources = ParseStringArray(value)
			case "markdown_compat":
				config.Inventory.Manual.MarkdownCompat = parseBoolPtr(stringValue)
			}
		case "inventory.agent":
			switch key {
			case "enabled":
				config.Inventory.Agent.Enabled = parseBoolPtr(stringValue)
			case "command":
				config.Inventory.Agent.Command = ParseStringArray(value)
			case "batch":
				config.Inventory.Agent.Batch = parseBoolPtr(stringValue)
			}
		case "backends":
			switch key {
			case "preference_order":
				config.Backends.PreferenceOrder = ParseStringArray(value)
			case "keep_homebrew":
				config.Backends.KeepHomebrew = ParseStringArray(value)
			}
		case "inventory.reports":
			if len(config.Inventory.Reports) == 0 {
				config.Inventory.Reports = append(config.Inventory.Reports, InventoryReportConfig{})
			}
			report := &config.Inventory.Reports[len(config.Inventory.Reports)-1]
			switch key {
			case "name":
				report.Name = stringValue
			case "providers":
				report.Providers = ParseStringArray(value)
			case "format":
				report.Format = stringValue
			case "path":
				report.Path = stringValue
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func PackageMetadataPath(config Config) string {
	if config.MiseBootstrap.PackageMetadata == nil {
		return updevpath.PackageMetadataFile()
	}
	return updevpath.ResolveConfigRelative(*config.MiseBootstrap.PackageMetadata, ConfigPath())
}

func ValidBrewfileDesiredMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "home", "root", "template", "disabled":
		return true
	default:
		return false
	}
}

func ValidBrewfileWriteMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "disabled", "direct", "template", "chezmoi-template":
		return true
	default:
		return false
	}
}

func ValidChezmoiBrewfileHookMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "warn", "apply-safe":
		return true
	default:
		return false
	}
}

func collapseTOMLMultilineArrays(data string) string {
	lines := []string{}
	var pending strings.Builder
	for _, raw := range strings.Split(data, "\n") {
		line := StripTOMLComment(strings.TrimSpace(raw))
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

func ConfiguredEnvString(defaultValue string, envName string) string {
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

func ConfiguredNonNegativeInt(defaultValue int, configured *int, envName string) int {
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

func ParseStringArray(value string) []string {
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

func ConfiguredNonNegativeFloat(defaultValue float64, configured *float64, envName string) float64 {
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
	parsed, ok := ParseBoolValue(value)
	if !ok {
		return nil
	}
	return &parsed
}

func ParseBoolValue(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func ValidUpdateSecurityMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "warn", "strict":
		return true
	default:
		return false
	}
}

func ValidMiseBumpMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "manual", "safe", "auto":
		return true
	default:
		return false
	}
}

func ValidUIInteractiveMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "on", "off":
		return true
	default:
		return false
	}
}

func ValidDescriptionTranslationMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "manual", "off":
		return true
	default:
		return false
	}
}

func StripTOMLComment(line string) string {
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
