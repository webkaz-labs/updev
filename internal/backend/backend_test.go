package backend

import (
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func TestPreferenceTiersWithOrderKeepsConfiguredOrderAndDefaults(t *testing.T) {
	tiers := PreferenceTiersWithOrder([]string{"store/native", "mise/github", "linux/apt"})
	if len(tiers) < 5 {
		t.Fatalf("expected configured and default tiers, got %#v", tiers)
	}
	for index, tier := range tiers {
		if tier.Rank != index+1 {
			t.Fatalf("expected ranks to be recomputed, got %#v", tiers)
		}
	}
	if tiers[0].Label != "store/native" || tiers[0].Provider != "mas" {
		t.Fatalf("expected store/native first with default provider mapping, got %#v", tiers[0])
	}
	if tiers[1].Label != "mise/github" || tiers[1].Provider != "mise" || tiers[1].Backend != "github" {
		t.Fatalf("expected mise/github second, got %#v", tiers[1])
	}
	if tiers[2].Label != "linux/apt" || tiers[2].Provider != "linux" || tiers[2].Backend != "apt" {
		t.Fatalf("expected unknown provider/backend label to be preserved, got %#v", tiers[2])
	}
	if tiers[3].Label != "mise/core" {
		t.Fatalf("expected unspecified defaults to remain after configured tiers, got %#v", tiers[:5])
	}
}

func TestRegistryPreferenceRecommendationUsesDataRules(t *testing.T) {
	registry := Registry{PreferenceOrder: []string{"mise/aqua", "mise/github"}}
	recommendation, ok := registry.PreferenceRecommendation("mise", "cargo:git-delta")
	if !ok {
		t.Fatal("expected cargo git-delta rule")
	}
	if recommendation.Name != "aqua:dandavison/delta" || recommendation.Tier != "mise/aqua" {
		t.Fatalf("unexpected recommendation: %#v", recommendation)
	}
	if !recommendation.RewriteAllowed {
		t.Fatalf("expected mise recommendations to be rewrite-capable: %#v", recommendation)
	}
	if len(recommendation.SourceEvidence) == 0 {
		t.Fatalf("expected source evidence: %#v", recommendation)
	}
}

func TestPreferenceRulesCarrySourceEvidence(t *testing.T) {
	for _, rule := range PreferenceRules() {
		if len(rule.SourceEvidence) == 0 {
			t.Fatalf("expected source evidence for %s/%s -> %s/%s", rule.SourceProvider, rule.SourceName, rule.RecommendedProvider, rule.RecommendedName)
		}
	}
}

func TestPreferenceRuleEntriesAreComplete(t *testing.T) {
	if len(preferenceRuleEntries) == 0 {
		t.Fatal("expected backend preference registry entries")
	}
	seen := map[string]bool{}
	for _, entry := range preferenceRuleEntries {
		key := entry.SourceProvider + "/" + entry.SourceName
		if seen[key] {
			t.Fatalf("duplicate backend preference entry: %s", key)
		}
		seen[key] = true
		if entry.SourceProvider == "" || entry.SourceName == "" || entry.RecommendedName == "" || entry.Reason == "" {
			t.Fatalf("incomplete backend preference entry: %#v", entry)
		}
		if len(entry.Commands) == 0 {
			t.Fatalf("expected command evidence for %s", key)
		}
		if len(entry.SourceEvidence) == 0 {
			t.Fatalf("expected source evidence for %s", key)
		}
	}
}

func TestFindingIncludesRecommendationSourceEvidence(t *testing.T) {
	registry := Registry{}
	recommendation, ok := registry.PreferenceRecommendation("brew", "fd")
	if !ok {
		t.Fatal("expected fd recommendation")
	}
	finding := backendHomebrewFinding(plan.Item{Kind: "brew", Name: "fd"}, recommendation, nil, runner.Local{})
	if len(finding.SourceEvidence) == 0 {
		t.Fatalf("expected finding source evidence: %#v", finding)
	}
}

func TestDeprecatedTiersAreRecognizedButNotDefault(t *testing.T) {
	for _, tier := range PreferenceTiersWithOrder(nil) {
		switch tier.Label {
		case "mise/ubi", "mise/vfox", "mise/asdf":
			t.Fatalf("expected deprecated backend to stay out of defaults, got %#v", tier)
		}
	}
	ubi := (Registry{}).PreferenceTierFor("mise", "ubi:owner/repo")
	if ubi.Label != "mise/ubi" || ubi.Rank != 90 {
		t.Fatalf("expected existing ubi backend to remain recognized as deprecated, got %#v", ubi)
	}
	configured := PreferenceTiersWithOrder([]string{"mise/asdf"})
	if configured[0].Label != "mise/asdf" || configured[0].Rank != 1 {
		t.Fatalf("expected explicit deprecated backend override to be honored, got %#v", configured[0])
	}
}
