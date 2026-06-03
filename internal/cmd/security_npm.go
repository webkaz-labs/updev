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

const defaultNPMRegistryURL = "https://registry.npmjs.org"

type npmPosture struct {
	Provider      string   `json:"provider"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Package       string   `json:"package"`
	Version       string   `json:"version,omitempty"`
	Latest        string   `json:"latest,omitempty"`
	PublishedDate string   `json:"published_date,omitempty"`
	ModifiedDate  string   `json:"modified_date,omitempty"`
	RepositoryURL string   `json:"repository_url,omitempty"`
	Binaries      []string `json:"binaries,omitempty"`
	Maintainers   int      `json:"maintainers,omitempty"`
	Deprecated    string   `json:"deprecated,omitempty"`
	Decision      string   `json:"decision"`
	Confidence    string   `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
	Remediation   string   `json:"remediation,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	URL           string   `json:"url,omitempty"`
}

type npmPackageMetadata struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Repository  npmRepository             `json:"repository"`
	Maintainers []npmMaintainer           `json:"maintainers"`
	DistTags    map[string]string         `json:"dist-tags"`
	Time        map[string]string         `json:"time"`
	Versions    map[string]npmVersionInfo `json:"versions"`
	Deprecated  string                    `json:"deprecated"`
	Bin         npmBin                    `json:"bin"`
}

type npmVersionInfo struct {
	Version    string        `json:"version"`
	Repository npmRepository `json:"repository"`
	Deprecated string        `json:"deprecated"`
	Bin        npmBin        `json:"bin"`
}

type npmRepository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func (repository *npmRepository) UnmarshalJSON(data []byte) error {
	var object struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(data, &object); err == nil {
		repository.Type = object.Type
		repository.URL = object.URL
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	repository.URL = raw
	return nil
}

type npmMaintainer struct {
	Name string `json:"name"`
}

type npmBin struct {
	Value string
	Names []string
}

func (bin *npmBin) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err == nil {
		bin.Value = strings.TrimSpace(raw)
		return nil
	}
	var object map[string]string
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	names := make([]string, 0, len(object))
	for name := range object {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	bin.Names = names
	return nil
}

func npmPosturesFromItems(ctx context.Context, client *http.Client, registryBase string, items []plan.Item) ([]npmPosture, error) {
	type requestedPackage struct {
		name    string
		pkg     string
		version string
	}
	requests := []requestedPackage{}
	seen := map[string]bool{}
	for _, item := range items {
		pkg, ok := npmPackageFromItem(item)
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
	postures := make([]npmPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := fetchNPMMetadata(ctx, client, registryBase, request.pkg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.pkg, err))
			postures = append(postures, npmPostureUnavailable(request.name, request.pkg, request.version, err))
			continue
		}
		postures = append(postures, npmPostureFromMetadata(request.name, request.pkg, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Package < postures[j].Package
	})
	return postures, summarizeErrors(errs, 3)
}

func npmPackageFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "npm:") {
		return "", false
	}
	pkg := strings.TrimSpace(strings.TrimPrefix(item.Name, "npm:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r") {
		return "", false
	}
	return pkg, true
}

func fetchNPMMetadata(ctx context.Context, client *http.Client, registryBase string, pkg string) (npmPackageMetadata, error) {
	cacheKey := updateSafetyCacheKey(registryBase, strings.ToLower(pkg))
	var cached npmPackageMetadata
	if loadSecurityMetadataCache("npm", cacheKey, registryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(registryBase, "/") + "/" + npmRegistryPackagePath(pkg)
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return npmPackageMetadata{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return npmPackageMetadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return npmPackageMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return npmPackageMetadata{}, fmt.Errorf("npm registry query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata npmPackageMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return npmPackageMetadata{}, err
	}
	saveSecurityMetadataCache("npm", cacheKey, metadata)
	return metadata, nil
}

func npmRegistryPackagePath(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		return strings.ReplaceAll(pkg, "/", "%2f")
	}
	return pkg
}

func npmPostureFromMetadata(requestedName string, pkg string, version string, metadata npmPackageMetadata) npmPosture {
	versionInfo := metadata.Versions[version]
	deprecated := firstNonEmpty(versionInfo.Deprecated, metadata.Deprecated)
	posture := npmPosture{
		Provider:      "mise",
		Kind:          "npm",
		Name:          requestedName,
		Package:       pkg,
		Version:       version,
		Latest:        metadata.DistTags["latest"],
		PublishedDate: metadata.Time[version],
		ModifiedDate:  metadata.Time["modified"],
		RepositoryURL: normalizeNPMRepositoryURL(firstNonEmpty(versionInfo.Repository.URL, metadata.Repository.URL)),
		Binaries:      npmBinariesFromMetadata(pkg, versionInfo, metadata),
		Maintainers:   len(metadata.Maintainers),
		Deprecated:    deprecated,
		Decision:      "allow",
		Confidence:    "medium",
		URL:           "https://www.npmjs.com/package/" + pkg,
	}
	switch {
	case deprecated != "":
		posture.Decision = "review"
		posture.Reason = "npm package version is deprecated: " + deprecated
		posture.Remediation = "replace the deprecated npm package version or update to a non-deprecated version after review"
	case version != "" && versionInfo.Version == "":
		posture.Decision = "review"
		posture.Reason = "installed npm version is not present in registry metadata"
		posture.Remediation = "verify the installed version and update to a registry version before allowing it"
	case len(metadata.Maintainers) == 0:
		posture.Decision = "review"
		posture.Reason = "npm package has no maintainers in registry metadata"
		posture.Remediation = "review package ownership and source provenance before keeping this package"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = "npm package does not expose a source repository URL"
		posture.Remediation = "review package provenance manually before adding a temporary policy override"
	}
	return posture
}

func npmBinariesFromMetadata(pkg string, versionInfo npmVersionInfo, metadata npmPackageMetadata) []string {
	if names := npmBinNames(pkg, versionInfo.Bin); len(names) > 0 {
		return names
	}
	return npmBinNames(pkg, metadata.Bin)
}

func npmBinNames(pkg string, bin npmBin) []string {
	switch {
	case len(bin.Names) > 0:
		return append([]string(nil), bin.Names...)
	case bin.Value != "":
		return []string{npmDefaultBinaryName(pkg)}
	default:
		return nil
	}
}

func npmDefaultBinaryName(pkg string) string {
	name := strings.TrimSpace(pkg)
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		name = parts[len(parts)-1]
	}
	return strings.TrimPrefix(name, "@")
}

func npmPostureUnavailable(requestedName string, pkg string, version string, err error) npmPosture {
	return npmPosture{
		Provider:   "mise",
		Kind:       "npm",
		Name:       requestedName,
		Package:    pkg,
		Version:    version,
		Decision:   "review",
		Confidence: "low",
		Reason:     "npm registry metadata unavailable: " + err.Error(),
		URL:        "https://www.npmjs.com/package/" + pkg,
	}
}

func normalizeNPMRepositoryURL(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	value = strings.TrimPrefix(value, "git+")
	value = strings.TrimSuffix(value, ".git")
	return value
}

func npmRegistryURL() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_NPM_REGISTRY_URL")); value != "" {
		return value
	}
	return defaultNPMRegistryURL
}

func hasNPMPostureReview(postures []npmPosture) bool {
	return npmPostureReviewCount(postures) > 0
}

func npmPostureReviewCount(postures []npmPosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
