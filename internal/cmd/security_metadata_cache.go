package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const securityMetadataCacheVersion = 1
const registryMetadataCacheMaxAge = 6 * time.Hour

type securityMetadataCacheEntry struct {
	Version   int             `json:"version"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

func loadSecurityMetadataCache(kind string, key string, maxAge time.Duration, out any) bool {
	path := securityMetadataCachePath(kind, key)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var entry securityMetadataCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return false
	}
	if entry.Version != securityMetadataCacheVersion || entry.Kind != kind || entry.Key != key {
		return false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return false
	}
	return json.Unmarshal(entry.Data, out) == nil
}

func saveSecurityMetadataCache(kind string, key string, value any) {
	path := securityMetadataCachePath(kind, key)
	if path == "" {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	entry := securityMetadataCacheEntry{
		Version:   securityMetadataCacheVersion,
		Kind:      kind,
		Key:       key,
		CreatedAt: time.Now(),
		Data:      payload,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func securityMetadataCachePath(kind string, key string) string {
	dir := updevCacheDir()
	if dir == "" || kind == "" || key == "" {
		return ""
	}
	return filepath.Join(dir, "security-metadata-v1", kind, key+".json")
}
