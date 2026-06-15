package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"

	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type updevChoice struct {
	Value       string
	Label       string
	Description string
	Selected    bool
}

const (
	updevActionBack = "back"
	updevActionHome = "home"
	updevActionExit = "exit"
)

type (
	textInputBrowserModel = reviewui.TextInputModel
	confirmBrowserModel   = reviewui.ConfirmModel
)

type (
	detailBrowserRow    = reviewui.DetailRow
	detailBrowserAction = reviewui.DetailAction
	detailBrowserState  = reviewui.State
	detailBrowserModel  = reviewui.DetailBrowserModel
)

const (
	browserMouseOff   = reviewui.MouseOff
	browserMouseWheel = reviewui.MouseWheel
	browserMouseClick = reviewui.MouseClick
)

func newTextInputBrowserModel(title string, description string, placeholder string, defaultValue string, color bool) textInputBrowserModel {
	return reviewui.NewTextInputModel(reviewui.TextInputOptions{
		Title:       title,
		Description: description,
		Placeholder: placeholder,
		Default:     defaultValue,
		Actions: reviewui.TextInputActions{
			Submit: "submit",
			Back:   updevActionBack,
			Exit:   updevActionExit,
		},
		Labels: reviewui.TextInputLabels{
			Controls: tr("Enter submit, Backspace edit, Ctrl+u clear, b/Esc Back, q Exit", "Enter で確定、Backspace で編集、Ctrl+u でクリア、b/Esc で戻る、q で終了"),
			Input:    tr("query:", "query:"),
		},
		Color: color,
	})
}

func newConfirmBrowserModel(title string, prompt string, description string, color bool) confirmBrowserModel {
	return reviewui.NewConfirmModel(title, prompt, description, reviewui.ConfirmActions{
		Apply: "apply",
		Back:  updevActionBack,
		Exit:  updevActionExit,
	}, reviewui.ConfirmLabels{
		Controls: tr("Enter/a apply, b/Esc Back, q Exit", "Enter/a で適用、b/Esc で戻る、q で終了"),
		Warning:  tr("This writes local state only after you confirm.", "確認後に local state を書き込みます。"),
	}, color)
}

func runDetailBrowser(title string, rows []detailBrowserRow, color bool) (detailBrowserState, error) {
	return runDetailBrowserWithState(title, rows, detailBrowserState{}, color)
}

func runDetailBrowserWithState(title string, rows []detailBrowserRow, state detailBrowserState, color bool) (detailBrowserState, error) {
	return reviewui.RunDetailBrowserModel(newDetailBrowserModel(title, rows, state, color))
}

func runPrimaryActionBrowserWithState(title string, rows []detailBrowserRow, state detailBrowserState, color bool) (detailBrowserState, error) {
	model := newDetailBrowserModel(title, rows, state, color)
	model.PrimaryEnterAction = true
	return reviewui.RunDetailBrowserModel(model)
}

func newDetailBrowserModel(title string, rows []detailBrowserRow, state detailBrowserState, color bool) detailBrowserModel {
	return reviewui.NewDetailBrowserModel(reviewui.DetailBrowserOptions{
		Title:   title,
		Rows:    rows,
		State:   state,
		Labels:  detailBrowserLabelsForLocale(),
		Format:  detailBrowserFormattersForLocale(),
		Actions: detailBrowserActions(),
		Color:   color,
	})
}

func detailBrowserActions() reviewui.BrowserActions {
	return reviewui.BrowserActions{Back: updevActionBack, Home: updevActionHome, Exit: updevActionExit}
}

