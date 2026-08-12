package packageparity

import (
	"fmt"
	"io"
	"strconv"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/textui"
)

func PrintText(w io.Writer, report Report, lang string, color bool) {
	tr := func(en string, ja string) string { return i18n.Pick(lang, en, ja) }
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev doctor package-parity", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("root:", "ルート:"), color), report.Root)
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("Brewfile:", color), report.BrewfilePath)
	fmt.Fprintf(w, "%s %d\n", textui.StyleLabel(tr("mise sources:", "mise source:"), color), len(report.MiseSources))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel(tr("summary:", "サマリー:"), color), fmt.Sprintf(
		tr("matched %d, Brewfile-only %d, mise-only %d", "一致 %d件, Brewfileのみ %d件, miseのみ %d件"),
		report.Summary.Matched,
		report.Summary.BrewfileOnly,
		report.Summary.MiseOnly,
	))

	rows := make([][]string, 0, len(report.Items))
	for _, item := range report.Items {
		note := item.RequestedVersion
		if item.ManagerAvailable != nil && !*item.ManagerAvailable {
			note = tr("manager unavailable", "manager利用不可")
			if item.UnavailableReason != "" {
				note += ": " + item.UnavailableReason
			}
		}
		rows = append(rows, []string{
			textui.StyleStatusText(parityLabel(item.Parity, lang), parityStatus(item.Parity), color),
			item.Kind,
			item.Name,
			strconv.FormatBool(item.BrewfileDesired),
			strconv.FormatBool(item.MiseDesired),
			note,
		})
	}
	fmt.Fprintln(w)
	textui.PrintTable(w, []textui.Column{
		{Header: tr("parity", "差分"), Min: 8, Max: 14},
		{Header: tr("kind", "種別"), Min: 7, Max: 9},
		{Header: tr("name", "名前"), Min: 12, Max: 32},
		{Header: "Brewfile", Min: 8, Max: 8},
		{Header: "mise", Min: 5, Max: 5},
		{Header: tr("detail", "詳細"), Min: 12, Max: 48},
	}, rows, color)
}

func parityLabel(value string, lang string) string {
	switch value {
	case ParityMatch:
		return i18n.Pick(lang, "match", "一致")
	case ParityBrewfileOnly:
		return i18n.Pick(lang, "Brewfile-only", "Brewfileのみ")
	case ParityMiseOnly:
		return i18n.Pick(lang, "mise-only", "miseのみ")
	default:
		return value
	}
}

func parityStatus(value string) string {
	if value == ParityMatch {
		return "ok"
	}
	return "drift"
}
