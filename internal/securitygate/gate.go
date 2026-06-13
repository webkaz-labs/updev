package securitygate

import (
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
)

type Gate struct {
	Provider string      `json:"provider"`
	Status   plan.Status `json:"status"`
	Summary  *Summary    `json:"summary,omitempty"`
	Error    string      `json:"error,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	Evidence []string    `json:"evidence,omitempty"`
	Findings []Finding   `json:"findings,omitempty"`
}

type Summary struct {
	Findings int `json:"findings"`
	Allow    int `json:"allow,omitempty"`
	Review   int `json:"review,omitempty"`
	Hold     int `json:"hold,omitempty"`
	Block    int `json:"block,omitempty"`
	Unknown  int `json:"unknown,omitempty"`
}

type Finding struct {
	Provider          string            `json:"provider"`
	Kind              string            `json:"kind"`
	Name              string            `json:"name"`
	InstalledVersions []string          `json:"installed_versions,omitempty"`
	CurrentVersion    string            `json:"current_version,omitempty"`
	Decision          string            `json:"decision"`
	Reason            string            `json:"reason"`
	ReasonCode        string            `json:"reason_code,omitempty"`
	ReasonArgs        map[string]string `json:"reason_args,omitempty"`
	Remediation       string            `json:"remediation,omitempty"`
	Evidence          []string          `json:"evidence,omitempty"`
	Source            string            `json:"source,omitempty"`
	Tap               string            `json:"tap,omitempty"`
	Publisher         string            `json:"publisher,omitempty"`
	PublisherVerified *bool             `json:"publisher_verified,omitempty"`
	ExecutesCode      bool              `json:"executes_code,omitempty"`
	RepositoryURL     string            `json:"repository_url,omitempty"`
	SupportURL        string            `json:"support_url,omitempty"`
	LastUpdated       string            `json:"last_updated,omitempty"`
	PublishedDate     string            `json:"published_date,omitempty"`
	Flags             string            `json:"flags,omitempty"`
	InstallCount      float64           `json:"install_count,omitempty"`
	AverageRating     float64           `json:"average_rating,omitempty"`
	Homepage          string            `json:"homepage,omitempty"`
	URL               string            `json:"url,omitempty"`
	HomepageHost      string            `json:"homepage_host,omitempty"`
	URLHost           string            `json:"url_host,omitempty"`
	HostMatched       bool              `json:"host_matched,omitempty"`
	Version           string            `json:"version,omitempty"`
	Deprecated        bool              `json:"deprecated,omitempty"`
	Disabled          bool              `json:"disabled,omitempty"`
	SkipLivecheck     bool              `json:"skip_livecheck,omitempty"`
	Autobump          bool              `json:"autobump,omitempty"`
	Confidence        string            `json:"confidence,omitempty"`
	TrustStatus       string            `json:"trust_status,omitempty"`
	TrustTarget       string            `json:"trust_target,omitempty"`
	TrustCommand      string            `json:"trust_command,omitempty"`
	TrustCommandArgv  []string          `json:"trust_command_argv,omitempty"`
	ReleaseDate       string            `json:"release_date,omitempty"`
	ReleaseAgeDays    int               `json:"release_age_days,omitempty"`
	MinReleaseAgeDays int               `json:"min_release_age_days,omitempty"`
	AdvisoryIDs       []string          `json:"advisory_ids,omitempty"`
	FixedVersions     []string          `json:"fixed_versions,omitempty"`
}

func SummaryFromFindings(findings []Finding) *Summary {
	if len(findings) == 0 {
		return nil
	}
	summary := Summary{Findings: len(findings)}
	for _, finding := range findings {
		switch strings.ToLower(strings.TrimSpace(finding.Decision)) {
		case "allow":
			summary.Allow++
		case "review":
			summary.Review++
		case "hold":
			summary.Hold++
		case "block":
			summary.Block++
		default:
			summary.Unknown++
		}
	}
	return &summary
}

func ApplyFindings(gate Gate, findings []Finding) Gate {
	gate.Findings = findings
	gate.Summary = SummaryFromFindings(findings)
	if gate.Status != plan.StatusOK {
		return gate
	}
	for _, finding := range findings {
		if DecisionNeedsAttention(finding.Decision) {
			gate.Status = plan.StatusHeld
			break
		}
	}
	return gate
}

func ValidDecision(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow", "review", "hold", "block":
		return true
	default:
		return false
	}
}

func DecisionNeedsAttention(decision string) bool {
	return !strings.EqualFold(strings.TrimSpace(decision), "allow")
}

func DecisionPriority(decision string) int {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "block":
		return 4
	case "hold":
		return 3
	case "review":
		return 2
	case "unknown":
		return 1
	default:
		return 0
	}
}
