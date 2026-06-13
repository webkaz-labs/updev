package securityreason

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	CandidateReleaseTooNew       = "candidate_release_too_new"
	MiseReleaseTooNew            = "mise_release_too_new"
	MiseMinimumAgeHeld           = "mise_minimum_age_held"
	SecurityPolicyOverride       = "security_policy_override"
	MiseOpaqueBackend            = "mise_opaque_backend"
	ScannerVulnerability         = "scanner_vulnerability"
	ScannerSecret                = "scanner_secret"
	ScannerWorkflow              = "scanner_workflow"
	ScannerMisconfiguration      = "scanner_misconfiguration"
	NativeAuditVulnerability     = "native_audit_vulnerability"
	GitHubRepositoryDisabled     = "github_repository_disabled"
	GitHubRepositoryArchived     = "github_repository_archived"
	GitHubRepositoryPrivate      = "github_repository_private"
	GitHubDependabotDisabled     = "github_dependabot_disabled"
	GitHubSecretScanningDisabled = "github_secret_scanning_disabled"
	GitHubPushProtectionDisabled = "github_push_protection_disabled"
	GitHubRepositoryTooNew       = "github_repository_too_new"
	GitHubMetadataUnavailable    = "github_metadata_unavailable"
	HomebrewEvidenceUnavailable  = "homebrew_evidence_unavailable"
	HomebrewMetadataUnavailable  = "homebrew_metadata_unavailable"
	HomebrewReleaseUnavailable   = "homebrew_release_unavailable"
	HomebrewOfficialFormula      = "homebrew_official_formula"
	HomebrewAdvisoryMatch        = "homebrew_advisory_match"
	HomebrewURLBasedCask         = "homebrew_url_based_cask"
	HomebrewNonOfficialTap       = "homebrew_non_official_tap"
	HomebrewCaskHostMismatch     = "homebrew_cask_host_mismatch"
	HomebrewCaskProvenanceReview = "homebrew_cask_provenance_review"
	HomebrewEntryDisabled        = "homebrew_entry_disabled"
	HomebrewEntryDeprecated      = "homebrew_entry_deprecated"
	VSCodeMarketplaceUnavailable = "vscode_marketplace_unavailable"
	VSCodeNotPublic              = "vscode_not_public"
	VSCodeNotValidated           = "vscode_not_validated"
	VSCodePublisherUnverified    = "vscode_publisher_unverified"
	VSCodeCodeMissingRepository  = "vscode_code_missing_repository"
	VSCodeMissingRepository      = "vscode_missing_repository"
	VSCodeExtensionTooNew        = "vscode_extension_too_new"
	VSCodeLowInstallCount        = "vscode_low_install_count"
	VSCodeLowRating              = "vscode_low_rating"
	VSCodeMarketplaceAllowed     = "vscode_marketplace_allowed"
	VSCodeAdvisoryMatch          = "vscode_advisory_match"
	RegistryMetadataUnavailable  = "registry_metadata_unavailable"
	RegistryVersionMissing       = "registry_version_missing"
	RegistryVersionDeprecated    = "registry_version_deprecated"
	RegistryNoMaintainers        = "registry_no_maintainers"
	RegistryMissingRepository    = "registry_missing_repository"
	RegistryVersionYanked        = "registry_version_yanked"
)

type Reason struct {
	Code string
	Text string
	Args map[string]string
}

func New(code string, text string, args map[string]string) Reason {
	return Reason{Code: code, Text: text, Args: cleanArgs(args)}
}

func CandidateReleaseTooNewReason(ageDays int, minDays int) Reason {
	return New(CandidateReleaseTooNew, fmt.Sprintf("candidate release is too new: age %d days, minimum %d days", ageDays, minDays), ageArgs(ageDays, minDays))
}

func MiseReleaseTooNewReason(ageDays int, minDays int) Reason {
	return New(MiseReleaseTooNew, fmt.Sprintf("mise candidate release is too new: age %d days, minimum %d days", ageDays, minDays), ageArgs(ageDays, minDays))
}

