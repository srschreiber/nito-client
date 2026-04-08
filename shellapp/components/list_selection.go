// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

type Component interface {
	SetFocused(focused bool)
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	Render() string
}

type ListSelectionComponent struct {
	Title               string
	Choices             []string
	Selected            map[int]struct{}
	FocusedElementIndex int
	focused             bool
}

func NewListSelectionComponent(title string, choices []string) *ListSelectionComponent {
	return &ListSelectionComponent{
		Title:    title,
		Choices:  choices,
		Selected: make(map[int]struct{}),
	}
}

func (l *ListSelectionComponent) Init() tea.Cmd {
	return nil
}

func (l *ListSelectionComponent) SetFocused(focused bool) {
	l.focused = focused
}

func (l *ListSelectionComponent) toggle(row int) {
	if _, ok := l.Selected[row]; ok {
		delete(l.Selected, row)
	} else {
		l.Selected[row] = struct{}{}
	}
}

func (l *ListSelectionComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if l.FocusedElementIndex > 0 {
				l.FocusedElementIndex--
			}
		case "down", "j":
			if l.FocusedElementIndex < len(l.Choices)-1 {
				l.FocusedElementIndex++
			}
		case "enter", "space":
			l.toggle(l.FocusedElementIndex)
		}
	}
	return nil
}

func (l *ListSelectionComponent) Render() string {
	s := styles.TitleStyle.Render(l.Title) + "\n"
	for i, choice := range l.Choices {
		cursor := " "
		if l.FocusedElementIndex == i {
			cursor = styles.CursorStyle.Render("›")
		}

		checked := " "
		if _, ok := l.Selected[i]; ok {
			checked = styles.SelectedStyle.Render("✓")
			choice = styles.SelectedStyle.Render(choice)
		}

		row := fmt.Sprintf("%s [%s] %s", cursor, checked, choice)
		rendered := styles.ItemStyle.Render(row)
		if l.FocusedElementIndex == i {
			rendered = styles.SelectionRowStyle.Render(rendered)
		}
		s += rendered + "\n"
	}

	if l.focused {
		return styles.FocusedBorderStyle.Render(s)
	}
	return styles.UnfocusedBorderStyle.Render(s)
}
