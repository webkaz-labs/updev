package securitygate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestCacheKeyStable(t *testing.T) {
	left := CacheKey("brew", "root", "candidate")
	right := CacheKey("brew", "root", "candidate")
	if left == "" || left != right {
		t.Fatalf("expected stable cache key, got %q and %q", left, right)
	}
	if left == CacheKey("brew", "root", "other") {
		t.Fatal("expected different inputs to produce different keys")
	}
}

func TestCachePathUsesUpdevCacheDir(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	want := filepath.Join(cacheHome, "updev", "update-safety-v1", "brew", "abc.json")
	if got := CachePath("brew", "abc"); got != want {
		t.Fatalf("expected cache path %q, got %q", want, got)
	}
}

func TestSaveAndLoadCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	key := CacheKey("provider", "candidate")
	SaveCache("mise", key, []Finding{{Provider: "mise", Kind: "tool", Name: "go", Decision: "allow"}}, []string{"warning"})

	entry, ok := LoadCache("mise", key, time.Hour)
	if !ok {
		t.Fatal("expected cache entry")
	}
	if entry.Version != CacheVersion || entry.Status != plan.StatusOK || len(entry.Findings) != 1 || entry.Findings[0].Name != "go" {
		t.Fatalf("unexpected cache entry: %#v", entry)
	}
	if len(entry.Warnings) != 1 || entry.Warnings[0] != "warning" {
		t.Fatalf("unexpected warnings: %#v", entry.Warnings)
	}
}

func TestLoadCacheRejectsStaleEntry(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	key := CacheKey("stale")
	SaveCacheEntry(CacheEntry{
		Version:   CacheVersion,
		Provider:  "brew",
		Key:       key,
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Status:    plan.StatusOK,
	})

	if _, ok := LoadCache("brew", key, time.Hour); ok {
		t.Fatal("expected stale cache to be rejected")
	}
}
