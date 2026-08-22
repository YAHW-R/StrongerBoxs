package session

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yahwr/strongboxs/client/internal/crypto"
)

// fakeMeta KV en memoria (misma semántica que store.Store sobre vault_meta).
type fakeMeta struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeMeta() *fakeMeta { return &fakeMeta{data: map[string]string{}} }

func (f *fakeMeta) GetMeta(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeMeta) SetMeta(items map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range items {
		f.data[k] = v
	}
	return nil
}

// scriptedPrompt responde en orden y registra las preguntas recibidas.
type scriptedPrompt struct {
	mu       sync.Mutex
	replies  []string
	i        int
	asked    []string
	exhaustF bool // si se agota el guion: error (true) o repetir última (false)
}

func newPrompt(replies ...string) *scriptedPrompt {
	return &scriptedPrompt{replies: replies}
}

func (s *scriptedPrompt) ask(label string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, label)
	if s.i >= len(s.replies) {
		if s.exhaustF {
			return "", errors.New("guion agotado")
		}
		return "", errors.New("prompt inesperado: " + label)
	}
	r := s.replies[s.i]
	s.i++
	return r, nil
}

func (s *scriptedPrompt) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asked)
}

const goodPass = "maestra-correcta-1"

func TestFirstRunCreatesVaultAndCaches(t *testing.T) {
	meta := newFakeMeta()
	p := newPrompt(goodPass, goodPass)
	m := New(meta, DefaultTTL, p.ask)

	v1, err := m.Ensure()
	if err != nil {
		t.Fatalf("Ensure primer inicio: %v", err)
	}
	if !crypto.HasVault(meta) {
		t.Fatal("la bóveda debería haberse creado")
	}
	env, err := v1.Seal("dato")
	if err != nil {
		t.Fatal(err)
	}

	// Segunda Ensure: sesión cacheada, sin prompts extra.
	v2, err := m.Ensure()
	if err != nil {
		t.Fatalf("Ensure cacheada: %v", err)
	}
	if v1 != v2 {
		t.Fatal("debería devolver la misma bóveda de la caché")
	}
	if got, _ := v2.Unseal(env); got != "dato" {
		t.Errorf("roundtrip tras caché: %q", got)
	}
	if n := p.count(); n != 2 {
		t.Errorf("primer inicio = 2 prompts; hay %d", n)
	}
	if !m.Alive() || m.ExpiresAt().IsZero() {
		t.Error("sesión debería estar viva con expiry definido")
	}
}

func TestFirstRunValidation(t *testing.T) {
	cases := []struct {
		name    string
		replies []string
		wantErr error
	}{
		{"confirmación no coincide", []string{"clave-uno-x1", "clave-dos-x2"}, ErrPasswordsDontMatch},
		{"contraseña vacía", []string{"", ""}, ErrEmptyPassword},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := newFakeMeta()
			p := newPrompt(tc.replies...)
			m := New(meta, DefaultTTL, p.ask)

			_, err := m.Ensure()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("esperaba %v, got %v", tc.wantErr, err)
			}
			if crypto.HasVault(meta) {
				t.Error("no debe crearse bóveda si la validación falla")
			}
		})
	}
}

func TestUnlockWrongPasswordRetries(t *testing.T) {
	meta := newFakeMeta()
	if _, err := crypto.CreateVault(meta, goodPass); err != nil {
		t.Fatal(err)
	}

	p := newPrompt("mala-1", "mala-2", "mala-3")
	m := New(meta, DefaultTTL, p.ask)

	if _, err := m.Ensure(); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("esperaba ErrAuthFailed, got %v", err)
	}
	if n := p.count(); n != MaxUnlockAttempts {
		t.Errorf("esperaba %d intentos, hubo %d", MaxUnlockAttempts, n)
	}
	if m.Alive() {
		t.Error("no debe quedar sesión viva tras agotar intentos")
	}

	// Reintento posterior con clave correcta abre.
	p2 := newPrompt(goodPass)
	m.prompt = p2.ask
	if _, err := m.Ensure(); err != nil {
		t.Fatalf("reintento correcto falló: %v", err)
	}
}

