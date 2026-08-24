package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yahwr/strongboxs/client/internal/store"
)

func seedNote(m Model, title, body string) Model {
	m = runCommand(m, "new "+title)
	m.ed.body.SetValue(body)
	m = runEditorCmd(m, "wq")
	return m
}

func TestSearchFiltersNotesLive(t *testing.T) {
	m := unlockedModel(t)
	m = seedNote(m, "compras", "café en grano")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	m = seedNote(m, "trabajo", "reunión de equipo")

	if m.board != secNotes || len(m.visibleNotes()) != 2 {
		t.Fatalf("precondición: visibles=%d", len(m.visibleNotes()))
	}

	// '/' abre la barra; filtrar es EN VIVO (sin Enter).
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = out.(Model)
	if !m.searchFocus {
		t.Fatal("'/' debe abrir la búsqueda")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("caf")})
	m = out.(Model)

	if m.query != "caf" {
		t.Fatalf("query=%q", m.query)
	}
	vn := m.visibleNotes()
	if len(vn) != 1 || vn[0].Title != "compras" {
		t.Fatalf("filtro vivo: %d resultados %+v", len(vn), vn)
	}

	// Enter aplica y mantiene el filtro; esc lo limpia.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.searchFocus || m.query != "caf" {
		t.Fatalf("enter debe aplicar sin limpiar: focus=%v q=%q", m.searchFocus, m.query)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.query != "" || len(m.visibleNotes()) != 2 {
		t.Fatalf("esc debe limpiar: q=%q visibles=%d", m.query, len(m.visibleNotes()))
	}
}

func TestFindCommandAndMatchFields(t *testing.T) {
	m := unlockedModel(t)
	m = seedNote(m, "servidor", "ssh root@10.0.0.1")
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	m = seedNote(m, "receta", "pan casero")

	m = runCommand(m, "find 10.0.0.1")
	vn := m.visibleNotes()
	if len(vn) != 1 || vn[0].Title != "servidor" {
		t.Fatalf(":find por cuerpo falló: %+v", vn)
	}

	m = runCommand(m, "find RECETA")
	if len(m.visibleNotes()) != 1 || m.visibleNotes()[0].Title != "receta" {
		t.Fatalf(":find insensible a mayúsculas falló")
	}

	// Sin coincidencias: cursor seguro.
	m = runCommand(m, "find zzz")
	if len(m.visibleNotes()) != 0 || m.selIdx != 0 {
		t.Fatalf("sin matches: %d sel=%d", len(m.visibleNotes()), m.selIdx)
	}
}

