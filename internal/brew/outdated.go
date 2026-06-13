package brew

import (
	"encoding/json"
	"fmt"
	"strings"
)

type OutdatedReport struct {
	Formulae []OutdatedItem `json:"formulae"`
	Casks    []OutdatedItem `json:"casks"`
}

type OutdatedItem struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
}

type flexibleStringList []string

func (values *flexibleStringList) UnmarshalJSON(data []byte) error {
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*values = many
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		if one == "" {
			*values = nil
		} else {
			*values = []string{one}
		}
		return nil
	}
	return fmt.Errorf("expected string or []string")
}

func (item *OutdatedItem) UnmarshalJSON(data []byte) error {
	type rawItem OutdatedItem
	var raw struct {
		rawItem
		InstalledVersions flexibleStringList `json:"installed_versions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*item = OutdatedItem(raw.rawItem)
	item.InstalledVersions = []string(raw.InstalledVersions)
	return nil
}

func ParseOutdatedReport(raw string) (OutdatedReport, error) {
	var report OutdatedReport
	payload, err := OutdatedJSONPayload(raw)
	if err != nil {
		return report, err
	}
	if payload == "" {
		return report, nil
	}
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		return report, fmt.Errorf("brew outdated --json=v2 --greedy returned invalid JSON: %w", err)
	}
	return report, nil
}

func OutdatedJSONPayload(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("brew outdated --json=v2 --greedy returned no JSON object")
	}
	return raw[start : end+1], nil
}
