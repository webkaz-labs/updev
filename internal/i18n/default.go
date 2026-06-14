package i18n

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func DefaultLanguage(configured string) string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_LANG")); value != "" {
		return Normalize(value)
	}
	if configured = strings.TrimSpace(configured); configured != "" && configured != "auto" {
		return Normalize(configured)
	}
	return DetectOSLanguage()
}

func DetectOSLanguage() string {
	if runtime.GOOS == "darwin" {
		if lang := LanguageFromAppleLanguages(readGlobalDefault("AppleLanguages")); lang != "" {
			return lang
		}
		if lang := LanguageFromIdentifier(readGlobalDefault("AppleLocale")); lang != "" {
			return lang
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if lang := LanguageFromIdentifier(os.Getenv(name)); lang != "" {
			return lang
		}
	}
	return LangEnglish
}

func readGlobalDefault(key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", key).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
