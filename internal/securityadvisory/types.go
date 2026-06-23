package securityadvisory

import (
	"github.com/webkaz-labs/updev/internal/plan"
)

type Package struct {
	Provider   string `json:"provider"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Ecosystem  string `json:"ecosystem"`
	Package    string `json:"package"`
	Confidence string `json:"confidence"`
	BinaryName string `json:"binary_name,omitempty"`
	BinaryPath string `json:"binary_path,omitempty"`
	PathState  string `json:"path_state,omitempty"`
}

type Finding struct {
	Provider         string       `json:"provider"`
	Name             string       `json:"name"`
	Version          string       `json:"version"`
	Ecosystem        string       `json:"ecosystem"`
	Package          string       `json:"package"`
	VulnID           string       `json:"vuln_id"`
	Aliases          []string     `json:"aliases,omitempty"`
	Modified         string       `json:"modified,omitempty"`
	Severity         string       `json:"severity,omitempty"`
	MatchType        string       `json:"match_type,omitempty"`
	AffectedVersions []string     `json:"affected_versions,omitempty"`
	AffectedRanges   []string     `json:"affected_ranges,omitempty"`
	KEV              *KEVFinding  `json:"kev,omitempty"`
	EPSS             *EPSSFinding `json:"epss,omitempty"`
	FixedVersions    []string     `json:"fixed_versions,omitempty"`
	BinaryName       string       `json:"binary_name,omitempty"`
	BinaryPath       string       `json:"binary_path,omitempty"`
	PathState        string       `json:"path_state,omitempty"`
	Exposure         string       `json:"exposure,omitempty"`
	Remediation      string       `json:"remediation,omitempty"`
	Decision         string       `json:"decision"`
	Confidence       string       `json:"confidence"`
	Reason           string       `json:"reason,omitempty"`
	Status           plan.Status  `json:"status"`
	URL              string       `json:"url,omitempty"`
}

type OSVBatchRequest struct {
	Queries []OSVQuery `json:"queries"`
}

type OSVQuery struct {
	Version string     `json:"version,omitempty"`
	Package OSVPackage `json:"package"`
}

type OSVPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type OSVBatchResponse struct {
	Results []OSVResult `json:"results"`
}

type OSVResult struct {
	Vulns []OSVVuln `json:"vulns,omitempty"`
}

type OSVVuln struct {
	ID       string `json:"id"`
	Modified string `json:"modified,omitempty"`
}

type OSVVulnDetail struct {
	ID       string        `json:"id"`
	Aliases  []string      `json:"aliases,omitempty"`
	Severity []OSVSeverity `json:"severity,omitempty"`
	Affected []OSVAffected `json:"affected,omitempty"`
}

type OSVSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type OSVAffected struct {
	Package  OSVPackage `json:"package"`
	Versions []string   `json:"versions,omitempty"`
	Ranges   []OSVRange `json:"ranges,omitempty"`
}

type OSVRange struct {
	Type   string          `json:"type,omitempty"`
	Repo   string          `json:"repo,omitempty"`
	Events []OSVRangeEvent `json:"events,omitempty"`
}

type OSVRangeEvent struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
	Limit        string `json:"limit,omitempty"`
}

type KEVCatalog struct {
	Vulnerabilities []KEVVulnerability `json:"vulnerabilities"`
}

type KEVVulnerability struct {
	CVEID                      string `json:"cveID"`
	VendorProject              string `json:"vendorProject,omitempty"`
	Product                    string `json:"product,omitempty"`
	VulnerabilityName          string `json:"vulnerabilityName,omitempty"`
	DateAdded                  string `json:"dateAdded,omitempty"`
	DueDate                    string `json:"dueDate,omitempty"`
	KnownRansomwareCampaignUse string `json:"knownRansomwareCampaignUse,omitempty"`
	RequiredAction             string `json:"requiredAction,omitempty"`
}

type KEVFinding struct {
	CVEID                      string `json:"cve_id"`
	VendorProject              string `json:"vendor_project,omitempty"`
	Product                    string `json:"product,omitempty"`
	VulnerabilityName          string `json:"vulnerability_name,omitempty"`
	DateAdded                  string `json:"date_added,omitempty"`
	DueDate                    string `json:"due_date,omitempty"`
	KnownRansomwareCampaignUse string `json:"known_ransomware_campaign_use,omitempty"`
	RequiredAction             string `json:"required_action,omitempty"`
}

type EPSSResponse struct {
	Data []EPSSEntry `json:"data"`
}

type EPSSEntry struct {
	CVE        string `json:"cve"`
	EPSS       string `json:"epss"`
	Percentile string `json:"percentile"`
	Date       string `json:"date"`
}

type EPSSFinding struct {
	CVEID      string  `json:"cve_id"`
	Score      float64 `json:"score"`
	Percentile float64 `json:"percentile"`
	Date       string  `json:"date,omitempty"`
}

type GitHubAdvisory struct {
	GHSAID          string                        `json:"ghsa_id"`
	CVEID           string                        `json:"cve_id,omitempty"`
	Summary         string                        `json:"summary,omitempty"`
	Type            string                        `json:"type,omitempty"`
	Severity        string                        `json:"severity,omitempty"`
	HTMLURL         string                        `json:"html_url,omitempty"`
	UpdatedAt       string                        `json:"updated_at,omitempty"`
	Vulnerabilities []GitHubAdvisoryVulnerability `json:"vulnerabilities,omitempty"`
}

type GitHubAdvisoryVulnerability struct {
	Package                GitHubAdvisoryPackage        `json:"package"`
	VulnerableVersionRange string                       `json:"vulnerable_version_range,omitempty"`
	FirstPatchedVersion    GitHubAdvisoryPatchedVersion `json:"first_patched_version,omitempty"`
}

type GitHubAdvisoryPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
}

type GitHubAdvisoryPatchedVersion string