func MiseMinimumAgeHeldReason(text string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "mise minimum_release_age held this candidate"
	}
	return New(MiseMinimumAgeHeld, text, nil)
}

func SecurityPolicyOverrideReason(decision string, text string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "security policy override"
	}
	return New(SecurityPolicyOverride, text, map[string]string{"decision": decision, "reason_text": text})
}

func MiseOpaqueBackendReason() Reason {
	return New(MiseOpaqueBackend, "mise backend is unsupported or opaque for updev-owned release-age evidence", nil)
}

func ScannerVulnerabilityReason(tool string, dependencyKind string) Reason {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		tool = "scanner"
	}
	text := tool + " reported vulnerability"
	args := map[string]string{"tool": tool}
	dependencyKind = strings.TrimSpace(dependencyKind)
	switch dependencyKind {
	case "direct":
		text += " in a directly managed package"
		args["dependency_kind"] = dependencyKind
	case "transitive":
		text += " in a transitive dependency"
		args["dependency_kind"] = dependencyKind
	}
	return New(ScannerVulnerability, text, args)
}

func ScannerFindingReason(code string, tool string, text string) Reason {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		tool = "scanner"
	}
	text = strings.TrimSpace(text)
	if text == "" {
		switch code {
		case ScannerSecret:
			text = tool + " reported possible secret"
		case ScannerWorkflow:
			text = tool + " reported workflow security finding"
		case ScannerMisconfiguration:
			text = tool + " reported misconfiguration"
		default:
			text = tool + " reported security finding"
		}
	}
	return New(code, text, map[string]string{"tool": tool})
}

func NativeAuditVulnerabilityReason(tool string, ecosystem string, target string, text string) Reason {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		tool = "native-audit"
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = tool + " reported vulnerabilities"
	}
	return New(NativeAuditVulnerability, text, map[string]string{
		"tool":      tool,
		"ecosystem": ecosystem,
		"target":    target,
	})
}

func GitHubPostureReason(code string, repository string, text string, args map[string]string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "GitHub repository posture requires review"
	}
	values := map[string]string{"repository": repository}
	for key, value := range args {
		values[key] = value
	}
	return New(code, text, values)
}

func HomebrewPostureReason(code string, kind string, name string, text string, args map[string]string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Homebrew posture requires review"
	}
	values := map[string]string{"kind": kind, "name": name}
	for key, value := range args {
		values[key] = value
	}
	return New(code, text, values)
}

func HomebrewCaskProvenanceReason(name string, text string, homepageHost string, urlHost string) Reason {
	code := HomebrewCaskProvenanceReview
	args := map[string]string{}
	if homepageHost != "" {
		args["homepage_host"] = homepageHost
	}
	if urlHost != "" {
		args["url_host"] = urlHost
	}
	if homepageHost != "" && urlHost != "" && homepageHost != urlHost {
		code = HomebrewCaskHostMismatch
	}
	return HomebrewPostureReason(code, "cask", name, text, args)
}

func VSCodePostureReason(code string, extension string, text string, args map[string]string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "VS Code Marketplace posture requires review"
	}
	values := map[string]string{"extension": extension}
	for key, value := range args {
		values[key] = value
	}
	return New(code, text, values)
}

func RegistryPostureReason(code string, registry string, packageName string, version string, text string) Reason {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "registry posture requires review"
	}
	return New(code, text, map[string]string{
		"registry": registry,
		"package":  packageName,
		"version":  version,
	})
}

