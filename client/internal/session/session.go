// Package session implementa la sesión estilo sudo de Strongboxs:
//
//   - mantiene la DEK descifrada SOLO en memoria durante un TTL,
//     con auto-lock al expirar (timer en background);
//   - cada operación sensible renueva el TTL (Touch), igual que sudo;
//   - Ensure() es el punto único de entrada: si es el primer inicio
//     (no existe bóveda) guía la creación; si está bloqueada, pide
//     la contraseña maestra.
package session

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yahwr/strongboxs/client/internal/authn"
	"github.com/yahwr/strongboxs/client/internal/crypto"
)

const (
	DefaultTTL        = 15 * time.Minute
	MaxUnlockAttempts = 3
)

var (
	ErrAuthFailed         = errors.New("session: autenticación fallida")
	ErrPasswordsDontMatch = errors.New("session: las contraseñas no coinciden")
	ErrEmptyPassword      = errors.New("session: la contraseña no puede estar vacía")
	ErrNoPrompt           = errors.New("session: no hay prompt configurado para interacción")
)

// PromptFunc solicita una contraseña (sin eco). Inyectable para tests
// y para sustituirla por un input dentro del TUI más adelante.
type PromptFunc func(label string) (string, error)

// Manager gestiona el ciclo de vida de la bóveda desbloqueada.
type Manager struct {
	meta   crypto.MetaStore
	ttl    time.Duration
	prompt PromptFunc

	mu        sync.Mutex
	vault     *crypto.Vault
	expiresAt time.Time
	timer     *time.Timer
	lockCh    chan struct{} // aviso a la UI cuando se bloquea

	authorizer authn.Authenticator // opcional: valida contra el SO (PAM/sudo)
}

func New(meta crypto.MetaStore, ttl time.Duration, prompt PromptFunc) *Manager {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Manager{
		meta:   meta,
		ttl:    ttl,
		prompt: prompt,
		lockCh: make(chan struct{}, 1),
	}
}

// WithAuthorizer exige validación contra el sistema Linux (PAM/sudo)
// antes de permitir el cambio de contraseña maestra. Encadenable.
func (m *Manager) WithAuthorizer(a authn.Authenticator) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authorizer = a
	return m
}

// Ensure devuelve una bóveda utilizable:
//
//  1. si hay sesión viva → la devuelve cacheada (sin pedir nada);
//  2. si no existe bóveda → flujo de primer inicio (crear contraseña);
//  3. si está bloqueada/expirada → solicita contraseña (hasta MaxUnlockAttempts).
func (m *Manager) Ensure() (*crypto.Vault, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.alive() {
		m.touch()
		return m.vault, nil
	}
	m.stopTimer()

	if !crypto.HasVault(m.meta) {
		return m.create()
	}
	return m.unlock()
}

func (m *Manager) alive() bool {
	return m.vault != nil && !m.vault.Locked() && time.Now().Before(m.expiresAt)
}

func (m *Manager) touch() {
	m.expiresAt = time.Now().Add(m.ttl)
	if m.timer != nil {
		m.timer.Stop()
	}
	v := m.vault
	ttl := m.ttl
	m.timer = time.AfterFunc(ttl, func() { m.autoLock(v) })
}

// autoLock solo actúa si sigue siendo "su" bóveda (evita cerrar una sesión nueva).
func (m *Manager) autoLock(v *crypto.Vault) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vault != v {
		return
	}
	v.Lock()
	m.notifyLock()
}

func (m *Manager) notifyLock() {
	select {
	case m.lockCh <- struct{}{}:
	default:
	}
}

func (m *Manager) stopTimer() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

// Lock cierra la sesión manualmente.
func (m *Manager) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopTimer()
	m.expiresAt = time.Time{}
	if m.vault != nil {
		m.vault.Lock()
	}
	m.notifyLock()
}

// Touch renueva el TTL de la sesión viva (llamar tras cada operación sensible).
func (m *Manager) Touch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.alive() {
		m.touch()
	}
}

// Alive informa si hay sesión desbloqueada sin interacción.
func (m *Manager) Alive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive()
}

// ExpiresAt devuelve cuándo se autobloqueará la sesión (cero si inactiva).
func (m *Manager) ExpiresAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.alive() {
		return time.Time{}
	}
	return m.expiresAt
}

// LockEvents entrega un aviso cada vez que la sesión se bloquea
// (manual o automático). La futura lock-screen del TUI lo consumirá.
func (m *Manager) LockEvents() <-chan struct{} { return m.lockCh }

