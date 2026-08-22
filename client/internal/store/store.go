// Package store implementa la persistencia local de Strongboxs.
//
// SQLite puro en Go (modernc.org/sqlite, sin CGO) para portabilidad y
// cross-compilación trivial. Los campos sensibles son BLOB: la capa
// de cifrado (fase siguiente) escribirá ciphertext, nunca texto claro.
package store

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS notes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid       TEXT NOT NULL UNIQUE,
	title      BLOB NOT NULL DEFAULT '',
	body       BLOB NOT NULL DEFAULT '',
	color      TEXT NOT NULL DEFAULT '',
	pinned     INTEGER NOT NULL DEFAULT 0,
	archived   INTEGER NOT NULL DEFAULT 0,
	version    INTEGER NOT NULL DEFAULT 1,
	dirty      INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_notes_updated ON notes(updated_at);

CREATE TABLE IF NOT EXISTS secrets (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid       TEXT NOT NULL UNIQUE,
	title      BLOB NOT NULL,
	username   BLOB NOT NULL DEFAULT '',
	password   BLOB NOT NULL,
	url        TEXT NOT NULL DEFAULT '',
	notes      BLOB NOT NULL DEFAULT '',
	version    INTEGER NOT NULL DEFAULT 1,
	dirty      INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_secrets_updated ON secrets(updated_at);
`

// Store encapsula el acceso a la BD local.
type Store struct {
	db *sql.DB
}

// DefaultPath devuelve $XDG_DATA_HOME/strongboxs/strongboxs.db
// (o ~/.local/share/strongboxs/strongboxs.db).
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("store: home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "strongboxs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("store: crear directorio: %w", err)
	}
	return filepath.Join(dir, "strongboxs.db"), nil
}

// Open abre (y migra) la BD. Con path vacío usa DefaultPath.
func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: abrir bd: %w", err)
	}
	// Un único proceso TUI: una conexión evita bloqueos WAL entre goroutines.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrar esquema: %w", err)
	}
	// La BD contendrá ciphertext: permisos restrictivos.
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: chmod bd: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---- utilidades ----

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand: no falla en plataformas soportadas
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func nowUTC() time.Time { return time.Now().UTC() }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	return &t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- Notes ----

// CreateNote inserta una nota y devuelve la entidad completa (ID, timestamps).
func (s *Store) CreateNote(title, body, color string) (Note, error) {
	n := Note{
		UUID:      newUUID(),
		Title:     title,
		Body:      body,
		Color:     color,
		Version:   1,
		Dirty:     true,
		CreatedAt: nowUTC(),
		UpdatedAt: nowUTC(),
	}
	res, err := s.db.Exec(`
		INSERT INTO notes (uuid, title, body, color, version, dirty, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		n.UUID, n.Title, n.Body, n.Color, n.Version, boolToInt(n.Dirty),
		n.CreatedAt.Format(time.RFC3339Nano), n.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Note{}, fmt.Errorf("store: crear nota: %w", err)
	}
	n.ID, err = res.LastInsertId()
	return n, err
}

const noteCols = `id, uuid, title, body, color, pinned, archived, version, dirty, created_at, updated_at, deleted_at`

func scanNote(row interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var pinned, archived, dirty int
	var createdAt, updatedAt string
	var deletedAt sql.NullString
	err := row.Scan(&n.ID, &n.UUID, &n.Title, &n.Body, &n.Color,
		&pinned, &archived, &n.Version, &dirty,
		&createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return Note{}, err
	}
	n.Pinned, n.Archived, n.Dirty = pinned == 1, archived == 1, dirty == 1
	n.CreatedAt, n.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	n.DeletedAt = parseTimePtr(deletedAt)
	return n, nil
}

