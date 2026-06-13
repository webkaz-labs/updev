package mise

import (
	"reflect"
	"testing"
)

func TestUpgradeCommandUsesScopedToolsAndReleaseAge(t *testing.T) {
	got := UpgradeCommand("/repo", []string{"node", " go ", "node"}, "3d")
	want := []string{"mise", "upgrade", "--yes", "--minimum-release-age", "3d", "--cd", "/repo", "go", "node"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpgradeCommand = %#v, want %#v", got, want)
	}
}

func TestBumpCommandCanBypassNativeMinimumReleaseAge(t *testing.T) {
	got := BumpCommand("/repo", []string{"github:owner/tool"}, true, false, true)
	want := []string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--dry-run", "--bump", "--cd", "/repo", "github:owner/tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BumpCommand = %#v, want %#v", got, want)
	}
}

func TestBumpApplyCommands(t *testing.T) {
	got := BumpApplyCommands("/repo", []string{"github:owner/tool"}, false)
	want := [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", "/repo", "github:owner/tool"},
		{"mise", "upgrade", "--bump", "--yes", "--cd", "/repo", "github:owner/tool"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BumpApplyCommands = %#v, want %#v", got, want)
	}
}

func TestUpgradeAllCommands(t *testing.T) {
	got := UpgradeAllCommands()
	want := [][]string{
		{"mise", "upgrade"},
		{"mise", "prune"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpgradeAllCommands = %#v, want %#v", got, want)
	}
}
