package securitygate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

const MetadataCacheVersion = 1
const RegistryMetadataCacheMaxAge = 6 * time.Hour

type MetadataCacheEntry struct {
	Version   int             `json:"version"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

func LoadMetadataCache(kind string, key string, maxAge time.Duration, out any) bool {
	path := MetadataCachePath(kind, key)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var entry MetadataCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return false
	}
	if entry.Version != MetadataCacheVersion || entry.Kind != kind || entry.Key != key {
		return false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return false
	}
	return json.Unmarshal(entry.Data, out) == nil
}

func SaveMetadataCache(kind string, key string, value any) {
	path := MetadataCachePath(kind, key)
	if path == "" {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	entry := MetadataCacheEntry{
		Version:   MetadataCacheVersion,
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

func MetadataCachePath(kind string, key string) string {
	return updevpath.SecurityMetadataCacheFile(kind, key)
}
