package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Paleta de comandos (ctrl+k), estilo ventana emergente ----

type paletteItem struct {
	label string // texto principal
	hint  string // pista a la derecha (categoría/atajo)
	run   func(Model) (tea.Model, tea.Cmd)
}

type paletteState struct {
	open   bool
	input  textinput.Model
	items  []paletteItem // filtradas actualmente
	cursor int
	offset int // scroll de la lista
}

const paletteVisible = 8

func newPalette() paletteState {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 48
	in.Placeholder = "Buscar comando…"
	return paletteState{input: in}
}

// openPalette construye la lista según el contexto actual y abre la paleta.
func (m *Model) openPalette() {
	m.pal.open = true
	m.pal.cursor = 0
	m.pal.offset = 0
	m.errMsg, m.notice = "", ""
	m.pal.input.SetValue("")
	m.pal.input.Focus()
	m.pal.items = m.buildPaletteItems()
}

func (m *Model) closePalette() {
	m.pal.open = false
	m.pal.input.Blur()
}

func (m Model) buildPaletteItems() []paletteItem {
	var items []paletteItem
	add := func(label, hint string, run func(Model) (tea.Model, tea.Cmd)) {
		items = append(items, paletteItem{label: label, hint: hint, run: run})
	}

	if m.board == secSecrets {
		add("Nueva entrada (simple)", "vault", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdNew("") })
		for _, t := range m.loadVaultTemplates() {
			tt := t
			if tt.Name == defaultTemplate {
				continue // ya está arriba como entrada por defecto
			}
			add(fmt.Sprintf("Nueva entrada: %s %s", tt.Icon, tt.Name), "plantilla",
				func(mm Model) (tea.Model, tea.Cmd) { return mm.secretNew(tt.Name) })
		}
		add("Nueva plantilla…", ":newp", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdNewp("") })
		add("Cambiar a NOTAS", "vista", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdSwitchView("notas") })
		add("Copiar valor secreto", "y", func(mm Model) (tea.Model, tea.Cmd) { return mm.yankPassword() })
	} else {
		add("Nueva nota", "ctrl+o", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdNew("") })
		for _, t := range builtinVaultTemplates()[1:] { // web, email, nota
			tt := t
			add(fmt.Sprintf("Crear plantilla %s y usar", tt.Title), "bóveda",
				func(mm Model) (tea.Model, tea.Cmd) { mm.board = secSecrets; return mm.secretNew(tt.Name) })
		}
		add("Convertir nota en entrada de bóveda", ":tv", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdTovault() })
		add("Cambiar a VAULT", "vista", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdSwitchView("secretos") })
		for es, hexVal := range map[string]string{
			"amarillo": "#F9AB00", "verde": "#34A853", "azul": "#4285F4",
			"rojo": "#EA4335", "violeta": "#7C4DFF", "turquesa": "#00BFA5", "rosa": "#F06292",
		} {
			name, _ := es, hexVal
			add("Color: "+name, "nota", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdColor(name) })
		}
	}

	add("Editar selección", "ctrl+e", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdEdit() })
	add("Borrar selección…", "ctrl+d", func(mm Model) (tea.Model, tea.Cmd) { return mm.askDelete() })
	add("Fijar / desfijar", ":pin", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdTogglePin() })
	add("Archivar / restaurar", ":arch", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdToggleArch() })

	if m.board == secNotes {
		add("Mostrar archivadas", ":all", func(mm Model) (tea.Model, tea.Cmd) {
			mm.showArchived = !mm.showArchived
			mm.refresh()
			return mm, nil
		})
	} else {
		add("Listar plantillas", ":tmpl", func(mm Model) (tea.Model, tea.Cmd) { return mm.cmdListTemplates() })
	}

	add("Buscar…", "/", func(mm Model) (tea.Model, tea.Cmd) {
		mm.searchFocus = true
		mm.searchLn.Focus()
		return mm, textinput.Blink
	})
	add("Sincronizar ahora", "ctrl+s", func(mm Model) (tea.Model, tea.Cmd) {
		mm.requestSync()
		mm.notice = "⟳ Sincronizando cambios pendientes…"
		return mm, nil
	})
	add("Ayuda", "?", func(mm Model) (tea.Model, tea.Cmd) { mm.showHelp = true; return mm, nil })
	add("Salir", "q", func(mm Model) (tea.Model, tea.Cmd) { return mm, tea.Quit })

	return items
}

