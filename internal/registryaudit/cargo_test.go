package registryaudit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestCargoPosturesFromItemsReportsYankedVersions(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	gotPath := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "crate": {
    "id": "fd-find",
    "max_version": "10.4.2",
    "repository": "https://github.com/sharkdp/fd",
    "updated_at": "2026-05-02T00:00:00Z",
    "downloads": 42
  },
  "versions": [
    {"num": "10.4.2", "yanked": true, "created_at": "2026-05-01T00:00:00Z"}
  ]
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
	}

	postures, err := CargoPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/crates/fd-find" {
		t.Fatalf("unexpected crates.io path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one cargo posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || postures[0].Reason != "installed crate version is yanked" {
		t.Fatalf("expected yanked crate review, got %#v", postures[0])
	}
	if postures[0].ReasonCode != securityreason.RegistryVersionYanked || postures[0].ReasonArgs["registry"] != "crates.io" || postures[0].ReasonArgs["package"] != "fd-find" {
		t.Fatalf("expected yanked crate reason code, got %#v", postures[0])
	}
	if !strings.Contains(postures[0].Remediation, "non-yanked") {
		t.Fatalf("expected cargo posture remediation, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/sharkdp/fd" || postures[0].Latest != "10.4.2" {
		t.Fatalf("expected crates.io metadata evidence, got %#v", postures[0])
	}
}

func TestCargoPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := CargoPostureFromMetadata("cargo:tool", "tool", "1.0.0", CratesIOResponse{
		Crate: CratesIOCrate{ID: "tool", MaxVersion: "1.0.0"},
		Versions: []CratesIOVersion{
			{Num: "1.0.0"},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.RegistryMissingRepository || posture.ReasonArgs["package"] != "tool" {
		t.Fatalf("expected missing repository reason code, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected cargo posture remediation, got %#v", posture)
	}
}
