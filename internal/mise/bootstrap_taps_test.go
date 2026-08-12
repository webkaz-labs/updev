package mise

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapTapsFromSourcesUsesOnlyResolvedSources(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "config.toml")
	scope := filepath.Join(root, "config.scope-alpha.toml")
	if err := os.WriteFile(base, []byte(`[bootstrap.brew.taps]
"example/tools" = "https://github.com/example/homebrew-tools.git"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scope, []byte(`[bootstrap.brew.taps]
"example/tools" = "https://mirror.invalid/example/tools.git"
"second/tap" = "https://github.com/second/homebrew-tap.git"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	taps, err := BootstrapTapsFromSources([]ConfigSource{{Path: base, ReportedOrder: 1}, {Path: scope, ReportedOrder: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(taps) != 2 || taps[0].Identity != "brew-tap:example/tools" || taps[1].Identity != "brew-tap:second/tap" {
		t.Fatalf("unexpected deterministic taps: %#v", taps)
	}
	if taps[0].URL != "https://github.com/example/homebrew-tools.git" {
		t.Fatalf("expected first reported source to preserve resolved tap URL, got %#v", taps[0])
	}
	if len(taps[0].Sources) != 2 || taps[0].Sources[0] != base || taps[0].Sources[1] != scope {
		t.Fatalf("expected all active tap sources, got %#v", taps[0].Sources)
	}
}

func TestBootstrapTapsFromSourcesRejectsInvalidSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mise.toml")
	if err := os.WriteFile(path, []byte("[bootstrap.brew.taps\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BootstrapTapsFromSources([]ConfigSource{{Path: path}}); err == nil {
		t.Fatal("expected invalid TOML to fail")
	}
}
