package mise

import "testing"

func TestSafetyFindingFromOutdated(t *testing.T) {
	finding := SafetyFindingFromOutdated(" node ", OutdatedItem{
		Requested: "lts",
		Current:   "24.16.0",
		Latest:    "24.17.0",
	})

	if finding.Provider != "mise" || finding.Kind != "tool" || finding.Name != "node" {
		t.Fatalf("unexpected finding identity: %#v", finding)
	}
	if len(finding.InstalledVersions) != 1 || finding.InstalledVersions[0] != "24.16.0" || finding.CurrentVersion != "24.17.0" || finding.Version != "lts" {
		t.Fatalf("unexpected finding versions: %#v", finding)
	}
	if finding.Decision != "review" || finding.Confidence != "low" {
		t.Fatalf("unexpected finding decision: %#v", finding)
	}
}

func TestBumpSafetyFindingFromOutdated(t *testing.T) {
	bump := "1.3.0"
	finding, ok := BumpSafetyFindingFromOutdated("tool", OutdatedItem{
		Requested: "1.2.0",
		Current:   "1.2.0",
		Latest:    "1.2.0",
		Bump:      &bump,
	})
	if !ok {
		t.Fatal("expected bump finding")
	}
	if finding.Source != BumpSource || finding.CurrentVersion != bump {
		t.Fatalf("unexpected bump finding: %#v", finding)
	}
	if len(finding.Evidence) != 2 || finding.Evidence[0] != "mise outdated --json --bump" || finding.Evidence[1] != "mise pinned-version bump candidate" {
		t.Fatalf("unexpected bump evidence: %#v", finding.Evidence)
	}

	if _, ok := BumpSafetyFindingFromOutdated("tool", OutdatedItem{}); ok {
		t.Fatal("expected missing bump to be skipped")
	}
}

func TestSafetyFindingsFromOutdatedJSON(t *testing.T) {
	findings, err := SafetyFindingsFromOutdatedJSON(`{
		"node": {"requested":"lts","current":"24.16.0","latest":"24.17.0"},
		"empty": {}
	}`)
	if err != nil {
		t.Fatalf("expected findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %#v", findings)
	}
	if findings[0].Name != "node" || findings[0].Version != "lts" || findings[0].CurrentVersion != "24.17.0" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestBumpSafetyFindingsFromOutdatedJSON(t *testing.T) {
	findings, err := BumpSafetyFindingsFromOutdatedJSON(`{
		"node": {"requested":"lts","current":"24.16.0","latest":"24.17.0","bump":null},
		"tool": {"requested":"1.2.0","current":"1.2.0","latest":"1.2.0","bump":"1.3.0"}
	}`)
	if err != nil {
		t.Fatalf("expected bump findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one bump finding, got %#v", findings)
	}
	if findings[0].Name != "tool" || findings[0].Source != BumpSource || findings[0].CurrentVersion != "1.3.0" {
		t.Fatalf("unexpected bump finding: %#v", findings[0])
	}
}
