package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/updatereason"
)

func TestRunUpdateStepDryRunDoesNotExecute(t *testing.T) {
	fake := &fakeCommandRunner{}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], true)
	if step.Status != plan.StatusOK {
		t.Fatalf("expected dry-run step to be ok, got %s", step.Status)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("dry-run executed commands: %+v", fake.calls)
	}
}

func TestRunUpdateStepReportsError(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Code: 2, Stderr: "failed", Err: os.ErrPermission}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if step.Status != plan.StatusError {
		t.Fatalf("expected error status, got %s", step.Status)
	}
	if step.Stderr != "failed" {
		t.Fatalf("expected stderr to be preserved, got %q", step.Stderr)
	}
}

func TestRunUpdateStepSummarizesProviderLogs(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Upgrading jq
jq 1.7 -> 1.8.1
Already up-to-date.`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.Updated) != 1 || step.Updated[0] != "jq 1.7 -> 1.8.1" {
		t.Fatalf("expected updated items from provider logs, got %#v", step.Updated)
	}
	if len(step.SkippedItems) != 0 {
		t.Fatalf("generic skipped provider logs should not become outcome rows, got %#v", step.SkippedItems)
	}
}

func TestRunUpdateStepDeduplicatesHomebrewUpgradeProgress(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Upgrading 3 outdated packages:
usage 3.3.0 -> 3.4.0
mise 2026.5.16 -> 2026.5.18
cursor 3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768
mole 1.41.0 -> 1.42.0 (3.5MB)
Upgraded 4 outdated packages
Upgrading usage
3.3.0 -> 3.4.0
Upgrading mise
2026.5.16 -> 2026.5.18
Upgrading cursor
3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768
Upgrading mole
1.41.0 -> 1.42.0`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	want := []string{
		"usage 3.3.0 -> 3.4.0",
		"mise 2026.5.16 -> 2026.5.18",
		"cursor 3.6.21,e7a7e93f4d75f8272503ecf33cedbaae10114a15 -> 3.6.31,81fcf2931d768",
		"mole 1.41.0 -> 1.42.0",
	}
	if strings.Join(step.Updated, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected only package version summary rows, got %#v", step.Updated)
	}
}

func TestUpdateOutcomeRowsSplitItemAndVersionDetail(t *testing.T) {
	report := updateReport{Steps: []updateStep{{
		Name:    "brew",
		Status:  plan.StatusOK,
		Updated: []string{"usage 3.3.0 -> 3.4.0", "Updated 2 taps (homebrew/core and homebrew/cask)."},
	}}}
	rows := updateOutcomeRows(report, 10, false)
	if len(rows) != 2 {
		t.Fatalf("expected two outcome rows, got %#v", rows)
	}
	if rows[0][2] != "usage" || rows[0][3] != "3.3.0 -> 3.4.0" {
		t.Fatalf("expected item/detail split, got %#v", rows[0])
	}
	if rows[1][2] != "Homebrew taps" || !strings.Contains(rows[1][3], "Updated 2 taps") {
		t.Fatalf("expected Homebrew tap update row, got %#v", rows[1])
	}
}

func TestUpdateOutcomeRowsSplitMiseBumpSkippedItems(t *testing.T) {
	report := updateReport{Steps: []updateStep{{
		Name:         miseBumpProvider,
		Status:       plan.StatusDrift,
		Skipped:      true,
		Reason:       "mise bump candidates available; mode=manual requires item review",
		SkippedItems: []string{"github:openai/codex 0.60.0 -> 0.60.1"},
	}}}
	rows := updateOutcomeRows(report, 10, false)
	if len(rows) != 1 {
		t.Fatalf("expected one outcome row, got %#v", rows)
	}
	if rows[0][2] != "github:openai/codex" || rows[0][3] != "0.60.0 -> 0.60.1" {
		t.Fatalf("expected mise-bump skipped item/detail split, got %#v", rows[0])
	}
}

func TestRunUpdateStepNormalizesHomebrewSkippedWarnings(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "Warning: Skipping oven-sh/bun because it is not trusted. Run `brew trust oven-sh/bun`."}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.SkippedItems) != 1 || step.SkippedItems[0] != "oven-sh/bun skipped: because it is not trusted. Run `brew trust oven-sh/bun`." {
		t.Fatalf("expected normalized skipped item, got %#v", step.SkippedItems)
	}
	rows := updateOutcomeRows(updateReport{Steps: []updateStep{step}}, 10, false)
	if len(rows) != 1 || rows[0][2] != "oven-sh/bun" || !strings.Contains(rows[0][3], "not trusted") {
		t.Fatalf("expected skipped outcome row to use item name, got %#v", rows)
	}
}

func TestLastUpdateReportNormalizesCachedOutcomes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cached := updateReport{Steps: []updateStep{{
		Name: "brew",
		Updated: []string{
			"mole 1.41.0 -> 1.42.0 (3.5MB)",
			"Upgraded 3 outdated packages",
			"mole 1.41.0 -> 1.42.0",
			"Updated Homebrew from 2920e720fa to 6a64c5ef91.",
		},
		SkippedItems: []string{
			"Warning: Skipping oven-sh/bun because it is not trusted. Run `brew trust oven-sh/bun`.",
			"Already up-to-date.",
		},
	}}}
	_ = saveLastUpdateReport(cached)
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected cached update report to load")
	}
	report := filterUpdateReport(entry.Report, lastReportOptions{})
	step := report.Steps[0]
	wantUpdated := []string{
		"mole 1.41.0 -> 1.42.0",
		"Updated Homebrew from 2920e720fa to 6a64c5ef91.",
	}
	if strings.Join(step.Updated, "\n") != strings.Join(wantUpdated, "\n") {
		t.Fatalf("expected cached update outcomes to be normalized, got %#v", step.Updated)
	}
	if len(step.SkippedItems) != 1 || step.SkippedItems[0] != "oven-sh/bun skipped: because it is not trusted. Run `brew trust oven-sh/bun`." {
		t.Fatalf("expected cached skipped outcomes to be normalized, got %#v", step.SkippedItems)
	}
	rows := updateOutcomeRows(report, 10, false)
	if len(rows) != 3 || rows[0][2] != "mole" || rows[2][2] != "oven-sh/bun" {
		t.Fatalf("expected normalized cached outcome rows, got %#v", rows)
	}
}

func TestRunUpdateStepIgnoresGenericHomebrewProgressLogs(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `Updating Homebrew...
Auto-updating Homebrew...
Adjust how often this is run with $HOMEBREW_AUTO_UPDATE_SECS or disable with $HOMEBREW_NO_AUTO_UPDATE.
Already up-to-date.`}}
	step := runUpdateStep(context.Background(), fake, updateSteps()[0], false)
	if len(step.Updated) != 0 || len(step.SkippedItems) != 0 {
		t.Fatalf("generic Homebrew progress logs should not become outcome rows, got updated=%#v skipped=%#v", step.Updated, step.SkippedItems)
	}
}

