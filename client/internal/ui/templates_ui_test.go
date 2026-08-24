package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yahwr/strongboxs/client/internal/crypto"

	"github.com/yahwr/strongboxs/client/internal/sync"
)

func builderWithRows(t *testing.T, rows ...[2]string) *tplBuilder {
	t.Helper()
	b := newTplBuilder("prueba")
	b.rows = nil
	for _, r := range rows {
		b.appendRow()
		i := len(b.rows) - 1
		b.rows[i].label.SetValue(r[0])
		for ti, ft := range fieldTypes {
			if ft.ID == r[1] {
				b.rows[i].typeIdx = ti
			}
		}
	}
	return b
}

func TestBuilderRowsToFields(t *testing.T) {
	b := builderWithRows(t,
		[2]string{"servidor", "texto"},
		[2]string{"Clave Acceso", "secreto"},
		[2]string{"notas", "multilinea"},
	)
	fields, err := b.rowsToFields()
	if err != nil {
		t.Fatalf("filas válidas: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("campos = %d", len(fields))
	}
	if fields[0].Key != "servidor" || fields[0].Sensitive {
		t.Errorf("campo0 = %+v", fields[0])
	}
	if !fields[1].Sensitive || fields[1].Key != "clave_acceso" {
		t.Errorf("campo1 = %+v (label con espacios → slug)", fields[1])
	}
	if !fields[2].Multi || fields[2].Sensitive {
		t.Errorf("campo2 debería ser multi no-sensible: %+v", fields[2])
	}

	dup := builderWithRows(t, [2]string{"a", "texto"}, [2]string{"a", "secreto"})
	if _, err := dup.rowsToFields(); err == nil {
		t.Error("duplicado debería fallar")
	}
	twoMulti := builderWithRows(t, [2]string{"m1", "multilinea"}, [2]string{"m2", "multilinea"})
	if _, err := twoMulti.rowsToFields(); err == nil {
		t.Error("dos multilínea deberían fallar")
	}
	empty := newTplBuilder("")
	if _, err := empty.rowsToFields(); err == nil {
		t.Error("sin campos debería fallar")
	}
}

func TestNewDefaultsToSimpleTemplate(t *testing.T) {
	m := unlockedModel(t)
	m = runCommand(m, "v secretos")
	m = runCommand(m, "new") // sin args → plantilla por defecto

	if !m.ed.open || m.ed.tplName != defaultTemplate {
		t.Fatalf("tpl=%q, quiero 'simple'", m.ed.tplName)
	}
	keys := []string{}
	for _, f := range m.ed.secFields {
		keys = append(keys, f.def.Key)
	}
	if strings.Join(keys, ",") != "username,password" {
		t.Fatalf("campos simple = %v", keys)
	}

	m.ed.title.SetValue("mi credencial")
	m.ed.setFieldValue("username", "pepe")
	m.ed.setFieldValue("password", "valor-secreto-99")
	m = runEditorCmd(m, "wq")

	s := m.secrets[0]
	if s.Template != "simple" || s.Username != "pepe" || s.Password != "valor-secreto-99" {
		t.Fatalf("entrada simple mal guardada: %+v", s)
	}
	if s.Title != "mi credencial" {
		t.Errorf("título no persistido: %q", s.Title)
	}
	if s.Extra != "" {
		t.Errorf("simple no debe tener extra; got %q", s.Extra)
	}

	// REGRESIÓN: reabrir la app (bloquear sesión → nuevo modelo → unlock)
	// debe mostrar el título descifrado.
	m.sess.Lock()
	m2 := New(m.sess, m.st)
	if m2.state != viewLocked {
		t.Fatalf("precondición lock: %d", m2.state)
	}
	m2.input.SetValue(testPw)
	m2 = pressEnter(m2)
	if len(m2.secrets) == 0 || m2.secrets[0].Title != "mi credencial" {
		t.Fatalf("título tras reiniciar: %+v", m2.secrets)
	}
}

func TestNewUnknownTemplateListsAvailable(t *testing.T) {
	m := unlockedModel(t)
	m = runCommand(m, "v secretos")
	m = runCommand(m, "new noexiste")
	if !strings.Contains(m.errMsg, "disponibles") || !strings.Contains(m.errMsg, "web") {
		t.Errorf("errMsg = %q", m.errMsg)
	}
}

func TestCustomTemplateLifecycle(t *testing.T) {
	m := unlockedModel(t)

	// 1) Crear plantilla con :newp (formulario, tipos por selección).
	m = runCommand(m, "newp miservidores")
	if !m.ed.open || !m.ed.building || m.ed.builder == nil {
		t.Fatalf(":newp debe abrir el constructor")
	}
	if m.ed.builder.name.Value() != "miservidores" {
		t.Errorf("nombre precargado = %q", m.ed.builder.name.Value())
	}

	pressKey := func(key tea.KeyType) Model {
		out, _ := m.Update(tea.KeyMsg{Type: key})
		return out.(Model)
	}

	// Fila 0 (ya existe): Servidor / Texto.
	m.ed.builder.rows[0].label.SetValue("Servidor")

	// Fila 1: ctrl+n → etiqueta + tipo Secreto con →.
	m = pressKey(tea.KeyCtrlN)
	if !m.ed.builder.rows[1].label.Focused() {
		t.Fatal("ctrl+n debe ENFOCAR la etiqueta nueva para escribir")
	}
	// Escribir directamente tras ctrl+n debe funcionar.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	m = out.(Model)
	m.ed.builder.rows[1].label.SetValue("Clave Acceso")
	if m.ed.field != 3 { // foco en la etiqueta de la fila nueva
		t.Fatalf("ctrl+n debe llevar a la etiqueta; field=%d", m.ed.field)
	}
	// Enter sobre la etiqueta salta al selector de tipo de la fila.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if m.ed.field != 4 {
		t.Fatalf("enter debe saltar al tipo; field=%d", m.ed.field)
	}
	m = pressKey(tea.KeyRight) // texto → secreto
	if rowTypeName(m.ed.builder.rows[1]) != "Secreto" {
		t.Fatalf("tipo fila1 = %q", rowTypeName(m.ed.builder.rows[1]))
	}

	// Fila 2: Puerto / Configuración.
	m = pressKey(tea.KeyCtrlN)
	m.ed.builder.rows[2].label.SetValue("Puerto")
	m = pressKey(tea.KeyTab)
	m = pressKey(tea.KeyLeft) // texto ← multilínea (vuelta circular)
	m = pressKey(tea.KeyLeft) // multilínea ← título
	m = pressKey(tea.KeyLeft) // título ← configuración

	// Guardar con ctrl+s.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = out.(Model)

	if m.ed.open {
		t.Fatal("ctrl+s en constructor debe cerrar")
	}
	if _, ok := m.findTemplate("miservidores"); !ok {
		t.Fatal("la plantilla no quedó registrada")
	}

	// 2) Usarla con :new <nombre>.
	// Cambiar al VAULT: ahí :new <nombre> usa plantillas.
	m = runCommand(m, "v secretos")

	m = runCommand(m, "new miservidores")
	if !m.ed.open || m.ed.tplName != "miservidores" {
		t.Fatalf("plantilla no aplicada: %q", m.ed.tplName)
	}
	if len(m.ed.secFields) != 3 {
		t.Fatalf("campos custom = %d", len(m.ed.secFields))
	}
	if !m.ed.secFields[1].def.Sensitive {
		t.Error("'Clave Acceso' debería ser sensible")
	}

	m.ed.title.SetValue("prod-01")
	m.ed.setFieldValue("servidor", "10.9.8.7")
	m.ed.setFieldValue("clave_acceso", "S3cr3to-ñ")
	m.ed.setFieldValue("puerto", "2222")
	m = runEditorCmd(m, "wq")

	s := m.secrets[m.selIdx]
	if s.Template != "miservidores" {
		t.Fatalf("template = %q", s.Template)
	}
	// En memoria el campo Extra sigue siendo el sobre cifrado;
	// los valores descifrados viven en extraBy (cargados en refresh).
	extra := m.extraBy[s.UUID]
	if extra == nil || extra["servidor"] != "10.9.8.7" || extra["puerto"] != "2222" {
		t.Fatalf("extra descifrado = %v", extra)
	}

	// En disco el blob extra está cifrado.
	rows, _ := m.st.ListSecrets()
	rawExtra := rows[0].Extra
	if rawExtra != "" && !strings.HasPrefix(rawExtra, "sb1.") {
		t.Errorf("extra en claro en BD: %q", rawExtra)
	}

	// La tarjeta muestra los campos; el sensible solo con reveal.
	masked := m.secretCard(s, false).Body
	if strings.Contains(masked, "S3cr3to") || !strings.Contains(masked, maskText) {
		t.Fatalf("tarjeta custom debe enmascarar: %q", masked)
	}
	if !strings.Contains(masked, "Servidor 10.9.8.7") {
		t.Errorf("falta campo libre en tarjeta: %q", masked)
	}

	// 3) Borrar plantilla personalizada sí se puede; integrada no.
	m = runCommand(m, "deltemplate simple")
	if !strings.Contains(m.errMsg, "integradas") {
		t.Errorf("errMsg=%q", m.errMsg)
	}
	m = runCommand(m, "deltemplate miservidores")
	if !strings.Contains(m.notice, "eliminada") {
		t.Errorf("notice=%q", m.notice)
	}
	if _, ok := m.findTemplate("miservidores"); ok {
		t.Error("la plantilla debió borrarse")
	}
}

