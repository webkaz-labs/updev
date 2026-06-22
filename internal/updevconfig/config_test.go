package updevconfig

import (
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