func Infer(text string) Reason {
	text = strings.TrimSpace(text)
	switch text {
	case "":
		return Reason{}
	case "mise backend is unsupported or opaque for updev-owned release-age evidence":
		return MiseOpaqueBackendReason()
	case "mise minimum_release_age held candidate before it appeared in normal outdated output", "mise minimum_release_age held this candidate":
		return MiseMinimumAgeHeldReason(text)
	case "security policy override":
		return SecurityPolicyOverrideReason("", text)
	}
	if reason, ok := inferScannerVulnerabilityReason(text); ok {
		return reason
	}
	if reason, ok := inferScannerFindingReason(text); ok {
		return reason
	}
	if reason, ok := inferNativeAuditVulnerabilityReason(text); ok {
		return reason
	}
	if reason, ok := inferRegistryPostureReason(text); ok {
		return reason
	}
	if reason, ok := inferGitHubPostureReason(text); ok {
		return reason
	}
	if reason, ok := inferHomebrewPostureReason(text); ok {
		return reason
	}
	if reason, ok := inferVSCodePostureReason(text); ok {
		return reason
	}
	if age, minAge, ok := parseAgeReason(text, "candidate release is too new: age ", " days, minimum ", " days"); ok {
		return CandidateReleaseTooNewReason(age, minAge)
	}
	if age, minAge, ok := parseAgeReason(text, "mise candidate release is too new: age ", " days, minimum ", " days"); ok {
		return MiseReleaseTooNewReason(age, minAge)
	}
	if strings.HasPrefix(text, "mise minimum_release_age held newer candidate ") {
		return MiseMinimumAgeHeldReason(text)
	}
	return Reason{Text: text}
}

