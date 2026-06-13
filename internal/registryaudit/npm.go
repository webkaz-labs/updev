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

type NPMPosture struct {
	Provider      string            `json:"provider"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Package       string            `json:"package"`
	Version       string            `json:"version,omitempty"`
	Latest        string            `json:"latest,omitempty"`
	PublishedDate string            `json:"published_date,omitempty"`
	ModifiedDate  string            `json:"modified_date,omitempty"`
	RepositoryURL string            `json:"repository_url,omitempty"`
	Binaries      []string          `json:"binaries,omitempty"`
	Maintainers   int               `json:"maintainers,omitempty"`
	Deprecated    string            `json:"deprecated,omitempty"`
	Decision      string            `json:"decision"`
	Confidence    string            `json:"confidence"`
	Reason        string            `json:"reason,omitempty"`
	ReasonCode    string            `json:"reason_code,omitempty"`
	ReasonArgs    map[string]string `json:"reason_args,omitempty"`
	Remediation   string            `json:"remediation,omitempty"`
	Evidence      []string          `json:"evidence,omitempty"`
	URL           string            `json:"url,omitempty"`
}

type NPMPackageMetadata struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Repository  NPMRepository             `json:"repository"`
	Maintainers []NPMMaintainer           `json:"maintainers"`
	DistTags    map[string]string         `json:"dist-tags"`
	Time        map[string]string         `json:"time"`
	Versions    map[string]NPMVersionInfo `json:"versions"`
	Deprecated  string                    `json:"deprecated"`
	Bin         NPMBin                    `json:"bin"`
}

type NPMVersionInfo struct {
	Version    string        `json:"version"`
	Repository NPMRepository `json:"repository"`
	Deprecated string        `json:"deprecated"`
	Bin        NPMBin        `json:"bin"`
}

type NPMRepository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func (repository *NPMRepository) UnmarshalJSON(data []byte) error {
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

type NPMMaintainer struct {
	Name string `json:"name"`
}

type NPMBin struct {
	Value string
	Names []string
}

func (bin *NPMBin) UnmarshalJSON(data []byte) error {
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

func NPMPosturesFromItems(ctx context.Context, client *http.Client, registryBase string, items []plan.Item) ([]NPMPosture, error) {
	type requestedPackage struct {
		name    string
		pkg     string
		version string
	}
	requests := []requestedPackage{}
	seen := map[string]bool{}
	for _, item := range items {
		pkg, ok := NPMPackageFromItem(item)
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
	postures := make([]NPMPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := FetchNPMMetadata(ctx, client, registryBase, request.pkg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.pkg, err))
			postures = append(postures, NPMPostureUnavailable(request.name, request.pkg, request.version, err))
			continue
		}
		postures = append(postures, NPMPostureFromMetadata(request.name, request.pkg, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Package < postures[j].Package
	})
	return postures, summarizeErrors(errs, 3)
}

func NPMPackageFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "npm:") {
		return "", false
	}
	pkg := strings.TrimSpace(strings.TrimPrefix(item.Name, "npm:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r") {
		return "", false
	}
	return pkg, true
}

func FetchNPMMetadata(ctx context.Context, client *http.Client, registryBase string, pkg string) (NPMPackageMetadata, error) {
	cacheKey := securitygate.CacheKey(registryBase, strings.ToLower(pkg))
	var cached NPMPackageMetadata
	if securitygate.LoadMetadataCache("npm", cacheKey, securitygate.RegistryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(registryBase, "/") + "/" + NPMRegistryPackagePath(pkg)
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return NPMPackageMetadata{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return NPMPackageMetadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return NPMPackageMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return NPMPackageMetadata{}, fmt.Errorf("npm registry query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata NPMPackageMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return NPMPackageMetadata{}, err
	}
	securitygate.SaveMetadataCache("npm", cacheKey, metadata)
	return metadata, nil
}

func NPMRegistryPackagePath(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		return strings.ReplaceAll(pkg, "/", "%2f")
	}
	return pkg
}

func NPMPostureFromMetadata(requestedName string, pkg string, version string, metadata NPMPackageMetadata) NPMPosture {
	versionInfo := metadata.Versions[version]
	deprecated := firstNonEmpty(versionInfo.Deprecated, metadata.Deprecated)
	posture := NPMPosture{
		Provider:      "mise",
		Kind:          "npm",
		Name:          requestedName,
		Package:       pkg,
		Version:       version,
		Latest:        metadata.DistTags["latest"],
		PublishedDate: metadata.Time[version],
		ModifiedDate:  metadata.Time["modified"],
		RepositoryURL: NormalizeNPMRepositoryURL(firstNonEmpty(versionInfo.Repository.URL, metadata.Repository.URL)),
		Binaries:      NPMBinariesFromMetadata(pkg, versionInfo, metadata),
		Maintainers:   len(metadata.Maintainers),
		Deprecated:    deprecated,
		Decision:      "allow",
		Confidence:    "medium",
		URL:           "https://www.npmjs.com/package/" + pkg,
	}
	switch {
	case deprecated != "":
		posture.Decision = "review"
		setNPMPostureReason(&posture, securityreason.RegistryVersionDeprecated, "npm package version is deprecated: "+deprecated)
		posture.Remediation = "replace the deprecated npm package version or update to a non-deprecated version after review"
	case version != "" && versionInfo.Version == "":
		posture.Decision = "review"
		setNPMPostureReason(&posture, securityreason.RegistryVersionMissing, "installed npm version is not present in registry metadata")
		posture.Remediation = "verify the installed version and update to a registry version before allowing it"
	case len(metadata.Maintainers) == 0:
		posture.Decision = "review"
		setNPMPostureReason(&posture, securityreason.RegistryNoMaintainers, "npm package has no maintainers in registry metadata")
		posture.Remediation = "review package ownership and source provenance before keeping this package"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		setNPMPostureReason(&posture, securityreason.RegistryMissingRepository, "npm package does not expose a source repository URL")
		posture.Remediation = "review package provenance manually before adding a temporary policy override"
	}
	return posture
}

func setNPMPostureReason(posture *NPMPosture, code string, text string) {
	if posture == nil {
		return
	}
	posture.Reason, posture.ReasonCode, posture.ReasonArgs = registryPostureReasonFields(code, "npm", posture.Package, posture.Version, text)
}

func NPMBinariesFromMetadata(pkg string, versionInfo NPMVersionInfo, metadata NPMPackageMetadata) []string {
	if names := NPMBinNames(pkg, versionInfo.Bin); len(names) > 0 {
		return names
	}
	return NPMBinNames(pkg, metadata.Bin)
}

func NPMBinNames(pkg string, bin NPMBin) []string {
	switch {
	case len(bin.Names) > 0:
		return append([]string(nil), bin.Names...)
	case bin.Value != "":
		return []string{NPMDefaultBinaryName(pkg)}
	default:
		return nil
	}
}

func NPMDefaultBinaryName(pkg string) string {
	name := strings.TrimSpace(pkg)
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		name = parts[len(parts)-1]
	}
	return strings.TrimPrefix(name, "@")
}

func NPMPostureUnavailable(requestedName string, pkg string, version string, err error) NPMPosture {
	posture := NPMPosture{
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
	setNPMPostureReason(&posture, securityreason.RegistryMetadataUnavailable, posture.Reason)
	return posture
}

func NormalizeNPMRepositoryURL(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	value = strings.TrimPrefix(value, "git+")
	value = strings.TrimSuffix(value, ".git")
	return value
}