func TestRunUpdateStepStreamsProviderLogsWhenRequested(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "brew stdout\n", Stderr: "brew stderr\n"}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	step := updateSteps()[0]
	result := runUpdateStepWithWriters(updateStepRunOptions{
		Context: context.Background(),
		Runner:  fake,
		Step:    step,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	if result.Status != plan.StatusOK {
		t.Fatalf("expected ok step, got %#v", result)
	}
	if !strings.Contains(stdout.String(), "brew stdout") || !strings.Contains(stderr.String(), "brew stderr") {
		t.Fatalf("expected provider logs to stream, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestShouldStreamUpdateProviderLogs(t *testing.T) {
	plainOpts, err := parseUpdateOptions([]string{"--plain"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		opts updateOptions
		want bool
	}{
		{
			name: "text update streams provider logs",
			opts: updateOptions{format: "text"},
			want: true,
		},
		{
			name: "interactive text update still streams provider logs",
			opts: updateOptions{format: "text", tui: true},
			want: true,
		},
		{
			name: "plain text update still streams provider logs",
			opts: plainOpts,
			want: true,
		},
		{
			name: "dry run keeps deterministic output",
			opts: updateOptions{format: "text", dryRun: true, tui: true},
			want: false,
		},
		{
			name: "json keeps deterministic output",
			opts: updateOptions{format: "json"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStreamUpdateProviderLogs(tt.opts); got != tt.want {
				t.Fatalf("shouldStreamUpdateProviderLogs()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateProviderStdoutWriterUsesStderrForInteractiveTTY(t *testing.T) {
	if got := updateProviderStdoutWriterForTerminal(true, true); got != os.Stderr {
		t.Fatalf("expected interactive provider stdout to stream to stderr, got %#v", got)
	}
	if got := updateProviderStdoutWriterForTerminal(false, true); got != os.Stdout {
		t.Fatalf("expected non-interactive provider stdout to stay on stdout, got %#v", got)
	}
	if got := updateProviderStdoutWriterForTerminal(true, false); got != os.Stdout {
		t.Fatalf("expected redirected provider stdout to stay on stdout, got %#v", got)
	}
}

func TestUpdateStepSummaryTextReportsSkippedHeldSteps(t *testing.T) {
	got := updateStepSummaryText([]updateStep{
		{Name: "brew", Status: plan.StatusHeld, Skipped: true, SkippedItems: []string{"security held"}},
		{Name: "mise", Status: plan.StatusOK, Updated: []string{"node 22 -> 24"}},
	})
	if got != "2 provider steps, 1 updated items, 1 deferred items, 1 held steps, 1 skipped steps" {
		t.Fatalf("unexpected update step summary: %q", got)
	}
}

func TestUpdateOutcomeRowsShowsBrewHeldCandidateItem(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	step := updateStep{
		Name:    "brew",
		Status:  plan.StatusHeld,
		Skipped: true,
		SkippedItems: updateSafetySkippedSummaries([]safetyFinding{{
			Provider:       "brew",
			Kind:           "cask",
			Name:           "wezterm@nightly",
			CurrentVersion: "latest",
			Decision:       "review",
			Reason:         "Homebrew cask download host differs from homepage host; vendor provenance review required",
		}}),
	}
	rows := updateOutcomeRows(updateReport{Steps: []updateStep{step}}, 10, false)
	if len(rows) != 1 {
		t.Fatalf("expected one skipped row, got %#v", rows)
	}
	if rows[0][2] != "wezterm@nightly" {
		t.Fatalf("expected skipped row item name, got %#v", rows[0])
	}
	if !strings.Contains(rows[0][3], "latest review") || !strings.Contains(rows[0][3], "Homebrew cask") {
		t.Fatalf("expected skipped row detail to include version, decision, and localized reason, got %#v", rows[0])
	}
}

func TestPrintUpdateTextIncludesSkippedStepStatus(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Report:   "/tmp/last-update.json",
		Security: "strict",
		Steps: []updateStep{
			{Name: "brew", Command: []string{"brew", "upgrade"}, Status: plan.StatusHeld, Skipped: true, Reason: "security=strict held update because safety gate requires review"},
			{Name: "mise", Command: []string{"mise", "upgrade"}, Status: plan.StatusOK, Updated: []string{"node 22.0.0 -> 22.1.0"}},
		},
	})
	got := buffer.String()
	for _, want := range []string{"update summary: 2 provider steps, 1 updated items, 1 deferred items, 1 held steps, 1 skipped steps", "update outcome", "node", "22.0.0 -> 22.1.0", "skipped", "brew", "yes", "reason: security=strict held update"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected update text to include %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "brew upgrade") || strings.Contains(got, "mise upgrade") || strings.Contains(got, "command") {
		t.Fatalf("expected human update text to hide raw commands, got %q", got)
	}
}

func TestSafetyHumanTextLocalizesReleaseAgeWarnings(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	finding := safetyFinding{
		Provider:          "brew",
		Kind:              "brew",
		Name:              "libomp",
		InstalledVersions: []string{"22.1.6"},
		CurrentVersion:    "22.1.7",
		Decision:          "hold",
		Reason:            "candidate release is too new: age 0 days, minimum 3 days",
		Remediation:       "wait until the release reaches the minimum age or allow temporarily by policy after review",
	}
	report := updateReport{
		Security: "warn",
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{finding},
		}},
	}
	var dashboard bytes.Buffer
	printUpdateSafetyDashboard(&dashboard, report, false)
	if got := dashboard.String(); !strings.Contains(got, "候補リリースが新しすぎます") || strings.Contains(got, "candidate release is too new") {
		t.Fatalf("expected localized safety dashboard reason, got %q", got)
	}
	var details bytes.Buffer
	printSafetyFindingDetails(&details, report.Safety, false)
	if got := details.String(); !strings.Contains(got, "候補リリースが新しすぎます") || !strings.Contains(got, "リリースが最小経過日数に達するまで") {
		t.Fatalf("expected localized safety details, got %q", got)
	}
	row := safetyFindingDetailRow(report.Safety[0], finding)
	metadata := strings.Join(row.Metadata, "\n")
	if !strings.Contains(row.Summary, "候補リリースが新しすぎます") || !strings.Contains(row.Detail, "リリースが最小経過日数に達するまで") || !strings.Contains(metadata, "候補リリースが新しすぎます") {
		t.Fatalf("expected localized safety detail row, row=%#v metadata=%q", row, metadata)
	}
}

func TestSecurityReviewTextLocalizesCandidateReasons(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	report := securityReviewReport{
		Status: plan.StatusHeld,
		Root:   "/repo",
		Candidates: []securityReviewCandidate{{
			Provider:    "github-repo",
			Kind:        "github",
			Name:        "owner/repo",
			Decision:    "review",
			Reason:      "repository is archived",
			Remediation: "replace the archived repository source or add a temporary policy override after review",
			Prompt:      "Review updev security candidate github-repo/github owner/repo.",
		}},
	}
	var buffer bytes.Buffer
	printSecurityReviewText(&buffer, report)
	got := buffer.String()
	if !strings.Contains(got, "reason: repository が archived です") || !strings.Contains(got, "remediation: archived repository source を置き換える") {
		t.Fatalf("expected localized review candidate reason/remediation, got %q", got)
	}
	if strings.Contains(got, "reason: repository is archived") || strings.Contains(got, "remediation: replace the archived repository source") {
		t.Fatalf("expected review candidate text to avoid English reason/remediation, got %q", got)
	}
}

func TestPrintUpdateTextOmitsEmptyDetailColumn(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusOK,
		Root:     "/repo",
		Security: "off",
		Steps: []updateStep{
			{Name: "brew", Status: plan.StatusOK},
			{Name: "mise", Status: plan.StatusOK},
		},
	})
	got := buffer.String()
	if strings.Contains(got, "detail") || strings.Contains(got, "詳細") {
		t.Fatalf("expected empty detail column to be omitted, got %q", got)
	}
}

func TestUpdateOutcomeRowsPreferGateProviderForSecurityFindings(t *testing.T) {
	rows := updateOutcomeRows(updateReport{
		Safety: []safetyGate{{
			Provider: "vscode",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider:          "brew",
				Kind:              "vscode",
				Name:              "publisher.extension",
				InstalledVersions: []string{"1.0.0"},
				CurrentVersion:    "1.1.0",
				Decision:          "hold",
			}},
		}},
	}, 10, false)
	if len(rows) != 1 || rows[0][1] != "vscode" || rows[0][3] != "1.0.0 -> 1.1.0" {
		t.Fatalf("expected vscode provider and concise version detail, got %#v", rows)
	}
}

func TestUpdateOutcomeRowsColorUpdatedItems(t *testing.T) {
	rows := updateOutcomeRows(updateReport{
		Steps: []updateStep{{
			Name:    "mise",
			Status:  plan.StatusOK,
			Updated: []string{"node 22.0.0 -> 24.0.0"},
		}},
	}, 10, true)
	if len(rows) != 1 {
		t.Fatalf("expected updated row, got %#v", rows)
	}
	for _, column := range []int{0, 2, 3} {
		if !strings.Contains(rows[0][column], "\033[32m") {
			t.Fatalf("expected updated column %d to be green, got %#v", column, rows[0])
		}
	}
}

func TestPrintUpdateTextShowsWarnModeSafetyAsWarnings(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusOK,
		Root:     "/repo",
		Security: "warn",
		Steps: []updateStep{{
			Name:    "brew",
			Status:  plan.StatusOK,
			Updated: []string{"mise 2026.5.16 -> 2026.5.18"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Summary:  &safetySummary{Findings: 1, Hold: 1},
			Findings: []safetyFinding{{
				Provider:          "brew",
				Kind:              "brew",
				Name:              "mise",
				InstalledVersions: []string{"2026.5.16"},
				CurrentVersion:    "2026.5.18",
				Decision:          "hold",
				Reason:            "candidate release is too new",
			}},
		}},
	})
	got := buffer.String()
	for _, want := range []string{"safety summary: 1 gates, 1 warnings", "warning", "mise", "2026.5.16 -> 2026.5.18"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected warn-mode update text to include %q, got %q", want, got)
		}
	}
	for _, unwanted := range []string{"held", "hold"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("expected warn-mode update text to avoid %q wording, got %q", unwanted, got)
		}
	}
}

func TestPrintUpdateTextUsesCompactInventoryDashboard(t *testing.T) {
	var buffer bytes.Buffer
	printUpdateTextTo(&buffer, updateReport{
		Status:   plan.StatusDrift,
		Root:     "/repo",
		Security: "off",
		Steps: []updateStep{
			{Name: "brew", Command: []string{"brew", "upgrade"}, Status: plan.StatusOK},
		},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Providers: []plan.ProviderSummary{
				{Name: "brew", Desired: 1, Live: 2, Extra: 1},
				{Name: "mise", Desired: 1, Live: 1},
			},
			Items: []plan.Item{
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true, Detail: "JSON processor"},
				{Provider: "brew", Kind: "cask", Name: "warp", Status: plan.StatusExtra, Live: true, Detail: inventoryannotate.ProfileMismatchDetail("personal")},
				{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusOK, Desired: true, Live: true, Detail: "Node runtime"},
			},
		},
	})
	got := buffer.String()
	for _, want := range []string{"inventory drift", "top inventory items", "jq", "profile", "profile-mismatch"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected compact update dashboard to include %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "updev last --format json") || strings.Contains(got, "\nnext\n") {
		t.Fatalf("expected human update dashboard to avoid follow-up command lists, got %q", got)
	}
	if strings.Contains(got, "brew / brew") || strings.Contains(got, "Node runtime") {
		t.Fatalf("expected update dashboard to avoid full inventory tables, got %q", got)
	}
}

func TestSaveAndLoadLastUpdateReport(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	path := saveLastUpdateReport(updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Security: "strict",
		Steps: []updateStep{
			{Name: "brew", Status: plan.StatusHeld, Skipped: true},
		},
	})
	if path == "" {
		t.Fatal("expected cached report path")
	}
	if !strings.HasPrefix(path, filepath.Join(cacheHome, "updev", "reports")) {
		t.Fatalf("expected report under XDG cache, got %q", path)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected cached report to load")
	}
	if entry.Report.Status != plan.StatusHeld || entry.Report.Root != "/repo" || entry.Report.Report != path {
		t.Fatalf("unexpected cached report: %#v", entry)
	}
}

func TestDryRunUpdateReportDoesNotReplaceLastUpdateEvidence(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	realPath := saveLastUpdateReport(updateReport{
		Status: plan.StatusHeld,
		Root:   "/repo",
		Steps: []updateStep{{
			Name:    "brew",
			Status:  plan.StatusOK,
			Updated: []string{"jq 1.7 -> 1.8.1"},
		}},
	})
	dryPath := saveLastUpdateReport(updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		DryRun: true,
		Steps:  []updateStep{{Name: "brew", Status: plan.StatusOK}},
	})
	if realPath == "" || dryPath == "" || realPath == dryPath {
		t.Fatalf("expected separate real and dry-run report paths, real=%q dry=%q", realPath, dryPath)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected real last update report to remain loadable")
	}
	if entry.Report.DryRun || entry.Report.Status != plan.StatusHeld || len(entry.Report.Steps) != 1 || len(entry.Report.Steps[0].Updated) != 1 {
		t.Fatalf("expected dry-run report not to replace real update evidence, got %#v", entry.Report)
	}
	if !strings.Contains(dryPath, "last-dry-run.json") {
		t.Fatalf("expected dry-run report path, got %q", dryPath)
	}
}

func TestPrintLastReportTextDoesNotRepeatUpdateHeader(t *testing.T) {
	var buffer bytes.Buffer
	printLastReportText(&buffer, updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status:   plan.StatusOK,
			Root:     "/repo",
			Security: "off",
			Steps: []updateStep{
				{Name: "brew", Status: plan.StatusOK},
			},
		},
	}, lastReportOptions{section: "summary"})
	got := buffer.String()
	if !strings.Contains(got, "updev last ok") || strings.Contains(got, "updev update") {
		t.Fatalf("expected last report to reuse update body without update header, got %q", got)
	}
}

func TestLastReportHubUsesTopAnchoredUpdateSummary(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Security: "strict",
		Steps: []updateStep{{
			Name:         "mise-bump",
			Status:       plan.StatusHeld,
			SkippedItems: []string{"aqua:modem-dev/hunk 0.14.0 -> 0.14.1"},
		}},
		Report: "/tmp/last-update.json",
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, lastReportHubDefaultAction("summary"), updateHubActionDashboard, false)
	model.height = 16
	model.applyDashboardSize(&model.dashboard)
	view := model.View().Content
	rootIndex := strings.Index(view, "root:")
	outcomeIndex := strings.Index(view, "update outcome")
	headerIndex := strings.Index(view, "type")
	rowIndex := strings.Index(view, "skipped")
	if rootIndex < 0 || outcomeIndex < 0 || headerIndex < 0 || rowIndex < 0 || !(rootIndex < outcomeIndex && outcomeIndex < headerIndex && headerIndex < rowIndex) {
		t.Fatalf("expected last-report hub summary to stay top-anchored with title/header before rows:\n%s", view)
	}
}

