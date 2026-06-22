package brew

import (
	"reflect"
	"testing"
)

func TestUpgradeGreedyNoAutoUpdateCommandCleansTargets(t *testing.T) {
	got := UpgradeGreedyNoAutoUpdateCommand([]string{" jq ", "", "git", "jq"})
	want := []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "git", "jq"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpgradeGreedyNoAutoUpdateCommand = %#v, want %#v", got, want)
	}
}

func TestUpgradeGreedyNoAutoUpdateCommands(t *testing.T) {
	got := UpgradeGreedyNoAutoUpdateCommands([]string{"jq"})
	want := [][]string{
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "jq"},
		{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "cleanup"},
		{"brew", "update"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpgradeGreedyNoAutoUpdateCommands = %#v, want %#v", got, want)
	}
}

func TestUpgradeGreedyCommands(t *testing.T) {
	got := UpgradeGreedyCommands()
	want := [][]string{
		{"brew", "update"},
		{"brew", "upgrade", "--greedy"},
		{"brew", "cleanup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpgradeGreedyCommands = %#v, want %#v", got, want)
	}
}

func TestTrustJSONCommandUsesLocalMetadata(t *testing.T) {
	got := TrustJSONCommand()
	want := []string{"env", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "trust", "--json=v1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrustJSONCommand = %#v, want %#v", got, want)
	}
}

func TestInstallCommandIsItemScopedAndDisablesAutoUpdate(t *testing.T) {
	tests := []struct {
		kind string
		name string
		want []string
	}{
		{kind: "brew", name: "jq", want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "jq"}},
		{kind: "cask", name: "firefox", want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "--cask", "firefox"}},
		{kind: "tap", name: "webkaz/tap", want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "tap", "webkaz/tap"}},
	}
	for _, tt := range tests {
		got := InstallCommand(tt.kind, tt.name)
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("InstallCommand(%q, %q) = %#v, want %#v", tt.kind, tt.name, got, tt.want)
		}
	}
	if got := InstallCommand("vscode", "x"); got != nil {
		t.Fatalf("expected unsupported kind to return nil, got %#v", got)
	}
}
