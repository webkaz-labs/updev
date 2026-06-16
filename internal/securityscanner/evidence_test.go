package securityscanner

import (
	"errors"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func TestFindingSourceIDAndDetail(t *testing.T) {
	finding := Finding{
		SourcePath:  "package-lock.json",
		File:        "fallback.txt",
		VulnID:      "CVE-1",
		RuleID:      "RULE-1",
		Package:     "left-pad",
		Version:     "1.0.0",
		Description: "fallback description",
	}
	if got := FindingSource(finding); got != "package-lock.json" {
		t.Fatalf("unexpected source: %q", got)
	}
	if got := FindingID(finding); got != "CVE-1" {
		t.Fatalf("unexpected id: %q", got)
	}
	if got := FindingDetail(finding); got != "left-pad@1.0.0" {
		t.Fatalf("unexpected detail: %q", got)
	}
}

func TestFindingDetailUsesLineOrDescription(t *testing.T) {
	if got := FindingDetail(Finding{StartLine: 2, EndLine: 5}); got != "lines 2-5" {
		t.Fatalf("unexpected line range detail: %q", got)
	}
	if got := FindingDetail(Finding{StartLine: 3}); got != "line 3" {
		t.Fatalf("unexpected line detail: %q", got)
	}
	if got := FindingDetail(Finding{Description: "possible secret"}); got != "possible secret" {
		t.Fatalf("unexpected description detail: %q", got)
	}
}

func TestSortFindings(t *testing.T) {
	findings := []Finding{
		{Kind: "workflow", RuleID: "workflow", Decision: "review"},
		{Kind: "vulnerability", VulnID: "direct", Decision: "hold", DependencyKind: "direct", Severity: "high"},
		{Kind: "secret", RuleID: "secret", Decision: "hold"},
		{Kind: "vulnerability", VulnID: "fixed", Decision: "hold", Severity: "critical", FixedVersions: []string{"1.2.3"}},
		{Kind: "vulnerability", VulnID: "block", Decision: "block"},
	}
	SortFindings(findings)
	got := []string{}
	for _, finding := range findings {
		got = append(got, FindingID(finding))
	}
	want := []string{"block", "secret", "fixed", "direct", "workflow"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected sort order: got %#v want %#v", got, want)
		}
	}
}

func TestApplyPolicyDecision(t *testing.T) {
	held := ApplyPolicyDecision(Evidence{
		Status:   "ok",
		Decision: "allow",
		Findings: []Finding{{
			Decision: "review",
		}},
	})
	if held.Status != "held" || held.Decision != "hold" {
		t.Fatalf("expected review finding to hold evidence, got %#v", held)
	}
	blocked := ApplyPolicyDecision(Evidence{
		Status:   "ok",
		Decision: "allow",
		Findings: []Finding{{
			Decision: "block",
		}},
	})
	if blocked.Status != "blocked" || blocked.Decision != "block" {
		t.Fatalf("expected block finding to block evidence, got %#v", blocked)
	}
	allowed := ApplyPolicyDecision(Evidence{
		Status:   "held",
		Decision: "hold",
		Findings: []Finding{{
			Decision: "allow",
		}},
	})
	if allowed.Status != "ok" || allowed.Decision != "allow" {
		t.Fatalf("expected allow-only findings to allow evidence, got %#v", allowed)
	}
}

func TestReportStatusAndSummary(t *testing.T) {
	scanners := []Evidence{
		{Status: plan.StatusOK},
		{Status: plan.StatusHeld},
		{Status: plan.StatusUnavailable},
		{Status: plan.StatusError},
	}

	if got := ReportStatus(plan.StatusOK, scanners); got != plan.StatusHeld {
		t.Fatalf("ReportStatus() = %q, want %q", got, plan.StatusHeld)
	}
	if got := ReportStatus(plan.StatusOK, []Evidence{{Status: plan.StatusBlocked}}); got != plan.StatusBlocked {
		t.Fatalf("ReportStatus() = %q, want %q", got, plan.StatusBlocked)
	}

	held, unavailable, errors := Summary(scanners)
	if held != 1 || unavailable != 1 || errors != 1 {
		t.Fatalf("Summary() = held %d unavailable %d errors %d, want 1/1/1", held, unavailable, errors)
	}
}

func TestHasAttention(t *testing.T) {
	tests := []struct {
		name     string
		scanners []Evidence
		want     bool
	}{
		{
			name:     "ok allow",
			scanners: []Evidence{{Status: plan.StatusOK, Decision: "allow"}},
		},
		{
			name:     "non ok status",
			scanners: []Evidence{{Status: plan.StatusHeld, Decision: "allow"}},
			want:     true,
		},
		{
			name:     "attention decision",
			scanners: []Evidence{{Status: plan.StatusOK, Decision: "review"}},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAttention(tt.scanners); got != tt.want {
				t.Fatalf("HasAttention() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasFindingAttention(t *testing.T) {
	if HasFindingAttention([]Evidence{{Findings: []Finding{{Decision: "allow"}}}}) {
		t.Fatal("allow-only findings should not need attention")
	}
	if !HasFindingAttention([]Evidence{{Findings: []Finding{{Decision: "hold"}}}}) {
		t.Fatal("hold finding should need attention")
	}
}

func TestFailureStatusAndKind(t *testing.T) {
	tests := []struct {
		name       string
		result     runner.Result
		wantStatus plan.Status
		wantKind   string
	}{
		{
			name:       "missing binary",
			result:     runner.Result{Code: 127, Stderr: "scanner: command not found"},
			wantStatus: plan.StatusUnavailable,
			wantKind:   FailureMissingBinary,
		},
		{
			name:       "timeout",
			result:     runner.Result{Err: errors.New("context deadline exceeded")},
			wantStatus: plan.StatusUnavailable,
			wantKind:   FailureTimeout,
		},
		{
			name:       "rate limit",
			result:     runner.Result{Code: 1, Stderr: "GitHub API rate limit exceeded"},
			wantStatus: plan.StatusUnavailable,
			wantKind:   FailureRateLimit,
		},
		{
			name:       "unsupported target",
			result:     runner.Result{Code: 1, Stderr: "no supported files found"},
			wantStatus: plan.StatusUnavailable,
			wantKind:   FailureUnsupportedTarget,
		},
		{
			name:       "command error",
			result:     runner.Result{Code: 2, Stderr: "scanner failed"},
			wantStatus: plan.StatusError,
			wantKind:   FailureCommandError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotKind := FailureStatusAndKind(tt.result)
			if gotStatus != tt.wantStatus || gotKind != tt.wantKind {
				t.Fatalf("FailureStatusAndKind() = %s/%s, want %s/%s", gotStatus, gotKind, tt.wantStatus, tt.wantKind)
			}
		})
	}
}
