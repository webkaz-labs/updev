package securityreason

import "testing"

func TestInferCandidateReleaseTooNew(t *testing.T) {
	reason := Infer("candidate release is too new: age 1 days, minimum 3 days")
	if reason.Code != CandidateReleaseTooNew || reason.Args["age_days"] != "1" || reason.Args["min_age_days"] != "3" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferScannerVulnerability(t *testing.T) {
	reason := Infer("osv-scanner reported vulnerability in a directly managed package")
	if reason.Code != ScannerVulnerability || reason.Args["tool"] != "osv-scanner" || reason.Args["dependency_kind"] != "direct" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferScannerVulnerabilityWithMatchStyle(t *testing.T) {
	reason := Infer("grype reported vulnerability in a transitive dependency via CPE-style match")
	if reason.Code != ScannerVulnerability || reason.Args["tool"] != "grype" || reason.Args["dependency_kind"] != "transitive" || reason.Args["match_style"] != "cpe" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferNativeAuditVulnerability(t *testing.T) {
	reason := Infer("pip-audit reported vulnerabilities")
	if reason.Code != NativeAuditVulnerability || reason.Args["tool"] != "pip-audit" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferRegistryPostureReason(t *testing.T) {
	reason := Infer("installed crate version is yanked")
	if reason.Code != RegistryVersionYanked || reason.Args["registry"] != "crates.io" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferScannerFindingReason(t *testing.T) {
	reason := Infer("trivy reported possible secret")
	if reason.Code != ScannerSecret || reason.Args["tool"] != "trivy" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferGitHubPostureReason(t *testing.T) {
	reason := Infer("tap repository is newly created: age 2 days, minimum 30 days")
	if reason.Code != GitHubRepositoryTooNew || reason.Args["age_days"] != "2" || reason.Args["min_age_days"] != "30" {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestLocalizeJapaneseMiseOpaqueBackend(t *testing.T) {
	got := LocalizeJapanese(MiseOpaqueBackendReason())
	want := "mise のバックエンドから updev がリリース経過日数の根拠を十分に確認できないため、確認が必要です"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseScannerVulnerability(t *testing.T) {
	got := LocalizeJapanese(ScannerVulnerabilityReason("osv-scanner", "transitive"))
	want := "osv-scanner が推移的依存関係の脆弱性を検出しました"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseNativeAuditVulnerability(t *testing.T) {
	got := LocalizeJapanese(NativeAuditVulnerabilityReason("pip-audit", "PyPI", "requirements.txt", "pip-audit reported vulnerabilities"))
	want := "pip-audit が PyPI の native audit で脆弱性を検出しました"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseRegistryPostureReason(t *testing.T) {
	got := LocalizeJapanese(RegistryPostureReason(RegistryVersionYanked, "PyPI", "frogmouth", "0.9.2", "installed PyPI version is yanked"))
	want := "PyPI/frogmouth: インストール済み version が yanked です"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseScannerFindingReason(t *testing.T) {
	got := LocalizeJapanese(ScannerFindingReason(ScannerWorkflow, "zizmor", "zizmor reported workflow security finding"))
	want := "zizmor が workflow security finding を報告しました"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseGitHubPostureReason(t *testing.T) {
	got := LocalizeJapanese(GitHubPostureReason(GitHubRepositoryArchived, "owner/tool", "repository is archived", nil))
	want := "owner/tool: repository が archived です"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLocalizeJapaneseHomebrewPostureReason(t *testing.T) {
	got := LocalizeJapanese(HomebrewPostureReason(HomebrewNonOfficialTap, "cask", "demo", "non-official Homebrew tap needs provenance review", nil))
	want := "cask/demo: 非公式 Homebrew tap は配布元の確認が必要です"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestInferHomebrewEvidenceUnavailable(t *testing.T) {
	reason := Infer("release-age and provenance evidence are not available in the first Go safety slice")
	if reason.Code != HomebrewEvidenceUnavailable {
		t.Fatalf("unexpected reason: %#v", reason)
	}
}

func TestInferHomebrewStructuredProviderReasons(t *testing.T) {
	cases := []struct {
		text string
		code string
		key  string
		want string
	}{
		{"official Homebrew formula metadata is available and not disabled or deprecated", HomebrewOfficialFormula, "kind", "brew"},
		{"GitHub release/tag date unavailable before update: rate limited", HomebrewReleaseUnavailable, "error", "rate limited"},
		{"OSV advisory match for Homebrew source tag: GHSA-demo", HomebrewAdvisoryMatch, "advisory_ids", "GHSA-demo"},
		{"GitHub Advisory match for curated Homebrew mapping: GHSA-demo", HomebrewAdvisoryMatch, "advisory_source", "GitHub Advisory"},
	}
	for _, tc := range cases {
		reason := Infer(tc.text)
		if reason.Code != tc.code || reason.Args[tc.key] != tc.want {
			t.Fatalf("unexpected reason for %q: %#v", tc.text, reason)
		}
	}
}

func TestLocalizeJapaneseVSCodePostureReason(t *testing.T) {
	got := LocalizeJapanese(VSCodePostureReason(VSCodePublisherUnverified, "publisher.extension", "publisher domain is not verified in Marketplace metadata", nil))
	want := "publisher.extension: Marketplace metadata で publisher domain が verified ではありません"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestInferVSCodeStructuredProviderReasons(t *testing.T) {
	cases := []struct {
		text string
		code string
		key  string
		want string
	}{
		{"VS Code Marketplace posture is allowed", VSCodeMarketplaceAllowed, "", ""},
		{"OSV advisory match for VS Code extension version: GHSA-demo", VSCodeAdvisoryMatch, "advisory_ids", "GHSA-demo"},
		{"Marketplace install count is below threshold: 42 installs, minimum 1000", VSCodeLowInstallCount, "install_count", "42"},
		{"Marketplace average rating is below threshold: 1.5, minimum 2.0", VSCodeLowRating, "min_average_rating", "2"},
	}
	for _, tc := range cases {
		reason := Infer(tc.text)
		if reason.Code != tc.code {
			t.Fatalf("unexpected code for %q: %#v", tc.text, reason)
		}
		if tc.key != "" && reason.Args[tc.key] != tc.want {
			t.Fatalf("unexpected args for %q: %#v", tc.text, reason)
		}
	}
}

func TestLocalizeJapaneseSecurityPolicyOverridePreservesReason(t *testing.T) {
	got := LocalizeJapanese(SecurityPolicyOverrideReason("allow", "trusted vendor"))
	want := "security policy により allow されています: trusted vendor"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
