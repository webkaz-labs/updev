package i18n

import (
	"fmt"
	"strings"
)

type prefixTranslation struct {
	Prefix string
	JA     string
}

// LocalizedSecurityReason localizes generated security reason text for human
// output. JSON reports keep the original English strings.
func LocalizedSecurityReason(lang string, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || !IsJapanese(lang) {
		return reason
	}
	var ageDays int
	var minDays int
	for _, pattern := range securityAgeReasonPatternsJA {
		if _, err := fmt.Sscanf(reason, pattern.En, &ageDays, &minDays); err == nil {
			return fmt.Sprintf(pattern.JA, ageDays, minDays)
		}
	}
	var left float64
	var right float64
	for _, pattern := range securityFloatReasonPatternsJA {
		if _, err := fmt.Sscanf(reason, pattern.En, &left, &right); err == nil {
			return fmt.Sprintf(pattern.JA, left, right)
		}
	}
	if value, ok := securityReasonJA[reason]; ok {
		return value
	}
	for _, item := range securityReasonPrefixesJA {
		if strings.HasPrefix(reason, item.Prefix) {
			return item.JA + " " + strings.TrimSpace(strings.TrimPrefix(reason, item.Prefix))
		}
	}
	return reason
}

// LocalizedSecurityRemediation localizes generated security remediation text
// for human output. JSON reports keep the original English strings.
func LocalizedSecurityRemediation(lang string, remediation string) string {
	remediation = strings.TrimSpace(remediation)
	if remediation == "" || !IsJapanese(lang) {
		return remediation
	}
	for _, item := range securityRemediationPrefixesJA {
		if strings.HasPrefix(remediation, item.Prefix) {
			return item.JA + " " + strings.TrimSpace(strings.TrimPrefix(remediation, item.Prefix))
		}
	}
	if strings.HasPrefix(remediation, "update ") && strings.Contains(remediation, "; then rerun ") {
		return "脆弱性のない version へ更新し、scanner を再実行してください: " + remediation
	}
	if value, ok := securityRemediationJA[remediation]; ok {
		return value
	}
	return remediation
}

var securityAgeReasonPatternsJA = []struct {
	En string
	JA string
}{
	{"mise candidate release is too new: age %d days, minimum %d days", "mise 候補リリースが新しすぎます: 経過 %d日、最小 %d日"},
	{"candidate release is too new: age %d days, minimum %d days", "候補リリースが新しすぎます: 経過 %d日、最小 %d日"},
	{"tap repository is newly created: age %d days, minimum %d days", "tap repository が新しすぎます: 経過 %d日、最小 %d日"},
	{"Marketplace extension is newly published: age %d days, minimum %d days", "Marketplace extension が公開直後です: 経過 %d日、最小 %d日"},
	{"Marketplace extension update is too new: age %d days, minimum %d days", "Marketplace extension 更新が新しすぎます: 経過 %d日、最小 %d日"},
}

var securityFloatReasonPatternsJA = []struct {
	En string
	JA string
}{
	{"Marketplace install count is below threshold: %f installs, minimum %f", "Marketplace install 数が閾値未満です: %.0f installs、最小 %.0f"},
	{"Marketplace average rating is below threshold: %f, minimum %f", "Marketplace average rating が閾値未満です: %.1f、最小 %.1f"},
}

