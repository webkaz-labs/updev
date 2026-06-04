package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

type miseMinimumReleaseAgeEvidence struct {
	Status                plan.Status `json:"status"`
	Active                bool        `json:"active"`
	Value                 string      `json:"value,omitempty"`
	Source                string      `json:"source,omitempty"`
	CommandShapeSupported bool        `json:"command_shape_supported"`
	Reason                string      `json:"reason,omitempty"`
	Remediation           string      `json:"remediation,omitempty"`
}

func detectMiseMinimumReleaseAge(ctx context.Context, commandRunner commandRunner, root string) miseMinimumReleaseAgeEvidence {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	evidence := miseMinimumReleaseAgeEvidence{
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
		evidence.Reason = "could not verify mise latest --minimum-release-age support: " + dependencyCommandError(shapeResult)
		evidence.Remediation = "upgrade mise or update updev if the command shape changed"
		return evidence
	}
	evidence.CommandShapeSupported = strings.Contains(shapeResult.Stdout, "--minimum-release-age") || strings.Contains(shapeResult.Stderr, "--minimum-release-age")
	if !evidence.CommandShapeSupported {
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
		evidence.Reason = "could not read mise settings: " + dependencyCommandError(settingsResult)
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
	value, source, ok := miseMinimumReleaseAgeFromSettings(payload)
	if !ok || value == "" {
		return evidence
	}
	evidence.Active = true
	evidence.Value = value
	evidence.Source = source
	evidence.Reason = "mise minimum_release_age is active"
	evidence.Remediation = ""
	return evidence
}

func miseMinimumReleaseAgeFromSettings(payload map[string]any) (string, string, bool) {
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