func TestEditorAdaptsToWindowSize(t *testing.T) {
	m := unlockedModel(t)

	small := tea.WindowSizeMsg{Width: 44, Height: 16}
	out, _ := m.Update(small)
	m = out.(Model)
	m = runCommand(m, "v secretos")
	m = runCommand(m, "new web")

	w := m.ed.secFields[0].input.Width
	if w >= 38 || w < 14 {
		t.Fatalf("en terminal estrecha el ancho debería reducirse; got %d", w)
	}

	big := tea.WindowSizeMsg{Width: 220, Height: 70}
	out, _ = m.Update(big)
	m = out.(Model)
	if got := m.ed.secFields[0].input.Width; got != 48 {
		t.Fatalf("en terminal grande debería limitarse a 48; got %d", got)
	}
}

func TestTovaultConvertsNote(t *testing.T) {
	m := unlockedModel(t)
	m = seedNote(m, "idea privada", "contenido muy secreto")
	m = runCommand(m, "tovault")
	if m.board != secSecrets {
		t.Fatalf("debe saltar al vault; got %v", m.board)
	}
	if m.visibleCount() != 1 {
		t.Fatalf("entradas tras conversión: %d", m.visibleCount())
	}
	s := m.secrets[0]
	if s.Template != "nota" || s.Title != "idea privada" || s.Notes != "contenido muy secreto" {
		t.Fatalf("conversión incompleta: %+v", s)
	}
	if len(m.visibleNotes()) != 0 {
		t.Fatal("la nota original debió borrarse")
	}
	rows, _ := m.st.ListNotes(true)
	for _, r := range rows {
		if r.DeletedAt == nil {
			t.Error("debe existir tombstone, nota viva no")
		}
	}

	// En BD la entrada del vault está cifrada.
	raw, _ := m.st.ListSecrets()
	if raw[0].Title == "idea privada" || strings.Contains(raw[0].Notes, "secreto") {
		t.Error("campos del vault deberían estar cifrados en disco")
	}
}

