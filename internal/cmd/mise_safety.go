package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/registryaudit"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

const miseNativeReleaseAgeSource = "mise-native-minimum-release-age"

func miseMinimumReleaseAgeGateEvidence(ctx context.Context, commandRunner commandRunner, root string) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"settings", "ls", "--json-extended"}
	if strings.TrimSpace(root) != "" {
		args = append(args, "--cd", root)
	}
	result := commandRunner.Run(ctx, "mise", args...)
	if result.Err != nil || result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return []string{"mise minimum_release_age evidence unavailable: " + miseOutdatedResultDetail(result, "mise settings output is empty")}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return []string{"mise minimum_release_age evidence unavailable: settings JSON parse failed"}
	}
	value, source, ok := mise.MinimumReleaseAgeFromSettings(payload)
	if !ok || value == "" {
		return []string{"mise minimum_release_age inactive"}
	}
	if source != "" {
		return []string{"mise minimum_release_age active: " + value + " from " + source}
	}
	return []string{"mise minimum_release_age active: " + value}
}

func miseNativeReleaseAgeHoldFindings(ctx context.Context, commandRunner commandRunner, root string, normal []safetyFinding) ([]safetyFinding, []string) {
	disabled, warnings, err := parseMiseOutdatedResult(runMiseOutdatedJSONAgeDisabled(ctx, commandRunner, root))
	if err != nil {
		return nil, []string{"mise minimum_release_age hold comparison unavailable: " + err.Error()}
	}
	held, moreWarnings := miseNativeReleaseAgeHoldFindingsFrom(normal, disabled, "mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d")
	return held, append(warnings, moreWarnings...)
}

func miseNativeReleaseAgeHoldFinding(finding safetyFinding, reason string) safetyFinding {
	finding.Source = miseNativeReleaseAgeSource
	finding.Decision = "hold"
	finding.Reason = reason
	finding.Remediation = "wait until mise minimum_release_age allows this candidate, or add a temporary policy allow after review"
	finding.Confidence = "medium"
	finding.Evidence = appendEvidence(finding.Evidence, "mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d")
	finding.Evidence = appendEvidence(finding.Evidence, "mise minimum_release_age provider comparison")
	return finding
}

