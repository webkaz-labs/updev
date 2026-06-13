package cmd

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func fakeCommandWasCalled(calls [][]string, want []string) bool {
	for _, call := range calls {
		if strings.Join(call, "\x00") == strings.Join(want, "\x00") {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func withDefaultLanguageForTest(t *testing.T, lang string) {
	t.Helper()
	old, hadOld := os.LookupEnv("UPDEV_LANG")
	_ = os.Setenv("UPDEV_LANG", lang)
	defaultLanguageOnce = sync.Once{}
	defaultLanguageValue = ""
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv("UPDEV_LANG", old)
		} else {
			_ = os.Unsetenv("UPDEV_LANG")
		}
		defaultLanguageOnce = sync.Once{}
		defaultLanguageValue = ""
	})
}
