package store

import (
	"fmt"
	"time"
)

// Operaciones de sincronización.
//
// El conflicto se resuelve POR FECHA (LWW temporal): cada fila lleva
// updated_at en formato RFC3339Nano UTC, cuya comparación lexicográfica
// equivale a la cronológica. El upsert remoto solo aplica si la fecha
// entrante es estrictamente mayor que la local — decisión atómica en SQL,
// sin carreras aunque dos dispositivos sincronicen a la vez.

func fmtTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

func fmtTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}

// UpsertRemoteNote inserta o actualiza desde el servidor respetando LWW por fecha.
// Devuelve applied=true si la fila local quedó con los datos remotos.
func (s *Store) UpsertRemoteNote(n Note) (bool, error) {
	res, err := s.db.Exec(`
		INSERT INTO notes (uuid, title, body, color, pinned, archived,
		                   version, dirty, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT(uuid) DO UPDATE SET
			title      = excluded.title,
			body       = excluded.body,
			color      = excluded.color,
			pinned     = excluded.pinned,
			archived   = excluded.archived,
			version    = excluded.version,
			dirty      = 0,
			updated_at = excluded.updated_at,
			deleted_at = excluded.deleted_at
		WHERE excluded.updated_at > notes.updated_at`,
		n.UUID, n.Title, n.Body, n.Color, boolToInt(n.Pinned), boolToInt(n.Archived),
		n.Version, fmtTime(n.CreatedAt), fmtTime(n.UpdatedAt), fmtTimePtr(n.DeletedAt))
	if err != nil {
		return false, fmt.Errorf("store: upsert nota remota %s: %w", n.UUID, err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// UpsertRemoteSecret es el equivalente para entradas de contraseñas.
func (s *Store) UpsertRemoteSecret(sc Secret) (bool, error) {
	res, err := s.db.Exec(`
		INSERT INTO secrets (uuid, title, username, password, url, notes,
		                     version, dirty, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT(uuid) DO UPDATE SET
			title      = excluded.title,
			username   = excluded.username,
			password   = excluded.password,
			url        = excluded.url,
			notes      = excluded.notes,
			version    = excluded.version,
			dirty      = 0,
			updated_at = excluded.updated_at,
			deleted_at = excluded.deleted_at
		WHERE excluded.updated_at > secrets.updated_at`,
		sc.UUID, sc.Title, sc.Username, sc.Password, sc.URL, sc.Notes,
		sc.Version, fmtTime(sc.CreatedAt), fmtTime(sc.UpdatedAt), fmtTimePtr(sc.DeletedAt))
	if err != nil {
		return false, fmt.Errorf("store: upsert secreto remoto %s: %w", sc.UUID, err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// ListNotesForSync devuelve TODAS las filas dirty, incluidas las borradas
// (tombstones necesarios para replicar el delete en el servidor).
func (s *Store) ListNotesForSync() ([]Note, error) {
	rows, err := s.db.Query(`SELECT ` + noteCols + ` FROM notes WHERE dirty=1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: notas para sync: %w", err)
	}
	defer rows.Close()

	out := make([]Note, 0, 8)
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListSecretsForSync ídem para secretos.
func (s *Store) ListSecretsForSync() ([]Secret, error) {
	rows, err := s.db.Query(`SELECT ` + secretCols + ` FROM secrets WHERE dirty=1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: secretos para sync: %w", err)
	}
	defer rows.Close()

	out := make([]Secret, 0, 8)
	for rows.Next() {
		sc, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// MarkSecretsSynced limpia dirty tras confirmación del servidor.
func (s *Store) MarkSecretsSynced(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE secrets SET dirty=0 WHERE id=?`, id); err != nil {
			return fmt.Errorf("store: marcar secreto %d sincronizado: %w", id, err)
		}
	}
	return nil
}
