package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
	"github.com/webkaz-labs/updev/internal/updevpath"
	"github.com/webkaz-labs/updev/internal/vscode"
)

const defaultVSCodeMarketplaceURL = "https://marketplace.visualstudio.com/_apis/public/gallery/extensionquery?api-version=7.2-preview.1"

const (
	defaultVSCodeMinInstallCount     = 1000
	defaultVSCodeMinAverageRating    = 2.0
	defaultVSCodeMinExtensionAgeDays = 14
	defaultVSCodeMinUpdateAgeDays    = 3
	includeVSCodeEnvName             = "UPDEV_INCLUDE_VSCODE"
	vscodeMinInstallCountEnvName     = "UPDEV_VSCODE_MIN_INSTALL_COUNT"
	vscodeMinAverageRatingEnvName    = "UPDEV_VSCODE_MIN_AVERAGE_RATING"
	vscodeMinExtensionAgeDaysEnvName = "UPDEV_VSCODE_MIN_EXTENSION_AGE_DAYS"
	vscodeMinUpdateAgeDaysEnvName    = "UPDEV_VSCODE_MIN_UPDATE_AGE_DAYS"
)

type vscodePosture = vscode.Posture
type vscodeMarketplaceRequest = vscode.MarketplaceRequest
type vscodeMarketplaceFilter = vscode.MarketplaceFilter
type vscodeMarketplaceCriterion = vscode.MarketplaceCriterion
type vscodeMarketplaceResponse = vscode.MarketplaceResponse
type vscodeMarketplaceResult = vscode.MarketplaceResult
type vscodeExtension = vscode.Extension
type vscodePublisher = vscode.Publisher
type vscodeVersion = vscode.Version
type vscodeProperty = vscode.Property
type vscodeStatistic = vscode.Statistic

func includeVSCodeExtensionsByDefault() bool {
	if value, ok := boolEnv(includeVSCodeEnvName); ok {
		return value
	}
	if configured := loadUpdevConfig().Providers.IncludeVSCode; configured != nil {
		return *configured
	}
	return false
}

func providerFilterIsVSCode(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "vscode")
}

func kindFilterIsVSCode(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "vscode")
}

func vscodePosturesFromItems(ctx context.Context, client *http.Client, endpoint string, items []plan.Item) ([]vscodePosture, error) {
	return vscode.PosturesFromItems(ctx, client, endpoint, items, vscodeThresholds())
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
	return applySafetyFindings(gate, gate.Findings)
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
	return applySafetyFindings(gate, gate.Findings)
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
	versions, detail := vscode.InstalledVersions(ctx, commandRunner)
	if detail != "" {
		if gate != nil {
			gate.Warnings = append(gate.Warnings, "VS Code installed extension versions unavailable: "+detail)
		}
		return nil
	}
	return versions
}

func vscodeItemsFromBrewfile(root string) ([]plan.Item, error) {
	path := updevpath.HomeOrRootBrewfile(root)
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
		ReasonCode:        posture.ReasonCode,
		ReasonArgs:        posture.ReasonArgs,
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
	finding.Confidence = "high"
	finding.Evidence = appendEvidence(finding.Evidence, "osv-vscode")
	finding.AdvisoryIDs = appendUniqueString(finding.AdvisoryIDs, advisory.VulnID)
	setSafetyFindingReason(&finding, securityreason.VSCodePostureReason(securityreason.VSCodeAdvisoryMatch, finding.Name, "OSV advisory match for VS Code extension version: "+strings.Join(finding.AdvisoryIDs, ","), map[string]string{"advisory_ids": strings.Join(finding.AdvisoryIDs, ",")}))
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
	return vscode.FetchMarketplaceExtension(ctx, client, endpoint, extension)
}

func vscodePostureFromMetadata(requestedName string, metadata vscodeExtension) vscodePosture {
	return vscode.PostureFromMetadata(requestedName, metadata, vscodeThresholds())
}

