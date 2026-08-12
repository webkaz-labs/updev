package brew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

type Posture struct {
	Provider         string            `json:"provider"`
	Kind             string            `json:"kind"`
	Name             string            `json:"name"`
	Tap              string            `json:"tap,omitempty"`
	Homepage         string            `json:"homepage,omitempty"`
	URL              string            `json:"url,omitempty"`
	HomepageHost     string            `json:"homepage_host,omitempty"`
	URLHost          string            `json:"url_host,omitempty"`
	HostMatched      bool              `json:"host_matched,omitempty"`
	Version          string            `json:"version,omitempty"`
	TrustStatus      string            `json:"trust_status,omitempty"`
	TrustTarget      string            `json:"trust_target,omitempty"`
	TrustCommand     string            `json:"trust_command,omitempty"`
	TrustCommandArgv []string          `json:"trust_command_argv,omitempty"`
	Deprecated       bool              `json:"deprecated"`
	Disabled         bool              `json:"disabled"`
	SkipLivecheck    bool              `json:"skip_livecheck"`
	Autobump         bool              `json:"autobump"`
	Decision         string            `json:"decision"`
	Confidence       string            `json:"confidence"`
	Reason           string            `json:"reason,omitempty"`
	ReasonCode       string            `json:"reason_code,omitempty"`
	ReasonArgs       map[string]string `json:"reason_args,omitempty"`
	Remediation      string            `json:"remediation,omitempty"`
	Evidence         []string          `json:"evidence,omitempty"`
}

type Metadata struct {
	Name              []string         `json:"name"`
	FullName          string           `json:"full_name"`
	Token             string           `json:"token"`
	FullToken         string           `json:"full_token"`
	Tap               string           `json:"tap"`
	Homepage          string           `json:"homepage"`
	URL               string           `json:"url"`
	Version           string           `json:"version"`
	Versions          MetadataVersions `json:"versions"`
	URLs              MetadataURLs     `json:"urls"`
	Deprecated        bool             `json:"deprecated"`
	Disabled          bool             `json:"disabled"`
	DeprecationDate   string           `json:"deprecation_date"`
	DeprecationReason string           `json:"deprecation_reason"`
	DisableDate       string           `json:"disable_date"`
	DisableReason     string           `json:"disable_reason"`
	SkipLivecheck     bool             `json:"skip_livecheck"`
	Autobump          bool             `json:"autobump"`
}

type MetadataVersions struct {
	Stable string `json:"stable"`
}

type MetadataURLs struct {
	Stable MetadataURL `json:"stable"`
}

type MetadataURL struct {
	URL string `json:"url"`
}

type CaskProvenanceVerdict struct {
	Decision    string
	Confidence  string
	Reason      string
	ReasonCode  string
	Remediation string
	Evidence    string
}

