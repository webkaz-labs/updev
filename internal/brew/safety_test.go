package brew

import (
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestSafetyFindingsFromOutdatedUsesManifestProvenance(t *testing.T) {
	manifest, err := ParseManifest(strings.NewReader(`
brew "jq"
cask "muxy-app/tap/muxy"
cask "https://example.com/custom-app.rb"
`), "/tmp/Brewfile")
	if err != nil {
		t.Fatal(err)
	}
	report := OutdatedReport{
		Formulae: []OutdatedItem{{Name: "jq", InstalledVersions: []string{"1.7"}, CurrentVersion: "1.8.1"}},
		Casks:    []OutdatedItem{{Name: "muxy", InstalledVersions: []string{"1.0"}, CurrentVersion: "1.1"}},
	}

	findings := SafetyFindingsFromOutdated(report, manifest)
	if len(findings) != 2 {
		t.Fatalf("expected two findings, got %#v", findings)
	}
	if findings[0].Name != "jq" || findings[0].Decision != "unknown" {
		t.Fatalf("expected core formula to remain release-age unknown, got %#v", findings[0])
	}
	if findings[0].ReasonCode != securityreason.HomebrewEvidenceUnavailable {
		t.Fatalf("expected structured missing evidence reason code, got %#v", findings[0])
	}
	if findings[1].Name != "muxy" || findings[1].Decision != "review" || findings[1].Tap != "muxy-app/tap" {
		t.Fatalf("expected custom tap cask review finding, got %#v", findings[1])
	}
	if findings[1].ReasonCode != securityreason.HomebrewNonOfficialTap {
		t.Fatalf("expected structured custom tap reason code, got %#v", findings[1])
	}
	if findings[1].TrustTarget != "muxy-app/tap/muxy" || findings[1].TrustCommand != "brew trust --cask muxy-app/tap/muxy" {
		t.Fatalf("expected item-scoped Homebrew trust metadata, got %#v", findings[1])
	}
	if got := strings.Join(findings[1].TrustCommandArgv, "\x00"); got != "brew\x00trust\x00--cask\x00muxy-app/tap/muxy" {
		t.Fatalf("expected structured Homebrew trust argv, got %#v", findings[1].TrustCommandArgv)
	}
}

func TestManifestWarningsReportsURLBasedEntries(t *testing.T) {
	manifest, err := ParseManifest(strings.NewReader(`
cask "https://example.com/custom-app.rb"
`), "/tmp/Brewfile")
	if err != nil {
		t.Fatal(err)
	}

	warnings := ManifestWarnings(manifest)
	if len(warnings) != 1 || warnings[0].Name != "https://example.com/custom-app.rb" || warnings[0].Decision != "review" {
		t.Fatalf("expected URL cask manifest warning, got %#v", warnings)
	}
	if warnings[0].ReasonCode != securityreason.HomebrewURLBasedCask {
		t.Fatalf("expected structured URL cask reason code, got %#v", warnings[0])
	}
	if !strings.Contains(warnings[0].Remediation, "cask source URL") {
		t.Fatalf("expected URL cask remediation, got %#v", warnings[0])
	}
}
