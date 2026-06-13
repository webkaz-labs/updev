package nativeaudit

import (
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestReportStatusPrioritizesBlockingStatuses(t *testing.T) {
	audits := []Evidence{
		{Status: plan.StatusHeld},
		{Status: plan.StatusError},
		{Status: plan.StatusBlocked},
	}

	if got := ReportStatus(plan.StatusOK, audits); got != plan.StatusError {
		t.Fatalf("ReportStatus() = %q, want %q", got, plan.StatusError)
	}

	if got := ReportStatus(plan.StatusOK, []Evidence{{Status: plan.StatusHeld}}); got != plan.StatusHeld {
		t.Fatalf("ReportStatus() = %q, want %q", got, plan.StatusHeld)
	}

	if got := ReportStatus(plan.StatusBlocked, []Evidence{{Status: plan.StatusHeld}}); got != plan.StatusBlocked {
		t.Fatalf("ReportStatus() = %q, want existing blocked status", got)
	}
}

func TestSummaryCountsAttentionStatuses(t *testing.T) {
	held, unavailable, errors := Summary([]Evidence{
		{Status: plan.StatusOK},
		{Status: plan.StatusHeld},
		{Status: plan.StatusUnavailable},
		{Status: plan.StatusError},
		{Status: plan.StatusHeld},
	})

	if held != 2 || unavailable != 1 || errors != 1 {
		t.Fatalf("Summary() = held %d unavailable %d errors %d, want 2/1/1", held, unavailable, errors)
	}
}

func TestHasAttentionChecksStatusAndDecision(t *testing.T) {
	tests := []struct {
		name   string
		audits []Evidence
		want   bool
	}{
		{
			name:   "ok allow",
			audits: []Evidence{{Status: plan.StatusOK, Decision: "allow"}},
		},
		{
			name:   "non ok status",
			audits: []Evidence{{Status: plan.StatusUnavailable, Decision: "allow"}},
			want:   true,
		},
		{
			name:   "attention decision",
			audits: []Evidence{{Status: plan.StatusOK, Decision: "review"}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAttention(tt.audits); got != tt.want {
				t.Fatalf("HasAttention() = %v, want %v", got, tt.want)
			}
		})
	}
}
