package mise

import (
	"encoding/json"
	"strings"
)

type RegistryEntry struct {
	Short    string   `json:"short"`
	Backends []string `json:"backends"`
	Aliases  []string `json:"aliases"`
}

func RegistryIndexFromJSON(stdout string) map[string]RegistryEntry {
	var entries []RegistryEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		return nil
	}
	index := make(map[string]RegistryEntry, len(entries))
	for _, entry := range entries {
		if key := strings.TrimSpace(entry.Short); key != "" {
			index[key] = entry
		}
		for _, alias := range entry.Aliases {
			if key := strings.TrimSpace(alias); key != "" {
				index[key] = entry
			}
		}
	}
	return index
}

func RegistryEntryForTool(registry map[string]RegistryEntry, name string) (RegistryEntry, bool) {
	for _, key := range RegistryLookupKeys(name) {
		if entry, ok := registry[key]; ok {
			return entry, true
		}
	}
	return RegistryEntry{}, false
}

func RegistryLookupKeys(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	keys := []string{name}
	if _, suffix, ok := strings.Cut(name, ":"); ok && suffix != "" {
		keys = appendUnique(keys, suffix)
	}
	return keys
}

func RegistryGitHubBackend(entry RegistryEntry) (string, string, bool) {
	for _, backend := range entry.Backends {
		rawRepo, ok := strings.CutPrefix(strings.TrimSpace(backend), "aqua:")
		if !ok {
			continue
		}
		parts := strings.Split(rawRepo, "/")
		if len(parts) != 2 || !validGitHubPathPart(parts[0]) || !validGitHubPathPart(parts[1]) {
			continue
		}
		return backend, parts[0] + "/" + parts[1], true
	}
	return "", "", false
}

type ProviderMetadataResolverType string

const ResolverVendorReleaseNotes ProviderMetadataResolverType = "vendor_release_notes"

type ProviderMetadataEntry struct {
	ID               string
	ProviderIdentity string
	ResolverType     ProviderMetadataResolverType
	URL              string
	EnvURLSuffix     string
	HeadingPattern   string
	Evidence         string
	SupportURL       string
}

func ProviderMetadataRegistry() []ProviderMetadataEntry {
	return []ProviderMetadataEntry{
		{
			ID:               "google-cloud-cli",
			ProviderIdentity: "vfox:mise-plugins/vfox-gcloud",
			ResolverType:     ResolverVendorReleaseNotes,
			URL:              "https://docs.cloud.google.com/sdk/docs/release-notes",
			EnvURLSuffix:     "GOOGLE_CLOUD_CLI",
			HeadingPattern:   `(?is)(?:^|[>\n])\s*%s\s*\((\d{4}-\d{2}-\d{2})\)`,
			Evidence:         "Google Cloud CLI release notes",
			SupportURL:       "https://docs.cloud.google.com/sdk/docs/install-sdk",
		},
	}
}

func RegistryProviderMetadataBackend(entry RegistryEntry, metadata []ProviderMetadataEntry) (string, ProviderMetadataEntry, bool) {
	byIdentity := make(map[string]ProviderMetadataEntry, len(metadata))
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
	return "", ProviderMetadataEntry{}, false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func validGitHubPathPart(value string) bool {
	if value == "" {
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
