package mise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type MinimumReleaseAgeEvidence struct {
	Status                plan.Status `json:"status"`
	Active                *bool       `json:"active,omitempty"`
	Value                 string      `json:"value,omitempty"`
	Source                string      `json:"source,omitempty"`
	CommandShapeSupported *bool       `json:"command_shape_supported,omitempty"`
	Reason                string      `json:"reason,omitempty"`
	Remediation           string      `json:"remediation,omitempty"`
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) runner.Result
}

func DetectMinimumReleaseAge(ctx context.Context, commandRunner CommandRunner, root string) MinimumReleaseAgeEvidence {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	evidence := MinimumReleaseAgeEvidence{
		Status:      plan.StatusOK,
		Reason:      "mise minimum_release_age is not configured",
		Remediation: "configure mise minimum_release_age if provider-native release-age filtering is desired",
	}

	shapeResult := commandRunner.Run(ctx, "mise", "latest", "--help")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "mise latest --help timed out"
		evidence.Remediation = "upgrade or repair mise before relying on minimum_release_age diagnostics"
		return evidence
	}
	if shapeResult.Err != nil || shapeResult.Code != 0 {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "could not verify mise latest --minimum-release-age support: " + commandError(shapeResult)
		evidence.Remediation = "upgrade mise or update updev if the command shape changed"
		return evidence
	}
	shapeSupported := strings.Contains(shapeResult.Stdout, "--minimum-release-age") || strings.Contains(shapeResult.Stderr, "--minimum-release-age")
	evidence.CommandShapeSupported = BoolPtr(shapeSupported)
	if !shapeSupported {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "mise latest help does not advertise --minimum-release-age"
		evidence.Remediation = "upgrade mise or update updev if the command shape changed"
		return evidence
	}

	settingsArgs := []string{"settings", "ls", "--json-extended"}
	if strings.TrimSpace(root) != "" {
		settingsArgs = append(settingsArgs, "--cd", root)
	}
	settingsResult := commandRunner.Run(ctx, "mise", settingsArgs...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "mise settings ls --json-extended timed out"
		evidence.Remediation = "upgrade or repair mise before relying on minimum_release_age diagnostics"
		return evidence
	}
	if settingsResult.Err != nil || settingsResult.Code != 0 {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "could not read mise settings: " + commandError(settingsResult)
		evidence.Remediation = "upgrade mise or update updev if the settings JSON contract changed"
		return evidence
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(settingsResult.Stdout), &payload); err != nil {
		evidence.Status = plan.StatusDrift
		evidence.Reason = "mise settings output is not valid JSON"
		evidence.Remediation = "upgrade mise or update updev if the settings JSON contract changed"
		return evidence
	}
	value, source, ok := MinimumReleaseAgeFromSettings(payload)
	if !ok || value == "" {
		evidence.Active = BoolPtr(false)
		return evidence
	}
	evidence.Active = BoolPtr(true)
	evidence.Value = value
	evidence.Source = source
	evidence.Reason = "mise minimum_release_age is active"
	evidence.Remediation = ""
	return evidence
}

func BoolPtr(value bool) *bool {
	return &value
}

func BoolValue(value *bool) bool {
	return value != nil && *value
}

func MinimumReleaseAgeFromSettings(payload map[string]any) (string, string, bool) {
	raw, ok := payload["minimum_release_age"]
	if !ok {
		return "", "", false
	}
	setting, ok := raw.(map[string]any)
	if !ok {
		if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" && value != "<nil>" {
			return value, "", true
		}
		return "", "", false
	}
	value := strings.TrimSpace(fmt.Sprint(setting["value"]))
	if value == "<nil>" {
		value = ""
	}
	source := strings.TrimSpace(fmt.Sprint(setting["source"]))
	if source == "<nil>" {
		source = ""
	}
	return value, source, value != ""
}

func commandError(result runner.Result) string {
	reason := strings.TrimSpace(result.Stderr)
	if reason == "" {
		reason = strings.TrimSpace(result.Stdout)
	}
	if reason == "" && errors.Is(result.Err, context.DeadlineExceeded) {
		return "command timed out"
	}
	if reason == "" && result.Err != nil {
		reason = result.Err.Error()
	}
	if reason == "" {
		reason = fmt.Sprintf("command exited with code %d", result.Code)
	}
	return truncate(strings.Join(strings.Fields(reason), " "), 240)
}

func truncate(text string, width int) string {
	if width <= 0 || len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}
