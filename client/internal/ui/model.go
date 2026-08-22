package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yahwr/strongboxs/client/internal/crypto"
	"github.com/yahwr/strongboxs/client/internal/session"
	"github.com/yahwr/strongboxs/client/internal/store"
)

type viewState int

const (
	viewSetup viewState = iota // primer arranque: crear contraseña maestra
	viewLocked                 // pedir contraseña
	viewBoard                  // tablero de notas descifrado
)

const (
	minMasterLen   = 8
	countdownEvery = 30 * time.Second
)

type (
	lockEventMsg struct{} // la sesión se bloqueó (manual o por TTL)
	tickMsg      time.Time
)

// editorState es la nota en edición (modal sobre el tablero).
type editorState struct {
	open  bool
	id    int64 // ID de la nota; 0 no ocurre (:new crea antes de abrir)
	title textinput.Model
	body  textarea.Model
	field int // 0 título · 1 cuerpo
	cmd   bool
	cmdLn textinput.Model
}

func newEditorState() editorState {
	t := textinput.New()
	t.Placeholder = "Título"
	t.CharLimit = 120
	t.Width = 38

	b := textarea.New()
	b.Placeholder = "Escribe tu nota…"
	b.SetHeight(7)
	b.SetWidth(44)
	b.ShowLineNumbers = false

	c := textinput.New()
	c.Prompt = ":"
	c.CharLimit = 32

	return editorState{title: t, body: b, cmdLn: c}
}

// Model es el estado raíz de la app BubbleTea.
type Model struct {
	sess *session.Manager
	st   *store.Store

	state         viewState
	width, height int

	entities     []store.Note // descifradas SOLO mientras hay sesión viva
	selIdx       int
	showArchived bool
	showHelp     bool

	cmdFocus       bool // barra de comandos activa en el tablero
	cmdLine        textinput.Model
	ed             editorState
	input          textinput.Model // contraseña (setup / lock-screen)
	setting        bool
	firstPw        string
	errMsg         string
}

func New(sess *session.Manager, st *store.Store) Model {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 128
	ti.Width = 36
	ti.Placeholder = "Contraseña maestra"
	ti.Focus()

	m := Model{
		sess:    sess,
		st:      st,
		input:   ti,
		cmdLine: newCommandLine(),
		ed:      newEditorState(),
	}

	switch {
	case !sess.HasVault():
		m.state = viewSetup
	case sess.Alive():
		m.state = viewBoard
		m.refresh()
	default:
		m.state = viewLocked
	}
	return m
}

func newCommandLine() textinput.Model {
	c := textinput.New()
	c.Prompt = ":"
	c.CharLimit = 64
	c.Width = 50
	return c
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		waitForLock(m.sess),
		countdownTick(),
	)
}

func waitForLock(sess *session.Manager) tea.Cmd {
	ch := sess.LockEvents()
	return func() tea.Msg {
		<-ch
		return lockEventMsg{}
	}
}

