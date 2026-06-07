package cmd

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type textInputBrowserModel struct {
	Title       string
	Description string
	Placeholder string
	Label       string
	Value       string
	Action      string
	Color       bool
}

func newTextInputBrowserModel(title string, description string, placeholder string, defaultValue string, color bool) textInputBrowserModel {
	return textInputBrowserModel{
		Title:       title,
		Description: description,
		Placeholder: placeholder,
		Label:       tr("query:", "query:"),
		Value:       defaultValue,
		Color:       color,
	}
}

func (m textInputBrowserModel) Init() tea.Cmd {
	return nil
}

func (m textInputBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			m.Action = "submit"
			return m, tea.Quit
		case "ctrl+u":
			m.Value = ""
			return m, nil
		case "esc", "b", "left", "backspace":
			if msg.String() == "backspace" && m.Value != "" {
				runes := []rune(m.Value)
				m.Value = string(runes[:len(runes)-1])
				return m, nil
			}
			m.Action = updevActionBack
			return m, tea.Quit
		case "q", "ctrl+c":
			m.Action = updevActionExit
			return m, tea.Quit
		default:
			if text := msg.Key().Text; text != "" {
				m.Value += text
			}
		}
	}
	return m, nil
}

func (m textInputBrowserModel) View() tea.View {
	var out strings.Builder
	out.WriteString(textui.StyleHeading(m.Title, m.Color))
	out.WriteString("\n")
	out.WriteString(textui.StyleDim(tr("Enter submit, Backspace edit, Ctrl+u clear, b/Esc Back, q Exit", "Enter で確定、Backspace で編集、Ctrl+u でクリア、b/Esc で戻る、q で終了"), m.Color))
	out.WriteString("\n\n")
	if m.Description != "" {
		out.WriteString(m.Description)
		out.WriteString("\n\n")
	}
	labelText := m.Label
	if strings.TrimSpace(labelText) == "" {
		labelText = tr("input:", "input:")
	}
	label := textui.StyleLabel(labelText, m.Color)
	value := m.Value
	if value == "" {
		value = textui.StyleDim(m.Placeholder, m.Color)
	}
	out.WriteString(label)
	out.WriteString(" ")
	out.WriteString(value)
	out.WriteString("\n")
	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}
