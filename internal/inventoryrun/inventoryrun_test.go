package inventoryrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/runner"
)

type recordingRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (recording *recordingRunner) LookPath(name string) (string, error) {
	if name == "mise" {
		return "/fake/bin/mise", nil
	}
	return "", errors.New("not found")
}

func (recording *recordingRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	call := append([]string{name}, args...)
	recording.mu.Lock()
	recording.calls = append(recording.calls, call)
	recording.mu.Unlock()
	if name == "mise" && len(args) >= 3 && reflect.DeepEqual(args[:3], []string{"ls", "--current", "--json"}) {
		return runner.Result{Stdout: `{}`}
	}
	return runner.Result{Err: errors.New("unexpected command: " + strings.Join(call, " ")), Code: 1}
}

func TestCollectCachedUsesInjectedRunner(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "dot_config", "mise", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	recording := &recordingRunner{}

	result := CollectCached(context.Background(), root, true, time.Hour, recording, Options{
		UseNativeMiseDesired: true,
		StateDir:             &stateDir,
	})
	if result.Cached {
		t.Fatal("expected a fresh injected inventory collection")
	}
	want := []string{"mise", "ls", "--current", "--json", "--cd", root}
	if len(recording.calls) == 0 {
		t.Fatal("expected injected mise inventory commands")
	}
	for _, call := range recording.calls {
		if reflect.DeepEqual(call, want) {
			continue
		}
		t.Fatalf("expected injected mise inventory command %#v, got %#v", want, recording.calls)
	}
	if len(result.Report.Providers) != 2 {
		t.Fatalf("expected brew and mise provider results, got %#v", result.Report.Providers)
	}
}
