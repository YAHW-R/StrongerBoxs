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

// startSyncIfConfigured lanza el motor reactivo si hay variables de entorno.
// Devuelve el manager (Trigger/flush), la Gate de la UI y si está activo.
func startSyncIfConfigured(st *store.Store) (*sync.Manager, *sync.Gate, bool) {
	url := os.Getenv("STRONGBOXS_SYNC_URL")
	if url == "" {
		return nil, nil, false
	}
	interval := 90 * time.Second
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
	gate := &sync.Gate{}
	mgr.BindGate(gate)
	mgr.Start(context.Background())
	fmt.Printf("⟳ Sincronización reactiva → %s (push tras cada guardado · pull cada %s)\n", url, interval)
	return mgr, gate, true
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

	// Sync reactiva: push tras cada guardado; pull de fondo solo en tablero.
	mgr, gate, syncOn := startSyncIfConfigured(st)

	sess := session.New(st, session.DefaultTTL, nil).WithAuthorizer(authn.Default())

	var opts []ui.Opt
	if syncOn {
		opts = append(opts, ui.WithSync(mgr.Trigger, gate))
	}

	p := tea.NewProgram(ui.New(sess, st, opts...), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatal(err)
	}

	// Flush final al salir: sube lo pendiente aunque la UI ya cerró.
	if syncOn {
		fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if sum, err := mgr.RunOnce(fctx); err == nil {
			fmt.Printf("⟳ Flush final: subidos=%d recibidos=%d\n", sum.Pushed, sum.Pulled)
		}
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
