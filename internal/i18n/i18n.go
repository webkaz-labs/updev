package i18n

import (
	"regexp"
	"strings"
)

const (
	LangEnglish  = "en"
	LangJapanese = "ja"
)

func IsJapanese(lang string) bool {
	return Normalize(lang) == LangJapanese
}

func Pick(lang string, en string, ja string) string {
	if IsJapanese(lang) && ja != "" {
		return ja
	}
	return en
}

func Normalize(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch {
	case normalized == "ja", strings.HasPrefix(normalized, "ja-"), normalized == "japanese", normalized == "日本語":
		return LangJapanese
	default:
		return LangEnglish
	}
}

func LanguageFromAppleLanguages(output string) string {
	matches := regexp.MustCompile(`"([^"]+)"|([A-Za-z][A-Za-z0-9_-]*)`).FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		if lang := LanguageFromIdentifier(value); lang != "" {
			return lang
		}
	}
	return ""
}

func LanguageFromIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "C" || value == "POSIX" {
		return ""
	}
	return Normalize(value)
}
