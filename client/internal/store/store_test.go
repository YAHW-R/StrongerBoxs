package store

import (
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNoteCRUD(t *testing.T) {
	s := openTestStore(t)

	n1, err := s.CreateNote("compras", "- café\n- pan", "#F9AB00")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if n1.ID == 0 || n1.UUID == "" || !n1.Dirty || n1.Version != 1 {
		t.Fatalf("nota inicial mal formada: %+v", n1)
	}

	if _, err := s.CreateNote("ideas", "masonry TUI", ""); err != nil {
		t.Fatalf("CreateNote 2: %v", err)
	}

	notes, err := s.ListNotes(false)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("esperaba 2 notas, hay %d", len(notes))
	}

	// Update: pin + body; versión debe incrementarse en el propio struct.
	n1.Pinned = true
	n1.Body = "- café\n- pan integral"
	if err := s.UpdateNote(&n1); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if !n1.Dirty || n1.Version != 2 {
		t.Errorf("versión/dirty no actualizados: v=%d dirty=%v", n1.Version, n1.Dirty)
	}

	fresh, err := s.ListNotes(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) == 0 || fresh[0].ID != n1.ID {
		t.Errorf("la nota fijada debería ir primero")
	}
	if fresh[0].Body != "- café\n- pan integral" {
		t.Errorf("body no persistido: %q", fresh[0].Body)
	}

	// Soft delete: desaparece del listado por defecto.
	if err := s.SoftDeleteNote(n1.ID); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	notes, _ = s.ListNotes(false)
	if len(notes) != 1 {
		t.Fatalf("tras borrado esperaba 1 nota, hay %d", len(notes))
	}
}

func TestSecretCRUD(t *testing.T) {
	s := openTestStore(t)

	sc, err := s.CreateSecret(Secret{
		Title:    "servidor prod",
		Username: "admin",
		Password: "ciphertext-aes",
		URL:      "ssh://10.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	got, err := s.GetSecretByUUID(sc.UUID)
	if err != nil {
		t.Fatalf("GetSecretByUUID: %v", err)
	}
	if got.Password != "ciphertext-aes" || got.Username != "admin" {
		t.Errorf("roundtrip incorrecto: %+v", got)
	}

	got.Password = "rotado"
	if err := s.UpdateSecret(&got); err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	if got.Password != "rotado" || got.Version != sc.Version+1 || !got.Dirty {
		t.Errorf("update no aplicado: %+v", got)
	}
	again, _ := s.GetSecretByUUID(sc.UUID)
	if again.Version != sc.Version+1 {
		t.Errorf("versión no persistida: %d", again.Version)
	}

	if err := s.SoftDeleteSecret(sc.ID); err != nil {
		t.Fatalf("SoftDeleteSecret: %v", err)
	}
	secrets, _ := s.ListSecrets()
	if len(secrets) != 0 {
		t.Errorf("tras borrado no debe listarse nada, hay %d", len(secrets))
	}
}

func TestMarkNotesSynced(t *testing.T) {
	s := openTestStore(t)

	a, _ := s.CreateNote("a", "", "")
	b, _ := s.CreateNote("b", "", "")

	if err := s.MarkNotesSynced([]int64{a.ID}); err != nil {
		t.Fatalf("MarkNotesSynced: %v", err)
	}
	notes, _ := s.ListNotes(true)
	dirtyCount := 0
	for _, n := range notes {
		if n.Dirty {
			dirtyCount++
			if n.ID != b.ID {
				t.Errorf("nota equivocada marcada dirty")
			}
		}
	}
	if dirtyCount != 1 {
		t.Errorf("esperaba 1 nota dirty, hay %d", dirtyCount)
	}
}