func filterPalette(items []paletteItem, q string) []paletteItem {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return items
	}
	out := make([]paletteItem, 0, len(items))
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label), q) ||
			strings.Contains(strings.ToLower(it.hint), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closePalette()
		return m, nil
	case tea.KeyUp:
		if m.pal.cursor > 0 {
			m.pal.cursor--
		}
		m.pal.offset = clampOffset(m.pal.cursor, m.pal.offset, len(m.pal.items))
		return m, nil
	case tea.KeyDown:
		if m.pal.cursor < len(m.pal.items)-1 {
			m.pal.cursor++
		}
		m.pal.offset = clampOffset(m.pal.cursor, m.pal.offset, len(m.pal.items))
		return m, nil
	case tea.KeyEnter:
		if len(m.pal.items) == 0 {
			m.closePalette()
			return m, nil
		}
		item := m.pal.items[min(m.pal.cursor, len(m.pal.items)-1)]
		m.closePalette()
		return item.run(m)
	}

	var cmd tea.Cmd
	m.pal.input, cmd = m.pal.input.Update(msg)
	q := strings.ToLower(strings.TrimSpace(m.pal.input.Value()))
	m.pal.items = filterPalette(m.buildPaletteItems(), q)
	if m.pal.cursor >= len(m.pal.items) {
		m.pal.cursor = max(0, len(m.pal.items)-1)
	}
	m.pal.offset = clampOffset(m.pal.cursor, m.pal.offset, len(m.pal.items))
	return m, cmd
}

// clampOffset mantiene el cursor dentro de la ventana visible de la lista.
func clampOffset(cur, off, n int) int {
	vis := min(paletteVisible, n)
	if cur < off {
		off = cur
	}
	if cur >= off+vis {
		off = cur - vis + 1
	}
	maxOff := max(0, n-vis)
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	return off
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	paletteBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorTeal).
			Padding(0, 1)

	paletteSelStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3A3A52")).
			Foreground(lipgloss.Color("#FFFFFF"))

	paletteHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7A7A9A"))
)

// viewPalette dibuja la ventana emergente centrada sobre un fondo atenuado.
func (m Model) viewPalette() string {
	width := m.width - 12
	if width > 64 {
		width = 64
	}
	if width < 32 {
		width = 32
	}

	var b strings.Builder
	b.WriteString(m.pal.input.View())
	b.WriteString("\n")

	if len(m.pal.items) == 0 {
		b.WriteString(helpStyle.Render("  sin coincidencias"))
	}

	end := min(m.pal.offset+paletteVisible, len(m.pal.items))
	for i := m.pal.offset; i < end; i++ {
		it := m.pal.items[i]
		line := " " + it.label
		gap := width - 6 - len([]rune(it.label)) - len([]rune(it.hint))
		if gap < 2 {
			gap = 2
		}
		hint := paletteHintStyle.Render(it.hint)
		row := line + strings.Repeat(" ", gap) + hint
		if i == m.pal.cursor {
			b.WriteString(paletteSelStyle.Render(row))
		} else {
			b.WriteString(row)
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	if len(m.pal.items) > paletteVisible {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render(fmt.Sprintf("  %d/%d", m.pal.cursor+1, len(m.pal.items))))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorViolet).
		Padding(1, 1).
		Width(width)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center,
			box.Render(b.String()),
			helpStyle.Render("↑↓ elegir · enter ejecutar · esc cerrar"),
		))
}
