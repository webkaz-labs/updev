package manualinventory

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/webkaz-labs/updev/internal/runner"
)

type fakeProviderScanRunner struct {
	results map[string]runner.Result
	missing map[string]bool
}

func (f fakeProviderScanRunner) LookPath(name string) (string, error) {
	if f.missing[name] {
		return "", errors.New("missing")
	}
	return "/bin/" + name, nil
}

func (f fakeProviderScanRunner) Run(ctx context.Context, name string, args ...string) runner.Result {
	key := name
	for _, arg := range args {
		key += "\x00" + arg
	}
	return f.results[key]
}

func TestLiveCaskInventoryItemsParsesBrewList(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Homebrew cask live inventory is macOS-only")
	}
	items := LiveCaskInventoryItems("/repo", true, fakeProviderScanRunner{results: map[string]runner.Result{
		"brew\x00list\x00--cask\x00-1": {Stdout: "visual-studio-code\nraycast\n"},
	}})
	if len(items) != 2 || items[0].Name != "visual-studio-code" || items[0].Provider != "brew" || items[0].Kind != "cask" || !items[0].Live {
		t.Fatalf("unexpected cask inventory items: %#v", items)
	}
}

func TestInstalledMASAppsParsesMasList(t *testing.T) {
	apps := InstalledMASApps("/repo", "/repo", fakeProviderScanRunner{results: map[string]runner.Result{
		"mas\x00list": {Stdout: "434290957 Motion (6.2)\n"},
	}})
	if len(apps) != 1 || apps[0].ID != "434290957" || apps[0].Name != "Motion" || apps[0].Version != "6.2" {
		t.Fatalf("unexpected MAS apps: %#v", apps)
	}
}

func TestInstalledMASAppsSkipsNonDefaultRoot(t *testing.T) {
	apps := InstalledMASApps("/repo/worktree", "/repo", fakeProviderScanRunner{results: map[string]runner.Result{
		"mas\x00list": {Stdout: "434290957 Motion (6.2)\n"},
	}})
	if len(apps) != 0 {
		t.Fatalf("expected non-default root to skip MAS live scan, got %#v", apps)
	}
}
