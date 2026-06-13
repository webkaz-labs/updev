package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/webkaz-labs/updev/internal/runner"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("UPDEV_LANG", "en")
	_ = os.Setenv("NO_COLOR", "1")
	if wd, err := os.Getwd(); err == nil {
		path := filepath.Clean(filepath.Join(wd, "..", "..", "mise.toml"))
		if _, err := os.Stat(path); err == nil {
			_ = os.Setenv("MISE_TRUSTED_CONFIG_PATHS", path)
		}
	}
	os.Exit(m.Run())
}

type fakeCommandRunner struct {
	mu        sync.Mutex
	result    runner.Result
	results   map[string]runner.Result
	sequences map[string][]runner.Result
	paths     map[string]error
	calls     [][]string
}

func (fake *fakeCommandRunner) LookPath(name string) (string, error) {
	if fake.paths != nil {
		if err, ok := fake.paths[name]; ok {
			if err != nil {
				return "", err
			}
			return "/fake/bin/" + name, nil
		}
	}
	return "/fake/bin/" + name, nil
}

func (fake *fakeCommandRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	call := append([]string{name}, args...)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls = append(fake.calls, call)
	key := strings.Join(call, "\x00")
	if fake.sequences != nil {
		if sequence := fake.sequences[key]; len(sequence) > 0 {
			result := sequence[0]
			fake.sequences[key] = sequence[1:]
			return result
		}
	}
	if fake.results != nil {
		if result, ok := fake.results[key]; ok {
			return result
		}
	}
	return fake.result
}

func (fake *fakeCommandRunner) RunStreaming(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result {
	result := fake.Run(ctx, name, args...)
	if stdout != nil && result.Stdout != "" {
		_, _ = io.WriteString(stdout, result.Stdout)
	}
	if stderr != nil && result.Stderr != "" {
		_, _ = io.WriteString(stderr, result.Stderr)
	}
	return result
}

type envRecordingRunner struct {
	fakeCommandRunner
	envCalls [][]string
}

func (recording *envRecordingRunner) RunWithEnv(ctx context.Context, env []string, name string, args ...string) runner.Result {
	recording.envCalls = append(recording.envCalls, append([]string(nil), env...))
	return recording.fakeCommandRunner.Run(ctx, name, args...)
}

func (recording *envRecordingRunner) RunStreamingWithEnv(ctx context.Context, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) runner.Result {
	recording.envCalls = append(recording.envCalls, append([]string(nil), env...))
	result := recording.fakeCommandRunner.Run(ctx, name, args...)
	if stdout != nil && result.Stdout != "" {
		_, _ = io.WriteString(stdout, result.Stdout)
	}
	if stderr != nil && result.Stderr != "" {
		_, _ = io.WriteString(stderr, result.Stderr)
	}
	return result
}

type deadlineRecordingRunner struct {
	calls       int
	sawDeadline bool
	result      runner.Result
}

func (recording *deadlineRecordingRunner) Run(ctx context.Context, _ string, _ ...string) runner.Result {
	recording.calls++
	if _, ok := ctx.Deadline(); ok {
		recording.sawDeadline = true
	}
	return recording.result
}

func enableManualMarkdownCompat(t *testing.T, root string) {
	t.Helper()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[inventory.manual]\nmarkdown_compat = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
}

func enableBrewfileWriteMode(t *testing.T, root string, mode string) {
	t.Helper()
	configPath := filepath.Join(root, "updev.toml")
	if err := os.WriteFile(configPath, []byte("[brewfile]\nwrite_mode = \""+mode+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDEV_CONFIG", configPath)
}
