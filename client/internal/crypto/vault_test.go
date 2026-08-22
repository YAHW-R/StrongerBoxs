package crypto

import (
	"errors"
	"strings"
	"testing"
)

type fakeMeta struct{ data map[string]string }

func newFakeMeta() *fakeMeta { return &fakeMeta{data: map[string]string{}} }

func (f *fakeMeta) GetMeta(key string) (string, bool, error) {
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeMeta) SetMeta(items map[string]string) error {
	for k, v := range items {
		f.data[k] = v
	}
	return nil
}

func TestVaultLifecycle(t *testing.T) {
	meta := newFakeMeta()

	if HasVault(meta) {
		t.Fatal("no debería haber bóveda aún")
	}
	if _, err := CreateVault(meta, "corta"); err == nil {
		t.Fatal("debería rechazar contraseña corta")
	}

	v, err := CreateVault(meta, "maestra-correcta-1")
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if !HasVault(meta) {
		t.Fatal("la bóveda debería existir")
	}
	if _, err := CreateVault(meta, "otra-clave-larga"); !errors.Is(err, ErrVaultExists) {
		t.Fatalf("segunda creación debería fallar con ErrVaultExists, got %v", err)
	}

	// Roundtrip de campos.
	secreto := "clave-super-secreta-ñ 🔐"
	env, err := v.Seal(secreto)
	if err != nil || !strings.HasPrefix(env, envPrefix) {
		t.Fatalf("Seal: %v / %q", err, env)
	}
	got, err := v.Unseal(env)
	if err != nil || got != secreto {
		t.Fatalf("Unseal: %v / %q", err, got)
	}

	// Nonces únicos: dos sellos difieren.
	env2, _ := v.Seal(secreto)
	if env2 == env {
		t.Fatal("nonce repetido: los dos sellos son idénticos")
	}

	// Cadenas vacías pasan sin cifrar.
	if e, _ := v.Seal(""); e != "" {
		t.Error("Seal(\"\") debe devolver \"\"")
	}
	if s, err := v.Unseal(""); err != nil || s != "" {
		t.Errorf("Unseal(\"\") = %q, %v", s, err)
	}

	// Tampering: alterar un byte del sobre → ErrDecryptFailed.
	bad := []byte(env)
	bad[len(bad)-1] ^= 0xFF
	if _, err := v.Unseal(string(bad)); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("tampering debería dar ErrDecryptFailed, got %v", err)
	}

	// Lock: bloquea operaciones.
	v.Lock()
	if !v.Locked() {
		t.Fatal("debería estar bloqueada")
	}
	if _, err := v.Seal("x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Seal bloqueada: %v", err)
	}
}

func TestOpenVaultWrongPassword(t *testing.T) {
	meta := newFakeMeta()
	if _, err := CreateVault(meta, "contraseña-original"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(meta, "incorrecta"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("esperaba ErrWrongPassword, got %v", err)
	}
	v, err := OpenVault(meta, "contraseña-original")
	if err != nil {
		t.Fatalf("apertura correcta falló: %v", err)
	}
	defer v.Lock()
}

func TestChangePassword(t *testing.T) {
	meta := newFakeMeta()
	v1, err := CreateVault(meta, "vieja-maestra-99")
	if err != nil {
		t.Fatal(err)
	}

	// Dato sellado antes del cambio de contraseña.
	env, err := v1.Seal("persiste-tras-cambio")
	if err != nil {
		t.Fatal(err)
	}

	if err := v1.ChangePassword("equivocada", "nueva-maestra-00"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("cambio con clave actual errónea: %v", err)
	}
	if err := v1.ChangePassword("corta", "nueva-maestra-00"); err == nil {
		t.Fatal("debería rechazar contraseña nueva corta")
	}
	if err := v1.ChangePassword("vieja-maestra-99", "nueva-maestra-00"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// La vieja ya no abre; la nueva sí.
	if _, err := OpenVault(meta, "vieja-maestra-99"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("clave vieja debería fallar, got %v", err)
	}
	v2, err := OpenVault(meta, "nueva-maestra-00")
	if err != nil {
		t.Fatalf("clave nueva debería abrir: %v", err)
	}
	defer v2.Lock()

	// El dato sellado con la DEK intacta sigue descifrándose.
	got, err := v2.Unseal(env)
	if err != nil || got != "persiste-tras-cambio" {
		t.Fatalf("dato tras cambio de contraseña: %q, %v", got, err)
	}
}
