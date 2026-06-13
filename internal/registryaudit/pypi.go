package registryaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

type PyPIPosture struct {
	Provider      string            `json:"provider"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Package       string            `json:"package"`
	Version       string            `json:"version,omitempty"`
	Latest        string            `json:"latest,omitempty"`
	PublishedDate string            `json:"published_date,omitempty"`
	ProjectURL    string            `json:"project_url,omitempty"`
	RepositoryURL string            `json:"repository_url,omitempty"`
	Yanked        bool              `json:"yanked"`
	Decision      string            `json:"decision"`
	Confidence    string            `json:"confidence"`
	Reason        string            `json:"reason,omitempty"`
	ReasonCode    string            `json:"reason_code,omitempty"`
	ReasonArgs    map[string]string `json:"reason_args,omitempty"`
	Remediation   string            `json:"remediation,omitempty"`
	Evidence      []string          `json:"evidence,omitempty"`
	URL           string            `json:"url,omitempty"`
}

type PyPIPackageMetadata struct {
	Info     PyPIInfo                 `json:"info"`
	Releases map[string][]PyPIRelease `json:"releases"`
}

type PyPIInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	ProjectURL  string            `json:"project_url"`
	ProjectURLs map[string]string `json:"project_urls"`
}

type PyPIRelease struct {
	UploadTimeISO8601 string `json:"upload_time_iso_8601"`
	Yanked            bool   `json:"yanked"`
	YankedReason      string `json:"yanked_reason"`
}

func PyPIPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item) ([]PyPIPosture, error) {
	type requestedPackage struct {
		name    string
		pkg     string
		version string
	}
	requests := []requestedPackage{}
	seen := map[string]bool{}
	for _, item := range items {
		pkg, ok := PyPIPackageFromItem(item)
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
	postures := make([]PyPIPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := FetchPyPIMetadata(ctx, client, apiBase, request.pkg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.pkg, err))
			postures = append(postures, PyPIPostureUnavailable(request.name, request.pkg, request.version, err))
			continue
		}
		postures = append(postures, PyPIPostureFromMetadata(request.name, request.pkg, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Package < postures[j].Package
	})
	return postures, summarizeErrors(errs, 3)
}

func PyPIPackageFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "pipx:") {
		return "", false
	}
	pkg := strings.TrimSpace(strings.TrimPrefix(item.Name, "pipx:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r/") {
		return "", false
	}
	return pkg, true
}

func FetchPyPIMetadata(ctx context.Context, client *http.Client, apiBase string, pkg string) (PyPIPackageMetadata, error) {
	cacheKey := securitygate.CacheKey(apiBase, strings.ToLower(pkg))
	var cached PyPIPackageMetadata
	if securitygate.LoadMetadataCache("pypi", cacheKey, securitygate.RegistryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/" + pkg + "/json"
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PyPIPackageMetadata{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return PyPIPackageMetadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return PyPIPackageMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PyPIPackageMetadata{}, fmt.Errorf("PyPI query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata PyPIPackageMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return PyPIPackageMetadata{}, err
	}
	securitygate.SaveMetadataCache("pypi", cacheKey, metadata)
	return metadata, nil
}

func PyPIPostureFromMetadata(requestedName string, pkg string, version string, metadata PyPIPackageMetadata) PyPIPosture {
	release, releaseFound := PyPIReleaseForVersion(metadata.Releases, version)
	posture := PyPIPosture{
		Provider:      "mise",
		Kind:          "pipx",
		Name:          requestedName,
		Package:       pkg,
		Version:       version,
		Latest:        metadata.Info.Version,
		PublishedDate: release.UploadTimeISO8601,
		ProjectURL:    firstNonEmpty(metadata.Info.ProjectURL, "https://pypi.org/project/"+pkg),
		RepositoryURL: PyPIRepositoryURL(metadata.Info.ProjectURLs),
		Yanked:        release.Yanked,
		Decision:      "allow",
		Confidence:    "medium",
		URL:           "https://pypi.org/project/" + pkg,
	}
	switch {
	case version != "" && !releaseFound:
		posture.Decision = "review"
		setPyPIPostureReason(&posture, securityreason.RegistryVersionMissing, "installed PyPI version is not present in metadata")
		posture.Remediation = "verify the installed Python package version and update to a PyPI release before allowing it"
	case release.Yanked:
		posture.Decision = "review"
		reason := "installed PyPI version is yanked"
		if release.YankedReason != "" {
			reason += ": " + release.YankedReason
		}
		setPyPIPostureReason(&posture, securityreason.RegistryVersionYanked, reason)
		posture.Remediation = "update to a non-yanked PyPI release or replace the package"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		setPyPIPostureReason(&posture, securityreason.RegistryMissingRepository, "PyPI package does not expose a source repository URL")
		posture.Remediation = "review package provenance manually before adding a temporary policy override"
	}
	return posture
}

func setPyPIPostureReason(posture *PyPIPosture, code string, text string) {
	if posture == nil {
		return
	}
	posture.Reason, posture.ReasonCode, posture.ReasonArgs = registryPostureReasonFields(code, "PyPI", posture.Package, posture.Version, text)
}

func PyPIReleaseForVersion(releases map[string][]PyPIRelease, version string) (PyPIRelease, bool) {
	candidates := releases[version]
	if len(candidates) == 0 {
		return PyPIRelease{}, false
	}
	for _, candidate := range candidates {
		if candidate.Yanked {
			return candidate, true
		}
	}
	return candidates[0], true
}

func PyPIRepositoryURL(projectURLs map[string]string) string {
	for _, key := range []string{"Source", "Source Code", "Repository", "Homepage", "Code"} {
		if value := strings.TrimSpace(projectURLs[key]); value != "" {
			return value
		}
	}
	return ""
}

func PyPIPostureUnavailable(requestedName string, pkg string, version string, err error) PyPIPosture {
	posture := PyPIPosture{
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
	setPyPIPostureReason(&posture, securityreason.RegistryMetadataUnavailable, posture.Reason)
	return posture
}
