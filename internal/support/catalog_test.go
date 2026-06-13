package support

import "testing"

func TestCatalogIncludesRequiredV07SupportLabels(t *testing.T) {
	entries := Catalog()
	required := map[string]string{
		"provider/homebrew":                 LabelSupportedPreview,
		"provider/mise":                     LabelSupportedPreview,
		"provider/linux":                    LabelExperimental,
		"provider/windows":                  LabelExperimental,
		"provider/external-installers":      LabelDeferred,
		"command/brewfile":                  LabelCompatibility,
		"inventory_source/agent-enrichment": LabelExperimental,
	}
	for key, want := range required {
		found := false
		for _, entry := range entries {
			if entry.Surface+"/"+entry.Name == key {
				found = true
				if entry.Label != want {
					t.Fatalf("%s label = %q, want %q", key, entry.Label, want)
				}
			}
		}
		if !found {
			t.Fatalf("catalog missing %s", key)
		}
	}
}

func TestFilterBySurfaceAndLabel(t *testing.T) {
	entries := []Entry{
		{Surface: "provider", Name: "mise", Label: LabelSupportedPreview},
		{Surface: "provider", Name: "linux", Label: LabelExperimental},
		{Surface: "command", Name: "brewfile", Label: LabelCompatibility},
	}
	got := Filter(entries, "provider", LabelExperimental)
	if len(got) != 1 || got[0].Name != "linux" {
		t.Fatalf("unexpected filtered entries: %#v", got)
	}
	if !ValidSurface("inventory_source") || ValidSurface("package") {
		t.Fatal("unexpected surface validation result")
	}
	if !ValidLabel(LabelDeferred) || ValidLabel("stable") {
		t.Fatal("unexpected label validation result")
	}
}