func detailBrowserLabelsForLocale() reviewui.DetailBrowserLabels {
	return reviewui.DetailBrowserLabels{
		Keyboard: tr(
			"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
			"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、a/1-9 action、/ filter、m mouse、x 解除、? help、b 戻る、q 終了",
		),
		PrimaryKeyboard: tr(
			"Up/Down/j/k move, PgUp/PgDn scroll, Enter action, Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b Back, q Exit",
			"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter 操作、Space 展開、a/1-9 action、/ filter、m mouse、x 解除、? help、b 戻る、q 終了",
		),
		Filter:               tr("filter:", "フィルター:"),
		NoRows:               tr("no detail rows", "該当する詳細行はありません"),
		FocusedActionsPrefix: tr("focused actions: ", "選択中の操作: "),
		HelpLines: []string{
			tr("Up/Down or j/k: move the focused row", "↑↓ または j/k: 行を移動"),
			tr("PgUp/PgDn or mouse wheel: scroll by a page", "PgUp/PgDn または mouse wheel: ページ単位でスクロール"),
			tr("Enter or Space: expand/collapse details", "Enter / Space: 詳細を展開/折りたたみ"),
			tr("a: run the first action on the focused row", "a: focus 行の最初の action を実行"),
			tr("1-9: run the numbered action shown in expanded details", "1-9: 展開詳細に表示された番号の action を実行"),
			tr("/: filter within the current view", "/: 現在の view 内を filter"),
			tr("m: cycle mouse off/wheel/click; off is best for selecting terminal text", "m: mouse off/wheel/click を切替。端末で文字選択したい時は off"),
			tr("x: clear the current in-view filter", "x: view 内 filter を解除"),
			tr("b, Left, or Backspace: go back; clears an in-view filter first", "b / ← / Backspace: 戻る。view 内 filter があれば先に解除"),
			tr("h: return to the top hub", "h: top hub に戻る"),
			tr("q, Esc, or Ctrl-C: exit the selector", "q / Esc / Ctrl-C: selector を終了"),
			tr("?, Esc, q, b, Enter, or Space: close this help", "? / Esc / q / b / Enter / Space: この help を閉じる"),
		},
		PrimaryEnterHelp:   tr("Enter: run the first action on the focused row", "Enter: focus 行の最初の action を実行"),
		PrimarySpaceHelp:   tr("Space: expand/collapse details", "Space: 詳細を展開/折りたたみ"),
		DetailsHeading:     tr("details", "詳細"),
		EvidenceHeading:    tr("evidence", "根拠"),
		ActionsHeading:     tr("actions", "操作"),
		NoAdditionalDetail: tr("no additional detail", "追加の詳細はありません"),
		ActionsBadge:       tr("actions", "操作"),
		PositionZeroFilter: func(total int, query string) string {
			return fmt.Sprintf(tr("0/%d rows, filter=%q", "0/%d 行、filter=%q"), total, query)
		},
		PositionZero: tr("0 rows", "0 行"),
		PositionFilter: func(selected int, count int, offset int, query string) string {
			return fmt.Sprintf(tr("row %d/%d, offset %d, filter=%q", "%d/%d 行、offset %d、filter=%q"), selected, count, offset, query)
		},
		Position: func(selected int, count int, offset int) string {
			return fmt.Sprintf(tr("row %d/%d, offset %d", "%d/%d 行、offset %d"), selected, count, offset)
		},
	}
}

func detailBrowserFormattersForLocale() reviewui.DetailBrowserFormatters {
	return reviewui.DetailBrowserFormatters{
		Truncate:         textTruncate,
		OneLine:          oneLine,
		SectionHeading:   browserSectionHeadingText,
		LocalizeEvidence: localizedListEvidenceText,
	}
}

func interactiveBrowserHelpLines() []string {
	return detailBrowserLabelsForLocale().HelpLines
}

func detailBrowserExpandedLines(row detailBrowserRow) []string {
	return reviewui.DetailBrowserExpandedLines(row)
}

func detailBrowserExpandedLinesWithWidth(row detailBrowserRow, width int) []string {
	return reviewui.DetailBrowserExpandedLinesWithWidth(row, width)
}

func detailBrowserExpandedLinesStyled(row detailBrowserRow, width int, color bool) []string {
	return reviewui.DetailBrowserExpandedLinesStyled(row, width, color)
}

func detailBrowserExpandedLinesStyledFocus(row detailBrowserRow, width int, color bool, actionFocus int) []string {
	return reviewui.DetailBrowserExpandedLinesStyledFocus(row, width, color, actionFocus)
}

func detailBrowserDetailLines(detail string, width int, color bool) []string {
	return reviewui.DetailBrowserDetailLines(detail, width, color)
}

func detailBrowserCollapsedSummary(row detailBrowserRow) string {
	return reviewui.DetailBrowserCollapsedSummary(row)
}

func detailBrowserActionKeyIndex(key string) (int, bool) {
	return reviewui.DetailBrowserActionKeyIndex(key)
}

type (
	toolTableBrowserModel = reviewui.TableBrowserModel
	toolTableMouseMsg     = reviewui.TableMouseMsg
	toolTableWheelMsg     = reviewui.TableWheelMsg
)

func runToolTableBrowserWithState(title string, sections []toolSection, state detailBrowserState, color bool) (detailBrowserState, error) {
	return reviewui.RunTableBrowserWithState(title, sections, state, tableBrowserLabels(), tableBrowserActions(), color)
}

