package ui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yahwr/strongboxs/client/internal/crypto"
	"github.com/yahwr/strongboxs/client/internal/session"
	"github.com/yahwr/strongboxs/client/internal/store"
)

func newTestModel(t *testing.T) (Model, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sess := session.New(st, session.DefaultTTL, nil)
	m := New(sess, st)
	m.width, m.height = 100, 40
	return m, st
}

func pressEnter(m Model) Model {
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return out.(Model)
}

const testPw = "clave-maestra-123"

func TestSetupFlowCreatesVaultAndShowsBoard(t *testing.T) {
	m, _ := newTestModel(t)
	if m.state != viewSetup {
		t.Fatalf("estado inicial = %d, quiero viewSetup", m.state)
	}

	m.input.SetValue(testPw)
	m = pressEnter(m)
	if !m.setting || m.errMsg != "" {
		t.Fatalf("fase confirmación: setting=%v err=%q", m.setting, m.errMsg)
	}
	if m.input.Placeholder != "Repite la contraseña" {
		t.Errorf("placeholder = %q", m.input.Placeholder)
	}

	m.input.SetValue("otra-clave-distinta")
	m = pressEnter(m)
	if m.setting || m.errMsg == "" || m.sess.HasVault() {
		t.Fatalf("confirmación errónea debe abortar: setting=%v err=%q vault=%v",
			m.setting, m.errMsg, m.sess.HasVault())
	}

	m.input.SetValue(testPw)
	m = pressEnter(m)

	m.input.SetValue(testPw)
	m = pressEnter(m)
	if !m.sess.HasVault() {
		t.Fatal("la bóveda debería existir tras el flujo correcto")
	}
	if m.state != viewBoard {
		t.Fatalf("estado final = %d, quiero viewBoard", m.state)
	}
	if len(m.notes) != 0 {
		t.Errorf("tablero de BD vacía no debe tener entidades; hay %d", len(m.notes))
	}
	if !m.sess.Alive() {
		t.Error("sesión debería estar activa en el tablero")
	}
}

func TestShortPasswordRejectedInSetup(t *testing.T) {
	m, _ := newTestModel(t)

	m.input.SetValue("corta")
	m = pressEnter(m)
	if m.errMsg == "" || m.sess.HasVault() {
		t.Fatalf("debería rechazar contraseña corta: err=%q", m.errMsg)
	}
}

func TestWrongPasswordKeepsLockScreen(t *testing.T) {
	m, _ := newTestModel(t)
	if _, err := crypto.CreateVault(m.st, "correcta-larga-99"); err != nil {
		t.Fatal(err)
	}

	m2 := New(m.sess, m.st)
	m2.width, m2.height = 100, 40
	if m2.state != viewLocked {
		t.Fatalf("con bóveda y sin sesión el estado inicial debe ser viewLocked")
	}

	m2.input.SetValue("equivocada-total")
	m2 = pressEnter(m2)
	if m2.state != viewLocked || m2.errMsg == "" {
		t.Fatalf("clave incorrecta debe quedarse en lock con error: state=%d err=%q",
			m2.state, m2.errMsg)
	}
	if v := m2.input.Value(); v != "" {
		t.Errorf("el input debe limpiarse tras fallo; contiene %q", v)
	}

	m2.input.SetValue("correcta-larga-99")
	m2 = pressEnter(m2)
	if m2.state != viewBoard || m2.errMsg != "" {
		t.Fatalf("clave correcta debe abrir tablero: state=%d err=%q", m2.state, m2.errMsg)
	}
}

func TestLockEventWipesPlaintextNotes(t *testing.T) {
	m, _ := newTestModel(t)
	if _, err := crypto.CreateVault(m.st, testPw); err != nil {
		t.Fatal(err)
	}
	m2 := New(m.sess, m.st)
	m2.width, m2.height = 100, 40

	m2.input.SetValue(testPw)
	m2 = pressEnter(m2)
	if m2.state != viewBoard {
		t.Fatalf("setup: estado=%d", m2.state)
	}
	m2.notes = []store.Note{{ID: 7, Title: "secreto", Body: "texto en claro"}}

	// En producción el evento llega cuando el TTL/manual ya bloqueó:
	// simulamos el bloqueo real y luego el aviso al TUI.
	m2.sess.Lock()
	out, _ := m2.Update(lockEventMsg{})
	m3 := out.(Model)
	if m3.state != viewLocked {
		t.Fatalf("lock event debe llevar a viewLocked; got %d", m3.state)
	}
	if len(m3.notes) != 0 {
		t.Error("las entidades descifradas deben eliminarse de memoria al bloquear")
	}
	if m3.input.Value() != "" {
		t.Error("el campo de contraseña debe quedar vacío")
	}

	// Re-unlock vuelve al tablero.
	m3.input.SetValue(testPw)
	m3 = pressEnter(m3)
	if m3.state != viewBoard {
		t.Fatalf("re-unlock: estado=%d", m3.state)
	}
}

func TestQuitKeysOnlyOnBoard(t *testing.T) {
	m, _ := newTestModel(t)
	if m.state != viewSetup {
		t.Fatal("precondición")
	}

	// 'q' mientras se escribe contraseña NO debe salir (es texto).
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m2 := out.(Model)
	if cmd != nil && quitRequested(cmd) {
		t.Error("'q' no debe salir de la pantalla de setup")
	}
	if m2.input.Value() != "q" {
		t.Errorf("'q' debe escribirse en el input; value=%q", m2.input.Value())
	}

	// ctrl+c siempre sale.
	_, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !quitRequested(cmd2) {
		t.Error("ctrl+c debe solicitar salida")
	}
}

func quitRequested(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	_, isQuit := msg.(tea.QuitMsg)
	return isQuit
}
