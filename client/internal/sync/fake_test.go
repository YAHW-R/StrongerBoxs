package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yahwr/strongboxs/client/internal/store"
)

// ---- servidor fake que replica el contrato de la API FastAPI ----

type fakeUser struct {
	salt     string
	verifier string
}

type fakeItem struct {
	kind      string
	payload   map[string]any
	version   int
	deleted   bool
	updatedAt time.Time
	syncedAt  time.Time
}

type fakeAPI struct {
	mu    sync.Mutex
	users map[string]*fakeUser
	items map[string]*fakeItem

	lastPullSince *time.Time
	pushCalls     int
	srv           *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		users: map[string]*fakeUser{},
		items: map[string]*fakeItem{},
	}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	decodeInto := func(r *http.Request, v any) { _ = json.NewDecoder(r.Body).Decode(v) }

	mux.HandleFunc("POST /auth/salt", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
		}
		decodeInto(r, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if u, ok := f.users[body.Username]; ok {
			writeJSON(w, 200, map[string]string{"salt": u.salt})
			return
		}
		writeJSON(w, 200, map[string]string{"salt": "ZHVtbXktc2FsdC1kZXRlcm1pbmlzdGE"})
	})

	mux.HandleFunc("POST /auth/register", func(w http.ResponseWriter, r *http.Request) {
		var body registerRequest
		decodeInto(r, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.users[body.Username]; ok {
			writeJSON(w, 409, map[string]string{"detail": "El usuario ya existe"})
			return
		}
		f.users[body.Username] = &fakeUser{salt: body.Salt, verifier: body.Verifier}
		writeJSON(w, 201, map[string]any{"access_token": "tok-reg", "expires_in": 3600})
	})

	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body loginRequest
		decodeInto(r, &body)
		f.mu.Lock()
		u, ok := f.users[body.Username]
		f.mu.Unlock()
		if !ok || u.verifier != body.Verifier {
			writeJSON(w, 401, map[string]string{"detail": "Credenciales inválidas"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "tok-" + body.Username,
			"token_type": "bearer", "expires_in": 3600, "user_id": "00000000-0000-0000-0000-000000000001"})
	})

	bearer := func(r *http.Request) bool { return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") }

	mux.HandleFunc("POST /items/push", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) {
			writeJSON(w, 401, map[string]string{"detail": "no auth"})
			return
		}
		var body PushRequest
		decodeInto(r, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pushCalls++
		resp := PushResponse{}
		for _, it := range body.Items {
			existing, ok := f.items[it.ItemUUID]
			switch {
			case !ok:
				f.items[it.ItemUUID] = &fakeItem{kind: it.Kind, payload: payloadToMap(it.Payload),
					version: it.Version, deleted: it.Deleted, updatedAt: it.UpdatedAt,
					syncedAt: time.Now().UTC()}
				resp.Accepted = append(resp.Accepted, ItemResult{ItemUUID: it.ItemUUID, Status: "accepted"})
			case it.UpdatedAt.After(existing.updatedAt):
				// LWW POR FECHA: gana la actualización más nueva.
				existing.payload = payloadToMap(it.Payload)
				existing.version = it.Version
				existing.deleted = it.Deleted
				existing.updatedAt = it.UpdatedAt
				existing.syncedAt = time.Now().UTC()
				resp.Accepted = append(resp.Accepted, ItemResult{ItemUUID: it.ItemUUID, Status: "accepted"})
			default:
				v := existing.version
				resp.Skipped = append(resp.Skipped, ItemResult{ItemUUID: it.ItemUUID,
					Status: "skipped", Reason: ptrStr("stale_date"), ServerVersion: &v})
			}
		}
		writeJSON(w, 200, resp)
	})

	mux.HandleFunc("POST /items/pull", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) {
			writeJSON(w, 401, map[string]string{"detail": "no auth"})
			return
		}
		var body PullRequest
		decodeInto(r, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastPullSince = body.Since
		resp := PullResponse{ServerTime: time.Now().UTC()}
		for uuid_, it := range f.items {
			if body.Since != nil && !it.syncedAt.After(*body.Since) {
				continue
			}
			resp.Items = append(resp.Items, ItemOut{
				ItemUUID: uuid_, Kind: it.kind, Version: it.version, Deleted: it.deleted,
				Payload: payloadFromMap(it.payload), UpdatedAt: it.updatedAt, SyncedAt: it.syncedAt,
			})
		}
		writeJSON(w, 200, resp)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func ptrStr(s string) *string { return &s }

func payloadToMap(p ItemPayload) map[string]any {
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func payloadFromMap(m map[string]any) ItemPayload {
	b, _ := json.Marshal(m)
	var p ItemPayload
	_ = json.Unmarshal(b, &p)
	return p
}

func (f *fakeAPI) URL() string    { return f.srv.URL }
func (f *fakeAPI) pushCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.pushCalls }
func (f *fakeAPI) item(uuid_ string) *fakeItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *f.items[uuid_]
	return &cp
}

func credsFor(url string) Credentials {
	return Credentials{BaseURL: url, Username: "tester", Password: "clave-de-cuenta-123"}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestManager(t *testing.T, url string, st *store.Store) *Manager {
	t.Helper()
	cl, err := NewClient(credsFor(url))
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{st: st, cl: cl, interval: 50 * time.Millisecond}
}
