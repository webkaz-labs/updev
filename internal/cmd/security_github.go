package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const defaultGitHubAPIURL = "https://api.github.com"
const githubRepositoryCacheMaxAge = 6 * time.Hour
const githubRepositoryCacheVersion = 1

var githubCLITokenCache struct {
	sync.Once
	value string
}

type githubPosture struct {
	Provider                     string   `json:"provider"`
	Name                         string   `json:"name"`
	Repository                   string   `json:"repository"`
	URL                          string   `json:"url,omitempty"`
	DefaultBranch                string   `json:"default_branch,omitempty"`
	Private                      bool     `json:"private"`
	Fork                         bool     `json:"fork"`
	Archived                     bool     `json:"archived"`
	Disabled                     bool     `json:"disabled"`
	CreatedAt                    string   `json:"created_at,omitempty"`
	PushedAt                     string   `json:"pushed_at,omitempty"`
	UpdatedAt                    string   `json:"updated_at,omitempty"`
	RepositoryAgeDays            int      `json:"repository_age_days,omitempty"`
	MinRepositoryAgeDays         int      `json:"min_repository_age_days,omitempty"`
	OpenIssuesCount              int      `json:"open_issues_count"`
	StargazersCount              int      `json:"stargazers_count"`
	AdvancedSecurity             string   `json:"advanced_security,omitempty"`
	SecretScanning               string   `json:"secret_scanning,omitempty"`
	SecretScanningPushProtection string   `json:"secret_scanning_push_protection,omitempty"`
	DependabotSecurityUpdates    string   `json:"dependabot_security_updates,omitempty"`
	Decision                     string   `json:"decision"`
	Confidence                   string   `json:"confidence"`
	Reason                       string   `json:"reason,omitempty"`
	Remediation                  string   `json:"remediation,omitempty"`
	Evidence                     []string `json:"evidence,omitempty"`
}

type githubRepository struct {
	FullName            string                    `json:"full_name"`
	HTMLURL             string                    `json:"html_url"`
	DefaultBranch       string                    `json:"default_branch"`
	Private             bool                      `json:"private"`
	Fork                bool                      `json:"fork"`
	Archived            bool                      `json:"archived"`
	Disabled            bool                      `json:"disabled"`
	CreatedAt           string                    `json:"created_at"`
	PushedAt            string                    `json:"pushed_at"`
	UpdatedAt           string                    `json:"updated_at"`
	OpenIssuesCount     int                       `json:"open_issues_count"`
	StargazersCount     int                       `json:"stargazers_count"`
	SecurityAndAnalysis githubSecurityAndAnalysis `json:"security_and_analysis"`
}

type githubSecurityAndAnalysis struct {
	AdvancedSecurity             githubSecurityFeature `json:"advanced_security"`
	SecretScanning               githubSecurityFeature `json:"secret_scanning"`
	SecretScanningPushProtection githubSecurityFeature `json:"secret_scanning_push_protection"`
	DependabotSecurityUpdates    githubSecurityFeature `json:"dependabot_security_updates"`
}

type githubSecurityFeature struct {
	Status string `json:"status"`
}

type githubAdvisory struct {
	GHSAID          string                        `json:"ghsa_id"`
	CVEID           string                        `json:"cve_id,omitempty"`
	Summary         string                        `json:"summary,omitempty"`
	Type            string                        `json:"type,omitempty"`
	Severity        string                        `json:"severity,omitempty"`
	HTMLURL         string                        `json:"html_url,omitempty"`
	UpdatedAt       string                        `json:"updated_at,omitempty"`
	Vulnerabilities []githubAdvisoryVulnerability `json:"vulnerabilities,omitempty"`
}

type githubAdvisoryVulnerability struct {
	Package                githubAdvisoryPackage        `json:"package"`
	VulnerableVersionRange string                       `json:"vulnerable_version_range,omitempty"`
	FirstPatchedVersion    githubAdvisoryPatchedVersion `json:"first_patched_version,omitempty"`
}

type githubAdvisoryPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
}

type githubAdvisoryPatchedVersion string

