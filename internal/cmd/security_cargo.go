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

const defaultCratesIOAPIURL = "https://crates.io"

type cargoPosture struct {
	Provider      string   `json:"provider"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Crate         string   `json:"crate"`
	Version       string   `json:"version,omitempty"`
	Latest        string   `json:"latest,omitempty"`
	PublishedDate string   `json:"published_date,omitempty"`
	UpdatedDate   string   `json:"updated_date,omitempty"`
	RepositoryURL string   `json:"repository_url,omitempty"`
	Downloads     int      `json:"downloads,omitempty"`
	Yanked        bool     `json:"yanked"`
	Decision      string   `json:"decision"`
	Confidence    string   `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
	Remediation   string   `json:"remediation,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	URL           string   `json:"url,omitempty"`
}

type cratesIOResponse struct {
	Crate    cratesIOCrate     `json:"crate"`
	Versions []cratesIOVersion `json:"versions"`
}

type cratesIOCrate struct {
	ID         string `json:"id"`
	MaxVersion string `json:"max_version"`
	Repository string `json:"repository"`
	UpdatedAt  string `json:"updated_at"`
	Downloads  int    `json:"downloads"`
}

type cratesIOVersion struct {
	Num       string `json:"num"`
	Yanked    bool   `json:"yanked"`
	CreatedAt string `json:"created_at"`
}

func cargoPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item) ([]cargoPosture, error) {
	type requestedCrate struct {
		name    string
		crate   string
		version string
	}
	requests := []requestedCrate{}
	seen := map[string]bool{}
	for _, item := range items {
		crate, ok := cargoCrateFromItem(item)
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
	postures := make([]cargoPosture, 0, len(requests))
	errs := []error{}
	for _, request := range requests {
		metadata, err := fetchCratesIOMetadata(ctx, client, apiBase, request.crate)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", request.crate, err))
			postures = append(postures, cargoPostureUnavailable(request.name, request.crate, request.version, err))
			continue
		}
		postures = append(postures, cargoPostureFromMetadata(request.name, request.crate, request.version, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Crate < postures[j].Crate
	})
	return postures, summarizeErrors(errs, 3)
}

func cargoCrateFromItem(item plan.Item) (string, bool) {
	if item.Provider != "mise" || !strings.HasPrefix(item.Name, "cargo:") {
		return "", false
	}
	crate := strings.TrimSpace(strings.TrimPrefix(item.Name, "cargo:"))
	if crate == "" || strings.ContainsAny(crate, " \t\n\r/") {
		return "", false
	}
	return crate, true
}

func fetchCratesIOMetadata(ctx context.Context, client *http.Client, apiBase string, crate string) (cratesIOResponse, error) {
	cacheKey := updateSafetyCacheKey(apiBase, strings.ToLower(crate))
	var cached cratesIOResponse
	if loadSecurityMetadataCache("crates-io", cacheKey, registryMetadataCacheMaxAge, &cached) {
		return cached, nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/api/v1/crates/" + crate
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return cratesIOResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "updev security scan")
	response, err := client.Do(request)
	if err != nil {
		return cratesIOResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return cratesIOResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return cratesIOResponse{}, fmt.Errorf("crates.io query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata cratesIOResponse
	if err := json.Unmarshal(body, &metadata); err != nil {
		return cratesIOResponse{}, err
	}
	saveSecurityMetadataCache("crates-io", cacheKey, metadata)
	return metadata, nil
}

func cargoPostureFromMetadata(requestedName string, crate string, version string, metadata cratesIOResponse) cargoPosture {
	versionInfo, versionFound := cratesIOVersionByNumber(metadata.Versions, version)
	posture := cargoPosture{
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
		URL:           "https://crates.io/crates/" + crate,
	}
	switch {
	case version != "" && !versionFound:
		posture.Decision = "review"
		posture.Reason = "installed crate version is not present in crates.io metadata"
		posture.Remediation = "verify the installed crate version and update to a crates.io version before allowing it"
	case versionInfo.Yanked:
		posture.Decision = "review"
		posture.Reason = "installed crate version is yanked"
		posture.Remediation = "update to a non-yanked crate version or replace the crate"
	case posture.RepositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = "crate does not expose a source repository URL"
		posture.Remediation = "review crate provenance manually before adding a temporary policy override"
	}
	return posture
}

func cratesIOVersionByNumber(versions []cratesIOVersion, version string) (cratesIOVersion, bool) {
	for _, candidate := range versions {
		if candidate.Num == version {
			return candidate, true
		}
	}
	return cratesIOVersion{}, false
}

func cargoPostureUnavailable(requestedName string, crate string, version string, err error) cargoPosture {
	return cargoPosture{
		Provider:   "mise",
		Kind:       "cargo",
		Name:       requestedName,
		Crate:      crate,
		Version:    version,
		Decision:   "review",
		Confidence: "low",
		Reason:     "crates.io metadata unavailable: " + err.Error(),
		URL:        "https://crates.io/crates/" + crate,
	}
}

func cratesIOAPIURL() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_CRATES_IO_API_URL")); value != "" {
		return value
	}
	return defaultCratesIOAPIURL
}

func hasCargoPostureReview(postures []cargoPosture) bool {
	return cargoPostureReviewCount(postures) > 0
}

func cargoPostureReviewCount(postures []cargoPosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