func countdownTick() tea.Cmd {
	return tea.Tick(countdownEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case lockEventMsg:
		if !m.sess.Alive() && m.state != viewSetup {
			m.enterLocked()
		}
		return m, waitForLock(m.sess)

	case tickMsg:
		if m.state == viewBoard && !m.sess.Alive() {
			m.enterLocked()
		}
		return m, countdownTick()

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch {
		case m.ed.open:
			return m.handleEditorKey(msg)
		case m.state == viewBoard && m.cmdFocus:
			return m.handleCmdKey(msg)
		}

		switch m.state {
		case viewBoard:
			return m.handleBoardKey(msg)

		case viewLocked:
			if msg.Type == tea.KeyEnter {
				return m.submitUnlock()
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case viewSetup:
			if msg.Type == tea.KeyEnter {
				return m.submitSetup()
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// ---- teclado: tablero ----

func (m Model) handleBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case ":":
		m.errMsg = ""
		m.cmdLine.SetValue("")
		m.cmdFocus = true
		m.cmdLine.Focus()
		return m, textinput.Blink

	case "?":
		m.showHelp = !m.showHelp
		return m, nil

	case "q":
		return m, tea.Quit

	case "esc":
		m.showHelp = false
		m.errMsg = ""
		return m, nil

	case "j", "down":
		m.moveSel(1)
		return m, nil

	case "k", "up":
		m.moveSel(-1)
		return m, nil

	case "g", "home":
		m.selIdx = 0
		return m, nil

	case "G", "end":
		m.selIdx = len(m.entities) - 1
		if m.selIdx < 0 {
			m.selIdx = 0
		}
		return m, nil

	case "enter", "e":
		if !m.hasSelection() {
			m.setErr("No hay notas: crea una con :new")
			return m, nil
		}
		m.openEditor(m.entities[m.selIdx])
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) handleCmdKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeCmdBar()
		return m, nil
	case tea.KeyEnter:
		raw := m.cmdLine.Value()
		m.closeCmdBar()
		return m.executeCommand(raw)
	}
	var cmd tea.Cmd
	m.cmdLine, cmd = m.cmdLine.Update(msg)
	return m, cmd
}

func (m *Model) closeCmdBar() {
	m.cmdFocus = false
	m.cmdLine.Blur()
	m.cmdLine.SetValue("")
}

// ---- ejecución de comandos (tablero) ----

func (m Model) executeCommand(raw string) (tea.Model, tea.Cmd) {
	name, args, ok := parseCommand(raw)
	if !ok {
		return m, nil
	}
	m.errMsg = ""

	switch name {
	case "new", "n":
		return m.cmdNew(args)
	case "edit", "e":
		return m.cmdEdit()
	case "delete", "del", "d", "rm":
		return m.cmdDelete()
	case "pin":
		return m.cmdToggle(func(n *store.Note) { n.Pinned = !n.Pinned })
	case "archive", "arch":
		return m.cmdToggle(func(n *store.Note) { n.Archived = !n.Archived })
	case "color":
		return m.cmdColor(args)
	case "all":
		m.showArchived = !m.showArchived
		m.refresh()
		return m, nil
	case "help", "h":
		m.showHelp = !m.showHelp
		return m, nil
	case "w", "write":
		m.setErr("Nada que guardar: los cambios se cifran al instante.")
		return m, nil
	case "q", "quit", "qa", "q!":
		return m, tea.Quit
	default:
		m.setErr(fmt.Sprintf("Comando desconocido: %s (prueba :help)", name))
		return m, nil
	}
}

func (m Model) cmdNew(title string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(title) == "" {
		title = "Nueva nota"
	}
	sealedTitle, err := m.sess.SealField(title)
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	sealedBody, err := m.sess.SealField("")
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	created, err := m.st.CreateNote(sealedTitle, sealedBody, "")
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	for i := range m.entities {
		if m.entities[i].ID == created.ID {
			m.selIdx = i
			break
		}
	}
	m.openEditor(m.entities[m.selIdx])
	return m, textinput.Blink
}

func (m Model) cmdEdit() (tea.Model, tea.Cmd) {
	if !m.hasSelection() {
		m.setErr("No hay notas: crea una con :new")
		return m, nil
	}
	m.openEditor(m.entities[m.selIdx])
	return m, textinput.Blink
}

func (m Model) cmdDelete() (tea.Model, tea.Cmd) {
	if !m.hasSelection() {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	id := m.entities[m.selIdx].ID
	if err := m.st.SoftDeleteNote(id); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

func (m Model) cmdToggle(mutate func(*store.Note)) (tea.Model, tea.Cmd) {
	if !m.hasSelection() {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	n := m.entities[m.selIdx]
	mutate(&n)
	if err := m.persistNote(n); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

func (m Model) cmdColor(arg string) (tea.Model, tea.Cmd) {
	hex, ok := colorByName(arg)
	if !ok {
		m.setErr("Color inválido: usa " + colorNamesList())
		return m, nil
	}
	n := m.entities[m.selIdx]
	n.Color = hex
	if err := m.persistNote(n); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

// ---- persistencia ----

// persistNote cifra título/cuerpo y escribe la fila; la entidad en memoria
// conserva texto en claro solo para la vista actual.
func (m Model) persistNote(n store.Note) error {
	db := n
	var err error
	if db.Title, err = m.sess.SealField(n.Title); err != nil {
		return err
	}
	if db.Body, err = m.sess.SealField(n.Body); err != nil {
		return err
	}
	return m.st.UpdateNote(&db)
}

// refresh recarga entidades descifradas desde la BD y ajusta el cursor.
func (m *Model) refresh() {
	list, err := m.st.ListNotes(m.showArchived)
	m.entities = nil
	if err != nil {
		m.setErr(err.Error())
	} else {
		for _, sn := range list {
			sn.Title = m.unsealOr(sn.Title, "(sin título)")
			sn.Body = m.unsealOr(sn.Body, "")
			m.entities = append(m.entities, sn)
		}
	}
	m.clampSel()
	m.sess.Touch()
}

func (m *Model) unsealOr(envelope, fallback string) string {
	s, err := m.sess.UnsealField(envelope)
	if err != nil || s == "" {
		return fallback
	}
	return s
}

// ---- cursor ----

func (m Model) hasSelection() bool {
	return len(m.entities) > 0 && m.selIdx >= 0 && m.selIdx < len(m.entities)
}

func (m *Model) clampSel() {
	if m.selIdx >= len(m.entities) {
		m.selIdx = len(m.entities) - 1
	}
	if m.selIdx < 0 {
		m.selIdx = 0
	}
}

func (m *Model) moveSel(delta int) {
	if len(m.entities) == 0 {
		return
	}
	m.selIdx += delta
	m.clampSel()
}

// ---- mensajes ----

func (m *Model) setErr(s string) { m.errMsg = s }

// ---- autenticación (setup / lock) ----

func (m Model) submitUnlock() (tea.Model, tea.Cmd) {
	pw := m.input.Value()
	if pw == "" {
		m.errMsg = "Escribe tu contraseña maestra."
		return m, nil
	}
	if err := m.sess.UnlockWith(pw); err != nil {
		if errors.Is(err, crypto.ErrWrongPassword) {
			m.errMsg = "Contraseña incorrecta."
		} else {
			m.errMsg = err.Error()
		}
		m.input.SetValue("")
		return m, nil
	}
	m.errMsg = ""
	m.input.SetValue("")
	m.state = viewBoard
	m.refresh()
	return m, nil
}

func (m Model) submitSetup() (tea.Model, tea.Cmd) {
	pw := m.input.Value()
	m.errMsg = ""

	if len(pw) < minMasterLen {
		m.errMsg = fmt.Sprintf("Mínimo %d caracteres.", minMasterLen)
		m.input.SetValue("")
		return m, nil
	}
	if !m.setting {
		m.firstPw = pw
		m.setting = true
		m.input.SetValue("")
		m.input.Placeholder = "Repite la contraseña"
		return m, textinput.Blink
	}
	if pw != m.firstPw {
		m.setting = false
		m.firstPw = ""
		m.input.SetValue("")
		m.input.Placeholder = "Contraseña maestra"
		m.errMsg = "No coinciden; empieza de nuevo."
		return m, nil
	}
	first := m.firstPw
	m.firstPw = ""
	m.setting = false
	m.input.SetValue("")
	if err := m.sess.CreateWith(first); err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.state = viewBoard
	m.refresh()
	return m, nil
}

// enterLocked limpia todo texto en claro de memoria y muestra el candado.
func (m *Model) enterLocked() {
	m.entities = nil
	m.state = viewLocked
	m.setting = false
	m.firstPw = ""
	m.errMsg = ""
	m.closeCmdBar()
	m.closeEditor()
	m.showHelp = false
	m.input.SetValue("")
	m.input.Focus()
}

// ---- editor ----

func (m *Model) openEditor(n store.Note) {
	m.ed.open = true
	m.ed.id = n.ID
	m.ed.title.SetValue(n.Title)
	m.ed.body.SetValue(n.Body)
	m.ed.field = 0
	m.ed.title.Focus()
	m.ed.body.Blur()
	m.ed.cmd = false
	m.ed.cmdLn.SetValue("")
	m.errMsg = ""
}

func (m *Model) closeEditor() {
	m.ed.open = false
	m.ed.id = 0
	m.ed.cmd = false
	m.ed.title.Blur()
	m.ed.body.Blur()
	m.ed.cmdLn.Blur()
}

// saveEditor persiste el búfer del editor; con close=true vuelve al tablero.
func (m Model) saveEditor(close bool) (tea.Model, tea.Cmd) {
	var target *store.Note
	for i := range m.entities {
		if m.entities[i].ID == m.ed.id {
			target = &m.entities[i]
			break
		}
	}
	if target == nil {
		m.setErr("La nota ya no existe.")
		m.closeEditor()
		return m, nil
	}
	target.Title = m.ed.title.Value()
	target.Body = m.ed.body.Value()
	if strings.TrimSpace(target.Title) == "" {
		target.Title = "(sin título)"
	}
	if err := m.persistNote(*target); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	if close {
		m.closeEditor()
	}
	return m, nil
}

func (m Model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ed.cmd {
		switch msg.Type {
		case tea.KeyEsc:
			m.ed.cmd = false
			m.ed.cmdLn.SetValue("")
			m.ed.cmdLn.Blur()
			return m, nil
		case tea.KeyEnter:
			raw := m.ed.cmdLn.Value()
			m.ed.cmd = false
			m.ed.cmdLn.SetValue("")
			m.ed.cmdLn.Blur()
			return m.execEditorCmd(raw)
		}
		var cmd tea.Cmd
		m.ed.cmdLn, cmd = m.ed.cmdLn.Update(msg)
		return m, cmd
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.closeEditor()
		return m, nil
	case tea.KeyTab:
		m.ed.field = 1 - m.ed.field
		m.refocusEditorField()
		return m, textinput.Blink
	case tea.KeyCtrlS:
		return m.saveEditor(false)
	}
	if msg.String() == ":" {
		m.ed.cmd = true
		m.ed.cmdLn.Focus()
		return m, textinput.Blink
	}

	var cmd tea.Cmd
	switch m.ed.field {
	case 0:
		m.ed.title, cmd = m.ed.title.Update(msg)
	default:
		var bodyCmd tea.Cmd
		m.ed.body, bodyCmd = m.ed.body.Update(msg)
		cmd = bodyCmd
	}
	return m, cmd
}

func (m *Model) refocusEditorField() {
	switch m.ed.field {
	case 0:
		m.ed.title.Focus()
		m.ed.body.Blur()
	default:
		m.ed.title.Blur()
		m.ed.body.Focus()
	}
}

func (m Model) execEditorCmd(raw string) (tea.Model, tea.Cmd) {
	name, _, ok := parseCommand(raw)
	if !ok {
		return m, nil
	}
	m.errMsg = ""
	switch name {
	case "w", "write":
		return m.saveEditor(false)
	case "wq", "x":
		return m.saveEditor(true)
	case "q", "quit", "q!":
		m.closeEditor()
		return m, nil
	case "help", "h":
		m.showHelp = true
		return m, nil
	default:
		m.setErr("En el editor: :w :wq :x :q :q!")
		return m, nil
	}
}

// ---- vista raíz ----

func (m Model) View() string {
	switch {
	case m.ed.open:
		return m.viewEditor()
	}
	switch m.state {
	case viewSetup:
		subtitle := "Elige una contraseña maestra (mínimo 8 caracteres)."
		if m.setting {
			subtitle = "Repite la contraseña para confirmar."
		}
		return m.viewAuth("🔑 Primera ejecución", subtitle)
	case viewLocked:
		return m.viewAuth("🔒 Strongboxs bloqueado", "Introduce tu contraseña maestra.")
	default:
		return m.viewBoard()
	}
}

func (m Model) viewAuth(title, subtitle string) string {
	lines := []string{
		appTitleStyle.Render("STRONGBOXS"),
		"",
		title,
		subtitle,
		"",
		m.input.View(),
	}
	if m.errMsg != "" {
		lines = append(lines, "", errStyle.Render("✗ "+m.errMsg))
	}
	body := lipgloss.JoinVertical(lipgloss.Center,
		authBoxStyle.Render(strings.Join(lines, "\n")),
		"",
		helpStyle.Render("enter · continuar    ctrl+c · salir"),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) statusLine() string {
	if m.cmdFocus {
		return helpStyle.Render("enter · ejecutar    esc · cancelar")
	}
	if !m.sess.Alive() {
		return errStyle.Render("🔒 sesión bloqueada")
	}
	d := m.sess.Remaining()
	if d < 0 {
		d = 0
	}
	mins := int((d + time.Minute - 1) / time.Minute) // techo
	return helpStyle.Render(fmt.Sprintf(
		"j/k mover · : comandos · ? ayuda · 🔓 %d min    q salir", mins))
}
