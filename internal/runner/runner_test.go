package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type requestRunner struct {
	mode   string
	env    []string
	name   string
	args   []string
	stdout io.Writer
	stderr io.Writer
}

func (recording *requestRunner) Run(_ context.Context, name string, args ...string) Result {
	recording.mode = "run"
	recording.name = name
	recording.args = append([]string(nil), args...)
	return Result{Stdout: "run"}
}

func (recording *requestRunner) RunStreaming(_ context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) Result {
	recording.mode = "stream"
	recording.name = name
	recording.args = append([]string(nil), args...)
	recording.stdout = stdout
	recording.stderr = stderr
	return Result{Stdout: "stream"}
}

func (recording *requestRunner) RunWithEnv(_ context.Context, env []string, name string, args ...string) Result {
	recording.mode = "env"
	recording.env = append([]string(nil), env...)
	recording.name = name
	recording.args = append([]string(nil), args...)
	return Result{Stdout: "env"}
}

func (recording *requestRunner) RunStreamingWithEnv(_ context.Context, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) Result {
	recording.mode = "env-stream"
	recording.env = append([]string(nil), env...)
	recording.name = name
	recording.args = append([]string(nil), args...)
	recording.stdout = stdout
	recording.stderr = stderr
	return Result{Stdout: "env-stream"}
}

func TestExecuteUsesCombinedEnvStreamingCapability(t *testing.T) {
	recording := &requestRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	result := Execute(context.Background(), recording, Request{
		Name:   "mise",
		Args:   []string{"upgrade", "node"},
		Env:    []string{"MISE_GITHUB_TOKEN=token"},
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if result.Stdout != "env-stream" || recording.mode != "env-stream" {
		t.Fatalf("expected env-stream execution, result=%#v recording=%#v", result, recording)
	}
	if recording.name != "mise" || !reflect.DeepEqual(recording.args, []string{"upgrade", "node"}) || !reflect.DeepEqual(recording.env, []string{"MISE_GITHUB_TOKEN=token"}) {
		t.Fatalf("unexpected request projection: %#v", recording)
	}
	if recording.stdout != &stdout || recording.stderr != &stderr {
		t.Fatalf("expected exact stream writers, got %#v", recording)
	}
}

type envOnlyRunner struct {
	mode string
	env  []string
}

func (recording *envOnlyRunner) Run(_ context.Context, _ string, _ ...string) Result {
	recording.mode = "run"
	return Result{Stdout: "run"}
}

func (recording *envOnlyRunner) RunWithEnv(_ context.Context, env []string, _ string, _ ...string) Result {
	recording.mode = "env"
	recording.env = append([]string(nil), env...)
	return Result{Stdout: "env"}
}

func TestExecuteUsesEnvCapabilityWithoutWriters(t *testing.T) {
	recording := &envOnlyRunner{}
	result := Execute(context.Background(), recording, Request{Name: "mise", Env: []string{"TOKEN=value"}})
	if result.Stdout != "env" || recording.mode != "env" || !reflect.DeepEqual(recording.env, []string{"TOKEN=value"}) {
		t.Fatalf("expected env execution, result=%#v recording=%#v", result, recording)
	}
}

type streamingOnlyRunner struct {
	mode string
}

func (recording *streamingOnlyRunner) Run(_ context.Context, _ string, _ ...string) Result {
	recording.mode = "run"
	return Result{Stdout: "run"}
}

func (recording *streamingOnlyRunner) RunStreaming(_ context.Context, _ io.Writer, _ io.Writer, _ string, _ ...string) Result {
	recording.mode = "stream"
	return Result{Stdout: "stream"}
}

type plainRunner struct {
	calls int
}

func (recording *plainRunner) Run(_ context.Context, _ string, _ ...string) Result {
	recording.calls++
	return Result{Stdout: "run"}
}

type splitCapabilityRunner struct {
	plainRunner
	streamCalls int
	envCalls    int
}

func (recording *splitCapabilityRunner) RunStreaming(_ context.Context, _ io.Writer, _ io.Writer, _ string, _ ...string) Result {
	recording.streamCalls++
	return Result{Stdout: "stream"}
}

func (recording *splitCapabilityRunner) RunWithEnv(_ context.Context, _ []string, _ string, _ ...string) Result {
	recording.envCalls++
	return Result{Stdout: "env"}
}

func TestExecuteCapabilityMatrix(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		recording := &plainRunner{}
		result := Execute(context.Background(), recording, Request{Name: "mise"})
		if result.Stdout != "run" || recording.calls != 1 {
			t.Fatalf("expected plain execution, result=%#v calls=%d", result, recording.calls)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		recording := &streamingOnlyRunner{}
		result := Execute(context.Background(), recording, Request{Name: "mise", Stdout: io.Discard})
		if result.Stdout != "stream" || recording.mode != "stream" {
			t.Fatalf("expected streaming execution, result=%#v mode=%q", result, recording.mode)
		}
	})

	t.Run("combined capability is required", func(t *testing.T) {
		recording := &splitCapabilityRunner{}
		result := Execute(context.Background(), recording, Request{
			Name:   "mise",
			Env:    []string{"TOKEN=value"},
			Stdout: io.Discard,
		})
		assertUnsupportedExecution(t, result, "environment and streaming")
		if recording.calls != 0 || recording.streamCalls != 0 || recording.envCalls != 0 {
			t.Fatalf("unsupported request must not fall back, recording=%#v", recording)
		}
	})

	t.Run("streaming capability is required", func(t *testing.T) {
		recording := &plainRunner{}
		result := Execute(context.Background(), recording, Request{Name: "mise", Stderr: io.Discard})
		assertUnsupportedExecution(t, result, "streaming")
		if recording.calls != 0 {
			t.Fatalf("unsupported request must not fall back, calls=%d", recording.calls)
		}
	})

	t.Run("environment capability is required", func(t *testing.T) {
		recording := &plainRunner{}
		result := Execute(context.Background(), recording, Request{Name: "mise", Env: []string{"TOKEN=value"}})
		assertUnsupportedExecution(t, result, "environment")
		if recording.calls != 0 {
			t.Fatalf("unsupported request must not fall back, calls=%d", recording.calls)
		}
	})
}

func assertUnsupportedExecution(t *testing.T, result Result, capability string) {
	t.Helper()
	if result.Code != 1 || result.Err == nil || !strings.Contains(result.Stderr, capability) {
		t.Fatalf("expected explicit %s capability error, got %#v", capability, result)
	}
}

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
