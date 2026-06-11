package securitygate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const CacheVersion = 1

type CacheEntry struct {
	Version   int         `json:"version"`
	Provider  string      `json:"provider"`
	Key       string      `json:"key"`
	CreatedAt time.Time   `json:"created_at"`
	Status    plan.Status `json:"status,omitempty"`
	Error     string      `json:"error,omitempty"`
	Findings  []Finding   `json:"findings"`
	Warnings  []string    `json:"warnings,omitempty"`
}

func LoadCache(provider string, key string, maxAge time.Duration) (CacheEntry, bool) {
	path := CachePath(provider, key)
	if path == "" {
		return CacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheEntry{}, false
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return CacheEntry{}, false
	}
	if entry.Version != CacheVersion || entry.Provider != provider || entry.Key != key {
		return CacheEntry{}, false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return CacheEntry{}, false
	}
	return entry, true
}

func SaveCache(provider string, key string, findings []Finding, warnings []string) {
	SaveCacheEntry(CacheEntry{
		Version:   CacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    plan.StatusOK,
		Findings:  findings,
		Warnings:  warnings,
	})
}

func SaveErrorCache(provider string, key string, status plan.Status, message string, warnings []string) {
	if status == "" {
		status = plan.StatusError
	}
	SaveCacheEntry(CacheEntry{
		Version:   CacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    status,
		Error:     message,
		Warnings:  warnings,
	})
}

func SaveUnavailableCache(provider string, key string, message string, findings []Finding, warnings []string) {
	SaveCacheEntry(CacheEntry{
		Version:   CacheVersion,
		Provider:  provider,
		Key:       key,
		CreatedAt: time.Now(),
		Status:    plan.StatusUnavailable,
		Error:     message,
		Findings:  findings,
		Warnings:  warnings,
	})
}

func SaveCacheEntry(entry CacheEntry) {
	path := CachePath(entry.Provider, entry.Key)
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

func CachePath(provider string, key string) string {
	dir := updevpath.CacheDir()
	if dir == "" || provider == "" || key == "" {
		return ""
	}
	return filepath.Join(dir, "update-safety-v1", provider, key+".json")
}

func CacheKey(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}
