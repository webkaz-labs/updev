package githubrepo

import (
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestRepoFromMiseNameParsesVersionAndRefSuffixes(t *testing.T) {
	repo, ok := RepoFromMiseName("github:owner/tool@1.2.3#main")
	if !ok || repo != "owner/tool" {
		t.Fatalf("expected owner/tool, got %q ok=%v", repo, ok)
	}
	if _, ok := RepoFromMiseName("github:owner/tool;rm"); ok {
		t.Fatal("expected invalid repository syntax to be rejected")
	}
}

func TestRepoFromAnyURLParsesGitHubRepository(t *testing.T) {
	repo, ok := RepoFromAnyURL("https://github.com/owner/tool.git/releases/tag/v1")
	if !ok || repo != "owner/tool" {
		t.Fatalf("expected owner/tool, got %q ok=%v", repo, ok)
	}
	repo, ok = RepoFromAnyURL("https://github.com/owner%2Dname/tool%2Egit")
	if !ok || repo != "owner-name/tool" {
		t.Fatalf("expected escaped owner-name/tool, got %q ok=%v", repo, ok)
	}
	if _, ok := RepoFromAnyURL("https://example.com/owner/tool"); ok {
		t.Fatal("expected non-GitHub URL to be rejected")
	}
}

func TestPostureFromRepositoryReportsRepositoryRisks(t *testing.T) {
	posture := PostureFromRepository("mise", "github:owner/tool", "owner/tool", Repository{
		FullName: "owner/tool",
		HTMLURL:  "https://github.com/owner/tool",
		Archived: true,
		SecurityAndAnalysis: SecurityAndAnalysis{
			DependabotSecurityUpdates: SecurityFeature{Status: "enabled"},
			SecretScanning:            SecurityFeature{Status: "enabled"},
		},
	}, 0)
	if posture.Decision != "review" || posture.Reason != "repository is archived" {
		t.Fatalf("expected archived repository review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.GitHubRepositoryArchived || posture.ReasonArgs["repository"] != "owner/tool" {
		t.Fatalf("expected archived repository reason code, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "archived repository") {
		t.Fatalf("expected archived remediation, got %#v", posture)
	}
}

func TestPostureFromRepositoryAppliesTapRepositoryAge(t *testing.T) {
	created := time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	posture := PostureFromRepository("brew", "tap:vendor/tap", "vendor/homebrew-tap", Repository{
		FullName:  "vendor/homebrew-tap",
		CreatedAt: created,
	}, 30*24*time.Hour)
	if posture.Decision != "review" || posture.RepositoryAgeDays < 1 || posture.MinRepositoryAgeDays != 30 {
		t.Fatalf("expected newly-created tap review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.GitHubRepositoryTooNew || posture.ReasonArgs["age_days"] == "" || posture.ReasonArgs["min_age_days"] != "30" {
		t.Fatalf("expected tap age reason code, got %#v", posture)
	}
	if !strings.Contains(posture.Remediation, "minimum age") {
		t.Fatalf("expected age remediation, got %#v", posture)
	}
}
