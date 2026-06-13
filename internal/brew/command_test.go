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
