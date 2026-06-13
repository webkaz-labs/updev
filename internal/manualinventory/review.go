package manualinventory

import (
	"fmt"
	"path/filepath"
	"strings"
)

type ReviewCandidate struct {
	Provider          string               `json:"provider"`
	Kind              string               `json:"kind"`
	Name              string               `json:"name"`
	ReasonCode        string               `json:"reason_code"`
	RemediationCode   string               `json:"remediation_code,omitempty"`
	Confidence        string               `json:"confidence,omitempty"`
	Params            map[string]string    `json:"params,omitempty"`
	Evidence          []ReviewEvidence     `json:"evidence,omitempty"`
	SuggestedOverride ReviewOverrideFields `json:"suggested_override"`
}

type ReviewEvidence struct {
	Scanner             string `json:"scanner"`
	Source              string `json:"source,omitempty"`
	Path                string `json:"path,omitempty"`
	ReviewURL           string `json:"review_url,omitempty"`
	SourceURL           string `json:"source_url,omitempty"`
	Owner               string `json:"owner,omitempty"`
	ManagedBy           string `json:"managed_by,omitempty"`
	UpdateOwner         string `json:"update_owner,omitempty"`
	OwnershipConfidence string `json:"ownership_confidence,omitempty"`
	ProviderMetadata    string `json:"provider_metadata,omitempty"`
	MASID               string `json:"mas_id,omitempty"`
	BundleID            string `json:"bundle_id,omitempty"`
	Version             string `json:"version,omitempty"`
}

