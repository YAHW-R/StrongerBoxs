package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/ui/components"
)

// viewBoard dibuja el tablero estilo Keep con masonry simulado,
// barra de comandos y overlay de ayuda.
func (m Model) viewBoard() string {
	var middle string

	switch {
	case m.showHelp:
		middle = m.helpOverlay()
	default:
		middle = m.boardGrid()
	}

	status := m.statusLine()
	if m.cmdFocus {
		status = m.cmdLine.View()
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		boardTitleStyle.Render("Notas"),
		middle,
		"",
		status,
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) boardGrid() string {
	cards := toCards(m.entities)
	if len(cards) == 0 {
		hint := lipgloss.NewStyle().
			Faint(true).
			Padding(1, 3).
			Render(strings.Join([]string{
				"Sin notas.",
				":new <título>  ·  crea la primera",
			}, "\n"))
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorViolet).
			Render(hint)
		return box
	}

	cardViews := make([]string, len(cards))
	for i, n := range cards {
		selected := i == m.selIdx && !m.ed.open
		cardViews[i] = components.RenderCard(n, selected)
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

var helpBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorTeal).
	Padding(1, 3)

func (m Model) helpOverlay() string {
	return helpBoxStyle.Render(helpText)
}
