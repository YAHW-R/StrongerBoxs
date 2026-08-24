package sync

import (
	"context"
	"testing"
	"time"

	"github.com/yahwr/strongboxs/client/internal/store"
)

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("condición no alcanzada a tiempo")
}

func TestTriggerRunsPushCycle(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	cl, _ := NewClient(credsFor(f.URL()))
	m := &Manager{st: st, cl: cl, interval: time.Hour, gate: &Gate{}, trigger: make(chan struct{}, 1)}

	st.CreateNote("reactiva", "contenido", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)

	waitFor(t, 3*time.Second, func() bool { return m.Status().TotalPushed >= 1 })
}

func TestGateBlocksPullButAllowsPush(t *testing.T) {
	f := newFakeAPI(t)
	st := openStore(t)
	cl, _ := NewClient(credsFor(f.URL()))
	gate := &Gate{}
	m := &Manager{st: st, cl: cl, interval: time.Hour, gate: gate, trigger: make(chan struct{}, 1)}

	now := time.Now().UTC()
	uuid_ := "dddddddd-0000-0000-0000-000000000009"

	// Remoto VIEJO (perderá por fecha frente a lo local).
	f.mu.Lock()
	f.items[uuid_] = &fakeItem{kind: KindNote,
		payload: map[string]any{"title": "remoto-viejo"}, version: 7,
		updatedAt: now.Add(-time.Minute), syncedAt: now.Add(-time.Minute)}
	f.mu.Unlock()

	// Local NUEVO y dirty (editar siempre avanza la fecha).
	applied, _ := st.UpsertRemoteNote(store.Note{UUID: uuid_, Title: "base",
		Version: 1, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)})
	if !applied {
		t.Fatal("seed")
	}
	if err := st.UpdateNote(&store.Note{ID: mustFind(t, st, uuid_), UUID: uuid_,
		Title: "local-editada", Body: "", Version: 1}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Gate ocupada: NO hay pull (no llega el remoto viejo a mezclarse),
	// pero el push de lo guardado sí sale.
	gate.Set(true)
	sum, err := m.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pushed != 1 {
		t.Fatalf("push con busy esperado=1 got=%+v", sum)
	}
	if got := f.item(uuid_).payload["title"]; got != "local-editada" {
		t.Fatalf("servidor = %v", got)
	}

	// Gate libre: ciclo completo; el remoto viejo pierde por fecha.
	gate.Set(false)
	if _, err := m.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	notes, _ := st.ListNotes(false)
	for _, n := range notes {
		if n.UUID == uuid_ && n.Title != "local-editada" {
			t.Fatalf("el merge pisó lo local más nuevo: %+v", n)
		}
	}
}
