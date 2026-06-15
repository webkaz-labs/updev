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
				Provider:      pkg.Provider,
				Name:          pkg.Name,
				Version:       pkg.Version,
				Ecosystem:     pkg.Ecosystem,
				Package:       pkg.Package,
				VulnID:        vuln.ID,
				Aliases:       detail.Aliases,
				Modified:      vuln.Modified,
				Severity:      PrimaryOSVSeverity(detail.Severity),
				FixedVersions: FixedVersionsFromOSVDetail(detail, pkg),
				BinaryName:    pkg.BinaryName,
				BinaryPath:    pkg.BinaryPath,
				PathState:     pkg.PathState,
				Exposure:      ExposureFromPackage(pkg),
				Decision:      "hold",
				Confidence:    pkg.Confidence,
				Status:        plan.StatusHeld,
				URL:           OSVVulnerabilityPageURL(vuln.ID),
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

func FixedVersionsFromOSVDetail(detail OSVVulnDetail, pkg Package) []string {
	seen := map[string]bool{}
	versions := []string{}
	for _, affected := range detail.Affected {
		if affected.Package.Name != "" && !strings.EqualFold(affected.Package.Name, pkg.Package) {
			continue
		}
		if affected.Package.Ecosystem != "" && !strings.EqualFold(affected.Package.Ecosystem, pkg.Ecosystem) {
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
