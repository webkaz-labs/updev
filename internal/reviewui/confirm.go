package reviewui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/webkaz-labs/updev/internal/textui"
)

type ConfirmActions struct {
	Apply string
	Back  string
	Exit  string
}

type ConfirmLabels struct {
	Controls string
	Warning  string
}

type ConfirmModel struct {
	Title       string
	Prompt      string
	Description string
	Action      string
	Color       bool
	actions     ConfirmActions
	labels      ConfirmLabels
}

func NewConfirmModel(title string, prompt string, description string, actions ConfirmActions, labels ConfirmLabels, color bool) ConfirmModel {
	return ConfirmModel{
		Title:       title,
		Prompt:      prompt,
		Description: description,
		Color:       color,
		actions:     fillConfirmActions(actions),
		labels:      fillConfirmLabels(labels),
	}
}

func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter", "a", "1":
			m.Action = m.actions.Apply
			return m, tea.Quit
		case "esc", "b", "left", "backspace":
			m.Action = m.actions.Back
			return m, tea.Quit
		case "q", "ctrl+c":
			m.Action = m.actions.Exit
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ConfirmModel) View() tea.View {
	var out strings.Builder
	out.WriteString(textui.StyleHeading(m.Title, m.Color))
	out.WriteString("\n")
	out.WriteString(textui.StyleDim(m.labels.Controls, m.Color))
	out.WriteString("\n\n")
	if m.Prompt != "" {
		out.WriteString(textui.StyleLabel(m.Prompt, m.Color))
		out.WriteString("\n\n")
	}
	if m.Description != "" {
		out.WriteString(m.Description)
		out.WriteString("\n\n")
	}
	out.WriteString(textui.StyleWarning(m.labels.Warning, m.Color))
	out.WriteString("\n")
	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}

func fillConfirmActions(actions ConfirmActions) ConfirmActions {
	if actions.Apply == "" {
		actions.Apply = "apply"
	}
	if actions.Back == "" {
		actions.Back = "back"
	}
	if actions.Exit == "" {
		actions.Exit = "exit"
	}
	return actions
}

func fillConfirmLabels(labels ConfirmLabels) ConfirmLabels {
	if labels.Controls == "" {
		labels.Controls = "Enter/a apply, b/Esc Back, q Exit"
	}
	if labels.Warning == "" {
		labels.Warning = "This writes local state only after you confirm."
	}
	return labels
}
