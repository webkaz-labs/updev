package registryaudit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/webkaz-labs/updev/internal/securityreason"
)

func summarizeErrors(errs []error, limit int) error {
	if len(errs) == 0 {
		return nil
	}
	if limit <= 0 || len(errs) <= limit {
		return errors.Join(errs...)
	}
	parts := make([]string, 0, limit+1)
	for _, err := range errs[:limit] {
		parts = append(parts, err.Error())
	}
	parts = append(parts, fmt.Sprintf("... %d more", len(errs)-limit))
	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}

func truncate(text string, width int) string {
	text = strings.TrimSpace(text)
	if width <= 0 || len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func registryPostureReasonFields(code string, registry string, packageName string, version string, text string) (string, string, map[string]string) {
	reason := securityreason.RegistryPostureReason(code, registry, packageName, version, text)
	return reason.Text, reason.Code, reason.Args
}
