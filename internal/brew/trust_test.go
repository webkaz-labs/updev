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

func TestTrustGroupsByTapSummarizesUntrustedTargets(t *testing.T) {
	targets, err := ParseTrustTargets(strings.NewReader(`
tap "vendor/tap"
brew "vendor/tap/tool"
cask "vendor/tap/app"
cask "other/tap/app"
`), "Brewfile.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	targets = ApplyTrustState(targets, TrustState{Formulae: []string{"vendor/tap/tool"}, Casks: []string{"other/tap/app"}})
	groups := TrustGroupsByTap(targets)
	if len(groups) != 2 {
		t.Fatalf("expected two tap groups, got %#v", groups)
	}
	if groups[0].Tap != "other/tap" || !groups[0].Trusted || groups[0].UntrustedCount != 0 {
		t.Fatalf("expected already trusted group to be complete, got %#v", groups[0])
	}
	if groups[1].Tap != "vendor/tap" || groups[1].Trusted || groups[1].UntrustedCount != 2 || groups[1].TrustedCount != 1 {
		t.Fatalf("expected vendor/tap group to summarize partial trust, got %#v", groups[1])
	}
	if groups[1].TrustCommand != "brew trust --tap vendor/tap" {
		t.Fatalf("expected tap-scoped trust command, got %#v", groups[1])
	}
	trusted, untrusted := TrustGroupCounts(groups)
	if trusted != 1 || untrusted != 1 {
		t.Fatalf("expected one trusted and one untrusted group, got trusted=%d untrusted=%d groups=%#v", trusted, untrusted, groups)
	}
	if names := UntrustedGroupNames(groups, 1); len(names) != 1 || !strings.Contains(names[0], "vendor/tap") {
		t.Fatalf("expected untrusted group name, got %#v", names)
	}
	if command := FirstUntrustedTrustCommand(groups); command != "brew trust --tap vendor/tap" {
		t.Fatalf("expected first untrusted tap command, got %q", command)
	}
}
