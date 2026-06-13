package textui

import (
	"strings"
	"testing"
)

func TestActionBadgeCompactsKnownActionsInStablePriority(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	got := ActionBadgeWithWidth([]ActionBadgeInput{
		{Label: "backend 整理を開く"},
		{Label: "security review"},
		{Badge: "up 1.7->1.8", Status: "updated"},
		{Badge: "hold 2d", Status: "held"},
	}, 64, false)
	want := "▶up 1.7->1.8 ▶sec ▶hold 2d ▶bak"
	if got != want {
		t.Fatalf("ActionBadge = %q, want %q", got, want)
	}
}

func TestActionBadgeUsesNerdFontMarkersWhenEnabled(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "1")
	got := ActionBadge([]ActionBadgeInput{
		{Badge: "sec"},
		{Badge: "upd"},
		{Badge: "bak"},
	}, false)
	for _, want := range []string{"🔄upd", "🔒sec", "📦bak"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestActionBadgeTruncatesAndSummarizesOverflow(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	got := ActionBadgeWithWidth([]ActionBadgeInput{
		{Badge: "upd 1.7->1.8.1"},
		{Badge: "sec"},
		{Badge: "hold 1d"},
		{Badge: "bak"},
	}, 14, false)
	if DisplayWidth(got) > 14 {
		t.Fatalf("expected badge width <= 14, got %d for %q", DisplayWidth(got), got)
	}
	if !strings.Contains(got, "…") && !strings.Contains(got, "+") {
		t.Fatalf("expected truncated or summarized badge, got %q", got)
	}
}

func TestActionBadgeColorsStatusBadges(t *testing.T) {
	t.Setenv("UPDEV_NERD_FONT", "0")
	got := ActionBadge([]ActionBadgeInput{{Badge: "hold 1d", Status: "held"}}, true)
	if !strings.Contains(got, ansiYellow) {
		t.Fatalf("expected held badge to use status color, got %q", got)
	}
}