func LocalizeJapanese(reason Reason) string {
	if reason.Code == "" {
		return reason.Text
	}
	switch reason.Code {
	case CandidateReleaseTooNew:
		return fmt.Sprintf("候補リリースが新しすぎます: 経過 %s日、最小 %s日", arg(reason, "age_days"), arg(reason, "min_age_days"))
	case MiseReleaseTooNew:
		return fmt.Sprintf("mise 候補リリースが新しすぎます: 経過 %s日、最小 %s日", arg(reason, "age_days"), arg(reason, "min_age_days"))
	case MiseMinimumAgeHeld:
		return localizeMiseMinimumAgeHeld(reason.Text)
	case SecurityPolicyOverride:
		decision := arg(reason, "decision")
		text := arg(reason, "reason_text")
		if text == "" {
			text = reason.Text
		}
		if decision == "" {
			return text
		}
		if text == "" || text == "security policy override" {
			return "security policy により " + decision + " されています"
		}
		return "security policy により " + decision + " されています: " + text
	case MiseOpaqueBackend:
		return "mise のバックエンドから updev がリリース経過日数の根拠を十分に確認できないため、確認が必要です"
	case ScannerVulnerability:
		tool := arg(reason, "tool")
		if tool == "" {
			tool = "scanner"
		}
		switch arg(reason, "dependency_kind") {
		case "direct":
			return tool + " が直接管理している依存関係の脆弱性を検出しました"
		case "transitive":
			return tool + " が推移的依存関係の脆弱性を検出しました"
		default:
			return tool + " が脆弱性を検出しました"
		}
	case ScannerSecret:
		return scannerTool(reason) + " が secret の可能性を報告しました"
	case ScannerWorkflow:
		return scannerTool(reason) + " が workflow security finding を報告しました"
	case ScannerMisconfiguration:
		return scannerTool(reason) + " が misconfiguration を報告しました"
	case NativeAuditVulnerability:
		tool := arg(reason, "tool")
		if tool == "" {
			tool = "native-audit"
		}
		ecosystem := arg(reason, "ecosystem")
		if ecosystem != "" {
			return tool + " が " + ecosystem + " の native audit で脆弱性を検出しました"
		}
		return tool + " が native audit で脆弱性を検出しました"
	case GitHubRepositoryDisabled:
		return githubPrefix(reason) + "repository が disabled です"
	case GitHubRepositoryArchived:
		return githubPrefix(reason) + "repository が archived です"
	case GitHubRepositoryPrivate:
		return githubPrefix(reason) + "repository が private です"
	case GitHubDependabotDisabled:
		return githubPrefix(reason) + "Dependabot security updates が disabled です"
	case GitHubSecretScanningDisabled:
		return githubPrefix(reason) + "secret scanning が disabled です"
	case GitHubPushProtectionDisabled:
		return githubPrefix(reason) + "secret scanning push protection が disabled です"
	case GitHubRepositoryTooNew:
		return fmt.Sprintf("%srepository が新しすぎます: 経過 %s日、最小 %s日", githubPrefix(reason), arg(reason, "age_days"), arg(reason, "min_age_days"))
	case GitHubMetadataUnavailable:
		return githubPrefix(reason) + "repository metadata を取得できないため確認が必要です"
	case HomebrewEvidenceUnavailable:
		return homebrewPrefix(reason) + "Homebrew の release-age/provenance 根拠が不足しているため確認が必要です"
	case HomebrewMetadataUnavailable:
		return homebrewPrefix(reason) + "Homebrew metadata を取得できないため確認が必要です"
	case HomebrewReleaseUnavailable:
		return homebrewPrefix(reason) + "Homebrew 更新前に upstream release 日時を確認できないため確認が必要です" + errorSuffix(reason)
	case HomebrewOfficialFormula:
		return homebrewPrefix(reason) + "公式 Homebrew formula metadata を確認済みです"
	case HomebrewAdvisoryMatch:
		source := arg(reason, "advisory_source")
		if source == "" {
			source = "advisory"
		}
		return homebrewPrefix(reason) + source + " が Homebrew 更新候補に一致しました" + advisorySuffix(reason)
	case HomebrewURLBasedCask:
		return homebrewPrefix(reason) + "URL 指定の Homebrew cask は配布元の確認が必要です"
	case HomebrewNonOfficialTap:
		return homebrewPrefix(reason) + "非公式 Homebrew tap は配布元の確認が必要です"
	case HomebrewCaskHostMismatch:
		homepageHost := arg(reason, "homepage_host")
		urlHost := arg(reason, "url_host")
		if homepageHost != "" && urlHost != "" {
			return homebrewPrefix(reason) + "Homebrew cask の download host (" + urlHost + ") が homepage host (" + homepageHost + ") と異なるため確認が必要です"
		}
		return homebrewPrefix(reason) + "Homebrew cask の download host が homepage host と異なるため確認が必要です"
	case HomebrewCaskProvenanceReview:
		return homebrewPrefix(reason) + "Homebrew cask の配布元確認が必要です"
	case HomebrewEntryDisabled:
		return homebrewPrefix(reason) + "Homebrew metadata で disabled です" + reasonTextSuffix(reason)
	case HomebrewEntryDeprecated:
		return homebrewPrefix(reason) + "Homebrew metadata で deprecated です" + reasonTextSuffix(reason)
	case VSCodeMarketplaceUnavailable:
		return vscodePrefix(reason) + "VS Code Marketplace metadata を取得できないため確認が必要です"
	case VSCodeNotPublic:
		return vscodePrefix(reason) + "VS Code Marketplace metadata で public と確認できません"
	case VSCodeNotValidated:
		return vscodePrefix(reason) + "VS Code Marketplace metadata で validated と確認できません"
	case VSCodePublisherUnverified:
		return vscodePrefix(reason) + "Marketplace metadata で publisher domain が verified ではありません"
	case VSCodeCodeMissingRepository:
		return vscodePrefix(reason) + "コード実行を伴う拡張ですが source repository が Marketplace metadata にありません"
	case VSCodeMissingRepository:
		return vscodePrefix(reason) + "source repository が VS Code Marketplace metadata にありません"
	case VSCodeExtensionTooNew:
		return fmt.Sprintf("%sVS Code extension が新しすぎます: 経過 %s日、最小 %s日", vscodePrefix(reason), arg(reason, "age_days"), arg(reason, "min_age_days"))
	case VSCodeLowInstallCount:
		return fmt.Sprintf("%sMarketplace install count が閾値未満です: %s installs、最小 %s", vscodePrefix(reason), arg(reason, "install_count"), arg(reason, "min_install_count"))
	case VSCodeLowRating:
		return fmt.Sprintf("%sMarketplace average rating が閾値未満です: %s、最小 %s", vscodePrefix(reason), arg(reason, "average_rating"), arg(reason, "min_average_rating"))
	case VSCodeMarketplaceAllowed:
		return vscodePrefix(reason) + "VS Code Marketplace metadata を確認済みです"
	case VSCodeAdvisoryMatch:
		return vscodePrefix(reason) + "OSV advisory が VS Code extension version に一致しました" + advisorySuffix(reason)
	case RegistryMetadataUnavailable:
		return registryPrefix(reason) + "metadata を取得できないため確認が必要です"
	case RegistryVersionMissing:
		return registryPrefix(reason) + "インストール済み version が registry metadata に存在しません"
	case RegistryVersionDeprecated:
		return registryPrefix(reason) + "インストール済み version が deprecated です"
	case RegistryNoMaintainers:
		return registryPrefix(reason) + "maintainer 情報が registry metadata にありません"
	case RegistryMissingRepository:
		return registryPrefix(reason) + "source repository URL が registry metadata にありません"
	case RegistryVersionYanked:
		return registryPrefix(reason) + "インストール済み version が yanked です"
	default:
		return reason.Text
	}
}

func cleanArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range args {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ageArgs(ageDays int, minDays int) map[string]string {
	return map[string]string{
		"age_days":     strconv.Itoa(ageDays),
		"min_age_days": strconv.Itoa(minDays),
	}
}

func arg(reason Reason, key string) string {
	if reason.Args == nil {
		return ""
	}
	return reason.Args[key]
}

func registryPrefix(reason Reason) string {
	registry := arg(reason, "registry")
	pkg := arg(reason, "package")
	if registry == "" && pkg == "" {
		return ""
	}
	if registry == "" {
		return pkg + ": "
	}
	if pkg == "" {
		return registry + ": "
	}
	return registry + "/" + pkg + ": "
}

func scannerTool(reason Reason) string {
	tool := arg(reason, "tool")
	if tool == "" {
		return "scanner"
	}
	return tool
}

func githubPrefix(reason Reason) string {
	repository := arg(reason, "repository")
	if repository == "" {
		return ""
	}
	return repository + ": "
}

func homebrewPrefix(reason Reason) string {
	kind := arg(reason, "kind")
	name := arg(reason, "name")
	if kind == "" && name == "" {
		return ""
	}
	if kind == "" {
		return name + ": "
	}
	if name == "" {
		return kind + ": "
	}
	return kind + "/" + name + ": "
}

func vscodePrefix(reason Reason) string {
	extension := arg(reason, "extension")
	if extension == "" {
		return ""
	}
	return extension + ": "
}

func reasonTextSuffix(reason Reason) string {
	text := arg(reason, "reason_text")
	if text == "" {
		text = reason.Text
	}
	if text == "" {
		return ""
	}
	return ": " + text
}

func errorSuffix(reason Reason) string {
	text := arg(reason, "error")
	if text == "" {
		return ""
	}
	return ": " + text
}

func advisorySuffix(reason Reason) string {
	ids := arg(reason, "advisory_ids")
	if ids == "" {
		return ""
	}
	return ": " + ids
}

func parseAgeReason(text string, prefix string, middle string, suffix string) (int, int, bool) {
	value, ok := strings.CutPrefix(text, prefix)
	if !ok {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, middle)
	if !ok {
		return 0, 0, false
	}
	right, ok = strings.CutSuffix(right, suffix)
	if !ok {
		return 0, 0, false
	}
	age, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return 0, 0, false
	}
	minAge, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return 0, 0, false
	}
	return age, minAge, true
}

func inferScannerVulnerabilityReason(text string) (Reason, bool) {
	for _, tool := range []string{"osv-scanner", "trivy", "grype"} {
		prefix := tool + " reported vulnerabilit"
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		matchStyle := ""
		if strings.HasSuffix(text, " via CPE-style match") {
			text = strings.TrimSuffix(text, " via CPE-style match")
			matchStyle = "cpe"
		}
		dependencyKind := ""
		switch text {
		case tool + " reported vulnerabilities", tool + " reported vulnerability":
		case tool + " reported vulnerability in a directly managed package":
			dependencyKind = "direct"
		case tool + " reported vulnerability in a transitive dependency":
			dependencyKind = "transitive"
		default:
			return Reason{}, false
		}
		reason := ScannerVulnerabilityReason(tool, dependencyKind)
		if matchStyle != "" {
			reason.Text += " via CPE-style match"
			if reason.Args == nil {
				reason.Args = map[string]string{}
			}
			reason.Args["match_style"] = matchStyle
		}
		return reason, true
	}
	return Reason{}, false
}