func vscodePostureUnavailable(extension string, err error) vscodePosture {
	return vscode.PostureUnavailable(extension, err)
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
	return vscode.StatisticValue(statistics, name)
}

func vscodePropertyValue(properties []vscodeProperty, key string) string {
	return vscode.PropertyValue(properties, key)
}

func vscodeMarketplaceURL() string {
	return configuredEnvString(defaultVSCodeMarketplaceURL, "UPDEV_VSCODE_MARKETPLACE_URL")
}

func vscodeThresholds() vscode.Thresholds {
	return vscode.Thresholds{
		MinInstallCount:  minVSCodeInstallCount(),
		MinAverageRating: minVSCodeAverageRating(),
		MinExtensionAge:  minVSCodeExtensionAge(),
	}
}

func minVSCodeInstallCount() float64 {
	return minVSCodeInstallCountWithConfig(loadUpdevConfig())
}

func minVSCodeInstallCountWithConfig(config updevConfig) float64 {
	return configuredNonNegativeFloat(float64(defaultVSCodeMinInstallCount), config.Security.VSCode.MinInstallCount, vscodeMinInstallCountEnvName)
}

func minVSCodeAverageRating() float64 {
	return minVSCodeAverageRatingWithConfig(loadUpdevConfig())
}

func minVSCodeAverageRatingWithConfig(config updevConfig) float64 {
	return configuredNonNegativeFloat(defaultVSCodeMinAverageRating, config.Security.VSCode.MinAverageRating, vscodeMinAverageRatingEnvName)
}

func minVSCodeExtensionAge() time.Duration {
	return minVSCodeExtensionAgeWithConfig(loadUpdevConfig())
}

func minVSCodeExtensionAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(defaultVSCodeMinExtensionAgeDays, config.Security.VSCode.MinExtensionAgeDays, vscodeMinExtensionAgeDaysEnvName)
	return time.Duration(days) * 24 * time.Hour
}

func minVSCodeUpdateAge() time.Duration {
	return minVSCodeUpdateAgeWithConfig(loadUpdevConfig())
}

func minVSCodeUpdateAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(defaultVSCodeMinUpdateAgeDays, config.Security.VSCode.MinUpdateAgeDays, vscodeMinUpdateAgeDaysEnvName)
	return time.Duration(days) * 24 * time.Hour
}

func vscodeExtensionTooNew(publishedDate string, minAge time.Duration) bool {
	return vscode.ExtensionTooNew(publishedDate, minAge)
}

func vscodeExtensionAgeReason(publishedDate string, minAge time.Duration) string {
	return vscode.ExtensionAgeReason(publishedDate, minAge)
}

func applyVSCodeUpdateAge(finding safetyFinding, minAge time.Duration) safetyFinding {
	updated, ok := parseVSCodeMarketplaceTime(finding.LastUpdated)
	if minAge <= 0 || !ok {
		return finding
	}
	finding, age := securitygate.AnnotateReleaseAge(finding, updated, minAge, "vscode-marketplace update-age")
	if age >= minAge {
		return finding
	}
	finding.Decision = "hold"
	finding.Confidence = "medium"
	setSafetyFindingReason(&finding, securityreason.VSCodePostureReason(securityreason.VSCodeExtensionTooNew, finding.Name, fmt.Sprintf("Marketplace extension update is too new: age %d days, minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays), map[string]string{
		"age_days":     fmt.Sprintf("%d", finding.ReleaseAgeDays),
		"min_age_days": fmt.Sprintf("%d", finding.MinReleaseAgeDays),
	}))
	finding.Remediation = "wait until the Marketplace extension update reaches the minimum age or add a temporary allow policy with reason and expiry after review"
	return finding
}

func parseVSCodeMarketplaceTime(value string) (time.Time, bool) {
	return vscode.ParseMarketplaceTime(value)
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
