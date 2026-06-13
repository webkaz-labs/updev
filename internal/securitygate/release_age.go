package securitygate

import (
	"time"
)

func AnnotateReleaseAge(finding Finding, releasedAt time.Time, minAge time.Duration, evidence string) (Finding, time.Duration) {
	age := time.Since(releasedAt)
	finding = AnnotateReleaseDate(finding, releasedAt, evidence)
	finding.ReleaseAgeDays = int(age.Hours() / 24)
	finding.MinReleaseAgeDays = int(minAge.Hours() / 24)
	return finding, age
}

func AnnotateReleaseDate(finding Finding, releasedAt time.Time, evidence string) Finding {
	finding.ReleaseDate = releasedAt.Format(time.RFC3339)
	finding.Evidence = AppendEvidence(finding.Evidence, evidence)
	return finding
}

func AppendEvidence(evidence []string, value string) []string {
	if value == "" {
		return evidence
	}
	for _, existing := range evidence {
		if existing == value {
			return evidence
		}
	}
	return append(evidence, value)
}
