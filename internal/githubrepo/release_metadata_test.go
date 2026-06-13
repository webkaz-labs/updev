package githubrepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchReleaseOrTagByTagUsesReleaseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer token header, got %q", got)
		}
		if r.URL.Path != "/repos/owner/tool/releases/tags/v1.2.3" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"published_at":"2026-06-10T00:00:00Z"}`))
	}))
	defer server.Close()

	release, evidence, err := FetchReleaseOrTagByTag(context.Background(), server.Client(), server.URL, "test-token", "owner/tool", "v1.2.3", false)
	if err != nil {
		t.Fatal(err)
	}
	if release.PublishedAt != "2026-06-10T00:00:00Z" || evidence != "GitHub release metadata" {
		t.Fatalf("unexpected release metadata: %#v evidence=%q", release, evidence)
	}
}

func TestFetchReleaseOrTagByTagFallsBackToCommitTagDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/tool/releases/tags/v1.2.3":
			http.NotFound(w, r)
		case "/repos/owner/tool/git/ref/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"object":{"type":"commit","sha":"abc123"}}`))
		case "/repos/owner/tool/git/commits/abc123":
			_, _ = w.Write([]byte(`{"committer":{"date":"2026-06-11T00:00:00Z"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	release, evidence, err := FetchReleaseOrTagByTag(context.Background(), server.Client(), server.URL, "", "owner/tool", "v1.2.3", true)
	if err != nil {
		t.Fatal(err)
	}
	if release.CreatedAt != "2026-06-11T00:00:00Z" || evidence != "GitHub inferred tag metadata" {
		t.Fatalf("unexpected fallback metadata: %#v evidence=%q", release, evidence)
	}
}

func TestFetchReleaseOrTagByTagReturnsBothErrors(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, _, err := FetchReleaseOrTagByTag(context.Background(), server.Client(), server.URL, "", "owner/tool", "v1.2.3", false)
	if err == nil || !strings.Contains(err.Error(), "tag metadata fallback failed") {
		t.Fatalf("expected combined release/tag error, got %v", err)
	}
}

func TestParseReleaseTime(t *testing.T) {
	got, err := ParseReleaseTime(Release{CreatedAt: "2026-06-11T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected release time %s", got)
	}
	if _, err := ParseReleaseTime(Release{}); err == nil {
		t.Fatal("expected empty release time to fail")
	}
}

func TestFetchRepositoryUsesTokenAndRepositoryPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repos/owner/tool" {
			t.Fatalf("unexpected repository path %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("expected bearer token header, got %q", got)
		}
		_, _ = w.Write([]byte(`{"full_name":"owner/tool","stargazers_count":42}`))
	}))
	defer server.Close()

	repo, err := FetchRepository(context.Background(), server.Client(), server.URL, "test-token", "owner/tool")
	if err != nil {
		t.Fatal(err)
	}
	if repo.FullName != "owner/tool" || repo.StargazersCount != 42 {
		t.Fatalf("unexpected repository metadata: %#v", repo)
	}
}

func TestFetchRepositoryUsesRepositoryErrorPrefix(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := FetchRepository(context.Background(), server.Client(), server.URL, "", "owner/tool")
	if err == nil || !strings.Contains(err.Error(), "github repository query failed: HTTP 404") {
		t.Fatalf("expected repository query error prefix, got %v", err)
	}
}