var securityReasonPrefixesJA = []prefixTranslation{
	{"GitHub release date unavailable before update:", "更新前に GitHub リリース日時を確認できません:"},
	{"GitHub release/tag date unavailable before mise core update", "mise core 更新前に GitHub release/tag 日時を確認できません"},
	{"GitHub release/tag date unavailable before mise update", "mise 更新前に GitHub release/tag 日時を確認できません"},
	{"Homebrew metadata unavailable:", "Homebrew metadata を確認できません:"},
	{"VS Code Marketplace metadata unavailable:", "VS Code Marketplace metadata を確認できません:"},
	{"repository metadata unavailable:", "repository metadata を確認できません:"},
	{"npm registry metadata unavailable:", "npm registry metadata を確認できません:"},
	{"crates.io metadata unavailable:", "crates.io metadata を確認できません:"},
	{"PyPI metadata unavailable:", "PyPI metadata を確認できません:"},
	{"npm package version is deprecated:", "npm package version は deprecated です:"},
	{"OSV advisory match for VS Code extension version:", "VS Code extension version に OSV advisory が一致しました:"},
	{"GitHub Advisory vulnerability match:", "GitHub Advisory vulnerability が一致しました:"},
	{"GitHub Advisory malware match:", "GitHub Advisory malware が一致しました:"},
	{"osv-scanner reported vulnerability in", "OSV-Scanner が vulnerability を報告しました:"},
	{"trivy reported vulnerability in", "Trivy が vulnerability を報告しました:"},
	{"grype reported vulnerability in", "Grype が vulnerability を報告しました:"},
}

