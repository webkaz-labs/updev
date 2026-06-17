package syncreport

import (
	"fmt"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/textui"
)

const SchemaVersion = 1

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	Status        plan.Status `json:"status"`
	Root          string      `json:"root"`
	Cached        bool        `json:"cached,omitempty"`
	CacheAge      string      `json:"cache_age,omitempty"`
	Entries       []Entry     `json:"entries,omitempty"`
	Inventory     plan.Report `json:"inventory"`
}

type Entry struct {
	Provider        string      `json:"provider"`
	Kind            string      `json:"kind"`
	Name            string      `json:"name"`
	Category        string      `json:"category,omitempty"`
	Reason          string      `json:"reason"`
	Status          plan.Status `json:"status"`
	Action          string      `json:"action,omitempty"`
	Detail          string      `json:"detail,omitempty"`
	RelatedProvider string      `json:"related_provider,omitempty"`
	RelatedKind     string      `json:"related_kind,omitempty"`
}

type Guidance struct {
	Action          string
	Detail          string
	RelatedProvider string
	RelatedKind     string
}

type ManualLocalOnlyFunc func(plan.Item) bool

func Build(inventory plan.Report, cached bool, createdAt time.Time, manualLocalOnly ManualLocalOnlyFunc, lang string) Report {
	entries := EntriesFromInventory(inventory, manualLocalOnly, lang)
	report := Report{
		SchemaVersion: SchemaVersion,
		Status:        Status(inventory, entries),
		Root:          inventory.Root,
		Cached:        cached,
		Entries:       entries,
		Inventory:     inventory,
	}
	if cached && !createdAt.IsZero() {
		report.CacheAge = textui.FriendlyAge(time.Since(createdAt))
	}
	return report
}

func EntriesFromInventory(inventory plan.Report, manualLocalOnly ManualLocalOnlyFunc, lang string) []Entry {
	entries := []Entry{}
	for _, provider := range inventory.Providers {
		if provider.Unavailable || provider.Error != "" {
			entries = append(entries, Entry{
				Provider: provider.Name,
				Reason:   "unavailable",
				Status:   plan.StatusUnavailable,
				Action:   "retry",
				Detail:   i18n.Pick(lang, "provider state or desired manifest evidence could not be collected", "provider 状態または desired manifest の証跡を取得できませんでした"),
			})
		}
	}
	related := ProviderMismatchIndex(inventory.Items)
	for _, item := range inventory.Items {
		reason := ReasonForItem(item, related, manualLocalOnly)
		if reason == "" {
			continue
		}
		entry := Entry{
			Provider: item.Provider,
			Kind:     item.Kind,
			Name:     item.Name,
			Category: item.Category,
			Reason:   reason,
			Status:   item.Status,
		}
		EnrichEntry(&entry, item, related, lang)
		entries = append(entries, entry)
	}
	return entries
}

func Status(inventory plan.Report, entries []Entry) plan.Status {
	if inventory.Status == plan.StatusError {
		return plan.StatusError
	}
	for _, entry := range entries {
		if entry.Reason != "skipped" {
			return plan.StatusDrift
		}
	}
	return plan.StatusOK
}

func ReasonForItem(item plan.Item, related map[string]plan.Item, manualLocalOnly ManualLocalOnlyFunc) string {
	switch item.Status {
	case plan.StatusMissing:
		if _, ok := related[EntryKey(item)]; ok {
			return "provider-mismatch"
		}
		return "missing"
	case plan.StatusExtra:
		if _, ok := related[EntryKey(item)]; ok {
			return "provider-mismatch"
		}
		if inventoryannotate.ItemHasProfileMismatch(item) {
			return "profile-mismatch"
		}
		if manualLocalOnly != nil && manualLocalOnly(item) {
			return "skipped"
		}
		return "extra"
	case plan.StatusUnavailable:
		return "unavailable"
	default:
		return ""
	}
}

func EnrichEntry(entry *Entry, item plan.Item, related map[string]plan.Item, lang string) {
	guidance := GuidanceForItem(entry.Reason, item, related, lang)
	entry.Action = guidance.Action
	entry.Detail = guidance.Detail
	entry.RelatedProvider = guidance.RelatedProvider
	entry.RelatedKind = guidance.RelatedKind
}

