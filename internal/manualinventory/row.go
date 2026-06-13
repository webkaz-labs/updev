package manualinventory

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var planURLPattern = regexp.MustCompile(`https?://[^)\s;]+`)

type ReviewRow struct {
	SectionName string
	Name        string
	State       string
	Detail      string
	Version     string
}

func DetailValue(detail string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	prefix := key + ":"
	for _, part := range strings.Split(detail, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(part, prefix))
		}
	}
	return ""
}

func DetailFirstValue(detail string, keys ...string) string {
	for _, key := range keys {
		if value := DetailValue(detail, key); value != "" {
			return value
		}
	}
	return ""
}

func EvidenceFromRow(row ReviewRow) ReviewEvidence {
	version := row.Version
	if version == "" {
		version = DetailValue(row.Detail, "version")
	}
	path := DetailValue(row.Detail, "path")
	masID := DetailValue(row.Detail, "mas_id")
	scanner := "macos_app_bundle"
	if path == "" && masID != "" {
		scanner = "mas_list"
	}
	identifiers := map[string]string{}
	for _, key := range []string{"desktop_id", "package_id", "app_id"} {
		if value := DetailValue(row.Detail, key); value != "" {
			identifiers[key] = value
		}
	}
	return ReviewEvidence{
		Scanner:             scanner,
		Source:              DetailValue(row.Detail, "source"),
		Path:                path,
		ReviewURL:           PlanReviewURL(row),
		SourceURL:           DetailFirstValue(row.Detail, "source_url", "url", "homepage"),
		Owner:               DetailFirstValue(row.Detail, "owner", "ownership", "publisher", "developer", "vendor"),
		ManagedBy:           EvidenceManagedBy(row),
		UpdateOwner:         EvidenceUpdateOwner(row),
		OwnershipConfidence: EvidenceOwnershipConfidence(row),
		ProviderMetadata:    EvidenceProviderMetadata(row),
		MASID:               masID,
		BundleID:            DetailValue(row.Detail, "bundle_id"),
		Version:             version,
		Identifiers:         identifiers,
	}
}

func EvidenceManagedBy(row ReviewRow) string {
	if value := DetailValue(row.Detail, "managed_by"); value != "" {
		return value
	}
	if strings.EqualFold(DetailValue(row.Detail, "source"), "homebrew cask") || DetailValue(row.Detail, "cask") != "" || row.State == "brew" {
		return "brew"
	}
	return SuggestedManagedBy(row)
}

func EvidenceUpdateOwner(row ReviewRow) string {
	if value := DetailValue(row.Detail, "update_owner"); value != "" {
		return value
	}
	switch EvidenceManagedBy(row) {
	case "brew":
		return "brew"
	case "mas":
		return "mas"
	default:
		return ""
	}
}

func EvidenceOwnershipConfidence(row ReviewRow) string {
	if value := DetailValue(row.Detail, "ownership_confidence"); value != "" {
		return value
	}
	if value := DetailValue(row.Detail, "confidence"); value != "" {
		return value
	}
	switch EvidenceManagedBy(row) {
	case "brew", "mas":
		return "high"
	default:
		return ReviewConfidence(row)
	}
}

func EvidenceProviderMetadata(row ReviewRow) string {
	if value := DetailValue(row.Detail, "provider_metadata"); value != "" {
		return value
	}
	source := DetailValue(row.Detail, "source")
	switch {
	case strings.EqualFold(source, "homebrew cask"):
		return "Homebrew cask inventory"
	case strings.EqualFold(source, "mas list"):
		return "mas list"
	case strings.EqualFold(source, "mac app store receipt"):
		return "mac app store receipt"
	case strings.EqualFold(source, "app bundle"):
		return "Info.plist"
	default:
		return ""
	}
}

