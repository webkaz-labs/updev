package mise

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVendorReleaseNoteDateFromBody(t *testing.T) {
	date, ok := VendorReleaseNoteDateFromBody(
		"<h2>Google Cloud CLI 572.0.0 (2026-06-10)</h2>",
		`(?is)%s\s*\((\d{4}-\d{2}-\d{2})\)`,
		"572.0.0",
	)
	if !ok {
		t.Fatal("expected release note date")
	}
	if got := date.Format("2006-01-02"); got != "2026-06-10" {
		t.Fatalf("unexpected release date %s", got)
	}
}

func TestProviderMetadataURLUsesEnvOverride(t *testing.T) {
	t.Setenv("UPDEV_PROVIDER_METADATA_URL_GOOGLE_CLOUD_CLI", "https://example.test/releases")
	url := ProviderMetadataURL(ProviderMetadataEntry{
		URL:          "https://docs.example.invalid/releases",
		EnvURLSuffix: "google-cloud-cli",
	})
	if url != "https://example.test/releases" {
		t.Fatalf("unexpected provider metadata URL %q", url)
	}
}

func TestFetchVendorReleaseNoteDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<h2>572.0.0 (2026-06-10)</h2>"))
	}))
	t.Cleanup(server.Close)

	date, err := FetchVendorReleaseNoteDate(context.Background(), server.Client(), ProviderMetadataEntry{
		URL:            server.URL,
		HeadingPattern: `(?is)%s\s*\((\d{4}-\d{2}-\d{2})\)`,
	}, "572.0.0")
	if err != nil {
		t.Fatalf("expected release note date: %v", err)
	}
	if got := date.Format("2006-01-02"); got != "2026-06-10" {
		t.Fatalf("unexpected release date %s", got)
	}
}
