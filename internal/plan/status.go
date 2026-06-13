package plan

import "strings"

func ProviderStatus(provider ProviderSummary) Status {
	switch {
	case provider.Unavailable:
		return StatusUnavailable
	case provider.Error != "":
		return StatusError
	case provider.Missing > 0 || provider.Extra > 0:
		return StatusDrift
	default:
		return StatusOK
	}
}

func StatusMatches(status Status, filter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filter))
	switch normalized {
	case "attention", "problem", "problems":
		return IsAttentionStatus(status)
	case "changed", "changes", "drift":
		return status == StatusMissing || status == StatusExtra || status == StatusDrift
	default:
		return strings.EqualFold(string(status), normalized)
	}
}

func ItemMatchesQuery(item Item, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{item.Name, item.Kind, item.Category, item.Version, item.Detail}, " "))
	return strings.Contains(haystack, needle)
}

func IsAttentionStatus(status Status) bool {
	for _, candidate := range AttentionStatusOrder() {
		if status == candidate {
			return true
		}
	}
	return false
}

func AttentionStatusOrder() []Status {
	return []Status{
		StatusError,
		StatusBlocked,
		StatusHeld,
		StatusMissing,
		StatusExtra,
		StatusDrift,
		StatusUnavailable,
	}
}
