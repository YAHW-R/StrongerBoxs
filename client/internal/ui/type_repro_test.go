package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSecretEditorTypingByKeys garantiza que los campos del editor de vault
// reciben teclas reales (regresión: inputs sin foco ignoraban todo).
func TestSecretEditorTypingByKeys(t *testing.T) {
	m := unlockedModel(t)
	m = runCommand(m, "v secretos")
	m = runCommand(m, "new web")

	if len(m.ed.secFields) == 0 {
		t.Fatal("sin campos")
	}

	// Índice 0 = Título.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mi")})
	m = out.(Model)
	if got := m.ed.title.Value(); got != "mi" {
		t.Fatalf("título no recibe teclas: %q", got)
	}

	// Tab → primer campo de la plantilla (url en 'web').
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = out.(Model)
	if m.ed.field != 1 {
		t.Fatalf("tab debe llevar al campo 1; field=%d", m.ed.field)
	}
	t.Logf("focos tras tab: title=%v url=%v", m.ed.title.Focused(), m.ed.secFields[0].input.Focused())
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://")})
	m = out.(Model)
	if got := m.ed.secFields[0].input.Value(); got != "https://" {
		t.Fatalf("campo url no recibe teclas: %q", got)
	}

	// Tab → segundo campo (username).
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = out.(Model)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("admin")})
	m = out.(Model)
	if got := m.ed.secFields[1].input.Value(); got != "admin" {
		t.Fatalf("campo username no recibe teclas: %q", got)
	}
}
