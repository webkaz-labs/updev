package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/snapshot"
)

func TestMutationReportAddsBrewfileEntryWithSnapshotAndDiff(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	enableBrewfileWriteMode(t, root, "template")
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.SchemaVersion != mutationReportSchemaVersion || report.Status != plan.StatusOK || !report.Changed || report.Snapshot == nil || report.Diff == "" {
		t.Fatalf("expected successful mutation with snapshot and diff, got %#v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, "Brewfile.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `brew "jq"`) {
		t.Fatalf("expected Brewfile entry, got %s", data)
	}
}

func TestMutationReportHoldsDefaultBrewfileWriteWithoutOptIn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("UPDEV_ROOT", root)
	t.Setenv("UPDEV_CONFIG", filepath.Join(root, "missing-updev.toml"))
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.Status != plan.StatusHeld || report.Changed || report.Snapshot != nil || !strings.Contains(report.Reason, "Brewfile writes are disabled") {
		t.Fatalf("expected default Brewfile write to hold before snapshot, got %#v", report)
	}
}

func TestMutationReportAllowsConfiguredBrewfileWriteMode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("UPDEV_ROOT", root)
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[brewfile]\nwrite_mode = \"template\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.Status != plan.StatusOK || !report.Changed || report.Snapshot == nil {
		t.Fatalf("expected configured Brewfile write to succeed, got %#v", report)
	}
}

func TestMutationReportHoldsAmbiguousBareName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action: "add",
		root:   root,
		name:   "jq",
	})
	if report.Status != plan.StatusHeld || report.Changed || report.Snapshot != nil {
		t.Fatalf("expected ambiguous bare name to hold without snapshot or write, got %#v", report)
	}
	if len(report.Candidates) < 2 {
		t.Fatalf("expected provider candidates, got %#v", report.Candidates)
	}
}