func inferGitHubPostureReason(text string) (Reason, bool) {
	switch {
	case text == "repository is disabled":
		return GitHubPostureReason(GitHubRepositoryDisabled, "", text, nil), true
	case text == "repository is archived":
		return GitHubPostureReason(GitHubRepositoryArchived, "", text, nil), true
	case text == "repository is private":
		return GitHubPostureReason(GitHubRepositoryPrivate, "", text, nil), true
	case text == "Dependabot security updates are disabled":
		return GitHubPostureReason(GitHubDependabotDisabled, "", text, nil), true
	case text == "secret scanning is disabled":
		return GitHubPostureReason(GitHubSecretScanningDisabled, "", text, nil), true
	case text == "secret scanning push protection is disabled":
		return GitHubPostureReason(GitHubPushProtectionDisabled, "", text, nil), true
	case strings.HasPrefix(text, "repository metadata unavailable:"):
		return GitHubPostureReason(GitHubMetadataUnavailable, "", text, nil), true
	}
	if age, minAge, ok := parseAgeReason(text, "tap repository is newly created: age ", " days, minimum ", " days"); ok {
		return GitHubPostureReason(GitHubRepositoryTooNew, "", text, ageArgs(age, minAge)), true
	}
	return Reason{}, false
}

func inferHomebrewPostureReason(text string) (Reason, bool) {
	switch {
	case strings.HasPrefix(text, "Homebrew metadata unavailable"):
		return HomebrewPostureReason(HomebrewMetadataUnavailable, "", "", text, nil), true
	case strings.HasPrefix(text, "GitHub release/tag date unavailable before update:"):
		return HomebrewPostureReason(HomebrewReleaseUnavailable, "", "", text, map[string]string{"error": strings.TrimSpace(strings.TrimPrefix(text, "GitHub release/tag date unavailable before update:"))}), true
	case strings.HasPrefix(text, "GitHub release date unavailable before update:"):
		return HomebrewPostureReason(HomebrewReleaseUnavailable, "", "", text, map[string]string{"error": strings.TrimSpace(strings.TrimPrefix(text, "GitHub release date unavailable before update:"))}), true
	case text == "official Homebrew formula metadata is available and not disabled or deprecated":
		return HomebrewPostureReason(HomebrewOfficialFormula, "brew", "", text, nil), true
	case strings.HasPrefix(text, "OSV advisory match for Homebrew source tag:"):
		return HomebrewPostureReason(HomebrewAdvisoryMatch, "", "", text, map[string]string{"advisory_source": "OSV source tag", "advisory_ids": strings.TrimSpace(strings.TrimPrefix(text, "OSV advisory match for Homebrew source tag:"))}), true
	case strings.HasPrefix(text, "GitHub Advisory match for curated Homebrew mapping:"):
		return HomebrewPostureReason(HomebrewAdvisoryMatch, "", "", text, map[string]string{"advisory_source": "GitHub Advisory", "advisory_ids": strings.TrimSpace(strings.TrimPrefix(text, "GitHub Advisory match for curated Homebrew mapping:"))}), true
	case strings.HasPrefix(text, "OSV advisory match for curated Homebrew mapping:"):
		return HomebrewPostureReason(HomebrewAdvisoryMatch, "", "", text, map[string]string{"advisory_source": "OSV curated mapping", "advisory_ids": strings.TrimSpace(strings.TrimPrefix(text, "OSV advisory match for curated Homebrew mapping:"))}), true
	case text == "release-age and provenance evidence are not available in the first Go safety slice":
		return HomebrewPostureReason(HomebrewEvidenceUnavailable, "", "", text, nil), true
	case text == "URL-based Homebrew cask needs manual provenance review", text == "URL-based Homebrew cask needs manual provenance review before update":
		return HomebrewPostureReason(HomebrewURLBasedCask, "cask", "", text, nil), true
	case text == "non-official Homebrew tap needs provenance review", text == "non-official Homebrew tap needs provenance review before update":
		return HomebrewPostureReason(HomebrewNonOfficialTap, "", "", text, nil), true
	case text == "Homebrew cask download host differs from homepage host; vendor provenance review required":
		return HomebrewPostureReason(HomebrewCaskHostMismatch, "cask", "", text, nil), true
	case text == "Homebrew cask update requires vendor provenance review", text == "Homebrew cask provenance needs review because homepage or download URL host is missing", text == "Homebrew cask updates need provenance and URL/release-age checks before strict mode can allow them":
		return HomebrewPostureReason(HomebrewCaskProvenanceReview, "cask", "", text, nil), true
	case text == "Homebrew metadata marks this entry disabled":
		return HomebrewPostureReason(HomebrewEntryDisabled, "", "", text, nil), true
	case text == "Homebrew metadata marks this entry deprecated":
		return HomebrewPostureReason(HomebrewEntryDeprecated, "", "", text, nil), true
	default:
		return Reason{}, false
	}
}

