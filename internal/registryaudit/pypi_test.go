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

func TestPyPIPosturesFromItemsReportsYankedVersions(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	gotPath := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
  "info": {
    "name": "frogmouth",
    "version": "0.9.2",
    "project_urls": {"Source": "https://github.com/Textualize/frogmouth"}
  },
  "releases": {
    "0.9.2": [{
      "upload_time_iso_8601": "2026-05-01T00:00:00.000Z",
      "yanked": true,
      "yanked_reason": "bad release"
    }]
  }
}`))
	}))
	defer server.Close()
	items := []plan.Item{
		{Provider: "mise", Name: "pipx:frogmouth", Version: "0.9.2", Kind: "tool", Category: "pipx"},
		{Provider: "mise", Name: "pipx:frogmouth", Version: "0.9.2", Kind: "tool", Category: "pipx"},
		{Provider: "mise", Name: "npm:pnpm", Version: "11.1.2", Kind: "tool", Category: "npm"},
	}

	postures, err := PyPIPosturesFromItems(context.Background(), server.Client(), server.URL, items)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/frogmouth/json" {
		t.Fatalf("unexpected PyPI path: %s", gotPath)
	}
	if len(postures) != 1 {
		t.Fatalf("expected one PyPI posture, got %#v", postures)
	}
	if postures[0].Decision != "review" || !strings.Contains(postures[0].Reason, "yanked") {
		t.Fatalf("expected yanked PyPI review, got %#v", postures[0])
	}
	if postures[0].ReasonCode != securityreason.RegistryVersionYanked || postures[0].ReasonArgs["registry"] != "PyPI" || postures[0].ReasonArgs["package"] != "frogmouth" {
		t.Fatalf("expected yanked PyPI reason code, got %#v", postures[0])
	}
	if !strings.Contains(postures[0].Remediation, "non-yanked") {
		t.Fatalf("expected PyPI posture remediation, got %#v", postures[0])
	}
	if postures[0].RepositoryURL != "https://github.com/Textualize/frogmouth" || postures[0].Latest != "0.9.2" {
		t.Fatalf("expected PyPI metadata evidence, got %#v", postures[0])
	}
}

func TestPyPIPostureReviewsMissingRepositoryURL(t *testing.T) {
	posture := PyPIPostureFromMetadata("pipx:tool", "tool", "1.0.0", PyPIPackageMetadata{
		Info: PyPIInfo{Name: "tool", Version: "1.0.0"},
		Releases: map[string][]PyPIRelease{
			"1.0.0": {{UploadTimeISO8601: "2026-05-01T00:00:00.000Z"}},
		},
	})
	if posture.Decision != "review" || posture.Confidence != "low" || !strings.Contains(posture.Reason, "source repository") {
		t.Fatalf("expected missing repository review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.RegistryMissingRepository || posture.ReasonArgs["package"] != "tool" {
		t.Fatalf("expected missing repository reason code, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "provenance") {
		t.Fatalf("expected PyPI posture remediation, got %#v", posture)
	}
}
