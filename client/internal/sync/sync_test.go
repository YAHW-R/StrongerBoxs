package sync

import (
	"context"
	"testing"
	"time"

	"github.com/yahwr/strongboxs/client/internal/store"
)

func TestDeriveVerifierDeterministic(t *testing.T) {
	a, err := DeriveVerifier("clave-cuenta", "c2FsdA==")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := DeriveVerifier("clave-cuenta", "c2FsdA==")
	if a != b || len(a) != 64 {
		t.Fatalf("verifier no determinista: %q vs %q", a, b)
	}
	c, _ := DeriveVerifier("otra-clave", "c2FsdA==")
	if a == c {
		t.Fatal("claves distintas deben dar verifiers distintos")
	}
}

func TestRunOncePushesDirtyAndClearsFlags(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	m := newTestManager(t, f.URL(), st)

	n1, _ := st.CreateNote("local uno", "contenido", "#F9AB00")
	sc, _ := st.CreateSecret(store.Secret{Title: "cred local", Username: "u", Password: "p"})

	sum, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sum.Pushed != 2 || sum.Skipped != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	if it := f.item(n1.UUID); it == nil || it.payload["title"] != "local uno" {
		t.Fatalf("nota no llegó al servidor: %+v", it)
	}
	if it := f.item(sc.UUID); it == nil || it.payload["username"] != "u" {
		t.Fatalf("secreto no llegó: %+v", it)
	}

	dirty, _ := st.ListNotesForSync()
	if len(dirty) != 0 {
		t.Fatalf("dirty debería haberse limpiado; quedan %d", len(dirty))
	}
}

func TestPullAppliesNewerRemoteByDate(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	m := newTestManager(t, f.URL(), st)

	old := time.Now().UTC().Add(-time.Hour)
	newer := time.Now().UTC()

	uuid_ := "aaaaaaaa-0000-0000-0000-000000000001"
	// Local antigua.
	applied, err := st.UpsertRemoteNote(store.Note{
		UUID: uuid_, Title: "vieja", Body: "viejo", Version: 3,
		CreatedAt: old, UpdatedAt: old,
	})
	if !applied {
		t.Fatal("seed local")
	}

	// Remota más nueva (mismo uuid).
	f.mu.Lock()
	f.items[uuid_] = &fakeItem{kind: KindNote,
		payload: map[string]any{"title": "nueva", "body": "remoto"},
		version: 4, updatedAt: newer, syncedAt: newer.Add(-time.Second)}
	f.mu.Unlock()

	sum, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Applied != 1 {
		t.Fatalf("aplicadas=%d", sum.Applied)
	}
	notes, _ := st.ListNotes(false)
	if notes[0].Title != "nueva" || notes[0].Version != 4 {
		t.Fatalf("merge por fecha falló: %+v", notes[0])
	}

	// La fila remota NO debe quedar dirty (ya viene del servidor).
	dirty, _ := st.ListNotesForSync()
	for _, d := range dirty {
		if d.UUID == uuid_ {
			t.Error("fila aplicada desde remoto quedó marcada dirty")
		}
	}
}

func TestMutualLWWRoundTrip(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	m := newTestManager(t, f.URL(), st)

	now := time.Now().UTC().Add(-5 * time.Minute)
	uuid_ := "bbbbbbbb-0000-0000-0000-000000000002"

	// Servidor tiene v1 vieja.
	f.mu.Lock()
	f.items[uuid_] = &fakeItem{kind: KindNote,
		payload: map[string]any{"title": "servidor-vieja"}, version: 1,
		updatedAt: now.Add(-time.Minute), syncedAt: now.Add(-time.Minute)}
	f.mu.Unlock()

	// Local más nueva y dirty.
	applied, _ := st.UpsertRemoteNote(store.Note{
		UUID: uuid_, Title: "local-nueva", Body: "", Version: 2,
		CreatedAt: now, UpdatedAt: now,
	})
	if !applied {
		t.Fatal("seed")
	}
	if err := st.UpdateNote(&store.Note{ID: mustFind(t, st, uuid_), UUID: uuid_,
		Title: "local-editada", Body: "", Version: 2}); err != nil {
		t.Fatal(err)
	}

	// Ciclo: pull ignora la remota (fecha menor), push sube la local.
	sum, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pushed != 1 {
		t.Fatalf("pushed=%d", sum.Pushed)
	}
	got := f.item(uuid_)
	if got.payload["title"] != "local-editada" || got.version < 3 {
		t.Fatalf("el servidor no recibió la versión ganadora: %+v", got)
	}

	// Segundo ciclo: nada pendiente, nada nuevo.
	sum2, err := m.RunOnce(context.Background())
	if err != nil || sum2.Pushed+sum2.Applied != 0 {
		t.Fatalf("segundo ciclo debería ser neutro: %+v %v", sum2, err)
	}
}