var securityReasonJA = map[string]string{
	"release-age and provenance evidence are not available in the first Go safety slice":                 "現在の Go safety 判定では、リリース経過日数と配布元の根拠を確認できません",
	"URL-based Homebrew cask needs manual provenance review before update":                               "URL 指定の Homebrew cask は、更新前に配布元の手動確認が必要です",
	"non-official Homebrew tap needs provenance review before update":                                    "非公式 Homebrew tap は、更新前に配布元の確認が必要です",
	"Homebrew cask updates need provenance and URL/release-age checks before strict mode can allow them": "Homebrew cask 更新は、strict mode で許可する前に配布元・URL・リリース経過日数の確認が必要です",
	"Homebrew cask download host differs from homepage host; vendor provenance review required":          "Homebrew cask のダウンロード元 host が homepage と異なるため、配布元の確認が必要です",
	"non-official Homebrew tap needs provenance review":                                                  "非公式 Homebrew tap は配布元の確認が必要です",
	"URL-based Homebrew cask needs manual provenance review":                                             "URL 指定の Homebrew cask は配布元の手動確認が必要です",
	"Homebrew metadata marks this entry disabled/deprecated":                                             "Homebrew metadata では、この entry が disabled/deprecated とされています",
	"repository is disabled":                                                               "repository が disabled です",
	"repository is archived":                                                               "repository が archived です",
	"repository is private":                                                                "repository が private です",
	"GitHub Advisory vulnerability match":                                                  "GitHub Advisory vulnerability が一致しました",
	"GitHub Advisory malware match":                                                        "GitHub Advisory malware が一致しました",
	"Dependabot security updates are disabled":                                             "Dependabot security updates が無効です",
	"secret scanning is disabled":                                                          "secret scanning が無効です",
	"secret scanning push protection is disabled":                                          "secret scanning push protection が無効です",
	"Marketplace metadata does not mark this extension public":                             "Marketplace metadata では、この extension が public と確認できません",
	"Marketplace metadata does not mark this extension validated":                          "Marketplace metadata では、この extension が validated と確認できません",
	"publisher domain is not verified in Marketplace metadata":                             "Marketplace metadata で publisher domain が verified ではありません",
	"extension executes code but Marketplace metadata does not expose a source repository": "extension は code を実行しますが、Marketplace metadata に source repository がありません",
	"Marketplace metadata does not expose a source repository":                             "Marketplace metadata に source repository がありません",
	"Marketplace extension age is unavailable":                                             "Marketplace extension の公開経過日数を確認できません",
	"Marketplace extension update age is unavailable":                                      "Marketplace extension 更新の経過日数を確認できません",
	"GitHub release/tag date unavailable before mise core update":                          "mise core 更新前に GitHub release/tag 日時を確認できません",
	"GitHub release/tag date unavailable before mise update":                               "mise 更新前に GitHub release/tag 日時を確認できません",
	"installed npm version is not present in registry metadata":                            "インストール済み npm version が registry metadata に存在しません",
	"npm package has no maintainers in registry metadata":                                  "npm package の maintainers が registry metadata にありません",
	"npm package does not expose a source repository URL":                                  "npm package に source repository URL がありません",
	"installed crate version is not present in crates.io metadata":                         "インストール済み crate version が crates.io metadata に存在しません",
	"installed crate version is yanked":                                                    "インストール済み crate version は yanked です",
	"crate does not expose a source repository URL":                                        "crate に source repository URL がありません",
	"installed PyPI version is not present in metadata":                                    "インストール済み PyPI version が metadata に存在しません",
	"installed PyPI version is yanked":                                                     "インストール済み PyPI version は yanked です",
	"PyPI package does not expose a source repository URL":                                 "PyPI package に source repository URL がありません",
	"npm native audit failed":                                                              "npm native audit が失敗しました",
	"npm lockfile audit unavailable":                                                       "npm lockfile audit を利用できません",
	"pnpm lockfile audit unavailable":                                                      "pnpm lockfile audit を利用できません",
	"bun lockfile audit unavailable":                                                       "bun lockfile audit を利用できません",
	"cargo audit unavailable":                                                              "cargo audit を利用できません",
	"Cargo project audit unavailable":                                                      "Cargo project audit を利用できません",
	"pip-audit unavailable":                                                                "pip-audit を利用できません",
	"Go project vulnerability audit unavailable":                                           "Go project vulnerability audit を利用できません",
	"Composer project audit unavailable":                                                   "Composer project audit を利用できません",
	"Bundler project audit unavailable":                                                    "Bundler project audit を利用できません",
	".NET project audit unavailable":                                                       ".NET project audit を利用できません",
	"Maven project audit unavailable":                                                      "Maven project audit を利用できません",
	"npm native audit reported vulnerabilities":                                            "npm audit が vulnerability を報告しました",
	"npm lockfile audit reported vulnerabilities":                                          "npm audit が vulnerability を報告しました",
	"pnpm lockfile audit reported vulnerabilities":                                         "pnpm audit が vulnerability を報告しました",
	"bun lockfile audit reported vulnerabilities":                                          "bun audit が vulnerability を報告しました",
	"cargo audit reported vulnerabilities":                                                 "cargo audit が vulnerability を報告しました",
	"pip-audit reported vulnerabilities":                                                   "pip-audit が vulnerability を報告しました",
	"govulncheck reported vulnerabilities":                                                 "govulncheck が vulnerability を報告しました",
	"composer audit reported vulnerabilities":                                              "composer audit が vulnerability を報告しました",
	"bundle-audit reported vulnerabilities":                                                "bundle-audit が vulnerability を報告しました",
	"dotnet package list reported vulnerabilities":                                         "dotnet package list が vulnerability を報告しました",
	"osv-scanner reported vulnerabilities":                                                 "OSV-Scanner が vulnerability を報告しました",
	"osv-scanner reported vulnerability":                                                   "OSV-Scanner が vulnerability を報告しました",
	"gitleaks reported possible secrets":                                                   "gitleaks が secret の可能性を報告しました",
	"gitleaks reported possible secret":                                                    "gitleaks が secret の可能性を報告しました",
	"zizmor reported workflow security findings":                                           "zizmor が workflow security finding を報告しました",
	"zizmor reported workflow security finding":                                            "zizmor が workflow security finding を報告しました",
	"trivy reported vulnerabilities":                                                       "Trivy が vulnerability を報告しました",
	"trivy reported security findings":                                                     "Trivy が vulnerability を報告しました",
	"trivy reported vulnerability":                                                         "Trivy が vulnerability を報告しました",
	"trivy reported misconfiguration":                                                      "Trivy が misconfiguration を報告しました",
	"trivy reported possible secret":                                                       "Trivy が secret の可能性を報告しました",
	"grype reported vulnerabilities":                                                       "Grype が vulnerability を報告しました",
	"scanner findings blocked by security policy":                                          "scanner finding は security policy により block されています",
	"scanner findings held by security policy":                                             "scanner finding は security policy により hold されています",
	"scanner findings require review by security policy":                                   "scanner finding は security policy により review が必要です",
	"scanner findings allowed by security policy":                                          "scanner finding は security policy により allow されています",
	"known exploited vulnerability":                                                        "既知の悪用済み vulnerability です",
	"OSV vulnerability match; on-PATH binary exposure":                                     "OSV vulnerability が一致し、on-PATH binary として露出しています",
	"OSV vulnerability match":                                                              "OSV vulnerability が一致しました",
	"missing version for high-confidence ecosystem":                                        "高信頼 ecosystem 判定に必要な version がありません",
	"unsupported mise backend ecosystem":                                                   "この mise backend ecosystem は自動照合に未対応です",
	"mise pinned-version bump candidate passed release-age and provenance checks":          "mise の固定バージョン更新候補はリリース経過日数と配布元確認を通過しました",
	"mise minimum_release_age held candidate before it appeared in normal outdated output": "mise minimum_release_age により、この候補は通常の更新候補に出る前に保留されています",
	"no direct OSV ecosystem mapping":                                                      "OSV ecosystem への直接 mapping がありません",
	"vscode extensions require marketplace advisory mapping":                               "VS Code extension は Marketplace advisory mapping が必要です",
	"homebrew requires curated advisory mapping":                                           "Homebrew は curated advisory mapping が必要です",
	"unsupported provider":                                                                 "この provider は自動照合に未対応です",
}

