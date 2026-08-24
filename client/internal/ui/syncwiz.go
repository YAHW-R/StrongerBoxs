package ui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/sync"
)

// SyncRuntime conecta la UI con el motor de sincronización sin acoplarse
// a su implementación ni al sistema de archivos.
type SyncRuntime struct {
	// Trigger dispara un push reactivo (coalescido). Opcional hasta arrancar.
	Trigger func()
	// Gate pausa el PULL mientras la UI está ocupada. Opcional.
	Gate *sync.Gate
	// Start valida, persiste la configuración y arranca el motor.
	// Devuelve true si quedó activo.
	Start func(sync.Credentials) bool
	// IsConfigured: ¿ya hay una cuenta de sync lista?
	IsConfigured func() bool
	// IsDeclined: ¿el usuario dijo "no ahora" en esta instalación?
	IsDeclined func() bool
	// MarkDeclined recuerda el "no ahora".
	MarkDeclined func()

	manager atomic.Pointer[sync.Manager]
}

// SetManager registra el motor activo.
func (r *SyncRuntime) SetManager(m *sync.Manager) { r.manager.Store(m) }

// Manager devuelve el motor activo (nil si la sync no está iniciada).
func (r *SyncRuntime) Manager() *sync.Manager {
	if r == nil {
		return nil
	}
	return r.manager.Load()
}

type syncWizardState struct {
	open  bool
	auto  bool // abierto automáticamente al inicio
	url   textinput.Model
	user  textinput.Model
	pass  textinput.Model
	field int // 0..2
}

func newSyncWizard() syncWizardState {
	return syncWizardState{
		url:  newTextInput("http://localhost:8000", false),
		user: newTextInput("usuario", false),
		pass: newTextInput("contraseña de cuenta", true),
	}
}

// openSyncWizard abre el asistente. Con auto=true, cancelar marca "no ahora".
func (m *Model) openSyncWizard(auto bool) {
	if m.wiz == nil {
		w := newSyncWizard()
		m.wiz = &w
	}
	m.setBusy(true)
	m.wiz.open = true
	m.wiz.auto = auto
	m.wiz.field = 0
	m.errMsg, m.notice = "", ""
	m.blurAllEditorWidgets()
	m.applyResponsiveSizes()
	switch m.wiz.field {
	case 0:
		m.wiz.url.Focus()
	case 1:
		m.wiz.user.Focus()
	default:
		m.wiz.pass.Focus()
	}
}

func (m *Model) closeSyncWizard() {
	if m.wiz == nil {
		return
	}
	m.wiz.open = false
	m.wiz.url.Blur()
	m.wiz.user.Blur()
	m.wiz.pass.Blur()
	m.setBusy(false)
}

func (m Model) handleWizardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.wiz
	switch msg.Type {
	case tea.KeyEsc:
		wasAuto := w.auto
		m.closeSyncWizard()
		if wasAuto && m.rt != nil && m.rt.MarkDeclined != nil {
			m.rt.MarkDeclined()
			m.notice = "Sincronización omitida (configúrala luego con ctrl+k)"
		}
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		delta := 1
		if msg.Type == tea.KeyShiftTab {
			delta = -1
		}
		w.field = ((w.field+delta)%3 + 3) % 3
		m.blurAllEditorWidgets()
		switch w.field {
		case 0:
			w.url.Focus()
		case 1:
			w.user.Focus()
		default:
			w.pass.Focus()
		}
		return m, textinput.Blink
	case tea.KeyCtrlS:
		creds := sync.Credentials{
			BaseURL:  strings.TrimSpace(w.url.Value()),
			Username: strings.ToLower(strings.TrimSpace(w.user.Value())),
			Password: w.pass.Value(),
		}
		if _, err := sync.ValidateCredentials(creds); err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		if m.rt == nil || m.rt.Start == nil {
			m.setErr("Sincronización no disponible en esta sesión")
			return m, nil
		}
		if !m.rt.Start(creds) {
			m.setErr("No se pudo iniciar la sincronización (revisa los datos)")
			return m, nil
		}
		m.closeSyncWizard()
		m.notice = "✓ Sincronización configurada y activa"
		return m, nil
	}

	var cmd tea.Cmd
	switch w.field {
	case 0:
		w.url, cmd = w.url.Update(msg)
	case 1:
		w.user, cmd = w.user.Update(msg)
	default:
		w.pass, cmd = w.pass.Update(msg)
	}
	return m, cmd
}

// viewSyncWizard: asistente de primera configuración.
func (m Model) viewSyncWizard() string {
	w := m.wiz
	marks := []string{"  ", "  ", "  "}
	marks[w.field] = "▸"

	lines := []string{
		appTitleStyle.Render("☁ SINCRONIZACIÓN"),
		"",
		"Conecta este equipo para respaldar tus notas y claves",
		"(viaja solo cifrado; tu maestra nunca sale de aquí).",
		"",
		marks[0] + " Servidor",
		w.url.View(),
		"",
		marks[1] + " Usuario",
		w.user.View(),
		"",
		marks[2] + " Contraseña de cuenta",
		w.pass.View(),
	}
	if m.errMsg != "" {
		lines = append(lines, "", errStyle.Render("✗ "+m.errMsg))
	}
	lines = append(lines,
		"",
		helpStyle.Render("tab · campo    ctrl+s · conectar    esc · más tarde"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorTeal).
		Padding(1, 3)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		box.Render(strings.Join(lines, "\n")))
}

var _ = fmt.Sprintf // reservado para formato futuro