func GuidanceForItem(reason string, item plan.Item, related map[string]plan.Item, lang string) Guidance {
	switch reason {
	case "provider-mismatch":
		if match, ok := related[EntryKey(item)]; ok {
			return Guidance{
				Action:          "choose-provider",
				Detail:          fmt.Sprintf(i18n.Pick(lang, "desired/live state appears under %s/%s; choose one provider and update manifests", "desired/live 状態が %s/%s 側にあります。管理 provider を一つに決めて manifest を更新してください"), match.Provider, match.Kind),
				RelatedProvider: match.Provider,
				RelatedKind:     match.Kind,
			}
		}
		return Guidance{Action: "choose-provider", Detail: i18n.Pick(lang, "desired/live state appears under another provider; choose one provider and update manifests", "desired/live 状態が別 provider 側にあります。管理 provider を一つに決めて manifest を更新してください")}
	case "missing":
		return MissingGuidance(item, lang)
	case "extra":
		return ExtraGuidance(item, lang)
	case "profile-mismatch":
		return Guidance{Action: "switch-profile-or-remove", Detail: i18n.Pick(lang, "installed entry is defined in another deployment scope; switch to that profile, remove it locally, or promote it into the active scope if it should be managed here", "インストール済み entry は別の deployment scope で定義されています。その profile に切り替えるか、ローカルから削除するか、この環境でも管理するなら active scope へ昇格してください")}
	case "skipped":
		return Guidance{Action: "manual-local-only", Detail: i18n.Pick(lang, "cask is documented in docs/apps.md manual inventory; no Brewfile action is needed unless Homebrew should own it", "cask は docs/apps.md の manual inventory に記録済みです。Homebrew 管理に移す場合以外、Brewfile 操作は不要です")}
	case "unavailable":
		return Guidance{Action: "retry", Detail: i18n.Pick(lang, "provider state or desired manifest evidence could not be collected", "provider 状態または desired manifest の証跡を取得できませんでした")}
	default:
		return Guidance{}
	}
}

func MissingGuidance(item plan.Item, lang string) Guidance {
	switch strings.ToLower(item.Provider) {
	case "brew":
		switch strings.ToLower(item.Kind) {
		case "tap":
			return Guidance{Action: "install-or-remove", Detail: fmt.Sprintf(i18n.Pick(lang, "tap is desired but not active; run brew tap %s or remove it from Brewfile if no longer needed", "tap は desired ですが有効ではありません。必要なら brew tap %s、不要なら Brewfile から削除してください"), item.Name)}
		case "cask":
			return Guidance{Action: "install-or-remove", Detail: i18n.Pick(lang, "cask is desired but missing; install with brew bundle/updev or remove it from Brewfile if no longer needed", "cask は desired ですが未インストールです。brew bundle/updev で入れるか、不要なら Brewfile から削除してください")}
		case "brew":
			return Guidance{Action: "install-or-migrate", Detail: i18n.Pick(lang, "formula is desired in Brewfile but missing; install it, remove it, or move it to mise if mise should own it", "formula は Brewfile で desired ですが未インストールです。入れるか、削除するか、mise 管理にすべきなら移してください")}
		case "vscode":
			return Guidance{Action: "install-or-disable", Detail: i18n.Pick(lang, "VS Code extension is desired but missing; install it or disable VS Code extension management if it is no longer used", "VS Code extension は desired ですが未インストールです。入れるか、使わないなら VS Code extension 管理を無効にしてください")}
		}
	case "mise":
		return Guidance{Action: "install-or-remove", Detail: i18n.Pick(lang, "mise tool is desired but missing; run mise install or remove it from mise config if no longer needed", "mise tool は desired ですが未インストールです。mise install を実行するか、不要なら mise config から削除してください")}
	}
	return Guidance{Action: "install-or-remove", Detail: i18n.Pick(lang, "desired entry is missing; install it or remove it from desired manifests if no longer needed", "desired entry が未インストールです。入れるか、不要なら desired manifest から削除してください")}
}

