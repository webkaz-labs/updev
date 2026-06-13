package runner

import (
	"errors"
	"testing"
)

func TestDetailPrefersStderrStdoutErrFallback(t *testing.T) {
	if got := ResultDetail(Result{Stderr: " stderr ", Stdout: "stdout", Err: errors.New("err")}, "fallback", ResultDetailOption{}); got != "stderr" {
		t.Fatalf("expected stderr, got %q", got)
	}
	if got := ResultDetail(Result{Stdout: " stdout ", Err: errors.New("err")}, "fallback", ResultDetailOption{}); got != "stdout" {
		t.Fatalf("expected stdout, got %q", got)
	}
	if got := ResultDetail(Result{Err: errors.New("err")}, "fallback", ResultDetailOption{}); got != "err" {
		t.Fatalf("expected err, got %q", got)
	}
	if got := ResultDetail(Result{}, " fallback ", ResultDetailOption{}); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestDetailCanIncludeExitStatus(t *testing.T) {
	if got := ResultDetail(Result{Code: 7}, "fallback", ResultDetailOption{IncludeExitStatus: true}); got != "exit status 7" {
		t.Fatalf("expected exit status, got %q", got)
	}
	if got := ResultDetail(Result{Code: 7}, "fallback", ResultDetailOption{}); got != "fallback" {
		t.Fatalf("expected fallback without exit status, got %q", got)
	}
}
