package reviewui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStartupProgressWritesAndClears(t *testing.T) {
	var out bytes.Buffer
	progress := NewStartupProgressWithDelay(true, &out, "読み込み中", 0)
	progress.Start()
	time.Sleep(10 * time.Millisecond)
	progress.Done()
	if got := out.String(); !strings.Contains(got, "読み込み中") || !strings.Contains(got, "\033[2K") {
		t.Fatalf("expected progress write and clear, got %q", got)
	}
}
