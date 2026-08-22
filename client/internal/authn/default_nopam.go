//go:build !pam

package authn

// Default devuelve el autenticador disponible según la compilación.
// Sin CGO: sudo (que a su vez aplica PAM).
func Default() Authenticator { return NewSudo() }
