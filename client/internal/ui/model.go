package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
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
	viewSetup  viewState = iota // primer arranque: crear contraseña maestra
	viewLocked                  // pedir contraseña
	viewBoard                   // tablero (notas o secretos)
)

type boardView int

const (
	secNotes   boardView = iota // notas estilo Keep
	secSecrets                  // bóveda de contraseñas
)

const (
	minMasterLen   = 8
	countdownEvery = 30 * time.Second
	maskText       = "••••••••"
)

type (
	lockEventMsg struct{} // la sesión se bloqueó (manual o por TTL)
	tickMsg      time.Time
)

// editorState es el modal de creación/edición de notas y secretos.
type editorState struct {
	open bool
	sec  boardView
	id   int64

	title textinput.Model
	body  textarea.Model

	user   textinput.Model
	pass   textinput.Model
	url    textinput.Model
	field  int  // nota: 0..1 · secreto: 0..4
	reveal bool // mostrar contraseña en claro mientras se edita

	cmd   bool
	cmdLn textinput.Model
}

func newEditorInputs() (textinput.Model, textarea.Model, textinput.Model, textinput.Model, textinput.Model, textinput.Model) {
	t := textinput.New()
	t.Placeholder = "Título"
	t.CharLimit = 120
	t.Width = 38

	b := textarea.New()
	b.Placeholder = "Escribe tu nota…"
	b.SetHeight(7)
	b.SetWidth(44)
	b.ShowLineNumbers = false

	u := textinput.New()
	u.Placeholder = "Usuario"
	u.CharLimit = 128
	u.Width = 38

	p := textinput.New()
	p.Placeholder = "Contraseña"
	p.CharLimit = 256
	p.Width = 38
	p.EchoMode = textinput.EchoPassword
	p.EchoCharacter = '•'

	url := textinput.New()
	url.Placeholder = "https://…"
	url.CharLimit = 256
	url.Width = 38

	return t, b, u, p, url, newCmdInput(32)
}

func newCmdInput(limit int) textinput.Model {
	c := textinput.New()
	c.Prompt = ":"
	c.CharLimit = limit
	c.Width = 50
	return c
}

func newEditorState() editorState {
	t, b, u, p, url, c := newEditorInputs()
	return editorState{title: t, body: b, user: u, pass: p, url: url, cmdLn: c}
}

// Model es el estado raíz de la app BubbleTea.
type Model struct {
	sess *session.Manager
	st   *store.Store

	state         viewState
	board         boardView
	width, height int

	notes   []store.Note   // descifradas SOLO con sesión viva
	secrets []store.Secret // descifradas ídem

	query       string // filtro '/' activo
	searchFocus bool   // escribiendo en la barra '/'
	searchLn    textinput.Model
	revealAll   bool // 'v': mostrar contraseñas en las tarjetas

	selIdx       int // sobre la lista VISIBLE de la vista actual
	showArchived bool
	showHelp     bool

	cmdFocus bool
	cmdLine  textinput.Model
	ed       editorState

	input   textinput.Model // contraseña maestra (setup / lock-screen)
	setting bool
	firstPw string
	errMsg  string
	notice  string
}

