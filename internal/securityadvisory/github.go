package securityadvisory

import (
	"context"
	"github.com/webkaz-labs/updev/internal/plan"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func (version *GitHubAdvisoryPatchedVersion) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*version = GitHubAdvisoryPatchedVersion(strings.TrimSpace(value))
		return nil
	}
	var object struct {
		Identifier string `json:"identifier"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*version = GitHubAdvisoryPatchedVersion(strings.TrimSpace(object.Identifier))
	return nil
}

func QueryGitHubAdvisories(ctx context.Context, client *http.Client, apiBase string, token string, packages []Package) ([]Finding, error) {
	findings := []Finding{}
	for _, pkg := range packages {
		ecosystem, ok := githubAdvisoryEcosystem(pkg.Ecosystem)
		if !ok || pkg.Package == "" || pkg.Version == "" {
			continue
		}
		for _, advisoryType := range []string{"reviewed", "malware"} {
			advisories, err := fetchGitHubAdvisories(ctx, client, apiBase, token, ecosystem, pkg.Package, pkg.Version, advisoryType)
			if err != nil {
				return findings, err
			}
			for _, advisory := range advisories {
				findings = append(findings, githubAdvisoryFinding(pkg, advisory))
			}
		}
	}
	return findings, nil
}

func fixedVersionsFromGitHubAdvisory(advisory GitHubAdvisory, pkg Package) []string {
	ecosystem, ok := githubAdvisoryEcosystem(pkg.Ecosystem)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	versions := []string{}
	for _, vulnerability := range advisory.Vulnerabilities {
		if vulnerability.Package.Name != "" && !strings.EqualFold(vulnerability.Package.Name, pkg.Package) {
			continue
		}
		if vulnerability.Package.Ecosystem != "" && !strings.EqualFold(vulnerability.Package.Ecosystem, ecosystem) {
			continue
		}
		fixed := strings.TrimSpace(string(vulnerability.FirstPatchedVersion))
		if fixed == "" || seen[fixed] {
			continue
		}
		seen[fixed] = true
		versions = append(versions, fixed)
	}
	sort.Strings(versions)
	return versions
}

func fetchGitHubAdvisories(ctx context.Context, client *http.Client, apiBase string, token string, ecosystem string, packageName string, version string, advisoryType string) ([]GitHubAdvisory, error) {
	endpoint, err := url.Parse(strings.TrimRight(apiBase, "/") + "/advisories")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("type", advisoryType)
	query.Set("ecosystem", ecosystem)
	query.Set("affects", packageName+"@"+version)
	query.Set("per_page", "100")
	endpoint.RawQuery = query.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("github advisory query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var advisories []GitHubAdvisory
	if err := json.Unmarshal(body, &advisories); err != nil {
		return nil, err
	}
	return advisories, nil
}

func githubAdvisoryFinding(pkg Package, advisory GitHubAdvisory) Finding {
	finding := Finding{
		Provider:      pkg.Provider,
		Name:          pkg.Name,
		Version:       pkg.Version,
		Ecosystem:     pkg.Ecosystem,
		Package:       pkg.Package,
		VulnID:        firstNonEmpty(advisory.GHSAID, advisory.CVEID),
		Aliases:       githubAdvisoryAliases(advisory),
		Modified:      advisory.UpdatedAt,
		Severity:      strings.ToLower(strings.TrimSpace(advisory.Severity)),
		FixedVersions: fixedVersionsFromGitHubAdvisory(advisory, pkg),
		BinaryName:    pkg.BinaryName,
		BinaryPath:    pkg.BinaryPath,
		PathState:     pkg.PathState,
		Exposure:      ExposureFromPackage(pkg),
		Decision:      "hold",
		Confidence:    pkg.Confidence,
		Reason:        githubAdvisoryReason(advisory),
		Status:        plan.StatusHeld,
		URL:           advisory.HTMLURL,
	}
	finding.Remediation = Remediation(finding)
	return finding
}

func githubAdvisoryEcosystem(ecosystem string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		return "npm", true
	case "pypi":
		return "pip", true
	case "crates.io":
		return "rust", true
	case "go":
		return "go", true
	case "rubygems":
		return "rubygems", true
	case "maven":
		return "maven", true
	case "nuget":
		return "nuget", true
	case "composer":
		return "composer", true
	default:
		return "", false
	}
}

func githubAdvisoryAliases(advisory GitHubAdvisory) []string {
	aliases := []string{}
	for _, value := range []string{advisory.GHSAID, advisory.CVEID} {
		value = strings.TrimSpace(value)
		if value == "" || value == advisory.GHSAID {
			continue
		}
		aliases = append(aliases, value)
	}
	return aliases
}

func githubAdvisoryReason(advisory GitHubAdvisory) string {
	if strings.EqualFold(advisory.Type, "malware") {
		return "GitHub Advisory malware match"
	}
	return "GitHub Advisory vulnerability match"
}

func IsGitHubAdvisoryFinding(finding Finding) bool {
	return strings.HasPrefix(finding.Reason, "GitHub Advisory") ||
		strings.Contains(finding.URL, "github.com/advisories/")
}
