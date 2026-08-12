package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

const (
	miseRegistryTimeout       = 5 * time.Second
	homebrewInfoTimeout       = 12 * time.Second
	misePlatformLockTimeout   = 12 * time.Second
	maxRegistryPlatformChecks = 32
)

type homebrewPackageEvidence struct {
	Kind        string
	Name        string
	Version     string
	Commands    []string
	FormulaURLs []string
	CLIOnly     bool
}

func isHomebrewRegistryCheckCandidate(item plan.Item, evidence map[string]homebrewPackageEvidence, miseRegistry map[string]mise.RegistryEntry, registry Registry, commandRunner runner.Runner) bool {
	if registry.KeepsHomebrew(item.Kind, item.Name) {
		return false
	}
	entry, ok := mise.RegistryEntryForTool(miseRegistry, item.Name)
	if !ok {
		return false
	}
	packageEvidence := evidence[homebrewEvidenceKey(item.Kind, item.Name)]
	if strings.TrimSpace(packageEvidence.Version) == "" || item.Kind == "cask" && !packageEvidence.CLIOnly {
		return false
	}
	if _, _, ok := preferredMiseRegistryBackend(entry, registry); !ok {
		return false
	}
	return len(registryCommandsOnPath(entry, packageEvidence, commandRunner)) > 0
}

func loadMiseRegistry(ctx context.Context, commandRunner runner.Runner) (map[string]mise.RegistryEntry, error) {
	requestCtx, cancel := context.WithTimeout(ctx, miseRegistryTimeout)
	defer cancel()
	result := commandRunner.Run(requestCtx, "mise", "registry", "--json")
	if result.Err != nil || result.Code != 0 {
		return nil, fmt.Errorf("mise registry --json failed: %s", runner.ResultDetail(result, "registry unavailable", runner.ResultDetailOption{}))
	}
	index := mise.RegistryIndexFromJSON(result.Stdout)
	if len(index) == 0 {
		return nil, fmt.Errorf("mise registry --json returned no usable entries")
	}
	return index, nil
}

func loadHomebrewPackageEvidence(ctx context.Context, items []plan.Item, commandRunner runner.Runner) (map[string]homebrewPackageEvidence, error) {
	names := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Kind != "brew" && item.Kind != "cask" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return map[string]homebrewPackageEvidence{}, nil
	}
	sort.Strings(names)
	requestCtx, cancel := context.WithTimeout(ctx, homebrewInfoTimeout)
	defer cancel()
	args := append([]string{"info", "--json=v2"}, names...)
	result := commandRunner.Run(requestCtx, "brew", args...)
	if result.Err != nil || result.Code != 0 {
		return nil, fmt.Errorf("brew info --json=v2 failed: %s", runner.ResultDetail(result, "Homebrew metadata unavailable", runner.ResultDetailOption{}))
	}
	return parseHomebrewPackageEvidence(result.Stdout)
}

