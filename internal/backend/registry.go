package backend

import "strings"

func (registry Registry) PreferenceRecommendation(sourceProvider string, sourceName string) (Recommendation, bool) {
	for _, rule := range PreferenceRules() {
		if rule.SourceProvider == sourceProvider && rule.SourceName == sourceName {
			tier := registry.PreferenceTierFor(rule.RecommendedProvider, rule.RecommendedName)
			return Recommendation{
				Provider:       rule.RecommendedProvider,
				Name:           rule.RecommendedName,
				Tier:           tier.Label,
				PreferenceRank: tier.Rank,
				Commands:       rule.Commands,
				Reason:         rule.Reason,
				SourceEvidence: rule.SourceEvidence,
				RewriteAllowed: sourceProvider == "mise",
			}, true
		}
	}
	return Recommendation{}, false
}

func (registry Registry) PreferenceTierFor(provider string, name string) PreferenceTier {
	for _, tier := range registry.PreferenceTiers() {
		if tier.Provider != provider {
			continue
		}
		if tier.Backend == "" && !strings.Contains(name, ":") {
			return tier
		}
		if tier.Backend != "" && (strings.HasPrefix(name, tier.Backend+":") || name == tier.Backend) {
			return tier
		}
	}
	for _, tier := range deprecatedBackendPreferenceTiers() {
		if tier.Provider != provider {
			continue
		}
		if tier.Backend != "" && (strings.HasPrefix(name, tier.Backend+":") || name == tier.Backend) {
			return tier
		}
	}
	return PreferenceTier{Rank: 99, Provider: provider, Label: provider + "/other", Reason: "provider has no explicit preference tier yet"}
}

func (registry Registry) PreferenceTiers() []PreferenceTier {
	return PreferenceTiersWithOrder(registry.PreferenceOrder)
}

func PreferenceTiersWithOrder(preferenceOrder []string) []PreferenceTier {
	defaults := defaultBackendPreferenceTiers()
	if len(preferenceOrder) == 0 {
		return defaults
	}
	byLabel := map[string]PreferenceTier{}
	for _, tier := range knownBackendPreferenceTiers() {
		byLabel[strings.ToLower(tier.Label)] = tier
	}
	out := make([]PreferenceTier, 0, len(defaults)+len(preferenceOrder))
	seen := map[string]bool{}
	for _, rawLabel := range preferenceOrder {
		label := strings.TrimSpace(rawLabel)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if seen[key] {
			continue
		}
		seen[key] = true
		tier, ok := byLabel[key]
		if !ok {
			tier = PreferenceTierFromLabel(label)
		}
		tier.Rank = len(out) + 1
		out = append(out, tier)
	}
	for _, tier := range defaults {
		key := strings.ToLower(tier.Label)
		if seen[key] {
			continue
		}
		tier.Rank = len(out) + 1
		out = append(out, tier)
	}
	return out
}

func defaultBackendPreferenceTiers() []PreferenceTier {
	return []PreferenceTier{
		{Rank: 1, Provider: "mise", Backend: "", Label: "mise/core", Reason: "prefer mise core backends for reliable CLI developer tools"},
		{Rank: 2, Provider: "mise", Backend: "aqua", Label: "mise/aqua", Reason: "prefer mise's preferred registry backend for feature and supply-chain coverage"},
		{Rank: 3, Provider: "mise", Backend: "github", Label: "mise/github", Reason: "prefer GitHub release backends when no core or aqua registry backend is available"},
		{Rank: 4, Provider: "mise", Backend: "gitlab", Label: "mise/gitlab", Reason: "prefer GitLab release backends when the upstream project is hosted on GitLab"},
		{Rank: 5, Provider: "mise", Backend: "conda", Label: "mise/conda", Reason: "use conda for tools that cannot reasonably use aqua or release backends"},
		{Rank: 6, Provider: "mise", Backend: "pipx", Label: "mise/pipx", Reason: "use pipx only for Python tools that need Python package distribution"},
		{Rank: 7, Provider: "mise", Backend: "npm", Label: "mise/npm", Reason: "use npm only for Node tools that need npm package distribution"},
		{Rank: 8, Provider: "mise", Backend: "gem", Label: "mise/gem", Reason: "use gem only for Ruby tools that need RubyGems package distribution"},
		{Rank: 9, Provider: "mise", Backend: "go", Label: "mise/go", Reason: "use go only when release-style binary distribution is unavailable"},
		{Rank: 10, Provider: "mise", Backend: "cargo", Label: "mise/cargo", Reason: "use cargo only when release-style binary distribution is unavailable"},
		{Rank: 11, Provider: "mise", Backend: "dotnet", Label: "mise/dotnet", Reason: "use dotnet only for tools that need dotnet package distribution"},
		{Rank: 12, Provider: "mas", Backend: "", Label: "store/native", Reason: "use store ownership when app-store evidence is stronger than tool-manager ownership"},
		{Rank: 13, Provider: "brew", Backend: "", Label: "package-manager/native", Reason: "use native package managers for GUI integration, bootstrap, or platform packaging"},
		{Rank: 14, Provider: "vendor", Backend: "", Label: "vendor/manual", Reason: "keep vendor/manual ownership for proprietary installers or weak package evidence"},
	}
}