func (metadata *Metadata) UnmarshalJSON(data []byte) error {
	type rawMetadata Metadata
	var raw struct {
		rawMetadata
		Name json.RawMessage `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*metadata = Metadata(raw.rawMetadata)
	if len(raw.Name) == 0 {
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw.Name, &many); err == nil {
		metadata.Name = many
		return nil
	}
	var one string
	if err := json.Unmarshal(raw.Name, &one); err == nil {
		metadata.Name = []string{one}
		return nil
	}
	return nil
}

func PosturesFromItems(ctx context.Context, client *http.Client, apiBase string, items []plan.Item, entryFor func(kind string, name string) ManifestEntry) ([]Posture, error) {
	requests := []plan.Item{}
	tapPostures := []Posture{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Provider == "brew" && item.Kind == "tap" {
			key := item.Kind + ":" + item.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			tapPostures = append(tapPostures, TapPosture(item))
			continue
		}
		if item.Provider != "brew" || (item.Kind != "brew" && item.Kind != "cask") {
			continue
		}
		key := item.Kind + ":" + item.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		requests = append(requests, item)
	}
	postures := make([]Posture, 0, len(requests)+len(tapPostures))
	postures = append(postures, tapPostures...)
	errs := []error{}
	for _, item := range requests {
		entry := ManifestEntry{}
		if entryFor != nil {
			entry = entryFor(item.Kind, item.Name)
		}
		if entry.URLBased {
			postures = append(postures, ManifestPosture(item, entry, "URL-based Homebrew cask needs manual provenance review"))
			continue
		}
		if entry.Tap != "" && !OfficialTap(entry.Tap) {
			postures = append(postures, ManifestPosture(item, entry, "non-official Homebrew tap needs provenance review"))
			continue
		}
		metadata, err := FetchMetadata(ctx, client, apiBase, item.Kind, item.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s:%s: %w", item.Kind, item.Name, err))
			postures = append(postures, MetadataUnavailable(item, entry, err))
			continue
		}
		postures = append(postures, PostureFromMetadata(item, entry, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		if postures[i].Kind != postures[j].Kind {
			return postures[i].Kind < postures[j].Kind
		}
		return postures[i].Name < postures[j].Name
	})
	return postures, errors.Join(errs...)
}

func TapPosture(item plan.Item) Posture {
	rawTap := strings.TrimSpace(item.Name)
	url := TapGitHubURL(rawTap)
	posture := Posture{
		Provider:   "brew",
		Kind:       "tap",
		Name:       rawTap,
		Tap:        rawTap,
		URL:        url,
		Decision:   "allow",
		Confidence: "medium",
		Evidence:   []string{"Brewfile tap"},
	}
	if url != "" {
		posture.Evidence = appendEvidence(posture.Evidence, "inferred Homebrew tap GitHub repository")
	}
	if !OfficialTap(rawTap) {
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.TrustStatus = "needs-review"
		posture.TrustTarget = rawTap
		posture.TrustCommandArgv = TrustCommandArgv("tap", rawTap)
		posture.TrustCommand = JoinCommand(posture.TrustCommandArgv)
		setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewNonOfficialTap, "tap", rawTap, "non-official Homebrew tap needs provenance review", nil))
		posture.Remediation = "review the tap repository provenance; prefer item-scoped trust for packages, or run " + posture.TrustCommand + " only if the whole tap is trusted"
	}
	return posture
}

func TapGitHubURL(tap string) string {
	parts := strings.Split(strings.TrimSpace(tap), "/")
	if len(parts) != 2 || !validGitHubPathPart(parts[0]) || !validGitHubPathPart(parts[1]) {
		return ""
	}
	return "https://github.com/" + parts[0] + "/homebrew-" + parts[1]
}

func FetchMetadata(ctx context.Context, client *http.Client, apiBase string, kind string, name string) (Metadata, error) {
	kindPath := "formula"
	if kind == "cask" {
		kindPath = "cask"
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/" + kindPath + "/" + url.PathEscape(name) + ".json"
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Metadata{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Metadata{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return Metadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, fmt.Errorf("homebrew metadata query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(body)), 180))
	}
	var metadata Metadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func PostureFromMetadata(item plan.Item, entry ManifestEntry, metadata Metadata) Posture {
	name := firstNonEmpty(metadata.Token, firstString(metadata.Name), item.Name)
	tap := firstNonEmpty(metadata.Tap, entry.Tap)
	version := firstNonEmpty(metadata.Version, metadata.Versions.Stable)
	downloadURL := firstNonEmpty(metadata.URL, metadata.URLs.Stable.URL)
	homepageHost := hostFromURL(metadata.Homepage)
	urlHost := hostFromURL(downloadURL)
	posture := Posture{
		Provider:      "brew",
		Kind:          item.Kind,
		Name:          name,
		Tap:           tap,
		Homepage:      metadata.Homepage,
		URL:           downloadURL,
		HomepageHost:  homepageHost,
		URLHost:       urlHost,
		HostMatched:   homepageHost != "" && urlHost != "" && homepageHost == urlHost,
		Version:       version,
		Deprecated:    metadata.Deprecated,
		Disabled:      metadata.Disabled,
		SkipLivecheck: metadata.SkipLivecheck,
		Autobump:      metadata.Autobump,
		Decision:      "allow",
		Confidence:    "medium",
	}
	if item.Kind == "brew" {
		setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewOfficialFormula, item.Kind, name, "official Homebrew formula metadata is available and not disabled or deprecated", nil))
	}
	switch {
	case metadata.Disabled:
		posture.Decision = "review"
		reasonText := firstNonEmpty(metadata.DisableReason, "Homebrew metadata marks this entry disabled")
		setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewEntryDisabled, item.Kind, name, reasonText, map[string]string{"reason_text": reasonText}))
		posture.Remediation = "remove or replace the disabled Homebrew entry before updating"
	case metadata.Deprecated:
		posture.Decision = "review"
		reasonText := firstNonEmpty(metadata.DeprecationReason, "Homebrew metadata marks this entry deprecated")
		setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewEntryDeprecated, item.Kind, name, reasonText, map[string]string{"reason_text": reasonText}))
		posture.Remediation = "replace the deprecated Homebrew entry or add a temporary policy override after review"
	case item.Kind == "cask":
		verdict := CaskProvenanceVerdictFor(homepageHost, urlHost, version)
		posture.Decision = verdict.Decision
		posture.Confidence = verdict.Confidence
		setPostureReason(&posture, securityreason.HomebrewPostureReason(verdict.ReasonCode, "cask", name, verdict.Reason, map[string]string{
			"homepage_host": homepageHost,
			"url_host":      urlHost,
		}))
		posture.Remediation = verdict.Remediation
		posture.Evidence = appendEvidence(posture.Evidence, verdict.Evidence)
	}
	return posture
}

func ManifestPosture(item plan.Item, entry ManifestEntry, reason string) Posture {
	name := firstNonEmpty(entry.RawName, item.Name)
	posture := Posture{
		Provider:    "brew",
		Kind:        item.Kind,
		Name:        name,
		Tap:         entry.Tap,
		Decision:    "review",
		Confidence:  "low",
		Remediation: "review the Brewfile entry provenance and add a temporary policy override only with reason and expiry",
	}
	setPostureReason(&posture, securityreason.Infer(reason))
	if posture.ReasonCode == "" {
		setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewCaskProvenanceReview, item.Kind, name, reason, nil))
	}
	if entry.Tap != "" && !OfficialTap(entry.Tap) {
		trustKind := "formula"
		if item.Kind == "cask" {
			trustKind = "cask"
		}
		posture.TrustStatus = "needs-review"
		posture.TrustTarget = name
		posture.TrustCommandArgv = TrustCommandArgv(trustKind, name)
		posture.TrustCommand = JoinCommand(posture.TrustCommandArgv)
		posture.Remediation = "review the Brewfile entry provenance, then prefer item-scoped trust with " + posture.TrustCommand + "; trust the whole tap only when you accept all current and future entries"
		posture.Evidence = appendEvidence(posture.Evidence, "Homebrew 6 tap trust target: "+trustKind+" "+name)
	}
	return posture
}

func MetadataUnavailable(item plan.Item, entry ManifestEntry, err error) Posture {
	name := firstNonEmpty(entry.RawName, item.Name)
	posture := Posture{
		Provider:    "brew",
		Kind:        item.Kind,
		Name:        name,
		Tap:         entry.Tap,
		Decision:    "review",
		Confidence:  "low",
		Remediation: "retry when Homebrew metadata is reachable or review the entry manually before adding a policy override",
	}
	setPostureReason(&posture, securityreason.HomebrewPostureReason(securityreason.HomebrewMetadataUnavailable, item.Kind, name, "Homebrew metadata unavailable: "+err.Error(), map[string]string{"error": err.Error()}))
	return posture
}

func OfficialTap(tap string) bool {
	switch normalizeName("tap", tap) {
	case "homebrew/core", "homebrew/cask":
		return true
	default:
		return false
	}
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

func CaskProvenanceVerdictFor(homepageHost string, urlHost string, version string) CaskProvenanceVerdict {
	homepageHost = strings.TrimSpace(homepageHost)
	urlHost = strings.TrimSpace(urlHost)
	version = strings.TrimSpace(version)
	switch {
	case homepageHost == "" || urlHost == "":
		return CaskProvenanceVerdict{
			Decision:    "review",
			Confidence:  "low",
			Reason:      "Homebrew cask provenance needs review because homepage or download URL host is missing",
			ReasonCode:  securityreason.HomebrewCaskProvenanceReview,
			Remediation: "review the cask vendor homepage and download URL before adding a policy override",
		}
	case version == "" || strings.EqualFold(version, "latest"):
		return CaskProvenanceVerdict{
			Decision:    "review",
			Confidence:  "low",
			Reason:      "Homebrew cask update requires vendor provenance review",
			ReasonCode:  securityreason.HomebrewCaskProvenanceReview,
			Remediation: "review the cask vendor homepage and download URL before adding a policy override",
		}
	case caskHostsSameSite(homepageHost, urlHost):
		return CaskProvenanceVerdict{
			Decision:   "allow",
			Confidence: "medium",
			Reason:     "Homebrew cask download host is under the homepage host; vendor provenance verified from Homebrew metadata",
			ReasonCode: securityreason.HomebrewCaskProvenanceOK,
			Evidence:   "Homebrew cask same-site download host",
		}
	case homepageHost != urlHost:
		return CaskProvenanceVerdict{
			Decision:    "review",
			Confidence:  "low",
			Reason:      "Homebrew cask download host differs from homepage host; vendor provenance review required",
			ReasonCode:  securityreason.HomebrewCaskHostMismatch,
			Remediation: "review whether " + urlHost + " is an official distribution host for " + homepageHost + " before adding a policy override",
		}
	default:
		return CaskProvenanceVerdict{
			Decision:    "review",
			Confidence:  "low",
			Reason:      "Homebrew cask update requires vendor provenance review",
			ReasonCode:  securityreason.HomebrewCaskProvenanceReview,
			Remediation: "review the cask vendor homepage and download URL before adding a policy override",
		}
	}
}

func caskHostsSameSite(homepageHost string, urlHost string) bool {
	homepageHost = normalizeCaskHostForComparison(homepageHost)
	urlHost = normalizeCaskHostForComparison(urlHost)
	if homepageHost == "" || urlHost == "" || sharedCaskHostSuffix(homepageHost) {
		return false
	}
	return urlHost == homepageHost || strings.HasSuffix(urlHost, "."+homepageHost)
}

func normalizeCaskHostForComparison(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return strings.TrimSuffix(host, ".")
}

func sharedCaskHostSuffix(host string) bool {
	switch host {
	case "github.io", "pages.dev", "vercel.app", "netlify.app", "cloudfront.net", "appspot.com", "googleapis.com", "amazonaws.com", "azureedge.net", "fastly.net":
		return true
	default:
		return false
	}
}

func setPostureReason(posture *Posture, reason securityreason.Reason) {
	if posture == nil {
		return
	}
	posture.Reason = reason.Text
	posture.ReasonCode = reason.Code
	posture.ReasonArgs = reason.Args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func hostFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func truncate(text string, width int) string {
	if width <= 0 || len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}

func validGitHubPathPart(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return false
		}
	}
	return true
}
