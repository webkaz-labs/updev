package securitygate

import (
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestBrewCandidateCacheKeyStable(t *testing.T) {
	findings := []Finding{
		{Kind: "cask", Name: "beta", CurrentVersion: "2", URL: "https://example.test/beta"},
		{Kind: "brew", Name: "alpha", CurrentVersion: "1", URL: "https://example.test/alpha"},
	}
	left := BrewCandidateCacheKey("/root", findings, 72*time.Hour)
	right := BrewCandidateCacheKey("/root", []Finding{findings[1], findings[0]}, 72*time.Hour)
	if left == "" || left != right {
		t.Fatalf("expected stable brew cache key, got %q and %q", left, right)
	}
	if left == BrewCandidateCacheKey("/root", findings, 24*time.Hour) {
		t.Fatal("expected min release age to affect brew cache key")
	}
}

func TestMiseCandidateCacheKeyIncludesProviderMetadataVersion(t *testing.T) {
	findings := []Finding{{Kind: "tool", Name: "go", CurrentVersion: "1.2.3", RepositoryURL: "https://github.com/golang/go"}}
	key := MiseCandidateCacheKey("/root", findings, 72*time.Hour)
	if key == "" {
		t.Fatal("expected mise cache key")
	}
	if key == CacheKey("mise", "/root", "min-release-age-days=3") {
		t.Fatal("expected mise cache key to include provider metadata version")
	}
}

func TestVSCodeCandidateCacheKeyStable(t *testing.T) {
	items := []ItemIdentity{{Kind: "vscode", Name: "publisher.two"}, {Kind: "vscode", Name: "publisher.one"}}
	installed := map[string]string{"publisher.one": "1.0.0", "publisher.two": "2.0.0"}
	left := VSCodeCandidateCacheKey("/root", items, installed)
	right := VSCodeCandidateCacheKey("/root", []ItemIdentity{items[1], items[0]}, installed)
	if left == "" || left != right {
		t.Fatalf("expected stable vscode cache key, got %q and %q", left, right)
	}
}

func TestItemIdentitiesFromPlanItems(t *testing.T) {
	got := ItemIdentitiesFromPlanItems([]plan.Item{
		{Kind: "vscode", Name: "publisher.two"},
		{Kind: "vscode", Name: "publisher.one"},
		{Kind: "vscode", Name: "publisher.one"},
		{Kind: "vscode"},
	})
	if len(got) != 2 {
		t.Fatalf("expected two identities, got %#v", got)
	}
	if got[0].Name != "publisher.one" || got[1].Name != "publisher.two" {
		t.Fatalf("expected sorted unique identities, got %#v", got)
	}
}
