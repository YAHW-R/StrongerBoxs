package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// startManager construye y arranca el motor con unas credenciales dadas.
func startManager(st *store.Store, creds sync.Credentials) (*sync.Manager, *sync.Gate) {
	interval := 90 * time.Second
	if s := os.Getenv("STRONGBOXS_SYNC_INTERVAL_SECS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}
	mgr := sync.NewManager(
		st, creds, interval,
		os.Getenv("STRONGBOXS_SYNC_DEBUG") != "",
	)
	gate := &sync.Gate{}
	mgr.BindGate(gate)
	mgr.Start(context.Background())
	return mgr, gate
}

// buildSyncRuntime arma el puente UI↔motor:
//   - si ya hay env/entorno configurado, arranca directamente;
//   - si no, Start() del runtime persiste sync.env y arranca en caliente.
func buildSyncRuntime(st *store.Store, declinedFallback bool) *ui.SyncRuntime {
	rt := &ui.SyncRuntime{}

	envURL := os.Getenv("STRONGBOXS_SYNC_URL")
	envUser := strings.TrimSpace(os.Getenv("STRONGBOXS_SYNC_USER"))
	envPass := os.Getenv("STRONGBOXS_SYNC_PASSWORD")

	rt.Start = func(creds sync.Credentials) bool {
		mgr, gate := startManager(st, creds)
		rt.SetManager(mgr)
		rt.Trigger = mgr.Trigger
		rt.Gate = gate
		st.SetMeta(map[string]string{"sync.configured": "1", "sync.declined": "0"})
		fmt.Printf("⟳ Sincronización activa → %s\n", creds.BaseURL)
		return true
	}

	if envURL != "" && envUser != "" && envPass != "" {
		// Configuración heredada del archivo/entorno: arranca al momento.
		rt.Start(sync.Credentials{BaseURL: envURL, Username: envUser, Password: envPass})
	}

	rt.IsConfigured = func() bool {
		if envURL != "" && envUser != "" && envPass != "" {
			return true
		}
		v, ok, _ := st.GetMeta("sync.configured")
		return ok && v == "1"
	}
	rt.IsDeclined = func() bool {
		v, ok, _ := st.GetMeta("sync.declined")
		return ok && v == "1"
	}
	rt.MarkDeclined = func() {
		_ = st.SetMeta(map[string]string{"sync.declined": "1"})
	}
	_ = declinedFallback
	return rt
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

	// Sync reactiva con asistente de primera configuración dentro de la TUI.
	rt := buildSyncRuntime(st, false)

	sess := session.New(st, session.DefaultTTL, nil).WithAuthorizer(authn.Default())

	p := tea.NewProgram(ui.New(sess, st, ui.WithSyncRuntime(rt)), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatal(err)
	}

	// Flush final al salir: sube lo pendiente aunque la UI ya cerró.
	if m := rt.Manager(); m != nil {
		fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if sum, err := m.RunOnce(fctx); err == nil && sum.Pushed+sum.Pulled > 0 {
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
