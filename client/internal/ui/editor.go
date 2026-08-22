package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewEditor es el modal de creación/edición de notas y secretos.
func (m Model) viewEditor() string {
	var lines []string
	hint := "tab · campo    ctrl+s · guardar    : comandos    esc · cancelar"

	if m.ed.sec == secSecrets {
		marks := make([]string, 5)
		for i := range marks {
			marks[i] = "  "
		}
		marks[m.ed.field] = "▸"
		lines = []string{
			appTitleStyle.Render("🔑 VAULT"),
			"",
			marks[0] + " Título",
			m.ed.title.View(),
			"",
			marks[1] + " Usuario",
			m.ed.user.View(),
			"",
			marks[2] + " Contraseña",
			m.ed.pass.View(),
			"",
			marks[3] + " URL",
			m.ed.url.View(),
			"",
			marks[4] + " Notas",
			m.ed.body.View(),
		}
		revealState := "oculta"
		if m.ed.reveal {
			revealState = "visible"
		}
		hint += "    ctrl+r · revelar (" + revealState + ")"
	} else {
		titleMark, bodyMark := "  ", "  "
		if m.ed.field == 0 {
			titleMark = "▸"
		} else {
			bodyMark = "▸"
		}
		lines = []string{
			appTitleStyle.Render("✎ NOTA"),
			"",
			titleMark + " Título",
			m.ed.title.View(),
			"",
			bodyMark + " Cuerpo",
			m.ed.body.View(),
		}
	}

	if m.ed.cmd {
		lines = append(lines, "", m.ed.cmdLn.View())
	}
	if m.errMsg != "" {
		lines = append(lines, "", errStyle.Render("✗ "+m.errMsg))
	}
	if m.notice != "" {
		lines = append(lines, "", okStyle.Render(m.notice))
	}
	lines = append(lines, "", helpStyle.Render(hint))

	boxColor := colorTeal
	border := lipgloss.RoundedBorder()
	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(boxColor).
		Padding(1, 3)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		box.Render(strings.Join(lines, "\n")))
}
