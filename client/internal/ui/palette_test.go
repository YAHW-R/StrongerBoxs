package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pressCtrl(m Model, key tea.KeyType) Model {
	out, _ := m.Update(tea.KeyMsg{Type: key})
	return out.(Model)
}

func TestPaletteOpensAndFilters(t *testing.T) {
	m := unlockedModel(t)

	m = pressCtrl(m, tea.KeyCtrlK)
	if !m.pal.open {
		t.Fatal("ctrl+k debe abrir la paleta")
	}
	if len(m.pal.items) < 8 {
		t.Fatalf("paleta con muy pocos comandos: %d", len(m.pal.items))
	}

	// Filtrar por texto.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("plantilla")})
	m = out.(Model)
	for _, it := range m.pal.items {
		if !strings.Contains(strings.ToLower(it.label+it.hint), "plantilla") {
			t.Fatalf("filtro dejó pasar %q", it.label)
		}
	}
	if len(m.pal.items) == 0 {
		t.Fatal("el filtro debería encontrar 'Nueva plantilla…'")
	}

	// Esc cierra sin ejecutar nada.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.pal.open || m.ed.open {
		t.Fatal("esc debe cerrar la paleta")
	}
}

func TestPaletteExecutesTemplateCommand(t *testing.T) {
	m := unlockedModel(t)
	m = runCommand(m, "v secretos") // contexto vault

	// ctrl+k → filtrar 'email' → enter ⇒ crea entrada con esa plantilla.
	m = pressCtrl(m, tea.KeyCtrlK)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("email")})
	m = out.(Model)
	found := false
	for _, it := range m.pal.items {
		if strings.Contains(it.label, "email") {
			found = true
		}
	}
	if !found {
		t.Fatalf("la paleta no lista la plantilla email: %+v", m.pal.items)
	}

	m = pressEnter(m) // ejecuta el primer match
	if !m.ed.open || m.ed.tplName != "email" {
		t.Fatalf("paleta debió crear entrada email; tpl=%q", m.ed.tplName)
	}
}

func TestPaletteContextAware(t *testing.T) {
	m := unlockedModel(t)

	// En NOTAS: sin entradas de vault ni colores.
	m = pressCtrl(m, tea.KeyCtrlK)
	hasColor := false
	hasVaultNew := false
	for _, it := range m.pal.items {
		if strings.HasPrefix(it.label, "Color:") {
			hasColor = true
		}
		if strings.HasPrefix(it.label, "Nueva entrada:") {
			hasVaultNew = true
		}
	}
	if !hasColor || hasVaultNew {
		t.Errorf("contexto notas incorrecto (color=%v vault=%v)", hasColor, hasVaultNew)
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)

	// En VAULT: al revés.
	m = runCommand(m, "v s")
	m = pressCtrl(m, tea.KeyCtrlK)
	hasColor, hasVaultNew = false, false
	for _, it := range m.pal.items {
		if strings.HasPrefix(it.label, "Color:") {
			hasColor = true
		}
		if strings.HasPrefix(it.label, "Nueva entrada:") {
			hasVaultNew = true
		}
	}
	if hasColor || !hasVaultNew {
		t.Errorf("contexto vault incorrecto (color=%v vault=%v)", hasColor, hasVaultNew)
	}
}

func TestGlobalShortcutsBoard(t *testing.T) {
	m := unlockedModel(t)

	// ctrl+o crea nota y abre editor.
	m = pressCtrl(m, tea.KeyCtrlO)
	if !m.ed.open || len(m.notes) != 1 {
		t.Fatalf("ctrl+o: open=%v notas=%d", m.ed.open, len(m.notes))
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)

	// ctrl+e edita la selección.
	m = pressCtrl(m, tea.KeyCtrlE)
	if !m.ed.open {
		t.Fatal("ctrl+e debe abrir el editor")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)

	// ctrl+d pide confirmación; n cancela; de nuevo + y borra.
	m = pressCtrl(m, tea.KeyCtrlD)
	if !m.confirmOpen {
		t.Fatal("ctrl+d debe abrir confirmación")
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = out.(Model)
	if m.confirmOpen || len(m.visibleNotes()) != 1 {
		t.Fatal("'n' debe cancelar sin borrar")
	}

	m = pressCtrl(m, tea.KeyCtrlD)
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = out.(Model)
	if len(m.visibleNotes()) != 0 {
		t.Fatal("'y' debe confirmar el borrado")
	}
}

func TestBoardColonBarRemoved(t *testing.T) {
	m := unlockedModel(t)

	// ':' ya no abre barra en el tablero: no dispara la paleta ni comandos.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m2 := out.(Model)
	if m2.pal.open {
		t.Error("':' no debe abrir la paleta")
	}
}
