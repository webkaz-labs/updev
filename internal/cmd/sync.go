package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/textui"
)

const syncReportSchemaVersion = 1

type syncOptions struct {
	format  string
	root    string
	refresh bool
}

type syncReport struct {
	SchemaVersion int         `json:"schema_version"`
	Status        plan.Status `json:"status"`
	Root          string      `json:"root"`
	Cached        bool        `json:"cached,omitempty"`
	CacheAge      string      `json:"cache_age,omitempty"`
	Entries       []syncEntry `json:"entries,omitempty"`
	Inventory     plan.Report `json:"inventory"`
}

type syncEntry struct {
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

func parseSyncOptions(args []string) (syncOptions, error) {
	opts := syncOptions{format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--refresh", "-r":
			opts.refresh = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runSync(opts syncOptions) int {
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, syncProgressMessage(defaultLanguage(), opts.refresh))
	}
	progress.Start()
	report := buildSyncReport(context.Background(), opts)
	progress.Done()
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSyncText(os.Stdout, report, textui.ColorEnabled())
	}
	return updateExitCode(report.Status)
}

func buildSyncReport(ctx context.Context, opts syncOptions) syncReport {
	result := collectInventoryCached(ctx, opts.root, opts.refresh, inventoryCacheMaxAge)
	inventory := result.Report
	entries := syncEntriesFromInventory(inventory)
	status := syncReportStatus(inventory, entries)
	report := syncReport{
		SchemaVersion: syncReportSchemaVersion,
		Status:        status,
		Root:          opts.root,
		Cached:        result.Cached,
		Entries:       entries,
		Inventory:     inventory,
	}
	if result.Cached && !result.CreatedAt.IsZero() {
		report.CacheAge = friendlyAge(time.Since(result.CreatedAt))
	}
	return report
}

func syncEntriesFromInventory(inventory plan.Report) []syncEntry {
	entries := []syncEntry{}
	for _, provider := range inventory.Providers {
		if provider.Unavailable || provider.Error != "" {
			entries = append(entries, syncEntry{
				Provider: provider.Name,
				Reason:   "unavailable",
				Status:   plan.StatusUnavailable,
				Action:   "retry",
				Detail:   "provider state or desired manifest evidence could not be collected",
			})
		}
	}
	related := syncProviderMismatchIndex(inventory.Items)
	manualIndex := manualAppIndex(inventory.Root)
	for _, item := range inventory.Items {
		reason := syncReasonForItemWithManual(item, related, manualIndex)
		if reason == "" {
			continue
		}
		entry := syncEntry{
			Provider: item.Provider,
			Kind:     item.Kind,
			Name:     item.Name,
			Category: item.Category,
			Reason:   reason,
			Status:   item.Status,
		}
		enrichSyncEntry(&entry, item, related)
		entries = append(entries, entry)
	}
	return entries
}

