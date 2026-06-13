package manualinventory

import "testing"

func TestPlanActionClassifiesManualRows(t *testing.T) {
	tests := []struct {
		name string
		row  ReviewRow
		want string
	}{
		{
			name: "homebrew cask evidence",
			row:  ReviewRow{Name: "Demo", State: "manual", Detail: "source: homebrew cask; cask: demo"},
			want: "adopt-brew",
		},
		{
			name: "mac app store receipt",
			row:  ReviewRow{Name: "Motion", State: "installed", Detail: "source: mac app store receipt; mas_id: 123"},
			want: "adopt-mas",
		},
		{
			name: "user local app",
			row:  ReviewRow{Name: "Local", State: "installed", Detail: "path: /Users/me/Applications/Local.app"},
			want: "ignore-local",
		},
		{
			name: "live only installed app",
			row:  ReviewRow{SectionName: "manual/installed-apps", Name: "Unknown", State: "installed", Detail: "source: app bundle"},
			want: "needs-review",
		},
		{
			name: "vendor row",
			row:  ReviewRow{Name: "Vendor", State: "manual", Detail: "vendor updater; source_url: https://example.com"},
			want: "open-vendor",
		},
		{
			name: "documented manual row",
			row:  ReviewRow{Name: "Documented", State: "manual", Detail: "manual / apps"},
			want: "keep-manual",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlanAction(tt.row); got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestPlanSuggestedOverride(t *testing.T) {
	row := ReviewRow{Name: "Demo", Detail: "path: /Applications/Demo.app; bundle_id: com.example.demo; cask: demo; package_id: org.example.Demo"}
	override := PlanSuggestedOverride("adopt-brew", row)
	if override.Name != "Demo" || override.ManagedBy != "brew" || override.Detail == "" {
		t.Fatalf("unexpected override: %#v", override)
	}
	for _, want := range []string{"Demo.app", "com.example.demo", "demo", "org.example.Demo"} {
		if !containsString(override.Aliases, want) {
			t.Fatalf("expected alias %q in %#v", want, override.Aliases)
		}
	}
}

func TestEvidenceFromRow(t *testing.T) {
	evidence := EvidenceFromRow(ReviewRow{
		Name:    "Motion",
		State:   "installed",
		Version: "6.2",
		Detail:  "source: mac app store receipt; path: /Applications/Motion.app; bundle_id: com.apple.motionapp; owner: Apple; package_id: org.example.Motion",
	})
	if evidence.Scanner != "macos_app_bundle" || evidence.Source != "mac app store receipt" || evidence.Path != "/Applications/Motion.app" || evidence.BundleID != "com.apple.motionapp" || evidence.Version != "6.2" {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
	if evidence.Identifiers["package_id"] != "org.example.Motion" {
		t.Fatalf("expected portable identifier evidence, got %#v", evidence)
	}
	if evidence.ManagedBy != "mas" || evidence.OwnershipConfidence != "high" || evidence.ProviderMetadata != "mac app store receipt" {
		t.Fatalf("unexpected ownership evidence: %#v", evidence)
	}
}
