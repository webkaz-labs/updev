package brew

import "testing"

func TestPostureReviewCountTreatsNonAllowAsAttention(t *testing.T) {
	postures := []Posture{
		{Decision: "allow"},
		{Decision: "review"},
		{Decision: "hold"},
		{Decision: "block"},
		{Decision: "unknown"},
		{Decision: ""},
	}

	if got := PostureReviewCount(postures); got != 5 {
		t.Fatalf("expected all non-allow decisions to need attention, got %d", got)
	}
	if !HasPostureReview(postures) {
		t.Fatal("expected review attention")
	}
}
