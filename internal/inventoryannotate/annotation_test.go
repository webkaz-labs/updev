package inventoryannotate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestProfileScopedExtraBecomesProfileMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{- if has "work" .profiles }}
# work - baseline macOS Homebrew entries, also included by personal profile
cask "ghostty"
{{- end }}
{{- if has "personal" .profiles }}
# personal - private-use desired entries
cask "warp"
{{- end }}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusDrift,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true},
		},
	}
	AnnotateProfileScopedExtras(&report, root)
	if got := report.Items[0].Category; got != "personal" {
		t.Fatalf("expected personal category, got %q in %#v", got, report.Items[0])
	}
	if !ItemHasProfileMismatch(report.Items[0]) {
		t.Fatalf("expected profile mismatch detail, got %#v", report.Items[0])
	}
	if got := ItemStatusLabel(report.Items[0]); got != "profile-mismatch" {
		t.Fatalf("expected profile-mismatch status label, got %q", got)
	}
}

func TestSourceScopedExtraBecomesProfileMismatchForAnyCategory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{- if has "work" .profiles }}
cask "cursor-cli"
{{- end }}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusDrift,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "cursor-cli", Status: plan.StatusExtra, Live: true},
		},
	}
	AnnotateProfileScopedExtras(&report, root)
	if got := report.Items[0].Category; got != "work" {
		t.Fatalf("expected work category, got %q in %#v", got, report.Items[0])
	}
	if !ItemHasProfileMismatch(report.Items[0]) {
		t.Fatalf("expected profile mismatch detail, got %#v", report.Items[0])
	}
	if !strings.Contains(report.Items[0].Detail, "source deployment scope work") {
		t.Fatalf("expected source/rendered mismatch wording, got %#v", report.Items[0])
	}
}

func TestAnnotateMiseManifestIssues(t *testing.T) {
	root := t.TempDir()
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte(`[tools]
go = "1.26.3"
"github:jnsahaj/lumen" = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusOK,
		Root:   root,
		Items: []plan.Item{
			{Provider: "mise", Kind: "tool", Name: "go", Status: plan.StatusOK},
		},
	}
	AnnotateMiseManifestIssues(&report, root)
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift status for manifest issue, got %#v", report)
	}
	var issue plan.Item
	for _, item := range report.Items {
		if item.Provider == "mise" && item.Kind == "manifest" {
			issue = item
			break
		}
	}
	if issue.Name != "github:jnsahaj/lumen" || issue.Category != "github" || issue.Status != plan.StatusBlocked || !strings.Contains(issue.Detail, "latest is not allowed") {
		t.Fatalf("expected blocked mise manifest issue, got %#v", report.Items)
	}
	AnnotateMiseManifestIssues(&report, root)
	count := 0
	for _, item := range report.Items {
		if item.Provider == "mise" && item.Kind == "manifest" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected idempotent manifest annotation, got %#v", report.Items)
	}
}

func TestAnnotateMiseManifestIssuesNilReport(t *testing.T) {
	AnnotateMiseManifestIssues(nil, t.TempDir())
	AnnotateMiseManifestIssues(&plan.Report{}, "")
}
