package cmd

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type confirmBrowserModel struct {
	Title       string
	Prompt      string
	Description string
	Action      string
	Color       bool
}

func newConfirmBrowserModel(title string, prompt string, description string, color bool) confirmBrowserModel {
	return confirmBrowserModel{
		Title:       title,
		Prompt:      prompt,
		Description: description,
		Color:       color,
	}
}

func (m confirmBrowserModel) Init() tea.Cmd {
	return nil
}

func (m confirmBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter", "a", "1":
			m.Action = "apply"
			return m, tea.Quit
		case "esc", "b", "left", "backspace":
			m.Action = updevActionBack
			return m, tea.Quit
		case "q", "ctrl+c":
			m.Action = updevActionExit
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmBrowserModel) View() tea.View {
	var out strings.Builder
	out.WriteString(textui.StyleHeading(m.Title, m.Color))
	out.WriteString("\n")
	out.WriteString(textui.StyleDim(tr("Enter/a apply, b/Esc Back, q Exit", "Enter/a で適用、b/Esc で戻る、q で終了"), m.Color))
	out.WriteString("\n\n")
	if m.Prompt != "" {
		out.WriteString(textui.StyleLabel(m.Prompt, m.Color))
		out.WriteString("\n\n")
	}
	if m.Description != "" {
		out.WriteString(m.Description)
		out.WriteString("\n\n")
	}
	out.WriteString(textui.StyleWarning(tr("This writes local state only after you confirm.", "確認後に local state を書き込みます。"), m.Color))
	out.WriteString("\n")
	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}