func (version *githubAdvisoryPatchedVersion) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*version = githubAdvisoryPatchedVersion(strings.TrimSpace(value))
		return nil
	}
	var object struct {
		Identifier string `json:"identifier"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*version = githubAdvisoryPatchedVersion(strings.TrimSpace(object.Identifier))
	return nil
}

type githubURLPostureRequest struct {
	provider string
	name     string
	urls     []string
}

type githubRepoPostureRequest struct {
	provider string
	name     string
	repo     string
}

const githubPostureFetchConcurrency = 6

func githubPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item) ([]githubPosture, error) {
	requests := []githubRepoPostureRequest{}
	seen := map[string]bool{}
	for _, item := range items {
		repo, ok := githubRepoFromMiseName(item.Name)
		if !ok || item.Provider != "mise" {
			continue
		}
		key := strings.ToLower(repo)
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, githubRepoPostureRequest{provider: item.Provider, name: item.Name, repo: repo})
	}
	return githubPosturesFromRepoRequests(ctx, client, apiBase, requests)
}

func githubPosturesFromHomebrew(ctx context.Context, client *http.Client, apiBase string, postures []homebrewPosture) ([]githubPosture, error) {
	requests := []githubRepoPostureRequest{}
	seen := map[string]bool{}
	for _, posture := range postures {
		repo, ok := githubRepoFromAnyURL(posture.URL)
		if !ok {
			repo, ok = githubRepoFromAnyURL(posture.Homepage)
		}
		if !ok {
			continue
		}
		key := strings.ToLower(repo)
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, githubRepoPostureRequest{provider: "brew", name: posture.Kind + ":" + posture.Name, repo: repo})
	}
	return githubPosturesFromRepoRequests(ctx, client, apiBase, requests)
}

func githubPosturesFromVSCode(ctx context.Context, client *http.Client, apiBase string, postures []vscodePosture) ([]githubPosture, error) {
	requests := []githubRepoPostureRequest{}
	seen := map[string]bool{}
	for _, posture := range postures {
		repo, ok := githubRepoFromAnyURL(posture.RepositoryURL)
		if !ok {
			continue
		}
		key := strings.ToLower(repo)
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, githubRepoPostureRequest{provider: "brew", name: "vscode:" + posture.Name, repo: repo})
	}
	return githubPosturesFromRepoRequests(ctx, client, apiBase, requests)
}

func githubPosturesFromRegistry(ctx context.Context, client *http.Client, apiBase string, npmPostures []npmPosture, cargoPostures []cargoPosture, pypiPostures []pypiPosture) ([]githubPosture, error) {
	requests := []githubURLPostureRequest{}
	for _, posture := range npmPostures {
		requests = append(requests, githubURLPostureRequest{
			provider: posture.Provider,
			name:     posture.Name,
			urls:     []string{posture.RepositoryURL},
		})
	}
	for _, posture := range cargoPostures {
		requests = append(requests, githubURLPostureRequest{
			provider: posture.Provider,
			name:     posture.Name,
			urls:     []string{posture.RepositoryURL},
		})
	}
	for _, posture := range pypiPostures {
		requests = append(requests, githubURLPostureRequest{
			provider: posture.Provider,
			name:     posture.Name,
			urls:     []string{posture.RepositoryURL, posture.ProjectURL},
		})
	}
	return githubPosturesFromURLRequests(ctx, client, apiBase, requests)
}

func githubPosturesFromURLRequests(ctx context.Context, client *http.Client, apiBase string, requests []githubURLPostureRequest) ([]githubPosture, error) {
	repoRequests := []githubRepoPostureRequest{}
	seen := map[string]bool{}
	for _, request := range requests {
		for _, rawURL := range request.urls {
			repo, ok := githubRepoFromAnyURL(rawURL)
			if !ok {
				continue
			}
			key := strings.ToLower(repo)
			if seen[key] {
				continue
			}
			seen[key] = true
			repoRequests = append(repoRequests, githubRepoPostureRequest{provider: request.provider, name: request.name, repo: repo})
			break
		}
	}
	return githubPosturesFromRepoRequests(ctx, client, apiBase, repoRequests)
}

func githubPosturesFromRepoRequests(ctx context.Context, client *http.Client, apiBase string, requests []githubRepoPostureRequest) ([]githubPosture, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	out := make([]githubPosture, len(requests))
	errs := make([]error, len(requests))
	workers := githubPostureFetchConcurrency
	if workers > len(requests) {
		workers = len(requests)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				request := requests[index]
				repo, err := fetchGitHubRepository(ctx, client, apiBase, request.repo)
				if err != nil {
					errs[index] = fmt.Errorf("%s: %w", request.repo, err)
					out[index] = githubPostureUnavailable(request.provider, request.name, request.repo, err)
					continue
				}
				out[index] = githubPostureFromRepo(request.provider, request.name, request.repo, repo)
			}
		}()
	}
	for index := range requests {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Repository < out[j].Repository
	})
	compactErrs := []error{}
	for _, err := range errs {
		if err != nil {
			compactErrs = append(compactErrs, err)
		}
	}
	return out, summarizeErrors(compactErrs, 3)
}

func githubRepoFromMiseName(name string) (string, bool) {
	raw, ok := strings.CutPrefix(name, "github:")
	if !ok {
		return "", false
	}
	raw, _, _ = strings.Cut(raw, "@")
	raw, _, _ = strings.Cut(raw, "#")
	raw = strings.Trim(raw, "/ ")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return "", false
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if !validGitHubPathPart(owner) || !validGitHubPathPart(repo) {
		return "", false
	}
	return owner + "/" + repo, true
}

func githubRepoFromAnyURL(rawURL string) (string, bool) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if !validGitHubPathPart(owner) || !validGitHubPathPart(repo) {
		return "", false
	}
	return owner + "/" + repo, true
}

func validGitHubPathPart(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func fetchGitHubRepository(ctx context.Context, client *http.Client, apiBase string, repository string) (githubRepository, error) {
	if repo, ok := loadCachedGitHubRepository(apiBase, repository); ok {
		return repo, nil
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRepository{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := githubToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return githubRepository{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return githubRepository{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubRepository{}, fmt.Errorf("github repository query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var repo githubRepository
	if err := json.Unmarshal(body, &repo); err != nil {
		return githubRepository{}, err
	}
	saveCachedGitHubRepository(apiBase, repository, repo)
	return repo, nil
}

type githubRepositoryCacheEntry struct {
	Version    int              `json:"version"`
	APIBase    string           `json:"api_base"`
	Repository string           `json:"repository"`
	CreatedAt  time.Time        `json:"created_at"`
	Repo       githubRepository `json:"repo"`
}

func loadCachedGitHubRepository(apiBase string, repository string) (githubRepository, bool) {
	path := githubRepositoryCachePath(apiBase, repository)
	if path == "" {
		return githubRepository{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return githubRepository{}, false
	}
	var entry githubRepositoryCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return githubRepository{}, false
	}
	if entry.Version != githubRepositoryCacheVersion || entry.APIBase != apiBase || !strings.EqualFold(entry.Repository, repository) {
		return githubRepository{}, false
	}
	if time.Since(entry.CreatedAt) > githubRepositoryCacheMaxAge {
		return githubRepository{}, false
	}
	return entry.Repo, true
}

func saveCachedGitHubRepository(apiBase string, repository string, repo githubRepository) {
	path := githubRepositoryCachePath(apiBase, repository)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	entry := githubRepositoryCacheEntry{
		Version:    githubRepositoryCacheVersion,
		APIBase:    apiBase,
		Repository: repository,
		CreatedAt:  time.Now(),
		Repo:       repo,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func githubRepositoryCachePath(apiBase string, repository string) string {
	dir := updevCacheDir()
	if dir == "" || apiBase == "" || repository == "" {
		return ""
	}
	key := updateSafetyCacheKey(apiBase, strings.ToLower(repository))
	return filepath.Join(dir, "security-metadata-v1", "github-repo", key+".json")
}

func queryGitHubAdvisories(ctx context.Context, client *http.Client, apiBase string, packages []securityPackage) ([]securityFinding, error) {
	findings := []securityFinding{}
	for _, pkg := range packages {
		ecosystem, ok := githubAdvisoryEcosystem(pkg.Ecosystem)
		if !ok || pkg.Package == "" || pkg.Version == "" {
			continue
		}
		for _, advisoryType := range []string{"reviewed", "malware"} {
			advisories, err := fetchGitHubAdvisories(ctx, client, apiBase, ecosystem, pkg.Package, pkg.Version, advisoryType)
			if err != nil {
				return findings, err
			}
			for _, advisory := range advisories {
				finding := githubAdvisoryFinding(pkg, advisory)
				findings = append(findings, finding)
			}
		}
	}
	return findings, nil
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

func fetchGitHubAdvisories(ctx context.Context, client *http.Client, apiBase string, ecosystem string, packageName string, version string, advisoryType string) ([]githubAdvisory, error) {
	endpoint, err := neturl.Parse(strings.TrimRight(apiBase, "/") + "/advisories")
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
	if token := githubToken(); token != "" {
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
	var advisories []githubAdvisory
	if err := json.Unmarshal(body, &advisories); err != nil {
		return nil, err
	}
	return advisories, nil
}

func githubAdvisoryFinding(pkg securityPackage, advisory githubAdvisory) securityFinding {
	finding := securityFinding{
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
		Exposure:      securityExposureFromPackage(pkg),
		Decision:      "hold",
		Confidence:    pkg.Confidence,
		Reason:        githubAdvisoryReason(advisory),
		Status:        plan.StatusHeld,
		URL:           advisory.HTMLURL,
	}
	finding.Remediation = securityFindingRemediation(finding)
	return finding
}

func githubAdvisoryAliases(advisory githubAdvisory) []string {
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

func githubAdvisoryReason(advisory githubAdvisory) string {
	if strings.EqualFold(advisory.Type, "malware") {
		return "GitHub Advisory malware match"
	}
	return "GitHub Advisory vulnerability match"
}

func isGitHubAdvisoryFinding(finding securityFinding) bool {
	return strings.HasPrefix(finding.Reason, "GitHub Advisory") ||
		strings.Contains(finding.URL, "github.com/advisories/")
}

func fixedVersionsFromGitHubAdvisory(advisory githubAdvisory, pkg securityPackage) []string {
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

func githubPostureFromRepo(provider string, name string, fallbackRepository string, repo githubRepository) githubPosture {
	repository := repo.FullName
	if repository == "" {
		repository = fallbackRepository
	}
	posture := githubPosture{
		Provider:                     provider,
		Name:                         name,
		Repository:                   repository,
		URL:                          repo.HTMLURL,
		DefaultBranch:                repo.DefaultBranch,
		Private:                      repo.Private,
		Fork:                         repo.Fork,
		Archived:                     repo.Archived,
		Disabled:                     repo.Disabled,
		CreatedAt:                    repo.CreatedAt,
		PushedAt:                     repo.PushedAt,
		UpdatedAt:                    repo.UpdatedAt,
		OpenIssuesCount:              repo.OpenIssuesCount,
		StargazersCount:              repo.StargazersCount,
		AdvancedSecurity:             githubSecurityStatusValue(repo.SecurityAndAnalysis.AdvancedSecurity),
		SecretScanning:               githubSecurityStatusValue(repo.SecurityAndAnalysis.SecretScanning),
		SecretScanningPushProtection: githubSecurityStatusValue(repo.SecurityAndAnalysis.SecretScanningPushProtection),
		DependabotSecurityUpdates:    githubSecurityStatusValue(repo.SecurityAndAnalysis.DependabotSecurityUpdates),
		Decision:                     "allow",
		Confidence:                   "medium",
	}
	switch {
	case repo.Disabled:
		posture.Decision = "review"
		posture.Reason = "repository is disabled"
	case repo.Archived:
		posture.Decision = "review"
		posture.Reason = "repository is archived"
	case repo.Private:
		posture.Decision = "review"
		posture.Reason = "repository is private"
	case posture.DependabotSecurityUpdates == "disabled":
		posture.Decision = "review"
		posture.Reason = "Dependabot security updates are disabled"
	case posture.SecretScanning == "disabled":
		posture.Decision = "review"
		posture.Reason = "secret scanning is disabled"
	case posture.SecretScanningPushProtection == "disabled":
		posture.Decision = "review"
		posture.Reason = "secret scanning push protection is disabled"
	}
	posture = applyGitHubTapRepositoryAge(posture, repo.CreatedAt, minHomebrewTapRepositoryAge())
	posture.Remediation = githubPostureRemediation(posture)
	return posture
}

func applyGitHubTapRepositoryAge(posture githubPosture, createdAt string, minAge time.Duration) githubPosture {
	if posture.Provider != "brew" || !strings.HasPrefix(posture.Name, "tap:") || minAge <= 0 {
		return posture
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	if err != nil {
		return posture
	}
	age := time.Since(created)
	posture.CreatedAt = created.Format(time.RFC3339)
	posture.RepositoryAgeDays = int(age.Hours() / 24)
	posture.MinRepositoryAgeDays = int(minAge.Hours() / 24)
	posture.Evidence = appendEvidence(posture.Evidence, "GitHub repository age")
	if age < minAge {
		posture.Decision = "review"
		posture.Confidence = "medium"
		posture.Reason = fmt.Sprintf("tap repository is newly created: age %d days, minimum %d days", posture.RepositoryAgeDays, posture.MinRepositoryAgeDays)
	}
	return posture
}

func githubPostureRemediation(posture githubPosture) string {
	if !securityDecisionNeedsAttention(posture.Decision) {
		return ""
	}
	switch {
	case posture.Disabled:
		return "replace the disabled repository source or add a temporary policy override after review"
	case posture.Archived:
		return "replace the archived repository source or add a temporary policy override after review"
	case posture.Private:
		return "verify private repository access and provenance before keeping this dependency"
	case posture.DependabotSecurityUpdates == "disabled":
		return "enable Dependabot security updates on the source repository or account for missing upstream security automation"
	case posture.SecretScanning == "disabled":
		return "enable secret scanning on the source repository or account for missing upstream secret detection"
	case posture.SecretScanningPushProtection == "disabled":
		return "enable secret scanning push protection on the source repository or account for missing push-time secret blocking"
	case posture.RepositoryAgeDays > 0 && posture.MinRepositoryAgeDays > 0 && posture.RepositoryAgeDays < posture.MinRepositoryAgeDays:
		return "wait until the repository reaches the minimum age or add a temporary policy override after review"
	default:
		return "review repository posture and add a temporary policy override only with reason and expiry"
	}
}

func minHomebrewTapRepositoryAge() time.Duration {
	return minHomebrewTapRepositoryAgeWithConfig(loadUpdevConfig())
}

func minHomebrewTapRepositoryAgeWithConfig(config updevConfig) time.Duration {
	days := 30
	if config.Security.Homebrew.MinTapAgeDays != nil && *config.Security.Homebrew.MinTapAgeDays >= 0 {
		days = *config.Security.Homebrew.MinTapAgeDays
	}
	if value := strings.TrimSpace(os.Getenv("UPDEV_HOMEBREW_MIN_TAP_AGE_DAYS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			days = parsed
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

func githubSecurityStatusValue(feature githubSecurityFeature) string {
	return strings.ToLower(strings.TrimSpace(feature.Status))
}

func githubPostureUnavailable(provider string, name string, repository string, err error) githubPosture {
	return githubPosture{
		Provider:    provider,
		Name:        name,
		Repository:  repository,
		URL:         "https://github.com/" + repository,
		Decision:    "review",
		Confidence:  "medium",
		Reason:      "repository metadata unavailable: " + err.Error(),
		Remediation: "retry when GitHub metadata is reachable or review the repository manually before adding a policy override",
	}
}

func githubAPIURL() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_GITHUB_API_URL")); value != "" {
		return value
	}
	return defaultGitHubAPIURL
}

func githubToken() string {
	for _, name := range []string{"UPDEV_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	githubCLITokenCache.Do(func() {
		githubCLITokenCache.value = githubTokenFromCLI()
	})
	return githubCLITokenCache.value
}

func githubTokenFromCLI() string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func hasGitHubPostureReview(postures []githubPosture) bool {
	return githubPostureReviewCount(postures) > 0
}

func githubPostureReviewCount(postures []githubPosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}

func summarizeErrors(errs []error, limit int) error {
	if len(errs) == 0 {
		return nil
	}
	if limit <= 0 || len(errs) <= limit {
		return errors.Join(errs...)
	}
	parts := make([]string, 0, limit+1)
	for _, err := range errs[:limit] {
		parts = append(parts, err.Error())
	}
	parts = append(parts, fmt.Sprintf("... %d more", len(errs)-limit))
	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}
