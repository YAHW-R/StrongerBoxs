package store

import "time"

// Note es una nota estilo Keep. Title/Body son BLOB en BD porque
// a partir de la fase de cifrado contendrán ciphertext AES-256-GCM.
type Note struct {
	ID        int64
	UUID      string
	Title     string
	Body      string
	Color     string // hex, ej. "#F9AB00"; '' = color por defecto
	Pinned    bool
	Archived  bool
	Version   int64
	Dirty     bool // pendiente de sincronizar con el servidor
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Secret es una entrada de contraseña. Todos los campos sensibles van
// como BLOB para almacenar ciphertext (Zero-Knowledge: nunca en claro).
//
// Template identifica la plantilla que definió los campos; Extra es un
// JSON cifrado {clave:valor} con los campos NO estándar de esa plantilla.
type Secret struct {
	ID        int64
	UUID      string
	Template  string
	Title     string
	Username  string
	Password  string
	URL       string
	Notes     string
	Extra     string // sobre "sb1.*" de un JSON map[string]string
	Version   int64
	Dirty     bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}
