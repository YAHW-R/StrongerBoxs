package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yahwr/strongboxs/client/internal/crypto"
)

// unlockedModel devuelve un modelo en el tablero con bóveda creada y sesión viva.
func unlockedModel(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t)
	if _, err := crypto.CreateVault(m.st, testPw); err != nil {
		t.Fatal(err)
	}
	m = New(m.sess, m.st) // con bóveda y sin sesión → lock-screen
	if m.state != viewLocked {
		t.Fatalf("precondición lock: estado=%d", m.state)
	}
	m.input.SetValue(testPw)
	m = pressEnter(m)
	if m.state != viewBoard {
		t.Fatalf("precondición: estado=%d", m.state)
	}
	return m
}

// runCommand ejecuta un comando directamente (la barra ':' del tablero
// fue sustituida por ctrl+k; los tests usan el dispatcher interno).
func runCommand(m Model, line string) Model {
	out, _ := m.executeCommand(strings.TrimPrefix(strings.TrimSpace(line), ":"))
	return out.(Model)
}

func TestCommandCreateEditDeleteFlow(t *testing.T) {
	m := unlockedModel(t)

	// :new crea nota + abre editor.
	m = runCommand(m, "new Lista de compras")
	if !m.ed.open || len(m.notes) != 1 || m.notes[0].Title != "Lista de compras" {
		t.Fatalf("tras :new: open=%v ents=%v", m.ed.open, m.notes)
	}

	// Editar cuerpo y guardar sin cerrar (:w).
	m.ed.body.SetValue("- café\n- pan integral")
	m = runEditorCmd(m, "w")
	if !m.ed.open {
		t.Fatal(":w no debe cerrar el editor")
	}

	// Verificación en disco: los BLOB NO son texto claro.
	rows, err := m.st.ListNotes(true)
	if err != nil || len(rows) != 1 {
		t.Fatalf("filas en BD: %d, %v", len(rows), err)
	}
	if rows[0].Title == "Lista de compras" || strings.Contains(rows[0].Body, "café") {
		t.Fatal("los campos deberían estar cifrados en la BD")
	}
	title, err := m.sess.UnsealField(rows[0].Title)
	if err != nil || title != "Lista de compras" {
		t.Fatalf("unseal título: %q, %v", title, err)
	}
	body, _ := m.sess.UnsealField(rows[0].Body)
	if body != "- café\n- pan integral" {
		t.Fatalf("unseal cuerpo: %q", body)
	}

	// Cerrar con esc → tablero.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.ed.open {
		t.Fatal("esc debe cerrar el editor")
	}

	// :pin fija; persiste tras refresh.
	m = runCommand(m, "pin")
	if !m.notes[0].Pinned {
		t.Fatal(":pin no fijó la nota")
	}

	// :color cambia el color.
	m = runCommand(m, "color turquesa")
	if m.notes[0].Color != "#00BFA5" {
		t.Fatalf(":color → %q", m.notes[0].Color)
	}

	// :arch oculta la nota del listado por defecto (filtro visual).
	m = runCommand(m, "arch")
	if len(m.visibleNotes()) != 0 || len(m.notes) != 1 {
		t.Fatalf(":arch debería ocultar la nota: visibles=%d totales=%d",
			len(m.visibleNotes()), len(m.notes))
	}
	m = runCommand(m, "all")
	if len(m.visibleNotes()) != 1 || !m.visibleNotes()[0].Archived {
		t.Fatalf(":all debería mostrar la archivada; hay %d", len(m.visibleNotes()))
	}
	m = runCommand(m, "arch") // deshacer

	// :d pide confirmación; 'y' ejecuta.
	m = runCommand(m, "d")
	if !m.confirmOpen {
		t.Fatal(":d debe abrir confirmación")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = out.(Model)
	if len(m.visibleNotes()) != 0 {
		t.Fatalf("tras :d quedan %d notas", len(m.visibleNotes()))
	}
	rows, _ = m.st.ListNotes(true)
	if len(rows) != 0 {
		t.Fatalf("BD debería estar sin notas vivas; hay %d", len(rows))
	}
}

