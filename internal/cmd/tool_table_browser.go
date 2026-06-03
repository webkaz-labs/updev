package cmd

import (
	"fmt"

	"github.com/webkaz-labs/updev/internal/reviewui"
	"github.com/webkaz-labs/updev/internal/textui"
)

type toolTableBrowserModel = reviewui.TableBrowserModel
type toolTableMouseMsg = reviewui.TableMouseMsg
type toolTableWheelMsg = reviewui.TableWheelMsg

func runToolTableBrowserWithState(title string, sections []toolSection, state detailBrowserState, color bool) (detailBrowserState, error) {
	return reviewui.RunTableBrowserWithState(title, sections, state, tableBrowserLabels(), tableBrowserActions(), color)
}

func newToolTableBrowserModel(title string, sections []toolSection, state detailBrowserState, color bool) toolTableBrowserModel {
	return reviewui.NewTableBrowserModel(title, sections, state, tableBrowserLabels(), tableBrowserActions(), color)
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

func tableBrowserLabels() reviewui.TableBrowserLabels {
	return reviewui.TableBrowserLabels{
		Labels: reviewLabels(),
		ControlsHelp: tr(
			"Up/Down/j/k move, PgUp/PgDn scroll, Enter/Space expand, / filter, m mouse, x clear, ? help, b/Backspace Back, q Exit",
			"↑↓/j/k 移動、PgUp/PgDn スクロール、Enter/Space 展開、/ filter、m mouse、x 解除、? help、b/Backspace 戻る、q 終了",
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
