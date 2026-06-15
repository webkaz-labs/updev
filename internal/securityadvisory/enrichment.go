package securityadvisory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

func EnrichWithKEV(ctx context.Context, client *http.Client, kevURL string, findings []Finding) ([]Finding, error) {
	if len(findings) == 0 || !findingsHaveCVE(findings) {
		return findings, nil
	}
	kev, err := fetchCISAKEV(ctx, client, kevURL)
	if err != nil {
		return findings, err
	}
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		for _, cve := range findingCVEIDs(finding) {
			match, ok := kev[cve]
			if !ok {
				continue
			}
			enriched := KEVFinding{
				CVEID:                      match.CVEID,
				VendorProject:              match.VendorProject,
				Product:                    match.Product,
				VulnerabilityName:          match.VulnerabilityName,
				DateAdded:                  match.DateAdded,
				DueDate:                    match.DueDate,
				KnownRansomwareCampaignUse: match.KnownRansomwareCampaignUse,
				RequiredAction:             match.RequiredAction,
			}
			finding.KEV = &enriched
			finding.Decision = "block"
			finding.Status = plan.StatusBlocked
			break
		}
		out = append(out, finding)
	}
	return out, nil
}

func EnrichWithEPSS(ctx context.Context, client *http.Client, epssURL string, findings []Finding) ([]Finding, error) {
	cves := uniqueFindingCVEIDs(findings)
	if len(cves) == 0 {
		return findings, nil
	}
	scores, err := fetchEPSS(ctx, client, epssURL, cves)
	if err != nil {
		return findings, err
	}
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		for _, cve := range findingCVEIDs(finding) {
			score, ok := scores[cve]
			if !ok {
				continue
			}
			finding.EPSS = &score
			break
		}
		out = append(out, finding)
	}
	return out, nil
}

func fetchCISAKEV(ctx context.Context, client *http.Client, kevURL string) (map[string]KEVVulnerability, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, kevURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("cisa kev query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var catalog KEVCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, err
	}
	out := map[string]KEVVulnerability{}
	for _, vulnerability := range catalog.Vulnerabilities {
		if vulnerability.CVEID != "" {
			out[vulnerability.CVEID] = vulnerability
		}
	}
	return out, nil
}

func findingsHaveCVE(findings []Finding) bool {
	for _, finding := range findings {
		if len(findingCVEIDs(finding)) > 0 {
			return true
		}
	}
	return false
}

func findingCVEIDs(finding Finding) []string {
	ids := []string{}
	if strings.HasPrefix(finding.VulnID, "CVE-") {
		ids = append(ids, finding.VulnID)
	}
	for _, alias := range finding.Aliases {
		if strings.HasPrefix(alias, "CVE-") {
			ids = append(ids, alias)
		}
	}
	return ids
}

func fetchEPSS(ctx context.Context, client *http.Client, epssURL string, cves []string) (map[string]EPSSFinding, error) {
	endpoint, err := url.Parse(epssURL)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("cve", strings.Join(cves, ","))
	endpoint.RawQuery = query.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("first epss query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var decoded EPSSResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	out := map[string]EPSSFinding{}
	for _, entry := range decoded.Data {
		score, err := strconv.ParseFloat(entry.EPSS, 64)
		if err != nil {
			continue
		}
		percentile, err := strconv.ParseFloat(entry.Percentile, 64)
		if err != nil {
			continue
		}
		out[entry.CVE] = EPSSFinding{
			CVEID:      entry.CVE,
			Score:      score,
			Percentile: percentile,
			Date:       entry.Date,
		}
	}
	return out, nil
}

func uniqueFindingCVEIDs(findings []Finding) []string {
	seen := map[string]bool{}
	cves := []string{}
	for _, finding := range findings {
		for _, cve := range findingCVEIDs(finding) {
			if seen[cve] {
				continue
			}
			seen[cve] = true
			cves = append(cves, cve)
		}
	}
	sort.Strings(cves)
	return cves
}
