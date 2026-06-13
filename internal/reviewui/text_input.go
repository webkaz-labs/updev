package reviewui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type TextInputActions struct {
	Submit string
	Back   string
	Exit   string
}

type TextInputLabels struct {
	Controls string
	Input    string
}

type TextInputModel struct {
	Title       string
	Description string
	Placeholder string
	Label       string
	Value       string
	Action      string
	Color       bool
	actions     TextInputActions
	labels      TextInputLabels
}

func NewTextInputModel(title string, description string, placeholder string, defaultValue string, actions TextInputActions, labels TextInputLabels, color bool) TextInputModel {
	filledLabels := fillTextInputLabels(labels)
	return TextInputModel{
		Title:       title,
		Description: description,
		Placeholder: placeholder,
		Label:       filledLabels.Input,
		Value:       defaultValue,
		Color:       color,
		actions:     fillTextInputActions(actions),
		labels:      filledLabels,
	}
}

func (m TextInputModel) Init() tea.Cmd {
	return nil
}

func (m TextInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			m.Action = m.actions.Submit
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
			m.Action = m.actions.Back
			return m, tea.Quit
		case "q", "ctrl+c":
			m.Action = m.actions.Exit
			return m, tea.Quit
		default:
			if text := msg.Key().Text; text != "" {
				m.Value += text
			}
		}
	}
	return m, nil
}

func (m TextInputModel) View() tea.View {
	var out strings.Builder
	out.WriteString(textui.StyleHeading(m.Title, m.Color))
	out.WriteString("\n")
	out.WriteString(textui.StyleDim(m.labels.Controls, m.Color))
	out.WriteString("\n\n")
	if m.Description != "" {
		out.WriteString(m.Description)
		out.WriteString("\n\n")
	}
	labelText := m.Label
	if strings.TrimSpace(labelText) == "" {
		labelText = m.labels.Input
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

func fillTextInputActions(actions TextInputActions) TextInputActions {
	if actions.Submit == "" {
		actions.Submit = "submit"
	}
	if actions.Back == "" {
		actions.Back = "back"
	}
	if actions.Exit == "" {
		actions.Exit = "exit"
	}
	return actions
}

func fillTextInputLabels(labels TextInputLabels) TextInputLabels {
	if labels.Controls == "" {
		labels.Controls = "Enter submit, Backspace edit, Ctrl+u clear, b/Esc Back, q Exit"
	}
	if labels.Input == "" {
		labels.Input = "input:"
	}
	return labels
}