func knownBackendPreferenceTiers() []PreferenceTier {
	known := append([]PreferenceTier{}, defaultBackendPreferenceTiers()...)
	known = append(known, deprecatedBackendPreferenceTiers()...)
	return known
}

func deprecatedBackendPreferenceTiers() []PreferenceTier {
	return []PreferenceTier{
		{Rank: 90, Provider: "mise", Backend: "ubi", Label: "mise/ubi", Reason: "ubi is deprecated in mise; prefer github"},
		{Rank: 91, Provider: "mise", Backend: "vfox", Label: "mise/vfox", Reason: "vfox is useful for private/custom plugins but new registry entries should use aqua or github"},
		{Rank: 92, Provider: "mise", Backend: "asdf", Label: "mise/asdf", Reason: "asdf plugins are legacy and carry higher supply-chain and portability risk"},
	}
}

func PreferenceTierFromLabel(label string) PreferenceTier {
	provider, backend, ok := strings.Cut(label, "/")
	if !ok {
		provider = label
		backend = ""
	}
	provider = strings.TrimSpace(provider)
	backend = strings.TrimSpace(backend)
	if provider == "" {
		provider = "custom"
	}
	return PreferenceTier{
		Provider: provider,
		Backend:  backend,
		Label:    label,
		Reason:   "configured backend preference tier",
	}
}

func PreferenceRules() []PreferenceRule {
	miseCoreReason := "stable mise core tool is preferred for CLI developer tools"
	miseCoreEvidence := []string{"source: mise core backend registry", "source: command name parity with Homebrew formula"}
	rule := func(sourceProvider string, sourceName string, recommendedName string, commands []string, reason string, evidence []string) PreferenceRule {
		return PreferenceRule{
			SourceProvider:      sourceProvider,
			SourceName:          sourceName,
			RecommendedProvider: "mise",
			RecommendedName:     recommendedName,
			Commands:            commands,
			Reason:              reason,
			SourceEvidence:      append([]string(nil), evidence...),
		}
	}
	aquaEvidence := func(repo string) []string {
		return []string{"source: mise aqua backend registry", "upstream: github.com/" + repo}
	}
	githubEvidence := func(repo string) []string {
		return []string{"source: mise github backend", "upstream: github.com/" + repo}
	}
	return []PreferenceRule{
		rule("brew", "bat", "bat", []string{"bat"}, miseCoreReason, miseCoreEvidence),
		rule("brew", "eza", "eza", []string{"eza"}, miseCoreReason, miseCoreEvidence),
		rule("brew", "fd", "aqua:sharkdp/fd", []string{"fd"}, "fd has a registry-backed mise/aqua path", aquaEvidence("sharkdp/fd")),
		rule("brew", "fzf", "fzf", []string{"fzf"}, miseCoreReason, miseCoreEvidence),
		rule("brew", "ripgrep", "ripgrep", []string{"rg"}, "ripgrep is already a stable mise-managed CLI", miseCoreEvidence),
		rule("brew", "shellcheck", "shellcheck", []string{"shellcheck"}, miseCoreReason, miseCoreEvidence),
		rule("brew", "starship", "starship", []string{"starship"}, miseCoreReason, miseCoreEvidence),
		rule("brew", "zoxide", "zoxide", []string{"zoxide"}, miseCoreReason, miseCoreEvidence),
		rule("mise", "cargo:fd-find", "aqua:sharkdp/fd", []string{"fd"}, "aqua prebuilt CLI is preferred over a cargo global build", aquaEvidence("sharkdp/fd")),
		rule("mise", "cargo:git-delta", "aqua:dandavison/delta", []string{"delta"}, "aqua prebuilt CLI is preferred over a cargo global build", aquaEvidence("dandavison/delta")),
		rule("mise", "cargo:sheldon", "aqua:rossmacarthur/sheldon", []string{"sheldon"}, "aqua prebuilt CLI is preferred over a cargo global build", aquaEvidence("rossmacarthur/sheldon")),
		rule("mise", "cargo:broot", "github:Canop/broot", []string{"broot"}, "mise GitHub release backend is preferred over a cargo global build when no core or aqua backend is defined", githubEvidence("Canop/broot")),
		rule("mise", "npm:pnpm", "aqua:pnpm/pnpm", []string{"pnpm"}, "aqua prebuilt CLI avoids npm global package-manager coupling", aquaEvidence("pnpm/pnpm")),
	}
}