func SuggestedAliases(row ReviewRow) []string {
	aliases := []string{}
	if path := DetailValue(row.Detail, "path"); path != "" {
		base := filepath.Base(path)
		if base != "" {
			aliases = append(aliases, base)
		}
	}
	if bundleID := DetailValue(row.Detail, "bundle_id"); bundleID != "" {
		aliases = append(aliases, bundleID)
	}
	for _, key := range []string{"desktop_id", "package_id", "app_id"} {
		if value := DetailValue(row.Detail, key); value != "" {
			aliases = append(aliases, value)
		}
	}
	return aliases
}

func SuggestedManagedBy(row ReviewRow) string {
	if strings.Contains(row.Detail, "source: mas list") {
		return "mas"
	}
	switch DetailValue(row.Detail, "source") {
	case "mac app store receipt":
		return "mas"
	default:
		return "manual"
	}
}

func ReviewConfidence(row ReviewRow) string {
	if strings.Contains(row.Detail, "source: mas list") {
		return "high"
	}
	switch DetailValue(row.Detail, "source") {
	case "mac app store receipt":
		return "high"
	default:
		return "medium"
	}
}

func PlanAction(row ReviewRow) string {
	if row.State == "brew" || strings.Contains(row.Detail, "source: homebrew cask") {
		return "adopt-brew"
	}
	if SuggestedManagedBy(row) == "mas" {
		return "adopt-mas"
	}
	if RowIsUserLocal(row) {
		return "ignore-local"
	}
	if row.SectionName == "manual/installed-apps" || row.State == "installed" {
		return "needs-review"
	}
	if RowHasVendorSource(row) {
		return "open-vendor"
	}
	return "keep-manual"
}

func PlanSuggestedProvider(action string, row ReviewRow) string {
	switch action {
	case "adopt-brew":
		return "brew"
	case "adopt-mas":
		return "mas"
	case "open-vendor":
		return "vendor"
	case "ignore-local", "needs-review":
		return SuggestedManagedBy(row)
	default:
		return ""
	}
}

func RowHasVendorSource(row ReviewRow) bool {
	detail := strings.ToLower(row.Detail)
	return strings.Contains(detail, "vendor") ||
		strings.Contains(row.Detail, "ベンダー") ||
		strings.Contains(row.Detail, "入手先:") ||
		strings.Contains(detail, "source:")
}

func RowIsUserLocal(row ReviewRow) bool {
	path := DetailValue(row.Detail, "path")
	return strings.Contains(path, "/Users/") && strings.Contains(path, "/Applications/")
}

func PlanActionNeedsReview(action string) bool {
	switch action {
	case "adopt-brew", "adopt-mas", "ignore-local", "needs-review", "open-vendor":
		return true
	default:
		return false
	}
}

func PlanConfidence(action string, row ReviewRow) string {
	switch action {
	case "adopt-brew", "adopt-mas":
		return "high"
	case "open-vendor", "ignore-local":
		return "medium"
	case "needs-review":
		return ReviewConfidence(row)
	default:
		return ""
	}
}

func PlanReasonCode(action string) string {
	switch action {
	case "adopt-brew":
		return "manual_app_homebrew_cask_available"
	case "adopt-mas":
		return "manual_app_mas_available"
	case "open-vendor":
		return "manual_app_vendor_review"
	case "ignore-local":
		return "manual_app_user_local"
	case "needs-review":
		return "manual_app_live_only"
	default:
		return ""
	}
}

func PlanRemediationCode(action string) string {
	switch action {
	case "adopt-brew", "adopt-mas", "needs-review":
		return "manual_inventory_override"
	case "open-vendor":
		return "manual_vendor_review"
	case "ignore-local":
		return "manual_inventory_ignore"
	default:
		return ""
	}
}

func PlanReviewURL(row ReviewRow) string {
	if url := DetailFirstValue(row.Detail, "review_url", "source_url", "url", "homepage"); url != "" {
		return strings.TrimRight(url, ".,")
	}
	match := planURLPattern.FindString(row.Detail)
	return strings.TrimRight(match, ".,")
}

