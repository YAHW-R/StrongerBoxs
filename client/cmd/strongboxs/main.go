package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/ui"
)

func main() {
	st, err := store.Open("") // ~/.local/share/strongboxs/strongboxs.db
	if err != nil {
		fmt.Fprintf(os.Stderr, "strongboxs: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	notes, err := st.ListNotes(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strongboxs: %v\n", err)
		st.Close()
		os.Exit(1)
	}

	p := tea.NewProgram(ui.New(notes), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "strongboxs: %v\n", err)
		os.Exit(1)
	}
}
