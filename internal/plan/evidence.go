package plan

import (
	"path/filepath"
	"strings"
)

type EvidenceIndex struct {
	Updates  map[string][]string
	Security map[string][]string
	Backends map[string][]string
}

type ItemEvidence struct {
	Updates  []string
	Security []string
	Backends []string
}

func NewEvidenceIndex() EvidenceIndex {
	return EvidenceIndex{
		Updates:  map[string][]string{},
		Security: map[string][]string{},
		Backends: map[string][]string{},
	}
}

func AddEvidence(index map[string][]string, key string, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	for _, existing := range index[key] {
		if existing == value {
			return
		}
	}
	index[key] = append(index[key], value)
}

func ItemEvidenceFor(item Item, index EvidenceIndex) ItemEvidence {
	keys := []string{
		EvidenceExactKey(item.Provider, item.Kind, item.Name),
		EvidenceProviderNameKey(item.Provider, item.Name),
		EvidenceNameKey(item.Name),
	}
	return ItemEvidence{
		Updates:  EvidenceLookup(index.Updates, keys),
		Security: EvidenceLookup(index.Security, keys),
		Backends: EvidenceLookup(index.Backends, keys),
	}
}

func MergeItemEvidence(left ItemEvidence, right ItemEvidence) ItemEvidence {
	return ItemEvidence{
		Updates:  mergeEvidenceStrings(left.Updates, right.Updates),
		Security: mergeEvidenceStrings(left.Security, right.Security),
		Backends: mergeEvidenceStrings(left.Backends, right.Backends),
	}
}

func EvidenceCounts(evidence EvidenceIndex) (int, int, int) {
	return EvidenceValueCount(evidence.Updates), EvidenceValueCount(evidence.Security), EvidenceValueCount(evidence.Backends)
}

func EvidenceValueCount(values map[string][]string) int {
	seen := map[string]bool{}
	for _, entries := range values {
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			seen[entry] = true
		}
	}
	return len(seen)
}

func EvidenceLookup(index map[string][]string, keys []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		for _, value := range index[key] {
			if strings.TrimSpace(value) == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func EvidenceExactKey(provider string, kind string, name string) string {
	provider = EvidenceToken(provider)
	kind = EvidenceToken(kind)
	name = EvidenceToken(name)
	if provider == "" || kind == "" || name == "" {
		return ""
	}
	return provider + "/" + kind + "/" + name
}

func EvidenceProviderNameKey(provider string, name string) string {
	provider = EvidenceToken(provider)
	name = EvidenceToken(name)
	if provider == "" || name == "" {
		return ""
	}
	return provider + "/" + name
}

func EvidenceNameKey(name string) string {
	return EvidenceToken(name)
}

func EvidenceToken(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
}

func EvidenceUpdateItemKeys(provider string, item string, miseBumpProvider string) []string {
	keys := []string{}
	add := func(key string) {
		if strings.TrimSpace(key) == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}
	for _, name := range EvidenceItemNameCandidates(item) {
		add(EvidenceProviderNameKey(provider, name))
		if strings.TrimSpace(provider) == "" {
			add(EvidenceNameKey(name))
		}
		if strings.EqualFold(provider, "brew") {
			add(EvidenceExactKey(provider, "brew", name))
			add(EvidenceExactKey(provider, "cask", name))
			add(EvidenceExactKey(provider, "tap", name))
		}
		if strings.EqualFold(provider, miseBumpProvider) {
			add(EvidenceExactKey("mise", "tool", name))
			add(EvidenceProviderNameKey("mise", name))
			add(EvidenceNameKey(name))
		}
	}
	return keys
}

func EvidenceItemNameCandidates(value string) []string {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" {
		return nil
	}
	fields := strings.Fields(value)
	if left, _, ok := strings.Cut(value, "->"); ok {
		leftFields := strings.Fields(left)
		if len(leftFields) >= 2 && evidenceLeadingTokenIsPrefix(leftFields[0]) {
			value = leftFields[0] + " " + leftFields[1]
		} else if len(leftFields) > 0 && evidenceSafeLeadingToken(leftFields[0]) {
			value = leftFields[0]
		}
		fields = strings.Fields(value)
	}
	candidates := []string{}
	leadingTokenIsPrefix := false
	if len(fields) >= 2 {
		if evidenceLeadingTokenIsPrefix(fields[0]) {
			leadingTokenIsPrefix = true
			candidates = append(candidates, fields[1])
		}
	}
	if !leadingTokenIsPrefix {
		candidates = append(candidates, value)
		if len(fields) > 0 && evidenceSafeLeadingToken(fields[0]) {
			candidates = append(candidates, fields[0])
		}
	}
	for _, candidate := range append([]string{}, candidates...) {
		if strings.Contains(candidate, "/") {
			candidates = append(candidates, filepath.Base(candidate))
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.Trim(strings.TrimSpace(candidate), `"'():;,.`)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func EvidenceRootsMatch(root string, reportRoot string) bool {
	root = strings.TrimSpace(root)
	reportRoot = strings.TrimSpace(reportRoot)
	if root == "" || reportRoot == "" {
		return true
	}
	return filepath.Clean(root) == filepath.Clean(reportRoot)
}

func mergeEvidenceStrings(left []string, right []string) []string {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	out := append([]string{}, left...)
	seen := map[string]bool{}
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range right {
		if strings.TrimSpace(value) == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func evidenceSafeLeadingToken(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'():;,.`))
	if value == "" {
		return false
	}
	switch value {
	case "security", "held", "deferred", "skipped", "skip", "gate", "policy", "review", "blocked", "updated", "update":
		return false
	default:
		return true
	}
}

func evidenceLeadingTokenIsPrefix(value string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(value), `:;,.`)) {
	case "brew", "formula", "cask", "tap", "tool", "mise":
		return true
	default:
		return false
	}
}
