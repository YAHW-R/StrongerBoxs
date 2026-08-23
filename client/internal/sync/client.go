package sync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Parámetros KDF del verifier de cuenta (documentados; el servidor no
// los necesita: solo recibe el hex final).
const (
	kdfTime      = 1
	kdfMemoryKiB = 64 * 1024
	kdfThreads   = 4
	kdfKeyLen    = 32
)

// DeriveVerifier replica el contrato del servidor:
//
//	verifier = hex(SHA256( Argon2id(contraseña_cuenta, salt_servidor) ))
//
// La contraseña de cuenta NUNCA viaja por la red.
func DeriveVerifier(accountPassword, saltB64 string) (string, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		salt, err = base64.RawStdEncoding.DecodeString(saltB64)
		if err != nil {
			return "", fmt.Errorf("sync: salt inválido: %w", err)
		}
	}
	key := argon2.IDKey([]byte(accountPassword), salt, kdfTime, kdfMemoryKiB, kdfThreads, kdfKeyLen)
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:]), nil
}

func newSalt() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

type Credentials struct {
	BaseURL  string
	Username string
	Password string
}

// APIError preserva el código HTTP para decisiones del manager.
type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("sync: servidor %d: %s", e.StatusCode, e.Detail)
}

func statusOf(err error) int {
	if ae, ok := err.(*APIError); ok {
		return ae.StatusCode
	}
	return 0
}

// Client es el cliente HTTP de la API de sincronización.
type Client struct {
	baseURL string
	http    *http.Client

	user, pass string

	token    string
	tokenExp time.Time
}

func NewClient(creds Credentials) (*Client, error) {
	url := strings.TrimRight(creds.BaseURL, "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("sync: URL inválida %q", creds.BaseURL)
	}
	return &Client{
		baseURL: url,
		http:    &http.Client{Timeout: 15 * time.Second},
		user:    creds.Username,
		pass:    creds.Password,
	}, nil
}

func (c *Client) BaseURL() string { return c.baseURL }

// Host devuelve host[:puerto] para chequeos de conectividad.
func (c *Client) Host() string {
	s := strings.TrimPrefix(strings.TrimPrefix(c.baseURL, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if !strings.Contains(s, ":") {
		if strings.HasPrefix(c.baseURL, "https://") {
			s += ":443"
		} else {
			s += ":80"
		}
	}
	return s
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("sync: codificar petición: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "strongboxs-sync/0.1")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sync: conexión: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		detail := strings.TrimSpace(string(raw))
		var apiErr struct {
			Detail any `json:"detail"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Detail != nil {
			if s, ok := apiErr.Detail.(string); ok {
				detail = s
			} else if b, err := json.Marshal(apiErr.Detail); err == nil {
				detail = string(b)
			}
		}
		return &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("sync: decodificar respuesta: %w", err)
		}
	}
	return nil
}

// EnsureAccount registra (si procede; 409 = ya existía) y abre sesión,
// dejando un token en memoria con su caducidad.
func (c *Client) EnsureAccount(ctx context.Context) error {
	regSalt := newSalt()
	vReg, err := DeriveVerifier(c.pass, regSalt)
	if err != nil {
		return err
	}
	err = c.do(ctx, http.MethodPost, "/auth/register",
		registerRequest{Username: c.user, Salt: regSalt, Verifier: vReg}, nil)
	if err != nil && statusOf(err) != http.StatusConflict {
		return err
	}

	var sr authSaltResponse
	if err := c.do(ctx, http.MethodPost, "/auth/salt", authSaltRequest{Username: c.user}, &sr); err != nil {
		return err
	}
	vLogin, err := DeriveVerifier(c.pass, sr.Salt)
	if err != nil {
		return err
	}
	var tr tokenResponse
	if err := c.do(ctx, http.MethodPost, "/auth/login",
		loginRequest{Username: c.user, Verifier: vLogin}, &tr); err != nil {
		return err
	}
	c.token = tr.AccessToken
	c.tokenExp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return nil
}

func (c *Client) ensureToken(ctx context.Context) error {
	if c.token == "" || time.Now().After(c.tokenExp.Add(-2*time.Minute)) {
		return c.EnsureAccount(ctx)
	}
	return nil
}

func (c *Client) Push(ctx context.Context, items []ItemIn) (PushResponse, error) {
	var out PushResponse
	if len(items) == 0 {
		return out, nil
	}
	if err := c.ensureToken(ctx); err != nil {
		return out, err
	}
	err := c.do(ctx, http.MethodPost, "/items/push", PushRequest{Items: items}, &out)
	return out, err
}

func (c *Client) Pull(ctx context.Context, since *time.Time) (PullResponse, error) {
	var out PullResponse
	if err := c.ensureToken(ctx); err != nil {
		return out, err
	}
	err := c.do(ctx, http.MethodPost, "/items/pull", PullRequest{Since: since}, &out)
	return out, err
}
