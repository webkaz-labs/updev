package packageparity

import (
	"testing"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
)

func TestBuildComparesFormulaCaskAndTapDesiredState(t *testing.T) {
	brewfileItems := []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "jq"},
		{Provider: "brew", Kind: "cask", Name: "firefox"},
		{Provider: "brew", Kind: "tap", Name: "example/tools"},
		{Provider: "brew", Kind: "brew", Name: "brewfile-only"},
		{Provider: "brew", Kind: "vscode", Name: "publisher.extension"},
	}
	set := mise.BootstrapPackageSet{
		Sources: []mise.ConfigSource{{Path: "/config/mise.toml", ReportedOrder: 1}},
		Packages: []mise.BootstrapPackageDesired{
			{Identity: "brew:jq", Manager: "brew", Name: "jq", RequestedVersion: "latest", ManagerAvailable: false, UnavailableReason: "unsupported architecture"},
			{Identity: "brew-cask:firefox", Manager: "brew-cask", Name: "firefox", RequestedVersion: "latest", ManagerAvailable: true},
			{Identity: "brew:mise-only", Manager: "brew", Name: "mise-only", RequestedVersion: "latest", ManagerAvailable: true},
			{Identity: "apt:curl", Manager: "apt", Name: "curl", RequestedVersion: "latest", ManagerAvailable: false},
		},
	}
	taps := []mise.BootstrapTapDesired{{Identity: "brew-tap:example/tools", Name: "example/tools"}}

	report, err := Build("/repo", "/home/example/Brewfile", brewfileItems, set, taps)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != plan.StatusDrift || report.Summary.Matched != 3 || report.Summary.BrewfileOnly != 1 || report.Summary.MiseOnly != 1 {
		t.Fatalf("unexpected parity summary: %#v", report)
	}
	if report.IgnoredMiseManagers["apt"] != 1 {
		t.Fatalf("expected out-of-scope apt count, got %#v", report.IgnoredMiseManagers)
	}
	if len(report.Items) != 5 || report.Items[0].Identity != "brew/cask/firefox" || report.Items[4].Identity != "brew/tap/example/tools" {
		t.Fatalf("unexpected deterministic items: %#v", report.Items)
	}
	for _, item := range report.Items {
		if item.Name == "jq" && (item.ManagerAvailable == nil || *item.ManagerAvailable || item.UnavailableReason != "unsupported architecture") {
			t.Fatalf("expected manager availability evidence, got %#v", item)
		}
	}
}

func TestBuildDerivesImplicitTapFromQualifiedMisePackage(t *testing.T) {
	set := mise.BootstrapPackageSet{Packages: []mise.BootstrapPackageDesired{{
		Identity:         "brew:owner/tools/example",
		Manager:          "brew",
		Name:             "owner/tools/example",
		RequestedVersion: "latest",
		ManagerAvailable: true,
	}}}
	report, err := Build("/repo", "/repo/Brewfile", []plan.Item{
		{Provider: "brew", Kind: "brew", Name: "example"},
		{Provider: "brew", Kind: "tap", Name: "owner/tools"},
	}, set, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != plan.StatusOK || report.Summary.Matched != 2 {
		t.Fatalf("expected qualified package and implicit tap parity, got %#v", report)
	}
}

func TestBuildRejectsDuplicateNormalizedMisePackages(t *testing.T) {
	set := mise.BootstrapPackageSet{Packages: []mise.BootstrapPackageDesired{
		{Identity: "brew:jq", Manager: "brew", Name: "jq", RequestedVersion: "latest", ManagerAvailable: true},
		{Identity: "brew:owner/tools/jq", Manager: "brew", Name: "owner/tools/jq", RequestedVersion: "latest", ManagerAvailable: true},
	}}
	if _, err := Build("/repo", "/repo/Brewfile", nil, set, nil); err == nil {
		t.Fatal("expected duplicate normalized identity error")
	}
}
