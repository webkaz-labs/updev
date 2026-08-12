package backend

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type registryTestRunner struct {
	mu           sync.Mutex
	paths        map[string]error
	results      map[string]runner.Result
	lockFailures map[string]runner.Result
	calls        []string
}

func (fake *registryTestRunner) LookPath(name string) (string, error) {
	if err, ok := fake.paths[name]; ok {
		return "/bin/" + name, err
	}
	return "", fmt.Errorf("missing %s", name)
}

func (fake *registryTestRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	key := name + "\x00" + strings.Join(args, "\x00")
	fake.mu.Lock()
	fake.calls = append(fake.calls, key)
	fake.mu.Unlock()
	if name == "mise" && len(args) > 0 && args[0] == "lock" {
		tool := args[len(args)-1]
		if result, ok := fake.lockFailures[tool]; ok {
			return result
		}
		return runner.Result{Stdout: "lock resolved"}
	}
	if result, ok := fake.results[key]; ok {
		return result
	}
	return runner.Result{Code: 1, Err: fmt.Errorf("unexpected command: %s", key)}
}

func TestFindingsPreferMiseRegistryForFormulaAndCLIOnlyCask(t *testing.T) {
	items := []plan.Item{
		{Kind: "brew", Name: "btop"},
		{Kind: "brew", Name: "rtk"},
		{Kind: "cask", Name: "antigravity-cli"},
		{Kind: "cask", Name: "firefox"},
		{Kind: "brew", Name: "chezmoi"},
		{Kind: "brew", Name: "gh"},
		{Kind: "brew", Name: "ripgrep"},
		{Kind: "brew", Name: "podman"},
	}
	names := "antigravity-cli\x00btop\x00chezmoi\x00firefox\x00gh\x00podman\x00ripgrep\x00rtk"
	fake := &registryTestRunner{
		paths: map[string]error{"btop": nil, "rtk": nil, "agy": nil, "firefox": nil, "chezmoi": nil, "gh": nil, "rg": nil, "podman": nil},
		results: map[string]runner.Result{
			"mise\x00registry\x00--json": {Stdout: `[
  {"short":"antigravity-cli","aliases":["agy"],"backends":["aqua:google-antigravity/antigravity-cli"]},
  {"short":"btop","backends":["aqua:aristocratos/btop"]},
  {"short":"rtk","backends":["aqua:rtk-ai/rtk","github:rtk-ai/rtk"]},
  {"short":"firefox","backends":["aqua:mozilla/firefox"]},
  {"short":"chezmoi","backends":["aqua:twpayne/chezmoi"]},
  {"short":"gh","backends":["aqua:cli/cli"]},
  {"short":"ripgrep","aliases":["rg"],"backends":["aqua:BurntSushi/ripgrep"]},
  {"short":"podman","backends":["github:podman-container-tools/podman"]}
]`},
			"brew\x00info\x00--json=v2\x00" + names: {Stdout: `{
  "formulae": [
    {"name":"btop","full_name":"btop","installed":[{"version":"1.4.7"}]},
    {"name":"rtk","full_name":"rtk","installed":[{"version":"0.44.2"}]},
    {"name":"chezmoi","full_name":"chezmoi","installed":[{"version":"2.70.4"}]},
    {"name":"gh","full_name":"gh","installed":[{"version":"2.80.0"}]},
    {"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"15.1.0"}]},
    {"name":"podman","full_name":"podman","installed":[{"version":"5.7.1"}]}
  ],
  "casks": [
    {"token":"antigravity-cli","full_token":"antigravity-cli","installed":"1.0.9,6003845613092864","artifacts":[{"binary":["antigravity",{"target":"agy"}],"target":"/usr/local/bin/agy"},{"zap":[{"trash":"~/.cache/agy"}]}]},
    {"token":"firefox","full_token":"firefox","installed":"145.0","artifacts":[{"app":["Firefox.app"]}]}
  ]
}`},
		},
		lockFailures: map[string]runner.Result{"btop": {Code: 1, Err: fmt.Errorf("unsupported env: darwin/amd64")}},
	}
	registry := Registry{KeepHomebrew: []string{"brew/chezmoi", "brew/gh", "brew/ripgrep", "brew/podman"}}
	findings, warnings := Findings(context.Background(), items, map[string]string{}, registry, fake)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
	if len(findings) != 3 {
		t.Fatalf("expected two compatible recommendations and one incompatible candidate, got %#v", findings)
	}
	byName := map[string]Finding{}
	for _, finding := range findings {
		byName[finding.Name] = finding
	}
	for name, version := range map[string]string{"rtk": "0.44.2", "antigravity-cli": "1.0.9"} {
		finding, ok := byName[name]
		if !ok {
			t.Fatalf("missing registry finding for %s: %#v", name, findings)
		}
		if finding.Type != "homebrew-to-mise" || finding.RecommendedName != name || finding.RecommendedTier != "mise/aqua" || finding.CurrentSpec != version || finding.ReleaseAssetStatus != "compatible" {
			t.Fatalf("unexpected registry finding for %s: %#v", name, finding)
		}
	}
	if byName["antigravity-cli"].Kind != "cask" || !containsRegistryTestString(byName["antigravity-cli"].CommandNames, "agy") {
		t.Fatalf("expected CLI-only cask evidence: %#v", byName["antigravity-cli"])
	}
	if btop := byName["btop"]; btop.Type != "homebrew-to-mise-candidate" || btop.RecommendationKind != "candidate" || btop.ReleaseAssetStatus != "no-match" {
		t.Fatalf("expected macOS-incompatible btop to remain review-only, got %#v", btop)
	}
	for _, excluded := range []string{"firefox", "chezmoi", "gh", "ripgrep", "podman"} {
		if _, ok := byName[excluded]; ok {
			t.Fatalf("expected %s to remain Homebrew-owned, got %#v", excluded, byName[excluded])
		}
	}
}

func containsRegistryTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPreferredMiseRegistryBackendUsesConfiguredOrder(t *testing.T) {
	entry := mustRegistryEntry(t, `[{"short":"rtk","backends":["aqua:rtk-ai/rtk","github:rtk-ai/rtk"]}]`, "rtk")
	backend, tier, ok := preferredMiseRegistryBackend(entry, Registry{PreferenceOrder: []string{"mise/github", "mise/aqua"}})
	if !ok || backend != "github:rtk-ai/rtk" || tier.Label != "mise/github" || tier.Rank != 1 {
		t.Fatalf("unexpected configured registry backend: backend=%q tier=%#v ok=%v", backend, tier, ok)
	}
	if recommended := registryRecommendationName(entry, backend); recommended != "github:rtk-ai/rtk" {
		t.Fatalf("expected explicit backend when configured order overrides registry order, got %q", recommended)
	}
}

func TestRegistryRecommendationNameKeepsShortNameForRegistryDefault(t *testing.T) {
	entry := mustRegistryEntry(t, `[{"short":"rtk","backends":["aqua:rtk-ai/rtk","github:rtk-ai/rtk"]}]`, "rtk")
	if recommended := registryRecommendationName(entry, "aqua:rtk-ai/rtk"); recommended != "rtk" {
		t.Fatalf("expected registry-default backend to keep short name, got %q", recommended)
	}
}

func TestHomebrewRegistryRecommendationUsesExplicitConfiguredBackend(t *testing.T) {
	entry := mustRegistryEntry(t, `[{"short":"rtk","backends":["aqua:rtk-ai/rtk","github:rtk-ai/rtk"]}]`, "rtk")
	fake := &registryTestRunner{paths: map[string]error{"rtk": nil}}
	recommendation, ok := homebrewRegistryRecommendation(
		context.Background(),
		plan.Item{Kind: "brew", Name: "rtk"},
		entry,
		homebrewPackageEvidence{Kind: "brew", Name: "rtk", Version: "0.44.2", CLIOnly: true},
		Registry{PreferenceOrder: []string{"mise/github", "mise/aqua"}},
		fake,
	)
	if !ok || recommendation.Name != "github:rtk-ai/rtk" || recommendation.Tier != "mise/github" {
		t.Fatalf("expected explicit configured backend recommendation, got %#v ok=%v", recommendation, ok)
	}
	lockCall := ""
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "mise\x00lock\x00") {
			lockCall = call
		}
	}
	if !strings.HasSuffix(lockCall, "\x00github:rtk-ai/rtk") {
		t.Fatalf("expected platform lock to resolve the explicit backend, got %q", lockCall)
	}
}

func TestMiseRegistryPlatformLockRejectsControlCharacters(t *testing.T) {
	fake := &registryTestRunner{}
	if miseRegistryPlatformLockCheck(context.Background(), "demo\n[env]", "1.0.0", fake) {
		t.Fatal("expected invalid config atom to be rejected")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("expected invalid target to avoid subprocesses, got %#v", fake.calls)
	}
}

func TestFindingsBoundsRegistryPlatformChecks(t *testing.T) {
	items := make([]plan.Item, 0, maxRegistryPlatformChecks+2)
	paths := map[string]error{}
	results := map[string]runner.Result{}
	names := make([]string, 0, maxRegistryPlatformChecks+2)
	registryRows := make([]string, 0, maxRegistryPlatformChecks+2)
	formulaRows := make([]string, 0, maxRegistryPlatformChecks+2)
	for index := 0; index < maxRegistryPlatformChecks+2; index++ {
		name := fmt.Sprintf("tool%02d", index)
		items = append(items, plan.Item{Kind: "brew", Name: name})
		names = append(names, name)
		paths[name] = nil
		registryRows = append(registryRows, fmt.Sprintf(`{"short":%q,"backends":[%q]}`, name, "aqua:example/"+name))
		formulaRows = append(formulaRows, fmt.Sprintf(`{"name":%q,"full_name":%q,"installed":[{"version":"1.0.0"}]}`, name, name))
	}
	results["mise\x00registry\x00--json"] = runner.Result{Stdout: "[" + strings.Join(registryRows, ",") + "]"}
	results["brew\x00info\x00--json=v2\x00"+strings.Join(names, "\x00")] = runner.Result{Stdout: `{"formulae":[` + strings.Join(formulaRows, ",") + `],"casks":[]}`}
	fake := &registryTestRunner{paths: paths, results: results}

	findings, warnings := Findings(context.Background(), items, map[string]string{}, Registry{}, fake)
	if len(findings) != maxRegistryPlatformChecks {
		t.Fatalf("expected %d bounded findings, got %d", maxRegistryPlatformChecks, len(findings))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "skipped 2") {
		t.Fatalf("expected explicit platform-check limit warning, got %#v", warnings)
	}
	platformLocks := 0
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "mise\x00lock\x00") {
			platformLocks++
		}
	}
	if platformLocks != maxRegistryPlatformChecks {
		t.Fatalf("expected %d isolated platform locks, got %d", maxRegistryPlatformChecks, platformLocks)
	}
}

func mustRegistryEntry(t *testing.T, payload string, name string) mise.RegistryEntry {
	t.Helper()
	index := mise.RegistryIndexFromJSON(payload)
	entry, ok := mise.RegistryEntryForTool(index, name)
	if !ok {
		t.Fatalf("missing registry entry %s", name)
	}
	return entry
}
