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

const cratesIOCratePageURLBase = "https://crates.io/crates/"

type CargoPosture struct {
	Provider      string            `json:"provider"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Crate         string            `json:"crate"`
	Version       string            `json:"version,omitempty"`
	Latest        string            `json:"latest,omitempty"`
	PublishedDate string            `json:"published_date,omitempty"`
	UpdatedDate   string            `json:"updated_date,omitempty"`
	RepositoryURL string            `json:"repository_url,omitempty"`
	Downloads     int               `json:"downloads,omitempty"`
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

type CratesIOResponse struct {
	Crate    CratesIOCrate     `json:"crate"`
	Versions []CratesIOVersion `json:"versions"`
}

type CratesIOCrate struct {
	ID         string `json:"id"`
	MaxVersion string `json:"max_version"`
	Repository string `json:"repository"`
	UpdatedAt  string `json:"updated_at"`
	Downloads  int    `json:"downloads"`
}

type CratesIOVersion struct {
	Num       string `json:"num"`
	Yanked    bool   `json:"yanked"`
	CreatedAt string `json:"created_at"`
}

func CargoPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item) ([]CargoPosture, error) {
	type requestedCrate struct {
		name    string
		crate   string
		version string
	}
	requests := []requestedCrate{}
	seen := map[string]bool{}
	for _, item := range items {
		crate, ok := CargoCrateFromItem(item)
		if !ok {
			continue
		}
		key := strings.ToLower(crate)
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, requestedCrate{name: item.Name, crate: crate, version: item.Version})
	}
	postures := make([]CargoPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := FetchCratesIOMetadata(ctx, client, apiBase, request.crate)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.crate, err))
			postures = append(postures, CargoPostureUnavailable(request.name, request.crate, request.version, err))
			continue
		}
		postures = append(postures, CargoPostureFromMetadata(request.name, request.crate, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Crate < postures[j].Crate
	})
	return postures, summarizeErrors(errs, 3)
}

func CargoCrateFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "cargo:") {
		return "", false
	}
	crate := strings.TrimSpace(strings.TrimPrefix(item.Name, "cargo:"))
	if crate == "" || strings.ContainsAny(crate, " \t\n\r/") {
		return "", false
	}
	return crate, true
}

func FetchCratesIOMetadata(ctx context.Context, client *http.Client, apiBase string, crate string) (CratesIOResponse, error) {
	cacheKey := securitygate.CacheKey(apiBase, strings.ToLower(crate))
	var cached CratesIOResponse
	if securitygate.LoadMetadataCache("crates-io", cacheKey, securitygate.RegistryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/api/v1/crates/" + crate
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CratesIOResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "updev security scan")
	response, err := client.Do(request)
	if err != nil {
		return CratesIOResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return CratesIOResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CratesIOResponse{}, fmt.Errorf("crates.io query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata CratesIOResponse
	if err := json.Unmarshal(body, &metadata); err != nil {
		return CratesIOResponse{}, err
	}
	securitygate.SaveMetadataCache("crates-io", cacheKey, metadata)
	return metadata, nil
}

func CargoPostureFromMetadata(requestedName string, crate string, version string, metadata CratesIOResponse) CargoPosture {
	versionInfo, versionFound := CratesIOVersionByNumber(metadata.Versions, version)
	posture := CargoPosture{
		Provider:      "mise",
		Kind:          "cargo",
		Name:          requestedName,
		Crate:         crate,
		Version:       version,
		Latest:        metadata.Crate.MaxVersion,
		PublishedDate: versionInfo.CreatedAt,
		UpdatedDate:   metadata.Crate.UpdatedAt,
		RepositoryURL: metadata.Crate.Repository,
		Downloads:     metadata.Crate.Downloads,
		Yanked:        versionInfo.Yanked,
		Decision:      "allow",
		Confidence:    "medium",
		URL:           CratesIOCratePageURL(crate),
	}
	switch {
	case version != "" && !versionFound:
		posture.Decision = "review"
		setCargoPostureReason(&posture, securityreason.RegistryVersionMissing, "installed crate version is not present in crates.io metadata")
		posture.Remediation = "verify the installed crate version and update to a crates.io version before allowing it"
	case versionInfo.Yanked:
		posture.Decision = "review"
		setCargoPostureReason(&posture, securityreason.RegistryVersionYanked, "installed crate version is yanked")
		posture.Remediation = "update to a non-yanked crate version or replace the crate"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		setCargoPostureReason(&posture, securityreason.RegistryMissingRepository, "crate does not expose a source repository URL")
		posture.Remediation = "review crate provenance manually before adding a temporary policy override"
	}
	return posture
}

func setCargoPostureReason(posture *CargoPosture, code string, text string) {
	if posture == nil {
		return
	}
	posture.Reason, posture.ReasonCode, posture.ReasonArgs = registryPostureReasonFields(code, "crates.io", posture.Crate, posture.Version, text)
}

func CratesIOVersionByNumber(versions []CratesIOVersion, version string) (CratesIOVersion, bool) {
	for _, candidate := range versions {
		if candidate.Num == version {
			return candidate, true
		}
	}
	return CratesIOVersion{}, false
}

func CargoPostureUnavailable(requestedName string, crate string, version string, err error) CargoPosture {
	posture := CargoPosture{
		Provider:   "mise",
		Kind:       "cargo",
		Name:       requestedName,
		Crate:      crate,
		Version:    version,
		Decision:   "review",
		Confidence: "low",
		Reason:     "crates.io metadata unavailable: " + err.Error(),
		URL:        CratesIOCratePageURL(crate),
	}
	setCargoPostureReason(&posture, securityreason.RegistryMetadataUnavailable, posture.Reason)
	return posture
}

func CratesIOCratePageURL(crate string) string {
	return cratesIOCratePageURLBase + crate
}
