package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/yahwr/strongboxs/client/internal/authn"
	"github.com/yahwr/strongboxs/client/internal/session"
	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/sync"
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
// startSyncIfConfigured lanza el motor de sincronización en segundo plano
// si hay variables de entorno de sync (no interfiere con la TUI).
func startSyncIfConfigured(st *store.Store) (context.CancelFunc, bool) {
	url := os.Getenv("STRONGBOXS_SYNC_URL")
	if url == "" {
		return nil, false
	}
	interval := 60 * time.Second
	if s := os.Getenv("STRONGBOXS_SYNC_INTERVAL_SECS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}
	mgr := sync.NewManager(
		st,
		sync.Credentials{
			BaseURL:  url,
			Username: os.Getenv("STRONGBOXS_SYNC_USER"),
			Password: os.Getenv("STRONGBOXS_SYNC_PASSWORD"),
		},
		interval,
		os.Getenv("STRONGBOXS_SYNC_DEBUG") != "",
	)
	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx)
	fmt.Printf("⟳ Sincronización en segundo plano activa → %s (cada %s)\n", url, interval)
	return cancel, true
}

// runTUI lanza la app: el propio TUI gestiona setup, lock-screen y
// desbloqueo (por eso la sesión se crea SIN prompt aquí).
func runTUI() {
	// Config persistente (~/.config/strongboxs/sync.env); el entorno
	// exportado en la shell tiene prioridad sobre el archivo.
	if err := loadEnvFile(envFilePath()); err != nil {
		fmt.Fprintf(os.Stderr, "strongboxs: aviso env: %v\n", err)
	}

	st, err := store.Open("")
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	// La sincronización trabaja solo con ciphertext: arranca aunque la
	// bóveda siga bloqueada, sin estorbar al flujo del usuario.
	cancelSync, _ := startSyncIfConfigured(st)
	defer cancelSync()

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
