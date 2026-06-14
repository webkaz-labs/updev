package plan

import "testing"

func TestEvidenceUpdateItemKeysMapsProviderAliases(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		item             string
		miseBumpProvider string
		want             []string
	}{
		{
			name:     "brew cask",
			provider: "brew",
			item:     "cask Demo.app",
			want: []string{
				"brew/demo.app",
				"brew/brew/demo.app",
				"brew/cask/demo.app",
				"brew/tap/demo.app",
			},
		},
		{
			name:     "brew cask version delta",
			provider: "brew",
			item:     "cask Demo.app 1.0.0 -> 1.1.0",
			want: []string{
				"brew/demo.app",
				"brew/brew/demo.app",
				"brew/cask/demo.app",
				"brew/tap/demo.app",
			},
		},
		{
			name:             "mise bump provider maps to mise tool",
			provider:         "mise-bump",
			item:             "github:owner/tool 1.0.0 -> 1.1.0",
			miseBumpProvider: "mise-bump",
			want: []string{
				"mise-bump/github:owner/tool",
				"mise/tool/github:owner/tool",
				"mise/github:owner/tool",
				"github:owner/tool",
				"mise-bump/tool",
				"mise/tool/tool",
				"mise/tool",
				"tool",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvidenceUpdateItemKeys(tt.provider, tt.item, tt.miseBumpProvider)
			assertStringSet(t, got, tt.want)
		})
	}
}

func TestItemEvidenceForUsesExactProviderAndNameFallbacks(t *testing.T) {
	index := NewEvidenceIndex()
	AddEvidence(index.Updates, EvidenceExactKey("brew", "cask", "demo"), "brew updated: demo 1.0 -> 1.1")
	AddEvidence(index.Security, EvidenceProviderNameKey("brew", "demo"), "brew/cask demo: hold")
	AddEvidence(index.Backends, EvidenceNameKey("demo"), "backend convergence review")

	got := ItemEvidenceFor(Item{Provider: "brew", Kind: "cask", Name: "demo"}, index)
	assertStringSet(t, got.Updates, []string{"brew updated: demo 1.0 -> 1.1"})
	assertStringSet(t, got.Security, []string{"brew/cask demo: hold"})
	assertStringSet(t, got.Backends, []string{"backend convergence review"})
}

func TestEvidenceCountsDeduplicatesValues(t *testing.T) {
	index := NewEvidenceIndex()
	AddEvidence(index.Updates, EvidenceNameKey("demo"), "updated")
	AddEvidence(index.Updates, EvidenceExactKey("brew", "cask", "demo"), "updated")
	AddEvidence(index.Security, EvidenceNameKey("demo"), "held")

	updates, security, backends := EvidenceCounts(index)
	if updates != 1 || security != 1 || backends != 0 {
		t.Fatalf("EvidenceCounts = (%d, %d, %d), want (1, 1, 0)", updates, security, backends)
	}
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d\ngot=%#v\nwant=%#v", len(got), len(want), got, want)
	}
	seen := map[string]bool{}
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			t.Fatalf("missing %q\ngot=%#v\nwant=%#v", value, got, want)
		}
	}
}