func syncReportStatus(inventory plan.Report, entries []syncEntry) plan.Status {
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

func syncReasonForItem(item plan.Item, related map[string]plan.Item) string {
	return syncReasonForItemWithManual(item, related, nil)
}

func syncReasonForItemWithManual(item plan.Item, related map[string]plan.Item, manualIndex map[string]toolRow) string {
	switch item.Status {
	case plan.StatusMissing:
		if _, ok := related[syncEntryKey(item)]; ok {
			return "provider-mismatch"
		}
		return "missing"
	case plan.StatusExtra:
		if _, ok := related[syncEntryKey(item)]; ok {
			return "provider-mismatch"
		}
		if itemHasProfileMismatch(item) {
			return "profile-mismatch"
		}
		if syncItemIsManualCask(item, manualIndex) {
			return "skipped"
		}
		return "extra"
	case plan.StatusUnavailable:
		return "unavailable"
	default:
		return ""
	}
}

func syncItemIsManualCask(item plan.Item, manualIndex map[string]toolRow) bool {
	if !strings.EqualFold(item.Provider, "brew") || !strings.EqualFold(item.Kind, "cask") {
		return false
	}
	row, ok := manualAppMatch(manualIndex, item.Name)
	if ok && row.State == "brew" {
		return false
	}
	return ok
}

func enrichSyncEntry(entry *syncEntry, item plan.Item, related map[string]plan.Item) {
	guidance := syncGuidanceForItem(entry.Reason, item, related)
	entry.Action = guidance.Action
	entry.Detail = guidance.Detail
	entry.RelatedProvider = guidance.RelatedProvider
	entry.RelatedKind = guidance.RelatedKind
}

type syncGuidance struct {
	Action          string
	Detail          string
	RelatedProvider string
	RelatedKind     string
}

func syncGuidanceForItem(reason string, item plan.Item, related map[string]plan.Item) syncGuidance {
	switch reason {
	case "provider-mismatch":
		if match, ok := related[syncEntryKey(item)]; ok {
			return syncGuidance{
				Action:          "choose-provider",
				Detail:          fmt.Sprintf(tr("desired/live state appears under %s/%s; choose one provider and update manifests", "desired/live 状態が %s/%s 側にあります。管理 provider を一つに決めて manifest を更新してください"), match.Provider, match.Kind),
				RelatedProvider: match.Provider,
				RelatedKind:     match.Kind,
			}
		}
		return syncGuidance{Action: "choose-provider", Detail: tr("desired/live state appears under another provider; choose one provider and update manifests", "desired/live 状態が別 provider 側にあります。管理 provider を一つに決めて manifest を更新してください")}
	case "missing":
		return missingSyncGuidance(item)
	case "extra":
		return extraSyncGuidance(item)
	case "profile-mismatch":
		return syncGuidance{Action: "switch-profile-or-remove", Detail: tr("installed entry is defined in another deployment scope; switch to that profile, remove it locally, or promote it into the active scope if it should be managed here", "インストール済み entry は別の deployment scope で定義されています。その profile に切り替えるか、ローカルから削除するか、この環境でも管理するなら active scope へ昇格してください")}
	case "skipped":
		return syncGuidance{Action: "manual-local-only", Detail: tr("cask is documented in docs/apps.md manual inventory; no Brewfile action is needed unless Homebrew should own it", "cask は docs/apps.md の manual inventory に記録済みです。Homebrew 管理に移す場合以外、Brewfile 操作は不要です")}
	case "unavailable":
		return syncGuidance{Action: "retry", Detail: tr("provider state or desired manifest evidence could not be collected", "provider 状態または desired manifest の証跡を取得できませんでした")}
	default:
		return syncGuidance{}
	}
}

func missingSyncGuidance(item plan.Item) syncGuidance {
	switch strings.ToLower(item.Provider) {
	case "brew":
		switch strings.ToLower(item.Kind) {
		case "tap":
			return syncGuidance{Action: "install-or-remove", Detail: fmt.Sprintf(tr("tap is desired but not active; run brew tap %s or remove it from Brewfile if no longer needed", "tap は desired ですが有効ではありません。必要なら brew tap %s、不要なら Brewfile から削除してください"), item.Name)}
		case "cask":
			return syncGuidance{Action: "install-or-remove", Detail: tr("cask is desired but missing; install with brew bundle/updev or remove it from Brewfile if no longer needed", "cask は desired ですが未インストールです。brew bundle/updev で入れるか、不要なら Brewfile から削除してください")}
		case "brew":
			return syncGuidance{Action: "install-or-migrate", Detail: tr("formula is desired in Brewfile but missing; install it, remove it, or move it to mise if mise should own it", "formula は Brewfile で desired ですが未インストールです。入れるか、削除するか、mise 管理にすべきなら移してください")}
		case "vscode":
			return syncGuidance{Action: "install-or-disable", Detail: tr("VS Code extension is desired but missing; install it or disable VS Code extension management if it is no longer used", "VS Code extension は desired ですが未インストールです。入れるか、使わないなら VS Code extension 管理を無効にしてください")}
		}
	case "mise":
		return syncGuidance{Action: "install-or-remove", Detail: tr("mise tool is desired but missing; run mise install or remove it from mise config if no longer needed", "mise tool は desired ですが未インストールです。mise install を実行するか、不要なら mise config から削除してください")}
	}
	return syncGuidance{Action: "install-or-remove", Detail: tr("desired entry is missing; install it or remove it from desired manifests if no longer needed", "desired entry が未インストールです。入れるか、不要なら desired manifest から削除してください")}
}

func extraSyncGuidance(item plan.Item) syncGuidance {
	switch strings.ToLower(item.Provider) {
	case "brew":
		switch strings.ToLower(item.Kind) {
		case "tap":
			return syncGuidance{Action: "adopt-or-untap", Detail: fmt.Sprintf(tr("tap is installed but not in Brewfile; add tap %s if it backs managed packages, otherwise run brew untap %s", "tap はインストール済みですが Brewfile にありません。管理対象が依存するなら tap %s を追加し、不要なら brew untap %s を実行してください"), item.Name, item.Name)}
		case "cask":
			return syncGuidance{Action: "adopt-remove-or-manual", Detail: tr("cask is installed outside desired manifests; add it to Brewfile, uninstall it, or document it as manual/local-only", "cask は desired manifest 外でインストールされています。Brewfile に追加するか、アンインストールするか、manual/local-only として記録してください")}
		case "brew":
			return syncGuidance{Action: "adopt-remove-or-migrate", Detail: tr("formula is explicitly installed but unmanaged; add it to Brewfile/mise, uninstall it, or document it as manual/local-only", "formula は明示インストール済みですが未管理です。Brewfile/mise に追加するか、アンインストールするか、manual/local-only として記録してください")}
		case "vscode":
			return syncGuidance{Action: "adopt-remove-or-disable", Detail: tr("VS Code extension is installed but unmanaged; add it only if VS Code extension management is opted in, otherwise remove or ignore it", "VS Code extension はインストール済みですが未管理です。VS Code extension 管理を opt-in する場合だけ追加し、それ以外は削除または無視してください")}
		}
	case "mise":
		return syncGuidance{Action: "adopt-remove-or-migrate", Detail: tr("mise tool is installed but not desired; add it to mise config, uninstall it, or move ownership to another provider", "mise tool はインストール済みですが desired ではありません。mise config に追加するか、アンインストールするか、別 provider に所有を移してください")}
	}
	return syncGuidance{Action: "adopt-remove-or-ignore", Detail: tr("installed entry is unmanaged; adopt it, remove it, or document it as manual/local-only", "インストール済み entry が未管理です。採用するか、削除するか、manual/local-only として記録してください")}
}

func syncProviderMismatchIndex(items []plan.Item) map[string]plan.Item {
	missing := map[string]plan.Item{}
	extra := map[string]plan.Item{}
	for _, item := range items {
		key := syncIdentityKey(item)
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
		related[syncEntryKey(missingItem)] = extraItem
		related[syncEntryKey(extraItem)] = missingItem
	}
	return related
}

func syncEntryKey(item plan.Item) string {
	return item.Provider + "\x00" + item.Kind + "\x00" + item.Name
}

func syncIdentityKey(item plan.Item) string {
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

func printSyncText(w io.Writer, report syncReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.Style("updev sync", "\033[1m", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", tr("root:", "ルート:"), report.Root)
	if report.Cached {
		fmt.Fprintf(w, "%s %s %s\n", tr("cache:", "キャッシュ:"), report.CacheAge+" old", tr("(use --refresh for a fresh read)", "(再取得は --refresh)"))
	}
	fmt.Fprintf(w, "%s %d\n", tr("entries:", "項目:"), len(report.Entries))
	if summary := syncReasonSummary(report.Entries); summary != "" {
		fmt.Fprintf(w, "%s %s\n", tr("summary:", "サマリー:"), summary)
	}
	if summary := syncCategorySummary(report.Entries, color); summary != "" {
		fmt.Fprintf(w, "%s %s\n", tr("categories:", "categories:"), summary)
	}
	if len(report.Entries) == 0 {
		fmt.Fprintf(w, "\n%s\n", tr("in sync", "同期済み"))
		return
	}
	rows := make([][]string, 0, len(report.Entries))
	for _, entry := range report.Entries {
		rows = append(rows, []string{
			textui.StyleName(entry.Provider, color),
			entry.Kind,
			textui.StyleName(entry.Name, color),
			entry.Category,
			textui.StyleStatus(entry.Reason, color),
			textui.StyleLabel(entry.Action, color),
		})
	}
	fmt.Fprintf(w, "\n%s\n", tr("reconcile", "reconcile"))
	textui.PrintTable(w, []textui.Column{
		{Header: "provider", Min: 8, Max: 10},
		{Header: "kind", Min: 7, Max: 10},
		{Header: "name", Min: 18, Max: 36},
		{Header: "category", Min: 8, Max: 10},
		{Header: "reason", Min: 10, Max: 18},
		{Header: "action", Min: 12, Max: 24},
	}, rows, color)
	printSyncEntryDetails(w, report.Entries, color)
	fmt.Fprintf(w, "\n%s\n", tr("next", "次"))
	fmt.Fprintf(w, "  %s\n", tr("review entries, then use updev add/remove or provider-specific commands; sync is read-only by default", "項目を確認してから updev add/remove または provider 固有コマンドを使ってください。sync は既定で read-only です"))
}

func printSyncEntryDetails(w io.Writer, entries []syncEntry, color bool) {
	wrote := false
	for _, entry := range entries {
		if entry.Detail == "" {
			continue
		}
		if !wrote {
			fmt.Fprintf(w, "\n%s\n", tr("details", "詳細"))
			wrote = true
		}
		target := entry.Provider
		if entry.Kind != "" || entry.Name != "" {
			target = strings.Trim(target+"/"+entry.Kind+" "+entry.Name, "/ ")
		}
		detail := entry.Detail
		if entry.RelatedProvider != "" {
			detail += fmt.Sprintf(" (related: %s/%s)", entry.RelatedProvider, entry.RelatedKind)
		}
		fmt.Fprintf(w, "  %s %s\n", textui.StyleName(target, color), textui.StyleDim(detail, color))
	}
}

func syncReasonSummary(entries []syncEntry) string {
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

func syncCategorySummary(entries []syncEntry, color bool) string {
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.Category != "" {
			counts[entry.Category]++
		}
	}
	keys := sortedMapKeys(counts)
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d (%s)", textui.StyleRequested(key, color), counts[key], categoryDescription(key)))
	}
	return strings.Join(parts, ", ")
}
