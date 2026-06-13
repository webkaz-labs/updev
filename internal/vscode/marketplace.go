package vscode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

type Thresholds struct {
	MinInstallCount  float64
	MinAverageRating float64
	MinExtensionAge  time.Duration
}

type Posture struct {
	Provider          string            `json:"provider"`
	Kind              string            `json:"kind"`
	Name              string            `json:"name"`
	Publisher         string            `json:"publisher,omitempty"`
	DisplayName       string            `json:"display_name,omitempty"`
	Version           string            `json:"version,omitempty"`
	LastUpdated       string            `json:"last_updated,omitempty"`
	PublishedDate     string            `json:"published_date,omitempty"`
	Flags             string            `json:"flags,omitempty"`
	PublisherVerified bool              `json:"publisher_verified"`
	ExecutesCode      bool              `json:"executes_code,omitempty"`
	RepositoryURL     string            `json:"repository_url,omitempty"`
	SupportURL        string            `json:"support_url,omitempty"`
	InstallCount      float64           `json:"install_count,omitempty"`
	AverageRating     float64           `json:"average_rating,omitempty"`
	Decision          string            `json:"decision"`
	Confidence        string            `json:"confidence"`
	Reason            string            `json:"reason,omitempty"`
	ReasonCode        string            `json:"reason_code,omitempty"`
	ReasonArgs        map[string]string `json:"reason_args,omitempty"`
	Remediation       string            `json:"remediation,omitempty"`
	Evidence          []string          `json:"evidence,omitempty"`
	URL               string            `json:"url,omitempty"`
}

type MarketplaceRequest struct {
	Filters []MarketplaceFilter `json:"filters"`
	Flags   int                 `json:"flags"`
}

type MarketplaceFilter struct {
	Criteria []MarketplaceCriterion `json:"criteria"`
}

type MarketplaceCriterion struct {
	FilterType int    `json:"filterType"`
	Value      string `json:"value"`
}

type MarketplaceResponse struct {
	Results []MarketplaceResult `json:"results"`
}

type MarketplaceResult struct {
	Extensions []Extension `json:"extensions"`
}

type Extension struct {
	Publisher     Publisher   `json:"publisher"`
	ExtensionName string      `json:"extensionName"`
	DisplayName   string      `json:"displayName"`
	Flags         string      `json:"flags"`
	LastUpdated   string      `json:"lastUpdated"`
	PublishedDate string      `json:"publishedDate"`
	Versions      []Version   `json:"versions"`
	Statistics    []Statistic `json:"statistics"`
}

type Publisher struct {
	PublisherName    string `json:"publisherName"`
	DisplayName      string `json:"displayName"`
	Flags            string `json:"flags"`
	Domain           string `json:"domain"`
	IsDomainVerified bool   `json:"isDomainVerified"`
}

type Version struct {
	Version     string     `json:"version"`
	LastUpdated string     `json:"lastUpdated"`
	Properties  []Property `json:"properties"`
}

type Property struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Statistic struct {
	StatisticName string  `json:"statisticName"`
	Value         float64 `json:"value"`
}

func PosturesFromItems(ctx context.Context, client *http.Client, endpoint string, items []plan.Item, thresholds Thresholds) ([]Posture, error) {
	extensions := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Provider != "brew" || item.Kind != "vscode" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		extensions = append(extensions, name)
	}
	postures := make([]Posture, 0, len(extensions))
	errs := []error{}
	for _, extension := range extensions {
		metadata, err := FetchMarketplaceExtension(ctx, client, endpoint, extension)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", extension, err))
			postures = append(postures, PostureUnavailable(extension, err))
			continue
		}
		postures = append(postures, PostureFromMetadata(extension, metadata, thresholds))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Name < postures[j].Name
	})
	return postures, errors.Join(errs...)
}