func New(sess *session.Manager, st *store.Store) Model {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 128
	ti.Width = 36
	ti.Placeholder = "Contraseña maestra"
	ti.Focus()

	sl := textinput.New()
	sl.Prompt = "/"
	sl.CharLimit = 64
	sl.Width = 40
	sl.Placeholder = "filtrar…"

	m := Model{
		sess:     sess,
		st:       st,
		input:    ti,
		cmdLine:  newCmdInput(64),
		ed:       newEditorState(),
		searchLn: sl,
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
		case m.searchFocus:
			return m.handleSearchKey(msg)
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

// ---- búsqueda ----

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.clearSearch()
		return m, nil
	case tea.KeyEnter:
		m.searchFocus = false
		m.searchLn.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.searchLn, cmd = m.searchLn.Update(msg)
	m.query = strings.ToLower(strings.TrimSpace(m.searchLn.Value()))
	m.clampSel()
	return m, cmd
}

func (m *Model) clearSearch() {
	m.query = ""
	m.searchFocus = false
	m.searchLn.Blur()
	m.searchLn.SetValue("")
	m.clampSel()
}

func matchAny(q string, fields ...string) bool {
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

func (m Model) visibleNotes() []store.Note {
	out := make([]store.Note, 0, len(m.notes))
	for _, n := range m.notes {
		if n.Archived && !m.showArchived {
			continue
		}
		if m.query != "" && !matchAny(m.query, n.Title, n.Body) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m Model) visibleSecrets() []store.Secret {
	out := make([]store.Secret, 0, len(m.secrets))
	for _, s := range m.secrets {
		if m.query != "" && !matchAny(m.query, s.Title, s.Username, s.URL, s.Notes) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// visibleCount nº de elementos tras aplicar archivado+búsqueda.
func (m Model) visibleCount() int {
	if m.board == secSecrets {
		return len(m.visibleSecrets())
	}
	return len(m.visibleNotes())
}

// curNote / curSecret resuelven la selección visible a la entidad real.
func (m Model) curNote() (store.Note, bool) {
	vn := m.visibleNotes()
	if m.selIdx < 0 || m.selIdx >= len(vn) {
		return store.Note{}, false
	}
	id := vn[m.selIdx].ID
	for _, n := range m.notes {
		if n.ID == id {
			return n, true
		}
	}
	return store.Note{}, false
}

func (m Model) curSecret() (store.Secret, bool) {
	vs := m.visibleSecrets()
	if m.selIdx < 0 || m.selIdx >= len(vs) {
		return store.Secret{}, false
	}
	id := vs[m.selIdx].ID
	for _, s := range m.secrets {
		if s.ID == id {
			return s, true
		}
	}
	return store.Secret{}, false
}

// ---- teclado: tablero ----

func (m Model) handleBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case ":":
		m.errMsg, m.notice = "", ""
		m.cmdLine.SetValue("")
		m.cmdFocus = true
		m.cmdLine.Focus()
		return m, textinput.Blink

	case "/":
		m.errMsg, m.notice = "", ""
		m.searchFocus = true
		m.searchLn.Focus()
		return m, textinput.Blink

	case "?":
		m.showHelp = !m.showHelp
		return m, nil

	case "q":
		return m, tea.Quit

	case "esc":
		m.showHelp = false
		m.errMsg, m.notice = "", ""
		if m.query != "" {
			m.clearSearch()
		}
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
		m.selIdx = m.visibleCount() - 1
		m.clampSel()
		return m, nil

	case "tab", "shift+tab":
		if m.board == secNotes {
			m.board = secSecrets
		} else {
			m.board = secNotes
		}
		m.selIdx = 0
		m.notice = ""
		return m, nil

	case "v":
		if m.board == secSecrets {
			m.revealAll = !m.revealAll
		}
		return m, nil

	case "y":
		return m.yankPassword()

	case "enter", "e":
		return m.cmdEdit()
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

// ---- ejecución de comandos ----

func (m Model) executeCommand(raw string) (tea.Model, tea.Cmd) {
	name, args, ok := parseCommand(raw)
	if !ok {
		return m, nil
	}
	m.errMsg, m.notice = "", ""

	switch name {
	case "new", "n":
		return m.cmdNew(args)
	case "edit", "e":
		return m.cmdEdit()
	case "delete", "del", "d", "rm":
		return m.cmdDelete()
	case "pin":
		return m.cmdTogglePin()
	case "archive", "arch":
		return m.cmdToggleArch()
	case "color":
		return m.cmdColor(args)
	case "all":
		if m.board != secNotes {
			m.setErr("':all' solo aplica a notas.")
			return m, nil
		}
		m.showArchived = !m.showArchived
		m.refresh()
		return m, nil
	case "vault", "v":
		return m.cmdSwitchView(args)
	case "find", "f":
		m.searchLn.SetValue(args)
		m.query = strings.ToLower(strings.TrimSpace(args))
		m.clampSel()
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

func (m Model) cmdSwitchView(arg string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		if m.board == secNotes {
			m.board = secSecrets
		} else {
			m.board = secNotes
		}
	case "notas", "notes", "n":
		m.board = secNotes
	case "secretos", "secrets", "s", "contraseñas":
		m.board = secSecrets
	default:
		m.setErr("Vista inválida: usa :v notas | :v secretos")
		return m, nil
	}
	m.selIdx = 0
	m.notice = ""
	return m, nil
}

func (m Model) cmdNew(arg string) (tea.Model, tea.Cmd) {
	if m.board == secSecrets {
		return m.secretNew(arg)
	}
	title := strings.TrimSpace(arg)
	if title == "" {
		title = "Nueva nota"
	}
	st_, err := m.sess.SealField(title)
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	sb, err := m.sess.SealField("")
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	created, err := m.st.CreateNote(st_, sb, "")
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	m.selectByID(created.ID)
	m.openEditor(created.ID)
	return m, textinput.Blink
}

func (m Model) secretNew(title string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(title) == "" {
		title = "Nueva entrada"
	}
	sc := store.Secret{Title: title}
	created, err := m.createSecret(sc)
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	m.selectByID(created.ID)
	m.openEditor(created.ID)
	return m, textinput.Blink
}

func (m Model) cmdEdit() (tea.Model, tea.Cmd) {
	if m.visibleCount() == 0 {
		if m.board == secSecrets {
			m.setErr("Sin entradas: crea una con :new")
		} else {
			m.setErr("No hay notas: crea una con :new")
		}
		return m, nil
	}
	var id int64
	if m.board == secSecrets {
		s, ok := m.curSecret()
		if !ok {
			return m, nil
		}
		id = s.ID
	} else {
		n, ok := m.curNote()
		if !ok {
			return m, nil
		}
		id = n.ID
	}
	m.openEditor(id)
	return m, textinput.Blink
}

func (m Model) cmdDelete() (tea.Model, tea.Cmd) {
	var err error
	if m.board == secSecrets {
		s, ok := m.curSecret()
		if !ok {
			m.setErr("Selecciona una entrada con j/k")
			return m, nil
		}
		err = m.st.SoftDeleteSecret(s.ID)
	} else {
		n, ok := m.curNote()
		if !ok {
			m.setErr("Selecciona una nota con j/k")
			return m, nil
		}
		err = m.st.SoftDeleteNote(n.ID)
	}
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

func (m Model) cmdTogglePin() (tea.Model, tea.Cmd) {
	if m.board != secNotes {
		m.setErr("':pin' solo aplica a notas.")
		return m, nil
	}
	n, ok := m.curNote()
	if !ok {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	n.Pinned = !n.Pinned
	if err := m.persistNote(n); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

func (m Model) cmdToggleArch() (tea.Model, tea.Cmd) {
	if m.board != secNotes {
		m.setErr("':arch' solo aplica a notas.")
		return m, nil
	}
	n, ok := m.curNote()
	if !ok {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	n.Archived = !n.Archived
	if err := m.persistNote(n); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

func (m Model) cmdColor(arg string) (tea.Model, tea.Cmd) {
	if m.board != secNotes {
		m.setErr("':color' solo aplica a notas.")
		return m, nil
	}
	hex, ok := colorByName(arg)
	if !ok {
		m.setErr("Color inválido: usa " + colorNamesList())
		return m, nil
	}
	n, ok := m.curNote()
	if !ok {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	n.Color = hex
	if err := m.persistNote(n); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	return m, nil
}

// yankPassword copia la contraseña de la entrada seleccionada al portapapeles.
func (m Model) yankPassword() (tea.Model, tea.Cmd) {
	if m.board != secSecrets {
		m.setErr("'y' copia contraseñas: cambia a la bóveda con :v secretos")
		return m, nil
	}
	s, ok := m.curSecret()
	if !ok {
		m.setErr("Selecciona una entrada con j/k")
		return m, nil
	}
	if err := clipboard.WriteAll(s.Password); err != nil {
		m.setErr(fmt.Sprintf("portapapeles no disponible: %v", err))
		return m, nil
	}
	m.notice = "✓ Contraseña copiada al portapapeles"
	return m, nil
}

// ---- persistencia ----

func (m *Model) selectByID(id int64) {
	count := m.visibleCount()
	for i := 0; i < count; i++ {
		if m.idAtVisible(i) == id {
			m.selIdx = i
			return
		}
	}
	m.clampSel()
}

func (m Model) idAtVisible(i int) int64 {
	if m.board == secSecrets {
		return m.visibleSecrets()[i].ID
	}
	return m.visibleNotes()[i].ID
}

// persistNote cifra título/cuerpo y escribe la fila.
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

// persistSecret cifra los cuatro campos sensibles (URL queda en claro).
func (m Model) persistSecret(s store.Secret) error {
	db := s
	var err error
	if db.Title, err = m.sess.SealField(s.Title); err != nil {
		return err
	}
	if db.Username, err = m.sess.SealField(s.Username); err != nil {
		return err
	}
	if db.Password, err = m.sess.SealField(s.Password); err != nil {
		return err
	}
	if db.Notes, err = m.sess.SealField(s.Notes); err != nil {
		return err
	}
	return m.st.UpdateSecret(&db)
}

// createSecret inserta una entrada ya cifrada.
func (m Model) createSecret(sc store.Secret) (store.Secret, error) {
	db := sc
	var err error
	if db.Title, err = m.sess.SealField(sc.Title); err != nil {
		return sc, err
	}
	if db.Username, err = m.sess.SealField(""); err != nil {
		return sc, err
	}
	if db.Password, err = m.sess.SealField(""); err != nil {
		return sc, err
	}
	if db.Notes, err = m.sess.SealField(""); err != nil {
		return sc, err
	}
	return m.st.CreateSecret(db)
}

// refresh recarga notas y secretos descifrados desde la BD.
func (m *Model) refresh() {
	list, err := m.st.ListNotes(true) // archivadas incluidas: el filtro es visual
	m.notes = nil
	if err != nil {
		m.setErr(err.Error())
	} else {
		for _, sn := range list {
			sn.Title = m.unsealOr(sn.Title, "(sin título)")
			sn.Body = m.unsealOr(sn.Body, "")
			m.notes = append(m.notes, sn)
		}
	}

	sl, err := m.st.ListSecrets()
	m.secrets = nil
	if err != nil {
		m.setErr(err.Error())
	} else {
		for _, ss := range sl {
			ss.Title = m.unsealOr(ss.Title, "(sin título)")
			ss.Username = m.unsealOr(ss.Username, "")
			ss.Password = m.unsealOr(ss.Password, "")
			ss.Notes = m.unsealOr(ss.Notes, "")
			m.secrets = append(m.secrets, ss)
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

func (m *Model) clampSel() {
	c := m.visibleCount()
	if m.selIdx >= c {
		m.selIdx = c - 1
	}
	if m.selIdx < 0 {
		m.selIdx = 0
	}
}

func (m *Model) moveSel(delta int) {
	if m.visibleCount() == 0 {
		return
	}
	m.selIdx += delta
	m.clampSel()
}

// ---- mensajes ----

func (m *Model) setErr(s string) { m.errMsg = s }

// ---- autenticación ----

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
	m.notes = nil
	m.secrets = nil
	m.state = viewLocked
	m.board = secNotes
	m.setting = false
	m.firstPw = ""
	m.errMsg, m.notice = "", ""
	m.revealAll = false
	m.closeCmdBar()
	m.closeEditor()
	m.clearSearch()
	m.showHelp = false
	m.input.SetValue("")
	m.input.Focus()
}

// ---- editor ----

func (m *Model) openEditor(id int64) {
	m.ed.open = true
	m.ed.sec = m.board
	m.ed.id = id
	m.ed.field = 0
	m.ed.reveal = false
	m.ed.cmd = false
	m.ed.cmdLn.SetValue("")
	m.ed.title.Blur()
	m.ed.body.Blur()
	m.ed.user.Blur()
	m.ed.pass.Blur()
	m.ed.url.Blur()
	m.ed.cmdLn.Blur()
	m.errMsg, m.notice = "", ""

	if m.board == secSecrets {
		if s, ok := m.byIDSecret(id); ok {
			m.ed.title.SetValue(s.Title)
			m.ed.user.SetValue(s.Username)
			m.ed.pass.SetValue(s.Password)
			m.ed.url.SetValue(s.URL)
			m.ed.body.SetValue(s.Notes)
		}
		m.ed.title.Focus()
		return
	}
	if n, ok := m.byIDNote(id); ok {
		m.ed.title.SetValue(n.Title)
		m.ed.body.SetValue(n.Body)
	}
	m.ed.title.Focus()
}

func (m Model) byIDNote(id int64) (store.Note, bool) {
	for _, n := range m.notes {
		if n.ID == id {
			return n, true
		}
	}
	return store.Note{}, false
}

func (m Model) byIDSecret(id int64) (store.Secret, bool) {
	for _, s := range m.secrets {
		if s.ID == id {
			return s, true
		}
	}
	return store.Secret{}, false
}

func (m *Model) closeEditor() {
	m.ed.open = false
	m.ed.id = 0
	m.ed.cmd = false
	m.ed.reveal = false
	m.ed.title.Blur()
	m.ed.body.Blur()
	m.ed.user.Blur()
	m.ed.pass.Blur()
	m.ed.url.Blur()
	m.ed.cmdLn.Blur()
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

	switch {
	case msg.Type == tea.KeyEsc:
		m.closeEditor()
		return m, nil
	case msg.Type == tea.KeyTab || msg.Type == tea.KeyShiftTab:
		delta := 1
		if msg.Type == tea.KeyShiftTab {
			delta = -1
		}
		fields := m.editorFieldCount()
		m.ed.field = ((m.ed.field+delta)%fields + fields) % fields
		m.refocusEditorField()
		return m, textinput.Blink
	case msg.String() == "ctrl+s":
		return m.saveEditor(false)
	case msg.String() == "ctrl+r" && m.ed.sec == secSecrets:
		m.ed.reveal = !m.ed.reveal
		if m.ed.reveal {
			m.ed.pass.EchoMode = textinput.EchoNormal
		} else {
			m.ed.pass.EchoMode = textinput.EchoPassword
		}
		return m, nil
	}
	if msg.String() == ":" {
		m.ed.cmd = true
		m.ed.cmdLn.Focus()
		return m, textinput.Blink
	}
	return m.routeEditorField(msg)
}

func (m Model) editorFieldCount() int {
	if m.ed.sec == secSecrets {
		return 5 // título, usuario, contraseña, url, notas
	}
	return 2 // título, cuerpo
}

func (m *Model) refocusEditorField() {
	m.ed.title.Blur()
	m.ed.body.Blur()
	m.ed.user.Blur()
	m.ed.pass.Blur()
	m.ed.url.Blur()
	switch {
	case m.ed.sec == secNotes && m.ed.field == 0:
		m.ed.title.Focus()
	case m.ed.sec == secNotes:
		m.ed.body.Focus()
	case m.ed.field == 0:
		m.ed.title.Focus()
	case m.ed.field == 1:
		m.ed.user.Focus()
	case m.ed.field == 2:
		m.ed.pass.Focus()
	case m.ed.field == 3:
		m.ed.url.Focus()
	default:
		m.ed.body.Focus()
	}
}

func (m Model) routeEditorField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.ed.sec == secNotes {
		switch m.ed.field {
		case 0:
			m.ed.title, cmd = m.ed.title.Update(msg)
		default:
			m.ed.body, cmd = m.ed.body.Update(msg)
		}
		return m, cmd
	}
	switch m.ed.field {
	case 0:
		m.ed.title, cmd = m.ed.title.Update(msg)
	case 1:
		m.ed.user, cmd = m.ed.user.Update(msg)
	case 2:
		m.ed.pass, cmd = m.ed.pass.Update(msg)
	case 3:
		m.ed.url, cmd = m.ed.url.Update(msg)
	default:
		m.ed.body, cmd = m.ed.body.Update(msg)
	}
	return m, cmd
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

// saveEditor persiste el búfer según el tipo de entidad editada.
func (m Model) saveEditor(close bool) (tea.Model, tea.Cmd) {
	if m.ed.sec == secSecrets {
		return m.saveSecret(close)
	}
	target, ok := m.byIDNote(m.ed.id)
	if !ok {
		m.setErr("La nota ya no existe.")
		m.closeEditor()
		return m, nil
	}
	target.Title = m.ed.title.Value()
	target.Body = m.ed.body.Value()
	if strings.TrimSpace(target.Title) == "" {
		target.Title = "(sin título)"
	}
	if err := m.persistNote(target); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	if close {
		m.closeEditor()
	}
	return m, nil
}

func (m Model) saveSecret(close bool) (tea.Model, tea.Cmd) {
	target, ok := m.byIDSecret(m.ed.id)
	if !ok {
		m.setErr("La entrada ya no existe.")
		m.closeEditor()
		return m, nil
	}
	target.Title = m.ed.title.Value()
	target.Username = m.ed.user.Value()
	target.Password = m.ed.pass.Value()
	target.URL = strings.TrimSpace(m.ed.url.Value())
	target.Notes = m.ed.body.Value()
	if strings.TrimSpace(target.Title) == "" {
		target.Title = "(sin título)"
	}
	if err := m.persistSecret(target); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	if close {
		m.closeEditor()
	}
	return m, nil
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
	switch {
	case m.searchFocus:
		return helpStyle.Render("enter · aplicar    esc · limpiar")
	case m.cmdFocus:
		return helpStyle.Render("enter · ejecutar    esc · cancelar")
	}

	label := "NOTAS"
	if m.board == secSecrets {
		label = "VAULT"
	}
	parts := []string{label}

	if m.query != "" {
		parts = append(parts, fmt.Sprintf("🔍 %q %d/%d",
			m.query, m.visibleCount(), len(m.notes)+len(m.secrets)))
	}
	if !m.sess.Alive() {
		parts = append(parts, errStyle.Render("🔒 bloqueada"))
		return joinStatus(parts)
	}
	d := m.sess.Remaining()
	if d < 0 {
		d = 0
	}
	mins := int((d + time.Minute - 1) / time.Minute) // techo
	parts = append(parts, fmt.Sprintf("🔓 %d min", mins))
	return joinStatus(parts)
}
