package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/yahwr/strongboxs/client/internal/store"
)

// ---- Definición de plantillas del vault ----

// fieldDef describe un campo de una plantilla. La ESTRUCTURA no es
// sensible (solo etiquetas); los VALORES viajan cifrados.
type fieldDef struct {
	Key       string `json:"key"`                 // username|password|url|notes | clave libre
	Label     string `json:"label"`               // etiqueta visible
	Sensitive bool   `json:"sensitive,omitempty"` // enmascarada + copiable con 'y'
	Multi     bool   `json:"multi,omitempty"`     // multilínea (máx. 1 por plantilla)
	IsTitle   bool   `json:"title,omitempty"`     // su valor encabeza la tarjeta
}

type vaultTemplate struct {
	Name   string     `json:"name"`
	Title  string     `json:"title"`
	Icon   string     `json:"icon,omitempty"`
	Fields []fieldDef `json:"fields"`
}

const (
	defaultTemplate = "simple"
	keyTitle        = "title" // siempre presente como cabecera, nunca listado
)

// Claves estándar mapeadas a columnas propias del modelo Secret.
var standardKeys = map[string]bool{
	"username": true, "password": true, "url": true, "notes": true,
}

func builtinVaultTemplates() []vaultTemplate {
	return []vaultTemplate{
		{
			Name: "simple", Title: "Simple", Icon: "🔑",
			Fields: []fieldDef{
				{Key: "username", Label: "Usuario"},
				{Key: "password", Label: "Valor", Sensitive: true},
			},
		},
		{
			Name: "web", Title: "Página web", Icon: "🌐",
			Fields: []fieldDef{
				{Key: "url", Label: "URL"},
				{Key: "username", Label: "Usuario"},
				{Key: "password", Label: "Contraseña", Sensitive: true},
				{Key: "notes", Label: "Notas", Multi: true},
			},
		},
		{
			Name: "email", Title: "Correo", Icon: "✉️",
			Fields: []fieldDef{
				{Key: "username", Label: "Email"},
				{Key: "password", Label: "Contraseña", Sensitive: true},
				{Key: "url", Label: "Webmail"},
				{Key: "notes", Label: "Notas", Multi: true},
			},
		},
		{
			Name: "nota", Title: "Nota segura", Icon: "🗒",
			Fields: []fieldDef{
				{Key: "notes", Label: "Contenido", Multi: true},
			},
		},
	}
}

// loadVaultTemplates une integradas + personalizadas (BD), ordenadas por nombre.
func (m *Model) loadVaultTemplates() []vaultTemplate {
	list := builtinVaultTemplates()
	customs, err := m.st.ListTemplates()
	if err == nil {
		for _, ct := range customs {
			vt, perr := decodeCustomTemplate(ct)
			if perr != nil {
				continue
			}
			list = append(list, vt)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func decodeCustomTemplate(ct store.Template) (vaultTemplate, error) {
	var fields []fieldDef
	if err := json.Unmarshal([]byte(ct.FieldsDef), &fields); err != nil {
		return vaultTemplate{}, fmt.Errorf("plantilla %q corrupta: %w", ct.Name, err)
	}
	if len(fields) == 0 {
		return vaultTemplate{}, fmt.Errorf("plantilla %q sin campos", ct.Name)
	}
	icon := ct.Icon
	if icon == "" {
		icon = "🔐"
	}
	return vaultTemplate{Name: ct.Name, Title: ct.Title, Icon: icon, Fields: fields}, nil
}

func (m *Model) findTemplate(name string) (vaultTemplate, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range m.loadVaultTemplates() {
		if t.Name == name {
			return t, true
		}
	}
	return vaultTemplate{}, false
}

func (m *Model) templateNames() string {
	var names []string
	for _, t := range m.loadVaultTemplates() {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

// encodeCustomTemplate serializa una plantilla personalizada para la BD.
func encodeCustomTemplate(vt vaultTemplate) store.Template {
	b, _ := json.Marshal(vt.Fields)
	return store.Template{Name: vt.Name, Title: vt.Title, Icon: vt.Icon, FieldsDef: string(b)}
}

// ---- Constructor de plantillas (:newp), por selección ----

// fieldType es un tipo de campo seleccionable con ←/→.
type fieldType struct {
	ID   string
	Name string
}

var fieldTypes = []fieldType{
	{"texto", "Texto"},
	{"secreto", "Secreto"},
	{"config", "Configuración"},
	{"titulo", "Título"},
	{"multilinea", "Multilínea"},
}

func typeFlags(id string) (sensitive, multi, isTitle bool) {
	switch id {
	case "secreto":
		return true, false, false
	case "multilinea":
		return false, true, false
	case "titulo":
		return false, false, true
	default: // texto, config
		return false, false, false
	}
}

type tplRow struct {
	label   textinput.Model
	typeIdx int
}

// tplBuilder construye plantillas por formulario: sin texto de tipos,
// sin ':' que chocar con la barra de comandos.
type tplBuilder struct {
	name textinput.Model
	rows []tplRow
}

func newTplBuilder(presetName string) *tplBuilder {
	b := &tplBuilder{name: newTextInput("Nombre interno (para :new)", false)}
	b.name.SetValue(presetName)
	b.appendRow()
	return b
}

func (b *tplBuilder) appendRow() {
	b.rows = append(b.rows, tplRow{label: newTextInput("Etiqueta del campo", false)})
}

// deleteRow elimina la fila i; deja siempre al menos una fila vacía.
func (b *tplBuilder) deleteRow(i int) {
	if i < 0 || i >= len(b.rows) {
		return
	}
	b.rows = append(b.rows[:i], b.rows[i+1:]...)
	if len(b.rows) == 0 {
		b.appendRow()
	}
}

// cycleType rota el tipo de la fila i (+1 o -1).
func (b *tplBuilder) cycleType(i, delta int) {
	if i < 0 || i >= len(b.rows) {
		return
	}
	n := len(fieldTypes)
	b.rows[i].typeIdx = ((b.rows[i].typeIdx+delta)%n + n) % n
}

func rowTypeName(r tplRow) string { return fieldTypes[r.typeIdx].Name }

// rowsToFields valida y convierte las filas a fieldDefs.
func (b *tplBuilder) rowsToFields() ([]fieldDef, error) {
	var out []fieldDef
	seen := map[string]bool{}
	multis := 0
	for _, r := range b.rows {
		label := strings.TrimSpace(r.label.Value())
		if label == "" {
			continue
		}
		key := sanitizeTemplateKey(label)
		if seen[key] {
			return nil, fmt.Errorf("campo duplicado %q", label)
		}
		seen[key] = true
		fd := fieldDef{Key: key, Label: label}
		fd.Sensitive, fd.Multi, fd.IsTitle = typeFlags(fieldTypes[r.typeIdx].ID)
		if fd.Multi {
			multis++
			if multis > 1 {
				return nil, fmt.Errorf("solo se permite un campo Multilínea")
			}
		}
		out = append(out, fd)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("añade al menos un campo (ctrl+n)")
	}
	return out, nil
}

// widgetCount para navegación: nombre + 2 widgets por fila.
func (b *tplBuilder) widgetCount() int { return 1 + len(b.rows)*2 }

func sanitizeTemplateKey(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		s = "campo"
	}
	return s
}