func TestPlainLastInventoryDetailsStayCachedReportOnly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{
			Status: plan.StatusOK,
			Items: []plan.Item{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "ripgrep",
				Version:  "15.1.0",
				Status:   plan.StatusOK,
				Detail:   "search tool",
				Desired:  true,
				Live:     true,
			}},
		},
	}
	var out bytes.Buffer
	printLastInventorySection(&out, report, lastReportOptions{section: "inventory", provider: "brew", query: "ripgrep", details: true}, false)
	text := out.String()
	if strings.Contains(text, "backend evidence:") || strings.Contains(text, "backend 整理") {
		t.Fatalf("expected plain last inventory details to avoid backend evidence injection:\n%s", text)
	}
	if !strings.Contains(text, "ripgrep") {
		t.Fatalf("expected cached inventory row:\n%s", text)
	}
}

func TestBuildUpdateReportSectionViewFiltersInventory(t *testing.T) {
	entry := updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status: plan.StatusDrift,
			Inventory: plan.Report{
				Status: plan.StatusDrift,
				Providers: []plan.ProviderSummary{
					{Name: "brew", Desired: 1, Live: 2, Extra: 1},
					{Name: "mise", Desired: 1, Live: 1},
				},
				Items: []plan.Item{
					{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true},
					{Provider: "mise", Kind: "tool", Name: "node", Status: plan.StatusOK, Desired: true, Live: true},
				},
			},
		},
	}
	view := buildUpdateReportSectionView(entry, lastReportOptions{section: "inventory", provider: "brew", status: "attention"})
	if view.Section != "inventory" || view.Inventory == nil {
		t.Fatalf("expected inventory view, got %#v", view)
	}
	if view.Status != plan.StatusDrift {
		t.Fatalf("expected inventory section status to come from inventory, got %s", view.Status)
	}
	if len(view.Inventory.Items) != 1 || view.Inventory.Items[0].Name != "jq" {
		t.Fatalf("expected filtered attention inventory item, got %#v", view.Inventory.Items)
	}
	if view.Summary.InventoryAttention != 1 || view.Filters["provider"] != "brew" {
		t.Fatalf("unexpected view summary/filters: %#v", view)
	}
}

func TestFilterPlanProvidersKeepsProviderWhenOnlyItemQueryIsSet(t *testing.T) {
	providers := []plan.ProviderSummary{{Name: "brew", Desired: 1, Live: 1}}
	filtered := filterPlanProviders(providers, lastReportOptions{provider: "brew", query: "ripgrep"})
	if len(filtered) != 1 || filtered[0].Name != "brew" {
		t.Fatalf("expected provider filter to survive item query, got %#v", filtered)
	}
}

func TestPrintLastReportSecurityDetails(t *testing.T) {
	var buffer bytes.Buffer
	printLastReportText(&buffer, updateReportCacheEntry{
		Version:   1,
		Type:      "update",
		CreatedAt: time.Date(2026, 5, 30, 1, 0, 0, 0, time.UTC),
		Report: updateReport{
			Status: plan.StatusHeld,
			Root:   "/repo",
			Safety: []safetyGate{{
				Provider: "brew",
				Status:   plan.StatusHeld,
				Findings: []safetyFinding{{
					Provider:    "brew",
					Kind:        "cask",
					Name:        "demo",
					Decision:    "hold",
					Reason:      "needs provenance review",
					Remediation: "verify upstream",
					Evidence:    []string{"unsigned cask"},
				}},
			}},
		},
	}, lastReportOptions{section: "security", status: "attention", details: true})
	got := buffer.String()
	for _, want := range []string{"section: security", "security details", "brew/cask demo", "needs provenance review", "unsigned cask"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected security detail output to include %q, got %q", want, got)
		}
	}
}

func TestUpdateDetailRowsExposeInventorySecurityAndLogs(t *testing.T) {
	report := updateReport{
		Status: plan.StatusHeld,
		Steps: []updateStep{{
			Name:         "brew",
			Command:      []string{"brew", "upgrade"},
			Status:       plan.StatusHeld,
			Reason:       "security review required",
			Stdout:       "kept current version",
			Updated:      []string{"jq 1.7 -> 1.8.1"},
			SkippedItems: []string{"demo held by policy"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider:      "brew",
				Kind:          "cask",
				Name:          "demo",
				Decision:      "hold",
				Reason:        "needs provenance review",
				Remediation:   "verify upstream",
				Evidence:      []string{"unsigned cask"},
				FixedVersions: []string{"1.2.3"},
			}, {
				Provider: "brew",
				Kind:     "brew",
				Name:     "safe",
				Decision: "allow",
				Reason:   "already trusted",
			}},
		}},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Items: []plan.Item{
				{Provider: "brew", Kind: "brew", Name: "jq", Status: plan.StatusExtra, Live: true, Detail: "extra package"},
				{Provider: "brew", Kind: "brew", Name: "git", Status: plan.StatusOK, Desired: true, Live: true},
			},
		},
	}
	if rows := updateInventoryDetailRows(report); len(rows) != 1 || rows[0].Title != "brew/brew jq" {
		t.Fatalf("expected attention inventory detail row, got %#v", rows)
	}
	securityRows := updateSecurityDetailRows(report)
	if len(securityRows) != 1 || !strings.Contains(strings.Join(securityRows[0].Metadata, " "), "fixed: 1.2.3") || !strings.Contains(strings.Join(securityRows[0].Metadata, " "), "decision: hold") {
		t.Fatalf("expected security finding metadata, got %#v", securityRows)
	}
	if len(securityRows[0].Actions) != 5 || !strings.Contains(securityRows[0].Actions[0].Value, securityDetailActionPrefix) {
		t.Fatalf("expected held security row to expose policy actions, got %#v", securityRows[0].Actions)
	}
	allowRows := updateSecurityDetailRowsForFilter(report, lastReportOptions{section: "security", status: "allow"})
	if len(allowRows) != 2 || allowRows[1].Title != "brew/brew safe" {
		t.Fatalf("expected explicit allow filter to show allow findings, got %#v", allowRows)
	}
	logRows := updateLogDetailRows(report)
	if len(logRows) != 3 {
		t.Fatalf("expected updated item, deferred item, and provider log rows, got %#v", logRows)
	}
	if logRows[0].Status != "updated" || logRows[0].Summary != "jq 1.7 -> 1.8.1" {
		t.Fatalf("expected first update detail row to be item-level updated row, got %#v", logRows[0])
	}
	if logRows[1].Status != "held" || logRows[1].Summary != "demo held by policy" || len(logRows[1].Actions) != 1 {
		t.Fatalf("expected second update detail row to be held item row with security action, got %#v", logRows[1])
	}
	providerLog := logRows[2]
	logMetadata := strings.Join(providerLog.Metadata, " ")
	if !strings.Contains(logMetadata, "stdout: kept current version") || !strings.Contains(logMetadata, "updated: jq") || !strings.Contains(logMetadata, "deferred: demo held") {
		t.Fatalf("expected update provider log metadata, got %#v", providerLog)
	}
	if providerLog.Summary != "security review required" {
		t.Fatalf("expected reason to remain update log summary, got %#v", providerLog)
	}
	if len(providerLog.Actions) != 1 || providerLog.Actions[0].Value != updateHubActionSecurity {
		t.Fatalf("expected held update log row to link to security detail actions, got %#v", providerLog.Actions)
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(securityRows[0].Actions[0].Value); !ok || action != "allow-7d-rerun" || provider != "brew" || kind != "cask" || name != "demo" {
		t.Fatalf("unexpected security detail action parse: action=%q provider=%q kind=%q name=%q ok=%v", action, provider, kind, name, ok)
	}
	nonRerunnable := securityDetailActions(safetyGate{Provider: "github-repo"}, safetyFinding{Kind: "repo", Name: "owner/tool", Decision: "hold"})
	if len(nonRerunnable) != 3 || strings.Contains(nonRerunnable[0].Value, "rerun") {
		t.Fatalf("expected non-update provider security actions to omit rerun, got %#v", nonRerunnable)
	}
	if action, _, _, _, ok := parseSecurityDetailAction(nonRerunnable[0].Value); !ok || action != "allow-custom" {
		t.Fatalf("expected custom allow to be the first non-rerunnable action, got %#v", nonRerunnable)
	}
	bumpReviewActions := securityDetailActions(safetyGate{Provider: miseBumpProvider}, safetyFinding{
		Provider:       "mise",
		Kind:           "tool",
		Name:           "github:openai/codex",
		CurrentVersion: "0.60.1",
		Decision:       "review",
		Source:         miseBumpSource,
	})
	for _, action := range bumpReviewActions {
		if strings.Contains(action.Value, "rerun") {
			t.Fatalf("expected mise-bump review actions to avoid normal provider rerun, got %#v", bumpReviewActions)
		}
	}
	bumpAllowActions := securityDetailActions(safetyGate{Provider: miseBumpProvider}, safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseNativeReleaseAgeSource,
	})
	if len(bumpAllowActions) != 1 || bumpAllowActions[0].Value != miseBumpDetailActionValue("github:openai/codex") {
		t.Fatalf("expected allowed mise-bump finding to route to scoped bump apply, got %#v", bumpAllowActions)
	}
}

func TestHomebrewTrustSecurityDetailActionsPreferItemScopedTargets(t *testing.T) {
	formula := safetyFinding{
		Provider:     "brew",
		Kind:         "brew",
		Name:         "custom-tool",
		Tap:          "vendor/tap",
		Decision:     "review",
		TrustStatus:  "needs-review",
		TrustTarget:  "vendor/tap/custom-tool",
		TrustCommand: "brew trust --formula vendor/tap/custom-tool",
	}
	actions := securityDetailActions(safetyGate{Provider: "brew"}, formula)
	if len(actions) < 1 {
		t.Fatalf("expected Homebrew trust action, got %#v", actions)
	}
	action, provider, kind, name, ok := parseSecurityDetailAction(actions[0].Value)
	if !ok || action != securityActionBrewTrustFormula || provider != "brew" || kind != "formula" || name != "vendor/tap/custom-tool" {
		t.Fatalf("expected item-scoped formula trust action, action=%q provider=%q kind=%q name=%q ok=%v actions=%#v", action, provider, kind, name, ok, actions)
	}
	if !securityDetailActionRequiresConfirmation(action) {
		t.Fatalf("expected Homebrew trust action to require confirmation")
	}
	if command, ok := homebrewTrustCommandForSecurityAction(action, name); !ok || joinCommand(command) != "brew trust --formula vendor/tap/custom-tool" {
		t.Fatalf("expected formula trust command, got command=%#v ok=%v", command, ok)
	}
	formula.TrustCommandArgv = []string{"brew", "trust", "--formula", "other/tool"}
	_, _, _, displayCommand, ok := homebrewTrustActionParts(formula)
	if !ok || displayCommand != "brew trust --formula vendor/tap/custom-tool" {
		t.Fatalf("expected trust action display command to be rebuilt from validated target, got %q ok=%v", displayCommand, ok)
	}

	tapActions := securityDetailActions(safetyGate{Provider: "brew"}, safetyFinding{
		Provider: "brew",
		Kind:     "tap",
		Name:     "vendor/tap",
		Decision: "review",
	})
	if len(tapActions) == 0 {
		t.Fatalf("expected whole-tap trust action for tap finding")
	}
	tapAction, _, tapKind, tapName, ok := parseSecurityDetailAction(tapActions[0].Value)
	if !ok || tapAction != securityActionBrewTrustTap || tapKind != "tap" || tapName != "vendor/tap" {
		t.Fatalf("expected tap trust action, action=%q kind=%q name=%q ok=%v actions=%#v", tapAction, tapKind, tapName, ok, tapActions)
	}

	official := securityDetailActions(safetyGate{Provider: "brew"}, safetyFinding{
		Provider: "brew",
		Kind:     "brew",
		Name:     "git",
		Tap:      "homebrew/core",
		Decision: "review",
	})
	for _, action := range official {
		if strings.Contains(action.Value, "brew-trust") {
			t.Fatalf("expected official taps to avoid trust write actions, got %#v", official)
		}
	}
	if _, ok := homebrewTrustCommandForSecurityAction(securityActionBrewTrustFormula, "--bad"); ok {
		t.Fatalf("expected option-like Homebrew trust target to be rejected")
	}
}

