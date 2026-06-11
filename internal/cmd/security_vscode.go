package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const defaultVSCodeMarketplaceURL = "https://marketplace.visualstudio.com/_apis/public/gallery/extensionquery?api-version=7.2-preview.1"

const (
	defaultVSCodeMinInstallCount     = 1000
	defaultVSCodeMinAverageRating    = 2.0
	defaultVSCodeMinExtensionAgeDays = 14
	defaultVSCodeMinUpdateAgeDays    = 3
	vscodeMinInstallCountEnvName     = "UPDEV_VSCODE_MIN_INSTALL_COUNT"
	vscodeMinAverageRatingEnvName    = "UPDEV_VSCODE_MIN_AVERAGE_RATING"
	vscodeMinExtensionAgeDaysEnvName = "UPDEV_VSCODE_MIN_EXTENSION_AGE_DAYS"
	vscodeMinUpdateAgeDaysEnvName    = "UPDEV_VSCODE_MIN_UPDATE_AGE_DAYS"
)

type vscodePosture struct {
	Provider          string   `json:"provider"`
	Kind              string   `json:"kind"`
	Name              string   `json:"name"`
	Publisher         string   `json:"publisher,omitempty"`
	DisplayName       string   `json:"display_name,omitempty"`
	Version           string   `json:"version,omitempty"`
	LastUpdated       string   `json:"last_updated,omitempty"`
	PublishedDate     string   `json:"published_date,omitempty"`
	Flags             string   `json:"flags,omitempty"`
	PublisherVerified bool     `json:"publisher_verified"`
	ExecutesCode      bool     `json:"executes_code,omitempty"`
	RepositoryURL     string   `json:"repository_url,omitempty"`
	SupportURL        string   `json:"support_url,omitempty"`
	InstallCount      float64  `json:"install_count,omitempty"`
	AverageRating     float64  `json:"average_rating,omitempty"`
	Decision          string   `json:"decision"`
	Confidence        string   `json:"confidence"`
	Reason            string   `json:"reason,omitempty"`
	Remediation       string   `json:"remediation,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	URL               string   `json:"url,omitempty"`
}

type vscodeMarketplaceRequest struct {
	Filters []vscodeMarketplaceFilter `json:"filters"`
	Flags   int                       `json:"flags"`
}

type vscodeMarketplaceFilter struct {
	Criteria []vscodeMarketplaceCriterion `json:"criteria"`
}

type vscodeMarketplaceCriterion struct {
	FilterType int    `json:"filterType"`
	Value      string `json:"value"`
}

type vscodeMarketplaceResponse struct {
	Results []vscodeMarketplaceResult `json:"results"`
}

type vscodeMarketplaceResult struct {
	Extensions []vscodeExtension `json:"extensions"`
}

type vscodeExtension struct {
	Publisher     vscodePublisher   `json:"publisher"`
	ExtensionName string            `json:"extensionName"`
	DisplayName   string            `json:"displayName"`
	Flags         string            `json:"flags"`
	LastUpdated   string            `json:"lastUpdated"`
	PublishedDate string            `json:"publishedDate"`
	Versions      []vscodeVersion   `json:"versions"`
	Statistics    []vscodeStatistic `json:"statistics"`
}

type vscodePublisher struct {
	PublisherName    string `json:"publisherName"`
	DisplayName      string `json:"displayName"`
	Flags            string `json:"flags"`
	Domain           string `json:"domain"`
	IsDomainVerified bool   `json:"isDomainVerified"`
}

type vscodeVersion struct {
	Version     string           `json:"version"`
	LastUpdated string           `json:"lastUpdated"`
	Properties  []vscodeProperty `json:"properties"`
}

type vscodeProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type vscodeStatistic struct {
	StatisticName string  `json:"statisticName"`
	Value         float64 `json:"value"`
}

func vscodePosturesFromItems(ctx context.Context, client *http.Client, endpoint string, items []plan.Item) ([]vscodePosture, error) {
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
	postures := make([]vscodePosture, 0, len(extensions))
	errs := []error{}
	for _, extension := range extensions {
		metadata, err := fetchVSCodeMarketplaceExtension(ctx, client, endpoint, extension)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", extension, err))
			postures = append(postures, vscodePostureUnavailable(extension, err))
			continue
		}
		postures = append(postures, vscodePostureFromMetadata(extension, metadata))
	}
	sort.Slice(postures, func(i, j int) bool {
		return postures[i].Name < postures[j].Name
	})
	return postures, errors.Join(errs...)
}

func collectVSCodeSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "vscode", Status: plan.StatusOK}
	items, err := vscodeItemsFromBrewfile(root)
	if err != nil {
		gate.Status = plan.StatusError
		gate.Error = "VS Code extension manifest unavailable: " + err.Error()
		return gate
	}
	installedVersions := vscodeInstalledVersions(ctx, commandRunner, &gate)
	postures, err := vscodePosturesFromItems(ctx, http.DefaultClient, vscodeMarketplaceURL(), items)
	if err != nil {
		gate.Warnings = append(gate.Warnings, "VS Code marketplace posture failed: "+err.Error())
	}
	postures = applySecurityPolicyToVSCodePostures(policy, postures)
	for _, posture := range postures {
		finding := vscodeSafetyFinding(posture, installedVersions[strings.ToLower(posture.Name)])
		gate.Findings = append(gate.Findings, finding)
	}
	var advisoryErr error
	gate.Findings, advisoryErr = enrichVSCodeSafetyAdvisories(ctx, http.DefaultClient, osvAPIURL(), gate.Findings)
	if advisoryErr != nil {
		gate.Warnings = append(gate.Warnings, "VS Code advisory OSV query failed: "+advisoryErr.Error())
	}
	gate.Findings = applySecurityPolicyToSafetyFindings(policy, gate.Findings)
	for _, finding := range gate.Findings {
		if finding.Decision != "allow" {
			gate.Status = plan.StatusHeld
		}
	}
	gate.Summary = safetySummaryFromFindings(gate.Findings)
	return gate
}

func collectVSCodeUpdateSafetyWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy) safetyGate {
	gate := safetyGate{Provider: "vscode", Status: plan.StatusOK}
	items, err := vscodeItemsFromBrewfile(root)
	if err != nil {
		gate.Status = plan.StatusError
		gate.Error = "VS Code extension manifest unavailable: " + err.Error()
		return gate
	}
	installedVersions := vscodeInstalledVersions(ctx, commandRunner, &gate)
	if len(installedVersions) == 0 {
		gate.Warnings = append(gate.Warnings, "VS Code update candidates unavailable; installed extension versions were not detected")
		return gate
	}
	candidateItems := vscodeInstalledBrewfileItems(items, installedVersions)
	if len(candidateItems) == 0 {
		return gate
	}
	identities := planItemIdentities(candidateItems)
	cacheKey := updateSafetyVSCodeCacheKey(root, identities, installedVersions)
	if cached, ok := loadUpdateSafetyCache("vscode", cacheKey, updateSafetyMarketplaceMaxAge); ok {
		gate.Warnings = append(gate.Warnings, cached.Warnings...)
		gate.Findings = updateSafetyCacheEvidence(cached.Findings, "vscode", cached.CreatedAt)
	} else if cached, ok := loadUpdateSafetyCache("vscode", updateSafetyVSCodeMarketplaceErrorCacheKey(cacheKey), updateSafetyUnavailableMaxAge); ok && cached.Error != "" {
		gate.Findings = applyUpdateSafetyUnavailableCache(&gate, cached, "cached VS Code marketplace posture failed", "vscode marketplace unavailable")
	} else if cached, ok := loadUpdateSafetyCache("vscode", updateSafetyVSCodeAdvisoryErrorCacheKey(cacheKey), updateSafetyUnavailableMaxAge); ok && cached.Error != "" {
		gate.Findings = applyUpdateSafetyUnavailableCache(&gate, cached, "cached VS Code advisory OSV query failed", "vscode advisory unavailable")
	} else {
		postures, err := vscodePosturesFromItems(ctx, http.DefaultClient, vscodeMarketplaceURL(), candidateItems)
		if err != nil {
			gate.Warnings = append(gate.Warnings, "VS Code marketplace posture failed: "+err.Error())
		}
		for _, posture := range postures {
			installed := installedVersions[strings.ToLower(posture.Name)]
			if installed == "" || posture.Version == "" || installed == posture.Version {
				continue
			}
			gate.Findings = append(gate.Findings, vscodeSafetyFinding(posture, installed))
		}
		if err == nil {
			var advisoryErr error
			gate.Findings, advisoryErr = enrichVSCodeSafetyAdvisories(ctx, http.DefaultClient, osvAPIURL(), gate.Findings)
			if advisoryErr != nil {
				gate.Warnings = append(gate.Warnings, "VS Code advisory OSV query failed: "+advisoryErr.Error())
				saveUpdateSafetyUnavailableCache("vscode", updateSafetyVSCodeAdvisoryErrorCacheKey(cacheKey), advisoryErr.Error(), gate.Findings, nil)
			} else {
				saveUpdateSafetyCache("vscode", cacheKey, gate.Findings, nil)
			}
		} else {
			saveUpdateSafetyUnavailableCache("vscode", updateSafetyVSCodeMarketplaceErrorCacheKey(cacheKey), err.Error(), gate.Findings, nil)
		}
	}
	gate.Findings = applySecurityPolicyToSafetyFindings(policy, gate.Findings)
	for _, finding := range gate.Findings {
		if finding.Decision != "allow" {
			gate.Status = plan.StatusHeld
		}
	}
	gate.Summary = safetySummaryFromFindings(gate.Findings)
	return gate
}

func vscodeInstalledBrewfileItems(items []plan.Item, installedVersions map[string]string) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		if installedVersions[strings.ToLower(item.Name)] == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func vscodeInstalledVersions(ctx context.Context, commandRunner commandRunner, gate *safetyGate) map[string]string {
	result := commandRunner.Run(ctx, "code", "--list-extensions", "--show-versions")
	if result.Code != 0 || result.Err != nil {
		if gate != nil {
			gate.Warnings = append(gate.Warnings, "VS Code installed extension versions unavailable: "+vscodeInstalledVersionsError(result))
		}
		return nil
	}
	return parseVSCodeInstalledVersions(result.Stdout)
}

func vscodeInstalledVersionsError(result runner.Result) string {
	if detail := firstNonEmpty(result.Stderr, result.Stdout); detail != "" {
		return detail
	}
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Code != 0 {
		return fmt.Sprintf("code exited with status %d", result.Code)
	}
	return "unknown error"
}

func parseVSCodeInstalledVersions(raw string) map[string]string {
	versions := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, version, ok := strings.Cut(line, "@")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" {
			continue
		}
		versions[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(version)
	}
	return versions
}

func vscodeItemsFromBrewfile(root string) ([]plan.Item, error) {
	home := updevpath.HomeDir()
	path := filepath.Join(home, "Brewfile")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(root, "Brewfile.tmpl")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lineRe := regexp.MustCompile(`^\s*vscode\s+"([^"]+)"`)
	items := []plan.Item{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		name := strings.TrimSpace(match[1])
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, plan.Item{Provider: "brew", Kind: "vscode", Name: name, Desired: true})
	}
	return items, scanner.Err()
}

