package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewEditor es el modal de notas, entradas de vault y plantillas (:newp).
func (m Model) viewEditor() string {
	var lines []string
	hint := "tab · campo    ctrl+s · guardar    : comandos    esc · cancelar"

	switch {
	case m.ed.building:
		lines = m.builderLines()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorTeal).
				Padding(1, 3).
				Render(strings.Join(lines, "\n")))
	case m.ed.sec == secSecrets:
		lines = m.secretEditorLines()
		revealState := "oculta"
		if m.ed.reveal {
			revealState = "visible"
		}
		hint = "tab · campo    ctrl+s · guardar    : comandos    esc · cancelar    ctrl+r · revelar (" + revealState + ")"
	default:
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

	borderColor := colorTeal
	if m.ed.sec == secNotes && !m.ed.building {
		borderColor = colorTeal
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 3)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		box.Render(strings.Join(lines, "\n")))
}

// builderLines: formulario del constructor de plantillas (:newp).
func (m Model) builderLines() []string {
	b := m.ed.builder

	nameMark := " "
	if m.ed.field == 0 {
		nameMark = "▸"
	}
	lines := []string{
		appTitleStyle.Render("🧩 NUEVA PLANTILLA"),
		"",
		nameMark + " Nombre interno (para :new)",
		b.name.View(),
		"",
	}

	for i, r := range b.rows {
		labelIdx := 1 + i*2
		typeIdx := labelIdx + 1
		active := m.ed.field == labelIdx || m.ed.field == typeIdx

		rowMark := " "
		if active {
			rowMark = "▸"
		}

		typ := rowTypeName(r)
		if m.ed.field == typeIdx {
			typ = selectedStyle.Render("‹ " + typ + " ›")
		} else {
			typ = helpStyle.Render("[" + typ + "]")
		}

		lbl := r.label.View()
		if strings.TrimSpace(r.label.Value()) == "" && !active {
			lbl = helpStyle.Render("(etiqueta)")
		}
		lines = append(lines, fmt.Sprintf("%s %s   %s", rowMark, lbl, typ))
	}

	lines = append(lines,
		"",
		helpStyle.Render("ctrl+n · añadir    ctrl+d · borrar fila    ←/→ · tipo    ctrl+s · crear    esc · cancelar"),
	)
	return lines
}

// secretEditorLines: campos dinámicos según la plantilla.
func (m Model) secretEditorLines() []string {
	tpl, ok := m.findTemplate(m.ed.tplName)
	icon, title := "🔐", "VAULT"
	if ok {
		icon, title = tpl.Icon, strings.ToUpper(tpl.Title)
	}

	lines := []string{appTitleStyle.Render(icon + " " + title), ""}

	// Índice 0 del editor = TÍTULO (input enfocable y enrutable).
	titleMark := " "
	if m.ed.field == 0 {
		titleMark = "▸"
	}
	lines = append(lines, titleMark+" Título", m.ed.title.View(), "")

	for i, f := range m.ed.secFields {
		mark := " "
		if i+1 == m.ed.field {
			mark = "▸"
		}
		lines = append(lines,
			fmt.Sprintf("%s %s", mark, f.def.Label),
			m.fieldWidgetView(i),
			"")
	}
	return lines[:len(lines)-1]
}

// fieldWidgetView: los campos multilínea usan el textarea compartido.
func (m Model) fieldWidgetView(i int) string {
	if i < 0 || i >= len(m.ed.secFields) {
		return ""
	}
	f := m.ed.secFields[i]
	if f.def.Multi {
		return m.ed.body.View()
	}
	return f.input.View()
}
