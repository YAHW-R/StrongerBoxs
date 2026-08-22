package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewEditor es el modal de creación/edición de nota.
func (m Model) viewEditor() string {
	titleMark, bodyMark := "  ", "  "
	if m.ed.field == 0 {
		titleMark = "▸"
	} else {
		bodyMark = "▸"
	}

	lines := []string{
		appTitleStyle.Render("✎ NOTA"),
		"",
		titleMark + " Título",
		m.ed.title.View(),
		"",
		bodyMark + " Cuerpo",
		m.ed.body.View(),
	}
	if m.ed.cmd {
		lines = append(lines, "", m.ed.cmdLn.View())
	}
	if m.errMsg != "" {
		lines = append(lines, "", errStyle.Render("✗ "+m.errMsg))
	}
	lines = append(lines,
		"",
		helpStyle.Render("tab · campo    ctrl+s · guardar    : comandos    esc · cancelar"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorTeal).
		Padding(1, 3)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		box.Render(strings.Join(lines, "\n")))
}
