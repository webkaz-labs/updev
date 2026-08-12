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

func TestNPMPosturesFromItemsReportsRegistryRisks(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	gotPath := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "name": "@scope/tool",
  "repository": {"url": "git+https://github.com/scope/tool.git"},
  "dist-tags": {"latest": "2.0.0"},
  "time": {"1.0.0": "2026-01-01T00:00:00Z", "modified": "2026-01-02T00:00:00Z"},
  "maintainers": [{"name": "alice"}],
  "versions": {
    "1.0.0": {
      "version": "1.0.0",
      "deprecated": "use 2.x",
      "bin": {"scope-tool": "bin/tool.js"}
    }
  }
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "npm:@scope/tool", Version: "1.0.0", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "npm:@scope/tool", Version: "1.0.0", Kind: "tool", Category: "npm"},
		{Provider: "mise", Name: "cargo:fd-find", Version: "10.4.2", Kind: "tool", Category: "cargo"},
	}

	postures, err := NPMPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/@scope/tool" && gotPath != "/@scope%2ftool" {
		t.Fatalf("unexpected npm registry path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one npm posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || !strings.Contains(postures[0].Reason, "deprecated") {
		t.Fatalf("expected deprecated npm review, got %#v", postures[0])
	}
	if postures[0].ReasonCode != securityreason.RegistryVersionDeprecated || postures[0].ReasonArgs["registry"] != "npm" || postures[0].ReasonArgs["package"] != "@scope/tool" {
		t.Fatalf("expected deprecated npm reason code, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/scope/tool" || postures[0].Latest != "2.0.0" || len(postures[0].Binaries) != 1 || postures[0].Binaries[0] != "scope-tool" {
		t.Fatalf("expected npm metadata evidence, got %#v", postures[0])
	}
}

func TestNPMPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := NPMPostureFromMetadata("npm:tool", "tool", "1.0.0", NPMPackageMetadata{
		DistTags:    map[string]string{"latest": "1.0.0"},
		Maintainers: []NPMMaintainer{{Name: "alice"}},
		Versions: map[string]NPMVersionInfo{
			"1.0.0": {Version: "1.0.0"},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.RegistryMissingRepository || posture.ReasonArgs["package"] != "tool" {
		t.Fatalf("expected missing repository reason code, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected npm posture remediation, got %#v", posture)
	}
}

func TestNPMPosturesFromItemsReviewsPrivateRegistryAuthFailure(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"authentication token not provided"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	postures, err := NPMPosturesFromItems(context.Background(), server.Client(), server.URL, []plan.Item{{
		Provider: "mise",
		Name:     "npm:@webkaz-labs/xskills",
		Version:  "0.1.0",
	}})
	if err != nil {
		t.Fatalf("expected item-scoped review posture without aggregate error, got %v", err)
	}
	if len(postures) != 1 || postures[0].Decision != "review" || postures[0].ReasonCode != securityreason.RegistryMetadataUnavailable {
		t.Fatalf("expected registry metadata review posture, got %#v", postures)
	}
	if !strings.Contains(postures[0].Reason, "authentication required") || !strings.Contains(postures[0].Remediation, "authentication") {
		t.Fatalf("expected auth-specific reason/remediation, got %#v", postures[0])
	}
}

func TestNPMPackageFromAuthErrorExtractsGitHubPackage(t *testing.T) {
	detail := "npm error 401 Unauthorized - GET https://npm.pkg.github.com/@webkaz-labs%2fxskills - authentication token not provided"
	if got := NPMPackageFromAuthError(detail); got != "@webkaz-labs/xskills" {
		t.Fatalf("expected package extraction, got %q", got)
	}
}
