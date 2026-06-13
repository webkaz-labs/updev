package registryaudit

import "testing"

func TestPostureReviewCountsUseSecurityDecisionSemantics(t *testing.T) {
	if got := NPMPostureReviewCount([]NPMPosture{{Decision: "allow"}, {Decision: "review"}, {Decision: "hold"}}); got != 2 {
		t.Fatalf("NPMPostureReviewCount = %d, want 2", got)
	}
	if got := CargoPostureReviewCount([]CargoPosture{{Decision: "allow"}, {Decision: "block"}}); got != 1 {
		t.Fatalf("CargoPostureReviewCount = %d, want 1", got)
	}
	if got := PyPIPostureReviewCount([]PyPIPosture{{Decision: "allow"}, {Decision: ""}}); got != 1 {
		t.Fatalf("PyPIPostureReviewCount = %d, want 1", got)
	}
}
