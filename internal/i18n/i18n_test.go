package i18n

import "testing"

func TestNormalizeJapaneseLocales(t *testing.T) {
	for _, value := range []string{"ja", "ja_JP.UTF-8", "ja-JP", "Japanese", "日本語"} {
		if got := Normalize(value); got != LangJapanese {
			t.Fatalf("Normalize(%q) = %q, want ja", value, got)
		}
	}
}

func TestLanguageFromAppleLanguages(t *testing.T) {
	if got := LanguageFromAppleLanguages(`("ja-JP", "en-US")`); got != LangJapanese {
		t.Fatalf("expected Japanese from AppleLanguages, got %q", got)
	}
}

func TestLocalizedSecurityReason(t *testing.T) {
	got := LocalizedSecurityReason(LangJapanese, "candidate release is too new: age 0 days, minimum 3 days")
	if got != "候補リリースが新しすぎます: 経過 0日、最小 3日" {
		t.Fatalf("unexpected localized release-age reason: %q", got)
	}
	got = LocalizedSecurityReason(LangJapanese, "repository is archived")
	if got != "repository が archived です" {
		t.Fatalf("unexpected localized posture reason: %q", got)
	}
	got = LocalizedSecurityReason(LangEnglish, "repository is archived")
	if got != "repository is archived" {
		t.Fatalf("expected English reason to remain stable, got %q", got)
	}
}

func TestLocalizedSecurityRemediation(t *testing.T) {
	got := LocalizedSecurityRemediation(LangJapanese, "replace the archived repository source or add a temporary policy override after review")
	if got != "archived repository source を置き換えるか、確認後に一時 policy override を追加してください" {
		t.Fatalf("unexpected localized remediation: %q", got)
	}
	got = LocalizedSecurityRemediation(LangJapanese, "update left-pad in package-lock.json to fixed version: 1.2.3; then rerun osv-scanner")
	if got == "" || got == "update left-pad in package-lock.json to fixed version: 1.2.3; then rerun osv-scanner" {
		t.Fatalf("expected scanner remediation to gain Japanese guidance, got %q", got)
	}
}