func runToolTableBrowserWithStateAndActions(title string, sections []toolSection, state detailBrowserState, actions reviewui.BrowserActions, labels reviewui.TableBrowserLabels, color bool) (detailBrowserState, error) {
	return reviewui.RunTableBrowserWithState(title, sections, state, labels, actions, color)
}

func newToolTableBrowserModel(title string, sections []toolSection, state detailBrowserState, color bool) toolTableBrowserModel {
	return reviewui.NewTableBrowserModel(title, sections, state, tableBrowserLabels(), tableBrowserActions(), color)
}

func newToolTableBrowserModelWithActions(title string, sections []toolSection, state detailBrowserState, actions reviewui.BrowserActions, labels reviewui.TableBrowserLabels, color bool) toolTableBrowserModel {
	return reviewui.NewTableBrowserModel(title, sections, state, labels, actions, color)
}

func filteredToolSections(sections []toolSection, rawQuery string) []toolSection {
	return reviewui.FilteredSections(sections, rawQuery)
}

func toolTableRowMatches(section toolSection, row toolRow, query string) bool {
	return reviewui.RowMatches(section, row, query)
}

func toolTableRowCount(sections []toolSection) int {
	return reviewui.RowCount(sections)
}

func toolSectionHasWanted(section toolSection) bool {
	return reviewui.HasWanted(section)
}

func toolTableColumns(includeWanted bool) []textui.Column {
	return reviewui.Columns(includeWanted, reviewLabels())
}

func toolTableStyledRows(rows []toolRow, includeWanted bool, color bool) [][]string {
	return reviewui.StyledRows(rows, includeWanted, color)
}

func toolTableHeader(columns []textui.Column, widths []int, color bool) string {
	return reviewui.Header(columns, widths, color)
}

func toolTableRow(row []string, widths []int) string {
	return reviewui.TableRow(row, widths)
}

func toolTableExpandedLines(row toolRow) []string {
	return reviewui.ExpandedLines(row, reviewLabels())
}

func toolTableVisibleRows(sections []toolSection, offset int, maxBodyLines int, expanded map[int]bool) map[int]bool {
	return reviewui.TableVisibleRows(sections, offset, maxBodyLines, expanded, reviewLabels())
}

func tableBrowserActions() reviewui.BrowserActions {
	return reviewui.BrowserActions{Back: updevActionBack, Home: updevActionHome, Exit: updevActionExit}
}

func tableBrowserActionsWithViewToggle(next string, previous string) reviewui.BrowserActions {
	actions := tableBrowserActions()
	actions.Next = next
	actions.Previous = previous
	return actions
}

func tableBrowserLabels() reviewui.TableBrowserLabels {
	return reviewui.TableBrowserLabels{
		Labels: reviewLabels(),
		ControlsHelp: tr(
			"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, m mouse, x clear, ? help, b/Backspace Back, q Exit",
			"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、a/1-9 action、/ filter、m mouse、x 解除、? help、b/Backspace 戻る、q 終了",
		),
		NoRows:      tr("no matching rows", "該当する行はありません"),
		FilterLabel: tr("filter:", "フィルター:"),
		HelpLines:   interactiveBrowserHelpLines(),
		PositionZeroFilter: func(total int, query string) string {
			return fmt.Sprintf(tr("0/%d rows, filter=%q", "0/%d 行、filter=%q"), total, query)
		},
		PositionZero: tr("0 rows", "0 行"),
		PositionFilter: func(selected int, count int, offset int, query string) string {
			return fmt.Sprintf(tr("row %d/%d, offset %d, filter=%q", "%d/%d 行、offset %d、filter=%q"), selected, count, offset, query)
		},
		Position: func(selected int, count int, offset int) string {
			return fmt.Sprintf(tr("row %d/%d, offset %d", "%d/%d 行、offset %d"), selected, count, offset)
		},
	}
}

func tableBrowserLabelsWithViewToggle() reviewui.TableBrowserLabels {
	labels := tableBrowserLabels()
	labels.ControlsHelp = tr(
		"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, a/1-9 action, / filter, Tab switch view, m mouse, x clear, ? help, b/Backspace Back, q Exit",
		"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、a/1-9 action、/ filter、Tab 表示切替、m mouse、x 解除、? help、b/Backspace 戻る、q 終了",
	)
	labels.HelpLines = append(labels.HelpLines, tr(
		"Tab / Shift+Tab: switch between installed inventory and manual apps.",
		"Tab / Shift+Tab: インストール済み一覧と手動管理アプリを切り替えます。",
	))
	return labels
}

