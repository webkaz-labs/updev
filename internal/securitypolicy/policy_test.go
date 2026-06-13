package securitypolicy

import (
	"path/filepath"
	"testing"
)

func TestReadWritePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security-policy.json")
	policy := Policy{Rules: []Rule{{
		Provider: "brew",
		Kind:     "cask",
		Name:     "sample",
		Decision: "allow",
		Reason:   "reviewed",
		Expires:  "2026-06-30",
	}}}
	if err := Write(path, policy); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected one rule, got %#v", got)
	}
	rule := got.Rules[0]
	if rule.Provider != "brew" || rule.Kind != "cask" || rule.Name != "sample" || rule.Decision != "allow" || rule.Line == 0 {
		t.Fatalf("unexpected rule after read: %#v", rule)
	}
}

func TestReadPolicyMissingFile(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 0 {
		t.Fatalf("expected empty policy for missing file, got %#v", got)
	}
}

func TestRuleLineNumbers(t *testing.T) {
	data := []byte(`{
  "rules": [
    {"provider":"brew","name":"a","decision":"allow"},
    {"provider":"mise","name":"b","decision":"hold"}
  ]
}`)
	got := RuleLineNumbers(data)
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("unexpected line numbers: %#v", got)
	}
}

func TestRuleViewsClassifyRules(t *testing.T) {
	policy := Policy{Rules: []Rule{
		{Provider: "brew", Kind: "cask", Name: "*", Decision: "allow", Reason: "reviewed", Expires: "2099-01-01"},
		{Provider: "brew", Kind: "cask", Name: "sample", Decision: "allow", Reason: "reviewed", Expires: "2099-01-01"},
		{Provider: "brew", Kind: "cask", Name: "*", Decision: "allow", Reason: "reviewed", Expires: "2099-01-01"},
		{Provider: "mise", Name: "tool", Decision: "allow"},
		{Provider: "npm", Name: "pkg", Decision: "wat"},
	}}
	views := RuleViews(policy)
	if len(views) != 5 {
		t.Fatalf("expected 5 views, got %#v", views)
	}
	if !views[0].Active || !views[0].Broad {
		t.Fatalf("expected first broad active rule, got %#v", views[0])
	}
	if !views[1].Shadowed || views[1].ShadowedBy != 1 {
		t.Fatalf("expected second rule to be shadowed by broad rule, got %#v", views[1])
	}
	if !views[2].Duplicate {
		t.Fatalf("expected third rule to be duplicate, got %#v", views[2])
	}
	if !views[3].MissingReason || !views[3].MissingExpiry {
		t.Fatalf("expected fourth rule to need reason and expiry, got %#v", views[3])
	}
	if !views[4].Invalid {
		t.Fatalf("expected invalid decision rule, got %#v", views[4])
	}
	counts := RuleCountsForViews(views)
	if counts.Active != 2 || counts.Shadowed != 1 || counts.Duplicate != 1 || counts.Invalid != 1 || counts.Broad != 1 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}

func TestNormalizeRule(t *testing.T) {
	got := NormalizeRule(Rule{
		Provider: " brew ",
		Kind:     " cask ",
		Name:     " sample ",
		Decision: " DENY ",
		Reason:   " reviewed ",
		Expires:  " 2099-01-01 ",
	})
	if got.Provider != "brew" || got.Kind != "cask" || got.Name != "sample" || got.Decision != "block" || got.Reason != "reviewed" || got.Expires != "2099-01-01" {
		t.Fatalf("unexpected normalized rule: %#v", got)
	}
}

func TestMatchingRule(t *testing.T) {
	policy := Policy{Rules: []Rule{
		{Provider: "brew", Kind: "cask", Name: "expired", Decision: "allow", Expires: "2000-01-01"},
		{Provider: "brew", Kind: "cask", Name: "invalid", Decision: "wat"},
		{Provider: "brew", Kind: "cask", Name: "*", Decision: "DENY", Reason: "blocked"},
	}}
	rule, ok := MatchingRule(policy, "brew", "cask", "sample")
	if !ok {
		t.Fatal("expected wildcard matching rule")
	}
	if rule.Decision != "block" || rule.Reason != "blocked" {
		t.Fatalf("unexpected matching rule: %#v", rule)
	}
	if _, ok := MatchingRule(policy, "brew", "cask", "expired"); !ok {
		t.Fatal("expected wildcard to still match expired specific rule name")
	}
	if _, ok := MatchingRule(policy, "brew", "tap", "sample"); ok {
		t.Fatal("did not expect kind mismatch to match")
	}
}

func TestProviderForEcosystem(t *testing.T) {
	tests := map[string]string{
		"npm":       "npm",
		"crates.io": "cargo",
		"PyPI":      "pypi",
		"Go":        "Go",
	}
	for ecosystem, want := range tests {
		if got := ProviderForEcosystem(ecosystem); got != want {
			t.Fatalf("ProviderForEcosystem(%q) = %q, want %q", ecosystem, got, want)
		}
	}
}
