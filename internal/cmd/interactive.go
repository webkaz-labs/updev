package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
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
