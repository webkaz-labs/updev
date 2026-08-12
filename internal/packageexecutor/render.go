package packageexecutor

import (
	"fmt"
	"io"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

func PrintText(w io.Writer, report Report, lang string, color bool) {
	tr := func(en string, ja string) string { return i18n.Pick(lang, en, ja) }
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev doctor package-executors", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s/%s\n", textui.StyleLabel(tr("platform:", "platform:"), color), report.Platform.OS, report.Platform.Arch)
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("summary:", "サマリー:"), color), fmt.Sprintf(
		tr("native %d, mise %d, unsupported %d", "native %d件, mise %d件, unsupported %d件"),
		report.Summary.Native,
		report.Summary.Mise,
		report.Summary.Unsupported,
	))
	rows := make([][]string, 0, len(report.Items))
	for _, item := range report.Items {
		rows = append(rows, []string{
			textui.StyleStatusText(executorLabel(item.Executor, lang), executorStatus(item.Executor), color),
			item.DesiredSource,
			item.Kind,
			item.Name,
			reasonLabel(item.ReasonCode, lang),
		})
	}
	fmt.Fprintln(w)
	textui.PrintTable(w, []textui.Column{
		{Header: "executor", Min: 8, Max: 11},
		{Header: tr("source", "source"), Min: 8, Max: 8},
		{Header: tr("kind", "種別"), Min: 7, Max: 9},
		{Header: tr("name", "名前"), Min: 12, Max: 32},
		{Header: tr("reason", "理由"), Min: 18, Max: 46},
	}, rows, color)
}

func DetailRows(report Report, lang string) []reviewui.DetailRow {
	rows := make([]reviewui.DetailRow, 0, len(report.Items))
	for _, item := range report.Items {
		managerAvailability := "unknown"
		if item.ManagerAvailable != nil {
			managerAvailability = fmt.Sprintf("%t", *item.ManagerAvailable)
		}
		rows = append(rows, reviewui.DetailRow{
			Title:   item.Identity,
			Status:  string(item.Status),
			Summary: executorLabel(item.Executor, lang) + " - " + reasonLabel(item.ReasonCode, lang),
			Detail:  ReasonText(item, lang),
			Metadata: []string{
				"identity: " + item.Identity,
				"desired source: " + item.DesiredSource,
				"manager: " + firstNonEmpty(item.Manager, "-"),
				"manager package: " + firstNonEmpty(item.ManagerPackage, "-"),
				"manager available: " + managerAvailability,
				fmt.Sprintf("native available: %t", item.NativeAvailable),
				"metadata executor: " + firstNonEmpty(item.MetadataExecutor, "auto"),
				"reason code: " + item.ReasonCode,
			},
			Columns: []string{item.Executor, item.DesiredSource, item.Kind, item.Name, reasonLabel(item.ReasonCode, lang)},
			ColumnHeaders: []reviewui.DetailColumn{
				{Header: "executor", Min: 8, Max: 11},
				{Header: "source", Min: 8, Max: 8},
				{Header: i18n.Pick(lang, "kind", "種別"), Min: 7, Max: 9},
				{Header: i18n.Pick(lang, "name", "名前"), Min: 12, Max: 30},
				{Header: i18n.Pick(lang, "reason", "理由"), Min: 18, Max: 42},
			},
		})
	}
	return rows
}

func executorLabel(executor string, lang string) string {
	if executor == ExecutorUnsupported {
		return i18n.Pick(lang, "unsupported", "非対応")
	}
	return executor
}

func executorStatus(executor string) string {
	if executor == ExecutorUnsupported {
		return "unavailable"
	}
	return "ok"
}

func reasonLabel(code string, lang string) string {
	labels := map[string][2]string{
		"intentional-duplicate-required": {"duplicate annotation required", "duplicate注記が必要"},
		"explicit-native":                {"explicit native", "nativeを明示"},
		"explicit-native-unavailable":    {"explicit native unavailable", "指定nativeが利用不可"},
		"explicit-mise":                  {"explicit mise", "miseを明示"},
		"explicit-mise-unavailable":      {"explicit mise unavailable", "指定miseが利用不可"},
		"brewfile-native-authority":      {"Brewfile native authority", "Brewfileをnative適用"},
		"macos-x64-native-homebrew":      {"macOS x64 native", "macOS x64はnative"},
		"mise-manager-available":         {"mise manager available", "mise manager利用可"},
		"native-provider-fallback":       {"native fallback", "nativeへfallback"},
		"native-provider-unavailable":    {"native unavailable", "native利用不可"},
		"unsupported-executor":           {"no supported executor", "対応executorなし"},
	}
	if label, ok := labels[code]; ok {
		return i18n.Pick(lang, label[0], label[1])
	}
	return code
}

func ReasonText(item Item, lang string) string {
	if lang != "ja" {
		return item.Reason
	}
	switch item.ReasonCode {
	case "intentional-duplicate-required":
		return "Brewfile と mise の両方で active です。移行中の意図的な重複として metadata に明記するまで executor を選択しません"
	case "explicit-native":
		return "package metadata で native executor が指定され、現在の provider capability で利用できます"
	case "explicit-native-unavailable":
		return "package metadata は native executor を指定していますが、現在の native provider は利用できません"
	case "explicit-mise":
		return "package metadata で mise executor が指定され、現在の manager capability で利用できます"
	case "explicit-mise-unavailable":
		return "package metadata は mise executor を指定していますが、mise desired または manager capability がありません"
	case "brewfile-native-authority":
		return "Brewfile だけが desired source のため、source を増やさず item-scoped native provider を選択します"
	case "macos-x64-native-homebrew":
		return "macOS x64 の Homebrew formula/cask/tap は item-scoped native provider を選択します"
	case "mise-manager-available":
		return "mise desired state と現在 platform の manager capability を確認できました"
	case "native-provider-fallback":
		return "mise manager は利用できませんが、明示的な native provider adapter を利用できます"
	case "native-provider-unavailable":
		return "必要な native provider adapter または executable を利用できません"
	default:
		return "対応する mise manager または native provider adapter がありません"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
