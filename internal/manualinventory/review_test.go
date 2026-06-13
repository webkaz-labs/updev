package manualinventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAgentDraftsEnrichesMatchedDraft(t *testing.T) {
	candidates := []ReviewCandidate{{
		Name: "Draft Demo",
		SuggestedOverride: ReviewOverrideFields{
			Aliases: []string{"com.example.draft"},
		},
		Evidence: []ReviewEvidence{{
			Scanner:          "macos_app_bundle",
			SourceURL:        "https://example.com/draft",
			Owner:            "Example",
			UpdateOwner:      "vendor",
			ProviderMetadata: "Info.plist",
			BundleID:         "com.example.draft",
		}},
	}}
	drafts, err := ValidateAgentDrafts(`
[[manual.apps]]
name = "Draft Demo"

[manual.apps.identifiers]
bundle_id = "com.example.draft"
`, candidates, []string{"/usr/local/bin/manual-agent"})
	if err != nil {
		t.Fatalf("expected valid draft: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("expected one draft, got %#v", drafts)
	}
	draft := drafts[0]
	if draft.ReviewStatus != "draft" || draft.Provenance["source"] != "agent" || draft.Provenance["command"] != "manual-agent" {
		t.Fatalf("expected draft provenance defaults, got %#v", draft)
	}
	if draft.Provenance["source_url"] != "https://example.com/draft" || draft.Provenance["owner"] != "Example" || draft.Provenance["update_owner"] != "vendor" || draft.Provenance["provider_metadata"] != "Info.plist" {
		t.Fatalf("expected evidence enrichment, got %#v", draft.Provenance)
	}
	if len(draft.Evidence) != 1 || draft.Evidence[0] != "macos_app_bundle" {
		t.Fatalf("expected scanner evidence, got %#v", draft.Evidence)
	}
}

func TestValidateAgentDraftsRejectsUnmatchedDraft(t *testing.T) {
	_, err := ValidateAgentDrafts(`
[[manual.apps]]
name = "Other App"
`, []ReviewCandidate{{Name: "Draft Demo"}}, nil)
	if err == nil {
		t.Fatal("expected unmatched draft rejection")
	}
}

func TestReviewCandidateIdentityKeys(t *testing.T) {
	keys := ReviewCandidateIdentityKeys(ReviewCandidate{
		Name: "Parent / Child",
		SuggestedOverride: ReviewOverrideFields{
			Aliases: []string{"Alias.app"},
		},
		Evidence: []ReviewEvidence{{
			BundleID:    "com.example.child",
			MASID:       "123",
			Path:        "/Applications/Child.app",
			Identifiers: map[string]string{"package_id": "org.example.Child"},
		}},
	})
	for _, want := range []string{"name:parentchild", "name:parent", "name:child", "name:aliasapp", "bundle:com.example.child", "mas:123", "package_id:org.example.child"} {
		if !containsString(keys, want) {
			t.Fatalf("expected key %q in %#v", want, keys)
		}
	}
}

func TestRenderOverridePreview(t *testing.T) {
	preview := RenderOverridePreview([]ReviewCandidate{{
		Name:            "Draft Demo",
		ReasonCode:      "manual_app_unknown",
		RemediationCode: "manual_inventory_override",
		Confidence:      "medium",
		Evidence: []ReviewEvidence{{
			Scanner:  "macos_app_bundle",
			Path:     "/Applications/Draft Demo.app",
			BundleID: "com.example.draft",
			Version:  "1.0.0",
		}},
		SuggestedOverride: ReviewOverrideFields{
			Name:      "Draft Demo",
			Aliases:   []string{"com.example.draft"},
			ManagedBy: "manual",
			Detail:    "review installed app ownership and lifecycle",
		},
	}})
	for _, want := range []string{
		"# Generated preview",
		`name = "Draft Demo"`,
		`aliases = ["com.example.draft"]`,
		`managed_by = "manual"`,
		`# reason_code = "manual_app_unknown"`,
		`# remediation_code = "manual_inventory_override"`,
		`# confidence = "medium"`,
		`# evidence scanner="macos_app_bundle" path="/Applications/Draft Demo.app" bundle_id="com.example.draft" version="1.0.0"`,
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("expected preview to contain %q:\n%s", want, preview)
		}
	}
}

func TestAppendOverrideBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual-apps.toml")
	if err := AppendOverrideBlock(path, RenderOverrideFields(ReviewOverrideFields{Name: "First"})); err != nil {
		t.Fatalf("append first override: %v", err)
	}
	if err := AppendOverrideBlock(path, RenderOverrideFields(ReviewOverrideFields{Name: "Second"})); err != nil {
		t.Fatalf("append second override: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overrides: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "First"`) || !strings.Contains(content, `name = "Second"`) || !strings.Contains(content, "\n\n[[manual.apps]]") {
		t.Fatalf("expected appended override blocks:\n%s", content)
	}
}
