package securityscanner

import (
	"encoding/json"
	"strings"
)

type OSVReport struct {
	Results []OSVResult `json:"results"`
}

type OSVResult struct {
	Source   OSVSource    `json:"source"`
	Packages []OSVPackage `json:"packages"`
}

type OSVSource struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type OSVPackage struct {
	Package         PackageInfo `json:"package"`
	Vulnerabilities []OSVVuln   `json:"vulnerabilities"`
	Groups          []OSVGroup  `json:"groups,omitempty"`
}

type OSVVuln struct {
	ID       string        `json:"id"`
	Aliases  []string      `json:"aliases"`
	Severity []OSVSeverity `json:"severity"`
	Affected []OSVAffected `json:"affected,omitempty"`
}

type OSVSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type OSVAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	Ranges []OSVRange `json:"ranges,omitempty"`
}

type OSVRange struct {
	Events []OSVEvent `json:"events,omitempty"`
}

type OSVEvent struct {
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}

type OSVGroup struct {
	IDs         []string `json:"ids,omitempty"`
	MaxSeverity string   `json:"max_severity,omitempty"`
}

type GitleaksFinding struct {
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	EndLine     int    `json:"EndLine"`
	Commit      string `json:"Commit"`
	RuleID      string `json:"RuleID"`
	Fingerprint string `json:"Fingerprint"`
}

type ZizmorFinding struct {
	Ident          string `json:"ident"`
	Desc           string `json:"desc"`
	URL            string `json:"url"`
	Determinations struct {
		Confidence string `json:"confidence"`
		Severity   string `json:"severity"`
	} `json:"determinations"`
	Locations []ZizmorLocation `json:"locations"`
	Ignored   bool             `json:"ignored"`
}

type ZizmorLocation struct {
	Symbolic struct {
		Key struct {
			Local struct {
				GivenPath string `json:"given_path"`
			} `json:"Local"`
		} `json:"key"`
	} `json:"symbolic"`
	Concrete struct {
		Location struct {
			StartPoint struct {
				Row int `json:"row"`
			} `json:"start_point"`
			EndPoint struct {
				Row int `json:"row"`
			} `json:"end_point"`
		} `json:"location"`
	} `json:"concrete"`
}

type TrivyReport struct {
	Results []TrivyResult `json:"Results"`
}

type TrivyResult struct {
	Target            string               `json:"Target"`
	Type              string               `json:"Type"`
	Vulnerabilities   []TrivyVulnerability `json:"Vulnerabilities"`
	Misconfigurations []TrivyMisconfig     `json:"Misconfigurations"`
	Secrets           []TrivySecret        `json:"Secrets"`
}

type TrivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
	PrimaryURL       string `json:"PrimaryURL"`
	PkgIdentifier    struct {
		PURL string `json:"PURL"`
	} `json:"PkgIdentifier"`
}

type TrivyMisconfig struct {
	ID            string             `json:"ID"`
	Type          string             `json:"Type"`
	Title         string             `json:"Title"`
	Message       string             `json:"Message"`
	Resolution    string             `json:"Resolution"`
	Severity      string             `json:"Severity"`
	PrimaryURL    string             `json:"PrimaryURL"`
	Status        string             `json:"Status"`
	CauseMetadata TrivyCauseMetadata `json:"CauseMetadata"`
}

type TrivyCauseMetadata struct {
	StartLine int `json:"StartLine"`
	EndLine   int `json:"EndLine"`
}

type TrivySecret struct {
	RuleID    string `json:"RuleID"`
	Category  string `json:"Category"`
	Severity  string `json:"Severity"`
	Title     string `json:"Title"`
	StartLine int    `json:"StartLine"`
	EndLine   int    `json:"EndLine"`
}

type GrypeReport struct {
	Matches []GrypeMatch `json:"matches"`
}

type GrypeMatch struct {
	Vulnerability GrypeVulnerability `json:"vulnerability"`
	Artifact      GrypeArtifact      `json:"artifact"`
	MatchDetails  []GrypeMatchDetail `json:"matchDetails"`
}

type GrypeVulnerability struct {
	ID          string   `json:"id"`
	Severity    string   `json:"severity"`
	Description string   `json:"description"`
	Fix         GrypeFix `json:"fix"`
	URLs        []string `json:"urls"`
}

type GrypeFix struct {
	Versions []string `json:"versions"`
	State    string   `json:"state"`
}

type GrypeArtifact struct {
	Name      string          `json:"name"`
	Version   string          `json:"version"`
	Type      string          `json:"type"`
	PURL      string          `json:"purl"`
	Locations []GrypeLocation `json:"locations"`
}

type GrypeLocation struct {
	Path string `json:"path"`
}

type GrypeMatchDetail struct {
	Type    string `json:"type"`
	Matcher string `json:"matcher"`
}

func ParseOSVReport(raw string) (OSVReport, bool) {
	var report OSVReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseGitleaksReport(raw string) ([]GitleaksFinding, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var findings []GitleaksFinding
	if err := json.Unmarshal([]byte(raw), &findings); err != nil {
		return nil, false
	}
	return findings, true
}

func ParseZizmorReport(raw string) ([]ZizmorFinding, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var findings []ZizmorFinding
	if err := json.Unmarshal([]byte(raw), &findings); err != nil {
		return nil, false
	}
	return findings, true
}

func ParseTrivyReport(raw string) (TrivyReport, bool) {
	var report TrivyReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseGrypeReport(raw string) (GrypeReport, bool) {
	var report GrypeReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}
