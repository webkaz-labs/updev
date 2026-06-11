package backend

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func homebrewGitHubBackendRecommendation(name string, githubRepos map[string]string, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	repo, ok := githubRepos[name]
	if !ok {
		return Recommendation{}, false
	}
	if _, err := commandRunner.LookPath(name); err != nil {
		return Recommendation{}, false
	}
	return githubBackendCandidate(repo, []string{name}, "Homebrew formula upstream is a GitHub repository; verify release assets and ownership before moving the tool out of Homebrew", registry, commandRunner)
}

func genericHomebrewRecommendationNames(items []plan.Item, registry Registry) []string {
	names := []string{}
	for _, item := range items {
		if item.Kind != "brew" {
			continue
		}
		if _, ok := explicitHomebrewBackendRecommendation(item.Name, registry); ok {
			continue
		}
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names
}

func homebrewFormulaGitHubRepos(names []string, commandRunner runner.Runner) map[string]string {
	repos := map[string]string{}
	if len(names) == 0 {
		return repos
	}
	args := append([]string{"info", "--json=v2"}, names...)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "brew", args...)
	if result.Err != nil || result.Code != 0 {
		return repos
	}
	var payload struct {
		Formulae []struct {
			Name string `json:"name"`
			URLs struct {
				Stable struct {
					URL string `json:"url"`
				} `json:"stable"`
				Head struct {
					URL string `json:"url"`
				} `json:"head"`
			} `json:"urls"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return repos
	}
	for _, formula := range payload.Formulae {
		if strings.TrimSpace(formula.Name) == "" {
			continue
		}
		for _, rawURL := range []string{formula.URLs.Stable.URL, formula.URLs.Head.URL} {
			if repo, ok := backendGitHubRepoFromURL(rawURL); ok {
				repos[formula.Name] = repo
				break
			}
		}
	}
	return repos
}

func miseGitHubBackendRecommendation(name string, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	backend, packageName, ok := strings.Cut(name, ":")
	if !ok || strings.TrimSpace(packageName) == "" {
		return Recommendation{}, false
	}
	var repo string
	switch backend {
	case "cargo":
		repo, ok = cargoPackageGitHubRepo(packageName, commandRunner)
	case "npm":
		repo, ok = npmPackageGitHubRepo(packageName, commandRunner)
	default:
		return Recommendation{}, false
	}
	if !ok {
		return Recommendation{}, false
	}
	command := backendPackageCommandGuess(packageName)
	if _, err := commandRunner.LookPath(command); err != nil {
		return Recommendation{}, false
	}
	return githubBackendCandidate(repo, []string{command}, fmt.Sprintf("%s package metadata points to a GitHub repository; verify GitHub release assets, version mapping, and official distribution ownership before replacing the language package backend", backend), registry, commandRunner)
}

func githubBackendRecommendation(repo string, commands []string, reason string, registry Registry) (Recommendation, bool) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return Recommendation{}, false
	}
	recommended := "github:" + repo
	tier := registry.PreferenceTierFor("mise", recommended)
	return Recommendation{
		Provider:       "mise",
		Name:           recommended,
		Tier:           tier.Label,
		PreferenceRank: tier.Rank,
		Commands:       commands,
		Kind:           "recommendation",
		Reason:         reason,
		SourceEvidence: []string{"source: upstream repository metadata", "upstream: github.com/" + repo},
	}, true
}

func githubBackendCandidate(repo string, commands []string, reason string, registry Registry, commandRunner runner.Runner) (Recommendation, bool) {
	recommendation, ok := githubBackendRecommendation(repo, commands, reason, registry)
	if !ok {
		return Recommendation{}, false
	}
	recommendation.Kind = "candidate"
	recommendation.AssetEvidence = githubReleaseAssetEvidence(repo, commandRunner)
	return recommendation, true
}

func RecommendationKind(recommendation Recommendation) string {
	if recommendation.Kind != "" {
		return recommendation.Kind
	}
	return "recommendation"
}

func githubReleaseAssetEvidence(repo string, commandRunner runner.Runner) GitHubAssetEvidence {
	evidence := GitHubAssetEvidence{
		Platform: backendCurrentPlatform(),
		Status:   "unknown",
	}
	if _, err := commandRunner.LookPath("gh"); err != nil {
		evidence.Status = "gh-unavailable"
		return evidence
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "gh", "api", "repos/"+repo+"/releases/latest", "--jq", ".assets[].name")
	if result.Err != nil || result.Code != 0 {
		evidence.Status = "unavailable"
		return evidence
	}
	assets := backendReleaseAssetNames(result.Stdout)
	evidence.Assets = limitedStrings(assets, 12)
	if len(assets) == 0 {
		evidence.Status = "no-assets"
		return evidence
	}
	matches := backendReleaseAssetMatchesPlatform(assets, evidence.Platform)
	evidence.Matches = limitedStrings(matches, 6)
	if len(matches) == 0 {
		evidence.Status = "no-match"
		return evidence
	}
	evidence.Status = "compatible"
	return evidence
}

func backendCurrentPlatform() string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "macos"
	}
	archName := runtime.GOARCH
	switch archName {
	case "amd64":
		archName = "x64"
	case "386":
		archName = "x86"
	}
	return osName + "/" + archName
}

func backendReleaseAssetNames(stdout string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func backendReleaseAssetMatchesPlatform(assets []string, platform string) []string {
	osName, archName, ok := strings.Cut(platform, "/")
	if !ok {
		return nil
	}
	matches := []string{}
	for _, asset := range assets {
		if backendAssetMatchesOS(asset, osName) && backendAssetMatchesArch(asset, archName) {
			matches = append(matches, asset)
		}
	}
	return matches
}

func backendAssetMatchesOS(asset string, osName string) bool {
	asset = strings.ToLower(asset)
	switch osName {
	case "macos":
		return strings.Contains(asset, "darwin") || strings.Contains(asset, "macos") || strings.Contains(asset, "apple-darwin") || strings.Contains(asset, "osx")
	case "linux":
		return strings.Contains(asset, "linux")
	case "windows":
		return strings.Contains(asset, "windows") || strings.Contains(asset, "win32") || strings.Contains(asset, "win64") || strings.Contains(asset, "win-")
	default:
		return strings.Contains(asset, osName)
	}
}

func backendAssetMatchesArch(asset string, archName string) bool {
	asset = strings.ToLower(asset)
	if strings.Contains(asset, "universal") || strings.Contains(asset, "noarch") {
		return true
	}
	switch archName {
	case "arm64":
		return strings.Contains(asset, "arm64") || strings.Contains(asset, "aarch64")
	case "x64":
		return strings.Contains(asset, "x86_64") || strings.Contains(asset, "amd64") || strings.Contains(asset, "x64")
	case "x86":
		return strings.Contains(asset, "i386") || strings.Contains(asset, "i686") || strings.Contains(asset, "x86")
	default:
		return strings.Contains(asset, archName)
	}
}

func limitedStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	out := append([]string{}, values[:limit]...)
	out = append(out, fmt.Sprintf("... %d more", len(values)-limit))
	return out
}

func cargoPackageGitHubRepo(name string, commandRunner runner.Runner) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "cargo", "info", name)
	if result.Err != nil || result.Code != 0 {
		return "", false
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "repository", "homepage":
			if repo, ok := backendGitHubRepoFromURL(strings.TrimSpace(value)); ok {
				return repo, true
			}
		}
	}
	return "", false
}

func npmPackageGitHubRepo(name string, commandRunner runner.Runner) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "npm", "view", name, "repository", "homepage", "--json")
	if result.Err != nil || result.Code != 0 {
		return "", false
	}
	for _, rawURL := range npmMetadataURLs(result.Stdout) {
		if repo, ok := backendGitHubRepoFromURL(rawURL); ok {
			return repo, true
		}
	}
	return "", false
}

func npmMetadataURLs(stdout string) []string {
	var payload any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil
	}
	urls := []string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			urls = append(urls, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for key, item := range typed {
				switch strings.ToLower(key) {
				case "url", "repository", "homepage":
					walk(item)
				default:
					if strings.Contains(strings.ToLower(key), "repository") {
						walk(item)
					}
				}
			}
		}
	}
	walk(payload)
	return urls
}

func backendPackageCommandGuess(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "@")
	if _, after, ok := strings.Cut(name, "/"); ok {
		name = after
	}
	return name
}

func backendGitHubRepoFromURL(rawURL string) (string, bool) {
	rawURL = strings.TrimPrefix(strings.TrimSpace(rawURL), "git+")
	parsed, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if validGitHubPathPart(owner) && validGitHubPathPart(repo) {
		return owner + "/" + repo, true
	}
	return "", false
}

func splitPath(path string) []string {
	out := []string{}
	for _, part := range strings.Split(path, "/") {
		part, err := neturl.PathUnescape(part)
		if err != nil {
			part = strings.TrimSpace(part)
		}
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validGitHubPathPart(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