func vscodeSafetyFinding(posture vscodePosture, installedVersion string) safetyFinding {
	installedVersions := []string(nil)
	if installedVersion != "" {
		installedVersions = []string{installedVersion}
	}
	var publisherVerified *bool
	if posture.Publisher != "" {
		value := posture.PublisherVerified
		publisherVerified = &value
	}
	finding := safetyFinding{
		Provider:          "brew",
		Kind:              "vscode",
		Name:              posture.Name,
		InstalledVersions: installedVersions,
		CurrentVersion:    posture.Version,
		Version:           posture.Version,
		Publisher:         posture.Publisher,
		PublisherVerified: publisherVerified,
		ExecutesCode:      posture.ExecutesCode,
		RepositoryURL:     posture.RepositoryURL,
		SupportURL:        posture.SupportURL,
		LastUpdated:       posture.LastUpdated,
		PublishedDate:     posture.PublishedDate,
		Flags:             posture.Flags,
		InstallCount:      posture.InstallCount,
		AverageRating:     posture.AverageRating,
		Decision:          posture.Decision,
		Reason:            firstNonEmpty(posture.Reason, "VS Code Marketplace posture is allowed"),
		Confidence:        posture.Confidence,
		Evidence:          appendEvidence(posture.Evidence, "vscode-marketplace"),
		URL:               posture.URL,
	}
	if finding.Decision != "allow" {
		finding.Remediation = "review VS Code Marketplace publisher/source metadata and add a temporary allow policy with reason and expiry if accepted"
	}
	if finding.Decision == "allow" && installedVersion != "" && posture.Version != "" && installedVersion != posture.Version {
		finding = applyVSCodeUpdateAge(finding, minVSCodeUpdateAge())
	}
	return finding
}