func FetchMarketplaceExtension(ctx context.Context, client *http.Client, endpoint string, extension string) (Extension, error) {
	body := MarketplaceRequest{
		Filters: []MarketplaceFilter{{
			Criteria: []MarketplaceCriterion{{
				FilterType: 7,
				Value:      extension,
			}},
		}},
		Flags: 914,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Extension{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return Extension{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json;api-version=7.2-preview.1")
	response, err := client.Do(request)
	if err != nil {
		return Extension{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return Extension{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Extension{}, fmt.Errorf("marketplace query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(raw)), 180))
	}
	var decoded MarketplaceResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Extension{}, err
	}
	for _, result := range decoded.Results {
		for _, candidate := range result.Extensions {
			fullName := strings.ToLower(candidate.Publisher.PublisherName + "." + candidate.ExtensionName)
			if fullName == strings.ToLower(extension) {
				return candidate, nil
			}
		}
	}
	return Extension{}, fmt.Errorf("extension not found")
}

func PostureFromMetadata(requestedName string, metadata Extension, thresholds Thresholds) Posture {
	version := ""
	properties := []Property{}
	if len(metadata.Versions) > 0 {
		version = metadata.Versions[0].Version
		properties = metadata.Versions[0].Properties
	}
	repositoryURL := firstNonEmpty(
		PropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Source"),
		PropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Repository"),
		PropertyValue(properties, "Microsoft.VisualStudio.Services.Links.GitHub"),
	)
	supportURL := PropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Support")
	executesCode := strings.EqualFold(PropertyValue(properties, "Microsoft.VisualStudio.Code.ExecutesCode"), "true")
	installCount, hasInstallCount := StatisticValue(metadata.Statistics, "install")
	averageRating, hasAverageRating := StatisticValue(metadata.Statistics, "averagerating")
	posture := Posture{
		Provider:          "brew",
		Kind:              "vscode",
		Name:              requestedName,
		Publisher:         metadata.Publisher.PublisherName,
		DisplayName:       metadata.DisplayName,
		Version:           version,
		LastUpdated:       metadata.LastUpdated,
		PublishedDate:     metadata.PublishedDate,
		Flags:             metadata.Flags,
		PublisherVerified: metadata.Publisher.IsDomainVerified,
		ExecutesCode:      executesCode,
		RepositoryURL:     repositoryURL,
		SupportURL:        supportURL,
		InstallCount:      installCount,
		AverageRating:     averageRating,
		Decision:          "allow",
		Confidence:        "medium",
		URL:               "https://marketplace.visualstudio.com/items?itemName=" + requestedName,
	}
	setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeMarketplaceAllowed, requestedName, "VS Code Marketplace posture is allowed", nil))
	switch {
	case !strings.Contains(metadata.Flags, "public"):
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeNotPublic, requestedName, "Marketplace metadata does not mark this extension public", nil))
		posture.Remediation = "review Marketplace visibility and source provenance before keeping this extension"
	case !strings.Contains(metadata.Flags, "validated"):
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeNotValidated, requestedName, "Marketplace metadata does not mark this extension validated", nil))
		posture.Remediation = "review Marketplace validation status and source provenance before keeping this extension"
	case !metadata.Publisher.IsDomainVerified:
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodePublisherUnverified, requestedName, "publisher domain is not verified in Marketplace metadata", map[string]string{"publisher": metadata.Publisher.PublisherName}))
		posture.Remediation = "verify publisher identity and source repository before adding a temporary policy override"
	case executesCode && repositoryURL == "":
		posture.Decision = "review"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeCodeMissingRepository, requestedName, "extension executes code but Marketplace metadata does not expose a source repository", nil))
		posture.Remediation = "require a trusted source repository or replace the extension before allowing code execution"
	case repositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeMissingRepository, requestedName, "Marketplace metadata does not expose a source repository", nil))
		posture.Remediation = "review extension provenance manually before adding a temporary policy override"
	case ExtensionTooNew(posture.PublishedDate, thresholds.MinExtensionAge):
		posture.Decision = "review"
		posture.Confidence = "low"
		setPostureReason(&posture, ExtensionAgeSecurityReason(requestedName, posture.PublishedDate, thresholds.MinExtensionAge))
		posture.Remediation = "wait until the Marketplace extension reaches the minimum age or review publisher/source provenance before allowing"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace age")
	case hasInstallCount && posture.InstallCount < thresholds.MinInstallCount:
		posture.Decision = "review"
		posture.Confidence = "low"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeLowInstallCount, requestedName, fmt.Sprintf("Marketplace install count is below threshold: %.0f installs, minimum %.0f", posture.InstallCount, thresholds.MinInstallCount), map[string]string{"install_count": fmt.Sprintf("%.0f", posture.InstallCount), "min_install_count": fmt.Sprintf("%.0f", thresholds.MinInstallCount)}))
		posture.Remediation = "review publisher and repository provenance before accepting a low-install extension"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace popularity")
	case hasAverageRating && posture.AverageRating < thresholds.MinAverageRating:
		posture.Decision = "review"
		posture.Confidence = "low"
		setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeLowRating, requestedName, fmt.Sprintf("Marketplace average rating is below threshold: %.1f, minimum %.1f", posture.AverageRating, thresholds.MinAverageRating), map[string]string{"average_rating": fmt.Sprintf("%.1f", posture.AverageRating), "min_average_rating": fmt.Sprintf("%.1f", thresholds.MinAverageRating)}))
		posture.Remediation = "review extension quality signals and source provenance before accepting a low-rated extension"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace rating")
	}
	return posture
}