func TestUpdateLogDetailRowsDistinguishSkippedErrorAndPreserveLogLines(t *testing.T) {
	rows := updateLogDetailRows(updateReport{Steps: []updateStep{
		{
			Name:    "brew",
			Command: []string{"brew", "upgrade"},
			Status:  plan.StatusOK,
			Skipped: true,
			Reason:  "dry run",
			Stdout:  "line one\nline two",
		},
		{
			Name:    "mise",
			Command: []string{"mise", "upgrade"},
			Status:  plan.StatusError,
			Stderr:  "error one\nerror two",
		},
	}})
	if len(rows) != 2 {
		t.Fatalf("expected skipped and error provider log rows, got %#v", rows)
	}
	if rows[0].Status != "skipped" || !strings.Contains(strings.Join(rows[0].Metadata, " "), "skipped: true") {
		t.Fatalf("expected skipped provider row to expose skipped state, got %#v", rows[0])
	}
	if rows[1].Status != string(plan.StatusError) || rows[1].Summary != "error one error two" {
		t.Fatalf("expected error provider row to summarize stderr, got %#v", rows[1])
	}
	expanded := strings.Join(detailBrowserExpandedLinesWithWidth(rows[0], 80), "\n")
	if !strings.Contains(expanded, "stdout: line one") || !strings.Contains(expanded, "line two") {
		t.Fatalf("expected expanded update log to preserve stdout newlines, got %q", expanded)
	}
}

func TestUpdateLogRouteQueryFiltersItemRows(t *testing.T) {
	report := filterUpdateReport(updateReport{Steps: []updateStep{{
		Name:   miseBumpProvider,
		Status: plan.StatusDrift,
		Reason: "mise bump candidates available; mode=manual requires item review",
		SkippedItems: []string{
			"aqua:modem-dev/hunk 0.14.0 -> 0.14.1",
			"cloudflared 2026.5.0 -> 2026.5.2",
			"copilot-cli 1.0.48 -> 1.0.61",
		},
	}}}, lastReportOptions{section: "logs", provider: miseBumpProvider, query: "cloudflared"})
	rows := updateLogDetailRows(report)
	if len(rows) != 2 {
		t.Fatalf("expected one matching item row plus provider summary, got %#v", rows)
	}
	joined := strings.Join([]string{rows[0].Summary, rows[1].Summary, strings.Join(rows[1].Metadata, " ")}, " ")
	if !strings.Contains(joined, "cloudflared") || strings.Contains(joined, "aqua:modem-dev/hunk") || strings.Contains(joined, "copilot-cli") {
		t.Fatalf("expected query-filtered update logs to keep only cloudflared item, got %#v", rows)
	}
}

func TestUpdateHubChoicesExposeNavigationTargets(t *testing.T) {
	manualPlan := inventoryPlanReport{ActionCounts: map[string]int{"adopt-brew": 2}, AttentionCount: 2}
	backendPlan := backendPlanReport{Findings: []backendFinding{{Name: "ripgrep", RecommendedName: "ripgrep"}}}
	choices := updateHubChoices(updateReport{Safety: []safetyGate{{Provider: "brew", Status: plan.StatusHeld}}}, manualPlan, backendPlan, updateHubActionManualPlan)
	values := map[string]bool{}
	for _, choice := range choices {
		values[choice.Value] = true
	}
	for _, want := range []string{updateHubActionInventoryAll, updateHubActionInventoryAttention, updateHubActionInventoryDetails, updateHubActionManualPlan, updateHubActionBackends, updateHubActionUpdatesFilter, updateHubActionSecurity, updateHubActionSecurityFilter, updateHubActionLogs, updateHubActionJSON, updevActionExit} {
		if !values[want] {
			t.Fatalf("expected update hub choice %q in %#v", want, choices)
		}
	}
	selected := ""
	for _, choice := range choices {
		if choice.Selected {
			selected = choice.Value
		}
	}
	if selected != updateHubActionManualPlan {
		t.Fatalf("expected manual plan to be selected when review actions exist, got %#v", choices)
	}
	if manualPlan.AttentionCount != 2 {
		t.Fatalf("expected manual plan attention count to include adoption actions")
	}
}

func TestUpdateDashboardDetailRowsExposeHubActions(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Report:   "/tmp/last-update.json",
		Security: "strict",
		Steps: []updateStep{{
			Name:         "brew",
			Status:       plan.StatusHeld,
			Reason:       "security=strict held update because safety gate requires review",
			Updated:      []string{"jq 1.7 -> 1.8.1"},
			SkippedItems: []string{"demo held"},
		}},
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []safetyFinding{{
				Provider: "brew",
				Kind:     "cask",
				Name:     "demo",
				Decision: "hold",
				Reason:   "review provenance",
			}},
		}},
		Inventory: plan.Report{
			Status: plan.StatusDrift,
			Items: []plan.Item{{
				Provider: "brew",
				Kind:     "brew",
				Name:     "jq",
				Status:   plan.StatusExtra,
			}},
			Providers: []plan.ProviderSummary{{Name: "brew", Desired: 1, Live: 2}},
		},
	}
	manualPlan := inventoryPlanReport{ActionCounts: map[string]int{"needs-review": 1}, AttentionCount: 1}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{Type: "mise-backend-rewrite"}}}
	rows := updateDashboardDetailRows(report, manualPlan, backendPlan)
	byTitle := map[string]detailBrowserRow{}
	for _, row := range rows {
		byTitle[row.Title] = row
	}
	for _, tc := range []struct {
		title string
		want  string
	}{
		{"Update steps", updateHubActionUpdatesFilter},
		{"Security", updateHubActionSecurity},
		{"Inventory", updateHubActionInventoryAll},
		{"Manual review", updateHubActionManualPlan},
		{"Backend convergence", updateHubActionBackends},
		{"Full report", updateHubActionFull},
	} {
		row := byTitle[tc.title]
		if len(row.Actions) == 0 || row.Actions[0].Value != tc.want {
			t.Fatalf("expected dashboard row %q to expose action %q, got %#v", tc.title, tc.want, row.Actions)
		}
	}
	backendMetadata := strings.Join(byTitle["Backend convergence"].Metadata, " ")
	if !strings.Contains(backendMetadata, "mise/github") || !strings.Contains(backendMetadata, "safe actions:") {
		t.Fatalf("expected backend preference metadata to include mise/github, got %#v", byTitle["Backend convergence"].Metadata)
	}
	dashboardView := newDetailBrowserModel("updev dashboard", rows, detailBrowserState{}, false).View().Content
	if !strings.Contains(dashboardView, "focused actions: a/1=filter updates") {
		t.Fatalf("expected dashboard view to expose focused row action hint:\n%s", dashboardView)
	}
	summaryModel := newUpdateSummaryBrowserModel(updateSummaryBrowserOptions{
		Title:       updateHubTitle(report),
		Report:      report,
		ManualPlan:  manualPlan,
		BackendPlan: backendPlan,
		State:       detailBrowserState{},
		FocusAction: updateHubActionLogs,
	})
	summaryModel.Height = 80
	dashboardView = summaryModel.View().Content
	for _, want := range []string{"updev update held", "root: /repo", "security: strict", "safety summary:", "update summary:", "focused actions:", "a/1=open update details", "updates", "security attention", "inventory drift", "review actions"} {
		if !strings.Contains(dashboardView, want) {
			t.Fatalf("expected update hub view to contain %q:\n%s", want, dashboardView)
		}
	}
	if !strings.Contains(dashboardView, "\n  provider") || !strings.Contains(dashboardView, "\n  brew") || strings.Contains(dashboardView, "\n    brew") {
		t.Fatalf("expected selectable table rows to align with table headers:\n%s", dashboardView)
	}
	if strings.Contains(dashboardView, "\n  report:") || strings.Contains(dashboardView, "\n  レポート:") {
		t.Fatalf("expected report metadata line to stay flush with summary labels:\n%s", dashboardView)
	}
	for _, line := range summaryModel.Lines {
		if strings.HasPrefix(strings.TrimSpace(line.Text), "reason:") || strings.HasPrefix(strings.TrimSpace(line.Text), "理由:") {
			if line.Action != "" {
				t.Fatalf("expected reason line to be non-selectable metadata, got %#v", line)
			}
		}
	}
	focusedLowerSummary := newUpdateSummaryBrowserModel(updateSummaryBrowserOptions{
		Title:       updateHubTitle(report),
		Report:      report,
		ManualPlan:  manualPlan,
		BackendPlan: backendPlan,
		State:       detailBrowserState{},
		FocusAction: updateHubActionManualPlan,
	})
	focusedLowerSummary.Height = 16
	focusedLowerSummary.EnsureSelectedVisible()
	focusedLowerView := focusedLowerSummary.View().Content
	rootIndex := strings.Index(focusedLowerView, "root:")
	outcomeIndex := strings.Index(focusedLowerView, "update outcome")
	headerIndex := strings.Index(focusedLowerView, "type")
	rowIndex := strings.Index(focusedLowerView, "skipped")
	if rootIndex < 0 || outcomeIndex < 0 || headerIndex < 0 || rowIndex < 0 || !(rootIndex < outcomeIndex && outcomeIndex < headerIndex && headerIndex < rowIndex) {
		t.Fatalf("expected initial summary to stay top-anchored with outcome title/header before rows:\n%s", focusedLowerView)
	}
	router := newUpdateHubRouterModel(report, manualPlan, false, backendPlan, false, updateHubActionDashboard, updateHubActionDashboard, false)
	router.detailStates["dashboard"] = detailBrowserState{Selected: 5, Offset: 8}
	router.showDashboard(updateHubActionDashboard)
	returnedView := router.View().Content
	if router.dashboard.State.Offset != 0 || !strings.Contains(returnedView, "root: /repo") {
		t.Fatalf("expected dashboard return to reset scroll to the report top, offset=%d:\n%s", router.dashboard.State.Offset, returnedView)
	}
	summaryActions := updateSummaryActionsByText(summaryModel.Lines)
	for _, tc := range []struct {
		contains string
		want     string
	}{
		{"updates", updateHubActionLogs},
		{"security attention", updateHubActionSecurity},
		{"inventory drift", updateHubActionInventoryAll},
		{"top inventory items", updateHubActionInventoryDetails},
		{"report:", updateHubActionFull},
		{"manual review", updateHubActionManualPlan},
		{"backend convergence", updateHubActionBackends},
	} {
		if got := summaryActions[tc.contains]; got != tc.want {
			t.Fatalf("expected summary row containing %q to route to %q, got %q", tc.contains, tc.want, got)
		}
	}
	if route, ok := firstUpdateSummaryRoute(summaryModel.Lines, "brew"); !ok || route.Base != updateHubActionLogs || route.Provider != "brew" {
		t.Fatalf("expected brew update row to route to provider-filtered logs, route=%+v ok=%v", route, ok)
	}
	reviewFocused := newUpdateSummaryBrowserModel(updateSummaryBrowserOptions{
		Title:       updateHubTitle(report),
		Report:      report,
		ManualPlan:  manualPlan,
		BackendPlan: backendPlan,
		State:       detailBrowserState{},
		FocusAction: updateHubActionManualPlan,
	})
	reviewFocused.Height = 80
	reviewView := reviewFocused.View().Content
	if !strings.Contains(reviewView, "action") || !strings.Contains(reviewView, "summary") {
		t.Fatalf("expected review actions to render as a small table:\n%s", reviewView)
	}
	if strings.Contains(reviewView, "[Enter: open manual review]") {
		t.Fatalf("expected review action row to avoid inline Enter badge:\n%s", reviewView)
	}
	coloredSummaryModel := newUpdateSummaryBrowserModel(updateSummaryBrowserOptions{
		Title:       updateHubTitle(report),
		Report:      report,
		ManualPlan:  manualPlan,
		BackendPlan: backendPlan,
		State:       detailBrowserState{},
		FocusAction: updateHubActionLogs,
		Color:       true,
	})
	coloredSummaryModel.Height = 80
	coloredSummaryView := coloredSummaryModel.View().Content
	if !strings.Contains(coloredSummaryView, "\033[1m\033[35mupdates") || !strings.Contains(coloredSummaryView, "\033[1m\033[35mreview actions") {
		t.Fatalf("expected summary section titles to be visually styled:\n%q", coloredSummaryView)
	}
	if !updateHubActionExists(updateHubActionBackends) || updateHubActionExists("unknown") {
		t.Fatalf("unexpected update hub action existence result")
	}
	for _, tc := range []struct {
		action string
		index  int
		title  string
	}{
		{updateHubActionManualPlan, 3, "Manual review"},
		{updateHubActionBackends, 4, "Backend convergence"},
		{updateHubActionSecurity, 1, "Security"},
		{updateHubActionInventoryDetails, 2, "Inventory"},
		{updateHubActionUpdatesFilter, 0, "Update steps"},
	} {
		index := updateDashboardRowIndexForAction(tc.action)
		if index != tc.index || rows[index].Title != tc.title {
			t.Fatalf("expected dashboard action %q to focus row %d/%q, got %d/%q", tc.action, tc.index, tc.title, index, rows[index].Title)
		}
	}
	for _, tc := range []struct {
		listAction string
		want       string
	}{
		{listHubActionManual, updateHubActionManualPlan},
		{listHubActionBackends, updateHubActionBackends},
		{listHubActionUpdates, updateHubActionLogs},
		{listHubActionSecurity, updateHubActionSecurity},
		{"unknown", ""},
	} {
		if got := updateHubActionFromListAction(tc.listAction); got != tc.want {
			t.Fatalf("list action %q mapped to %q, want %q", tc.listAction, got, tc.want)
		}
	}
	if got := initialUpdateHubAction("", updateHubActionManualPlan); got != updateHubActionDashboard {
		t.Fatalf("expected bare update hub to open summary first, got %q", got)
	}
	if got := initialUpdateHubAction(updateHubActionSecurity, updateHubActionSecurity); got != updateHubActionSecurity {
		t.Fatalf("expected explicit preferred section to open directly, got %q", got)
	}
}

