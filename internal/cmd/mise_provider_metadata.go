package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type miseProviderMetadataResolverType string

const miseResolverVendorReleaseNotes miseProviderMetadataResolverType = "vendor_release_notes"

type miseProviderMetadataEntry struct {
	ID               string
	ProviderIdentity string
	ResolverType     miseProviderMetadataResolverType
	URL              string
	EnvURLSuffix     string
	HeadingPattern   string
	Evidence         string
	SupportURL       string
}

func miseProviderMetadataRegistry() []miseProviderMetadataEntry {
	return []miseProviderMetadataEntry{
		{
			ID:               "google-cloud-cli",
			ProviderIdentity: "vfox:mise-plugins/vfox-gcloud",
			ResolverType:     miseResolverVendorReleaseNotes,
			URL:              "https://docs.cloud.google.com/sdk/docs/release-notes",
			EnvURLSuffix:     "GOOGLE_CLOUD_CLI",
			HeadingPattern:   `(?is)(?:^|[>\n])\s*%s\s*\((\d{4}-\d{2}-\d{2})\)`,
			Evidence:         "Google Cloud CLI release notes",
			SupportURL:       "https://docs.cloud.google.com/sdk/docs/install-sdk",
		},
	}
}

func applyMiseRegistryProviderMetadataReleaseAge(ctx context.Context, client *http.Client, registry map[string]miseRegistryEntry, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	entry, ok := miseRegistryEntryForFinding(registry, finding)
	if !ok {
		return finding, false
	}
	backend, metadata, ok := miseRegistryProviderMetadataBackend(entry, miseProviderMetadataRegistry())
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Kind = "tool"
	finding.Evidence = appendEvidence(finding.Evidence, "mise registry backend "+backend)
	finding.Evidence = appendEvidence(finding.Evidence, "provider metadata "+metadata.ID)
	switch metadata.ResolverType {
	case miseResolverVendorReleaseNotes:
		enriched := applyMiseVendorReleaseNotesAge(ctx, client, metadata, finding, minAge)
		enriched.Name = originalName
		return enriched, true
	default:
		finding.Name = originalName
		return miseReviewFinding(finding, "provider metadata resolver is unsupported for mise backend", "keep the candidate held until updev can resolve provider metadata for this backend"), true
	}
}

func miseRegistryProviderMetadataBackend(entry miseRegistryEntry, metadata []miseProviderMetadataEntry) (string, miseProviderMetadataEntry, bool) {
	byIdentity := make(map[string]miseProviderMetadataEntry, len(metadata))
	for _, item := range metadata {
		if key := strings.TrimSpace(item.ProviderIdentity); key != "" {
			byIdentity[key] = item
		}
	}
	for _, backend := range entry.Backends {
		backend = strings.TrimSpace(backend)
		if backend == "" {
			continue
		}
		if item, ok := byIdentity[backend]; ok {
			return backend, item, true
		}
	}
	return "", miseProviderMetadataEntry{}, false
}

func applyMiseVendorReleaseNotesAge(ctx context.Context, client *http.Client, metadata miseProviderMetadataEntry, finding safetyFinding, minAge time.Duration) safetyFinding {
	version := miseCandidateVersion(finding)
	if version == "" {
		return miseReviewFinding(finding, "mise provider metadata candidate version is empty", "retry after mise reports a concrete candidate version")
	}
	releaseDate, err := fetchVendorReleaseNoteDate(ctx, client, metadata, version)
	finding.URL = providerMetadataURL(metadata)
	finding.SupportURL = metadata.SupportURL
	finding.Evidence = appendEvidence(finding.Evidence, metadata.Evidence)
	if err != nil {
		return miseReviewFinding(finding, "vendor release notes metadata unavailable before mise update: "+err.Error(), "retry when vendor release notes are reachable or review the upstream release manually before allowing")
	}
	return applyMiseReleaseAgeFromTime(finding, releaseDate.Format(time.RFC3339), minAge, metadata.Evidence)
}

func fetchVendorReleaseNoteDate(ctx context.Context, client *http.Client, metadata miseProviderMetadataEntry, version string) (time.Time, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url := providerMetadataURL(metadata)
	if strings.TrimSpace(url) == "" {
		return time.Time{}, fmt.Errorf("metadata URL is empty")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return time.Time{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return time.Time{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return time.Time{}, fmt.Errorf("HTTP %d from vendor release notes", response.StatusCode)
	}
	date, ok := vendorReleaseNoteDateFromBody(string(body), metadata.HeadingPattern, version)
	if !ok {
		return time.Time{}, fmt.Errorf("version %s was not found in vendor release notes", version)
	}
	return date, nil
}

func providerMetadataURL(metadata miseProviderMetadataEntry) string {
	if suffix := sanitizeProviderMetadataEnvSuffix(metadata.EnvURLSuffix); suffix != "" {
		if value := strings.TrimSpace(os.Getenv("UPDEV_PROVIDER_METADATA_URL_" + suffix)); value != "" {
			return value
		}
	}
	return metadata.URL
}

func sanitizeProviderMetadataEnvSuffix(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, value)
	value = strings.Trim(value, "_")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}

func vendorReleaseNoteDateFromBody(body string, pattern string, version string) (time.Time, bool) {
	version = strings.TrimSpace(version)
	if version == "" || pattern == "" {
		return time.Time{}, false
	}
	expression, err := regexp.Compile(fmt.Sprintf(pattern, regexp.QuoteMeta(version)))
	if err != nil {
		return time.Time{}, false
	}
	match := expression.FindStringSubmatch(body)
	if len(match) < 2 {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(match[1]))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
