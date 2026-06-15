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
