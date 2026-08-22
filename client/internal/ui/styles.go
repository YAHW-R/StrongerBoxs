package ui

import "github.com/charmbracelet/lipgloss"

// Paleta estilo Google Keep (hex, persistible en BD como string).
var (
	colorYellow = lipgloss.Color("#F9AB00")
	colorTeal   = lipgloss.Color("#00BFA5")
	colorViolet = lipgloss.Color("#7C4DFF")
)

var (
	appTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("FFFFFF")).
			Background(lipgloss.Color("7C4DFF")).
			Padding(0, 2)

	helloBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorViolet).
			Padding(2, 6).
			MarginTop(2)

	boardTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("B3B3B3")).
			MarginBottom(1).
			MarginLeft(1)

	cardBaseStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("444444")).
			Padding(1, 2)

	cardTitleStyle = lipgloss.NewStyle().
			Bold(true)

	cardBodyStyle = lipgloss.NewStyle().
			Faint(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("666666"))
)