func PlanInstallHint(action string, row ReviewRow) string {
	switch action {
	case "adopt-brew":
		if cask := DetailValue(row.Detail, "cask"); cask != "" {
			return "review Homebrew cask metadata before moving ownership to cask " + cask
		}
		return "review Homebrew cask metadata before moving ownership to Homebrew"
	case "adopt-mas":
		if masID := DetailValue(row.Detail, "mas_id"); masID != "" {
			return "verify Mac App Store ownership for mas id " + masID + " before adding an override"
		}
		return "verify Mac App Store ownership before adding an override"
	case "open-vendor":
		if PlanReviewURL(row) != "" {
			return "open the vendor URL for review only; do not run installer commands from inventory output"
		}
		return "review the vendor source manually; inventory output must not install external packages"
	case "ignore-local":
		return "add a local-only override only after confirming this app is machine-local"
	case "needs-review":
		return "accept, edit, or ignore one explicit override after ownership review"
	default:
		return ""
	}
}

func PlanCommandPreview(action string, row ReviewRow) []string {
	switch action {
	case "adopt-brew":
		if cask := DetailValue(row.Detail, "cask"); cask != "" {
			return []string{"brew info --cask " + strconv.Quote(cask)}
		}
	case "adopt-mas":
		if masID := DetailValue(row.Detail, "mas_id"); masID != "" {
			return []string{"mas lookup " + strconv.Quote(masID)}
		}
		return []string{"mas search " + strconv.Quote(row.Name)}
	case "open-vendor":
		if url := PlanReviewURL(row); url != "" {
			return []string{"open " + strconv.Quote(url)}
		}
	case "ignore-local":
		return []string{"updev inventory review --provider manual --action ignore --query " + strconv.Quote(row.Name)}
	case "needs-review":
		return []string{"updev inventory review --provider manual --query " + strconv.Quote(row.Name)}
	}
	return nil
}

func PlanSuggestedOverride(action string, row ReviewRow) ReviewOverrideFields {
	override := ReviewOverrideFields{
		Name:    row.Name,
		Aliases: PlanSuggestedAliases(action, row),
		Detail:  PlanInstallHint(action, row),
	}
	switch action {
	case "adopt-brew":
		override.ManagedBy = "brew"
	case "adopt-mas":
		override.ManagedBy = "mas"
	case "open-vendor":
		override.ManagedBy = "vendor"
	case "ignore-local":
		override.Category = "Ignored"
		override.Lifecycle = "local-only"
	case "needs-review":
		override.ManagedBy = SuggestedManagedBy(row)
		override.Detail = "review installed app ownership and lifecycle"
	}
	return override
}

func PlanSuggestedAliases(action string, row ReviewRow) []string {
	aliases := SuggestedAliases(row)
	if action == "adopt-brew" {
		if cask := DetailValue(row.Detail, "cask"); cask != "" {
			aliases = append(aliases, cask)
		}
	}
	return aliases
}

func PlanNextStep(action string, row ReviewRow) string {
	switch action {
	case "adopt-brew":
		if cask := DetailValue(row.Detail, "cask"); cask != "" {
			return "review Homebrew cask ownership, then keep desired state with cask " + cask + " or add an alias override"
		}
		return "review Homebrew cask ownership, then keep desired state or add an alias override"
	case "adopt-mas":
		return "review Mac App Store ownership, then add an override with managed_by = \"mas\""
	case "open-vendor":
		if url := PlanReviewURL(row); url != "" {
			return "open the vendor source " + url + " for review, then keep manual management or add an ignore/override decision"
		}
		return "open the vendor source manually, then keep manual management or add an ignore/override decision"
	case "ignore-local":
		return "review whether this user-local app should stay local-only or be ignored by manual inventory"
	case "needs-review":
		return "review ownership and lifecycle, then accept/edit/ignore the suggested inventory override"
	default:
		return "keep this manual inventory row as documented"
	}
}
