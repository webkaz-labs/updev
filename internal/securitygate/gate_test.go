package securitygate

import (
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
)

func TestSummaryFromFindingsCountsDecisions(t *testing.T) {
	summary := SummaryFromFindings([]Finding{
		{Decision: "allow"},
		{Decision: " review "},
		{Decision: "HOLD"},
		{Decision: "block"},
		{Decision: ""},
	})
	if summary == nil {
		t.Fatal("expected summary")
	}
	if summary.Findings != 5 || summary.Allow != 1 || summary.Review != 1 || summary.Hold != 1 || summary.Block != 1 || summary.Unknown != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestSummaryFromFindingsEmpty(t *testing.T) {
	if got := SummaryFromFindings(nil); got != nil {
		t.Fatalf("expected nil summary, got %#v", got)
	}
}

func TestApplyFindingsSetsSummaryAndHeldStatus(t *testing.T) {
	gate := ApplyFindings(Gate{Provider: "brew", Status: plan.StatusOK}, []Finding{
		{Decision: "allow"},
		{Decision: "review"},
	})
	if gate.Status != plan.StatusHeld {
		t.Fatalf("expected held status, got %q", gate.Status)
	}
	if gate.Summary == nil || gate.Summary.Findings != 2 || gate.Summary.Allow != 1 || gate.Summary.Review != 1 {
		t.Fatalf("unexpected summary: %#v", gate.Summary)
	}
}

func TestApplyFindingsPreservesNonOKStatus(t *testing.T) {
	gate := ApplyFindings(Gate{Provider: "brew", Status: plan.StatusError}, []Finding{{Decision: "review"}})
	if gate.Status != plan.StatusError {
		t.Fatalf("expected existing error status, got %q", gate.Status)
	}
}

func TestDecisionHelpers(t *testing.T) {
	if !ValidDecision("allow") || !ValidDecision(" review ") || !ValidDecision("hold") || !ValidDecision("block") {
		t.Fatal("expected allow/review/hold/block to be valid decisions")
	}
	if ValidDecision("unknown") || ValidDecision("") {
		t.Fatal("expected unknown and empty decisions to be invalid policy decisions")
	}
	if DecisionNeedsAttention("allow") {
		t.Fatal("expected allow not to need attention")
	}
	for _, decision := range []string{"", "unknown", "review", "hold", "block"} {
		if !DecisionNeedsAttention(decision) {
			t.Fatalf("expected %q to need attention", decision)
		}
	}
	priorities := map[string]int{"block": 4, "hold": 3, "review": 2, "unknown": 1, "allow": 0, "": 0}
	for decision, want := range priorities {
		if got := DecisionPriority(decision); got != want {
			t.Fatalf("expected priority %d for %q, got %d", want, decision, got)
		}
	}
}
