package sync_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yahwr/strongboxs/client/internal/crypto"
	"github.com/yahwr/strongboxs/client/internal/store"
	"github.com/yahwr/strongboxs/client/internal/sync"
)

// TestE2ERealServer corre contra http://localhost:8000 (docker compose up).
// Verifica el ciclo completo con cifrado real: seal local → push →
// pull opaco → merge → unseal idéntico.
func TestE2ERealServer(t *testing.T) {
	url := "http://localhost:8000"
	for _, p := range []string{"/tmp/opencode/e2e.db", "/tmp/opencode/e2e.db-wal", "/tmp/opencode/e2e.db-shm"} {
		_ = os.Remove(p)
	}
	st, err := store.Open("/tmp/opencode/e2e.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := sync.NewManager(st, sync.Credentials{
		BaseURL: url, Username: "e2e-user", Password: "cuenta-pass-123",
	}, time.Second, true)
	ctx := context.Background()

	vault, err := crypto.CreateVault(st, "maestra-e2e-123")
	if err != nil {
		if err != crypto.ErrVaultExists {
			t.Fatal(err)
		}
		if vault, err = crypto.OpenVault(st, "maestra-e2e-123"); err != nil {
			t.Fatal(err)
		}
	}

	// 1) nota local CIFRADA (como hace la app) y push.
	title, _ := vault.Seal("nota-e2e")
	body, _ := vault.Seal("contenido cifrado")
	n1, err := st.CreateNote(title, body, "#F9AB00")
	if err != nil {
		t.Fatal(err)
	}
	s1, err := m.RunOnce(ctx)
	if err != nil {
		t.Fatalf("ciclo 1: %v", err)
	}
	fmt.Printf("ciclo1 %+v\n", s1)
	if s1.Pushed != 1 {
		t.Fatalf("esperaba 1 pushed: %+v", s1)
	}

	// 2) el cursor se guardó ANTES del push: puede llegar un eco del propio
	// ítem, pero con fechas normalizadas a µs ya NO debe re-aplicarse.
	s2, _ := m.RunOnce(ctx)
	fmt.Printf("ciclo2 %+v\n", s2)
	if s2.Applied != 0 {
		t.Fatalf("el eco no debe re-aplicarse (LWW por fecha igual): %+v", s2)
	}

	// 3) ciclo plenamente estable.
	s3, _ := m.RunOnce(ctx)
	fmt.Printf("ciclo3 %+v\n", s3)
	if s3.Pushed+s3.Applied+s3.Pulled != 0 {
		t.Fatalf("ciclo 3 debería ser neutro: %+v", s3)
	}

	// 3) el contenido en disco sigue siendo ciphertext y descifra igual.
	rows, err := st.ListNotes(false)
	if err != nil || len(rows) == 0 {
		t.Fatalf("filas: %d %v", len(rows), err)
	}
	var row *store.Note
	for i := range rows {
		if rows[i].UUID == n1.UUID {
			row = &rows[i]
		}
	}
	if row == nil || row.Title == "nota-e2e" {
		t.Fatal("el título en BD debería seguir cifrado")
	}
	gotTitle, err := vault.Unseal(row.Title)
	if err != nil || gotTitle != "nota-e2e" {
		t.Fatalf("unseal tras sync: %q %v", gotTitle, err)
	}
	fmt.Println("✓ E2E: push/pull con ciphertext íntegro")
}
