package cmd

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
)

var (
	defaultLanguageOnce  sync.Once
	defaultLanguageValue string
)

func defaultLanguage() string {
	defaultLanguageOnce.Do(func() {
		if value := strings.TrimSpace(os.Getenv("UPDEV_LANG")); value != "" {
			defaultLanguageValue = i18n.Normalize(value)
			return
		}
		if configured := loadUpdevConfig().UI.Language; configured != nil && *configured != "" && *configured != "auto" {
			defaultLanguageValue = i18n.Normalize(*configured)
			return
		}
		defaultLanguageValue = detectOSLanguage()
	})
	return defaultLanguageValue
}

func detectOSLanguage() string {
	if runtime.GOOS == "darwin" {
		if lang := i18n.LanguageFromAppleLanguages(readGlobalDefault("AppleLanguages")); lang != "" {
			return lang
		}
		if lang := i18n.LanguageFromIdentifier(readGlobalDefault("AppleLocale")); lang != "" {
			return lang
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if lang := i18n.LanguageFromIdentifier(os.Getenv(name)); lang != "" {
			return lang
		}
	}
	return i18n.LangEnglish
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

func tr(en string, ja string) string {
	return i18n.Pick(defaultLanguage(), en, ja)
}