func TestSecretFullLifecycle(t *testing.T) {
	m := unlockedModel(t)

	// Cambiar a VAULT.
	m = runCommand(m, "v secretos")
	if m.board != secSecrets || m.visibleCount() != 0 {
		t.Fatalf("vista vault: board=%v count=%d", m.board, m.visibleCount())
	}

	// :new <plantilla>: "web" trae url/usuario/contraseña/notas.
	m = runCommand(m, "new web")
	if !m.ed.open || m.ed.sec != secSecrets || m.ed.tplName != "web" {
		t.Fatalf("editor web: open=%v sec=%v tpl=%q", m.ed.open, m.ed.sec, m.ed.tplName)
	}
	if len(m.ed.secFields) != 4 {
		t.Fatalf("campos de 'web': %d", len(m.ed.secFields))
	}
	m.ed.title.SetValue("Servidor prod")
	m.ed.setFieldValue("username", "admin")
	m.ed.setFieldValue("password", "S3cr3t-ñ-123")
	m.ed.setFieldValue("url", "ssh://10.0.0.1")
	m.ed.setFieldValue("notes", "clave rotada cada 90 días")
	m = runEditorCmd(m, "wq")

	if m.ed.open || m.visibleCount() != 1 {
		t.Fatalf("tras :wq: open=%v count=%d", m.ed.open, m.visibleCount())
	}
	s := m.secrets[0]
	if s.Template != "web" || s.Username != "admin" ||
		s.Password != "S3cr3t-ñ-123" || s.URL != "ssh://10.0.0.1" {
		t.Fatalf("entidad descifrada incorrecta: %+v", s)
	}

	// En disco: campos sensibles cifrados, URL EN CLARO (por diseño).
	rows, err := m.st.ListSecrets()
	if err != nil || len(rows) != 1 {
		t.Fatalf("filas BD: %d, %v", len(rows), err)
	}
	raw := rows[0]
	if raw.Title == "Servidor prod" || raw.Username == "admin" ||
		strings.Contains(raw.Password, "S3cr3t") || strings.Contains(raw.Notes, "rotada") {
		t.Fatal("los campos sensibles deberían estar cifrados en la BD")
	}
	if raw.URL != "ssh://10.0.0.1" {
		t.Errorf("la URL no debería cifrarse: %q", raw.URL)
	}

	// Tarjeta: contraseña enmascarada por defecto; 'v' la revela solo en pantalla.
	cardMasked := m.secretCard(s, false).Body
	if strings.Contains(cardMasked, "S3cr3t") || !strings.Contains(cardMasked, maskText) {
		t.Fatalf("tarjeta debe enmascarar: %q", cardMasked)
	}
	cardReveal := m.secretCard(s, true).Body
	if !strings.Contains(cardReveal, "S3cr3t-ñ-123") {
		t.Fatalf("reveal debe mostrar la contraseña: %q", cardReveal)
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = out.(Model)
	if !m.revealAll {
		t.Error("'v' debe alternar revealAll")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = out.(Model)
	if m.revealAll {
		t.Error("segundo 'v' debe ocultar de nuevo")
	}

	// Búsqueda también cubre la bóveda (usuario/url).
	m = runCommand(m, "find admin")
	if len(m.visibleSecrets()) != 1 {
		t.Fatalf("find en vault: %d", len(m.visibleSecrets()))
	}

	// Limpiar filtro antes de seguir (:find sin args).
	m = runCommand(m, "find")
	if m.query != "" || m.visibleCount() != 1 {
		t.Fatalf("limpiar find: q=%q visibles=%d", m.query, m.visibleCount())
	}

	// Editar: cambiar usuario y guardar.
	m = runCommand(m, "e")
	m.ed.setFieldValue("username", "root")
	m = runEditorCmd(m, ":x")
	if m.secrets[0].Username != "root" {
		t.Fatalf("edición no guardada: %q", m.secrets[0].Username)
	}

	// Guardas de comandos exclusivos de notas.
	m = runCommand(m, "pin")
	if !strings.Contains(m.errMsg, "solo aplica a notas") {
		t.Errorf("errMsg=%q", m.errMsg)
	}

	// :d borra la entrada.
	m = runCommand(m, "d")
	if m.visibleCount() != 0 {
		t.Fatalf("tras :d quedan %d entradas", m.visibleCount())
	}
	rows, _ = m.st.ListSecrets()
	if len(rows) != 0 {
		t.Fatalf("BD con %d filas vivas tras :d", len(rows))
	}
}

func TestVaultToggleAndGuards(t *testing.T) {
	m := unlockedModel(t)
	_ = store.Note{}

	m = runCommand(m, "vault")
	if m.board != secSecrets {
		t.Fatalf(":v alterna a vault; got %v", m.board)
	}
	m = runCommand(m, "v notas")
	if m.board != secNotes {
		t.Fatalf(":v notas vuelve; got %v", m.board)
	}
	m = runCommand(m, "v nube")
	if !strings.Contains(m.errMsg, "Vista inválida") {
		t.Errorf("errMsg=%q", m.errMsg)
	}

	// tab alterna también.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := out.(Model)
	if m2.board != secSecrets {
		t.Error("tab debe alternar vista")
	}
}
