package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const updateSafetyCacheVersion = 1

var (
	updateSafetyMarketplaceMaxAge   = 6 * time.Hour
	updateSafetyUnavailableMaxAge   = 45 * time.Minute
	updateSafetyHomebrewMetadataAge = 12 * time.Hour
	updateSafetyBrewOutdatedMaxAge  = 5 * time.Minute
)

type updateSafetyCacheEntry struct {
	Version   int             `json:"version"`
	Provider  string          `json:"provider"`
	Key       string          `json:"key"`
	CreatedAt time.Time       `json:"created_at"`
	Status    plan.Status     `json:"status,omitempty"`
	Error     string          `json:"error,omitempty"`
	Findings  []safetyFinding `json:"findings"`
	Warnings  []string        `json:"warnings,omitempty"`
}

func loadUpdateSafetyCache(provider string, key string, maxAge time.Duration) (updateSafetyCacheEntry, bool) {
	path := updateSafetyCachePath(provider, key)
	if path == "" {
		return updateSafetyCacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateSafetyCacheEntry{}, false
	}
	var entry updateSafetyCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return updateSafetyCacheEntry{}, false
	}
	if entry.Version != updateSafetyCacheVersion || entry.Provider != provider || entry.Key != key {
		return updateSafetyCacheEntry{}, false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return updateSafetyCacheEntry{}, false
	}
	return entry, true
}

func saveUpdateSafetyCache(provider string, key string, findings []safetyFinding, warnings []string) {
	saveUpdateSafetyCacheEntry(updateSafetyCacheEntry{
		Version:   updateSafetyCacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    plan.StatusOK,
		Findings:  findings,
		Warnings:  warnings,
	})
}

func saveUpdateSafetyErrorCache(provider string, key string, status plan.Status, message string, warnings []string) {
	if status == "" {
		status = plan.StatusError
	}
	saveUpdateSafetyCacheEntry(updateSafetyCacheEntry{
		Version:   updateSafetyCacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    status,
		Error:     message,
		Warnings:  warnings,
	})
}

func saveUpdateSafetyUnavailableCache(provider string, key string, message string, findings []safetyFinding, warnings []string) {
	saveUpdateSafetyCacheEntry(updateSafetyCacheEntry{
		Version:   updateSafetyCacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    plan.StatusUnavailable,
		Error:     message,
		Findings:  findings,
		Warnings:  warnings,
	})
}

func saveUpdateSafetyCacheEntry(entry updateSafetyCacheEntry) {
	path := updateSafetyCachePath(entry.Provider, entry.Key)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func updateSafetyCachePath(provider string, key string) string {
	dir := updevCacheDir()
	if dir == "" || provider == "" || key == "" {
		return ""
	}
	return filepath.Join(dir, "update-safety-v1", provider, key+".json")
}

func updateSafetyCacheKey(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
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
