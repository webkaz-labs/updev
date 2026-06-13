package brew

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestPosturesFromItemsReportsMetadataRisks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/formula/jq.json":
			_, _ = w.Write([]byte(`{
  "name": "jq",
  "tap": "homebrew/core",
  "homepage": "https://jqlang.github.io/jq/",
  "versions": {"stable": "1.8.1"},
  "urls": {"stable": {"url": "https://github.com/jqlang/jq/releases/download/jq-1.8.1/jq-1.8.1.tar.gz"}},
  "deprecated": true,
  "deprecation_reason": "use yq"
}`))
		case "/cask/visual-studio-code.json":
			_, _ = w.Write([]byte(`{
  "token": "visual-studio-code",
  "tap": "homebrew/cask",
  "homepage": "https://code.visualstudio.com/",
  "url": "https://update.code.visualstudio.com/latest/darwin/stable",
  "version": "1.101.0"
}`))
		default:
			t.Fatalf("unexpected Homebrew API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	items := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq"},
		{Provider: "brew", Kind: "cask", Name: "visual-studio-code"},
		{Provider: "brew", Kind: "cask", Name: "custom-app"},
		{Provider: "brew", Kind: "tap", Name: "vendor/tap"},
		{Provider: "brew", Kind: "tap", Name: "homebrew/core"},
	}
	postures, err := PosturesFromItems(context.Background(), server.Client(), server.URL, items, func(kind string, name string) ManifestEntry {
		if kind == "cask" && name == "custom-app" {
			return ManifestEntry{RawName: "vendor/tap/custom-app", Tap: "vendor/tap"}
		}
		return ManifestEntry{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(postures) != 5 {
		t.Fatalf("expected five Homebrew posture entries, got %#v", postures)
	}
	reasons := map[string]string{}
	reasonCodes := map[string]string{}
	trustCommands := map[string]string{}
	trustArgv := map[string][]string{}
	remediations := map[string]string{}
	for _, posture := range postures {
		reasons[posture.Name] = posture.Reason
		reasonCodes[posture.Name] = posture.ReasonCode
		trustCommands[posture.Name] = posture.TrustCommand
		trustArgv[posture.Name] = posture.TrustCommandArgv
		remediations[posture.Name] = posture.Remediation
	}
	if reasons["jq"] != "use yq" {
		t.Fatalf("expected deprecated formula reason, got %#v", postures)
	}
	if reasons["visual-studio-code"] != "Homebrew cask download host differs from homepage host; vendor provenance review required" {
		t.Fatalf("expected cask review reason, got %#v", postures)
	}
	if reasons["vendor/tap/custom-app"] != "non-official Homebrew tap needs provenance review" {
		t.Fatalf("expected custom tap review reason, got %#v", postures)
	}
	if reasonCodes["jq"] != securityreason.HomebrewEntryDeprecated ||
		reasonCodes["visual-studio-code"] != securityreason.HomebrewCaskHostMismatch ||
		reasonCodes["vendor/tap/custom-app"] != securityreason.HomebrewNonOfficialTap ||
		reasonCodes["vendor/tap"] != securityreason.HomebrewNonOfficialTap {
		t.Fatalf("expected structured Homebrew reason codes, got %#v", postures)
	}
	if trustCommands["vendor/tap/custom-app"] != "brew trust --cask vendor/tap/custom-app" ||
		trustCommands["vendor/tap"] != "brew trust --tap vendor/tap" {
		t.Fatalf("expected Homebrew 6 trust commands on non-official entries, got %#v", postures)
	}
	if strings.Join(trustArgv["vendor/tap/custom-app"], "\x00") != "brew\x00trust\x00--cask\x00vendor/tap/custom-app" ||
		strings.Join(trustArgv["vendor/tap"], "\x00") != "brew\x00trust\x00--tap\x00vendor/tap" {
		t.Fatalf("expected structured Homebrew 6 trust argv on non-official entries, got %#v", postures)
	}
	if !strings.Contains(remediations["visual-studio-code"], "visualstudio.com") ||
		!strings.Contains(remediations["visual-studio-code"], "update.code.visualstudio.com") ||
		!strings.Contains(remediations["vendor/tap"], "tap repository") {
		t.Fatalf("expected Homebrew posture remediation, got %#v", postures)
	}
	if reasons["vendor/tap"] != "non-official Homebrew tap needs provenance review" {
		t.Fatalf("expected custom tap review posture, got %#v", postures)
	}
	if reasons["homebrew/core"] != "" {
		t.Fatalf("expected official tap allow posture, got %#v", postures)
	}
	for _, posture := range postures {
		if posture.Name == "vendor/tap" && posture.URL != "https://github.com/vendor/homebrew-tap" {
			t.Fatalf("expected inferred tap GitHub URL, got %#v", posture)
		}
	}
}

func TestMetadataUnmarshalAcceptsStringOrArrayName(t *testing.T) {
	for _, input := range []string{`{"name":"jq"}`, `{"name":["jq"]}`} {
		var metadata Metadata
		if err := json.Unmarshal([]byte(input), &metadata); err != nil {
			t.Fatal(err)
		}
		if len(metadata.Name) != 1 || metadata.Name[0] != "jq" {
			t.Fatalf("expected name jq from %s, got %#v", input, metadata.Name)
		}
	}
}
