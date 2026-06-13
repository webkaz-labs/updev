package nativeaudit

import (
	"encoding/json"
	"io"
	"strings"
)

type Counts struct {
	Info     int `json:"info,omitempty"`
	Low      int `json:"low,omitempty"`
	Moderate int `json:"moderate,omitempty"`
	High     int `json:"high,omitempty"`
	Critical int `json:"critical,omitempty"`
	Total    int `json:"total,omitempty"`
}

type NPMReport struct {
	Error           NPMError                   `json:"error"`
	Vulnerabilities map[string]json.RawMessage `json:"vulnerabilities"`
	Metadata        struct {
		Vulnerabilities Counts `json:"vulnerabilities"`
	} `json:"metadata"`
}

type NPMError struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

type CargoReport struct {
	Vulnerabilities struct {
		Found bool              `json:"found"`
		Count int               `json:"count"`
		List  []json.RawMessage `json:"list"`
	} `json:"vulnerabilities"`
}

type PipReport struct {
	Dependencies []PipDependency `json:"dependencies"`
}

type PipDependency struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Vulns   []json.RawMessage `json:"vulns"`
}

type ComposerReport struct {
	Advisories json.RawMessage `json:"advisories"`
}

type BundlerReport struct {
	Results         json.RawMessage `json:"results"`
	Vulnerabilities json.RawMessage `json:"vulnerabilities"`
	Advisories      json.RawMessage `json:"advisories"`
}

type DotnetReport struct {
	Projects []DotnetProject `json:"projects"`
}

type DotnetProject struct {
	Frameworks []DotnetFramework `json:"frameworks"`
}

type DotnetFramework struct {
	TopLevelPackages   []DotnetPackage `json:"topLevelPackages"`
	TransitivePackages []DotnetPackage `json:"transitivePackages"`
}

type DotnetPackage struct {
	Vulnerabilities []json.RawMessage `json:"vulnerabilities"`
}

type GenericReport struct {
	Error      NPMError
	Advisories int
	Vulnerable Counts
}

type GovulncheckReport struct {
	Findings []GovulncheckFinding
}

type GovulncheckFinding struct {
	OSV          string `json:"osv"`
	FixedVersion string `json:"fixed_version"`
}

type govulncheckMessage struct {
	Finding *GovulncheckFinding `json:"finding"`
}

func ParseNPMReport(raw string) (NPMReport, bool) {
	var report NPMReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseGenericReport(raw string) (GenericReport, bool) {
	var report GenericReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return report, false
	}
	if rawError, ok := root["error"]; ok {
		_ = json.Unmarshal(rawError, &report.Error)
	}
	report.Advisories += CountJSONEntries(root["vulnerabilities"])
	report.Advisories += CountJSONEntries(root["advisories"])
	if rawMetadata, ok := root["metadata"]; ok {
		var metadata struct {
			Vulnerabilities Counts `json:"vulnerabilities"`
		}
		if err := json.Unmarshal(rawMetadata, &metadata); err == nil {
			report.Vulnerable = metadata.Vulnerabilities
		}
	}
	return report, true
}

func ParseCargoReport(raw string) (CargoReport, bool) {
	var report CargoReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParsePipReport(raw string) (PipReport, bool) {
	var report PipReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseGovulncheckReport(raw string) (GovulncheckReport, bool) {
	var report GovulncheckReport
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return report, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	for {
		var message govulncheckMessage
		if err := decoder.Decode(&message); err != nil {
			if err == io.EOF {
				break
			}
			return report, false
		}
		if message.Finding == nil || strings.TrimSpace(message.Finding.OSV) == "" {
			continue
		}
		report.Findings = append(report.Findings, *message.Finding)
	}
	return report, true
}

func ParseComposerReport(raw string) (ComposerReport, bool) {
	var report ComposerReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseBundlerReport(raw string) (BundlerReport, bool) {
	var report BundlerReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func ParseDotnetReport(raw string) (DotnetReport, bool) {
	var report DotnetReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func CountJSONEntries(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var many []json.RawMessage
	if err := json.Unmarshal(raw, &many); err == nil {
		return len(many)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		return len(object)
	}
	return 0
}

func CountComposerAdvisories(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var grouped map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &grouped); err == nil {
		count := 0
		for _, advisories := range grouped {
			count += len(advisories)
		}
		return count
	}
	return CountJSONEntries(raw)
}

func CountBundlerFindings(report BundlerReport) int {
	for _, raw := range []json.RawMessage{report.Results, report.Vulnerabilities, report.Advisories} {
		if count := CountJSONEntries(raw); count > 0 {
			return count
		}
	}
	return 0
}