func inferVSCodePostureReason(text string) (Reason, bool) {
	switch {
	case strings.HasPrefix(text, "VS Code Marketplace metadata unavailable:"):
		return VSCodePostureReason(VSCodeMarketplaceUnavailable, "", text, nil), true
	case text == "Marketplace metadata does not mark this extension public":
		return VSCodePostureReason(VSCodeNotPublic, "", text, nil), true
	case text == "Marketplace metadata does not mark this extension validated":
		return VSCodePostureReason(VSCodeNotValidated, "", text, nil), true
	case text == "publisher domain is not verified in Marketplace metadata":
		return VSCodePostureReason(VSCodePublisherUnverified, "", text, nil), true
	case text == "extension executes code but Marketplace metadata does not expose a source repository":
		return VSCodePostureReason(VSCodeCodeMissingRepository, "", text, nil), true
	case text == "Marketplace metadata does not expose a source repository":
		return VSCodePostureReason(VSCodeMissingRepository, "", text, nil), true
	case text == "VS Code Marketplace posture is allowed":
		return VSCodePostureReason(VSCodeMarketplaceAllowed, "", text, nil), true
	case strings.HasPrefix(text, "OSV advisory match for VS Code extension version:"):
		return VSCodePostureReason(VSCodeAdvisoryMatch, "", text, map[string]string{"advisory_ids": strings.TrimSpace(strings.TrimPrefix(text, "OSV advisory match for VS Code extension version:"))}), true
	}
	if age, minAge, ok := parseAgeReason(text, "Marketplace extension is newly published: age ", " days, minimum ", " days"); ok {
		return VSCodePostureReason(VSCodeExtensionTooNew, "", text, ageArgs(age, minAge)), true
	}
	if age, minAge, ok := parseAgeReason(text, "Marketplace extension update is too new: age ", " days, minimum ", " days"); ok {
		return VSCodePostureReason(VSCodeExtensionTooNew, "", text, ageArgs(age, minAge)), true
	}
	if count, minCount, ok := parseFloatReason(text, "Marketplace install count is below threshold: ", " installs, minimum "); ok {
		return VSCodePostureReason(VSCodeLowInstallCount, "", text, map[string]string{"install_count": formatFloatReasonArg(count), "min_install_count": formatFloatReasonArg(minCount)}), true
	}
	if rating, minRating, ok := parseFloatReason(text, "Marketplace average rating is below threshold: ", ", minimum "); ok {
		return VSCodePostureReason(VSCodeLowRating, "", text, map[string]string{"average_rating": formatFloatReasonArg(rating), "min_average_rating": formatFloatReasonArg(minRating)}), true
	}
	return Reason{}, false
}

