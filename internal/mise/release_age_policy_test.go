package mise

import (
	"context"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type releaseAgePolicyRunner struct {
	results []runner.Result
	calls   [][]string
}

func (fake *releaseAgePolicyRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	fake.calls = append(fake.calls, append([]string{name}, args...))
	if len(fake.results) == 0 {
		return runner.Result{Code: 1, Err: context.Canceled}
	}
	result := fake.results[0]
	fake.results = fake.results[1:]
	return result
}

func TestMinimumReleaseAgeFromSettings(t *testing.T) {
	value, source, ok := MinimumReleaseAgeFromSettings(map[string]any{
		"minimum_release_age": map[string]any{"value": "3d", "source": "~/.config/mise/config.toml"},
	})
	if !ok || value != "3d" || source != "~/.config/mise/config.toml" {
		t.Fatalf("unexpected minimum_release_age parse: value=%q source=%q ok=%v", value, source, ok)
	}
}

func TestDetectMinimumReleaseAgeActive(t *testing.T) {
	fake := &releaseAgePolicyRunner{results: []runner.Result{
		{Stdout: "--minimum-release-age"},
		{Stdout: `{"minimum_release_age":{"value":"3d","source":"config.toml"}}`},
	}}
	evidence := DetectMinimumReleaseAge(context.Background(), fake, "/repo")
	if evidence.Status != plan.StatusOK || !BoolValue(evidence.Active) || evidence.Value != "3d" || evidence.Source != "config.toml" {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
	if len(fake.calls) != 2 || strings.Join(fake.calls[1], "\x00") != "mise\x00settings\x00ls\x00--json-extended\x00--cd\x00/repo" {
		t.Fatalf("unexpected runner calls: %#v", fake.calls)
	}
}
