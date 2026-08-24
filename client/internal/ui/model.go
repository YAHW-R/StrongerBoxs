package ui

import (
	"encoding/json"
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
	"github.com/yahwr/strongboxs/client/internal/sync"
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

// secFieldInput es un campo dinámico del editor de vault.
type secFieldInput struct {
	def   fieldDef
	input textinput.Model
}

// editorState es el modal de creación/edición de notas, secretos y plantillas.
type editorState struct {
	open bool
	sec  boardView
	id   int64

	title textinput.Model
	body  textarea.Model
	field int // índice sobre la lista activa (nota o campos de secreto)

	secFields []secFieldInput   // vault: campos según la plantilla
	tplName   string            // plantilla asociada a la entrada abierta
	extraVals map[string]string // valores descifrados de campos libres

	building bool        // modo :newp (constructor de plantillas)
	builder  *tplBuilder // formulario del constructor (nil fuera de él)
	reveal   bool        // mostrar campos sensibles en claro mientras se edita

	cmd   bool
	cmdLn textinput.Model
}

// setFieldValue localiza un campo por clave y le asigna valor.
func (e *editorState) setFieldValue(key, val string) bool {
	for i := range e.secFields {
		if e.secFields[i].def.Key == key {
			if e.secFields[i].def.Multi {
				e.body.SetValue(val)
				return true
			}
			e.secFields[i].input.SetValue(val)
			return true
		}
	}
	return false
}

func (e *editorState) fieldValue(key string) string {
	for _, f := range e.secFields {
		if f.def.Key != key {
			continue
		}
		if f.def.Multi {
			return e.body.Value()
		}
		return f.input.Value()
	}
	return ""
}

func newTextInput(placeholder string, sensitive bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	ti.Width = 38
	if sensitive {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

func newBodyArea() textarea.Model {
	b := textarea.New()
	b.Placeholder = "Escribe aquí…"
	b.SetHeight(7)
	b.SetWidth(44)
	b.ShowLineNumbers = false
	return b
}

func newCmdInput(limit int) textinput.Model {
	c := textinput.New()
	c.Prompt = ":"
	c.CharLimit = limit
	c.Width = 50
	return c
}

func newEditorState() editorState {
	return editorState{
		title:     newTextInput("Título", false),
		body:      newBodyArea(),
		extraVals: map[string]string{},
		cmdLn:     newCmdInput(32),
	}
}

// Model es el estado raíz de la app BubbleTea.
type Model struct {
	sess *session.Manager
	st   *store.Store

	state         viewState
	board         boardView
	width, height int

	notes   []store.Note                 // descifradas SOLO con sesión viva
	secrets []store.Secret               // descifradas ídem
	extraBy map[string]map[string]string // uuid → campos libres descifrados

	rt  *SyncRuntime     // puente con el motor reactivo (opcional)
	wiz *syncWizardState // asistente de configuración

	query       string // filtro '/' activo
	searchFocus bool   // escribiendo en la barra '/'
	searchLn    textinput.Model
	revealAll   bool // 'v': mostrar contraseñas en las tarjetas

	selIdx       int // sobre la lista VISIBLE de la vista actual
	showArchived bool
	showHelp     bool

	pal          paletteState // paleta de comandos (ctrl+k)
	confirmOpen  bool         // diálogo ¿borrar? (y/n)
	confirmIsSec bool

	ed editorState

	input   textinput.Model // contraseña maestra (setup / lock-screen)
	setting bool
	firstPw string
	errMsg  string
	notice  string
}

// Opt configura extras del modelo (sincronización reactiva).
type Opt func(*Model)

// WithSyncRuntime conecta la UI con el motor (trigger, gate, arranque).
func WithSyncRuntime(rt *SyncRuntime) Opt {
	return func(m *Model) { m.rt = rt }
}

// WithSync mantiene compatibilidad: solo trigger + gate.
func WithSync(trigger func(), g *sync.Gate) Opt {
	return func(m *Model) {
		if m.rt == nil {
			m.rt = &SyncRuntime{}
		}
		m.rt.Trigger = trigger
		if g != nil {
			m.rt.Gate = g
		}
	}
}

func New(sess *session.Manager, st *store.Store, opts ...Opt) Model {
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
		ed:       newEditorState(),
		pal:      newPalette(),
		searchLn: sl,
	}

	for _, o := range opts {
		o(&m)
	}

	for _, o := range opts {
		o(&m)
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

	// Primer paso de onboarding: ofrecer configurar la sincronización una vez.
	if m.shouldOfferSyncSetup() {
		m.openSyncWizard(true)
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

// applyResponsiveSizes adapta los widgets del editor al tamaño real de la
// ventana (anchos fijos se salían por los bordes en terminales pequeñas).
func (m *Model) applyResponsiveSizes() {
	w := m.width - 18
	switch {
	case w < 14:
		w = 14
	case w > 48:
		w = 48
	}
	h := m.height - 20
	switch {
	case h < 3:
		h = 3
	case h > 10:
		h = 10
	}

	m.ed.title.Width = w
	m.ed.body.SetWidth(w + 4)
	m.ed.body.SetHeight(h)
	for i := range m.ed.secFields {
		m.ed.secFields[i].input.Width = w
	}
	if b := m.ed.builder; b != nil {
		b.name.Width = w
		for i := range b.rows {
			b.rows[i].label.Width = w
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyResponsiveSizes()
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
		case m.wiz != nil && m.wiz.open:
			return m.handleWizardKey(msg)
		case m.ed.open:
			return m.handleEditorKey(msg)
		case m.pal.open:
			return m.handlePaletteKey(msg)
		case m.confirmOpen:
			return m.handleConfirmKey(msg)
		case m.searchFocus:
			return m.handleSearchKey(msg)
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
	case "ctrl+k":
		m.openPalette()
		return m, textinput.Blink

	case "ctrl+o":
		if m.board == secSecrets {
			return m.secretNew("")
		}
		return m.cmdNew("")

	case "ctrl+e":
		return m.cmdEdit()

	case "ctrl+s":
		m.requestSync()
		m.notice = "⟳ Sincronizando cambios pendientes…"
		return m, nil

	case "ctrl+d":
		return m.askDelete()

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
	case "tovault", "tv", "cifrar":
		return m.cmdTovault()
	case "newp", "newtemplate":
		return m.cmdNewp(args)
	case "deltemplate", "deltpl":
		return m.cmdDelTemplate(args)
	case "tmpl", "templates", "plantillas":
		return m.cmdListTemplates()
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
	// NOTAS: el argumento es el título inicial.
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
	m.requestSync()
	m.selectByID(created.ID)
	m.openEditor(created.ID)
	return m, textinput.Blink
}

// secretNew crea una entrada con la plantilla indicada
// (:new sin args → plantilla "simple": usuario + valor).
func (m Model) secretNew(tplName string) (tea.Model, tea.Cmd) {
	tplName = strings.ToLower(strings.TrimSpace(tplName))
	if tplName == "" {
		tplName = defaultTemplate
	}
	tpl, ok := m.findTemplate(tplName)
	if !ok {
		m.setErr("Plantilla inexistente: " + tplName + " · disponibles: " + m.templateNames())
		return m, nil
	}
	sc := store.Secret{Template: tpl.Name}
	created, err := m.createSecret(sc)
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	m.requestSync()
	m.selectByID(created.ID)
	m.openEditor(created.ID)
	return m, textinput.Blink
}

// cmdTovault convierte la nota seleccionada en entrada cifrada del vault
// (plantilla "nota") y borra la original. Ambos lados sincronizan.
func (m Model) cmdTovault() (tea.Model, tea.Cmd) {
	if m.board != secNotes {
		m.setErr("':tovault' se usa sobre una nota.")
		return m, nil
	}
	n, ok := m.curNote()
	if !ok {
		m.setErr("Selecciona una nota con j/k")
		return m, nil
	}
	sc := store.Secret{Template: "nota", Title: n.Title, Notes: n.Body}
	created, err := m.createSecret(sc)
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	if err := m.st.SoftDeleteNote(n.ID); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.board = secSecrets
	m.selIdx = 0
	m.refresh()
	m.selectByID(created.ID)
	m.requestSync()
	m.notice = "✓ Nota cifrada en el vault (original borrada)"
	return m, nil
}

// cmdNewp abre el constructor de plantillas (:newp [nombre]).
func (m Model) cmdNewp(name string) (tea.Model, tea.Cmd) {
	m.ed.open = true
	m.ed.building = true
	m.ed.sec = secSecrets
	m.ed.id = 0
	m.ed.field = 0
	m.ed.cmd = false
	m.ed.cmdLn.SetValue("")
	m.ed.secFields = nil
	m.ed.builder = newTplBuilder(strings.TrimSpace(name))
	m.blurAllEditorWidgets()
	m.errMsg, m.notice = "", ""
	m.applyResponsiveSizes()
	m.ed.builder.name.Focus()
	return m, textinput.Blink
}

func (m Model) cmdDelTemplate(name string) (tea.Model, tea.Cmd) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, b := range builtinVaultTemplates() {
		if b.Name == name {
			m.setErr("Las plantillas integradas no se pueden borrar.")
			return m, nil
		}
	}
	if err := m.st.DeleteTemplate(name); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.notice = "✓ Plantilla eliminada: " + name
	return m, nil
}

func (m Model) cmdListTemplates() (tea.Model, tea.Cmd) {
	var lines []string
	for _, t := range m.loadVaultTemplates() {
		lines = append(lines, fmt.Sprintf("%s %s · :new %s", t.Icon, t.Title, t.Name))
	}
	m.notice = strings.Join(lines, "   |   ")
	return m, nil
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

// askDelete abre el diálogo de confirmación (ctrl+d / paleta).
func (m Model) askDelete() (tea.Model, tea.Cmd) {
	if m.visibleCount() == 0 {
		m.setErr("Nada que borrar")
		return m, nil
	}
	if _, ok := m.curSecret(); ok {
		m.confirmIsSec = true
	} else if _, ok := m.curNote(); ok {
		m.confirmIsSec = false
	} else {
		m.setErr("Selecciona un elemento con j/k")
		return m, nil
	}
	m.confirmOpen = true
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmOpen = false
		return m.performDelete()
	case "n", "esc":
		m.confirmOpen = false
		return m, nil
	}
	return m, nil // cualquier otra tecla se ignora dentro del diálogo
}

func (m Model) performDelete() (tea.Model, tea.Cmd) {
	var err error
	if m.confirmIsSec {
		s, ok := m.curSecret()
		if !ok {
			return m, nil
		}
		err = m.st.SoftDeleteSecret(s.ID)
	} else {
		n, ok := m.curNote()
		if !ok {
			return m, nil
		}
		err = m.st.SoftDeleteNote(n.ID)
	}
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	m.requestSync()
	m.notice = "✓ Eliminado"
	return m, nil
}

func (m Model) cmdDelete() (tea.Model, tea.Cmd) {
	return m.askDelete()
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
	m.requestSync()
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
	m.requestSync()
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
	m.requestSync()
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

	tplName := s.Template
	if tplName == "" {
		tplName = defaultTemplate
	}
	tpl, found := m.findTemplate(tplName)
	extra := m.extraBy[s.UUID]
	if !found {
		tpl, _ = m.findTemplate(defaultTemplate)
	}
	secretVal := ""
	for _, f := range tpl.Fields {
		if !f.Sensitive || f.Multi {
			continue
		}
		switch f.Key {
		case "username":
			secretVal = s.Username
		case "password":
			secretVal = s.Password
		case "url":
			secretVal = s.URL
		case "notes":
			secretVal = s.Notes
		default:
			secretVal = extra[f.Key]
		}
		if secretVal != "" {
			break
		}
	}
	if secretVal == "" {
		m.setErr("La entrada no tiene ningún valor secreto que copiar")
		return m, nil
	}
	if err := clipboard.WriteAll(secretVal); err != nil {
		m.setErr(fmt.Sprintf("portapapeles no disponible: %v", err))
		return m, nil
	}
	m.notice = "✓ Valor copiado al portapapeles"
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

// persistSecret cifra los campos sensibles y el blob de campos libres
// (la URL queda en claro: metadato útil para búsqueda/sync).
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
	if db.Extra, err = m.sess.SealField(s.Extra); err != nil {
		return err
	}
	return m.st.UpdateSecret(&db)
}

// createSecret inserta una entrada sellando SUS valores (título, campos
// estándar y blob extra). Los vacíos se sellan como "".
func (m Model) createSecret(sc store.Secret) (store.Secret, error) {
	db := sc
	var err error
	if db.Title, err = m.sess.SealField(sc.Title); err != nil {
		return sc, err
	}
	if db.Username, err = m.sess.SealField(sc.Username); err != nil {
		return sc, err
	}
	if db.Password, err = m.sess.SealField(sc.Password); err != nil {
		return sc, err
	}
	if db.URL, err = m.sess.SealField(sc.URL); err != nil {
		return sc, err
	}
	if db.Notes, err = m.sess.SealField(sc.Notes); err != nil {
		return sc, err
	}
	if db.Extra, err = m.sess.SealField(sc.Extra); err != nil {
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
			ss.Extra = m.unsealOr(ss.Extra, "")
			m.secrets = append(m.secrets, ss)
			if ss.Extra != "" {
				var em map[string]string
				if json.Unmarshal([]byte(ss.Extra), &em) == nil {
					if m.extraBy == nil {
						m.extraBy = map[string]map[string]string{}
					}
					m.extraBy[ss.UUID] = em
				}
			}
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

// shouldOfferSyncSetup: ¿toca ofrecer el asistente? Solo en tablero,
// con runtime presente, sin cuenta configurada y sin "no ahora".
func (m Model) shouldOfferSyncSetup() bool {
	return m.state == viewBoard && m.rt != nil &&
		m.rt.IsConfigured != nil && m.rt.IsDeclined != nil &&
		!m.rt.IsConfigured() && !m.rt.IsDeclined()
}

// setBusy marca la Gate: con editor/asistente abiertos el PULL se pausa.
func (m *Model) setBusy(b bool) {
	if m.rt != nil && m.rt.Gate != nil {
		m.rt.Gate.Set(b)
	}
}

// requestSync dispara un ciclo push reactivo (coalescido en el motor).
func (m *Model) requestSync() {
	if m.rt != nil && m.rt.Trigger != nil {
		m.rt.Trigger()
	}
}

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
	if m.shouldOfferSyncSetup() {
		m.openSyncWizard(true)
	}
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
	if m.shouldOfferSyncSetup() {
		m.openSyncWizard(true)
	}
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
	m.closeSyncWizard()
	m.pal.open = false
	m.confirmOpen = false
	m.closeEditor()
	m.clearSearch()
	m.showHelp = false
	m.input.SetValue("")
	m.input.Focus()
}

// ---- editor ----

func (m *Model) blurAllEditorWidgets() {
	m.ed.title.Blur()
	m.ed.body.Blur()
	for i := range m.ed.secFields {
		m.ed.secFields[i].input.Blur()
	}
	if m.ed.builder != nil {
		m.ed.builder.name.Blur()
		for i := range m.ed.builder.rows {
			m.ed.builder.rows[i].label.Blur()
		}
	}
	m.ed.cmdLn.Blur()
}

func (m *Model) buildSecFields(tpl vaultTemplate, s store.Secret, extra map[string]string) {
	m.ed.secFields = nil
	for _, def := range tpl.Fields {
		label := def.Label
		in := newTextInput(label, def.Sensitive)
		var val string
		switch def.Key {
		case "username":
			val = s.Username
		case "password":
			val = s.Password
		case "url":
			val = s.URL
		case "notes":
			val = s.Notes
		default:
			val = extra[def.Key]
		}
		if def.Multi {
			m.ed.body.SetValue(val)
			in.SetValue("")
		} else {
			in.SetValue(val)
		}
		m.ed.secFields = append(m.ed.secFields, secFieldInput{def: def, input: in})
	}
}

func (m *Model) openSecretEditor(id int64) {
	s, ok := m.byIDSecret(id)
	if !ok {
		return
	}
	tplName := s.Template
	if tplName == "" {
		tplName = defaultTemplate
	}
	tpl, found := m.findTemplate(tplName)
	if !found {
		tpl, _ = m.findTemplate(defaultTemplate)
		tplName = defaultTemplate
	}
	m.ed.tplName = tplName
	m.buildSecFields(tpl, s, m.extraBy[s.UUID])
	m.ed.field = 0
	m.applyResponsiveSizes()
	m.ed.title.Focus()
}

func (m *Model) openEditor(id int64) {
	m.ed.open = true
	m.ed.sec = m.board
	m.ed.id = id
	m.ed.field = 0
	m.ed.reveal = false
	m.ed.cmd = false
	m.ed.building = false
	m.ed.secFields = nil
	m.ed.extraVals = map[string]string{}
	m.ed.cmdLn.SetValue("")
	m.blurAllEditorWidgets()
	m.errMsg, m.notice = "", ""

	if m.board == secSecrets {
		m.openSecretEditor(id)
		return
	}
	if n, ok := m.byIDNote(id); ok {
		m.ed.title.SetValue(n.Title)
		m.ed.body.SetValue(n.Body)
	}
	m.applyResponsiveSizes()
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
	m.ed.building = false
	m.ed.secFields = nil
	m.ed.builder = nil
	m.blurAllEditorWidgets()
}

func (m Model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ed.building {
		return m.handleBuilderKey(msg)
	}
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
		for i := range m.ed.secFields {
			if !m.ed.secFields[i].def.Sensitive {
				continue
			}
			if m.ed.reveal {
				m.ed.secFields[i].input.EchoMode = textinput.EchoNormal
			} else {
				m.ed.secFields[i].input.EchoMode = textinput.EchoPassword
			}
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
	switch {
	case m.ed.building:
		return m.ed.builder.widgetCount()
	case m.ed.sec == secSecrets:
		return len(m.ed.secFields) + 1 // índice 0 = título
	default:
		return 2 // título, cuerpo
	}
}

// focusEditorIndex enfoca el widget del índice dado (blur del resto).
func (m *Model) focusEditorIndex(i int) {
	m.blurAllEditorWidgets()
	switch {
	case i < 0 || i >= m.editorFieldCount():
		m.ed.title.Focus()
	case m.ed.building:
		b := m.ed.builder
		m.blurAllEditorWidgets()
		switch {
		case i == 0:
			b.name.Focus()
		case (i-1)%2 == 0:
			b.rows[(i-1)/2].label.Focus()
		}
		// El selector de tipo no es un input: solo resalta en la vista.
	case m.ed.sec == secNotes:
		if i == 0 {
			m.ed.title.Focus()
		} else {
			m.ed.body.Focus()
		}
	default:
		idx := i - 1
		if idx < 0 || idx >= len(m.ed.secFields) {
			m.ed.title.Focus()
			return
		}
		// PUNTERO al elemento del slice: Focus() sobre una copia no pega.
		f := &m.ed.secFields[idx]
		if f.def.Multi {
			m.ed.body.Focus()
		} else {
			f.input.Focus()
		}
	}
}

func (m *Model) refocusEditorField() {
	m.focusEditorIndex(m.ed.field)
}

func (m Model) routeEditorField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case m.ed.building:
		// manejado por handleBuilderKey; nunca debería llegar aquí
		return m, nil
	case m.ed.sec == secNotes:
		if m.ed.field == 0 {
			m.ed.title, cmd = m.ed.title.Update(msg)
		} else {
			m.ed.body, cmd = m.ed.body.Update(msg)
		}
	case m.ed.sec == secSecrets && m.ed.field == 0:
		m.ed.title, cmd = m.ed.title.Update(msg)
	case m.ed.sec == secSecrets && m.ed.field-1 < len(m.ed.secFields):
		f := m.ed.secFields[m.ed.field-1]
		if f.def.Multi {
			m.ed.body, cmd = m.ed.body.Update(msg)
		} else {
			in := f.input
			in, cmd = in.Update(msg)
			m.ed.secFields[m.ed.field-1].input = in
		}
	}
	return m, cmd
}

func (m Model) execEditorCmd(raw string) (tea.Model, tea.Cmd) {
	name, _, ok := parseCommand(raw)
	if !ok {
		return m, nil
	}
	m.errMsg = ""
	if m.ed.building {
		switch name {
		case "w", "write":
			return m.saveTemplate(false)
		case "wq", "x":
			return m.saveTemplate(true)
		case "q", "quit", "q!":
			m.closeEditor()
			return m, nil
		default:
			m.setErr("En el constructor: :w crea la plantilla · :q cancela")
			return m, nil
		}
	}
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
	m.requestSync()
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

	// El título vive en su propio input (índice 0 del editor).
	target.Title = m.ed.title.Value()
	if strings.TrimSpace(target.Title) == "" {
		target.Title = "(sin título)"
	}

	target.Template = m.ed.tplName
	extra := map[string]string{}
	for _, f := range m.ed.secFields {
		val := f.input.Value()
		switch f.def.Key {
		case "username":
			target.Username = val
		case "password":
			target.Password = val
		case "url":
			target.URL = strings.TrimSpace(val)
		case "notes":
			target.Notes = val
		default:
			if val != "" {
				extra[f.def.Key] = val
			}
		}
	}
	if strings.TrimSpace(target.Title) == "" {
		target.Title = "(sin título)"
	}

	if len(extra) > 0 {
		// Se guarda en CLARO en la entidad de memoria; persistSecret lo
		// sella junto al resto de campos sensibles (único punto de cifrado).
		blob, err := json.Marshal(extra)
		if err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		target.Extra = string(blob)
	} else {
		target.Extra = ""
	}

	if err := m.persistSecret(target); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.refresh()
	m.requestSync()
	if close {
		m.closeEditor()
	}
	return m, nil
}

// handleBuilderKey gestiona el formulario de :newp:
// tab navega · ←/→/espacio cambian el tipo · ctrl+n añade · ctrl+d borra ·
// ctrl+s crea · esc cancela. NO hay barra ':' (por eso nunca interfiere).
func (m Model) handleBuilderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := m.ed.builder
	count := b.widgetCount()

	switch msg.Type {
	case tea.KeyEsc:
		m.closeEditor()
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		delta := 1
		if msg.Type == tea.KeyShiftTab {
			delta = -1
		}
		m.ed.field = ((m.ed.field+delta)%count + count) % count
		m.refocusEditorField()
		return m, textinput.Blink
	case tea.KeyCtrlS:
		return m.saveTemplate(true)
	case tea.KeyCtrlN:
		b.appendRow()
		// Foco en la ETIQUETA de la fila recién creada.
		m.ed.field = 1 + (len(b.rows)-1)*2
		m.refocusEditorField()
		return m, textinput.Blink
	case tea.KeyCtrlD:
		if m.ed.field >= 1 {
			b.deleteRow((m.ed.field - 1) / 2)
			if m.ed.field >= count {
				m.ed.field = count - 1
			}
			m.refocusEditorField()
		}
		return m, nil
	case tea.KeyEnter:
		// Enter sobre una etiqueta salta al selector de tipo de la fila.
		if m.ed.field >= 1 && (m.ed.field-1)%2 == 0 {
			m.ed.field++
			m.refocusEditorField()
			return m, textinput.Blink
		}
	case tea.KeyLeft, tea.KeyRight, tea.KeySpace:
		if m.ed.field >= 1 && (m.ed.field-1)%2 == 1 { // widget de tipo
			delta := 1
			if msg.Type == tea.KeyLeft {
				delta = -1
			}
			b.cycleType((m.ed.field-1)/2, delta)
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch {
	case m.ed.field == 0:
		m.ed.builder.name, cmd = m.ed.builder.name.Update(msg)
	default:
		row := (m.ed.field - 1) / 2
		if row < len(b.rows) && (m.ed.field-1)%2 == 0 {
			in := b.rows[row].label
			in, cmd = in.Update(msg)
			b.rows[row].label = in
		}
	}
	return m, cmd
}

// saveTemplate crea la plantilla desde el formulario del constructor.
func (m Model) saveTemplate(close bool) (tea.Model, tea.Cmd) {
	name := strings.ToLower(strings.TrimSpace(m.ed.builder.name.Value()))
	if name == "" {
		m.setErr("Define el nombre interno (para invocarla con :new).")
		return m, nil
	}
	fields, err := m.ed.builder.rowsToFields()
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	vt := vaultTemplate{Name: name, Title: name, Icon: "🔐", Fields: fields}
	if err := m.st.CreateTemplate(encodeCustomTemplate(vt)); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.notice = "✓ Plantilla creada: usa :new " + name
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
	parts = append(parts, "ctrl+k comandos · ctrl+o nuevo · q salir")
	return joinStatus(parts)
}