func parseHomebrewPackageEvidence(stdout string) (map[string]homebrewPackageEvidence, error) {
	var payload struct {
		Formulae []struct {
			Name      string `json:"name"`
			FullName  string `json:"full_name"`
			Installed []struct {
				Version string `json:"version"`
			} `json:"installed"`
			URLs struct {
				Stable struct {
					URL string `json:"url"`
				} `json:"stable"`
				Head struct {
					URL string `json:"url"`
				} `json:"head"`
			} `json:"urls"`
		} `json:"formulae"`
		Casks []struct {
			Token     string           `json:"token"`
			FullToken string           `json:"full_token"`
			Installed json.RawMessage  `json:"installed"`
			Artifacts []map[string]any `json:"artifacts"`
		} `json:"casks"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}
	result := map[string]homebrewPackageEvidence{}
	for _, formula := range payload.Formulae {
		version := ""
		if len(formula.Installed) > 0 {
			version = strings.TrimSpace(formula.Installed[len(formula.Installed)-1].Version)
		}
		evidence := homebrewPackageEvidence{
			Kind:        "brew",
			Name:        strings.TrimSpace(formula.Name),
			Version:     version,
			FormulaURLs: compactStrings([]string{formula.URLs.Stable.URL, formula.URLs.Head.URL}),
			CLIOnly:     true,
		}
		addHomebrewEvidence(result, evidence, formula.FullName)
	}
	for _, cask := range payload.Casks {
		commands, cliOnly := caskCLICommands(cask.Artifacts)
		evidence := homebrewPackageEvidence{
			Kind:     "cask",
			Name:     strings.TrimSpace(cask.Token),
			Version:  caskInstalledVersion(cask.Installed),
			Commands: commands,
			CLIOnly:  cliOnly,
		}
		addHomebrewEvidence(result, evidence, cask.FullToken)
	}
	return result, nil
}

func addHomebrewEvidence(index map[string]homebrewPackageEvidence, evidence homebrewPackageEvidence, aliases ...string) {
	if evidence.Name == "" {
		return
	}
	index[homebrewEvidenceKey(evidence.Kind, evidence.Name)] = evidence
	for _, alias := range aliases {
		if alias = strings.TrimSpace(alias); alias != "" {
			index[homebrewEvidenceKey(evidence.Kind, alias)] = evidence
		}
	}
}

func homebrewEvidenceKey(kind string, name string) string {
	return strings.TrimSpace(kind) + "\x00" + strings.TrimSpace(name)
}

func caskInstalledVersion(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	version, _, _ := strings.Cut(strings.TrimSpace(value), ",")
	return strings.TrimSpace(version)
}

func caskCLICommands(artifacts []map[string]any) ([]string, bool) {
	commands := []string{}
	hasBinary := false
	for _, artifact := range artifacts {
		for key, value := range artifact {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "binary":
				hasBinary = true
				commands = append(commands, artifactCommandNames(value)...)
			case "target":
				commands = append(commands, artifactCommandNames(value)...)
			case "manpage", "bash_completion", "zsh_completion", "fish_completion", "zap", "uninstall":
			default:
				return nil, false
			}
		}
	}
	return compactStrings(commands), hasBinary
}

func artifactCommandNames(value any) []string {
	commands := []string{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			name := filepath.Base(strings.TrimSpace(typed))
			if name != "" && name != "." {
				commands = append(commands, name)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if target, ok := typed["target"]; ok {
				walk(target)
			}
		}
	}
	walk(value)
	return commands
}

func (registry Registry) KeepsHomebrew(kind string, name string) bool {
	target := strings.ToLower(strings.TrimSpace(kind) + "/" + strings.TrimSpace(name))
	for _, configured := range registry.KeepHomebrew {
		if strings.ToLower(strings.TrimSpace(configured)) == target {
			return true
		}
	}
	return false
}

func homebrewRegistryRecommendation(ctx context.Context, item plan.Item, entry mise.RegistryEntry, evidence homebrewPackageEvidence, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	if strings.TrimSpace(entry.Short) == "" || strings.TrimSpace(evidence.Version) == "" {
		return Recommendation{}, false
	}
	if item.Kind == "cask" && !evidence.CLIOnly {
		return Recommendation{}, false
	}
	backend, tier, ok := preferredMiseRegistryBackend(entry, registry)
	if !ok {
		return Recommendation{}, false
	}
	commands := registryCommandsOnPath(entry, evidence, commandRunner)
	if len(commands) == 0 {
		return Recommendation{}, false
	}
	recommendedName := registryRecommendationName(entry, backend)
	target := recommendedName + "@" + evidence.Version
	compatible := miseRegistryPlatformLockCheck(ctx, recommendedName, evidence.Version, commandRunner)
	kind := "recommendation"
	reason := fmt.Sprintf("mise registry resolves %s to %s and the pinned version resolves for the current platform", entry.Short, backend)
	platformStatus := "compatible"
	if !compatible {
		kind = "candidate"
		platformStatus = "no-match"
		reason = fmt.Sprintf("mise registry resolves %s to %s, but the pinned version does not resolve for the current platform", entry.Short, backend)
	}
	return Recommendation{
		Provider:       "mise",
		Name:           recommendedName,
		Tier:           tier.Label,
		PreferenceRank: tier.Rank,
		Commands:       commands,
		Kind:           kind,
		Version:        evidence.Version,
		Reason:         reason,
		SourceEvidence: []string{
			"source: mise registry --json",
			"registry backend: " + backend,
			"platform check: isolated mise lock for " + target + " on " + miseLockPlatform(),
		},
		AssetEvidence: GitHubAssetEvidence{
			Platform: backendCurrentPlatform(),
			Status:   platformStatus,
			Matches:  []string{backend},
		},
	}, true
}

func registryRecommendationName(entry mise.RegistryEntry, selectedBackend string) string {
	for _, candidate := range entry.Backends {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == selectedBackend {
			return entry.Short
		}
		break
	}
	return selectedBackend
}

func miseRegistryPlatformLockCheck(ctx context.Context, name string, version string, commandRunner runner.Runner) bool {
	if !validMiseLockAtom(name) || !validMiseLockAtom(version) {
		return false
	}
	dir, err := os.MkdirTemp("", "updev-mise-platform-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	config := fmt.Sprintf("[tools]\n%q = %q\n", name, version)
	if err := os.WriteFile(filepath.Join(dir, "mise.toml"), []byte(config), 0o600); err != nil {
		return false
	}
	requestCtx, cancel := context.WithTimeout(ctx, misePlatformLockTimeout)
	defer cancel()
	result := commandRunner.Run(requestCtx, "mise", "lock", "--platform", miseLockPlatform(), "-C", dir, name)
	return result.Err == nil && result.Code == 0
}

func validMiseLockAtom(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func miseLockPlatform() string {
	return strings.ReplaceAll(backendCurrentPlatform(), "/", "-")
}

func preferredMiseRegistryBackend(entry mise.RegistryEntry, registry Registry) (string, PreferenceTier, bool) {
	backend := ""
	var selected PreferenceTier
	for _, candidate := range entry.Backends {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		name := candidate
		if strings.HasPrefix(candidate, "core:") {
			name = entry.Short
		}
		tier := registry.PreferenceTierFor("mise", name)
		if backend == "" || tier.Rank < selected.Rank {
			backend = candidate
			selected = tier
		}
	}
	return backend, selected, backend != ""
}

func registryCommandsOnPath(entry mise.RegistryEntry, evidence homebrewPackageEvidence, commandRunner runner.Runner) []string {
	candidates := append([]string{}, evidence.Commands...)
	candidates = append(candidates, entry.Aliases...)
	candidates = append(candidates, entry.Short, evidence.Name)
	commands := []string{}
	for _, command := range compactStrings(candidates) {
		if _, err := commandRunner.LookPath(command); err == nil {
			commands = append(commands, command)
		}
	}
	return compactStrings(commands)
}

func compactStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
