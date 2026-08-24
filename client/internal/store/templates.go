package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Migraciones incrementales para BDs creadas con versiones anteriores.
// SQLite no soporta IF NOT EXISTS en ADD COLUMN: intentamos y toleramos
// el error de columna duplicada.
func (s *Store) migrate() {
	alters := []string{
		`ALTER TABLE secrets ADD COLUMN template TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE secrets ADD COLUMN extra BLOB NOT NULL DEFAULT ''`,
	}
	for _, q := range alters {
		if _, err := s.db.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			// No fatal: registramos y seguimos; la app funciona con lo que haya.
			fmt.Printf("strongboxs: aviso migración: %v\n", err)
		}
	}
}

// ---- Plantillas personalizadas de vault ----
//
// Solo se persisten las plantillas del USUARIO; las integradas viven en
// código. fields_json es la estructura de campos (labels): no es sensible.

type Template struct {
	Name      string // slug: usado en :new <nombre>
	Title     string // nombre visible
	Icon      string
	Builtin   bool
	FieldsDef string // JSON [{key,label,sensitive,multi}]
}

const createTemplates = `
CREATE TABLE IF NOT EXISTS templates (
	name        TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	icon        TEXT NOT NULL DEFAULT '',
	fields_json TEXT NOT NULL DEFAULT '[]'
);
`

func (s *Store) CreateTemplate(t Template) error {
	if t.Name == "" {
		return fmt.Errorf("store: plantilla sin nombre")
	}
	_, err := s.db.Exec(`
		INSERT INTO templates (name, title, icon, fields_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET title=excluded.title,
		                               icon=excluded.icon,
		                               fields_json=excluded.fields_json`,
		t.Name, t.Title, t.Icon, t.FieldsDef)
	if err != nil {
		return fmt.Errorf("store: guardar plantilla %q: %w", t.Name, err)
	}
	return nil
}

func (s *Store) GetTemplate(name string) (Template, bool, error) {
	row := s.db.QueryRow(
		`SELECT name, title, icon, fields_json FROM templates WHERE name=?`, name)
	var t Template
	err := row.Scan(&t.Name, &t.Title, &t.Icon, &t.FieldsDef)
	if err == sql.ErrNoRows {
		return Template{}, false, nil
	}
	if err != nil {
		return Template{}, false, fmt.Errorf("store: leer plantilla %q: %w", name, err)
	}
	return t, true, nil
}

func (s *Store) ListTemplates() ([]Template, error) {
	rows, err := s.db.Query(`SELECT name, title, icon, fields_json FROM templates ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: listar plantillas: %w", err)
	}
	defer rows.Close()

	out := []Template{}
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.Name, &t.Title, &t.Icon, &t.FieldsDef); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTemplate(name string) error {
	res, err := s.db.Exec(`DELETE FROM templates WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("store: borrar plantilla %q: %w", name, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("store: plantilla %q no encontrada", name)
	}
	return nil
}
