package cmd

import (
	"context"
	"net/http"
	"strings"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/plan"
)

const defaultHomebrewAPIURL = "https://formulae.brew.sh/api"

type homebrewPosture = brew.Posture
type homebrewMetadata = brew.Metadata
type homebrewVersions = brew.MetadataVersions
type homebrewURLs = brew.MetadataURLs
type homebrewURL = brew.MetadataURL

type homebrewAdvisoryMapping struct {
	Ecosystem string
	Package   string
}

var curatedHomebrewAdvisoryMappings = map[string]homebrewAdvisoryMapping{
	"brew:pnpm":        {Ecosystem: "npm", Package: "pnpm"},
	"brew:yarn":        {Ecosystem: "npm", Package: "yarn"},
	"brew:typescript":  {Ecosystem: "npm", Package: "typescript"},
	"brew:mise":        {Ecosystem: "crates.io", Package: "mise"},
	"brew:ripgrep":     {Ecosystem: "crates.io", Package: "ripgrep"},
	"brew:fd":          {Ecosystem: "crates.io", Package: "fd-find"},
	"brew:cargo-audit": {Ecosystem: "crates.io", Package: "cargo-audit"},
}

func homebrewPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, root string, items []plan.Item) ([]homebrewPosture, error) {
	manifest, _ := loadBrewSafetyManifest(root)
	return brew.PosturesFromItems(ctx, client, apiBase, items, func(kind string, name string) brew.ManifestEntry {
		entry := manifest.entry(kind, name)
		return homebrewAuditManifestEntry(entry)
	})
}

func homebrewTapPosture(item plan.Item) homebrewPosture {
	return brew.TapPosture(item)
}

func homebrewTapGitHubURL(tap string) string {
	return brew.TapGitHubURL(tap)
}

func fetchHomebrewMetadata(ctx context.Context, client *http.Client, apiBase string, kind string, name string) (homebrewMetadata, error) {
	return brew.FetchMetadata(ctx, client, apiBase, kind, name)
}

func homebrewPostureFromMetadata(item plan.Item, entry brewSafetyEntry, metadata homebrewMetadata) homebrewPosture {
	return brew.PostureFromMetadata(item, homebrewAuditManifestEntry(entry), metadata)
}

func homebrewManifestPosture(item plan.Item, entry brewSafetyEntry, reason string) homebrewPosture {
	return brew.ManifestPosture(item, homebrewAuditManifestEntry(entry), reason)
}

func homebrewMetadataUnavailable(item plan.Item, entry brewSafetyEntry, err error) homebrewPosture {
	return brew.MetadataUnavailable(item, homebrewAuditManifestEntry(entry), err)
}

func homebrewAuditManifestEntry(entry brewSafetyEntry) brew.ManifestEntry {
	return brew.ManifestEntry{
		RawName:  entry.RawName,
		Tap:      entry.Tap,
		URLBased: entry.URLBased,
	}
}

func homebrewAdvisoryPackagesFromPostures(postures []homebrewPosture) []securityPackage {
	packages := []securityPackage{}
	seen := map[string]bool{}
	for _, posture := range postures {
		if posture.Kind != "brew" && posture.Kind != "cask" {
			continue
		}
		for _, pkg := range homebrewAdvisoryPackages(posture.Kind, posture.Name, posture.Version, posture.URL) {
			key := safetyAdvisoryPackageKey(pkg)
			if seen[key] {
				continue
			}
			seen[key] = true
			packages = append(packages, pkg)
		}
	}
	return packages
}

func homebrewAdvisoryPackages(kind string, name string, version string, rawURL string) []securityPackage {
	packages := []securityPackage{}
	if rawURL != "" {
		if repo, tag, ok := githubrepo.RepoTagFromURL(rawURL); ok {
			packages = append(packages, securityPackage{
				Provider:   "brew",
				Name:       name,
				Version:    tag,
				Ecosystem:  "GIT",
				Package:    githubRepoGitURL(repo),
				Confidence: "medium",
			})
		}
	}
	if mapped, ok := curatedHomebrewAdvisoryMapping(kind, name); ok && version != "" {
		packages = append(packages, securityPackage{
			Provider:   "brew",
			Name:       name,
			Version:    version,
			Ecosystem:  mapped.Ecosystem,
			Package:    mapped.Package,
			Confidence: "medium",
		})
	}
	return packages
}

func curatedHomebrewAdvisoryMapping(kind string, name string) (homebrewAdvisoryMapping, bool) {
	mapping, ok := curatedHomebrewAdvisoryMappings[strings.ToLower(kind+":"+name)]
	return mapping, ok
}

func githubRepoGitURL(repo string) string {
	return "https://github.com/" + repo + ".git"
}

func homebrewAPIURL() string {
	return configuredEnvString(defaultHomebrewAPIURL, "UPDEV_HOMEBREW_API_URL")
}

func hasHomebrewPostureReview(postures []homebrewPosture) bool {
	return homebrewPostureReviewCount(postures) > 0
}

func homebrewPostureReviewCount(postures []homebrewPosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
