package updatereport

import (
	"reflect"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
)

type testPolicy struct {
	Path string `json:"path"`
}

func TestReportAggregatesStatusWithoutLosingHigherPriority(t *testing.T) {
	report := New("/repo", false, "strict", &testPolicy{Path: "/policy"})
	report.AppendStep(Step{Name: "brew", Status: plan.StatusHeld})
	report.AppendStep(Step{Name: "mise", Status: plan.StatusDrift})
	report.SetInventory(plan.Report{Status: plan.StatusError})
	report.RecomputeStatus(false)

	if report.Status != plan.StatusError {
		t.Fatalf("Status = %q, want error", report.Status)
	}
	if got := len(report.Steps); got != 2 {
		t.Fatalf("len(Steps) = %d, want 2", got)
	}
}

func TestInventoryDriftDoesNotChangeTopLevelUpdateStatus(t *testing.T) {
	report := New("/repo", false, "strict", (*testPolicy)(nil))
	report.SetInventory(plan.Report{Status: plan.StatusDrift})
	report.RecomputeStatus(false)
	if report.Status != plan.StatusOK {
		t.Fatalf("Status = %q, want ok", report.Status)
	}
}

func TestReplaceOrAppendStepRecomputesStatus(t *testing.T) {
	report := New("/repo", false, "strict", (*testPolicy)(nil))
	report.AppendStep(Step{Name: "brew", Status: plan.StatusHeld})
	report.ReplaceOrAppendStep(Step{Name: "brew", Status: plan.StatusOK})
	report.RecomputeStatus(false)
	if report.Status != plan.StatusOK || len(report.Steps) != 1 {
		t.Fatalf("unexpected replaced report: %#v", report)
	}
	report.ReplaceOrAppendStep(Step{Name: "mise", Status: plan.StatusBlocked})
	report.RecomputeStatus(false)
	if report.Status != plan.StatusBlocked || len(report.Steps) != 2 {
		t.Fatalf("unexpected appended report: %#v", report)
	}
}

func TestReplaceOrAppendGateReplacesWithoutDuplication(t *testing.T) {
	report := New("/repo", false, "strict", (*testPolicy)(nil))
	report.ReplaceOrAppendGate(securitygate.Gate{Provider: "brew", Status: plan.StatusHeld})
	report.ReplaceOrAppendGate(securitygate.Gate{Provider: "brew", Status: plan.StatusOK})
	report.RecomputeStatus(true)
	if len(report.Safety) != 1 || report.Safety[0].Status != plan.StatusOK {
		t.Fatalf("unexpected gates: %#v", report.Safety)
	}
	if report.Status != plan.StatusOK {
		t.Fatalf("Status = %q, want ok", report.Status)
	}
}

func TestRecomputeStatusUsesStrictSafetyAndInventoryError(t *testing.T) {
	report := New("/repo", false, "strict", (*testPolicy)(nil))
	report.AppendStep(Step{Name: "brew", Status: plan.StatusDrift})
	report.ReplaceOrAppendGate(securitygate.Gate{Provider: "brew", Status: plan.StatusHeld})
	report.RecomputeStatus(false)
	if report.Status != plan.StatusDrift {
		t.Fatalf("non-strict Status = %q, want drift", report.Status)
	}
	report.RecomputeStatus(true)
	if report.Status != plan.StatusHeld {
		t.Fatalf("strict Status = %q, want held", report.Status)
	}
	report.Inventory.Status = plan.StatusError
	report.RecomputeStatus(true)
	if report.Status != plan.StatusError {
		t.Fatalf("inventory error Status = %q, want error", report.Status)
	}
}

