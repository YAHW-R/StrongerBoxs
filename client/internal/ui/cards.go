package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/ui/components"
)

func defaultCardColor() lipgloss.Color { return colorViolet }

// toCards mapea entidades del store a tarjetas visuales.
// Sin notas devuelve nil (el tablero muestra una pista para :new).
func toCards(notes []store.Note) []components.Note {
	cards := make([]components.Note, 0, len(notes))
	for _, n := range notes {
		color := defaultCardColor()
		if n.Color != "" {
			color = lipgloss.Color(n.Color)
		}
		title := n.Title
		if n.Pinned {
			title = "📌 " + title
		}
		if n.Archived {
			title = "🗄 " + title
		}
		body := n.Body
		if len(body) > 220 {
			body = body[:217] + "…"
		}
		cards = append(cards, components.Note{Title: title, Body: body, Color: color})
	}
	return cards
}
