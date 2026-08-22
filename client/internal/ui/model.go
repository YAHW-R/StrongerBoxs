package ui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/ui/components"
)

type viewState int

const (
	viewHello viewState = iota
	viewBoard
)

// Model es el estado raíz de la app BubbleTea.
type Model struct {
	state  viewState
	width  int
	height int
	notes  []components.Note
}

// New crea el modelo. Recibe las notas ya cargadas del store;
// si no hay ninguna (primer arranque), muestra tarjetas de demostración.
func New(notes []store.Note) Model {
	return Model{
		state: viewHello,
		notes: toCards(notes),
	}
}

func defaultCardColor() lipgloss.Color { return colorViolet }

// toCards mapea entidades del store a tarjetas visuales.
func toCards(notes []store.Note) []components.Note {
	if len(notes) == 0 {
		return demoNotes()
	}
	cards := make([]components.Note, len(notes))
	for i, n := range notes {
		color := defaultCardColor()
		if n.Color != "" {
			color = lipgloss.Color(n.Color)
		}
		title := n.Title
		if n.Pinned {
			title = "📌 " + title
		}
		cards[i] = components.Note{Title: title, Body: n.Body, Color: color}
	}
	return cards
}

func demoNotes() []components.Note {
	return []components.Note{
		{
			Title: "Lista de compras",
			Body:  "- Café en grano\n- Pan integral\n- Palomitas\n- Leche de avena",
			Color: colorYellow,
		},
		{
			Title: "Contraseña servidor",
			Body:  "Cifrada con AES-256-GCM.\nNunca se muestra sin sesión sudo.",
			Color: colorTeal,
		},
		{
			Title: "Ideas",
			Body:  "Sincronización Zero-Knowledge con el servidor FastAPI.",
			Color: colorViolet,
		},
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter", " ", "esc":
			if m.state == viewHello {
				m.state = viewBoard
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case viewHello:
		return m.viewHello()
	default:
		return m.viewBoard()
	}
}

func (m Model) viewHello() string {
	content := strings.Join([]string{
		appTitleStyle.Render("STRONGBOXS"),
		"",
		"Hola Mundo desde BubbleTea.",
		"Pulsa Enter para ver el tablero de notas.",
	}, "\n")

	body := lipgloss.JoinVertical(lipgloss.Center, helloBoxStyle.Render(content), "", helpStyle.Render("q / ctrl+c · salir"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewBoard() string {
	cards := make([]string, len(m.notes))
	for i, n := range m.notes {
		cards[i] = components.RenderCard(n)
	}

	// Masonry simulado: nº de columnas según el ancho de la terminal
	// (mín. 1, máx. 3; ~40 columnas por tarjeta).
	cols := m.width / 42
	if cols < 1 {
		cols = 1
	}
	if cols > len(cards) {
		cols = len(cards)
	}
	columns := components.Distribute(cards, cols)
	colViews := make([]string, 0, len(columns))
	for _, col := range columns {
		colViews = append(colViews, lipgloss.JoinVertical(lipgloss.Left, col...))
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, colViews...)

	header := boardTitleStyle.Render("Notas")
	help := helpStyle.Render("q · salir")

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		board,
		"",
		help,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}
