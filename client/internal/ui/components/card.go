package components

import (
	"github.com/charmbracelet/lipgloss"
)

// Note es la unidad visual básica del tablero (equivalente a una nota de Keep).
type Note struct {
	Title string
	Body  string
	Color lipgloss.Color
}

// RenderCard dibuja una nota con borde redondeado y color propio.
// La tarjeta seleccionada usa borde grueso para resaltarla.
func RenderCard(n Note, selected bool) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(n.Color).
		Render(n.Title)

	border := lipgloss.RoundedBorder()
	if selected {
		border = lipgloss.DoubleBorder()
	}

	card := lipgloss.NewStyle().
		Border(border).
		BorderForeground(n.Color).
		Padding(1, 2)

	return card.Render(title, "", n.Body)
}

// Distribute reparte tarjetas en columnas simulando masonry:
// cada tarjeta va a la columna con menor altura acumulada (aproximada por nº de líneas).
func Distribute(cards []string, cols int) [][]string {
	if cols < 1 {
		cols = 1
	}
	columns := make([][]string, cols)
	heights := make([]int, cols)
	for _, c := range cards {
		h := lipgloss.Height(c)
		min := 0
		for i, hh := range heights {
			if hh < heights[min] {
				min = i
			}
		}
		columns[min] = append(columns[min], c)
		heights[min] += h + 1 // +1 por el hueco entre notas
	}
	return columns
}
