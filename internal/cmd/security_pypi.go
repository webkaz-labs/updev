package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const defaultPyPIAPIURL = "https://pypi.org/pypi"

type pypiPosture struct {
	Provider      string   `json:"provider"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Package       string   `json:"package"`
	Version       string   `json:"version,omitempty"`
	Latest        string   `json:"latest,omitempty"`
	PublishedDate string   `json:"published_date,omitempty"`
	ProjectURL    string   `json:"project_url,omitempty"`
	RepositoryURL string   `json:"repository_url,omitempty"`
	Yanked        bool     `json:"yanked"`
	Decision      string   `json:"decision"`
	Confidence    string   `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
	Remediation   string   `json:"remediation,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	URL           string   `json:"url,omitempty"`
}

type pypiPackageMetadata struct {
	Info     pypiInfo                 `json:"info"`
	Releases map[string][]pypiRelease `json:"releases"`
}

type pypiInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	ProjectURL  string            `json:"project_url"`
	ProjectURLs map[string]string `json:"project_urls"`
}

type pypiRelease struct {
	UploadTimeISO8601 string `json:"upload_time_iso_8601"`
	Yanked            bool   `json:"yanked"`
	YankedReason      string `json:"yanked_reason"`
}

func pypiPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item) ([]pypiPosture, error) {
	type requestedPackage struct {
		name    string
		pkg     string
		version string
	}
	requests := []requestedPackage{}
	seen := map[string]bool{}
	for _, item := range items {
		pkg, ok := pypiPackageFromItem(item)
		if !ok {
			continue
		}
		key := strings.ToLower(pkg)
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, requestedPackage{name: item.Name, pkg: pkg, version: item.Version})
	}
	postures := make([]pypiPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := fetchPyPIMetadata(ctx, client, apiBase, request.pkg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.pkg, err))
			postures = append(postures, pypiPostureUnavailable(request.name, request.pkg, request.version, err))
			continue
		}
		postures = append(postures, pypiPostureFromMetadata(request.name, request.pkg, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Package < postures[j].Package
	})
	return postures, summarizeErrors(errs, 3)
}

func pypiPackageFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "pipx:") {
		return "", false
	}
	pkg := strings.TrimSpace(strings.TrimPrefix(item.Name, "pipx:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r/") {
		return "", false
	}
	return pkg, true
}

func fetchPyPIMetadata(ctx context.Context, client *http.Client, apiBase string, pkg string) (pypiPackageMetadata, error) {
	cacheKey := updateSafetyCacheKey(apiBase, strings.ToLower(pkg))
	var cached pypiPackageMetadata
	if loadSecurityMetadataCache("pypi", cacheKey, registryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/" + pkg + "/json"
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return pypiPackageMetadata{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return pypiPackageMetadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return pypiPackageMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return pypiPackageMetadata{}, fmt.Errorf("PyPI query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata pypiPackageMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return pypiPackageMetadata{}, err
	}
	saveSecurityMetadataCache("pypi", cacheKey, metadata)
	return metadata, nil
}

func pypiPostureFromMetadata(requestedName string, pkg string, version string, metadata pypiPackageMetadata) pypiPosture {
	release, releaseFound := pypiReleaseForVersion(metadata.Releases, version)
	posture := pypiPosture{
		Provider:      "mise",
		Kind:          "pipx",
		Name:          requestedName,
		Package:       pkg,
		Version:       version,
		Latest:        metadata.Info.Version,
		PublishedDate: release.UploadTimeISO8601,
		ProjectURL:    firstNonEmpty(metadata.Info.ProjectURL, "https://pypi.org/project/"+pkg),
		RepositoryURL: pypiRepositoryURL(metadata.Info.ProjectURLs),
		Yanked:        release.Yanked,
		Decision:      "allow",
		Confidence:    "medium",
		URL:           "https://pypi.org/project/" + pkg,
	}
	switch {
	case version != "" && !releaseFound:
		posture.Decision = "review"
		posture.Reason = "installed PyPI version is not present in metadata"
		posture.Remediation = "verify the installed Python package version and update to a PyPI release before allowing it"
	case release.Yanked:
		posture.Decision = "review"
		posture.Reason = "installed PyPI version is yanked"
		if release.YankedReason != "" {
			posture.Reason += ": " + release.YankedReason
		}
		posture.Remediation = "update to a non-yanked PyPI release or replace the package"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = "PyPI package does not expose a source repository URL"
		posture.Remediation = "review package provenance manually before adding a temporary policy override"
	}
	return posture
}

func pypiReleaseForVersion(releases map[string][]pypiRelease, version string) (pypiRelease, bool) {
	candidates := releases[version]
	if len(candidates) == 0 {
		return pypiRelease{}, false
	}
	for _, candidate := range candidates {
		if candidate.Yanked {
			return candidate, true
		}
	}
	return candidates[0], true
}

func pypiRepositoryURL(projectURLs map[string]string) string {
	for _, key := range []string{"Source", "Source Code", "Repository", "Homepage", "Code"} {
		if value := strings.TrimSpace(projectURLs[key]); value != "" {
			return value
		}
	}
	return ""
}

func pypiPostureUnavailable(requestedName string, pkg string, version string, err error) pypiPosture {
	return pypiPosture{
		Provider:   "mise",
		Kind:       "pipx",
		Name:       requestedName,
		Package:    pkg,
		Version:    version,
		Decision:   "review",
		Confidence: "low",
		Reason:     "PyPI metadata unavailable: " + err.Error(),
		URL:        "https://pypi.org/project/" + pkg,
	}
}

func pypiAPIURL() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_PYPI_API_URL")); value != "" {
		return value
	}
	return defaultPyPIAPIURL
}

func hasPyPIPostureReview(postures []pypiPosture) bool {
	return pypiPostureReviewCount(postures) > 0
}

func pypiPostureReviewCount(postures []pypiPosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
