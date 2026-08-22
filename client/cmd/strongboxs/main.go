package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/yahwr/strongboxs/client/internal/authn"
	"github.com/yahwr/strongboxs/client/internal/session"
	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/ui"
)

// termPrompt lectura sin eco en terminal (solo flujo CLI passwd).
func termPrompt(label string) (string, error) {
	fmt.Print(label)
	defer fmt.Println()
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "strongboxs: %v\n", err)
	os.Exit(1)
}

// runTUI lanza la app: el propio TUI gestiona setup, lock-screen y desbloqueo.
func runTUI() {
	st, err := store.Open("")
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	sess := session.New(st, session.DefaultTTL, nil).WithAuthorizer(authn.Default())

	p := tea.NewProgram(ui.New(sess, st), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatal(err)
	}
}

// runPasswd cambia la contraseña maestra desde CLI
// (validando contra el sistema Linux vía PAM/sudo).
func runPasswd() {
	st, err := store.Open("")
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	sess := session.New(st, session.DefaultTTL, termPrompt).WithAuthorizer(authn.Default())
	if _, err := sess.Ensure(); err != nil {
		fatal(err)
	}
	if err := sess.ChangeMasterPassword(); err != nil {
		fatal(err)
	}
	fmt.Println("✓ Contraseña maestra actualizada.")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "passwd" {
		runPasswd()
		return
	}
	runTUI()
}