var declined bool

func TestSyncWizardFlow(t *testing.T) {
	declined = false
	var started int
	var gotUser string
	var rt *SyncRuntime
	rt = &SyncRuntime{
		Start: func(c sync.Credentials) bool {
			started++
			gotUser = c.Username
			rt.Trigger = func() {}
			rt.Gate = &sync.Gate{}
			return true
		},
		IsConfigured: func() bool { return started > 0 },
		IsDeclined:   func() bool { return declined },
		MarkDeclined: func() { declined = true },
	}

	m, _ := newTestModel(t)
	m = New(m.sess, m.st, WithSyncRuntime(rt)) // sin bóveda → setup; wizard no debe abrir aquí

	if _, err := crypto.CreateVault(m.st, testPw); err != nil {
		t.Fatal(err)
	}
	m = New(m.sess, m.st, WithSyncRuntime(rt))
	if m.state != viewLocked {
		t.Fatalf("precondición lock: %d", m.state)
	}
	m.input.SetValue(testPw)
	m = pressEnter(m)
	if m.state != viewBoard {
		t.Fatalf("precondición board: %d", m.state)
	}
	if m.wiz == nil || !m.wiz.open {
		t.Fatal("primer arranque sin sync debe abrir el asistente")
	}

	// Esc = "más tarde": cierra, marca declined y libera la gate.
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.wiz.open || !declined || m.rt.Gate.Busy() {
		t.Fatal("esc debió cerrar, marcar declined y liberar")
	}

	// Abrir de nuevo desde la paleta conceptual (directo) y completar.
	m.openSyncWizard(false)
	m.wiz.url.SetValue("http://localhost:8000")
	m.wiz.user.SetValue("MiUsuario")
	m.wiz.pass.SetValue("cuenta-pass-1")
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = out.(Model)

	if started != 1 {
		t.Fatalf("Start llamadas=%d", started)
	}
	if gotUser != "miusuario" {
		t.Errorf("usuario normalizado=%q", gotUser)
	}
	if m.wiz.open || m.notice == "" {
		t.Fatalf("wizard=%v notice=%q", m.wiz.open, m.notice)
	}
	if m.rt.Gate.Busy() {
		t.Error("tras configurar la gate debe quedar libre")
	}

	// Validación: URL inválida no arranca.
	m2 := New(m.sess, m.st, WithSyncRuntime(rt))
	_ = m2
	m.openSyncWizard(false)
	m.wiz.url.SetValue("no-es-url")
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = out.(Model)
	if started != 1 || m.errMsg == "" {
		t.Errorf("URL inválida debe fallar validación; err=%q", m.errMsg)
	}
}