func TestAutoLockAfterTTL(t *testing.T) {
	meta := newFakeMeta()
	p := newPrompt(goodPass, goodPass) // creación (2 prompts)
	m := New(meta, 40*time.Millisecond, p.ask)

	if _, err := m.Ensure(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(90 * time.Millisecond)

	if m.Alive() {
		t.Fatal("la sesión debió expirar por TTL")
	}
	select {
	case <-m.LockEvents():
	default:
		t.Error("debió emitirse evento de bloqueo")
	}

	// Tras expirar, Ensure vuelve a pedir contraseña.
	p2 := newPrompt(goodPass)
	m.prompt = p2.ask
	if _, err := m.Ensure(); err != nil {
		t.Fatalf("Ensure tras expiración: %v", err)
	}
	if n := p2.count(); n != 1 {
		t.Errorf("expirado debe pedir contraseña una vez; pidió %d", n)
	}
}

func TestTouchExtendsSession(t *testing.T) {
	meta := newFakeMeta()
	p := newPrompt(goodPass, goodPass) // creación (2 prompts)
	m := New(meta, 80*time.Millisecond, p.ask)

	if _, err := m.Ensure(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.Touch() // actividad continua mantiene viva la sesión
		time.Sleep(20 * time.Millisecond)
	}
	if !m.Alive() {
		t.Fatal("con Touch continuo la sesión no debe morir")
	}
}

func TestChangeMasterPasswordFlow(t *testing.T) {
	meta := newFakeMeta()
	setup := newPrompt(goodPass, goodPass)
	m := New(meta, DefaultTTL, setup.ask)

	v, err := m.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	env, _ := v.Seal("persiste-tras-cambio")

	change := newPrompt(goodPass, "nueva-clave-larga", "nueva-clave-larga")
	m.prompt = change.ask
	if err := m.ChangeMasterPassword(); err != nil {
		t.Fatalf("ChangeMasterPassword: %v", err)
	}

	// Confirmaciones incorrectas abortan sin tocar nada (3 prompts: actual, nueva, confirmación).
	bad := newPrompt("nueva-clave-larga", "otra-distinta-x", "tercera-diferente")
	m.prompt = bad.ask
	if err := m.ChangeMasterPassword(); !errors.Is(err, ErrPasswordsDontMatch) {
		t.Fatalf("confirmación distinta debería fallar, got %v", err)
	}

	// La nueva abre; la vieja ya no; el dato sigue descifrándose.
	m.Lock()
	reopen := newPrompt("nueva-clave-larga")
	m.prompt = reopen.ask
	v2, err := m.Ensure()
	if err != nil {
		t.Fatalf("abrir con clave nueva: %v", err)
	}
	if got, err := v2.Unseal(env); err != nil || got != "persiste-tras-cambio" {
		t.Fatalf("dato tras cambio: %q, %v", got, err)
	}

	// Con la clave vieja agota los 3 intentos y no abre.
	oldTry := newPrompt(goodPass, goodPass, goodPass)
	m.prompt = oldTry.ask
	m.Lock()
	if _, err := m.Ensure(); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("clave vieja no debe abrir, got %v", err)
	}
}

func TestManualLockEmitsEventOnce(t *testing.T) {
	meta := newFakeMeta()
	// 2 respuestas para el flujo de creación + 1 para el re-unlock.
	p := newPrompt(goodPass, goodPass, goodPass)
	m := New(meta, DefaultTTL, p.ask)

	if _, err := m.Ensure(); err != nil {
		t.Fatal(err)
	}
	m.Lock()
	select {
	case <-m.LockEvents():
	default:
		t.Error("Lock manual debería emitir evento")
	}
	// Drenar y comprobar que LockEvents sigue utilizable tras re-unlock.
	for len(m.LockEvents()) > 0 {
		<-m.LockEvents()
	}
	if _, err := m.Ensure(); err != nil {
		t.Fatalf("re-unlock tras Lock: %v", err)
	}
}

