package updevconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTOMLWithErrorReportsScannerError(t *testing.T) {
	_, err := ParseTOMLWithError("[ui]\nlanguage = \"" + strings.Repeat("x", 70_000) + "\"\n")
	if err == nil {
		t.Fatal("expected scanner error for oversized config token")
	}
}

func TestParseTOMLKeepsCompatibilityWrapper(t *testing.T) {
	config := ParseTOML("[ui]\nlanguage = \"ja\"\n")
	if config.UI.Language == nil || *config.UI.Language != "ja" {
		t.Fatalf("unexpected parsed language: %#v", config.UI.Language)
	}
}

func TestParseTOMLReadsChezmoiBrewfileHookMode(t *testing.T) {
	config := ParseTOML("[chezmoi_hooks.brewfile]\nmode = \"apply-safe\"\n")
	if config.ChezmoiHooks.Brewfile.Mode == nil || *config.ChezmoiHooks.Brewfile.Mode != "apply-safe" {
		t.Fatalf("unexpected hook mode: %#v", config.ChezmoiHooks.Brewfile.Mode)
	}
	if !ValidChezmoiBrewfileHookMode("warn") || !ValidChezmoiBrewfileHookMode("off") || !ValidChezmoiBrewfileHookMode("apply-safe") {
		t.Fatal("expected documented hook modes to be valid")
	}
	if ValidChezmoiBrewfileHookMode("bundle") {
		t.Fatal("did not expect bundle mode to be valid")
	}
}

func TestParseTOMLReadsBackendHomebrewOwnershipExceptions(t *testing.T) {
	config := ParseTOML(`[backends]
preference_order = ["mise/aqua", "mise/github"]
keep_homebrew = ["brew/chezmoi", "cask/example-app"]
`)
	if len(config.Backends.PreferenceOrder) != 2 || config.Backends.PreferenceOrder[0] != "mise/aqua" {
		t.Fatalf("unexpected backend preference order: %#v", config.Backends.PreferenceOrder)
	}
	if len(config.Backends.KeepHomebrew) != 2 || config.Backends.KeepHomebrew[0] != "brew/chezmoi" || config.Backends.KeepHomebrew[1] != "cask/example-app" {
		t.Fatalf("unexpected Homebrew ownership exceptions: %#v", config.Backends.KeepHomebrew)
	}
}

func TestPackageMetadataPathUsesDefaultAndConfigRelativeOverride(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("UPDEV_CONFIG", "")

	if got, want := PackageMetadataPath(Config{}), filepath.Join(configHome, "updev", "package-metadata.toml"); got != want {
		t.Fatalf("expected default package metadata path %q, got %q", want, got)
	}

	standaloneConfig := filepath.Join(t.TempDir(), "standalone", "updev.toml")
	t.Setenv("UPDEV_CONFIG", standaloneConfig)
	config := ParseTOML("[mise_bootstrap]\npackage_metadata = \"metadata/packages.toml\"\n")
	if config.MiseBootstrap.PackageMetadata == nil || *config.MiseBootstrap.PackageMetadata != "metadata/packages.toml" {
		t.Fatalf("unexpected package metadata config: %#v", config.MiseBootstrap.PackageMetadata)
	}
	want := filepath.Join(filepath.Dir(standaloneConfig), "metadata", "packages.toml")
	if got := PackageMetadataPath(config); got != want {
		t.Fatalf("expected config-relative package metadata path %q, got %q", want, got)
	}
}
