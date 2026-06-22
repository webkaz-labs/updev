package syncreport

import (
	"testing"

	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/plan"
)

func TestHomebrewProfileMismatchIsNotAdoptable(t *testing.T) {
	item := plan.Item{
		Provider: "brew",
		Kind:     "cask",
		Name:     "cursor-cli",
		Status:   plan.StatusExtra,
		Live:     true,
		Detail:   inventoryannotate.ProfileMismatchDetail("work"),
	}
	if HomebrewExtraAdoptable(item) {
		t.Fatalf("expected source/rendered scope mismatch to skip Brewfile adoption action: %#v", item)
	}
}