func TestProgrammaticLifecycle(t *testing.T) {
	meta := newFakeMeta()
	m := New(meta, DefaultTTL, nil) // sin prompt: solo API programática

	// Sin prompt, los flujos interactivos no deben colgar ni paniquear.
	if _, err := m.Ensure(); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("Ensure sin prompt: esperaba ErrNoPrompt, got %v", err)
	}

	if err := m.CreateWith("corta"); err == nil {
		t.Fatal("CreateWith debería rechazar contraseña corta")
	}
	if err := m.CreateWith(goodPass); err != nil {
		t.Fatalf("CreateWith: %v", err)
	}
	if err := m.CreateWith("otra-clave-larga"); !errors.Is(err, crypto.ErrVaultExists) {
		t.Fatalf("segunda CreateWith: %v", err)
	}
	if !m.Alive() || !m.HasVault() {
		t.Fatal("tras crear debe haber sesión viva y bóveda existente")
	}
	if m.Remaining() <= 0 || m.Remaining() > DefaultTTL {
		t.Errorf("Remaining fuera de rango: %v", m.Remaining())
	}

	m.Lock()
	if _, err := m.Ensure(); !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("Ensure tras Lock sin prompt: %v", err)
	}
	if err := m.UnlockWith("incorrecta"); !errors.Is(err, crypto.ErrWrongPassword) {
		t.Fatalf("UnlockWith incorrecta: %v", err)
	}
	if err := m.UnlockWith(goodPass); err != nil {
		t.Fatalf("UnlockWith correcta: %v", err)
	}
	if !m.Alive() {
		t.Fatal("debe quedar sesión viva tras UnlockWith")
	}
}

// fakeAuth registra las contraseñas recibidas y devuelve un error fijo.
type fakeAuth struct {
	got []string
	err error
}

func (f *fakeAuth) Authenticate(pw string) error {
	f.got = append(f.got, pw)
	return f.err
}

func TestChangeMasterPasswordRequiresOSAuth(t *testing.T) {
	meta := newFakeMeta()
	setup := newPrompt(goodPass, goodPass)
	m := New(meta, DefaultTTL, setup.ask)
	if _, err := m.Ensure(); err != nil {
		t.Fatal(err)
	}

	authErr := errors.New("pam: fallo simulado")
	fa := &fakeAuth{err: authErr}
	m.WithAuthorizer(fa)

	// El primer prompt es la contraseña del sistema; el autorizador falla.
	m.prompt = newPrompt("clave-linux-mala").ask
	err := m.ChangeMasterPassword()
	if !errors.Is(err, authErr) {
		t.Fatalf("debería propagarse el error del SO, got %v", err)
	}
	if len(fa.got) != 1 || fa.got[0] != "clave-linux-mala" {
		t.Errorf("el autorizador debió recibir la contraseña escrita; got %v", fa.got)
	}

	// La bóveda no debe haber cambiado: la clave vieja sigue abriendo.
	if _, err := crypto.OpenVault(meta, goodPass); err != nil {
		t.Fatalf("la clave vieja debería seguir válida tras aborto: %v", err)
	}
}

func TestChangeMasterPasswordWithOSAuthSuccess(t *testing.T) {
	meta := newFakeMeta()
	setup := newPrompt(goodPass, goodPass)
	m := New(meta, DefaultTTL, setup.ask)
	if _, err := m.Ensure(); err != nil {
		t.Fatal(err)
	}
	defer m.Lock()

	fa := &fakeAuth{}
	m.WithAuthorizer(fa)

	// Orden esperado: sistema → actual → nueva → confirmación.
	m.prompt = newPrompt("clave-linux-ok", goodPass, "nueva-clave-larga", "nueva-clave-larga").ask
	if err := m.ChangeMasterPassword(); err != nil {
		t.Fatalf("cambio completo: %v", err)
	}
	if len(fa.got) != 1 || fa.got[0] != "clave-linux-ok" {
		t.Errorf("autorizador llamado %v", fa.got)
	}
	if _, err := crypto.OpenVault(meta, "nueva-clave-larga"); err != nil {
		t.Fatalf("abrir con nueva clave: %v", err)
	}
}