func updateSummaryActionsByText(lines []updateSummaryLine) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		plain := strings.ToLower(line.Text)
		for _, key := range []string{"updates", "security attention", "inventory drift", "top inventory items", "report:", "manual review", "backend convergence"} {
			if strings.Contains(plain, key) && line.Action != "" {
				action := line.Action
				if route, ok := parseUpdateSummaryRoute(action); ok {
					action = route.Base
				}
				out[key] = action
			}
		}
	}
	return out
}

func firstUpdateSummaryRoute(lines []updateSummaryLine, contains string) (updateSummaryRoute, bool) {
	for _, line := range lines {
		if !strings.Contains(strings.ToLower(line.Text), strings.ToLower(contains)) {
			continue
		}
		if route, ok := parseUpdateSummaryRoute(line.Action); ok {
			return route, true
		}
	}
	return updateSummaryRoute{}, false
}

func TestUpdateSummarySecurityRoutesOpenNonEmptyDetails(t *testing.T) {
	providerRoute, _, ok := updateSummaryRouteForTableLine("security", "brew held 1 review Homebrew cask host mismatch")
	if !ok || providerRoute.Provider != "brew" || providerRoute.Query != "" {
		t.Fatalf("expected provider summary security row to route by provider only, route=%+v ok=%v", providerRoute, ok)
	}
	itemRoute, _, ok := updateSummaryRouteForTableLine("security", "hold mise-bump tool/aqua:modem-dev/hunk 0.14.0 -> 0.15.0 release age")
	if !ok || itemRoute.Provider != "mise-bump" || itemRoute.Query != "tool/aqua:modem-dev/hunk" {
		t.Fatalf("expected security item row to route by provider and item identity, route=%+v ok=%v", itemRoute, ok)
	}
	report := updateReport{Security: "strict", Safety: []safetyGate{
		{Provider: "brew", Status: plan.StatusHeld, Findings: []safetyFinding{{Provider: "brew", Kind: "cask", Name: "wezterm@nightly", Decision: "review", Reason: "host mismatch"}}},
		{Provider: "mise", Status: plan.StatusOK},
		{Provider: "mise-bump", Status: plan.StatusHeld, Findings: []safetyFinding{{Provider: "mise-bump", Kind: "tool", Name: "aqua:modem-dev/hunk", Decision: "hold", Reason: "release age"}}},
	}}
	for _, route := range []updateSummaryRoute{
		providerRoute,
		{Base: updateHubActionSecurity, Provider: "mise"},
		itemRoute,
		{Base: updateHubActionSecurity, Provider: "brew", Query: "cask/wezterm@nightly"},
	} {
		opts := lastReportOptions{section: "security", provider: route.Provider, query: route.Query}
		filtered := filterUpdateReport(report, opts)
		if rows := updateSecurityDetailRowsForFilter(filtered, opts); len(rows) == 0 {
			t.Fatalf("expected non-empty security detail rows for route %+v", route)
		}
	}
}

func TestUpdateFacetCounts(t *testing.T) {
	steps := []updateStep{
		{Name: "brew", Status: plan.StatusOK},
		{Name: "brew", Status: plan.StatusHeld},
		{Name: "mise", Status: plan.StatusError},
	}
	if counts := updateStepProviderCounts(steps); counts["brew"] != 2 || counts["mise"] != 1 {
		t.Fatalf("unexpected update provider counts: %#v", counts)
	}
	if counts := updateStepStatusCounts(steps); counts["ok"] != 1 || counts["held"] != 1 || counts["error"] != 1 || counts["attention"] != 2 {
		t.Fatalf("unexpected update status counts: %#v", counts)
	}
	gates := []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusError,
		Error:    "brew metadata unavailable",
	}, {
		Provider: "vscode",
		Status:   plan.StatusHeld,
		Warnings: []string{"publisher unverified"},
		Findings: []safetyFinding{
			{Decision: "hold"},
			{Decision: "review"},
			{Decision: "allow"},
		},
	}}
	if counts := safetyProviderCounts(gates); counts["brew"] != 1 || counts["vscode"] != 4 {
		t.Fatalf("unexpected safety provider counts: %#v", counts)
	}
	if counts := safetyDecisionCounts(gates); counts["error"] != 1 || counts["hold"] != 1 || counts["review"] != 1 || counts["allow"] != 1 || counts["attention"] != 4 {
		t.Fatalf("unexpected safety decision counts: %#v", counts)
	}
}

func TestUpdateExitCodeTreatsHeldAsNonSuccess(t *testing.T) {
	if got := updateExitCode(plan.StatusHeld); got != 2 {
		t.Fatalf("expected held exit code 2, got %d", got)
	}
	if got := updateExitCode(plan.StatusBlocked); got != 3 {
		t.Fatalf("expected blocked exit code 3, got %d", got)
	}
	if got := updateExitCode(plan.StatusError); got != 1 {
		t.Fatalf("expected error exit code 1, got %d", got)
	}
	if got := updateExitCode(plan.StatusOK); got != 0 {
		t.Fatalf("expected ok exit code 0, got %d", got)
	}
}

func TestRunUpdateStepCanBeHeldByStrictSafety(t *testing.T) {
	fake := &fakeCommandRunner{}
	step := runUpdateStepWithHold(context.Background(), fake, updateSteps()[0], false, "security=strict held update")
	if step.Status != plan.StatusHeld || !step.Skipped {
		t.Fatalf("expected held skipped step, got %#v", step)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("held step executed commands: %+v", fake.calls)
	}
}

func TestRunUpdateStrictSafetyHoldsTooNewBrewCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "jq"`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}}
}`))
		case "/repos/jqlang/jq/releases/tags/jq-1.8.1":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2\x00--greedy": {Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict"}, fake)
	if code != 2 {
		t.Fatalf("expected held update exit code 2, got %d", code)
	}
	for _, call := range fake.calls {
		if fakeCommandIsBrewUpgrade(call) {
			t.Fatalf("strict safety hold executed brew upgrade: %#v", fake.calls)
		}
	}
	if !fakeCommandWasCalled(fake.calls, []string{"brew", "update"}) {
		t.Fatalf("strict safety should still refresh Homebrew metadata while holding only unsafe candidates: %#v", fake.calls)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	if entry.Report.Status != plan.StatusHeld || len(entry.Report.Safety) != 2 || entry.Report.Safety[0].Status != plan.StatusHeld {
		t.Fatalf("expected held safety report, got %#v", entry.Report)
	}
	if len(entry.Report.Safety[0].Findings) != 1 || entry.Report.Safety[0].Findings[0].Decision != "hold" {
		t.Fatalf("expected too-new hold finding, got %#v", entry.Report.Safety[0].Findings)
	}
	if finding := entry.Report.Safety[0].Findings[0]; finding.ReasonCode != securityreason.CandidateReleaseTooNew || finding.ReasonArgs["age_days"] == "" {
		t.Fatalf("expected structured too-new finding reason, got %#v", finding)
	}
	var brewStep updateStep
	for _, step := range entry.Report.Steps {
		if step.Name == "brew" {
			brewStep = step
			break
		}
	}
	if brewStep.Status != plan.StatusHeld || !brewStep.Skipped || len(brewStep.SkippedItems) != 1 {
		t.Fatalf("expected item-scoped brew hold, got %#v", brewStep)
	}
	if brewStep.ReasonCode != updatereason.StrictBrewHeld || brewStep.ReasonArgs["held"] != "1" {
		t.Fatalf("expected structured brew hold reason, got %#v", brewStep)
	}
	if !strings.Contains(brewStep.SkippedItems[0], "jq -> 1.8.1 hold") || strings.Contains(brewStep.SkippedItems[0], "security=strict held update") {
		t.Fatalf("expected package-specific hold reason, got %#v", brewStep.SkippedItems)
	}
}