// ListNotes devuelve notas vivas (no borradas). Si includeArchived es false,
// excluye archivadas. Orden: fijadas primero, luego por actualización descendente.
func (s *Store) ListNotes(includeArchived bool) ([]Note, error) {
	q := `SELECT ` + noteCols + ` FROM notes WHERE deleted_at IS NULL`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY pinned DESC, updated_at DESC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("store: listar notas: %w", err)
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateNote guarda cambios de contenido y metadatos, incrementa versión
// y marca dirty para la futura sincronización.
func (s *Store) UpdateNote(n *Note) error {
	n.Version++
	n.Dirty = true
	n.UpdatedAt = nowUTC()
	res, err := s.db.Exec(`
		UPDATE notes SET title=?, body=?, color=?, pinned=?, archived=?,
		                 version=?, dirty=1, updated_at=?
		WHERE id=?`,
		n.Title, n.Body, n.Color, boolToInt(n.Pinned), boolToInt(n.Archived),
		n.Version, n.UpdatedAt.Format(time.RFC3339Nano), n.ID)
	if err != nil {
		return fmt.Errorf("store: actualizar nota %d: %w", n.ID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("store: nota %d no encontrada", n.ID)
	}
	return nil
}

// SoftDeleteNote marca la nota como borrada (necesario para replicar
// el borrado al servidor sin perder datos).
func (s *Store) SoftDeleteNote(id int64) error {
	now := nowUTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE notes SET deleted_at=?, dirty=1 WHERE id=?`, now, id)
	if err != nil {
		return fmt.Errorf("store: borrar nota %d: %w", id, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("store: nota %d no encontrada", id)
	}
	return nil
}

// ---- Secrets ----

// CreateSecret inserta una entrada de contraseña. Los campos llegan ya
// cifrados desde la capa superior (o en claro durante desarrollo).
func (s *Store) CreateSecret(sc Secret) (Secret, error) {
	sc.UUID = newUUID()
	sc.Version = 1
	sc.Dirty = true
	sc.CreatedAt, sc.UpdatedAt = nowUTC(), nowUTC()
	res, err := s.db.Exec(`
		INSERT INTO secrets (uuid, title, username, password, url, notes, version, dirty, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.UUID, sc.Title, sc.Username, sc.Password, sc.URL, sc.Notes,
		sc.Version, boolToInt(sc.Dirty),
		sc.CreatedAt.Format(time.RFC3339Nano), sc.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Secret{}, fmt.Errorf("store: crear secreto: %w", err)
	}
	sc.ID, err = res.LastInsertId()
	return sc, err
}

const secretCols = `id, uuid, title, username, password, url, notes, version, dirty, created_at, updated_at, deleted_at`

func scanSecret(row interface{ Scan(...any) error }) (Secret, error) {
	var sc Secret
	var dirty int
	var createdAt, updatedAt string
	var deletedAt sql.NullString
	err := row.Scan(&sc.ID, &sc.UUID, &sc.Title, &sc.Username, &sc.Password,
		&sc.URL, &sc.Notes, &sc.Version, &dirty, &createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return Secret{}, err
	}
	sc.Dirty = dirty == 1
	sc.CreatedAt, sc.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	sc.DeletedAt = parseTimePtr(deletedAt)
	return sc, nil
}

// ListSecrets devuelve las entradas vivas ordenadas por título.
func (s *Store) ListSecrets() ([]Secret, error) {
	rows, err := s.db.Query(
		`SELECT ` + secretCols + ` FROM secrets WHERE deleted_at IS NULL ORDER BY title`)
	if err != nil {
		return nil, fmt.Errorf("store: listar secretos: %w", err)
	}
	defer rows.Close()

	var out []Secret
	for rows.Next() {
		sc, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// GetSecretByUUID localiza una entrada por UUID (clave de sincronización).
func (s *Store) GetSecretByUUID(uuid string) (Secret, error) {
	row := s.db.QueryRow(`SELECT `+secretCols+` FROM secrets WHERE uuid=?`, uuid)
	sc, err := scanSecret(row)
	if err == sql.ErrNoRows {
		return Secret{}, fmt.Errorf("store: secreto %s no encontrado", uuid)
	}
	return sc, err
}

// UpdateSecret guarda cambios, incrementa versión y marca dirty.
func (s *Store) UpdateSecret(sc *Secret) error {
	sc.Version++
	sc.Dirty = true
	sc.UpdatedAt = nowUTC()
	res, err := s.db.Exec(`
		UPDATE secrets SET title=?, username=?, password=?, url=?, notes=?,
		                   version=?, dirty=1, updated_at=?
		WHERE id=?`,
		sc.Title, sc.Username, sc.Password, sc.URL, sc.Notes,
		sc.Version, sc.UpdatedAt.Format(time.RFC3339Nano), sc.ID)
	if err != nil {
		return fmt.Errorf("store: actualizar secreto %d: %w", sc.ID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("store: secreto %d no encontrado", sc.ID)
	}
	return nil
}

// SoftDeleteSecret marca una entrada como borrada.
func (s *Store) SoftDeleteSecret(id int64) error {
	now := nowUTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE secrets SET deleted_at=?, dirty=1 WHERE id=?`, now, id)
	if err != nil {
		return fmt.Errorf("store: borrar secreto %d: %w", id, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("store: secreto %d no encontrado", id)
	}
	return nil
}

// MarkNotesSynced limpia el flag dirty tras subir cambios al servidor (fase sync).
func (s *Store) MarkNotesSynced(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE notes SET dirty=0 WHERE id=?`, id); err != nil {
			return fmt.Errorf("store: marcar nota %d sincronizada: %w", id, err)
		}
	}
	return nil
}