func TestMutationReportSurfacesValidationError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	fakeMise := filepath.Join(binDir, "mise")
	if err := os.WriteFile(fakeMise, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	enableBrewfileWriteMode(t, root, "template")
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`{{ if has "personal" .profiles }}
{{ end }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	miseDir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(miseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const scannerOverflowTokenBytes = 70 * 1024 // bufio.Scanner default token limit is 64 KiB.
	if err := os.WriteFile(filepath.Join(miseDir, "config.toml"), []byte("[tools]\n"+strings.Repeat("a", scannerOverflowTokenBytes)+" = \"1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildMutationReport(context.Background(), mutationOptions{
		action:   "add",
		root:     root,
		provider: "brew",
		kind:     "brew",
		name:     "jq",
		category: "personal",
	})
	if report.Status != plan.StatusError || report.Validation.Status != plan.StatusError {
		t.Fatalf("expected validation failure to surface in report status, got %#v", report)
	}
	if !report.Changed || report.Snapshot == nil || report.RollbackCommand == "" {
		t.Fatalf("expected successful write to remain rollbackable despite validation failure, got %#v", report)
	}
}

func TestRollbackReportRestoresLatestSnapshot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "Brewfile.tmpl")
	if err := os.WriteFile(path, []byte("brew \"git\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Create(root, []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("brew \"jq\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := buildRollbackReport(rollbackOptions{root: root})
	if report.Status != plan.StatusOK || report.Token == "" || len(report.RestoredFiles) != 1 {
		t.Fatalf("expected latest rollback to succeed, got %#v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "brew \"git\"\n" {
		t.Fatalf("expected restored manifest, got %q", data)
	}
}

func TestSimpleUnifiedDiffKeepsDeletionFocused(t *testing.T) {
	diff := simpleUnifiedDiff("Brewfile.tmpl", "{{ if has \"personal\" .profiles }}\nbrew \"jq\"\n{{ end }}\n", "{{ if has \"personal\" .profiles }}\n{{ end }}\n")
	if strings.Contains(diff, "+{{ end }}") {
		t.Fatalf("expected deletion-only diff to keep common closing line, got:\n%s", diff)
	}
	if !strings.Contains(diff, "-brew \"jq\"") {
		t.Fatalf("expected deleted brew line, got:\n%s", diff)
	}
}

func TestShouldUseHomeBrewfileOnlyForDefaultRoot(t *testing.T) {
	t.Setenv("UPDEV_ROOT", "")
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-updev.toml"))
	t.Setenv("CHEZMOI_SOURCE_DIR", "/repo")
	if !shouldUseHomeBrewfile("/repo/") {
		t.Fatal("expected default root to use rendered home Brewfile")
	}
	if shouldUseHomeBrewfile("/tmp/repo") {
		t.Fatal("expected alternate root to use source Brewfile.tmpl")
	}
}

func TestSyncEntriesFromInventoryUsesV1Reasons(t *testing.T) {
	report := plan.Report{
		Providers: []plan.ProviderSummary{{Name: "brew", Unavailable: true}},
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusMissing},
			{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusExtra},
			{Provider: "mise", Kind: "tool", Name: "go", Status: plan.StatusOK},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 3 {
		t.Fatalf("expected unavailable, missing, and extra entries, got %#v", entries)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Reason] = true
	}
	for _, reason := range []string{"unavailable", "missing", "extra"} {
		if !got[reason] {
			t.Fatalf("expected reason %s in %#v", reason, entries)
		}
	}
}

func TestSyncEntriesClassifyProviderMismatchAndActions(t *testing.T) {
	report := plan.Report{
		Items: []plan.Item{
			{Provider: "brew", Kind: "brew", Name: "node", Status: plan.StatusMissing, Desired: true},
			{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusExtra, Live: true},
			{Provider: "brew", Kind: "cask", Name: "orphan-app", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	byName := map[string]syncEntry{}
	for _, entry := range entries {
		byName[entry.Provider+"/"+entry.Name] = entry
	}
	if entry := byName["brew/node"]; entry.Reason != "provider-mismatch" || entry.Action != "choose-provider" || entry.RelatedProvider != "mise" {
		t.Fatalf("expected brew node provider mismatch with related mise entry, got %#v", entry)
	}
	if entry := byName["mise/node"]; entry.Reason != "provider-mismatch" || entry.Action != "choose-provider" || entry.RelatedProvider != "brew" {
		t.Fatalf("expected mise node provider mismatch with related brew entry, got %#v", entry)
	}
	if entry := byName["brew/orphan-app"]; entry.Reason != "extra" || entry.Action != "adopt-remove-or-manual" || entry.Detail == "" {
		t.Fatalf("expected extra action guidance, got %#v", entry)
	}
}

func TestSyncEntriesClassifyManualCaskAsSkipped(t *testing.T) {
	root := t.TempDir()
	enableManualMarkdownCompat(t, root)
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "apps.md"), []byte(`# macOS 手動管理アプリ

## ベンダー独自更新

| アプリ | 入手先 | 用途 |
|--------|--------|------|
| Evernote | 公式 | ノート |
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report := plan.Report{
		Status: plan.StatusDrift,
		Root:   root,
		Items: []plan.Item{
			{Provider: "brew", Kind: "cask", Name: "evernote", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one skipped manual entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Reason != "skipped" || entry.Action != "manual-local-only" || !strings.Contains(entry.Detail, "docs/apps.md") {
		t.Fatalf("expected manual cask to be skipped with guidance, got %#v", entry)
	}
	if got := syncReportStatus(report, entries); got != plan.StatusOK {
		t.Fatalf("expected all-skipped sync report to stay ok, got %s", got)
	}
}

func TestSyncEntriesGiveTapSpecificGuidance(t *testing.T) {
	report := plan.Report{
		Items: []plan.Item{
			{Provider: "brew", Kind: "tap", Name: "webkaz/tap", Status: plan.StatusExtra, Live: true},
		},
	}
	entries := syncEntriesFromInventory(report)
	if len(entries) != 1 {
		t.Fatalf("expected one tap entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Action != "adopt-or-untap" || !strings.Contains(entry.Detail, "brew untap webkaz/tap") {
		t.Fatalf("expected tap-specific untap guidance, got %#v", entry)
	}
}

func TestPrintSyncTextShowsCategoryMeaning(t *testing.T) {
	report := syncReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Entries: []syncEntry{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "orphan-app",
			Category: "personal",
			Reason:   "extra",
			Status:   plan.StatusExtra,
			Action:   "adopt-remove-or-manual",
			Detail:   "installed entry is unmanaged",
		}},
	}
	var out bytes.Buffer
	printSyncText(&out, report, false)
	text := out.String()
	for _, want := range []string{"categories: personal=1", "personal-only additions on top of work", "action", "adopt-remove-or-manual", "details"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected sync text to include %q:\n%s", want, text)
		}
	}
}
