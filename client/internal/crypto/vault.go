// Package crypto implementa la bóveda local de Strongboxs.
//
// Jerarquía de claves (patrón envelope):
//
//	contraseña maestra --Argon2id--> KEK (key encryption key)
//	DEK aleatoria de 256 bits --AES-256-GCM con KEK--> DEK envuelta
//	campos (títulos, cuerpos, contraseñas) --AES-256-GCM con DEK--> BLOBs
//
// Al cambiar la contraseña maestra solo se re-envuelve la DEK:
// no hay que recifrar ninguna fila.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

var (
	ErrWrongPassword = errors.New("crypto: contraseña maestra incorrecta")
	ErrLocked        = errors.New("crypto: la bóveda está bloqueada")
	ErrVaultExists   = errors.New("crypto: la bóveda ya existe")
	ErrDecryptFailed = errors.New("crypto: no se pudo descifrar el dato")
)

const (
	minPasswordLen  = 8
	saltLen         = 16
	keyLen          = 32 // AES-256
	nonceLen        = 12 // GCM estándar
	envelopeVersion = byte(1)
	envPrefix       = "sb1."
)

const (
	metaKeyParams    = "kdf.params"
	metaKeySalt      = "kdf.salt"
	metaKeyWrappedDE = "kdf.wrapped_dek"
)

// MetaStore es el KV persistente donde la bóveda guarda sus metadatos
// (implementado por store.Store sobre la tabla vault_meta).
type MetaStore interface {
	GetMeta(key string) (val string, ok bool, err error)
	SetMeta(items map[string]string) error
}

// KDFParams se persisten para poder endurecerlos en el futuro sin romper bóvedas antiguas.
type KDFParams struct {
	Algorithm string `json:"alg"`
	Time      uint32 `json:"t"`
	MemoryKiB uint32 `json:"m"`
	Threads   uint8  `json:"p"`
}

func defaultKDFParams() KDFParams {
	// OWASP: Argon2id m>=19MiB; elegimos 64 MiB / t=1 / p=4 para uso interactivo.
	return KDFParams{Algorithm: "argon2id", Time: 1, MemoryKiB: 64 * 1024, Threads: 4}
}

// Vault es una bóveda desbloqueada. El cero-value no es utilizable.
type Vault struct {
	meta   MetaStore
	params KDFParams
	salt   []byte
	dek    []byte // nil == bloqueada
}

func deriveKEK(password string, salt []byte, p KDFParams) []byte {
	return argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, keyLen)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	return gcm, nil
}

// sealWith cifra plaintext: [version][nonce][ciphertext+tag].
func sealWith(aead cipher.AEAD, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	out := make([]byte, 0, 1+nonceLen+len(plaintext)+aead.Overhead())
	out = append(out, envelopeVersion)
	out = append(out, nonce...)
	return aead.Seal(out[:1+nonceLen], nonce, plaintext, nil), nil
}