var securityRemediationPrefixesJA = []prefixTranslation{
	{"upgrade to a Homebrew version that includes upstream fixed version:", "上流の修正版を含む Homebrew バージョンへ更新してください。難しい場合は、確認後に理由と期限付きの一時 policy override を追加します:"},
	{"upgrade the VS Code extension to a fixed Marketplace version:", "VS Code extension を修正版の Marketplace version へ更新してください。難しい場合は、確認後に理由と期限付きの一時 policy override を追加します:"},
	{"upgrade to a fixed version:", "修正版へ更新してください:"},
}

var securityRemediationJA = map[string]string{
	"wait until the release reaches the minimum age or allow temporarily by policy after review":                                               "リリースが最小経過日数に達するまで待つか、確認後に policy で一時的に許可してください",
	"wait until mise minimum_release_age allows this candidate, or add a temporary policy allow after review":                                  "mise minimum_release_age がこの候補を許可するまで待つか、確認後に policy で一時的に許可してください",
	"retry when GitHub metadata is reachable or review the upstream release manually before allowing":                                          "GitHub metadata に到達できるようになったら再試行するか、許可前に上流リリースを手動確認してください",
	"retry when GitHub metadata is reachable or review the core runtime release manually before allowing":                                      "GitHub metadata に到達できるようになったら再試行するか、許可前に core runtime release を手動確認してください",
	"review the upstream release manually; retry when GitHub release metadata is available or allow by policy":                                 "上流リリースを手動確認してください。GitHub metadata が取得できるようになったら再試行するか、policy で許可してください",
	"review the cask source URL and add a temporary allow policy with reason and expiry if accepted":                                           "cask の source URL を確認し、許容する場合は理由と期限付きの一時 allow policy を追加してください",
	"review the tap repository and add a temporary allow policy with reason and expiry if accepted":                                            "tap repository を確認し、許容する場合は理由と期限付きの一時 allow policy を追加してください",
	"review vendor provenance and add a temporary allow policy with reason and expiry if accepted":                                             "vendor の配布元を確認し、許容する場合は理由と期限付きの一時 allow policy を追加してください",
	"retry after Homebrew metadata is reachable; otherwise review manually or allow by policy with reason and expiry":                          "Homebrew metadata に到達できるようになってから再試行してください。難しい場合は手動確認するか、理由と期限付きの policy で許可してください",
	"review the OSV advisory and wait for a fixed Homebrew version, or add a temporary policy override with reason and expiry after review":    "OSV advisory を確認して修正版の Homebrew version を待つか、確認後に理由と期限付きの一時 policy override を追加してください",
	"review the OSV advisory and wait for a fixed Marketplace version, or add a temporary policy override with reason and expiry after review": "OSV advisory を確認して修正版の Marketplace version を待つか、確認後に理由と期限付きの一時 policy override を追加してください",
	"review the advisory and wait for a fixed version or replacement":                                                                          "advisory を確認し、修正版または代替に置き換わるまで待ってください",
	"remove or disable the on-PATH binary until fixed":                                                                                         "修正版へ更新できるまで、on-PATH binary を削除または無効化してください",
	"enable Dependabot security updates or add a temporary policy override with reason and expiry":                                             "Dependabot security updates を有効化するか、理由と期限付きの一時 policy override を追加してください",
	"enable secret scanning or add a temporary policy override with reason and expiry":                                                         "secret scanning を有効化するか、理由と期限付きの一時 policy override を追加してください",
	"enable secret scanning push protection or add a temporary policy override with reason and expiry":                                         "secret scanning push protection を有効化するか、理由と期限付きの一時 policy override を追加してください",
	"enable Dependabot security updates on the source repository or account for missing upstream security automation":                          "source repository の Dependabot security updates を有効化するか、上流の security automation 不足を考慮して判断してください",
	"enable secret scanning on the source repository or account for missing upstream secret detection":                                         "source repository の secret scanning を有効化するか、上流の secret detection 不足を考慮して判断してください",
	"enable secret scanning push protection on the source repository or account for missing push-time secret blocking":                         "source repository の secret scanning push protection を有効化するか、push 時の secret blocking 不足を考慮して判断してください",
	"replace the disabled repository source or add a temporary policy override after review":                                                   "disabled repository source を置き換えるか、確認後に一時 policy override を追加してください",
	"replace the archived repository source or add a temporary policy override after review":                                                   "archived repository source を置き換えるか、確認後に一時 policy override を追加してください",
	"review private repository access and add a temporary policy override only if the source is trusted":                                       "private repository の access と信頼性を確認し、必要な場合だけ一時 policy override を追加してください",
	"verify private repository access and provenance before keeping this dependency":                                                           "この dependency を維持する前に private repository の access と provenance を確認してください",
	"wait until the repository reaches the minimum age or add a temporary policy override after review":                                        "repository が最小経過日数に達するまで待つか、確認後に一時 policy override を追加してください",
	"review repository posture and add a temporary policy override only with reason and expiry":                                                "repository posture を確認し、許容する場合だけ理由と期限付きの一時 policy override を追加してください",
	"review the newly created tap repository and add a temporary policy override only with reason and expiry":                                  "作成直後の tap repository を確認し、許容する場合だけ理由と期限付きの一時 policy override を追加してください",
	"review Marketplace visibility and source provenance before keeping this extension":                                                        "この extension を維持する前に Marketplace visibility と source provenance を確認してください",
	"review Marketplace validation status and source provenance before keeping this extension":                                                 "この extension を維持する前に Marketplace validation status と source provenance を確認してください",
	"verify publisher identity and source repository before adding a temporary policy override":                                                "一時 policy override を追加する前に publisher identity と source repository を確認してください",
	"require a trusted source repository or replace the extension before allowing code execution":                                              "code execution を許可する前に、信頼できる source repository を確認するか extension を置き換えてください",
	"review extension provenance manually before adding a temporary policy override":                                                           "一時 policy override を追加する前に extension provenance を手動確認してください",
	"wait until the Marketplace extension reaches the minimum age or review publisher/source provenance before allowing":                       "Marketplace extension が最小経過日数に達するまで待つか、許可前に publisher/source provenance を確認してください",
	"review publisher and repository provenance before accepting a low-install extension":                                                      "install 数が少ない extension を受け入れる前に publisher と repository provenance を確認してください",
	"review extension quality signals and source provenance before accepting a low-rated extension":                                            "rating が低い extension を受け入れる前に quality signal と source provenance を確認してください",
	"retry when Marketplace metadata is reachable or review the extension manually before adding a policy override":                            "Marketplace metadata に到達できるようになってから再試行してください。難しい場合は policy override 前に extension を手動確認してください",
	"review VS Code Marketplace publisher/source metadata and add a temporary allow policy with reason and expiry if accepted":                 "VS Code Marketplace の publisher/source metadata を確認し、許容する場合は理由と期限付きの一時 allow policy を追加してください",
	"update to a maintained npm version or replace the package":                                                                                "maintained な npm version へ更新するか package を置き換えてください",
	"replace the deprecated npm package version or update to a non-deprecated version after review":                                            "deprecated な npm package version を置き換えるか、確認後に non-deprecated version へ更新してください",
	"verify the installed version and update to a registry version before allowing it":                                                         "許可する前にインストール済み version を確認し、registry version へ更新してください",
	"review package ownership and source provenance before keeping this package":                                                               "この package を維持する前に ownership と source provenance を確認してください",
	"require a source repository or replace the npm package":                                                                                   "source repository を確認するか npm package を置き換えてください",
	"review package provenance manually before adding a temporary policy override":                                                             "一時 policy override を追加する前に package provenance を手動確認してください",
	"retry after npm registry metadata is reachable; otherwise review manually or allow by policy with reason and expiry":                      "npm registry metadata に到達できるようになってから再試行してください。難しい場合は手動確認するか、理由と期限付きの policy で許可してください",
	"verify the installed crate version and update to a crates.io version before allowing it":                                                  "許可する前にインストール済み crate version を確認し、crates.io version へ更新してください",
	"update to a non-yanked crate version or replace the crate":                                                                                "non-yanked crate version へ更新するか crate を置き換えてください",
	"require a source repository or replace the crate":                                                                                         "source repository を確認するか crate を置き換えてください",
	"review crate provenance manually before adding a temporary policy override":                                                               "一時 policy override を追加する前に crate provenance を手動確認してください",
	"retry after crates.io metadata is reachable; otherwise review manually or allow by policy with reason and expiry":                         "crates.io metadata に到達できるようになってから再試行してください。難しい場合は手動確認するか、理由と期限付きの policy で許可してください",
	"verify the installed Python package version and update to a PyPI release before allowing it":                                              "許可する前にインストール済み Python package version を確認し、PyPI release へ更新してください",
	"update to a non-yanked PyPI release or replace the package":                                                                               "non-yanked PyPI release へ更新するか package を置き換えてください",
	"require a source repository or replace the PyPI package":                                                                                  "source repository を確認するか PyPI package を置き換えてください",
	"retry after PyPI metadata is reachable; otherwise review manually or allow by policy with reason and expiry":                              "PyPI metadata に到達できるようになってから再試行してください。難しい場合は手動確認するか、理由と期限付きの policy で許可してください",
	"revoke or rotate the suspected secret, inspect the finding, and add a scanner policy override only for a confirmed false positive":        "疑わしい secret を revoke/rotate し、finding を確認してください。false positive と確認できる場合だけ scanner policy override を追加します",
	"revoke or rotate the secret if real, remove it from source/history, then rerun gitleaks":                                                  "実際の secret であれば revoke/rotate し、source/history から削除してから gitleaks を再実行してください",
	"revoke or rotate the secret if real, remove it from source/history, then rerun trivy":                                                     "実際の secret であれば revoke/rotate し、source/history から削除してから Trivy を再実行してください",
	"review the workflow finding and harden the GitHub Actions workflow before adding a policy override":                                       "workflow finding を確認し、policy override 前に GitHub Actions workflow を hardening してください",
	"update the GitHub Actions workflow according to the zizmor finding and rerun zizmor":                                                      "zizmor finding に従って GitHub Actions workflow を更新し、zizmor を再実行してください",
	"review the scanner finding and update dependencies, configuration, or secrets before adding a policy override":                            "scanner finding を確認し、policy override 前に dependency/configuration/secret を更新してください",
	"review the configuration finding and rerun trivy":                                                                                         "configuration finding を確認し、Trivy を再実行してください",
}
