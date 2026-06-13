package manualinventory

import (
	"strings"
	"testing"
)

func TestParseStructuredAppsReadsManualSource(t *testing.T) {
	apps := ParseStructuredApps(`
[[manual.apps]]
name = "Demo App"
aliases = [
  "Demo.app",
  "com.example.demo",
]
category = "Vendor"
description = "Vendor managed"
managed_by = "vendor"
review_status = "ACCEPTED"

[manual.apps.identifiers]
bundle_id = "com.example.demo"

[manual.apps.provenance]
source = "vendor"
evidence = ["agent", "bundle"]
`)
	if len(apps) != 1 {
		t.Fatalf("expected one app, got %#v", apps)
	}
	app := apps[0]
	if app.Name != "Demo App" || app.Category != "Vendor" || app.Detail != "Vendor managed" || app.ManagedBy != "vendor" || app.ReviewStatus != "accepted" {
		t.Fatalf("unexpected app fields: %#v", app)
	}
	if len(app.Aliases) != 2 || app.Aliases[0] != "Demo.app" || app.Aliases[1] != "com.example.demo" {
		t.Fatalf("unexpected aliases: %#v", app.Aliases)
	}
	if app.Identifiers["bundle_id"] != "com.example.demo" || app.Provenance["source"] != "vendor" {
		t.Fatalf("unexpected structured maps: %#v", app)
	}
	if len(app.Evidence) != 2 || app.Evidence[0] != "agent" || app.Evidence[1] != "bundle" {
		t.Fatalf("unexpected evidence: %#v", app.Evidence)
	}
}

func TestSourceKindDetection(t *testing.T) {
	if !SourceIsStructured("manual-apps.TOML") || SourceIsStructured("manual-apps.md") {
		t.Fatalf("unexpected structured source detection")
	}
	if !SourceIsMarkdown("docs/apps.markdown") || SourceIsMarkdown("manual-apps.toml") {
		t.Fatalf("unexpected markdown source detection")
	}
}

func TestStructuredDraftBlocks(t *testing.T) {
	content := `[[manual.apps]]
name = "Accepted"
review_status = "accepted"

[[manual.apps]]
name = "Draft Demo"
aliases = ["com.example.draft"]
review_status = "draft"
`
	blocks := ParseStructuredAppRawBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected two blocks, got %#v", blocks)
	}
	block, err := SelectStructuredDraftBlock(blocks, "example.draft")
	if err != nil {
		t.Fatalf("expected draft match: %v", err)
	}
	if block.App.Name != "Draft Demo" {
		t.Fatalf("expected draft demo, got %#v", block.App)
	}
	replacement := RenderStructuredAppBlock(StructuredApp{Name: "Draft Demo", ReviewStatus: "accepted"})
	updated := ReplaceStructuredBlock(content, block, replacement)
	if !strings.Contains(updated, `review_status = "accepted"`) || strings.Contains(updated, "example.draft") {
		t.Fatalf("expected replaced draft block:\n%s", updated)
	}
}

func TestRenderStructuredDraftBlock(t *testing.T) {
	rendered := RenderStructuredDraftBlock(StructuredApp{
		Name:   "Draft Demo",
		Detail: "Generated detail",
		Identifiers: map[string]string{
			"bundle_id": "com.example.draft",
		},
		Provenance: map[string]string{
			"owner": "Example",
		},
		Evidence: []string{"agent"},
	})
	for _, want := range []string{
		`name = "Draft Demo"`,
		`description = "Generated detail"`,
		`review_status = "draft"`,
		`bundle_id = "com.example.draft"`,
		`source = "agent"`,
		`owner = "Example"`,
		`evidence = ["agent"]`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered draft to contain %q:\n%s", want, rendered)
		}
	}
}
