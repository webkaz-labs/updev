package githubrepo

import "testing"

func TestRepoTagFromURLSupportsArchiveForms(t *testing.T) {
	tests := []struct {
		raw  string
		repo string
		tag  string
	}{
		{raw: "https://github.com/owner/tool/releases/download/v1.0.0/tool.tar.gz", repo: "owner/tool", tag: "v1.0.0"},
		{raw: "https://github.com/owner/tool/archive/refs/tags/v1.0.0.tar.gz", repo: "owner/tool", tag: "v1.0.0"},
		{raw: "https://github.com/owner/tool/archive/v1.0.0.zip", repo: "owner/tool", tag: "v1.0.0"},
	}
	for _, tt := range tests {
		repo, tag, ok := RepoTagFromURL(tt.raw)
		if !ok || repo != tt.repo || tag != tt.tag {
			t.Fatalf("unexpected github repo/tag for %s: repo=%q tag=%q ok=%v", tt.raw, repo, tag, ok)
		}
	}
}

func TestRepoTagFromURLRejectsNonGitHub(t *testing.T) {
	if repo, tag, ok := RepoTagFromURL("https://example.com/owner/tool/archive/v1.0.0.zip"); ok || repo != "" || tag != "" {
		t.Fatalf("expected non-GitHub URL to be rejected, got repo=%q tag=%q ok=%v", repo, tag, ok)
	}
}

func TestRepoFromURLsReturnsFirstGitHubRepository(t *testing.T) {
	repo, ok := RepoFromURLs("https://example.com/vendor/tool", "https://github.com/owner/tool/releases")
	if !ok || repo != "owner/tool" {
		t.Fatalf("expected first GitHub repository, got repo=%q ok=%v", repo, ok)
	}
}

func TestVersionTagCandidatesPreservesInferenceOrder(t *testing.T) {
	got := VersionTagCandidates("tool", "1.2.3")
	want := []string{"1.2.3", "v1.2.3", "tool-1.2.3", "tool-v1.2.3"}
	if len(got) != len(want) {
		t.Fatalf("unexpected candidates %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected candidates %#v", got)
		}
	}
}

func TestVersionTagCandidatesAvoidsDuplicateVPrefix(t *testing.T) {
	got := VersionTagCandidates("tool", "v1.2.3")
	want := []string{"v1.2.3", "tool-v1.2.3"}
	if len(got) != len(want) {
		t.Fatalf("unexpected candidates %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected candidates %#v", got)
		}
	}
}
