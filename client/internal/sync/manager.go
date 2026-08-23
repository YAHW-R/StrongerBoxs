package sync

import (
	"context"
	"errors"
	"fmt"
	"net"
	stdsync "sync"
	"time"

	"github.com/yahwr/strongboxs/client/internal/store"
)

const metaCursorKey = "sync.cursor"

var ErrSyncInProgress = errors.New("sync: ya hay un ciclo en curso")

type Summary struct {
	Pulled  int // ítems recibidos del servidor
	Applied int // fusionados en local (ganaban por fecha)
	Pushed  int // confirmados por el servidor
	Skipped int // rechazados (local ya era más nuevo en el servidor)
}

// Stats es la foto de estado para la UI (thread-safe).
type Stats struct {
	LastRun      time.Time
	LastOK       bool
	LastError    string
	TotalPulled  int64
	TotalPushed  int64
	InProgress   bool
	CyclesOK     int64
	CyclesFailed int64
}

// Manager ejecuta ciclos pull→merge→push en segundo plano.
type Manager struct {
	st       *store.Store
	cl       *Client // nil si la configuración es inválida
	initErr  error
	interval time.Duration
	debug    bool

	mu      stdsync.Mutex
	syncing bool
	stats   Stats
}

func NewManager(st *store.Store, creds Credentials, interval time.Duration, debug bool) *Manager {
	if interval <= 0 {
		interval = time.Minute
	}
	m := &Manager{st: st, interval: interval, debug: debug}
	cl, err := NewClient(creds)
	if err != nil {
		m.initErr = err
		return m
	}
	m.cl = cl
	return m
}

func (m *Manager) Status() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

// Start lanza el bucle de fondo: primer intento a los 3 s y luego cada
// intervalo, siempre con chequeo previo de conectividad.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		// Primer ciclo casi inmediato; luego cada intervalo.
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		for {
			m.cycle(ctx)
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.interval):
			}
		}
	}()
}

func (m *Manager) cycle(ctx context.Context) {
	if m.cl == nil {
		m.record(m.initErr)
		return
	}
	if !m.online() {
		m.record(fmt.Errorf("sin conexión con %s", m.cl.Host()))
		return
	}
	defer func() {
		if r := recover(); r != nil {
			m.record(fmt.Errorf("pánico en ciclo sync: %v", r))
		}
	}()

	sum, err := m.RunOnce(ctx)
	if err != nil {
		m.record(err)
		return
	}
	m.mu.Lock()
	m.stats.LastRun = time.Now()
	m.stats.LastOK = true
	m.stats.LastError = ""
	m.stats.TotalPulled += int64(sum.Pulled)
	m.stats.TotalPushed += int64(sum.Pushed)
	m.stats.CyclesOK++
	m.stats.InProgress = false
	m.mu.Unlock()

	if m.debug {
		fmt.Printf("[sync] ok · recibidos=%d aplicados=%d subidos=%d saltados=%d\n",
			sum.Pulled, sum.Applied, sum.Pushed, sum.Skipped)
	}
}

func (m *Manager) record(err error) {
	m.mu.Lock()
	m.stats.LastRun = time.Now()
	m.stats.LastOK = false
	m.stats.LastError = err.Error()
	m.stats.CyclesFailed++
	m.stats.InProgress = false
	m.mu.Unlock()
	if m.debug {
		fmt.Printf("[sync] error: %v\n", err)
	}
}

// online comprueba conectividad TCP real contra el host del servidor.
func (m *Manager) online() bool {
	if m.cl == nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", m.cl.Host(), 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// RunOnce ejecuta un ciclo completo: PULL+merge → cursor → PUSH dirty.
// Es seguro llamarlo concurrentemente; si ya corre devuelve ErrSyncInProgress.
func (m *Manager) RunOnce(ctx context.Context) (Summary, error) {
	m.mu.Lock()
	if m.syncing {
		m.mu.Unlock()
		return Summary{}, ErrSyncInProgress
	}
	m.syncing = true
	m.stats.InProgress = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.syncing = false
		m.stats.InProgress = false
		m.mu.Unlock()
	}()

	var sum Summary

	// ---- PULL + merge LWW por fecha ----
	var since *time.Time
	if cur, ok, _ := m.st.GetMeta(metaCursorKey); ok {
		if t, err := time.Parse(time.RFC3339Nano, cur); err == nil {
			since = &t
		}
	}
	pull, err := m.cl.Pull(ctx, since)
	if err != nil {
		return sum, err
	}
	sum.Pulled = len(pull.Items)
	for _, it := range pull.Items {
		applied, err := mergeItem(m.st, it)
		if err != nil {
			return sum, err
		}
		if applied {
			sum.Applied++
		}
	}
	// Cursor = reloj del servidor; SOLO se usa para pedir deltas,
	// jamás para decidir qué contenido gana.
	cursor := pull.ServerTime.UTC().Format(time.RFC3339Nano)
	if err := m.st.SetMeta(map[string]string{metaCursorKey: cursor}); err != nil {
		return sum, err
	}

	// ---- PUSH de todo lo dirty local (incluye tombstones) ----
	notes, err := m.st.ListNotesForSync()
	if err != nil {
		return sum, err
	}
	secrets, err := m.st.ListSecretsForSync()
	if err != nil {
		return sum, err
	}

	items := make([]ItemIn, 0, len(notes)+len(secrets))
	noteIDs := map[string]int64{}
	secretIDs := map[string]int64{}
	for _, n := range notes {
		items = append(items, noteToItem(n))
		noteIDs[n.UUID] = n.ID
	}
	for _, sc := range secrets {
		items = append(items, secretToItem(sc))
		secretIDs[sc.UUID] = sc.ID
	}

	resp, err := m.cl.Push(ctx, items)
	if err != nil {
		return sum, err
	}
	sum.Pushed = len(resp.Accepted)
	sum.Skipped = len(resp.Skipped)

	var accNotes, accSecrets []int64
	for _, r := range resp.Accepted {
		if id, ok := noteIDs[r.ItemUUID]; ok {
			accNotes = append(accNotes, id)
		}
		if id, ok := secretIDs[r.ItemUUID]; ok {
			accSecrets = append(accSecrets, id)
		}
	}
	if err := m.st.MarkNotesSynced(accNotes); err != nil {
		return sum, err
	}
	if err := m.st.MarkSecretsSynced(accSecrets); err != nil {
		return sum, err
	}
	return sum, nil
}

func mergeItem(st *store.Store, it ItemOut) (bool, error) {
	switch it.Kind {
	case KindSecret:
		return st.UpsertRemoteSecret(remoteToSecret(it))
	case KindNote:
		return st.UpsertRemoteNote(remoteToNote(it))
	default:
		return false, fmt.Errorf("sync: kind desconocido %q", it.Kind)
	}
}
