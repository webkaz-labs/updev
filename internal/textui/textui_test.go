package textui

import (
	"strings"
	"testing"
	"time"
)

func TestDisplayWidthIgnoresANSISequences(t *testing.T) {
	value := StyleName("node", true) + " " + StyleVersion("24.16.0", true)
	if got := DisplayWidth(value); got != len("node 24.16.0") {
		t.Fatalf("expected visible width only, got %d for %q", got, value)
	}
}

func TestDisplayWidthUsesGraphemeAwareSegmentsAroundANSI(t *testing.T) {
	value := StyleName("👨‍💻", true) + "dev"
	want := displayWidthCondition.StringWidth("👨‍💻dev")
	if got := DisplayWidth(value); got != want {
		t.Fatalf("expected grapheme-aware width %d, got %d for %q", want, got, value)
	}
}

func TestDisplayWidthKeepsActionTriangleNarrowAndEmojiWide(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{value: "▶bak", want: 4},
		{value: "▶sec", want: 4},
		{value: "🔄upd", want: 5},
		{value: "✅up", want: 4},
		{value: "確認", want: 4},
	}
	for _, tt := range tests {
		if got := DisplayWidth(tt.value); got != tt.want {
			t.Fatalf("DisplayWidth(%q)=%d want %d", tt.value, got, tt.want)
		}
	}
}

func TestTruncatePreservesANSIReset(t *testing.T) {
	value := StyleName("very-long-tool-name", true)
	got := Truncate(value, 8)
	if DisplayWidth(got) != 8 {
		t.Fatalf("expected truncated visible width 8, got %d for %q", DisplayWidth(got), got)
	}
	if !strings.HasSuffix(got, ansiReset) {
		t.Fatalf("expected truncated styled value to end with reset, got %q", got)
	}
}

func TestWrapTextSplitsLongWords(t *testing.T) {
	lines := WrapText("detail: https://github.com/example/repository-with-a-very-long-name", 24)
	if len(lines) < 2 {
		t.Fatalf("expected long URL to wrap, got %#v", lines)
	}
	for _, line := range lines {
		if DisplayWidth(line) > 24 {
			t.Fatalf("expected wrapped line width <= 24, got %d for %q in %#v", DisplayWidth(line), line, lines)
		}
	}
}

func TestColorEnabledRequiresTerm(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "")
	if ColorEnabled() {
		t.Fatal("expected color to be disabled when TERM is unset")
	}
	t.Setenv("TERM", "xterm-256color")
	if !ColorEnabled() {
		t.Fatal("expected color to be enabled when TERM supports it")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled() {
		t.Fatal("expected NO_COLOR to disable color")
	}
}

func TestStyleStatusColorsManualAndPolicyAttentionStates(t *testing.T) {
	for _, status := range []string{"needs-review", "adopt-brew", "adopt-mas", "open-vendor", "ignore-local"} {
		got := StyleStatus(status, true)
		if !strings.Contains(got, ansiYellow) {
			t.Fatalf("expected %s to be warning-colored, got %q", status, got)
		}
	}
	if got := StyleStatus("block", true); !strings.Contains(got, ansiRed) {
		t.Fatalf("expected block to be error-colored, got %q", got)
	}
	if got := StyleStatus("keep-manual", true); !strings.Contains(got, ansiGreen) {
		t.Fatalf("expected keep-manual to be ok-colored, got %q", got)
	}
}

func TestFriendlyAge(t *testing.T) {
	tests := []struct {
		age  time.Duration
		want string
	}{
		{age: 500 * time.Millisecond, want: "0s"},
		{age: 12 * time.Second, want: "12s"},
		{age: 3 * time.Minute, want: "3m"},
		{age: 2 * time.Hour, want: "2h"},
	}
	for _, tt := range tests {
		if got := FriendlyAge(tt.age); got != tt.want {
			t.Fatalf("FriendlyAge(%s)=%q want %q", tt.age, got, tt.want)
		}
	}
}

func TestFilterSummaryUsesProvidedKeyOrder(t *testing.T) {
	got := FilterSummary(map[string]string{
		"query":    "git",
		"provider": "brew",
		"empty":    "",
		"ignored":  "yes",
	}, "provider", "empty", "query")
	want := "provider=brew query=git"
	if got != want {
		t.Fatalf("FilterSummary = %q, want %q", got, want)
	}
}

func TestFilterSummaryWithSeparatorKeepsProvidedKeyOrder(t *testing.T) {
	got := FilterSummaryWithSeparator(map[string]string{
		"name":     "git",
		"decision": "review",
		"kind":     "tap",
	}, ", ", "decision", "kind", "name")
	want := "decision=review, kind=tap, name=git"
	if got != want {
		t.Fatalf("FilterSummaryWithSeparator = %q, want %q", got, want)
	}
}
