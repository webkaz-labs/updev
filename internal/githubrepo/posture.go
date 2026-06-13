package githubrepo

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/securityreason"
)

type Posture struct {
	Provider                     string            `json:"provider"`
	Name                         string            `json:"name"`
	Repository                   string            `json:"repository"`
	URL                          string            `json:"url,omitempty"`
	DefaultBranch                string            `json:"default_branch,omitempty"`
	Private                      bool              `json:"private"`
	Fork                         bool              `json:"fork"`
	Archived                     bool              `json:"archived"`
	Disabled                     bool              `json:"disabled"`
	CreatedAt                    string            `json:"created_at,omitempty"`
	PushedAt                     string            `json:"pushed_at,omitempty"`
	UpdatedAt                    string            `json:"updated_at,omitempty"`
	RepositoryAgeDays            int               `json:"repository_age_days,omitempty"`
	MinRepositoryAgeDays         int               `json:"min_repository_age_days,omitempty"`
	OpenIssuesCount              int               `json:"open_issues_count"`
	StargazersCount              int               `json:"stargazers_count"`
	AdvancedSecurity             string            `json:"advanced_security,omitempty"`
	SecretScanning               string            `json:"secret_scanning,omitempty"`
	SecretScanningPushProtection string            `json:"secret_scanning_push_protection,omitempty"`
	DependabotSecurityUpdates    string            `json:"dependabot_security_updates,omitempty"`
	Decision                     string            `json:"decision"`
	Confidence                   string            `json:"confidence"`
	Reason                       string            `json:"reason,omitempty"`
	ReasonCode                   string            `json:"reason_code,omitempty"`
	ReasonArgs                   map[string]string `json:"reason_args,omitempty"`
	Remediation                  string            `json:"remediation,omitempty"`
	Evidence                     []string          `json:"evidence,omitempty"`
}

type Repository struct {
	FullName            string              `json:"full_name"`
	HTMLURL             string              `json:"html_url"`
	DefaultBranch       string              `json:"default_branch"`
	Private             bool                `json:"private"`
	Fork                bool                `json:"fork"`
	Archived            bool                `json:"archived"`
	Disabled            bool                `json:"disabled"`
	CreatedAt           string              `json:"created_at"`
	PushedAt            string              `json:"pushed_at"`
	UpdatedAt           string              `json:"updated_at"`
	OpenIssuesCount     int                 `json:"open_issues_count"`
	StargazersCount     int                 `json:"stargazers_count"`
	SecurityAndAnalysis SecurityAndAnalysis `json:"security_and_analysis"`
}

type SecurityAndAnalysis struct {
	AdvancedSecurity             SecurityFeature `json:"advanced_security"`
	SecretScanning               SecurityFeature `json:"secret_scanning"`
	SecretScanningPushProtection SecurityFeature `json:"secret_scanning_push_protection"`
	DependabotSecurityUpdates    SecurityFeature `json:"dependabot_security_updates"`
}

type SecurityFeature struct {
	Status string `json:"status"`
}

func RepoFromMiseName(name string) (string, bool) {
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
	if !ValidPathPart(owner) || !ValidPathPart(repo) {
		return "", false
	}
	return owner + "/" + repo, true
}

func RepoFromAnyURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if !ValidPathPart(owner) || !ValidPathPart(repo) {
		return "", false
	}
	return owner + "/" + repo, true
}

func ValidPathPart(value string) bool {
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

func PostureFromRepository(provider string, name string, fallbackRepository string, repo Repository, minTapAge time.Duration) Posture {
	repository := repo.FullName
	if repository == "" {
		repository = fallbackRepository
	}
	posture := Posture{
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
		AdvancedSecurity:             SecurityStatusValue(repo.SecurityAndAnalysis.AdvancedSecurity),
		SecretScanning:               SecurityStatusValue(repo.SecurityAndAnalysis.SecretScanning),
		SecretScanningPushProtection: SecurityStatusValue(repo.SecurityAndAnalysis.SecretScanningPushProtection),
		DependabotSecurityUpdates:    SecurityStatusValue(repo.SecurityAndAnalysis.DependabotSecurityUpdates),
		Decision:                     "allow",
		Confidence:                   "medium",
	}
	switch {
	case repo.Disabled:
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubRepositoryDisabled, "repository is disabled", nil)
	case repo.Archived:
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubRepositoryArchived, "repository is archived", nil)
	case repo.Private:
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubRepositoryPrivate, "repository is private", nil)
	case posture.DependabotSecurityUpdates == "disabled":
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubDependabotDisabled, "Dependabot security updates are disabled", nil)
	case posture.SecretScanning == "disabled":
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubSecretScanningDisabled, "secret scanning is disabled", nil)
	case posture.SecretScanningPushProtection == "disabled":
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.GitHubPushProtectionDisabled, "secret scanning push protection is disabled", nil)
	}
	posture = ApplyTapRepositoryAge(posture, repo.CreatedAt, minTapAge)
	posture.Remediation = Remediation(posture)
	return posture
}

func ApplyTapRepositoryAge(posture Posture, createdAt string, minAge time.Duration) Posture {
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
		text := fmt.Sprintf("tap repository is newly created: age %d days, minimum %d days", posture.RepositoryAgeDays, posture.MinRepositoryAgeDays)
		setPostureReason(&posture, securityreason.GitHubRepositoryTooNew, text, map[string]string{
			"age_days":     fmt.Sprintf("%d", posture.RepositoryAgeDays),
			"min_age_days": fmt.Sprintf("%d", posture.MinRepositoryAgeDays),
		})
	}
	return posture
}

func Remediation(posture Posture) string {
	if !needsAttention(posture.Decision) {
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

func SecurityStatusValue(feature SecurityFeature) string {
	return strings.ToLower(strings.TrimSpace(feature.Status))
}

func PostureUnavailable(provider string, name string, repository string, err error) Posture {
	posture := Posture{
		Provider:    provider,
		Name:        name,
		Repository:  repository,
		URL:         "https://github.com/" + repository,
		Decision:    "review",
		Confidence:  "medium",
		Reason:      "repository metadata unavailable: " + err.Error(),
		Remediation: "retry when GitHub metadata is reachable or review the repository manually before adding a policy override",
	}
	setPostureReason(&posture, securityreason.GitHubMetadataUnavailable, posture.Reason, nil)
	return posture
}

func setPostureReason(posture *Posture, code string, text string, args map[string]string) {
	if posture == nil {
		return
	}
	reason := securityreason.GitHubPostureReason(code, posture.Repository, text, args)
	posture.Reason = reason.Text
	posture.ReasonCode = reason.Code
	posture.ReasonArgs = reason.Args
}

func appendEvidence(evidence []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return evidence
	}
	for _, existing := range evidence {
		if existing == value {
			return evidence
		}
	}
	return append(evidence, value)
}

func needsAttention(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "", "allow", "ok":
		return false
	default:
		return true
	}
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			unescaped, err := url.PathUnescape(part)
			if err == nil {
				part = unescaped
			}
			out = append(out, part)
		}
	}
	return out
}