func PostureUnavailable(extension string, err error) Posture {
	posture := Posture{
		Provider:    "brew",
		Kind:        "vscode",
		Name:        extension,
		Decision:    "review",
		Confidence:  "low",
		Remediation: "retry when Marketplace metadata is reachable or review the extension manually before adding a policy override",
		URL:         "https://marketplace.visualstudio.com/items?itemName=" + extension,
	}
	setPostureReason(&posture, securityreason.VSCodePostureReason(securityreason.VSCodeMarketplaceUnavailable, extension, "VS Code Marketplace metadata unavailable: "+err.Error(), map[string]string{"error": err.Error()}))
	return posture
}

func StatisticValue(statistics []Statistic, name string) (float64, bool) {
	for _, statistic := range statistics {
		if statistic.StatisticName == name {
			return statistic.Value, true
		}
	}
	return 0, false
}

func PropertyValue(properties []Property, key string) string {
	for _, property := range properties {
		if property.Key == key {
			return strings.TrimSpace(property.Value)
		}
	}
	return ""
}

func ExtensionTooNew(publishedDate string, minAge time.Duration) bool {
	if minAge <= 0 {
		return false
	}
	published, ok := ParseMarketplaceTime(publishedDate)
	if !ok {
		return false
	}
	return time.Since(published) < minAge
}

func ExtensionAgeReason(publishedDate string, minAge time.Duration) string {
	published, ok := ParseMarketplaceTime(publishedDate)
	if !ok {
		return "Marketplace extension age is unavailable"
	}
	ageDays := int(time.Since(published).Hours() / 24)
	minDays := int(minAge.Hours() / 24)
	return fmt.Sprintf("Marketplace extension is newly published: age %d days, minimum %d days", ageDays, minDays)
}

func ExtensionAgeSecurityReason(extension string, publishedDate string, minAge time.Duration) securityreason.Reason {
	text := ExtensionAgeReason(publishedDate, minAge)
	published, ok := ParseMarketplaceTime(publishedDate)
	if !ok {
		return securityreason.VSCodePostureReason(securityreason.VSCodeExtensionTooNew, extension, text, nil)
	}
	ageDays := int(time.Since(published).Hours() / 24)
	minDays := int(minAge.Hours() / 24)
	return securityreason.VSCodePostureReason(securityreason.VSCodeExtensionTooNew, extension, text, map[string]string{
		"age_days":     fmt.Sprintf("%d", ageDays),
		"min_age_days": fmt.Sprintf("%d", minDays),
	})
}

func setPostureReason(posture *Posture, reason securityreason.Reason) {
	if posture == nil {
		return
	}
	posture.Reason = reason.Text
	posture.ReasonCode = reason.Code
	posture.ReasonArgs = reason.Args
}

func ParseMarketplaceTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