type ReviewOverrideFields struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases,omitempty"`
	Category  string   `json:"category,omitempty"`
	ManagedBy string   `json:"managed_by,omitempty"`
	Lifecycle string   `json:"lifecycle,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

func ValidateAgentDrafts(content string, candidates []ReviewCandidate, command []string) ([]StructuredApp, error) {
	drafts := ParseStructuredApps(content)
	if len(drafts) == 0 {
		return nil, fmt.Errorf("manual inventory agent output did not contain any [[manual.apps]] draft entries")
	}
	candidateIndex := map[string]ReviewCandidate{}
	for _, candidate := range candidates {
		for _, key := range ReviewCandidateIdentityKeys(candidate) {
			if key != "" {
				candidateIndex[key] = candidate
			}
		}
	}
	out := make([]StructuredApp, 0, len(drafts))
	seen := map[string]bool{}
	for _, draft := range drafts {
		match, ok := AgentDraftMatchesCandidate(draft, candidateIndex)
		if !ok {
			return nil, fmt.Errorf("manual inventory agent draft %q does not match selected candidates", draft.Name)
		}
		draft.ReviewStatus = "draft"
		if draft.Provenance == nil {
			draft.Provenance = map[string]string{}
		}
		draft.Provenance["source"] = "agent"
		if len(command) > 0 && draft.Provenance["command"] == "" {
			draft.Provenance["command"] = filepath.Base(command[0])
		}
		for _, evidence := range match.Evidence {
			if evidence.Scanner != "" && !containsString(draft.Evidence, evidence.Scanner) {
				draft.Evidence = append(draft.Evidence, evidence.Scanner)
			}
			if draft.Provenance["source_url"] == "" {
				draft.Provenance["source_url"] = firstNonEmpty(evidence.SourceURL, evidence.ReviewURL)
			}
			if draft.Provenance["owner"] == "" {
				draft.Provenance["owner"] = evidence.Owner
			}
			if draft.Provenance["update_owner"] == "" {
				draft.Provenance["update_owner"] = evidence.UpdateOwner
			}
			if draft.Provenance["provider_metadata"] == "" {
				draft.Provenance["provider_metadata"] = evidence.ProviderMetadata
			}
		}
		key := normalizedAppKey(draft.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, draft)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("manual inventory agent output did not contain usable draft entries")
	}
	return out, nil
}

func AppendOverrideBlock(path string, content string) error {
	return appendStructuredContent(path, content)
}

func RenderOverridePreview(candidates []ReviewCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("# Generated preview by `updev inventory review --provider manual`.\n")
	builder.WriteString("# Review each entry before copying it into the configured inventory overrides TOML.\n")
	for _, candidate := range candidates {
		builder.WriteString("\n")
		builder.WriteString(strings.TrimRight(RenderOverrideBlock(candidate, candidate.SuggestedOverride), "\n"))
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func RenderOverrideBlock(candidate ReviewCandidate, override ReviewOverrideFields) string {
	var builder strings.Builder
	builder.WriteString(RenderOverrideFields(override))
	if candidate.ReasonCode != "" {
		builder.WriteString("# reason_code = ")
		builder.WriteString(tomlString(candidate.ReasonCode))
		builder.WriteString("\n")
	}
	if candidate.RemediationCode != "" {
		builder.WriteString("# remediation_code = ")
		builder.WriteString(tomlString(candidate.RemediationCode))
		builder.WriteString("\n")
	}
	if candidate.Confidence != "" {
		builder.WriteString("# confidence = ")
		builder.WriteString(tomlString(candidate.Confidence))
		builder.WriteString("\n")
	}
	for _, evidence := range candidate.Evidence {
		builder.WriteString("# evidence")
		if evidence.Scanner != "" {
			builder.WriteString(" scanner=")
			builder.WriteString(tomlString(evidence.Scanner))
		}
		if evidence.Path != "" {
			builder.WriteString(" path=")
			builder.WriteString(tomlString(evidence.Path))
		}
		if evidence.ReviewURL != "" {
			builder.WriteString(" review_url=")
			builder.WriteString(tomlString(evidence.ReviewURL))
		}
		if evidence.SourceURL != "" {
			builder.WriteString(" source_url=")
			builder.WriteString(tomlString(evidence.SourceURL))
		}
		if evidence.Owner != "" {
			builder.WriteString(" owner=")
			builder.WriteString(tomlString(evidence.Owner))
		}
		if evidence.ManagedBy != "" {
			builder.WriteString(" managed_by=")
			builder.WriteString(tomlString(evidence.ManagedBy))
		}
		if evidence.UpdateOwner != "" {
			builder.WriteString(" update_owner=")
			builder.WriteString(tomlString(evidence.UpdateOwner))
		}
		if evidence.OwnershipConfidence != "" {
			builder.WriteString(" ownership_confidence=")
			builder.WriteString(tomlString(evidence.OwnershipConfidence))
		}
		if evidence.ProviderMetadata != "" {
			builder.WriteString(" provider_metadata=")
			builder.WriteString(tomlString(evidence.ProviderMetadata))
		}
		if evidence.BundleID != "" {
			builder.WriteString(" bundle_id=")
			builder.WriteString(tomlString(evidence.BundleID))
		}
		if evidence.Version != "" {
			builder.WriteString(" version=")
			builder.WriteString(tomlString(evidence.Version))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func RenderOverrideFields(override ReviewOverrideFields) string {
	var builder strings.Builder
	builder.WriteString("[[manual.apps]]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlString(override.Name))
	builder.WriteString("\n")
	if len(override.Aliases) > 0 {
		builder.WriteString("aliases = ")
		builder.WriteString(tomlStringArray(override.Aliases))
		builder.WriteString("\n")
	}
	if override.Category != "" {
		builder.WriteString("category = ")
		builder.WriteString(tomlString(override.Category))
		builder.WriteString("\n")
	}
	if override.ManagedBy != "" {
		builder.WriteString("managed_by = ")
		builder.WriteString(tomlString(override.ManagedBy))
		builder.WriteString("\n")
	}
	if override.Lifecycle != "" {
		builder.WriteString("lifecycle = ")
		builder.WriteString(tomlString(override.Lifecycle))
		builder.WriteString("\n")
	}
	if override.Detail != "" {
		builder.WriteString("detail = ")
		builder.WriteString(tomlString(override.Detail))
		builder.WriteString("\n")
	}
	return builder.String()
}

func ReviewCandidateIdentityKeys(candidate ReviewCandidate) []string {
	keys := []string{}
	for _, key := range AppNameKeys(candidate.Name) {
		keys = append(keys, "name:"+key)
	}
	for _, alias := range candidate.SuggestedOverride.Aliases {
		if key := normalizedAppKey(alias); key != "" {
			keys = append(keys, "name:"+key)
		}
	}
	for _, evidence := range candidate.Evidence {
		if evidence.BundleID != "" {
			keys = append(keys, "bundle:"+strings.ToLower(evidence.BundleID))
		}
		if evidence.MASID != "" {
			keys = append(keys, "mas:"+strings.ToLower(evidence.MASID))
		}
		if evidence.Path != "" {
			for _, key := range AppPathKeys(evidence.Path) {
				keys = append(keys, "name:"+key)
			}
		}
	}
	return keys
}

func AgentDraftMatchesCandidate(draft StructuredApp, candidateIndex map[string]ReviewCandidate) (ReviewCandidate, bool) {
	for _, key := range StructuredAppIdentityKeys(draft) {
		if candidate, ok := candidateIndex[key]; ok {
			return candidate, true
		}
	}
	return ReviewCandidate{}, false
}

func StructuredAppIdentityKeys(app StructuredApp) []string {
	keys := []string{}
	if bundleID := strings.TrimSpace(app.Identifiers["bundle_id"]); bundleID != "" {
		keys = append(keys, "bundle:"+strings.ToLower(bundleID))
	}
	if masID := strings.TrimSpace(app.Identifiers["mas_id"]); masID != "" {
		keys = append(keys, "mas:"+strings.ToLower(masID))
	}
	if path := strings.TrimSpace(app.Identifiers["path"]); path != "" {
		for _, key := range AppPathKeys(path) {
			keys = append(keys, "name:"+key)
		}
	}
	if cask := strings.TrimSpace(app.Identifiers["cask"]); cask != "" {
		for _, key := range AppNameKeys(cask) {
			keys = append(keys, "name:"+key)
		}
	}
	for _, key := range AppNameKeys(app.Name) {
		keys = append(keys, "name:"+key)
	}
	for _, alias := range app.Aliases {
		if key := normalizedAppKey(alias); key != "" {
			keys = append(keys, "name:"+key)
		}
	}
	return keys
}

func AppPathKeys(path string) []string {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	base = strings.TrimSuffix(base, ".app")
	if base == "" || base == "." || base == string(filepath.Separator) {
		return nil
	}
	return AppNameKeys(base)
}

func AppNameKeys(name string) []string {
	keys := []string{normalizedAppKey(name)}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '／'
	}) {
		keys = append(keys, normalizedAppKey(part))
	}
	return keys
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
