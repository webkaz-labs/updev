package securitygate

import "testing"

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