const (
	updevSelectMinHeight     = 6
	updevSelectMaxHeight     = 16
	updevChoiceLabelMaxWidth = 34
	updevPostActionMinHeight = 4
	updevPostActionMaxHeight = 6
	updevInteractiveEnv      = "UPDEV_TUI"
	updevInteractiveDisable  = "0"
)

func shouldRunUpdevInteractive(input io.Reader, output io.Writer, format string, force bool, disabled bool) bool {
	if disabled || os.Getenv(updevInteractiveEnv) == updevInteractiveDisable {
		return false
	}
	if format != "" && format != "text" {
		return false
	}
	if !isTerminal(input) || !isTerminal(output) {
		return false
	}
	if force {
		return true
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if configured := loadUpdevConfig().UI.Interactive; configured != nil {
		switch *configured {
		case "off":
			return false
		case "on":
			return true
		}
	}
	return true
}

func warnInteractiveUnavailable(input io.Reader, output io.Writer, format string, force bool, disabled bool) {
	if !force || disabled || os.Getenv(updevInteractiveEnv) == updevInteractiveDisable {
		return
	}
	if format != "" && format != "text" {
		return
	}
	if isTerminal(input) && isTerminal(output) {
		return
	}
	fmt.Fprintln(os.Stderr, "updev: --interactive requires a TTY; falling back to text output")
}

func isTerminal(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runUpdevSelect(title string, description string, choices []updevChoice, defaultValue string) (string, error) {
	return runUpdevSelectWithHeight(title, description, choices, defaultValue, updevSelectMinHeight, updevSelectMaxHeight)
}

func runUpdevSelectWithHeight(title string, description string, choices []updevChoice, defaultValue string, minHeight int, maxHeight int) (string, error) {
	options := make([]huh.Option[string], 0, len(choices))
	labelWidth := updevChoiceLabelWidth(choices)
	for _, choice := range choices {
		options = append(options, huh.NewOption(updevChoiceDisplayLabel(choice, labelWidth), choice.Value))
		if defaultValue == "" && choice.Selected {
			defaultValue = choice.Value
		}
	}
	if defaultValue == "" && len(choices) > 0 {
		defaultValue = choices[0].Value
	}
	selected := defaultValue
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description(description).
			Options(options...).
			Height(updevSelectHeight(len(options), minHeight, maxHeight)).
			Value(&selected),
	)).Run()
	if err != nil {
		return "", err
	}
	return selected, nil
}

func runUpdevInput(title string, description string, placeholder string, defaultValue string) (string, error) {
	value := defaultValue
	err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(title).
			Description(description).
			Placeholder(placeholder).
			Value(&value),
	)).Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func updevChoiceLabelWidth(choices []updevChoice) int {
	width := 0
	for _, choice := range choices {
		if choice.Description == "" {
			continue
		}
		if candidate := textui.DisplayWidth(choice.Label); candidate > width {
			width = candidate
		}
	}
	if width > updevChoiceLabelMaxWidth {
		return updevChoiceLabelMaxWidth
	}
	return width
}

func updevChoiceDisplayLabel(choice updevChoice, labelWidth int) string {
	if choice.Description == "" {
		return choice.Label
	}
	label := choice.Label
	if labelWidth > 0 {
		label = textPadRight(textTruncate(label, labelWidth), labelWidth)
	}
	return label + "  " + choice.Description
}

func updevSelectHeight(optionCount int, minHeight int, maxHeight int) int {
	if minHeight < 1 {
		minHeight = 1
	}
	if maxHeight < minHeight {
		maxHeight = minHeight
	}
	height := optionCount
	if height < minHeight {
		height = minHeight
	}
	if height > maxHeight {
		height = maxHeight
	}
	return height
}

func textPadRight(value string, width int) string {
	return textui.PadRight(value, width)
}

func textTruncate(value string, width int) string {
	return textui.Truncate(value, width)
}

func runPostSectionNavigation() (string, error) {
	return runUpdevSelectWithHeight("updev", "Choose where to go next.", []updevChoice{
		{Value: updevActionBack, Label: "Back", Description: "Return to the previous selector.", Selected: true},
		{Value: updevActionHome, Label: "Home", Description: "Return to the top updev hub."},
		{Value: updevActionExit, Label: "Exit", Description: "Leave the selector."},
	}, updevActionBack, updevPostActionMinHeight, updevPostActionMaxHeight)
}
