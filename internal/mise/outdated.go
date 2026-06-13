package mise

import (
	"encoding/json"
	"strings"
)

type OutdatedItem struct {
	Requested string  `json:"requested"`
	Current   string  `json:"current"`
	Latest    string  `json:"latest"`
	Bump      *string `json:"bump"`
}

func ParseOutdatedReport(raw string) (map[string]OutdatedItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var report map[string]OutdatedItem
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, err
	}
	return report, nil
}