func TestFilterReportNormalizesAndFiltersAllSections(t *testing.T) {
	report := Report[testPolicy]{
		Status: plan.StatusHeld,
		Steps: []Step{{
			Name:         "brew",
			Status:       plan.StatusHeld,
			Updated:      []string{"Updated Homebrew", "mole 1.0 -> 1.1", "mole 1.0 -> 1.1"},
			SkippedItems: []string{"Warning: generic", "mole skipped: held"},
		}},
		Safety: []securitygate.Gate{{
			Provider: "brew",
			Status:   plan.StatusHeld,
			Findings: []securitygate.Finding{{Provider: "brew", Kind: "brew", Name: "mole", Decision: "hold"}},
		}},
		Inventory: plan.Report{Items: []plan.Item{{Provider: "brew", Name: "mole", Status: plan.StatusHeld}}},
	}
	filtered := FilterReport(report, Filter{Provider: "brew", Query: "mole"}, func(inventory plan.Report, filter Filter) plan.Report {
		inventory.Items = []plan.Item{inventory.Items[0]}
		return inventory
	})

	if got, want := filtered.Steps[0].Updated, []string{"mole 1.0 -> 1.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Updated = %#v, want %#v", got, want)
	}
	if got := len(filtered.Safety[0].Findings); got != 1 {
		t.Fatalf("safety findings = %d, want 1", got)
	}
	if got := len(filtered.Inventory.Items); got != 1 {
		t.Fatalf("inventory items = %d, want 1", got)
	}
}

func TestBuildSectionViewUsesFilteredReport(t *testing.T) {
	report := Report[testPolicy]{
		Status: plan.StatusHeld,
		Steps:  []Step{{Name: "brew", Status: plan.StatusHeld, Skipped: true}},
		Safety: []securitygate.Gate{{Provider: "brew", Status: plan.StatusHeld}},
	}
	entry := CacheEntry[testPolicy]{Version: 1, Type: "update", Report: report}
	view := BuildSectionView(entry, "updates", map[string]string{"provider": "brew"}, report)

	if view.Status != plan.StatusHeld || len(view.Steps) != 1 || view.Report != nil {
		t.Fatalf("unexpected updates view: %#v", view)
	}
	if view.Summary.SkippedSteps != 1 || view.Summary.HeldSteps != 1 {
		t.Fatalf("unexpected summary: %#v", view.Summary)
	}
}

func TestTapAndSafetyAttentionContributeToSummary(t *testing.T) {
	report := Report[testPolicy]{
		Safety: []securitygate.Gate{{
			Provider: "brew",
			Status:   plan.StatusOK,
			Findings: []securitygate.Finding{{Decision: "review"}},
		}},
		Inventory: plan.Report{Items: []plan.Item{{Status: plan.StatusExtra}}},
	}
	summary := Summary(report)
	if summary.SafetyAttention != 1 || summary.InventoryAttention != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestSummaryPreservesGateAttentionContract(t *testing.T) {
	for _, status := range []plan.Status{plan.StatusDrift, plan.StatusUnavailable} {
		report := Report[testPolicy]{Safety: []securitygate.Gate{{
			Provider: "brew",
			Status:   status,
			Findings: []securitygate.Finding{{Decision: "allow"}},
		}}}
		if got := Summary(report).SafetyAttention; got != 0 {
			t.Fatalf("status %q SafetyAttention = %d, want 0", status, got)
		}
	}
}

func TestSecuritySectionStatusIgnoresNonGateAttentionStatuses(t *testing.T) {
	for _, status := range []plan.Status{plan.StatusDrift, plan.StatusUnavailable} {
		report := Report[testPolicy]{Safety: []securitygate.Gate{{Provider: "brew", Status: status}}}
		if got := SectionStatus(report, "security"); got != plan.StatusOK {
			t.Fatalf("status %q section status = %q, want ok", status, got)
		}
		entry := CacheEntry[testPolicy]{Version: 1, Type: "update", Report: report}
		view := BuildSectionView(entry, "security", nil, report)
		if view.Status != plan.StatusOK {
			t.Fatalf("status %q view status = %q, want ok", status, view.Status)
		}
	}
}

func TestFilterReportGateQueryUsesFirstAttentionReason(t *testing.T) {
	report := Report[testPolicy]{Safety: []securitygate.Gate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []securitygate.Finding{
			{Decision: "allow", Reason: "allowed metadata"},
			{Decision: "review", Reason: "review provenance"},
		},
	}}}
	filtered := FilterReport(report, Filter{Status: "held", Query: "review provenance"}, nil)
	if got := len(filtered.Safety); got != 1 {
		t.Fatalf("len(Safety) = %d, want 1", got)
	}
	if got := len(filtered.Safety[0].Findings); got != 0 {
		t.Fatalf("len(filtered findings) = %d, want 0 for held status", got)
	}
}