// ---- API programática (para interfaces embebidas como el TUI) ----

// HasVault informa si ya existe una bóveda creada.
func (m *Manager) HasVault() bool { return crypto.HasVault(m.meta) }

// CreateWith crea la bóveda en el primer inicio y deja la sesión activa,
// sin pedir nada por terminal.
func (m *Manager) CreateWith(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopTimer()
	v, err := crypto.CreateVault(m.meta, password)
	if err != nil {
		return err
	}
	m.vault = v
	m.touch()
	return nil
}

// UnlockWith desbloquea con la contraseña dada sin interacción por terminal.
// Devuelve crypto.ErrWrongPassword si la clave no es válida.
func (m *Manager) UnlockWith(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.alive() {
		m.touch()
		return nil
	}
	m.stopTimer()
	v, err := crypto.OpenVault(m.meta, password)
	if err != nil {
		return err
	}
	m.vault = v
	m.touch()
	return nil
}

// Remaining devuelve el tiempo hasta el auto-bloqueo (0 sin sesión viva).
func (m *Manager) Remaining() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.alive() {
		return 0
	}
	return time.Until(m.expiresAt)
}

// ---- operaciones de campo (renuevan el TTL como haría sudo) ----

// SealField cifra un campo usando la sesión; pide contraseña si expiró.
func (m *Manager) SealField(plaintext string) (string, error) {
	v, err := m.Ensure()
	if err != nil {
		return "", err
	}
	env, err := v.Seal(plaintext)
	if err != nil {
		return "", err
	}
	m.Touch()
	return env, nil
}

// UnsealField descifra un campo usando la sesión; pide contraseña si expiró.
func (m *Manager) UnsealField(envelope string) (string, error) {
	v, err := m.Ensure()
	if err != nil {
		return "", err
	}
	s, err := v.Unseal(envelope)
	if err != nil {
		return "", err
	}
	m.Touch()
	return s, nil
}

// ChangeMasterPassword cambia la contraseña tras verificar la actual.
// Solo re-envuelve la DEK: los datos cifrados no cambian.
// Si hay autorizador (PAM/sudo), exige primero la contraseña del sistema Linux.
func (m *Manager) ChangeMasterPassword() error {
	v, err := m.Ensure()
	if err != nil {
		return err
	}

	if m.authorizer != nil {
		osPass, err := m.prompt(fmt.Sprintf("Contraseña de %s (sistema): ", authn.Username()))
		if err != nil {
			return err
		}
		if err := m.authorizer.Authenticate(osPass); err != nil {
			return err
		}
	}

	current, err := m.prompt("Contraseña actual: ")
	if err != nil {
		return err
	}
	next, err := m.prompt("Nueva contraseña maestra: ")
	if err != nil {
		return err
	}
	confirm, err := m.prompt("Repite la nueva contraseña: ")
	if err != nil {
		return err
	}
	if next != confirm {
		return ErrPasswordsDontMatch
	}
	if err := v.ChangePassword(current, next); err != nil {
		return err
	}
	m.Touch()
	return nil
}

// ---- flujos internos (con mutex ya tomado) ----

func (m *Manager) create() (*crypto.Vault, error) {
	if m.prompt == nil {
		return nil, ErrNoPrompt
	}
	pw1, err := m.prompt("Nueva contraseña maestra: ")
	if err != nil {
		return nil, err
	}
	if pw1 == "" {
		return nil, ErrEmptyPassword
	}
	pw2, err := m.prompt("Confirma la contraseña: ")
	if err != nil {
		return nil, err
	}
	if pw1 != pw2 {
		return nil, ErrPasswordsDontMatch
	}
	v, err := crypto.CreateVault(m.meta, pw1)
	if err != nil {
		return nil, err
	}
	m.vault = v
	m.touch()
	fmt.Println("✓ Bóveda creada.")
	return v, nil
}

func (m *Manager) unlock() (*crypto.Vault, error) {
	if m.prompt == nil {
		return nil, ErrNoPrompt
	}
	for attempt := 1; attempt <= MaxUnlockAttempts; attempt++ {
		pw, err := m.prompt(fmt.Sprintf("Contraseña maestra (%d/%d): ", attempt, MaxUnlockAttempts))
		if err != nil {
			return nil, err
		}
		v, err := crypto.OpenVault(m.meta, pw)
		if err == nil {
			m.vault = v
			m.touch()
			return v, nil
		}
		if !errors.Is(err, crypto.ErrWrongPassword) {
			return nil, err
		}
	}
	return nil, ErrAuthFailed
}
