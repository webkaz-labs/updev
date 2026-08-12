package securityadvisory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const osvVulnerabilityPageURLBase = "https://osv.dev/vulnerability/"

func QueryOSVBatch(ctx context.Context, client *http.Client, apiURL string, packages []Package) ([]Finding, error) {
	requestBody := OSVBatchRequest{Queries: make([]OSVQuery, 0, len(packages))}
	for _, pkg := range packages {
		requestBody.Queries = append(requestBody.Queries, OSVQuery{
			Version: pkg.Version,
			Package: OSVPackage{
				Name:      pkg.Package,
				Ecosystem: pkg.Ecosystem,
			},
		})
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
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
		return nil, fmt.Errorf("osv query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var batch OSVBatchResponse
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, err
	}
	if len(batch.Results) != len(packages) {
		return nil, fmt.Errorf("osv query returned %d results for %d packages", len(batch.Results), len(packages))
	}
	findings := []Finding{}
	for i, result := range batch.Results {
		pkg := packages[i]
		for _, vuln := range result.Vulns {
			detail, err := getOSVVulnDetail(ctx, client, apiURL, vuln.ID)
			if err != nil {
				detail = OSVVulnDetail{}
			}
			finding := Finding{
				Provider:         pkg.Provider,
				Name:             pkg.Name,
				Version:          pkg.Version,
				Ecosystem:        pkg.Ecosystem,
				Package:          pkg.Package,
				VulnID:           vuln.ID,
				Aliases:          detail.Aliases,
				Modified:         vuln.Modified,
				Severity:         PrimaryOSVSeverity(detail.Severity),
				MatchType:        OSVMatchType(detail, pkg),
				AffectedVersions: OSVAffectedVersions(detail, pkg),
				AffectedRanges:   OSVAffectedRanges(detail, pkg),
				FixedVersions:    FixedVersionsFromOSVDetail(detail, pkg),
				BinaryName:       pkg.BinaryName,
				BinaryPath:       pkg.BinaryPath,
				PathState:        pkg.PathState,
				Exposure:         ExposureFromPackage(pkg),
				Decision:         "hold",
				Confidence:       pkg.Confidence,
				Status:           plan.StatusHeld,
				URL:              OSVVulnerabilityPageURL(vuln.ID),
			}
			finding.Remediation = Remediation(finding)
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

func OSVVulnerabilityPageURL(id string) string {
	return osvVulnerabilityPageURLBase + id
}

const (
	AdvisoryMatchCandidateVersionAffected = "candidate_version_affected"
	AdvisoryMatchSourceRange              = "source_range_match"
	AdvisoryMatchRelated                  = "advisory_related"
)

func OSVMatchType(detail OSVVulnDetail, pkg Package) string {
	if osvDetailListsVersion(detail, pkg) {
		return AdvisoryMatchCandidateVersionAffected
	}
	if len(OSVAffectedRanges(detail, pkg)) > 0 {
		return AdvisoryMatchSourceRange
	}
	return AdvisoryMatchRelated
}

func OSVAffectedVersions(detail OSVVulnDetail, pkg Package) []string {
	seen := map[string]bool{}
	versions := []string{}
	for _, affected := range detail.Affected {
		if !osvAffectedPackageMatches(affected, pkg) {
			continue
		}
		for _, version := range affected.Versions {
			version = strings.TrimSpace(version)
			if version == "" || seen[version] {
				continue
			}
			seen[version] = true
			versions = append(versions, version)
		}
	}
	sort.Strings(versions)
	return versions
}

func OSVAffectedRanges(detail OSVVulnDetail, pkg Package) []string {
	seen := map[string]bool{}
	ranges := []string{}
	for _, affected := range detail.Affected {
		if !osvAffectedPackageMatches(affected, pkg) {
			continue
		}
		for _, versionRange := range affected.Ranges {
			parts := []string{}
			if versionRange.Type != "" {
				parts = append(parts, versionRange.Type)
			}
			if versionRange.Repo != "" {
				parts = append(parts, versionRange.Repo)
			}
			for _, event := range versionRange.Events {
				switch {
				case event.Introduced != "":
					parts = append(parts, "introduced "+event.Introduced)
				case event.Fixed != "":
					parts = append(parts, "fixed "+event.Fixed)
				case event.LastAffected != "":
					parts = append(parts, "last_affected "+event.LastAffected)
				case event.Limit != "":
					parts = append(parts, "limit "+event.Limit)
				}
			}
			value := strings.TrimSpace(strings.Join(parts, " "))
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			ranges = append(ranges, value)
		}
	}
	sort.Strings(ranges)
	return ranges
}

func osvDetailListsVersion(detail OSVVulnDetail, pkg Package) bool {
	candidates := advisoryVersionCandidates(pkg)
	if len(candidates) == 0 {
		return false
	}
	for _, affectedVersion := range OSVAffectedVersions(detail, pkg) {
		if candidates[normalizeAdvisoryVersion(affectedVersion)] {
			return true
		}
	}
	return false
}

func advisoryVersionCandidates(pkg Package) map[string]bool {
	version := strings.TrimSpace(pkg.Version)
	if version == "" {
		return nil
	}
	candidates := map[string]bool{}
	add := func(value string) {
		value = normalizeAdvisoryVersion(value)
		if value != "" {
			candidates[value] = true
		}
	}
	add(version)
	unprefixed := trimAdvisoryVersionPrefix(version)
	add(unprefixed)
	name := strings.TrimSpace(pkg.Name)
	if name != "" {
		add(name + "-" + version)
		add(name + "-" + unprefixed)
	}
	return candidates
}

func normalizeAdvisoryVersion(version string) string {
	return strings.ToLower(strings.TrimSpace(version))
}

func trimAdvisoryVersionPrefix(version string) string {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}

func osvAffectedPackageMatches(affected OSVAffected, pkg Package) bool {
	if osvAffectedGitRepoMatches(affected, pkg) {
		return true
	}
	if affected.Package.Name != "" && !strings.EqualFold(affected.Package.Name, pkg.Package) {
		return false
	}
	if affected.Package.Ecosystem != "" && !strings.EqualFold(affected.Package.Ecosystem, pkg.Ecosystem) {
		return false
	}
	return true
}

func osvAffectedGitRepoMatches(affected OSVAffected, pkg Package) bool {
	if !strings.EqualFold(strings.TrimSpace(pkg.Ecosystem), "GIT") {
		return false
	}
	target := normalizeOSVGitRepo(pkg.Package)
	if target == "" {
		return false
	}
	for _, versionRange := range affected.Ranges {
		if !strings.EqualFold(strings.TrimSpace(versionRange.Type), "GIT") {
			continue
		}
		if normalizeOSVGitRepo(versionRange.Repo) == target {
			return true
		}
	}
	return false
}

func normalizeOSVGitRepo(repo string) string {
	repo = strings.ToLower(strings.TrimSpace(repo))
	repo = strings.TrimSuffix(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

func FixedVersionsFromOSVDetail(detail OSVVulnDetail, pkg Package) []string {
	seen := map[string]bool{}
	versions := []string{}
	for _, affected := range detail.Affected {
		if !osvAffectedPackageMatches(affected, pkg) {
			continue
		}
		for _, versionRange := range affected.Ranges {
			for _, event := range versionRange.Events {
				fixed := strings.TrimSpace(event.Fixed)
				if fixed == "" || seen[fixed] {
					continue
				}
				seen[fixed] = true
				versions = append(versions, fixed)
			}
		}
	}
	sort.Strings(versions)
	return versions
}

func PrimaryOSVSeverity(severities []OSVSeverity) string {
	for _, severity := range severities {
		if severity.Type != "" && severity.Score != "" {
			return severity.Type + ":" + severity.Score
		}
		if severity.Score != "" {
			return severity.Score
		}
	}
	return ""
}

func getOSVVulnDetail(ctx context.Context, client *http.Client, apiURL string, id string) (OSVVulnDetail, error) {
	endpoint := strings.TrimSuffix(apiURL, "/querybatch") + "/vulns/" + id
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OSVVulnDetail{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return OSVVulnDetail{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return OSVVulnDetail{}, fmt.Errorf("osv detail failed: HTTP %d", response.StatusCode)
	}
	var detail OSVVulnDetail
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&detail); err != nil {
		return OSVVulnDetail{}, err
	}
	return detail, nil
}