// runEditorCmd abre la mini-barra del editor y ejecuta un comando.
func runEditorCmd(m Model, line string) Model {
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	me := out.(Model)
	if !me.ed.cmd {
		panic("la barra del editor no se abrió")
	}
	me.ed.cmdLn.SetValue(line)
	return pressEnter(me)
}

func TestEditorWQAndQBang(t *testing.T) {
	m := unlockedModel(t)

	m = runCommand(m, ":n Nota wq")
	if !m.ed.open {
		t.Fatal(":n debe abrir editor")
	}
	m.ed.body.SetValue("contenido final")

	// :wq guarda y cierra.
	m = runEditorCmd(m, "wq")
	if m.ed.open || len(m.notes) != 1 || m.notes[0].Body != "contenido final" {
		t.Fatalf("tras :wq: open=%v body=%q", m.ed.open, m.notes[0].Body)
	}

	// Reabrir, modificar y salir SIN guardar con :q!.
	m = runCommand(m, "e")
	m.ed.title.SetValue("Título cambiado sin guardar")
	m = runEditorCmd(m, "q!")
	if m.ed.open {
		t.Fatal(":q! debe cerrar")
	}
	if m.notes[0].Title != "Nota wq" {
		t.Errorf(":q! no debía guardar cambios; título=%q", m.notes[0].Title)
	}
}

func TestUnknownCommandAndGuards(t *testing.T) {
	m := unlockedModel(t)

	m = runCommand(m, "frobnicate")
	if !strings.Contains(m.errMsg, "desconocido") {
		t.Errorf("errMsg=%q", m.errMsg)
	}

	m = runCommand(m, "color dorado")
	if !strings.Contains(m.errMsg, "Color inválido") {
		t.Errorf("errMsg=%q", m.errMsg)
	}

	m = runCommand(m, "e")
	if !strings.Contains(m.errMsg, ":new") {
		t.Errorf("editar sin notas debe sugerir :new; errMsg=%q", m.errMsg)
	}

	m = runCommand(m, "help")
	if !m.showHelp {
		t.Error(":help debe abrir overlay")
	}
}

func TestQuitCommandEmitsQuit(t *testing.T) {
	m := unlockedModel(t)
	out, cmd := m.executeCommand("q")
	_ = out
	if cmd == nil {
		t.Fatal(":q debe devolver comando")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf(":q debe producir tea.QuitMsg; got %T", msg)
	}
}

func TestCursorNavigationJK(t *testing.T) {
	m := unlockedModel(t)
	for _, title := range []string{"uno", "dos", "tres"} {
		m = runCommand(m, "new "+title)
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // cerrar editor
		m = out.(Model)
	}
	if len(m.notes) != 3 {
		t.Fatalf("precondición: %d entidades", len(m.notes))
	}
	// El listado es updated_at DESC: la última creada queda primera.
	if m.selIdx != 0 || m.notes[0].Title != "tres" {
		t.Fatalf("selIdx=%d título0=%q; quiero 0/'tres'", m.selIdx, m.notes[0].Title)
	}

	step := func(key string) Model {
		out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		return out.(Model)
	}
	m = step("j") // tres→dos
	if m.selIdx != 1 || m.notes[m.selIdx].Title != "dos" {
		t.Fatalf("j: selIdx=%d", m.selIdx)
	}
	m = step("j") // dos→uno
	if m.notes[m.selIdx].Title != "uno" {
		t.Fatalf("segundo j: título=%q", m.notes[m.selIdx].Title)
	}
	m = step("j") // clamp al último
	if m.selIdx != 2 {
		t.Fatalf("clamp abajo: selIdx=%d", m.selIdx)
	}
	m = step("g")
	if m.selIdx != 0 {
		t.Fatalf("g: selIdx=%d", m.selIdx)
	}
	m = step("k") // clamp arriba
	if m.selIdx != 0 {
		t.Fatalf("clamp arriba: selIdx=%d", m.selIdx)
	}
}
