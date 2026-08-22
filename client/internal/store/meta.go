package store

import (
	"database/sql"
	"fmt"
)

// GetMeta lee un valor de vault_meta. ok=false si no existe la clave.
func (s *Store) GetMeta(key string) (val string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM vault_meta WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: leer meta %q: %w", key, err)
	}
	return val, true, nil
}

// SetMeta inserta/actualiza claves en una transacción (escritura atómica).
func (s *Store) SetMeta(items map[string]string) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: tx meta: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO vault_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if err != nil {
		return fmt.Errorf("store: preparar upsert meta: %w", err)
	}
	defer stmt.Close()

	for k, v := range items {
		if _, err := stmt.Exec(k, v); err != nil {
			return fmt.Errorf("store: escribir meta %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit meta: %w", err)
	}
	return nil
}