// openWith descifra un sobre generado por sealWith.
func openWith(aead cipher.AEAD, envelope []byte) ([]byte, error) {
	if len(envelope) < 1+nonceLen+aead.Overhead() || envelope[0] != envelopeVersion {
		return nil, ErrDecryptFailed
	}
	plaintext, err := aead.Open(nil, envelope[1:1+nonceLen], envelope[1+nonceLen:], nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// CreateVault inicializa una bóveda nueva con la contraseña maestra dada.
func CreateVault(meta MetaStore, masterPassword string) (*Vault, error) {
	if len(masterPassword) < minPasswordLen {
		return nil, fmt.Errorf("crypto: la contraseña debe tener al menos %d caracteres", minPasswordLen)
	}
	if _, ok, _ := meta.GetMeta(metaKeySalt); ok {
		return nil, ErrVaultExists
	}

	params := defaultKDFParams()
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("crypto: salt: %w", err)
	}
	dek := make([]byte, keyLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("crypto: dek: %w", err)
	}

	kek := deriveKEK(masterPassword, salt, params)
	aead, err := newAEAD(kek)
	if err != nil {
		return nil, err
	}
	wrapped, err := sealWith(aead, dek)
	if err != nil {
		return nil, err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	b64 := base64.RawURLEncoding
	err = meta.SetMeta(map[string]string{
		metaKeyParams:    string(paramsJSON),
		metaKeySalt:      b64.EncodeToString(salt),
		metaKeyWrappedDE: b64.EncodeToString(wrapped),
	})
	if err != nil {
		return nil, err
	}
	return &Vault{meta: meta, params: params, salt: salt, dek: dek}, nil
}

// OpenVault verifica la contraseña maestra y devuelve la bóveda desbloqueada.
func OpenVault(meta MetaStore, masterPassword string) (*Vault, error) {
	paramsJSON, okP, err := meta.GetMeta(metaKeyParams)
	saltB64, okS, err2 := meta.GetMeta(metaKeySalt)
	wrapB64, okW, err3 := meta.GetMeta(metaKeyWrappedDE)
	for _, e := range []error{err, err2, err3} {
		if e != nil {
			return nil, e
		}
	}
	if !okP || !okS || !okW {
		return nil, errors.New("crypto: bóveda incompleta o corrupta")
	}

	var params KDFParams
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return nil, fmt.Errorf("crypto: params kdf: %w", err)
	}
	b64 := base64.RawURLEncoding
	salt, err := b64.DecodeString(saltB64)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	wrapped, err := b64.DecodeString(wrapB64)
	if err != nil {
		return nil, ErrDecryptFailed
	}

	aead, err := newAEAD(deriveKEK(masterPassword, salt, params))
	if err != nil {
		return nil, err
	}
	dek, err := openWith(aead, wrapped)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return &Vault{meta: meta, params: params, salt: salt, dek: dek}, nil
}

// HasVault informa si ya existe una bóveda en el almacén.
func HasVault(meta MetaStore) bool {
	_, ok, _ := meta.GetMeta(metaKeySalt)
	return ok
}

// Locked indica si la DEK sigue en memoria.
func (v *Vault) Locked() bool { return v == nil || len(v.dek) == 0 }

// Lock borra la DEK de memoria (best-effort; Go no garantiza wipe).
func (v *Vault) Lock() {
	for i := range v.dek {
		v.dek[i] = 0
	}
	v.dek = nil
}

func (v *Vault) aead() (cipher.AEAD, error) {
	if v.Locked() {
		return nil, ErrLocked
	}
	return newAEAD(v.dek)
}

// Seal cifra un campo a sobre "sb1.<base64>". Cadena vacía → cadena vacía.
func (v *Vault) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	aead, err := v.aead()
	if err != nil {
		return "", err
	}
	env, err := sealWith(aead, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return envPrefix + base64.RawURLEncoding.EncodeToString(env), nil
}

// Unseal descifra un campo producido por Seal.
func (v *Vault) Unseal(envelope string) (string, error) {
	if envelope == "" {
		return "", nil
	}
	if len(envelope) < len(envPrefix) || envelope[:len(envPrefix)] != envPrefix {
		return "", ErrDecryptFailed
	}
	env, err := base64.RawURLEncoding.DecodeString(envelope[len(envPrefix):])
	if err != nil {
		return "", ErrDecryptFailed
	}
	aead, err := v.aead()
	if err != nil {
		return "", err
	}
	pt, err := openWith(aead, env)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ChangePassword re-envuelve la misma DEK con una nueva contraseña.
// Los datos cifrados no se tocan.
func (v *Vault) ChangePassword(current, next string) error {
	if v.Locked() {
		return ErrLocked
	}
	if len(next) < minPasswordLen {
		return fmt.Errorf("crypto: la contraseña debe tener al menos %d caracteres", minPasswordLen)
	}

	// Verificación explícita contra lo almacenado (no confiamos en el estado interno).
	wrapB64, ok, err := v.meta.GetMeta(metaKeyWrappedDE)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("crypto: bóveda incompleta")
	}
	wrapped, err := base64.RawURLEncoding.DecodeString(wrapB64)
	if err != nil {
		return ErrDecryptFailed
	}
	curAead, err := newAEAD(deriveKEK(current, v.salt, v.params))
	if err != nil {
		return err
	}
	dekCheck, err := openWith(curAead, wrapped)
	if err != nil {
		return ErrWrongPassword
	}
	if string(dekCheck) != string(v.dek) {
		return ErrWrongPassword
	}

	newSalt := make([]byte, saltLen)
	if _, err := rand.Read(newSalt); err != nil {
		return fmt.Errorf("crypto: salt: %w", err)
	}
	newAead, err := newAEAD(deriveKEK(next, newSalt, v.params))
	if err != nil {
		return err
	}
	newWrapped, err := sealWith(newAead, v.dek)
	if err != nil {
		return err
	}
	b64 := base64.RawURLEncoding
	if err := v.meta.SetMeta(map[string]string{
		metaKeySalt:      b64.EncodeToString(newSalt),
		metaKeyWrappedDE: b64.EncodeToString(newWrapped),
	}); err != nil {
		return err
	}
	v.salt = newSalt
	return nil
}