func mustFind(t *testing.T, st *store.Store, uuid_ string) int64 {
	t.Helper()
	notes, err := st.ListNotes(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.UUID == uuid_ {
			return n.ID
		}
	}
	t.Fatal("no encontrada")
	return 0
}

func TestTombstoneBothDirections(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	m := newTestManager(t, f.URL(), st)

	// Dirección local→servidor: borrar una nota sincronizada.
	n1, _ := st.CreateNote("para borrar", "", "")
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := st.SoftDeleteNote(n1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.item(n1.UUID).deleted {
		t.Fatal("la tombstone no llegó al servidor")
	}
	// Ya no debe reenviarse.
	dirty, _ := st.ListNotesForSync()
	if len(dirty) != 0 {
		t.Fatalf("tombstone sigue dirty: %d", len(dirty))
	}

	// Dirección servidor→local: tombstone de un uuid inexistente aquí.
	other := "cccccccc-0000-0000-0000-000000000003"
	ts := time.Now().UTC()
	f.mu.Lock()
	f.items[other] = &fakeItem{kind: KindNote, deleted: true, version: 9,
		updatedAt: ts, syncedAt: ts}
	f.mu.Unlock()

	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	live, _ := st.ListNotes(false)
	for _, n := range live {
		if n.UUID == other {
			t.Error("tombstone remota aparece como viva")
		}
	}
}

func TestCursorDeltaSentOnSecondCycle(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	m := newTestManager(t, f.URL(), st)

	ctx := context.Background()
	first, err := m.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	cursor1, ok, _ := st.GetMeta("sync.cursor")
	if !ok {
		t.Fatal("cursor no persistido tras primer ciclo")
	}
	pull1Since := f.lastPullSince

	if _, err := m.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if f.lastPullSince == nil || pull1Since == nil && f.lastPullSince == nil {
		t.Fatal("segundo pull sin cursor")
	}
	if pull1Since == nil {
		// Primer ciclo sin cursor previo (since=null); el segundo ya lleva delta.
		if f.lastPullSince == nil {
			t.Fatal("delta ausente en segundo ciclo")
		}
		want, err := time.Parse(time.RFC3339Nano, cursor1)
		if err != nil {
			t.Fatal(err)
		}
		if !f.lastPullSince.Equal(want) {
			t.Fatalf("cursor delta = %v, quiero %v", f.lastPullSince, want)
		}
	}
}

func TestOfflineCycleRecordsErrorAndKeepsData(t *testing.T) {
	st := openStore(t)
	// Puerto cerrado: dial falla rápido.
	m := NewManager(st, Credentials{BaseURL: "http://127.0.0.1:1", Username: "x", Password: "y"}, time.Second, false)

	st.CreateNote("pendiente offline", "", "")
	sum, err := m.RunOnce(context.Background())
	if err == nil {
		t.Fatal("debería fallar sin servidor")
	}
	if sum.Pushed != 0 {
		t.Errorf("no debió subir nada: %+v", sum)
	}
	dirty, _ := st.ListNotesForSync()
	if len(dirty) != 1 {
		t.Error("los datos locales deben conservarse intactos para reintentar")
	}
}

func TestStartLoopRunsInBackground(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	cl, err := NewClient(credsFor(f.URL()))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{st: st, cl: cl, interval: 40 * time.Millisecond}

	st.CreateNote("auto", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Status()
		if s.CyclesOK >= 1 && s.TotalPushed >= 1 {
			return // éxito: ciclo automático detectó red y subió la nota
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("bucle de fondo no sincronizó: %+v", m.Status())
}

func TestNewClientNormalizesUsername(t *testing.T) {
	c, err := NewClient(Credentials{BaseURL: "http://localhost:8000", Username: "TuUsuario", Password: "x-pass-123"})
	if err != nil {
		t.Fatalf("normalización falló: %v", err)
	}
	if c.user != "tuusuario" {
		t.Errorf("user = %q", c.user)
	}
	if _, err := NewClient(Credentials{BaseURL: "http://l", Username: "ok-user", Password: ""}); err == nil {
		t.Error("contraseña vacía debería rechazarse")
	}
}
