package brew

import (
	"strings"
	"testing"
)

func TestParseManifestEntries(t *testing.T) {
	manifest, err := ParseManifest(strings.NewReader(`
tap "custom/tap"
brew "jq"
cask "custom/tap/app"
cask "https://example.com/custom.rb"
vscode "publisher.extension"
`), "/tmp/Brewfile")
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if entry := manifest.Entry("brew", "jq"); entry.RawName != "jq" || entry.Tap != "" || entry.URLBased {
		t.Fatalf("unexpected brew entry: %#v", entry)
	}
	if entry := manifest.Entry("cask", "app"); entry.RawName != "custom/tap/app" || entry.Tap != "custom/tap" || entry.URLBased {
		t.Fatalf("unexpected cask tap entry: %#v", entry)
	}
	if entry := manifest.Entry("cask", "https://example.com/custom.rb"); !entry.URLBased || entry.RawName != "https://example.com/custom.rb" {
		t.Fatalf("unexpected URL cask entry: %#v", entry)
	}
	if entries := manifest.Entries(); len(entries) != 3 {
		t.Fatalf("expected three brew/cask entries, got %#v", entries)
	}
}

func TestManifestNameHelpers(t *testing.T) {
	if got := NormalizePackageName("cask", "custom/tap/app"); got != "app" {
		t.Fatalf("NormalizePackageName() = %q, want app", got)
	}
	if got := TapName("brew", "custom/tap/tool"); got != "custom/tap" {
		t.Fatalf("TapName() = %q, want custom/tap", got)
	}
	if !IsURLName("https://example.com/app.rb") || IsURLName("custom/tap/app") {
		t.Fatal("unexpected URL name detection")
	}
	if !IsOfficialTap("homebrew/custom") || IsOfficialTap("custom/tap") {
		t.Fatal("unexpected official tap detection")
	}
}