func fakeCommandIsBrewUpgrade(call []string) bool {
	for i, arg := range call {
		if arg == "brew" && i+1 < len(call) && call[i+1] == "upgrade" {
			return true
		}
	}
	return false
}

func updateStepHasCommands(step updateStep, want [][]string) bool {
	if len(step.Commands) != len(want) {
		return false
	}
	for i, command := range step.Commands {
		if strings.Join(command.Command, "\x00") != strings.Join(want[i], "\x00") {
			return false
		}
	}
	return true
}

func TestRunUpdateStepCommandsPreservesOrderAndStopsOnFailure(t *testing.T) {
	step := updateStep{
		Name:    "brew",
		Command: []string{"brew", "upgrade", "--greedy"},
		Commands: []updateCommand{
			{Command: []string{"brew", "update"}},
			{Command: []string{"brew", "upgrade", "--greedy", "jq"}},
			{Command: []string{"brew", "cleanup"}},
		},
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"brew", "update"}, "\x00"):                    {Stdout: "Updated Homebrew metadata"},
		strings.Join([]string{"brew", "upgrade", "--greedy", "jq"}, "\x00"): {Stdout: "jq 1.7 -> 1.8.1", Stderr: "upgrade failed", Code: 1},
		strings.Join([]string{"brew", "cleanup"}, "\x00"):                   {Stdout: "cleanup should not run"},
	}}
	result := runUpdateStepWithHold(context.Background(), fake, step, false, "")
	if result.Status != plan.StatusError {
		t.Fatalf("expected failed subcommand to mark step error, got %#v", result)
	}
	if !containsString(result.Updated, "jq 1.7 -> 1.8.1") {
		t.Fatalf("expected successful output before failure to be retained, got %#v", result.Updated)
	}
	wantCalls := [][]string{
		{"brew", "update"},
		{"brew", "upgrade", "--greedy", "jq"},
	}
	if !sameCommandCalls(fake.calls, wantCalls) {
		t.Fatalf("expected commands to stop before cleanup\nwant=%#v\ngot=%#v", wantCalls, fake.calls)
	}
}

func sameCommandCalls(got [][]string, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if strings.Join(got[i], "\x00") != strings.Join(want[i], "\x00") {
			return false
		}
	}
	return true
}

func TestRunUpdateStrictSafetyHoldsMiseCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0",
			"/repos/openai/codex/releases/tags/codex-0.61.0", "/repos/openai/codex/git/ref/tags/codex-0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2\x00--greedy": {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"):                                        {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`},
		strings.Join([]string{"brew", "update"}, "\x00"):                                                                  {Stdout: "Already up-to-date."},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict"}, fake)
	if code != 2 {
		t.Fatalf("expected held update exit code 2, got %d", code)
	}
	for _, call := range fake.calls {
		if sameCommandCalls([][]string{call}, [][]string{{"mise", "upgrade"}}) {
			t.Fatalf("strict safety hold executed mise upgrade: %#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	if entry.Report.Status != plan.StatusHeld || len(entry.Report.Safety) != 2 || entry.Report.Safety[1].Status != plan.StatusHeld {
		t.Fatalf("expected held mise safety report, got %#v", entry.Report)
	}
	if len(entry.Report.Safety[1].Findings) != 1 || entry.Report.Safety[1].Findings[0].Provider != "mise" || entry.Report.Safety[1].Findings[0].Decision != "hold" {
		t.Fatalf("expected mise hold finding, got %#v", entry.Report.Safety[1].Findings)
	}
}

func TestRunUpdateStrictSafetyAppliesMiseSafeCandidateAndHoldsNewerNativeCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	oldRelease := time.Now().AddDate(0, 0, -4).UTC().Format(time.RFC3339)
	newRelease := time.Now().AddDate(0, 0, -1).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex/releases/tags/0.61.0", "/repos/openai/codex/git/ref/tags/0.61.0",
			"/repos/openai/codex/releases/tags/codex-0.61.0", "/repos/openai/codex/git/ref/tags/codex-0.61.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.61.0":
			_, _ = w.Write([]byte(`{"published_at":"` + oldRelease + `"}`))
		case "/repos/openai/codex/releases/tags/0.62.0", "/repos/openai/codex/git/ref/tags/0.62.0",
			"/repos/openai/codex/releases/tags/codex-0.62.0", "/repos/openai/codex/git/ref/tags/codex-0.62.0":
			http.NotFound(w, r)
		case "/repos/openai/codex/releases/tags/v0.62.0":
			_, _ = w.Write([]byte(`{"published_at":"` + newRelease + `"}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	scopedMiseKey := strings.Join([]string{"mise", "upgrade", "--yes", "--minimum-release-age", "3d", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		"env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2\x00--greedy": {Stdout: `{"formulae":[],"casks":[]}`},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"):                                        {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.61.0"}}`},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"):  {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","latest":"0.62.0"}}`},
		scopedMiseKey: {Stdout: "github:openai/codex 0.60.0 -> 0.61.0"},
		strings.Join([]string{"mise", "prune"}, "\x00"): {Stdout: "mise pruned configuration links"},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict", noTUI: true}, fake)
	if code != 2 {
		t.Fatalf("expected held report with safe scoped mise update, got %d", code)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(scopedMiseKey, "\x00")) {
		t.Fatalf("expected scoped mise upgrade for safe candidate, calls=%#v", fake.calls)
	}
	for _, call := range fake.calls {
		if sameCommandCalls([][]string{call}, [][]string{{"mise", "upgrade"}}) {
			t.Fatalf("mixed safety must not execute unscoped mise upgrade, calls=%#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	var miseStep updateStep
	for _, step := range entry.Report.Steps {
		if step.Name == "mise" {
			miseStep = step
			break
		}
	}
	if miseStep.Status != plan.StatusHeld || len(miseStep.Updated) != 1 || len(miseStep.SkippedItems) != 1 {
		t.Fatalf("expected safe mise update plus held newer candidate, got %#v", miseStep)
	}
	if !strings.Contains(miseStep.Updated[0], "0.60.0 -> 0.61.0") || !strings.Contains(miseStep.SkippedItems[0], "0.62.0") {
		t.Fatalf("expected safe and held mise versions in report, got updated=%#v skipped=%#v", miseStep.Updated, miseStep.SkippedItems)
	}
}

func TestRunUpdateStrictSafetyAppliesBrewAllowedCandidatesAndSkipsHeldCandidates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	allowed := safetyFinding{
		Provider:          "brew",
		Kind:              "brew",
		Name:              "jq",
		InstalledVersions: []string{"1.7"},
		CurrentVersion:    "1.8.1",
		Decision:          "allow",
		Reason:            "candidate release age passed",
	}
	held := safetyFinding{
		Provider:          "brew",
		Kind:              "brew",
		Name:              "fast-release",
		InstalledVersions: []string{"1.0.0"},
		CurrentVersion:    "3.0.0",
		Decision:          "hold",
		Reason:            "candidate release is too new: age 1 days, minimum 3 days",
	}
	scoped := updateSteps()[0]
	scoped, holdReason := updateStepWithStrictSafety(scoped, updateOptions{root: root, security: "strict"}, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{allowed, held},
	}})
	if holdReason != "" {
		t.Fatalf("expected scoped brew command, got hold reason %q", holdReason)
	}
	wantCommand := []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "jq"}
	if strings.Join(scoped.Command, "\x00") != strings.Join(wantCommand, "\x00") {
		t.Fatalf("expected scoped brew command %#v, got %#v", wantCommand, scoped.Command)
	}
	wantCommands := [][]string{
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "jq"},
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "cleanup"},
		{"brew", "update"},
	}
	if !updateStepHasCommands(scoped, wantCommands) {
		t.Fatalf("expected scoped brew command plan %#v, got %#v", wantCommands, scoped.Commands)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: "jq 1.7 -> 1.8.1"}}
	result := runUpdateStepWithHold(context.Background(), fake, scoped, false, holdReason)
	if result.Status != plan.StatusHeld || len(result.Updated) != 1 || len(result.SkippedItems) != 1 {
		t.Fatalf("expected partial brew update with held skipped item, got %#v", result)
	}
	if !strings.Contains(result.SkippedItems[0], "fast-release -> 3.0.0 hold") {
		t.Fatalf("expected held brew candidate summary, got %#v", result.SkippedItems)
	}
}

func TestRunUpdateStrictSafetyScopesAllAllowedProviderCandidates(t *testing.T) {
	root := t.TempDir()
	brew := updateSteps()[0]
	brew, holdReason := updateStepWithStrictSafety(brew, updateOptions{root: root, security: "strict"}, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:       "brew",
			Kind:           "brew",
			Name:           "jq",
			CurrentVersion: "1.8.1",
			Decision:       "allow",
		}},
	}})
	if holdReason != "" {
		t.Fatalf("expected scoped all-allow brew command, got hold reason %q", holdReason)
	}
	if got := strings.Join(brew.Command, " "); strings.Contains(got, "brew update && brew upgrade") || !strings.Contains(got, "brew upgrade --greedy jq") {
		t.Fatalf("expected scoped all-allow brew command without brew update, got %#v", brew.Command)
	}
	if !updateStepHasCommands(brew, [][]string{
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "jq"},
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "cleanup"},
		{"brew", "update"},
	}) {
		t.Fatalf("expected scoped all-allow brew command plan, got %#v", brew.Commands)
	}
	mise := updateSteps()[1]
	mise, holdReason = updateStepWithStrictSafety(mise, updateOptions{root: root, security: "strict"}, []safetyGate{{
		Provider: "mise",
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:       "mise",
			Kind:           "tool",
			Name:           "github:openai/codex",
			CurrentVersion: "0.61.0",
			Decision:       "allow",
		}},
	}})
	if holdReason != "" {
		t.Fatalf("expected scoped all-allow mise command, got hold reason %q", holdReason)
	}
	if got := strings.Join(mise.Command, " "); strings.Contains(got, "mise upgrade &&") || !strings.Contains(got, "mise upgrade --yes --minimum-release-age 3d --cd "+root+" github:openai/codex") {
		t.Fatalf("expected scoped all-allow mise command, got %#v", mise.Command)
	}
	if !updateStepHasCommands(mise, [][]string{
		{"mise", "upgrade", "--yes", "--minimum-release-age", "3d", "--cd", root, "github:openai/codex"},
		{"mise", "prune"},
	}) {
		t.Fatalf("expected scoped all-allow mise command plan, got %#v", mise.Commands)
	}
}

func TestScopedSecurityRerunStepOnlyTargetsSelectedFinding(t *testing.T) {
	report := updateReport{
		Root: "/repo",
		Safety: []safetyGate{{
			Provider: "brew",
			Status:   plan.StatusOK,
			Findings: []safetyFinding{{
				Provider:       "brew",
				Kind:           "cask",
				Name:           "wezterm@nightly",
				CurrentVersion: "latest",
				Decision:       "allow",
			}, {
				Provider:       "brew",
				Kind:           "cask",
				Name:           "cursor",
				CurrentVersion: "3.7.19",
				Decision:       "allow",
			}},
		}},
	}
	step, ok := scopedSecurityRerunStep(report, "brew", "cask", "wezterm@nightly")
	if !ok {
		t.Fatal("expected scoped security rerun step")
	}
	command := strings.Join(step.Command, " ")
	if !strings.Contains(command, "brew upgrade --greedy wezterm@nightly") || strings.Contains(command, "cursor") {
		t.Fatalf("expected selected cask only in scoped brew rerun command, got %#v", step.Command)
	}
	if strings.Contains(command, "brew update && brew upgrade") {
		t.Fatalf("expected scoped rerun not to use unscoped provider update command, got %#v", step.Command)
	}
}

func TestScopedMiseUpgradeCommandUsesConfiguredMinimumReleaseAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "5")
	root := t.TempDir()

	command := scopedMiseUpgradeCommand(root, []safetyFinding{{
		Provider: "mise",
		Name:     "github:openai/codex",
		Decision: "allow",
	}})

	got := strings.Join(command, " ")
	want := "mise upgrade --yes --minimum-release-age 5d --cd " + root + " github:openai/codex"
	if !strings.Contains(got, want) {
		t.Fatalf("expected configured minimum release age in scoped mise command\nwant: %s\ngot:  %s", want, got)
	}
}

func TestScopedMiseUpgradeCommandOmitsMinimumReleaseAgeWhenDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "0")

	command := scopedMiseUpgradeCommand("", []safetyFinding{{
		Provider: "mise",
		Name:     "github:openai/codex",
		Decision: "allow",
	}})

	got := strings.Join(command, " ")
	if strings.Contains(got, "--minimum-release-age") {
		t.Fatalf("expected disabled minimum release age to omit flag, got %s", got)
	}
}

func TestRunUpdateStrictSafetyRefreshesBrewMetadataOnlyWhenNoCandidates(t *testing.T) {
	brew := updateSteps()[0]
	brew, holdReason := updateStepWithStrictSafety(brew, updateOptions{security: "strict"}, []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusOK,
	}})
	if holdReason != "" {
		t.Fatalf("expected metadata-only brew command, got hold reason %q", holdReason)
	}
	want := []string{"brew", "update"}
	if strings.Join(brew.Command, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("expected metadata-only brew update, got %#v", brew.Command)
	}
	if !strings.Contains(brew.Reason, "metadata only") {
		t.Fatalf("expected metadata-only reason, got %q", brew.Reason)
	}
}

func TestRunUpdateStrictSafetyRefreshesBrewMetadataAndAppliesDiscoveredSafeCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}}
}`))
		case "/repos/jqlang/jq/releases/tags/jq-1.8.1":
			_, _ = w.Write([]byte(`{"published_at":"` + time.Now().AddDate(0, 0, -4).UTC().Format(time.RFC3339) + `"}`))
		case "/":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("UPDEV_HOMEBREW_API_URL", server.URL)
	t.Setenv("UPDEV_GITHUB_API_URL", server.URL)
	t.Setenv("UPDEV_OSV_API_URL", server.URL)
	outdatedKey := "env\x00HOMEBREW_NO_AUTO_UPDATE=1\x00HOMEBREW_NO_INSTALL_FROM_API=1\x00brew\x00outdated\x00--json=v2\x00--greedy"
	scopedBrewKey := strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "jq"}, "\x00")
	fake := &fakeCommandRunner{
		results: map[string]runner.Result{
			strings.Join([]string{"brew", "update"}, "\x00"): {Stdout: "Updated Homebrew metadata"},
			scopedBrewKey: {Stdout: "jq 1.7 -> 1.8.1"},
		},
		sequences: map[string][]runner.Result{
			outdatedKey: {
				{Stdout: `{"formulae":[],"casks":[]}`},
				{Stdout: `{"formulae":[{"name":"jq","installed_versions":["1.7"],"current_version":"1.8.1"}],"casks":[]}`},
			},
		},
	}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict", noTUI: true}, fake)
	if code != 0 {
		t.Fatalf("expected same-run safe Homebrew update after metadata refresh, got %d", code)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(scopedBrewKey, "\x00")) {
		t.Fatalf("expected scoped brew upgrade after metadata refresh, calls=%#v", fake.calls)
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	var brewStep updateStep
	for _, step := range entry.Report.Steps {
		if step.Name == "brew" {
			brewStep = step
			break
		}
	}
	if brewStep.Status != plan.StatusOK || !containsString(brewStep.Updated, "jq 1.7 -> 1.8.1") {
		t.Fatalf("expected discovered safe Homebrew candidate to update in same run, got %#v", brewStep)
	}
}

