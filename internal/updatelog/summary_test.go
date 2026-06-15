package updatelog

import (
	"reflect"
	"testing"
)

func TestSummarizeExtractsUpdatedAndSkippedRows(t *testing.T) {
	summary := Summarize(`==> Updating Homebrew
==> Upgrading mole
  1.41.0 -> 1.42.0 (3.5MB)
🍺  mole 1.41.0 -> 1.42.0
Upgraded 1 outdated package`, `Warning: Skipping microsoft/git because it is not trusted.`)

	if want := []string{"mole 1.41.0 -> 1.42.0"}; !reflect.DeepEqual(summary.Updated, want) {
		t.Fatalf("Updated = %#v, want %#v", summary.Updated, want)
	}
	if want := []string{"microsoft/git skipped: because it is not trusted."}; !reflect.DeepEqual(summary.Skipped, want) {
		t.Fatalf("Skipped = %#v, want %#v", summary.Skipped, want)
	}
}

func TestAppendUniqueUpdatedKeepsShortestPerPackage(t *testing.T) {
	got := AppendUniqueUpdated(nil, "mole 1.41.0 -> 1.42.0 (3.5MB)", "mole 1.41.0 -> 1.42.0")
	want := []string{"mole 1.41.0 -> 1.42.0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendUniqueUpdated = %#v, want %#v", got, want)
	}
}

func TestNormalizeSkippedItemNamesHomebrewTrustWarnings(t *testing.T) {
	got := NormalizeSkippedItem("Warning: Skipping oven-sh/bun because it is not trusted. Run `brew trust oven-sh/bun`.")
	want := "oven-sh/bun skipped: because it is not trusted. Run `brew trust oven-sh/bun`."
	if got != want {
		t.Fatalf("NormalizeSkippedItem = %q, want %q", got, want)
	}
}

func TestAppendUniqueUpdatedKeepsHomebrewAndTapKeysSeparate(t *testing.T) {
	got := AppendUniqueUpdated(
		nil,
		"Updated Homebrew from abc to def.",
		"Updated 3 taps (homebrew/core, webkaz/tap and microsoft/git).",
	)
	want := []string{
		"Updated Homebrew from abc to def.",
		"Updated 3 taps (homebrew/core, webkaz/tap and microsoft/git).",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendUniqueUpdated = %#v, want %#v", got, want)
	}
}

func TestUpdatedItemParts(t *testing.T) {
	tests := []struct {
		name       string
		item       string
		wantName   string
		wantDetail string
	}{
		{name: "package version delta", item: "mole 1.41.0 -> 1.42.0", wantName: "mole", wantDetail: "1.41.0 -> 1.42.0"},
		{name: "homebrew core update", item: "Updated Homebrew from abc to def.", wantName: "Homebrew", wantDetail: "Updated Homebrew from abc to def."},
		{name: "tap update", item: "Updated 3 taps (homebrew/core and webkaz/tap).", wantName: "Homebrew taps", wantDetail: "Updated 3 taps (homebrew/core and webkaz/tap)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDetail := UpdatedItemParts(tt.item)
			if gotName != tt.wantName || gotDetail != tt.wantDetail {
				t.Fatalf("UpdatedItemParts(%q) = %q, %q; want %q, %q", tt.item, gotName, gotDetail, tt.wantName, tt.wantDetail)
			}
		})
	}
}
