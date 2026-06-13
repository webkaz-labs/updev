package githubrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

type Release struct {
	PublishedAt string `json:"published_at"`
	CreatedAt   string `json:"created_at"`
}

type gitObject struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

type gitRef struct {
	Ref    string    `json:"ref"`
	Object gitObject `json:"object"`
}

type gitIdentity struct {
	Date string `json:"date"`
}

type gitTag struct {
	Tagger gitIdentity `json:"tagger"`
	Object gitObject   `json:"object"`
}

type gitCommit struct {
	Author    gitIdentity `json:"author"`
	Committer gitIdentity `json:"committer"`
}

func fetchReleaseByTag(ctx context.Context, client *http.Client, apiBase string, token string, repository string, tag string) (Release, error) {
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/releases/tags/" + neturl.PathEscape(tag)
	var release Release
	if err := fetchJSON(ctx, client, endpoint, token, &release); err != nil {
		return Release{}, err
	}
	return release, nil
}

func FetchReleaseOrTagByTag(ctx context.Context, client *http.Client, apiBase string, token string, repository string, tag string, inferred bool) (Release, string, error) {
	release, releaseErr := fetchReleaseByTag(ctx, client, apiBase, token, repository, tag)
	if releaseErr == nil {
		if inferred {
			return release, "GitHub inferred release metadata", nil
		}
		return release, "GitHub release metadata", nil
	}
	release, tagErr := fetchTagDateByRef(ctx, client, apiBase, token, repository, tag)
	if tagErr == nil {
		if inferred {
			return release, "GitHub inferred tag metadata", nil
		}
		return release, "GitHub tag metadata", nil
	}
	return Release{}, "", fmt.Errorf("%w; tag metadata fallback failed: %v", releaseErr, tagErr)
}

func fetchTagDateByRef(ctx context.Context, client *http.Client, apiBase string, token string, repository string, tag string) (Release, error) {
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/ref/tags/" + neturl.PathEscape(tag)
	var ref gitRef
	if err := fetchJSON(ctx, client, endpoint, token, &ref); err != nil {
		return Release{}, err
	}
	switch ref.Object.Type {
	case "tag":
		return fetchAnnotatedTagDate(ctx, client, apiBase, token, repository, ref.Object.SHA)
	case "commit":
		return fetchCommitDate(ctx, client, apiBase, token, repository, ref.Object.SHA)
	default:
		return Release{}, fmt.Errorf("unsupported github tag object type: %s", ref.Object.Type)
	}
}

func fetchAnnotatedTagDate(ctx context.Context, client *http.Client, apiBase string, token string, repository string, sha string) (Release, error) {
	if sha == "" {
		return Release{}, fmt.Errorf("github tag object sha is empty")
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/tags/" + neturl.PathEscape(sha)
	var tag gitTag
	if err := fetchJSON(ctx, client, endpoint, token, &tag); err != nil {
		return Release{}, err
	}
	if strings.TrimSpace(tag.Tagger.Date) != "" {
		return Release{CreatedAt: tag.Tagger.Date}, nil
	}
	if tag.Object.Type == "commit" && tag.Object.SHA != "" {
		return fetchCommitDate(ctx, client, apiBase, token, repository, tag.Object.SHA)
	}
	return Release{}, fmt.Errorf("github annotated tag date is empty")
}

func fetchCommitDate(ctx context.Context, client *http.Client, apiBase string, token string, repository string, sha string) (Release, error) {
	if sha == "" {
		return Release{}, fmt.Errorf("github commit object sha is empty")
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository + "/git/commits/" + neturl.PathEscape(sha)
	var commit gitCommit
	if err := fetchJSON(ctx, client, endpoint, token, &commit); err != nil {
		return Release{}, err
	}
	date := firstNonEmpty(commit.Committer.Date, commit.Author.Date)
	if strings.TrimSpace(date) == "" {
		return Release{}, fmt.Errorf("github commit date is empty")
	}
	return Release{CreatedAt: date}, nil
}

func ParseReleaseTime(release Release) (time.Time, error) {
	for _, value := range []string{release.PublishedAt, release.CreatedAt} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("published_at and created_at are empty")
}

func FetchRepository(ctx context.Context, client *http.Client, apiBase string, token string, repository string) (Repository, error) {
	endpoint := strings.TrimRight(apiBase, "/") + "/repos/" + repository
	var repo Repository
	if err := fetchJSONWithLimit(ctx, client, endpoint, token, 2*1024*1024, "github repository query failed", &repo); err != nil {
		return Repository{}, err
	}
	return repo, nil
}

func fetchJSON(ctx context.Context, client *http.Client, endpoint string, token string, out any) error {
	return fetchJSONWithLimit(ctx, client, endpoint, token, 1024*1024, "github query failed", out)
}

func fetchJSONWithLimit(ctx context.Context, client *http.Client, endpoint string, token string, bodyLimit int64, errorPrefix string, out any) error {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token = strings.TrimSpace(token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, bodyLimit))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d: %s", errorPrefix, response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