func enrichVSCodeSafetyAdvisories(ctx context.Context, client *http.Client, apiURL string, findings []safetyFinding) ([]safetyFinding, error) {
	packages, indexes := vscodeSafetyAdvisoryPackagesFromFindings(findings)
	if len(packages) == 0 {
		return findings, nil
	}
	advisories, err := queryOSVBatch(ctx, client, apiURL, packages)
	if err != nil {
		return findings, err
	}
	out := append([]safetyFinding(nil), findings...)
	for _, advisory := range advisories {
		key := safetyAdvisoryPackageKey(securityPackage{
			Provider:  advisory.Provider,
			Name:      advisory.Name,
			Version:   advisory.Version,
			Ecosystem: advisory.Ecosystem,
			Package:   advisory.Package,
		})
		for _, index := range indexes[key] {
			out[index] = applyVSCodeSafetyAdvisory(out[index], advisory)
		}
	}
	return out, nil
}

func vscodeSafetyAdvisoryPackagesFromFindings(findings []safetyFinding) ([]securityPackage, map[string][]int) {
	packages := []securityPackage{}
	indexes := map[string][]int{}
	seen := map[string]bool{}
	for index, finding := range findings {
		if finding.Provider != "brew" || finding.Kind != "vscode" || finding.Name == "" || finding.Version == "" {
			continue
		}
		pkg := securityPackage{
			Provider:   "brew",
			Name:       finding.Name,
			Version:    finding.Version,
			Ecosystem:  "VSCode",
			Package:    finding.Name,
			Confidence: "high",
		}
		key := safetyAdvisoryPackageKey(pkg)
		indexes[key] = append(indexes[key], index)
		if seen[key] {
			continue
		}
		seen[key] = true
		packages = append(packages, pkg)
	}
	return packages, indexes
}