func ExtraGuidance(item plan.Item, lang string) Guidance {
	switch strings.ToLower(item.Provider) {
	case "brew":
		switch strings.ToLower(item.Kind) {
		case "tap":
			return Guidance{Action: "adopt-or-untap", Detail: fmt.Sprintf(i18n.Pick(lang, "tap is installed but not in Brewfile; add tap %s if it backs managed packages, otherwise run brew untap %s", "tap はインストール済みですが Brewfile にありません。管理対象が依存するなら tap %s を追加し、不要なら brew untap %s を実行してください"), item.Name, item.Name)}
		case "cask":
			return Guidance{Action: "adopt-remove-or-manual", Detail: i18n.Pick(lang, "cask is installed outside desired manifests; add it to Brewfile, uninstall it, or document it as manual/local-only", "cask は desired manifest 外でインストールされています。Brewfile に追加するか、アンインストールするか、manual/local-only として記録してください")}
		case "brew":
			return Guidance{Action: "adopt-remove-or-migrate", Detail: i18n.Pick(lang, "formula is explicitly installed but unmanaged; add it to Brewfile/mise, uninstall it, or document it as manual/local-only", "formula は明示インストール済みですが未管理です。Brewfile/mise に追加するか、アンインストールするか、manual/local-only として記録してください")}
		case "vscode":
			return Guidance{Action: "adopt-remove-or-disable", Detail: i18n.Pick(lang, "VS Code extension is installed but unmanaged; add it only if VS Code extension management is opted in, otherwise remove or ignore it", "VS Code extension はインストール済みですが未管理です。VS Code extension 管理を opt-in する場合だけ追加し、それ以外は削除または無視してください")}
		}
	case "mise":
		return Guidance{Action: "adopt-remove-or-migrate", Detail: i18n.Pick(lang, "mise tool is installed but not desired; add it to mise config, uninstall it, or move ownership to another provider", "mise tool はインストール済みですが desired ではありません。mise config に追加するか、アンインストールするか、別 provider に所有を移してください")}
	}
	return Guidance{Action: "adopt-remove-or-ignore", Detail: i18n.Pick(lang, "installed entry is unmanaged; adopt it, remove it, or document it as manual/local-only", "インストール済み entry が未管理です。採用するか、削除するか、manual/local-only として記録してください")}
}

func HomebrewExtraAdoptable(item plan.Item) bool {
	if !strings.EqualFold(item.Provider, "brew") || item.Status != plan.StatusExtra || !item.Live {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "brew", "cask", "tap", "vscode":
		return strings.TrimSpace(item.Name) != ""
	default:
		return false
	}
}

func HomebrewExtraDriftDetail(item plan.Item, lang string) string {
	if !HomebrewExtraAdoptable(item) {
		return ""
	}
	return i18n.Pick(lang,
		"Homebrew reports this entry as installed, but it is not in the desired Brewfile. This can happen when it was installed through command brew/full-path brew, another shell, brew bundle, a vendor/app sidecar installer, or before the updev shell wrapper was adopted. Choose an explicit category before adopting it into Brewfile; otherwise uninstall it outside updev or document it as manual/local-only.",
		"Homebrew にはインストール済みですが、desired Brewfile にはありません。原因候補: command brew / フルパス brew、別 shell、brew bundle、vendor/app の sidecar installer、updev shell wrapper 導入前の install。Brewfile に採用する場合は category を明示して選びます。採用しない場合は updev 外で uninstall するか、manual/local-only として記録してください。")
}

func ProviderMismatchIndex(items []plan.Item) map[string]plan.Item {
	missing := map[string]plan.Item{}
	extra := map[string]plan.Item{}
	for _, item := range items {
		key := IdentityKey(item)
		if key == "" {
			continue
		}
		switch item.Status {
		case plan.StatusMissing:
			missing[key] = item
		case plan.StatusExtra:
			extra[key] = item
		}
	}
	related := map[string]plan.Item{}
	for key, missingItem := range missing {
		extraItem, ok := extra[key]
		if !ok || extraItem.Provider == missingItem.Provider {
			continue
		}
		related[EntryKey(missingItem)] = extraItem
		related[EntryKey(extraItem)] = missingItem
	}
	return related
}

func EntryKey(item plan.Item) string {
	return item.Provider + "\x00" + item.Kind + "\x00" + item.Name
}

func IdentityKey(item plan.Item) string {
	name := strings.ToLower(strings.TrimSpace(item.Name))
	if name == "" {
		return ""
	}
	if after, ok := strings.CutPrefix(name, "npm:"); ok {
		name = after
	}
	if after, ok := strings.CutPrefix(name, "cargo:"); ok {
		name = after
	}
	if after, ok := strings.CutPrefix(name, "pipx:"); ok {
		name = after
	}
	if strings.Contains(name, "/") {
		return ""
	}
	return name
}

func ReasonSummary(entries []Entry) string {
	counts := map[string]int{}
	for _, entry := range entries {
		counts[entry.Reason]++
	}
	parts := []string{}
	for _, reason := range []string{"missing", "extra", "provider-mismatch", "profile-mismatch", "skipped", "unavailable"} {
		if counts[reason] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", reason, counts[reason]))
		}
	}
	return strings.Join(parts, ", ")
}
