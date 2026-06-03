package textui

import (
	"strings"
	"testing"
)

func TestDisplayWidthIgnoresANSISequences(t *testing.T) {
	value := StyleName("node", true) + " " + StyleVersion("24.16.0", true)
	if got := DisplayWidth(value); got != len("node 24.16.0") {
		t.Fatalf("expected visible width only, got %d for %q", got, value)
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