func applyVSCodeSafetyAdvisory(finding safetyFinding, advisory securityFinding) safetyFinding {
	finding.Decision = "hold"
	finding.Reason = "OSV advisory match for VS Code extension version: " + advisory.VulnID
	finding.Confidence = "high"
	finding.Evidence = appendEvidence(finding.Evidence, "osv-vscode")
	finding.AdvisoryIDs = appendUniqueString(finding.AdvisoryIDs, advisory.VulnID)
	finding.Reason = "OSV advisory match for VS Code extension version: " + strings.Join(finding.AdvisoryIDs, ",")
	for _, fixed := range advisory.FixedVersions {
		finding.FixedVersions = appendUniqueString(finding.FixedVersions, fixed)
	}
	finding.Remediation = vscodeAdvisoryRemediation(finding.FixedVersions)
	return finding
}

func vscodeAdvisoryRemediation(fixedVersions []string) string {
	if len(fixedVersions) > 0 {
		return "upgrade the VS Code extension to a fixed Marketplace version: " + strings.Join(fixedVersions, ",") + "; otherwise wait or add a temporary policy override with reason and expiry after review"
	}
	return "review the OSV advisory and wait for a fixed Marketplace version, or add a temporary policy override with reason and expiry after review"
}

func fetchVSCodeMarketplaceExtension(ctx context.Context, client *http.Client, endpoint string, extension string) (vscodeExtension, error) {
	body := vscodeMarketplaceRequest{
		Filters: []vscodeMarketplaceFilter{{
			Criteria: []vscodeMarketplaceCriterion{{
				FilterType: 7,
				Value:      extension,
			}},
		}},
		Flags: 914,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return vscodeExtension{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return vscodeExtension{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json;api-version=7.2-preview.1")
	response, err := client.Do(request)
	if err != nil {
		return vscodeExtension{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return vscodeExtension{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return vscodeExtension{}, fmt.Errorf("marketplace query failed: HTTP %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(raw)), 180))
	}
	var decoded vscodeMarketplaceResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return vscodeExtension{}, err
	}
	for _, result := range decoded.Results {
		for _, candidate := range result.Extensions {
			fullName := strings.ToLower(candidate.Publisher.PublisherName + "." + candidate.ExtensionName)
			if fullName == strings.ToLower(extension) {
				return candidate, nil
			}
		}
	}
	return vscodeExtension{}, fmt.Errorf("extension not found")
}

func vscodePostureFromMetadata(requestedName string, metadata vscodeExtension) vscodePosture {
	version := ""
	properties := []vscodeProperty{}
	if len(metadata.Versions) > 0 {
		version = metadata.Versions[0].Version
		properties = metadata.Versions[0].Properties
	}
	repositoryURL := firstNonEmpty(
		vscodePropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Source"),
		vscodePropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Repository"),
		vscodePropertyValue(properties, "Microsoft.VisualStudio.Services.Links.GitHub"),
	)
	supportURL := vscodePropertyValue(properties, "Microsoft.VisualStudio.Services.Links.Support")
	executesCode := strings.EqualFold(vscodePropertyValue(properties, "Microsoft.VisualStudio.Code.ExecutesCode"), "true")
	installCount, hasInstallCount := vscodeStatisticValue(metadata.Statistics, "install")
	averageRating, hasAverageRating := vscodeStatisticValue(metadata.Statistics, "averagerating")
	posture := vscodePosture{
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
	minInstallCount := minVSCodeInstallCount()
	minAverageRating := minVSCodeAverageRating()
	minExtensionAge := minVSCodeExtensionAge()
	switch {
	case !strings.Contains(metadata.Flags, "public"):
		posture.Decision = "review"
		posture.Reason = "Marketplace metadata does not mark this extension public"
		posture.Remediation = "review Marketplace visibility and source provenance before keeping this extension"
	case !strings.Contains(metadata.Flags, "validated"):
		posture.Decision = "review"
		posture.Reason = "Marketplace metadata does not mark this extension validated"
		posture.Remediation = "review Marketplace validation status and source provenance before keeping this extension"
	case !metadata.Publisher.IsDomainVerified:
		posture.Decision = "review"
		posture.Reason = "publisher domain is not verified in Marketplace metadata"
		posture.Remediation = "verify publisher identity and source repository before adding a temporary policy override"
	case executesCode && repositoryURL == "":
		posture.Decision = "review"
		posture.Reason = "extension executes code but Marketplace metadata does not expose a source repository"
		posture.Remediation = "require a trusted source repository or replace the extension before allowing code execution"
	case repositoryURL == "":
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = "Marketplace metadata does not expose a source repository"
		posture.Remediation = "review extension provenance manually before adding a temporary policy override"
	case vscodeExtensionTooNew(posture.PublishedDate, minExtensionAge):
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = vscodeExtensionAgeReason(posture.PublishedDate, minExtensionAge)
		posture.Remediation = "wait until the Marketplace extension reaches the minimum age or review publisher/source provenance before allowing"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace age")
	case hasInstallCount && posture.InstallCount < minInstallCount:
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = fmt.Sprintf("Marketplace install count is below threshold: %.0f installs, minimum %.0f", posture.InstallCount, minInstallCount)
		posture.Remediation = "review publisher and repository provenance before accepting a low-install extension"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace popularity")
	case hasAverageRating && posture.AverageRating < minAverageRating:
		posture.Decision = "review"
		posture.Confidence = "low"
		posture.Reason = fmt.Sprintf("Marketplace average rating is below threshold: %.1f, minimum %.1f", posture.AverageRating, minAverageRating)
		posture.Remediation = "review extension quality signals and source provenance before accepting a low-rated extension"
		posture.Evidence = appendEvidence(posture.Evidence, "vscode-marketplace rating")
	}
	return posture
}

func vscodePostureUnavailable(extension string, err error) vscodePosture {
	return vscodePosture{
		Provider:    "brew",
		Kind:        "vscode",
		Name:        extension,
		Decision:    "review",
		Confidence:  "low",
		Reason:      "VS Code Marketplace metadata unavailable: " + err.Error(),
		Remediation: "retry when Marketplace metadata is reachable or review the extension manually before adding a policy override",
		URL:         "https://marketplace.visualstudio.com/items?itemName=" + extension,
	}
}

func vscodeAdvisoryPackagesFromPostures(postures []vscodePosture) []securityPackage {
	packages := []securityPackage{}
	seen := map[string]bool{}
	for _, posture := range postures {
		if posture.Name == "" || posture.Version == "" {
			continue
		}
		key := strings.ToLower(posture.Name + "\x00" + posture.Version)
		if seen[key] {
			continue
		}
		seen[key] = true
		packages = append(packages, securityPackage{
			Provider:   "brew",
			Name:       posture.Name,
			Version:    posture.Version,
			Ecosystem:  "VSCode",
			Package:    posture.Name,
			Confidence: "high",
		})
	}
	return packages
}

func vscodeStatisticValue(statistics []vscodeStatistic, name string) (float64, bool) {
	for _, statistic := range statistics {
		if statistic.StatisticName == name {
			return statistic.Value, true
		}
	}
	return 0, false
}

func vscodePropertyValue(properties []vscodeProperty, key string) string {
	for _, property := range properties {
		if property.Key == key {
			return strings.TrimSpace(property.Value)
		}
	}
	return ""
}

func vscodeMarketplaceURL() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_VSCODE_MARKETPLACE_URL")); value != "" {
		return value
	}
	return defaultVSCodeMarketplaceURL
}

func minVSCodeInstallCount() float64 {
	return minVSCodeInstallCountWithConfig(loadUpdevConfig())
}

func minVSCodeInstallCountWithConfig(config updevConfig) float64 {
	threshold := float64(defaultVSCodeMinInstallCount)
	if config.Security.VSCode.MinInstallCount != nil && *config.Security.VSCode.MinInstallCount >= 0 {
		threshold = *config.Security.VSCode.MinInstallCount
	}
	if value := strings.TrimSpace(os.Getenv(vscodeMinInstallCountEnvName)); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil && parsed >= 0 {
			threshold = parsed
		}
	}
	return threshold
}

func minVSCodeAverageRating() float64 {
	return minVSCodeAverageRatingWithConfig(loadUpdevConfig())
}

func minVSCodeAverageRatingWithConfig(config updevConfig) float64 {
	threshold := defaultVSCodeMinAverageRating
	if config.Security.VSCode.MinAverageRating != nil && *config.Security.VSCode.MinAverageRating >= 0 {
		threshold = *config.Security.VSCode.MinAverageRating
	}
	if value := strings.TrimSpace(os.Getenv(vscodeMinAverageRatingEnvName)); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil && parsed >= 0 {
			threshold = parsed
		}
	}
	return threshold
}

func minVSCodeExtensionAge() time.Duration {
	return minVSCodeExtensionAgeWithConfig(loadUpdevConfig())
}

func minVSCodeExtensionAgeWithConfig(config updevConfig) time.Duration {
	days := defaultVSCodeMinExtensionAgeDays
	if config.Security.VSCode.MinExtensionAgeDays != nil && *config.Security.VSCode.MinExtensionAgeDays >= 0 {
		days = *config.Security.VSCode.MinExtensionAgeDays
	}
	if value := strings.TrimSpace(os.Getenv(vscodeMinExtensionAgeDaysEnvName)); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			days = parsed
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

func minVSCodeUpdateAge() time.Duration {
	return minVSCodeUpdateAgeWithConfig(loadUpdevConfig())
}

func minVSCodeUpdateAgeWithConfig(config updevConfig) time.Duration {
	days := defaultVSCodeMinUpdateAgeDays
	if config.Security.VSCode.MinUpdateAgeDays != nil && *config.Security.VSCode.MinUpdateAgeDays >= 0 {
		days = *config.Security.VSCode.MinUpdateAgeDays
	}
	if value := strings.TrimSpace(os.Getenv(vscodeMinUpdateAgeDaysEnvName)); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			days = parsed
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

func vscodeExtensionTooNew(publishedDate string, minAge time.Duration) bool {
	if minAge <= 0 {
		return false
	}
	published, ok := parseVSCodeMarketplaceTime(publishedDate)
	if !ok {
		return false
	}
	return time.Since(published) < minAge
}

func vscodeExtensionAgeReason(publishedDate string, minAge time.Duration) string {
	published, ok := parseVSCodeMarketplaceTime(publishedDate)
	if !ok {
		return "Marketplace extension age is unavailable"
	}
	ageDays := int(time.Since(published).Hours() / 24)
	minDays := int(minAge.Hours() / 24)
	return fmt.Sprintf("Marketplace extension is newly published: age %d days, minimum %d days", ageDays, minDays)
}

func applyVSCodeUpdateAge(finding safetyFinding, minAge time.Duration) safetyFinding {
	updated, ok := parseVSCodeMarketplaceTime(finding.LastUpdated)
	if minAge <= 0 || !ok {
		return finding
	}
	age := time.Since(updated)
	finding.ReleaseDate = updated.Format(time.RFC3339)
	finding.ReleaseAgeDays = int(age.Hours() / 24)
	finding.MinReleaseAgeDays = int(minAge.Hours() / 24)
	finding.Evidence = appendEvidence(finding.Evidence, "vscode-marketplace update-age")
	if age >= minAge {
		return finding
	}
	finding.Decision = "hold"
	finding.Confidence = "medium"
	finding.Reason = fmt.Sprintf("Marketplace extension update is too new: age %d days, minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays)
	finding.Remediation = "wait until the Marketplace extension update reaches the minimum age or add a temporary allow policy with reason and expiry after review"
	return finding
}

func parseVSCodeMarketplaceTime(value string) (time.Time, bool) {
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

func hasVSCodePostureReview(postures []vscodePosture) bool {
	return vscodePostureReviewCount(postures) > 0
}

func vscodePostureReviewCount(postures []vscodePosture) int {
	count := 0
	for _, posture := range postures {
		if securityDecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
