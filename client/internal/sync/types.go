// Package sync implementa la sincronización bidireccional de Strongboxs
// contra el servidor FastAPI Zero-Knowledge.
//
// Filosofía (requisito del producto):
//   - Detección por FECHAS: cada entidad lleva updated_at (RFC3339Nano UTC)
//     y gana la copia con fecha estrictamente mayor — nunca se compara
//     contra el reloj del servidor para decidir contenido.
//   - Segundo plano: un goroutine reintenta cada intervalo SOLO si hay red;
//     jamás bloquea la TUI ni pide intervención.
//   - Escritura doble: todo cambio local queda persistido al instante
//     (dirty=1) y el ciclo lo sube; lo remoto llega por pull y se fusiona.
package sync

import "time"

// ItemPayload es el sobre JSON que viaja al servidor: campos sensibles ya
// cifrados ("sb1.…") + metadatos en claro. Un único struct cubre ambos
// kind gracias a omitempty (el servidor rechaza claves ajenas a cada tipo).
type ItemPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Color    string `json:"color,omitempty"`
	Pinned   bool   `json:"pinned,omitempty"`
	Archived bool   `json:"archived,omitempty"`

	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	URL      string `json:"url,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

const (
	KindNote   = "note"
	KindSecret = "secret"
)

type ItemIn struct {
	ItemUUID  string      `json:"item_uuid"`
	Kind      string      `json:"kind"`
	Payload   ItemPayload `json:"payload"`
	Version   int         `json:"version"`
	Deleted   bool        `json:"deleted"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type PushRequest struct {
	Items []ItemIn `json:"items"`
}

type ItemResult struct {
	ItemUUID      string  `json:"item_uuid"`
	Status        string  `json:"status"`
	Reason        *string `json:"reason"`
	ServerVersion *int    `json:"server_version"`
}

type PushResponse struct {
	Accepted []ItemResult `json:"accepted"`
	Skipped  []ItemResult `json:"skipped"`
}

type PullRequest struct {
	Since *time.Time `json:"since"`
}

type ItemOut struct {
	ItemUUID  string      `json:"item_uuid"`
	Kind      string      `json:"kind"`
	Payload   ItemPayload `json:"payload"`
	Version   int         `json:"version"`
	Deleted   bool        `json:"deleted"`
	UpdatedAt time.Time   `json:"updated_at"`
	SyncedAt  time.Time   `json:"synced_at"`
}

type PullResponse struct {
	Items      []ItemOut `json:"items"`
	ServerTime time.Time `json:"server_time"`
}

type authSaltRequest struct {
	Username string `json:"username"`
}

type authSaltResponse struct {
	Salt string `json:"salt"`
}

type registerRequest struct {
	Username string `json:"username"`
	Salt     string `json:"salt"`
	Verifier string `json:"verifier"`
}

type loginRequest struct {
	Username string `json:"username"`
	Verifier string `json:"verifier"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	UserID      string `json:"user_id"`
}
