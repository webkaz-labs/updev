package securitygate

import (
	"testing"
	"time"
)

func TestAnnotateReleaseAgeSetsAgeFieldsAndEvidence(t *testing.T) {
	releasedAt := time.Now().Add(-49 * time.Hour)
	finding, age := AnnotateReleaseAge(Finding{Evidence: []string{"existing"}}, releasedAt, 3*24*time.Hour, "release metadata")
	if finding.ReleaseDate == "" || finding.ReleaseAgeDays != 2 || finding.MinReleaseAgeDays != 3 {
		t.Fatalf("unexpected release age fields: %#v", finding)
	}
	if age < 48*time.Hour || age > 50*time.Hour {
		t.Fatalf("unexpected age %s", age)
	}
	if len(finding.Evidence) != 2 || finding.Evidence[0] != "existing" || finding.Evidence[1] != "release metadata" {
		t.Fatalf("unexpected evidence: %#v", finding.Evidence)
	}
}

func TestAnnotateReleaseDateDoesNotSetAgeFields(t *testing.T) {
	releasedAt := time.Now().Add(-49 * time.Hour)
	finding := AnnotateReleaseDate(Finding{}, releasedAt, "release metadata")
	if finding.ReleaseDate == "" || finding.ReleaseAgeDays != 0 || finding.MinReleaseAgeDays != 0 {
		t.Fatalf("unexpected release date fields: %#v", finding)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0] != "release metadata" {
		t.Fatalf("unexpected evidence: %#v", finding.Evidence)
	}
}

func TestAppendEvidenceSkipsEmptyAndDuplicates(t *testing.T) {
	evidence := AppendEvidence([]string{"metadata"}, "")
	evidence = AppendEvidence(evidence, "metadata")
	if len(evidence) != 1 || evidence[0] != "metadata" {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}