func parseFloatReason(text string, prefix string, middle string) (float64, float64, bool) {
	value, ok := strings.CutPrefix(text, prefix)
	if !ok {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, middle)
	if !ok {
		return 0, 0, false
	}
	first, err := strconv.ParseFloat(strings.TrimSpace(left), 64)
	if err != nil {
		return 0, 0, false
	}
	second, err := strconv.ParseFloat(strings.TrimSpace(right), 64)
	if err != nil {
		return 0, 0, false
	}
	return first, second, true
}

func formatFloatReasonArg(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".")
}

func inferScannerFindingReason(text string) (Reason, bool) {
	switch text {
	case "gitleaks reported possible secret", "gitleaks reported possible secrets":
		return ScannerFindingReason(ScannerSecret, "gitleaks", text), true
	case "trivy reported possible secret":
		return ScannerFindingReason(ScannerSecret, "trivy", text), true
	case "zizmor reported workflow security finding", "zizmor reported workflow security findings":
		return ScannerFindingReason(ScannerWorkflow, "zizmor", text), true
	case "trivy reported misconfiguration":
		return ScannerFindingReason(ScannerMisconfiguration, "trivy", text), true
	default:
		return Reason{}, false
	}
}

func inferNativeAuditVulnerabilityReason(text string) (Reason, bool) {
	for _, prefix := range []string{
		"npm native audit",
		"npm lockfile audit",
		"pnpm lockfile audit",
		"bun lockfile audit",
		"cargo audit",
		"pip-audit",
		"govulncheck",
		"composer audit",
		"bundle-audit",
		"dotnet package list",
	} {
		if text == prefix+" reported vulnerabilities" {
			return NativeAuditVulnerabilityReason(prefix, "", "", text), true
		}
	}
	return Reason{}, false
}

func inferRegistryPostureReason(text string) (Reason, bool) {
	switch {
	case strings.HasPrefix(text, "npm registry metadata unavailable:"):
		return RegistryPostureReason(RegistryMetadataUnavailable, "npm", "", "", text), true
	case strings.HasPrefix(text, "crates.io metadata unavailable:"):
		return RegistryPostureReason(RegistryMetadataUnavailable, "crates.io", "", "", text), true
	case strings.HasPrefix(text, "PyPI metadata unavailable:"):
		return RegistryPostureReason(RegistryMetadataUnavailable, "PyPI", "", "", text), true
	case text == "installed npm version is not present in registry metadata":
		return RegistryPostureReason(RegistryVersionMissing, "npm", "", "", text), true
	case text == "installed crate version is not present in crates.io metadata":
		return RegistryPostureReason(RegistryVersionMissing, "crates.io", "", "", text), true
	case text == "installed PyPI version is not present in metadata":
		return RegistryPostureReason(RegistryVersionMissing, "PyPI", "", "", text), true
	case strings.HasPrefix(text, "npm package version is deprecated"):
		return RegistryPostureReason(RegistryVersionDeprecated, "npm", "", "", text), true
	case text == "npm package has no maintainers in registry metadata":
		return RegistryPostureReason(RegistryNoMaintainers, "npm", "", "", text), true
	case text == "npm package does not expose a source repository URL":
		return RegistryPostureReason(RegistryMissingRepository, "npm", "", "", text), true
	case text == "crate does not expose a source repository URL":
		return RegistryPostureReason(RegistryMissingRepository, "crates.io", "", "", text), true
	case text == "PyPI package does not expose a source repository URL":
		return RegistryPostureReason(RegistryMissingRepository, "PyPI", "", "", text), true
	case text == "installed crate version is yanked":
		return RegistryPostureReason(RegistryVersionYanked, "crates.io", "", "", text), true
	case strings.HasPrefix(text, "installed PyPI version is yanked"):
		return RegistryPostureReason(RegistryVersionYanked, "PyPI", "", "", text), true
	default:
		return Reason{}, false
	}
}

func localizeMiseMinimumAgeHeld(text string) string {
	switch text {
	case "mise minimum_release_age held candidate before it appeared in normal outdated output":
		return "mise minimum_release_age により、この候補は通常の更新候補に出る前に保留されています"
	case "mise minimum_release_age held this candidate":
		return "mise minimum_release_age により、この候補は保留されています"
	default:
		return text
	}
}