func TestUpdateHubRouterBackReturnsFromDetailToDashboard(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionLogs, updateHubActionLogs, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected router to start in logs detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back to return to dashboard, screen=%q\n%s", model.screen, model.View().Content)
	}
	if !strings.Contains(model.View().Content, "updev update ok") {
		t.Fatalf("expected dashboard view after Back:\n%s", model.View().Content)
	}
}

func TestUpdateHubRouterClearsDashboardActionAfterReturning(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected router to start on dashboard, screen=%q", model.screen)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "logs" {
		t.Fatalf("expected dashboard action to open logs detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Back from logs to return to dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard {
		t.Fatalf("expected Down after Back to stay on dashboard, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterBackPreservesDashboardFocus(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{Name: "brew", Status: plan.StatusOK}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = updated.(updateHubRouterModel)
	selected := model.dashboard.State.Selected
	lineIndex := model.dashboard.SelectedLineIndex()
	if lineIndex < 0 || model.dashboard.Lines[lineIndex].Action == "" || selected == 0 {
		t.Fatalf("expected dashboard focus to move before opening route, selected=%d line=%d", selected, lineIndex)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen == updateHubRouterDashboard {
		t.Fatalf("expected Enter to open a routed view")
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDashboard || model.dashboard.State.Selected != selected {
		t.Fatalf("expected Back to preserve dashboard focus selected=%d, got screen=%q selected=%d", selected, model.screen, model.dashboard.State.Selected)
	}
}

func TestUpdateHubRouterBackKeepsDashboardTopAnchored(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Root:     "/repo",
		Security: "strict",
		Steps: []updateStep{{
			Name:   "mise-bump",
			Status: plan.StatusHeld,
			SkippedItems: []string{
				"aqua:modem-dev/hunk 0.14.0 -> 0.14.1",
				"cloudflared 2026.5.0 -> 2026.5.2",
				"copilot-cli 1.0.48 -> 1.0.61",
				"fzf 0.72.0 -> 0.73.1",
				"go 1.26.3 -> 1.26.4",
				"lazygit 0.61.1 -> 0.62.2",
				"node 24.16.0 -> 26.3.0",
				"rust 1.95.0 -> 1.96.0",
				"uv 0.11.14 -> 0.11.19",
			},
		}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.height = 10
	model.applyDashboardSize(&model.dashboard)
	for range 8 {
		updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
		model = updated.(updateHubRouterModel)
	}
	selected := model.dashboard.State.Selected
	if model.dashboard.State.Offset == 0 {
		t.Fatalf("expected low dashboard focus to scroll before opening a route")
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen == updateHubRouterDashboard {
		t.Fatalf("expected Enter to open a routed view")
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	view := model.View().Content
	if model.screen != updateHubRouterDashboard || model.dashboard.State.Selected != selected {
		t.Fatalf("expected Back to preserve dashboard focus selected=%d, got screen=%q selected=%d", selected, model.screen, model.dashboard.State.Selected)
	}
	if model.dashboard.State.Offset != 0 || !strings.Contains(view, "root: /repo") {
		t.Fatalf("expected Back to keep dashboard top anchored, offset=%d\n%s", model.dashboard.State.Offset, view)
	}
}

func TestUpdateHubRouterOpensFullReportWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionFull, updateHubActionFull, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "full" {
		t.Fatalf("expected router to open full report detail, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev full report") || !strings.Contains(view, "cached update report") {
		t.Fatalf("expected full report detail view:\n%s", view)
	}
}

func TestUpdateHubRouterOpensBackendTableWithoutSubprogram(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "bat",
		RecommendedProvider: "mise",
		RecommendedName:     "bat",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	if model.screen != updateHubRouterTable || model.stateKey != "backends" {
		t.Fatalf("expected router to open backend table, screen=%q stateKey=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "updev backend convergence") || !strings.Contains(view, "bat") {
		t.Fatalf("expected backend table view:\n%s", view)
	}
}

func TestUpdateHubRouterRefreshesReviewPlansAsynchronously(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	initialView := model.View().Content
	if !strings.Contains(initialView, "loading - preparing manual") || !strings.Contains(initialView, "loading - preparing backend") {
		t.Fatalf("expected dashboard to show review plan loading rows:\n%s", initialView)
	}
	for _, want := range []string{"review actions", "action", "description"} {
		if !strings.Contains(initialView, want) {
			t.Fatalf("expected loading review action table to contain %q:\n%s", want, initialView)
		}
	}
	if model.dashboard.SelectedLineIndex() < 0 || model.dashboard.Lines[model.dashboard.SelectedLineIndex()].Action != updateHubActionLogs || model.dashboard.State.Offset != 0 {
		t.Fatalf("expected initial loading dashboard to stay top-anchored on update details, selected=%d offset=%d lines=%#v", model.dashboard.SelectedLineIndex(), model.dashboard.State.Offset, model.dashboard.Lines)
	}
	manualPlan := inventoryPlanReport{
		Status:         plan.StatusDrift,
		AttentionCount: 1,
		Items:          []manualPlanItem{{Name: "Vendor App", Action: "needs-review"}},
		ActionCounts:   map[string]int{"needs-review": 1},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	updated, _ := model.Update(updateHubManualPlanMsg{Report: manualPlan})
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(updateHubBackendPlanMsg{Report: backendPlan})
	model = updated.(updateHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "loading - preparing") || !strings.Contains(readyView, "needs-review=1") || !strings.Contains(readyView, "homebrew-to-mise=1") {
		t.Fatalf("expected dashboard to refresh review plan rows:\n%s", readyView)
	}
}

func TestUpdateHubRouterKeepsReviewRoutesLoadingUntilAsyncPlanArrives(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, updateHubActionManualPlan, updateHubActionManualPlan, false)
	if model.screen != updateHubRouterTable || model.stateKey != "manual-plan" || !model.manualLoading {
		t.Fatalf("expected manual review route to remain loading, screen=%q state=%q manualLoading=%v", model.screen, model.stateKey, model.manualLoading)
	}
	if view := model.View().Content; !strings.Contains(view, "manual review loading") {
		t.Fatalf("expected manual review loading title:\n%s", view)
	}

	model = newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, updateHubActionBackends, updateHubActionBackends, false)
	if model.screen != updateHubRouterTable || model.stateKey != "backends" || !model.backendLoading {
		t.Fatalf("expected backend route to remain loading, screen=%q state=%q backendLoading=%v", model.screen, model.stateKey, model.backendLoading)
	}
	if view := model.View().Content; !strings.Contains(view, "backend evidence loading") {
		t.Fatalf("expected backend loading title:\n%s", view)
	}
}

func TestUpdateHubRouterShowsJapaneseReviewActionColumnsWhileLoading(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	report := updateReport{Status: plan.StatusOK, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	view := model.View().Content
	for _, want := range []string{"確認アクション", "操作", "説明", "loading - 手動/vendor app 候補を準備中", "loading - backend evidence を準備中"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected Japanese loading review action table to contain %q:\n%s", want, view)
		}
	}
}

func TestUpdateHubRouterAsyncPlanBuildersUseContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var manualContextCanceled bool
	var backendContextCanceled bool
	builders := updateHubPlanBuilders{
		Manual: func(ctx context.Context, root string) inventoryPlanReport {
			manualContextCanceled = ctx.Err() != nil
			return canceledUpdateHubManualPlan(root)
		},
		Backend: func(ctx context.Context, root string) backendPlanReport {
			backendContextCanceled = ctx.Err() != nil
			return canceledUpdateHubBackendPlan(root)
		},
	}
	model := newUpdateHubRouterModelWithContext(ctx, builders, updateReport{Status: plan.StatusOK, Root: "/repo"}, inventoryPlanReport{}, true, backendPlanReport{}, true, "", updateHubActionDashboard, false)
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected async plan commands")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected two async plan commands, got %#v", msg)
	}
	var manualMsg updateHubManualPlanMsg
	var backendMsg updateHubBackendPlanMsg
	for _, batchCmd := range batch {
		switch got := batchCmd().(type) {
		case updateHubManualPlanMsg:
			manualMsg = got
		case updateHubBackendPlanMsg:
			backendMsg = got
		default:
			t.Fatalf("unexpected async plan message: %#v", got)
		}
	}
	if !manualContextCanceled || !backendContextCanceled {
		t.Fatalf("expected canceled context in async builders, manual=%v backend=%v", manualContextCanceled, backendContextCanceled)
	}
	if manualMsg.Report.Status != plan.StatusHeld || backendMsg.Report.Status != plan.StatusHeld {
		t.Fatalf("expected canceled partial reports, manual=%#v backend=%#v", manualMsg.Report, backendMsg.Report)
	}
}

func TestUpdateHubCanceledPlanReportsExplainPartialResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manualPlan := buildUpdateHubManualPlanWithContext(ctx, "/repo")
	backendPlan := buildUpdateHubBackendPlanWithContext(ctx, "/repo")
	if manualPlan.Status != plan.StatusHeld || !strings.Contains(strings.Join(manualPlan.NextSteps, " "), "canceled") {
		t.Fatalf("expected held manual cancel report, got %#v", manualPlan)
	}
	if backendPlan.Status != plan.StatusHeld || !strings.Contains(strings.Join(backendPlan.Warnings, " "), "canceled") {
		t.Fatalf("expected held backend cancel report, got %#v", backendPlan)
	}
}

func TestUpdateHubRouterUpdateFilterStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" || !model.detail.PrimaryEnterAction {
		t.Fatalf("expected update filter menu inside router, screen=%q state=%q primary=%v", model.screen, model.stateKey, model.detail.PrimaryEnterAction)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:provider:brew" {
		t.Fatalf("expected Enter to open filtered update evidence, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") {
		t.Fatalf("expected filtered update detail to include provider evidence:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from filtered evidence to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusOK, Root: "/repo", Steps: []updateStep{{
		Name:   "brew",
		Status: plan.StatusOK,
		Stdout: "brew output",
	}, {
		Name:   "mise",
		Status: plan.StatusOK,
		Stdout: "mise output",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionUpdatesFilter, updateHubActionUpdatesFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("updates"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:updates" {
		t.Fatalf("expected update query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "brew", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:updates:query:brew" {
		t.Fatalf("expected query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "brew output") || strings.Contains(view, "mise output") {
		t.Fatalf("expected query-filtered update detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:updates" {
		t.Fatalf("expected Back from query result to return to update filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterSecurityQueryInputStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusHeld, Root: "/repo", Safety: []safetyGate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{{
			Provider: "brew",
			Kind:     "cask",
			Name:     "danger-app",
			Decision: "hold",
			Reason:   "unique-risk",
		}, {
			Provider: "brew",
			Kind:     "cask",
			Name:     "other-app",
			Decision: "hold",
			Reason:   "other reason",
		}},
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionSecurityFilter, updateHubActionSecurityFilter, false)
	updated, _ := model.handleAction(updateHubQueryActionValue("security"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterInput || model.stateKey != "query-input:security" {
		t.Fatalf("expected security query input inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "unique-risk", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-result:security:query:unique-risk" {
		t.Fatalf("expected security query result inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "danger-app") || strings.Contains(view, "other-app") {
		t.Fatalf("expected query-filtered security detail:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || model.stateKey != "filter-menu:security" {
		t.Fatalf("expected Back from security query result to return to security filter menu, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterWriteConfirmationStaysInsideRouter(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:            "mise-backend-rewrite",
		Name:            "cargo:broot",
		RecommendedName: "github:Canop/broot",
		RewriteAllowed:  true,
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionBackends, updateHubActionBackends, false)
	action := backendDetailActionValue("rewrite-mise", "cargo:broot", "github:Canop/broot")
	updated, _ := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterConfirm || !strings.HasPrefix(model.stateKey, "write-confirm:") {
		t.Fatalf("expected backend write confirmation inside router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "Rewrite mise backend") || !strings.Contains(view, "cargo:broot") {
		t.Fatalf("expected backend confirmation view:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != updateHubActionBackends {
		t.Fatalf("expected Back from confirmation to return to backend table, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterManualEditRemainsExternal(t *testing.T) {
	report := updateReport{Status: plan.StatusDrift, Root: "/repo"}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionManualPlan, updateHubActionManualPlan, false)
	action := manualPlanDetailActionValue("edit", "Vendor App")
	updated, cmd := model.handleAction(action)
	model = updated.(updateHubRouterModel)
	if model.finalAction != action || cmd == nil {
		t.Fatalf("expected manual edit to remain an external action, final=%q cmdNil=%v", model.finalAction, cmd == nil)
	}
}

func TestUpdateHubRouterInventoryTabSwitchesToManualInventory(t *testing.T) {
	report := updateReport{
		Status: plan.StatusDrift,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "mise",
			Kind:     "tool",
			Name:     "ripgrep",
			Version:  "14.1.1",
			Status:   plan.StatusOK,
		}, {
			Provider: manualProviderName,
			Kind:     "app",
			Name:     "Vendor App",
			Status:   plan.StatusDrift,
		}}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionInventoryAll, updateHubActionInventoryAll, false)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected installed inventory table, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != listHubActionManual {
		t.Fatalf("expected Tab to switch to manual inventory inside update router, screen=%q state=%q", model.screen, model.stateKey)
	}
	if view := model.View().Content; !strings.Contains(view, "updev list manual") || !strings.Contains(view, "Vendor App") {
		t.Fatalf("expected manual inventory view after Tab:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "shift+tab", Code: tea.KeyExtended}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || model.stateKey != "inventory-all" {
		t.Fatalf("expected Shift+Tab to switch back to installed inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
}

func TestUpdateHubRouterRefreshesSummaryInventoryBackendEvidenceAsynchronously(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}}},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, true, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: "brew"})
	if model.screen != updateHubRouterTable || !strings.HasPrefix(model.stateKey, "summary:") {
		t.Fatalf("expected summary inventory route, screen=%q state=%q", model.screen, model.stateKey)
	}
	initialView := model.View().Content
	if !strings.Contains(initialView, "backend evidence loading") || strings.Contains(initialView, "▶bak") || strings.Contains(initialView, "node") {
		t.Fatalf("expected summary inventory to show filtered loading view before backend evidence:\n%s", initialView)
	}
	updated, _ := model.Update(updateHubBackendPlanMsg{Report: backendPlan})
	model = updated.(updateHubRouterModel)
	readyView := model.View().Content
	if strings.Contains(readyView, "backend evidence loading") {
		t.Fatalf("expected summary inventory backend loading state to clear:\n%s", readyView)
	}
	if !strings.Contains(readyView, "▶bak") || !strings.Contains(readyView, "open backend review") || strings.Contains(readyView, "node") {
		t.Fatalf("expected summary inventory refresh to keep filter and add backend badge:\n%s", readyView)
	}
}

func TestUpdateHubRouterRouteBackReturnsToSummaryInventory(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	report := updateReport{
		Status: plan.StatusOK,
		Root:   "/repo",
		Inventory: plan.Report{Items: []plan.Item{{
			Provider: "brew",
			Kind:     "brew",
			Name:     "ripgrep",
			Status:   plan.StatusOK,
		}, {
			Provider: "mise",
			Kind:     "tool",
			Name:     "node",
			Status:   plan.StatusOK,
		}}},
	}
	backendPlan := backendPlanReport{Status: plan.StatusDrift, Findings: []backendFinding{{
		Type:                "homebrew-to-mise",
		Provider:            "brew",
		Kind:                "brew",
		Name:                "ripgrep",
		RecommendedProvider: "mise",
		RecommendedName:     "ripgrep",
		Action:              "review",
	}}}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlan, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionInventoryAll, Provider: "brew"})
	updated, _ := model.handleAction(listRouteActionValueForTarget(listHubActionBackends, "brew", "brew", "ripgrep"))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterDetail || !strings.HasPrefix(model.stateKey, "route:") {
		t.Fatalf("expected backend route detail, screen=%q state=%q", model.screen, model.stateKey)
	}
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'}))
	model = updated.(updateHubRouterModel)
	if model.screen != updateHubRouterTable || !strings.HasPrefix(model.stateKey, "summary:") {
		t.Fatalf("expected Back from backend route to return to summary inventory, screen=%q state=%q", model.screen, model.stateKey)
	}
	view := model.View().Content
	if !strings.Contains(view, "ripgrep") || !strings.Contains(view, "▶bak") || strings.Contains(view, "node") {
		t.Fatalf("expected summary inventory return to keep provider filter and backend badge:\n%s", view)
	}
}

func TestUpdateHubRouterSecurityRouteShowsAllowedFindingForQuery(t *testing.T) {
	report := updateReport{
		Status:   plan.StatusHeld,
		Security: "strict",
		Root:     "/repo",
		Safety: []safetyGate{{
			Provider: "mise-bump",
			Status:   plan.StatusHeld,
			Summary:  &safetySummary{Allow: 1, Hold: 12},
			Findings: []safetyFinding{{
				Provider: "mise",
				Kind:     "tool",
				Name:     "github:ogulcancelik/herdr",
				Version:  "0.6.8 -> 0.6.9",
				Decision: "allow",
				Reason:   "accepted from updev detail browser after local review",
			}, {
				Provider: "mise",
				Kind:     "tool",
				Name:     "cloudflared",
				Version:  "2026.5.2 -> 2026.6.0",
				Decision: "hold",
				Reason:   "candidate is too new",
			}},
		}},
	}
	model := newUpdateHubRouterModel(report, inventoryPlanReport{}, false, backendPlanReport{}, false, updateHubActionDashboard, updateHubActionDashboard, false)
	model.showUpdateSummaryRoute(updateSummaryRoute{Base: updateHubActionSecurity, Provider: "mise-bump", Query: "tool/github:ogulcancelik/herdr"})
	view := model.View().Content
	if !strings.Contains(view, "github:ogulcancelik/herdr") || !strings.Contains(view, "allow") {
		t.Fatalf("expected queried allowed security finding in routed detail view:\n%s", view)
	}
	if strings.Contains(view, "mise-bump security 12 hold") || strings.Contains(view, "cloudflared") {
		t.Fatalf("expected route to avoid fallback gate summary and unrelated findings:\n%s", view)
	}
}