func enrichMiseSafetyFindings(ctx context.Context, commandRunner commandRunner, client *http.Client, findings []safetyFinding, minReleaseAge time.Duration) []safetyFinding {
	registry := miseRegistryIndex(ctx, commandRunner)
	out := make([]safetyFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Provider != "mise" {
			out = append(out, finding)
			continue
		}
		nativeHold := finding.Source == miseNativeReleaseAgeSource
		originalReason := finding.Reason
		switch {
		case strings.HasPrefix(finding.Name, "github:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "aqua:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseAquaReleaseAge(ctx, client, finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "npm:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseNPMReleaseAge(ctx, client, npmRegistryURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "cargo:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMiseCargoReleaseAge(ctx, client, cratesIOAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		case strings.HasPrefix(finding.Name, "pipx:"):
			out = append(out, preserveMiseNativeReleaseAgeHold(applyMisePyPIReleaseAge(ctx, client, pypiAPIURL(), finding, minReleaseAge), nativeHold, originalReason))
		default:
			if enriched, ok := applyMiseCoreReleaseAge(ctx, client, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			if enriched, ok := applyMiseRegistryGitHubReleaseAge(ctx, client, registry, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			if enriched, ok := applyMiseRegistryProviderMetadataReleaseAge(ctx, client, registry, finding, minReleaseAge); ok {
				out = append(out, preserveMiseNativeReleaseAgeHold(enriched, nativeHold, originalReason))
				continue
			}
			finding.Decision = "review"
			setSafetyFindingReason(&finding, securityreason.MiseOpaqueBackendReason())
			finding.Remediation = "keep the update held until mise native policy evidence or provider metadata can be verified"
			finding.Confidence = "low"
			out = append(out, preserveMiseNativeReleaseAgeHold(finding, nativeHold, originalReason))
		}
	}
	return out
}

func applyMiseCoreReleaseAge(ctx context.Context, client *http.Client, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	repo, tags, ok := miseCoreGitHubRelease(finding)
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.RepositoryURL = "https://github.com/" + repo
	finding.URL = finding.RepositoryURL
	finding.Evidence = appendEvidence(finding.Evidence, "mise core backend "+originalName)
	for _, tag := range tags {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, githubAPIURL(), repo, tag, true)
		if err == nil {
			enriched := applyMiseReleaseAgeFromTime(finding, firstNonEmpty(release.PublishedAt, release.CreatedAt), minAge, evidence)
			enriched.Name = originalName
			enriched.Kind = "tool"
			return enriched, true
		}
	}
	finding.Name = originalName
	finding.Kind = "tool"
	finding.Evidence = appendEvidence(finding.Evidence, "GitHub release/tag metadata")
	return miseReviewFinding(finding, "GitHub release/tag date unavailable before mise core update", "retry when GitHub metadata is reachable or review the core runtime release manually before allowing"), true
}

func miseCoreGitHubRelease(finding safetyFinding) (string, []string, bool) {
	name := strings.TrimSpace(finding.Name)
	version := miseCandidateVersion(finding)
	if version == "" {
		return "", nil, false
	}
	switch name {
	case "go":
		return "golang/go", []string{"go" + version}, true
	case "node":
		return "nodejs/node", []string{"v" + version, version}, true
	case "rust":
		return "rust-lang/rust", []string{version}, true
	default:
		return "", nil, false
	}
}

func applyMiseAquaReleaseAge(ctx context.Context, client *http.Client, finding safetyFinding, minAge time.Duration) safetyFinding {
	repo := strings.TrimSpace(strings.TrimPrefix(finding.Name, "aqua:"))
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !githubrepo.ValidPathPart(parts[0]) || !githubrepo.ValidPathPart(parts[1]) {
		return miseReviewFinding(finding, "mise aqua backend did not expose a valid GitHub repository", "review the mise aqua backend source before allowing this update")
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.Evidence = appendEvidence(finding.Evidence, "mise aqua backend "+repo)
	enriched := applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minAge)
	enriched.Name = originalName
	enriched.Kind = "tool"
	return enriched
}

func miseRegistryIndex(ctx context.Context, commandRunner commandRunner) map[string]mise.RegistryEntry {
	if commandRunner == nil {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := commandRunner.Run(requestCtx, "mise", "registry", "--json")
	if result.Err != nil || result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	return mise.RegistryIndexFromJSON(result.Stdout)
}

func applyMiseRegistryGitHubReleaseAge(ctx context.Context, client *http.Client, registry map[string]mise.RegistryEntry, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	entry, ok := mise.RegistryEntryForTool(registry, finding.Name)
	if !ok {
		return finding, false
	}
	backend, repo, ok := mise.RegistryGitHubBackend(entry)
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Name = "github:" + repo
	finding.Evidence = appendEvidence(finding.Evidence, "mise registry backend "+backend)
	enriched := applyMiseGitHubReleaseAge(ctx, client, githubAPIURL(), finding, minAge)
	enriched.Name = originalName
	enriched.Kind = "tool"
	return enriched, true
}

func applyMiseRegistryProviderMetadataReleaseAge(ctx context.Context, client *http.Client, registry map[string]mise.RegistryEntry, finding safetyFinding, minAge time.Duration) (safetyFinding, bool) {
	entry, ok := mise.RegistryEntryForTool(registry, finding.Name)
	if !ok {
		return finding, false
	}
	backend, metadata, ok := mise.RegistryProviderMetadataBackend(entry, mise.ProviderMetadataRegistry())
	if !ok {
		return finding, false
	}
	originalName := finding.Name
	finding.Kind = "tool"
	finding.Evidence = appendEvidence(finding.Evidence, "mise registry backend "+backend)
	finding.Evidence = appendEvidence(finding.Evidence, "provider metadata "+metadata.ID)
	switch metadata.ResolverType {
	case mise.ResolverVendorReleaseNotes:
		enriched := applyMiseVendorReleaseNotesAge(ctx, client, metadata, finding, minAge)
		enriched.Name = originalName
		return enriched, true
	default:
		finding.Name = originalName
		return miseReviewFinding(finding, "provider metadata resolver is unsupported for mise backend", "keep the candidate held until updev can resolve provider metadata for this backend"), true
	}
}

func applyMiseVendorReleaseNotesAge(ctx context.Context, client *http.Client, metadata mise.ProviderMetadataEntry, finding safetyFinding, minAge time.Duration) safetyFinding {
	version := miseCandidateVersion(finding)
	if version == "" {
		return miseReviewFinding(finding, "mise provider metadata candidate version is empty", "retry after mise reports a concrete candidate version")
	}
	releaseDate, err := mise.FetchVendorReleaseNoteDate(ctx, client, metadata, version)
	finding.URL = mise.ProviderMetadataURL(metadata)
	finding.SupportURL = metadata.SupportURL
	finding.Evidence = appendEvidence(finding.Evidence, metadata.Evidence)
	if err != nil {
		return miseReviewFinding(finding, "vendor release notes metadata unavailable before mise update: "+err.Error(), "retry when vendor release notes are reachable or review the upstream release manually before allowing")
	}
	return applyMiseReleaseAgeFromTime(finding, releaseDate.Format(time.RFC3339), minAge, metadata.Evidence)
}

func preserveMiseNativeReleaseAgeHold(finding safetyFinding, nativeHold bool, reason string) safetyFinding {
	if !nativeHold {
		return finding
	}
	finding.Source = miseNativeReleaseAgeSource
	finding.Decision = "hold"
	setSafetyFindingReason(&finding, securityreason.MiseMinimumAgeHeldReason(firstNonEmpty(reason, "mise minimum_release_age held this candidate")))
	finding.Remediation = "wait until mise minimum_release_age allows this candidate, or add a temporary policy allow after review"
	if finding.Confidence == "" || finding.Confidence == "low" {
		finding.Confidence = "medium"
	}
	return finding
}

func applyMiseGitHubReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	repo, ok := miseGitHubRepository(finding.Name)
	if !ok {
		return miseReviewFinding(finding, "mise github backend did not expose a valid GitHub repository", "review the mise backend source before allowing this update")
	}
	finding.RepositoryURL = "https://github.com/" + repo
	finding.URL = finding.RepositoryURL
	if minAge <= 0 {
		return allowMiseFinding(finding, "mise github backend candidate accepted because release-age gate is disabled", "GitHub repository metadata")
	}
	tags := miseGitHubReleaseTags(finding)
	for _, tag := range tags {
		release, evidence, err := fetchGitHubReleaseOrTagByTag(ctx, client, apiBase, repo, tag, true)
		if err == nil {
			return applyMiseReleaseAgeFromTime(finding, firstNonEmpty(release.PublishedAt, release.CreatedAt), minAge, evidence)
		}
	}
	finding.Evidence = appendEvidence(finding.Evidence, "GitHub release/tag metadata")
	return miseReviewFinding(finding, "GitHub release/tag date unavailable before mise update", "retry when GitHub metadata is reachable or review the upstream release manually before allowing")
}

func applyMiseNPMReleaseAge(ctx context.Context, client *http.Client, registryBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	pkg := strings.TrimSpace(strings.TrimPrefix(finding.Name, "npm:"))
	if pkg == "" {
		return miseReviewFinding(finding, "mise npm backend package name is empty", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchNPMMetadata(ctx, client, registryBase, pkg)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "npm registry metadata")
		return miseReviewFinding(finding, "npm registry metadata unavailable before mise update: "+err.Error(), "retry when the registry is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	versionInfo := metadata.Versions[version]
	deprecated := firstNonEmpty(versionInfo.Deprecated, metadata.Deprecated)
	finding.Kind = "npm"
	finding.RepositoryURL = registryaudit.NormalizeNPMRepositoryURL(firstNonEmpty(versionInfo.Repository.URL, metadata.Repository.URL))
	finding.URL = "https://www.npmjs.com/package/" + pkg
	finding.Evidence = appendEvidence(finding.Evidence, "npm registry metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise npm candidate version is empty", "retry after mise reports a concrete candidate version")
	case versionInfo.Version == "":
		return miseReviewFinding(finding, "mise npm candidate version is not present in registry metadata", "verify the candidate version before allowing this update")
	case deprecated != "":
		return miseReviewFinding(finding, "mise npm candidate version is deprecated: "+deprecated, "replace the deprecated npm package version or update to a non-deprecated version after review")
	case len(metadata.Maintainers) == 0:
		return miseReviewFinding(finding, "npm package has no maintainers in registry metadata", "review package ownership and source provenance before allowing this update")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "npm package does not expose a source repository URL", "review package provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, metadata.Time[version], minAge, "npm publish metadata")
}

func applyMiseCargoReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	crate := strings.TrimSpace(strings.TrimPrefix(finding.Name, "cargo:"))
	if crate == "" || strings.ContainsAny(crate, " \t\n\r/") {
		return miseReviewFinding(finding, "mise cargo backend crate name is invalid", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchCratesIOMetadata(ctx, client, apiBase, crate)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "crates.io metadata")
		return miseReviewFinding(finding, "crates.io metadata unavailable before mise update: "+err.Error(), "retry when crates.io is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	versionInfo, versionFound := registryaudit.CratesIOVersionByNumber(metadata.Versions, version)
	finding.Kind = "cargo"
	finding.RepositoryURL = metadata.Crate.Repository
	finding.URL = "https://crates.io/crates/" + crate
	finding.Evidence = appendEvidence(finding.Evidence, "crates.io metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise cargo candidate version is empty", "retry after mise reports a concrete candidate version")
	case !versionFound:
		return miseReviewFinding(finding, "mise cargo candidate version is not present in crates.io metadata", "verify the candidate version before allowing this update")
	case versionInfo.Yanked:
		return miseReviewFinding(finding, "mise cargo candidate version is yanked", "update to a non-yanked crate version or replace the crate")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "crate does not expose a source repository URL", "review crate provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, versionInfo.CreatedAt, minAge, "crates.io publish metadata")
}

func applyMisePyPIReleaseAge(ctx context.Context, client *http.Client, apiBase string, finding safetyFinding, minAge time.Duration) safetyFinding {
	pkg := strings.TrimSpace(strings.TrimPrefix(finding.Name, "pipx:"))
	if pkg == "" || strings.ContainsAny(pkg, " \t\n\r/") {
		return miseReviewFinding(finding, "mise pipx backend package name is invalid", "review the mise entry before allowing this update")
	}
	metadata, err := registryaudit.FetchPyPIMetadata(ctx, client, apiBase, pkg)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, "PyPI metadata")
		return miseReviewFinding(finding, "PyPI metadata unavailable before mise update: "+err.Error(), "retry when PyPI is reachable or review manually before allowing")
	}
	version := miseCandidateVersion(finding)
	release, releaseFound := registryaudit.PyPIReleaseForVersion(metadata.Releases, version)
	finding.Kind = "pipx"
	finding.RepositoryURL = registryaudit.PyPIRepositoryURL(metadata.Info.ProjectURLs)
	finding.URL = "https://pypi.org/project/" + pkg
	finding.Evidence = appendEvidence(finding.Evidence, "PyPI metadata")
	switch {
	case version == "":
		return miseReviewFinding(finding, "mise pipx candidate version is empty", "retry after mise reports a concrete candidate version")
	case !releaseFound:
		return miseReviewFinding(finding, "mise pipx candidate version is not present in PyPI metadata", "verify the candidate version before allowing this update")
	case release.Yanked:
		reason := "mise pipx candidate version is yanked"
		if release.YankedReason != "" {
			reason += ": " + release.YankedReason
		}
		return miseReviewFinding(finding, reason, "update to a non-yanked PyPI release or replace the package")
	case finding.RepositoryURL == "":
		finding.Confidence = "low"
		return miseReviewFinding(finding, "PyPI package does not expose a source repository URL", "review package provenance manually before allowing this update")
	}
	return applyMiseReleaseAgeFromTime(finding, release.UploadTimeISO8601, minAge, "PyPI upload metadata")
}

func applyMiseReleaseAgeFromTime(finding safetyFinding, rawTime string, minAge time.Duration, evidence string) safetyFinding {
	releasedAt, err := parseMiseReleaseTime(rawTime)
	if err != nil {
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
		return miseReviewFinding(finding, "candidate release date unavailable before mise update: "+err.Error(), "retry when provider metadata includes a release date or review manually before allowing")
	}
	finding = securitygate.AnnotateReleaseDate(finding, releasedAt, evidence)
	finding.PublishedDate = finding.ReleaseDate
	if minAge <= 0 {
		return allowMiseFinding(finding, "mise candidate accepted because release-age gate is disabled", "")
	}
	finding, age := securitygate.AnnotateReleaseAge(finding, releasedAt, minAge, "")
	if age < minAge {
		finding.Decision = "hold"
		setSafetyFindingReason(&finding, securityreason.MiseReleaseTooNewReason(finding.ReleaseAgeDays, finding.MinReleaseAgeDays))
		finding.Remediation = "wait until the release reaches the minimum age or allow temporarily by policy after review"
		finding.Confidence = "medium"
		return finding
	}
	return allowMiseFinding(finding, fmt.Sprintf("mise candidate release age passed: age %d days, minimum %d days", finding.ReleaseAgeDays, finding.MinReleaseAgeDays), "")
}

func allowMiseFinding(finding safetyFinding, reason string, evidence string) safetyFinding {
	finding.Decision = "allow"
	setSafetyFindingReasonText(&finding, reason)
	finding.Remediation = ""
	if finding.Confidence == "" || finding.Confidence == "low" {
		finding.Confidence = "medium"
	}
	if evidence != "" {
		finding.Evidence = appendEvidence(finding.Evidence, evidence)
	}
	return finding
}

func miseReviewFinding(finding safetyFinding, reason string, remediation string) safetyFinding {
	finding.Decision = "review"
	setSafetyFindingReasonText(&finding, reason)
	finding.Remediation = remediation
	if finding.Confidence == "" {
		finding.Confidence = "low"
	}
	return finding
}

func miseCandidateVersion(finding safetyFinding) string {
	return strings.TrimSpace(firstNonEmpty(finding.CurrentVersion, finding.Version))
}

func miseGitHubRepository(name string) (string, bool) {
	repo := strings.TrimSpace(strings.TrimPrefix(name, "github:"))
	repo = strings.TrimSuffix(repo, ".git")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !githubrepo.ValidPathPart(parts[0]) || !githubrepo.ValidPathPart(parts[1]) {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

func miseGitHubReleaseTags(finding safetyFinding) []string {
	version := miseCandidateVersion(finding)
	if version == "" {
		return nil
	}
	tags := []string{}
	tags = appendUniqueString(tags, version)
	if !strings.HasPrefix(strings.ToLower(version), "v") {
		tags = appendUniqueString(tags, "v"+version)
	}
	_, repoName, ok := strings.Cut(strings.TrimPrefix(finding.Name, "github:"), "/")
	if ok && repoName != "" {
		tags = appendUniqueString(tags, repoName+"-"+version)
		if !strings.HasPrefix(strings.ToLower(version), "v") {
			tags = appendUniqueString(tags, repoName+"-v"+version)
		}
	}
	return tags
}

func parseMiseReleaseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("release timestamp is empty")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported release timestamp: %s", value)
}

func minMiseReleaseAge() time.Duration {
	return minMiseReleaseAgeWithConfig(loadUpdevConfig())
}

func minMiseReleaseAgeWithConfig(config updevConfig) time.Duration {
	days := configuredNonNegativeInt(3, config.Security.Mise.MinReleaseAgeDays, "UPDEV_MISE_MIN_RELEASE_AGE_DAYS")
	return time.Duration(days) * 24 * time.Hour
}
