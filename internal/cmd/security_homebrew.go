package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const defaultHomebrewAPIURL = "https://formulae.brew.sh/api"

type homebrewPosture struct {
	Provider      string   `json:"provider"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Tap           string   `json:"tap,omitempty"`
	Homepage      string   `json:"homepage,omitempty"`
	URL           string   `json:"url,omitempty"`
	HomepageHost  string   `json:"homepage_host,omitempty"`
	URLHost       string   `json:"url_host,omitempty"`
	HostMatched   bool     `json:"host_matched,omitempty"`
	Version       string   `json:"version,omitempty"`
	Deprecated    bool     `json:"deprecated"`
	Disabled      bool     `json:"disabled"`
	SkipLivecheck bool     `json:"skip_livecheck"`
	Autobump      bool     `json:"autobump"`
	Decision      string   `json:"decision"`
	Confidence    string   `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
	Remediation   string   `json:"remediation,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
}

type homebrewMetadata struct {
	Name              flexStringSlice  `json:"name"`
	FullName          string           `json:"full_name"`
	Token             string           `json:"token"`
	FullToken         string           `json:"full_token"`
	Tap               string           `json:"tap"`
	Homepage          string           `json:"homepage"`
	URL               string           `json:"url"`
	Version           string           `json:"version"`
	Versions          homebrewVersions `json:"versions"`
	URLs              homebrewURLs     `json:"urls"`
	Deprecated        bool             `json:"deprecated"`
	Disabled          bool             `json:"disabled"`
	DeprecationDate   string           `json:"deprecation_date"`
	DeprecationReason string           `json:"deprecation_reason"`
	DisableDate       string           `json:"disable_date"`
	DisableReason     string           `json:"disable_reason"`
	SkipLivecheck     bool             `json:"skip_livecheck"`
	Autobump          bool             `json:"autobump"`
}

type homebrewVersions struct {
	Stable string `json:"stable"`
}

type homebrewURLs struct {
	Stable homebrewURL `json:"stable"`
}

type homebrewURL struct {
	URL string `json:"url"`
}

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
	requests := []plan.Item{}
	tapPostures := []homebrewPosture{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Provider == "brew" && item.Kind == "tap" {
			key := item.Kind + ":" + item.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			tapPostures = append(tapPostures, homebrewTapPosture(item))
			continue
		}
		if item.Provider != "brew" || (item.Kind != "brew" && item.Kind != "cask") {
			continue
		}
		key := item.Kind + ":" + item.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, item)
	}
	postures := make([]homebrewPosture, 0, len(requests)+len(tapPostures))
	postures = append(postures, tapPostures...)
	errs := []error{}
	for _, item := range requests {
		entry := manifest.entry(item.Kind, item.Name)
		if entry.URLBased {
			postures = append(postures, homebrewManifestPosture(item, entry, "URL-based Homebrew cask needs manual provenance review"))
			continue
		}
		if entry.Tap != "" && !isOfficialBrewTap(entry.Tap) {
			postures = append(postures, homebrewManifestPosture(item, entry, "non-official Homebrew tap needs provenance review"))
			continue
		}
		metadata, err := fetchHomebrewMetadata(ctx, client, apiBase, item.Kind, item.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s:%s: %w", item.Kind, item.Name, err))
			postures = append(postures, homebrewMetadataUnavailable(item, entry, err))
			continue
		}
		postures = append(postures, homebrewPostureFromMetadata(item, entry, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		if postures[i].Kind != postures[j].Kind {
			return postures[i].Kind < postures[j].Kind
		}
		return postures[i].Name < postures[j].Name
	})
	return postures, errors.Join(errs...)
}

func homebrewTapPosture(item plan.Item) homebrewPosture {
	url := homebrewTapGitHubURL(item.Name)
	posture := homebrewPosture{
		Provider:   "brew",
		Kind:       "tap",
		Name:       item.Name,
		Tap:        item.Name,
		URL:        url,
		Decision:   "allow",
		Confidence: "medium",
		Evidence:   []string{"Brewfile tap"},
	}
	if url != "" {
		posture.Evidence = appendEvidence(posture.Evidence, "inferred Homebrew tap GitHub repository")
	}
	if !isOfficialBrewTap(item.Name) {
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = "non-official Homebrew tap needs provenance review"
		posture.Remediation = "review the tap repository provenance and add a temporary policy override only with reason and expiry"
	}
	return posture
}

func homebrewTapGitHubURL(tap string) string {
	parts := strings.Split(strings.TrimSpace(tap), "/")
	if len(parts) != 2 || !validGitHubPathPart(parts[0]) || !validGitHubPathPart(parts[1]) {
		return ""
	}
	return "https://github.com/" + parts[0] + "/homebrew-" + parts[1]
}

func fetchHomebrewMetadata(ctx context.Context, client *http.Client, apiBase string, kind string, name string) (homebrewMetadata, error) {
	kindPath := "formula"
	if kind == "cask" {
		kindPath = "cask"
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/" + kindPath + "/" + url.PathEscape(name) + ".json"
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return homebrewMetadata{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return homebrewMetadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return homebrewMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return homebrewMetadata{}, fmt.Errorf("homebrew metadata query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata homebrewMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return homebrewMetadata{}, err
	}
	return metadata, nil
}

func homebrewPostureFromMetadata(item plan.Item, entry brewSafetyEntry, metadata homebrewMetadata) homebrewPosture {
	name := firstNonEmpty(metadata.Token, firstFlexString(metadata.Name), item.Name)
	tap := firstNonEmpty(metadata.Tap, entry.Tap)
	version := firstNonEmpty(metadata.Version, metadata.Versions.Stable)
	downloadURL := firstNonEmpty(metadata.URL, metadata.URLs.Stable.URL)
	homepageHost := hostFromURL(metadata.Homepage)
	urlHost := hostFromURL(downloadURL)
	posture := homebrewPosture{
		Provider:      "brew",
		Kind:          item.Kind,
		Name:          name,
		Tap:           tap,
		Homepage:      metadata.Homepage,
		URL:           downloadURL,
		HomepageHost:  homepageHost,
		URLHost:       urlHost,
		HostMatched:   homepageHost != "" && urlHost != "" && homepageHost == urlHost,
		Version:       version,
		Deprecated:    metadata.Deprecated,
		Disabled:      metadata.Disabled,
		SkipLivecheck: metadata.SkipLivecheck,
		Autobump:      metadata.Autobump,
		Decision:      "allow",
		Confidence:    "medium",
	}
	switch {
	case metadata.Disabled:
		posture.Decision = "review"
		posture.Reason = firstNonEmpty(metadata.DisableReason, "Homebrew metadata marks this entry disabled")
		posture.Remediation = "remove or replace the disabled Homebrew entry before updating"
	case metadata.Deprecated:
		posture.Decision = "review"
		posture.Reason = firstNonEmpty(metadata.DeprecationReason, "Homebrew metadata marks this entry deprecated")
		posture.Remediation = "replace the deprecated Homebrew entry or add a temporary policy override after review"
	case item.Kind == "cask":
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = caskProvenanceReason(homepageHost, urlHost)
		posture.Remediation = caskProvenanceRemediation(homepageHost, urlHost)
	}
	return posture
}

func homebrewManifestPosture(item plan.Item, entry brewSafetyEntry, reason string) homebrewPosture {
	name := firstNonEmpty(entry.RawName, item.Name)
	return homebrewPosture{
		Provider:    "brew",
		Kind:        item.Kind,
		Name:        name,
		Tap:         entry.Tap,
		Decision:    "review",
		Confidence:  "low",
		Reason:      reason,
		Remediation: "review the Brewfile entry provenance and add a temporary policy override only with reason and expiry",
	}
}

func homebrewMetadataUnavailable(item plan.Item, entry brewSafetyEntry, err error) homebrewPosture {
	name := firstNonEmpty(entry.RawName, item.Name)
	return homebrewPosture{
		Provider:    "brew",
		Kind:        item.Kind,
		Name:        name,
		Tap:         entry.Tap,
		Decision:    "review",
		Confidence:  "low",
		Reason:      "Homebrew metadata unavailable: " + err.Error(),
		Remediation: "retry when Homebrew metadata is reachable or review the entry manually before adding a policy override",
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
		if repo, tag, ok := githubRepoTagFromURL(rawURL); ok {
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
	if value := strings.TrimSpace(os.Getenv("UPDEV_HOMEBREW_API_URL")); value != "" {
		return value
	}
	return defaultHomebrewAPIURL
}

func firstFlexString(values flexStringSlice) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
