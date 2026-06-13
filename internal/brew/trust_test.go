package brew

import (
	"strings"
	"testing"
)

func TestParseTrustTargetsKeepsItemScopedCommands(t *testing.T) {
	targets, err := ParseTrustTargets(strings.NewReader(`
tap "vendor/tap"
brew "vendor/tap/tool"
cask "vendor/tap/app"
brew "git"
brew "https://example.com/tool.rb"
`), "Brewfile.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected three non-official trust targets, got %#v", targets)
	}
	for _, target := range targets {
		if target.TrustCommand == "" || len(target.TrustCommandArgv) == 0 {
			t.Fatalf("expected target to keep trust command evidence, got %#v", target)
		}
		if target.Source != "Brewfile.tmpl" {
			t.Fatalf("expected source evidence, got %#v", target)
		}
	}
}

func TestApplyTrustStateTreatsWholeTapAsTrusted(t *testing.T) {
	targets, err := ParseTrustTargets(strings.NewReader(`
brew "vendor/tap/tool"
cask "vendor/tap/app"
`), "Brewfile.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	targets = ApplyTrustState(targets, TrustState{Taps: []string{"vendor/tap"}})
	trusted, untrusted := TrustTargetCounts(targets)
	if trusted != 2 || untrusted != 0 {
		t.Fatalf("expected whole tap trust to cover formula and cask targets, got trusted=%d untrusted=%d targets=%#v", trusted, untrusted, targets)
	}
	for _, target := range targets {
		if target.TrustSource != "tap" {
			t.Fatalf("expected tap trust source, got %#v", targets)
		}
	}
}
