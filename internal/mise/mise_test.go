package mise

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func TestDesiredParsesToolsTable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `
[tools]
go = "1.26.3"
"npm:pnpm" = "11.1.2"
# ignored = "commented"

[settings]
experimental = false
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := (Provider{Root: root, Runner: nil}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "go" || items[1].Name != "npm:pnpm" {
		t.Fatalf("unexpected mise items: %#v", items)
	}
}

func TestAddRemoveToolPreservesToolsTable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	content := `[tools]
go = "1.26.3"

[settings]
experimental = false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := AddTool(root, "npm:demo", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !HasTool(root, "npm:demo") {
		t.Fatalf("expected npm:demo to be added")
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `"npm:demo" = "1.2.3"`) {
		t.Fatalf("expected quoted backend entry, got %s", updated)
	}
	changed, err = RemoveTool(root, "npm:demo")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || HasTool(root, "npm:demo") {
		t.Fatalf("expected npm:demo to be removed")
	}
}

func TestRenameToolPreservesSpecAndInlineComment(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	content := `[tools]
"cargo:fd-find" = { version = "10.4.2", os = ["macos/x64"] } # keep condition

[settings]
experimental = false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := RenameTool(root, "cargo:fd-find", "aqua:sharkdp/fd")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || HasTool(root, "cargo:fd-find") || !HasTool(root, "aqua:sharkdp/fd") {
		t.Fatalf("expected mise tool rename to update desired tools")
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"aqua:sharkdp/fd" = { version = "10.4.2", os = ["macos/x64"] } # keep condition`, `[settings]`} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("expected renamed config to contain %q:\n%s", want, updated)
		}
	}
	projectPath := filepath.Join(root, "mise.toml")
	if err := os.WriteFile(projectPath, []byte(`[tools]
"cargo:git-delta" = "0.19.2"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err = RenameTool(root, "cargo:git-delta", "aqua:dandavison/delta")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || HasTool(root, "cargo:git-delta") || !HasTool(root, "aqua:dandavison/delta") {
		t.Fatalf("expected project-local mise tool rename to update desired tools")
	}
	projectUpdated, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectUpdated), `"aqua:dandavison/delta" = "0.19.2"`) {
		t.Fatalf("expected project-local rename, got %s", projectUpdated)
	}
}

func TestAddToolRequiresPinnedVersion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTool(root, "npm:demo", ""); err == nil || !strings.Contains(err.Error(), "explicit version") {
		t.Fatalf("expected explicit-version error, got %v", err)
	}
	if _, err := AddTool(root, "npm:demo", "latest"); err == nil || !strings.Contains(err.Error(), "latest is not allowed") {
		t.Fatalf("expected latest rejection, got %v", err)
	}
	if _, err := AddTool(root, "python", "lts"); err == nil || !strings.Contains(err.Error(), "lts is only allowed") {
		t.Fatalf("expected non-node lts rejection, got %v", err)
	}
	if changed, err := AddTool(root, "node", "lts"); err != nil || !changed {
		t.Fatalf("expected node lts to remain allowed, changed=%v err=%v", changed, err)
	}
}

func TestManifestIssuesRejectsLatestAndMissingVersions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`[tools]
node = "lts"
go = "1.26.3"
"aqua:modem-dev/hunk" = "latest"
"npm:@rivolink/leaf" = { version = "latest", os = ["macos"] }
"github:jnsahaj/lumen" = { os = ["macos"] }
python = "lts"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	issues, err := ManifestIssues(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 4 {
		t.Fatalf("expected four manifest issues, got %#v", issues)
	}
	got := map[string]ManifestIssue{}
	for _, issue := range issues {
		got[issue.Tool] = issue
	}
	for _, name := range []string{"aqua:modem-dev/hunk", "npm:@rivolink/leaf"} {
		if !strings.Contains(got[name].Reason, "latest is not allowed") || got[name].Version != "latest" {
			t.Fatalf("expected latest issue for %s, got %#v", name, got[name])
		}
	}
	if !strings.Contains(got["github:jnsahaj/lumen"].Reason, "no version") {
		t.Fatalf("expected missing-version issue, got %#v", got["github:jnsahaj/lumen"])
	}
	if !strings.Contains(got["python"].Reason, "lts is only allowed") {
		t.Fatalf("expected non-node lts issue, got %#v", got["python"])
	}
	if got["aqua:modem-dev/hunk"].Backend != "aqua" || got["github:jnsahaj/lumen"].Backend != "github" {
		t.Fatalf("expected backend classification, got %#v", got)
	}
}

func TestRegistryIndexFromJSONIncludesAliases(t *testing.T) {
	registry := RegistryIndexFromJSON(`[{"short":"gcloud","aliases":["google-cloud-sdk"],"backends":["vfox:mise-plugins/vfox-gcloud"]}]`)
	for _, key := range []string{"gcloud", "google-cloud-sdk"} {
		entry, ok := registry[key]
		if !ok || entry.Short != "gcloud" {
			t.Fatalf("expected registry key %q to resolve gcloud, got %#v ok=%v", key, entry, ok)
		}
	}
}

func TestRegistryGitHubBackendUsesAquaRepository(t *testing.T) {
	backend, repo, ok := RegistryGitHubBackend(RegistryEntry{Backends: []string{"vfox:demo/tool", "aqua:astral-sh/uv"}})
	if !ok || backend != "aqua:astral-sh/uv" || repo != "astral-sh/uv" {
		t.Fatalf("expected aqua GitHub backend, got backend=%q repo=%q ok=%v", backend, repo, ok)
	}
}

func TestRegistryProviderMetadataBackendMatchesProviderIdentity(t *testing.T) {
	backend, metadata, ok := RegistryProviderMetadataBackend(
		RegistryEntry{Backends: []string{"vfox:mise-plugins/vfox-gcloud"}},
		ProviderMetadataRegistry(),
	)
	if !ok || backend != "vfox:mise-plugins/vfox-gcloud" || metadata.ID != "google-cloud-cli" {
		t.Fatalf("expected Google Cloud metadata entry, got backend=%q metadata=%#v ok=%v", backend, metadata, ok)
	}
}

func TestManifestIssuesIncludesProjectLocalMiseFiles(t *testing.T) {
	root := t.TempDir()
	globalDir := filepath.Join(root, "dot_config", "mise")
	projectDir := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(globalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte("[tools]\ngo = \"1.26.3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mise.toml"), []byte("[tools]\n\"npm:root-tool\" = \"latest\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".mise.toml"), []byte("[tools]\npython = \"lts\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "mise.toml"), []byte("[tools]\nignored = \"latest\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	issues, err := ManifestIssues(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ManifestIssue{}
	for _, issue := range issues {
		got[issue.Tool] = issue
	}
	if len(got) != 2 {
		t.Fatalf("expected project-local issues only, got %#v", issues)
	}
	if got["npm:root-tool"].Path != filepath.Join(root, "mise.toml") {
		t.Fatalf("expected root mise.toml issue, got %#v", got["npm:root-tool"])
	}
	if got["python"].Path != filepath.Join(projectDir, ".mise.toml") {
		t.Fatalf("expected project .mise.toml issue, got %#v", got["python"])
	}
}

func TestDesiredIncludesProjectLocalMiseFiles(t *testing.T) {
	root := t.TempDir()
	globalDir := filepath.Join(root, "dot_config", "mise")
	projectDir := filepath.Join(root, "tools", "demo")
	if err := os.MkdirAll(globalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte("[tools]\ngo = \"1.26.3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "mise.toml"), []byte(`[tools]
goreleaser = "2.15.4"
"go:golang.org/x/tools/gopls" = "0.22.0"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := fakeRunner{results: map[string]runner.Result{
		"mise\x00ls\x00--current\x00--json\x00--cd\x00" + root: {Stdout: `{"go": [{"version": "1.26.3", "requested_version": "1.26.3", "installed": true}]}`},
		"mise\x00ls\x00--current\x00--json\x00--cd\x00" + projectDir: {Stdout: `{
			"go": [{"version": "1.26.3", "requested_version": "1.26.3", "installed": true}],
			"goreleaser": [{"version": "2.15.4", "requested_version": "2.15.4", "installed": true}],
			"go:golang.org/x/tools/gopls": [{"version": "0.22.0", "requested_version": "0.22.0", "installed": true}]
		}`},
	}}
	items, err := (Provider{Root: root, Runner: &fake, UseNativeDesired: true}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]plan.Item{}
	for _, item := range items {
		got[item.Name] = item
	}
	for _, name := range []string{"go", "goreleaser", "go:golang.org/x/tools/gopls"} {
		if !got[name].Desired && got[name].Name == "" {
			t.Fatalf("expected desired tool %s in %#v", name, items)
		}
		if got[name].Provider != "mise" || got[name].Kind != "tool" {
			t.Fatalf("expected mise tool item for %s, got %#v", name, got[name])
		}
	}

	tools, err := DesiredTools(root)
	if err != nil {
		t.Fatal(err)
	}
	if tools["goreleaser"] != `"2.15.4"` || tools["go:golang.org/x/tools/gopls"] != `"0.22.0"` {
		t.Fatalf("expected project-local desired tools, got %#v", tools)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("expected root and project mise native desired calls, got %#v", fake.calls)
	}
}

func TestLiveIncludesInstalledInactiveTools(t *testing.T) {
	if !miseToolInstalled([]byte(`[{"installed":true,"active":false}]`)) {
		t.Fatal("expected installed inactive tool to count as live")
	}
	if miseToolInstalled([]byte(`[{"installed":false,"active":false}]`)) {
		t.Fatal("did not expect uninstalled tool to count as live")
	}
	if !miseToolInstalled([]byte(`[{"active":false}]`)) {
		t.Fatal("expected missing installed field to preserve old mise compatibility")
	}
	if miseToolInstalled([]byte(`[{"installed":false},{"installed":false}]`)) {
		t.Fatal("did not expect all-uninstalled states to count as live")
	}
	if !miseToolInstalled([]byte(`[{"installed":false},{"installed":true}]`)) {
		t.Fatal("expected any installed state to count as live")
	}
	if miseToolInstalled([]byte(`[]`)) {
		t.Fatal("did not expect empty state list to count as live")
	}
	if miseToolInstalled([]byte(`{"installed":false}`)) {
		t.Fatal("did not expect uninstalled object state to count as live")
	}
	if !miseToolInstalled([]byte(`{"installed":true}`)) {
		t.Fatal("expected installed object state to count as live")
	}
}

func TestLiveUsesAllInstalledToolsNotOnlyCurrent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dot_config", "mise")
	projectDir := filepath.Join(root, "tools", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`[tools]
node = "lts"
"github:openai/tunnel-client" = "0.0.9--context-conduit-topaz"
missing-tool = "latest"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "mise.toml"), []byte(`[tools]
goreleaser = "2.15.4"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := fakeRunner{results: map[string]runner.Result{
		"mise\x00ls\x00--current\x00--json\x00--cd\x00" + root: {Stdout: `{"node": [{"installed": true, "active": true}]}`},
		"mise\x00ls\x00--current\x00--json\x00--cd\x00" + projectDir: {Stdout: `{
			"node": [{"installed": true, "active": true}],
			"github:openai/tunnel-client": [{"installed": true, "active": false}],
			"missing-tool": [{"installed": false, "active": false}],
			"goreleaser": [{"installed": true, "active": false}]
		}`},
		"mise\x00ls\x00--json\x00github:openai/tunnel-client": {Stdout: `[{"installed": true, "active": false}]`},
		"mise\x00ls\x00--json\x00missing-tool":                {Stdout: `[{"installed": false, "active": false}]`},
		"mise\x00ls\x00--json\x00goreleaser":                  {Stdout: `[{"installed": true, "active": false}]`},
	}}
	items, err := (Provider{Root: root, Runner: &fake, UseNativeDesired: true}).Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Name] = true
	}
	if !got["node"] || !got["github:openai/tunnel-client"] || !got["goreleaser"] {
		t.Fatalf("expected installed active and inactive tools, got %#v", got)
	}
	if got["missing-tool"] {
		t.Fatalf("did not expect uninstalled tool, got %#v", got)
	}
	if len(fake.calls) != 6 {
		t.Fatalf("expected current list plus two desired probes, got %#v", fake.calls)
	}
}

type fakeRunner struct {
	result  runner.Result
	results map[string]runner.Result
	calls   [][]string
}

func (fake *fakeRunner) LookPath(name string) (string, error) {
	return "/fake/" + name, nil
}

func (fake *fakeRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	call := append([]string{name}, args...)
	fake.calls = append(fake.calls, call)
	if fake.results != nil {
		if result, ok := fake.results[strings.Join(call, "\x00")]; ok {
			return result
		}
	}
	return fake.result
}
