package support

import (
	"sort"
	"strings"
)

const (
	LabelSupportedPreview = "supported_preview"
	LabelExperimental     = "experimental"
	LabelCompatibility    = "compatibility"
	LabelDeferred         = "deferred"
)

type Entry struct {
	Surface     string   `json:"surface"`
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Summary     string   `json:"summary"`
	Scope       string   `json:"scope,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
	Next        string   `json:"next,omitempty"`
}

func Catalog() []Entry {
	entries := []Entry{
		{Surface: "provider", Name: "homebrew", Label: LabelSupportedPreview, Summary: "Homebrew package, cask, tap, trust, and update evidence on macOS.", Scope: "macOS package/app provider for the public preview", Evidence: []string{"brew JSON contracts", "Homebrew 6 trust diagnostics", "candidate-scoped safety gates"}, Limitations: []string{"external tap trust remains a human decision"}},
		{Surface: "provider", Name: "mise", Label: LabelSupportedPreview, Summary: "mise runtime/tool inventory, update, bump, manifest hygiene, and backend convergence evidence.", Scope: "developer CLI runtimes and tools", Evidence: []string{"mise JSON contracts", "minimum-release-age diagnostics", "backend metadata resolvers"}, Limitations: []string{"opaque backends remain review-held"}},
		{Surface: "provider", Name: "manual-apps", Label: LabelSupportedPreview, Summary: "macOS manual/vendor app evidence and review-first overrides.", Scope: "macOS .app bundle, MAS receipt, cask ownership, local-only review", Evidence: []string{"bundle identifiers", "MAS receipt evidence", "Homebrew cask matches"}, Limitations: []string{"agent enrichment stays draft-only"}},
		{Surface: "provider", Name: "vscode", Label: LabelExperimental, Summary: "Opt-in VS Code extension evidence and security gate support.", Scope: "extension inventory and advisory review", Evidence: []string{"code --list-extensions", "Marketplace metadata"}, Limitations: []string{"not part of default provider mutation path"}},
		{Surface: "provider", Name: "linux", Label: LabelExperimental, Summary: "Read-only Linux inventory scanners for early dogfood.", Scope: ".desktop, Flatpak, Snap, AppImage evidence", Evidence: []string{"fixture scanners", "container/VM dogfood required"}, Limitations: []string{"not a supported update provider in v0.7"}},
		{Surface: "provider", Name: "windows", Label: LabelExperimental, Summary: "Windows winget export fixture evidence.", Scope: "winget export JSON fixture/spike", Evidence: []string{"winget export parser fixtures"}, Limitations: []string{"requires real runner or machine before promotion"}},
		{Surface: "provider", Name: "external-installers", Label: LabelDeferred, Summary: "Vendor/external installer execution is not enabled by default.", Scope: "manual/vendor installers", Limitations: []string{"no automatic execution in v0.7"}, Next: "design gated preview and explicit confirmation before enabling"},
		{Surface: "provider", Name: "dynamic-plugins", Label: LabelDeferred, Summary: "Dynamic provider plugins are outside the v0.x preview scope.", Scope: "third-party provider loading", Next: "revisit only after the built-in provider contract is stable"},

		{Surface: "command", Name: "updev/update", Label: LabelSupportedPreview, Summary: "Primary update workflow with provider logs and post-update dashboard.", Evidence: []string{"structured update reports", "candidate-scoped safety gates"}},
		{Surface: "command", Name: "list/last/hub", Label: LabelSupportedPreview, Summary: "Primary human review surfaces for inventory and cached reports.", Evidence: []string{"grouped TTY browser", "Back/Home route restoration", "JSON/plain fallbacks"}},
		{Surface: "command", Name: "status/check/plan/sync", Label: LabelSupportedPreview, Summary: "Read-only state checks and reconciliation reports.", Evidence: []string{"inventory cache", "desired/live provider summaries"}},
		{Surface: "command", Name: "add/remove/edit/rollback", Label: LabelSupportedPreview, Summary: "Guided desired-state mutation and recovery flows.", Evidence: []string{"snapshots", "validation after edits"}},
		{Surface: "command", Name: "security scan/gate/review/policy", Label: LabelSupportedPreview, Summary: "Security findings, gates, local policy, and review decisions.", Evidence: []string{"shared decision vocabulary", "policy cleanup", "native/scanner evidence"}},
		{Surface: "command", Name: "backends doctor/plan", Label: LabelSupportedPreview, Summary: "Provider/backend convergence evidence and safe rewrite review.", Evidence: []string{"preference tiers", "applyability diagnostics"}},
		{Surface: "command", Name: "doctor dependencies", Label: LabelSupportedPreview, Summary: "Provider CLI/API contract checks and compatibility ledger output.", Evidence: []string{"local/CI contract probes", "ledger JSON"}},
		{Surface: "command", Name: "support", Label: LabelSupportedPreview, Summary: "Support-level catalog for providers, commands, reports, and inventory sources.", Evidence: []string{"static release catalog", "JSON/text output"}},
		{Surface: "command", Name: "skill/help agent", Label: LabelSupportedPreview, Summary: "Embedded agent guidance generated from canonical docs.", Evidence: []string{"docs-check drift coverage"}},
		{Surface: "command", Name: "brewfile", Label: LabelCompatibility, Summary: "Low-level compatibility surface for Brewfile-oriented workflows.", Scope: "not the main human workflow", Limitations: []string{"prefer updev add/remove/edit/sync for user-facing work"}},

		{Surface: "report", Name: "version", Label: LabelSupportedPreview, Summary: "Version report with SemVer parts and pre-stable contract label."},
		{Surface: "report", Name: "update", Label: LabelSupportedPreview, Summary: "Structured update, safety, inventory, and log report cache."},
		{Surface: "report", Name: "inventory/list", Label: LabelSupportedPreview, Summary: "Desired/live inventory and grouped list report data."},
		{Surface: "report", Name: "security", Label: LabelSupportedPreview, Summary: "Shared security decision vocabulary for gates, scans, review, and policy."},
		{Surface: "report", Name: "backend", Label: LabelSupportedPreview, Summary: "Backend convergence findings with recommendation tiers and applyability."},
		{Surface: "report", Name: "manual-inventory", Label: LabelSupportedPreview, Summary: "Manual app evidence, plan/check/review, and draft override reports."},
		{Surface: "report", Name: "compatibility-ledger", Label: LabelSupportedPreview, Summary: "Provider-version compatibility ledger from dependency contract checks."},
		{Surface: "report", Name: "linux-windows-inventory", Label: LabelExperimental, Summary: "Portable inventory evidence from Linux scanners and Windows winget export fixtures."},

		{Surface: "inventory_source", Name: "macos-app-bundle", Label: LabelSupportedPreview, Summary: "Local macOS .app bundle metadata.", Evidence: []string{"Info.plist", "bundle_id", "version"}},
		{Surface: "inventory_source", Name: "mac-app-store", Label: LabelSupportedPreview, Summary: "Mac App Store ownership evidence when receipt or mas evidence is available.", Evidence: []string{"MAS receipts", "mas list/search"}},
		{Surface: "inventory_source", Name: "homebrew-cask", Label: LabelSupportedPreview, Summary: "Homebrew cask ownership and adoption evidence.", Evidence: []string{"brew list --cask", "cask metadata"}},
		{Surface: "inventory_source", Name: "manual-markdown", Label: LabelCompatibility, Summary: "Repository-local Markdown source compatibility.", Limitations: []string{"ignored unless explicitly configured"}},
		{Surface: "inventory_source", Name: "linux-desktop-flatpak-snap-appimage", Label: LabelExperimental, Summary: "Linux read-only app evidence.", Evidence: []string{".desktop", "Flatpak", "Snap", "AppImage"}, Limitations: []string{"fixture/container dogfood required"}},
		{Surface: "inventory_source", Name: "windows-winget-export", Label: LabelExperimental, Summary: "Windows winget export JSON evidence.", Limitations: []string{"fixture/spike until real validation"}},
		{Surface: "inventory_source", Name: "agent-enrichment", Label: LabelExperimental, Summary: "Optional agent-generated manual app metadata drafts.", Limitations: []string{"default off", "schema validated", "review required before desired-state changes"}},
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Surface != entries[j].Surface {
			return entries[i].Surface < entries[j].Surface
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func Filter(entries []Entry, surface string, label string) []Entry {
	surface = strings.TrimSpace(strings.ToLower(surface))
	label = strings.TrimSpace(strings.ToLower(label))
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if surface != "" && surface != "all" && strings.ToLower(entry.Surface) != surface {
			continue
		}
		if label != "" && strings.ToLower(entry.Label) != label {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func ValidSurface(surface string) bool {
	switch strings.TrimSpace(strings.ToLower(surface)) {
	case "", "all", "provider", "command", "report", "inventory_source":
		return true
	default:
		return false
	}
}

func ValidLabel(label string) bool {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "", LabelSupportedPreview, LabelExperimental, LabelCompatibility, LabelDeferred:
		return true
	default:
		return false
	}
}
