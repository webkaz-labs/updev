package packageapply

import (
	"reflect"
	"testing"

	"github.com/webkaz-labs/updev/internal/packageexecutor"
)

func TestInstallCommandUsesExactSelectedExecutor(t *testing.T) {
	tests := []struct {
		name string
		root string
		item packageexecutor.Item
		want []string
	}{
		{
			name: "native formula",
			item: packageexecutor.Item{Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq", Executor: packageexecutor.ExecutorNative},
			want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "jq"},
		},
		{
			name: "native cask",
			item: packageexecutor.Item{Identity: "brew/cask/firefox", Provider: "brew", Kind: "cask", Name: "firefox", Executor: packageexecutor.ExecutorNative},
			want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "install", "--cask", "firefox"},
		},
		{
			name: "native tap",
			item: packageexecutor.Item{Identity: "brew/tap/example/tools", Provider: "brew", Kind: "tap", Name: "example/tools", Executor: packageexecutor.ExecutorNative},
			want: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "tap", "example/tools"},
		},
		{
			name: "mise explicit package",
			root: "/repo",
			item: packageexecutor.Item{Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq", Manager: "brew", ManagerPackage: "homebrew/core/jq", Executor: packageexecutor.ExecutorMise},
			want: []string{"mise", "bootstrap", "packages", "apply", "--yes", "--cd", "/repo", "brew:homebrew/core/jq"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InstallCommand(tt.root, tt.item)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("InstallCommand() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstallCommandFailsClosed(t *testing.T) {
	tests := []struct {
		root string
		item packageexecutor.Item
	}{
		{root: "/repo", item: packageexecutor.Item{Identity: "apt/package/curl", Provider: "apt", Kind: "package", Name: "curl", Executor: packageexecutor.ExecutorNative}},
		{root: "/repo", item: packageexecutor.Item{Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq", Executor: packageexecutor.ExecutorMise}},
		{root: "/repo", item: packageexecutor.Item{Identity: "brew/tap/example/tools", Provider: "brew", Kind: "tap", Name: "example/tools", Manager: "brew-tap", ManagerPackage: "example/tools", Executor: packageexecutor.ExecutorMise}},
		{root: " /repo", item: packageexecutor.Item{Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq", Manager: "brew", ManagerPackage: "jq", Executor: packageexecutor.ExecutorMise}},
		{root: "/repo", item: packageexecutor.Item{Identity: "brew/formula/jq", Provider: "brew", Kind: "formula", Name: "jq", Executor: packageexecutor.ExecutorUnsupported}},
	}
	for _, tt := range tests {
		if command, err := InstallCommand(tt.root, tt.item); err == nil || command != nil {
			t.Fatalf("expected package %s to fail closed, command=%#v err=%v", tt.item.Identity, command, err)
		}
	}
}
