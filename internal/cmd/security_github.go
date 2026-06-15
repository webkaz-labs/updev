package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const (
	defaultGitHubAPIURL          = "https://api.github.com"
	githubRepositoryCacheMaxAge  = 6 * time.Hour
	githubRepositoryCacheVersion = 1
)

var githubCLITokenCache struct {
	sync.Once
	value string
}

type (
	githubPosture             = githubrepo.Posture
	githubRepository          = githubrepo.Repository
	githubSecurityAndAnalysis = githubrepo.SecurityAndAnalysis
	githubSecurityFeature     = githubrepo.SecurityFeature
)

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
		repo, ok := githubrepo.RepoFromMiseName(item.Name)
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
		repo, ok := githubrepo.RepoFromAnyURL(posture.URL)
		if !ok {
			repo, ok = githubrepo.RepoFromAnyURL(posture.Homepage)
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
		repo, ok := githubrepo.RepoFromAnyURL(posture.RepositoryURL)
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
			repo, ok := githubrepo.RepoFromAnyURL(rawURL)
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
				out[index] = githubrepo.PostureFromRepository(request.provider, request.name, request.repo, repo, minHomebrewTapRepositoryAge())
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

func fetchGitHubRepository(ctx context.Context, client *http.Client, apiBase string, repository string) (githubRepository, error) {
	if repo, ok := loadCachedGitHubRepository(apiBase, repository); ok {
		return repo, nil
	}
	repo, err := githubrepo.FetchRepository(ctx, client, apiBase, githubToken(), repository)
	if err != nil {
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
	if strings.TrimSpace(apiBase) == "" || strings.TrimSpace(repository) == "" {
		return ""
	}
	key := updateSafetyCacheKey(apiBase, strings.ToLower(repository))
	return updevpath.SecurityMetadataCacheFile("github-repo", key)
}

func minHomebrewTapRepositoryAge() time.Duration {
	return minHomebrewTapRepositoryAgeWithConfig(loadUpdevConfig())
}

func minHomebrewTapRepositoryAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(30, config.Security.Homebrew.MinTapAgeDays, "UPDEV_HOMEBREW_MIN_TAP_AGE_DAYS")
	return time.Duration(days) * 24 * time.Hour
}

func githubPostureUnavailable(provider string, name string, repository string, err error) githubPosture {
	return githubrepo.PostureUnavailable(provider, name, repository, err)
}

func githubAPIURL() string {
	return configuredEnvString(defaultGitHubAPIURL, "UPDEV_GITHUB_API_URL")
}

func githubToken() string {
	for _, name := range []string{"UPDEV_GITHUB_TOKEN", "GITHUB_API_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
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
