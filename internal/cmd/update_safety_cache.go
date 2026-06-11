package cmd

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
)

var (
	updateSafetyMarketplaceMaxAge   = 6 * time.Hour
	updateSafetyUnavailableMaxAge   = 45 * time.Minute
	updateSafetyHomebrewMetadataAge = 12 * time.Hour
	updateSafetyMiseMetadataAge     = 12 * time.Hour
	updateSafetyBrewOutdatedMaxAge  = 5 * time.Minute
)

type updateSafetyCacheEntry = securitygate.CacheEntry

func loadUpdateSafetyCache(provider string, key string, maxAge time.Duration) (updateSafetyCacheEntry, bool) {
	return securitygate.LoadCache(provider, key, maxAge)
}

func saveUpdateSafetyCache(provider string, key string, findings []safetyFinding, warnings []string) {
	securitygate.SaveCache(provider, key, findings, warnings)
}

func saveUpdateSafetyErrorCache(provider string, key string, status plan.Status, message string, warnings []string) {
	securitygate.SaveErrorCache(provider, key, status, message, warnings)
}

func saveUpdateSafetyUnavailableCache(provider string, key string, message string, findings []safetyFinding, warnings []string) {
	securitygate.SaveUnavailableCache(provider, key, message, findings, warnings)
}

func updateSafetyCachePath(provider string, key string) string {
	return securitygate.CachePath(provider, key)
}

func updateSafetyCacheKey(parts ...string) string {
	return securitygate.CacheKey(parts...)
}

func updateSafetyBrewCacheKey(root string, findings []safetyFinding, minReleaseAge time.Duration) string {
	parts := []string{"brew", root, "min-release-age-days=" + strconv.Itoa(int(minReleaseAge.Hours()/24))}
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Kind,
			finding.Name,
			strings.Join(finding.InstalledVersions, ","),
			finding.CurrentVersion,
			finding.Source,
			finding.Tap,
			finding.URL,
		}, "\x1f"))
	}
	sort.Strings(parts[3:])
	return updateSafetyCacheKey(parts...)
}

func updateSafetyBrewOutdatedErrorCacheKey(root string) string {
	return updateSafetyCacheKey("brew", root, "outdated-json-v2")
}

func updateSafetyMiseCacheKey(root string, findings []safetyFinding, minReleaseAge time.Duration) string {
	parts := []string{"mise", root, "mise-provider-metadata-v1", "min-release-age-days=" + strconv.Itoa(int(minReleaseAge.Hours()/24))}
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Kind,
			finding.Name,
			strings.Join(finding.InstalledVersions, ","),
			finding.CurrentVersion,
			finding.Version,
			finding.Source,
			finding.RepositoryURL,
		}, "\x1f"))
	}
	sort.Strings(parts[4:])
	return updateSafetyCacheKey(parts...)
}

func updateSafetyBrewAdvisoryErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("brew", candidateKey, "advisory-unavailable")
}

func updateSafetyVSCodeCacheKey(root string, items []planItemIdentity, installed map[string]string) string {
	parts := []string{"vscode", root}
	for _, item := range items {
		parts = append(parts, strings.Join([]string{item.Kind, item.Name, installed[strings.ToLower(item.Name)]}, "\x1f"))
	}
	sort.Strings(parts[2:])
	return updateSafetyCacheKey(parts...)
}

func updateSafetyVSCodeMarketplaceErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("vscode", candidateKey, "marketplace-unavailable")
}

func updateSafetyVSCodeAdvisoryErrorCacheKey(candidateKey string) string {
	return updateSafetyCacheKey("vscode", candidateKey, "advisory-unavailable")
}

type planItemIdentity struct {
	Kind string
	Name string
}

func planItemIdentities(items []plan.Item) []planItemIdentity {
	out := make([]planItemIdentity, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		key := strings.ToLower(item.Kind + "\x00" + item.Name)
		if item.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, planItemIdentity{Kind: item.Kind, Name: item.Name})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func updateSafetyCacheEvidence(findings []safetyFinding, provider string, createdAt time.Time) []safetyFinding {
	if createdAt.IsZero() {
		return findings
	}
	age := friendlyAge(time.Since(createdAt))
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		finding.Evidence = appendEvidence(finding.Evidence, "updev update safety cache: "+provider+" "+age+" old")
		out = append(out, finding)
	}
	return out
}

func applyUpdateSafetyUnavailableCache(gate *safetyGate, cached updateSafetyCacheEntry, warning string, evidenceProvider string) []safetyFinding {
	if cached.Error != "" {
		gate.Warnings = append(gate.Warnings, warning+": "+cached.Error)
	}
	gate.Warnings = append(gate.Warnings, cached.Warnings...)
	return updateSafetyCacheEvidence(cached.Findings, evidenceProvider, cached.CreatedAt)
}
