package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/ui/components"
)

// secretCard convierte una entrada de la bóveda en tarjeta según SU
// plantilla. Los valores sensibles NUNCA se muestran sin reveal explícito.
func (m Model) secretCard(s store.Secret, reveal bool) components.Note {
	tplName := s.Template
	if tplName == "" {
		tplName = defaultTemplate
	}
	tpl, ok := m.findTemplate(tplName)
	if !ok {
		tpl, _ = m.findTemplate(defaultTemplate)
	}
	extra := m.extraBy[s.UUID]

	titleLine := s.Title
	if tpl.Icon != "" {
		titleLine = tpl.Icon + " " + titleLine
	}

	var lines []string
	for _, f := range tpl.Fields {
		var val string
		switch f.Key {
		case "username":
			val = s.Username
		case "password":
			val = s.Password
		case "url":
			val = s.URL
		case "notes":
			continue // las notas no van en la tarjeta
		default:
			val = extra[f.Key]
		}
		if val == "" {
			continue
		}
		shown := val
		if f.Sensitive && !reveal {
			shown = maskText
		}
		lines = append(lines, fmt.Sprintf("%s %s", f.Label, shown))
	}
	if len(lines) == 0 {
		lines = append(lines, helpStyle.Render("(vacía)"))
	}
	// Chip de plantilla: deja claro de qué tipo es la entrada.
	lines = append(lines, helpStyle.Render("‹"+tpl.Name+"›"))

	return components.Note{
		Title: titleLine,
		Body:  strings.Join(lines, "\n"),
		Color: colorTeal,
	}
}

// viewBoard dibuja el tablero activo (notas o vault) con masonry,
// búsqueda, barra de comandos y overlay de ayuda.
func (m Model) viewBoard() string {
	var middle string

	switch {
	case m.showHelp:
		middle = m.helpOverlay()
	default:
		middle = m.boardGrid()
	}

	status := m.statusLine()
	switch {
	case m.searchFocus:
		status = m.searchLn.View()
	case m.cmdFocus:
		status = m.cmdLine.View()
	}

	lines := []string{
		boardTitleStyle.Render("Notas"),
		middle,
	}
	if m.notice != "" {
		lines = append(lines, "", okStyle.Render(m.notice))
	} else if m.errMsg != "" {
		lines = append(lines, "", errStyle.Render("✗ "+m.errMsg))
	}
	lines = append(lines, "", status)

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) boardGrid() string {
	var cardViews []string
	selected := func(i int) bool { return i == m.selIdx && !m.ed.open }

	switch m.board {
	case secSecrets:
		vs := m.visibleSecrets()
		for i, s := range vs {
			cardViews = append(cardViews, components.RenderCard(m.secretCard(s, m.revealAll), selected(i)))
		}
		if len(vs) == 0 {
			return emptyHint("Sin entradas en la bóveda.", ":new <título>  ·  añade tu primera contraseña")
		}
	default:
		vn := m.visibleNotes()
		cards := toCards(vn)
		for i, c := range cards {
			cardViews = append(cardViews, components.RenderCard(c, selected(i)))
		}
		if len(cards) == 0 {
			return emptyHint("Sin notas.", ":new <título>  ·  crea la primera")
		}
	}

	// Columnas responsive al ancho de la terminal (~44 cols por tarjeta).
	cols := m.width / 44
	if cols < 1 {
		cols = 1
	}
	if cols > len(cardViews) {
		cols = len(cardViews)
	}

	columns := components.Distribute(cardViews, cols)
	colViews := make([]string, 0, len(columns))
	for _, col := range columns {
		colViews = append(colViews, lipgloss.JoinVertical(lipgloss.Left, col...))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, colViews...)
}

var (
	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorTeal).
			Padding(1, 3)

	emptyBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorViolet).
			Padding(1, 3)
)

func emptyHint(line1, line2 string) string {
	hint := lipgloss.NewStyle().
		Faint(true).
		Padding(0, 1).
		Render(strings.Join([]string{line1, line2}, "\n"))
	return emptyBoxStyle.Render(hint)
}

func (m Model) helpOverlay() string {
	return helpBoxStyle.Render(helpText)
}
