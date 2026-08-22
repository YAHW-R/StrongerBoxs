// Package authn valida la contraseña del usuario de Linux para autorizar
// operaciones sensibles (cambio de contraseña maestra), como hace sudo.
//
// Dos mecanismos:
//   - build por defecto (sin CGO): delega en `sudo -k -S -v`, que aplica
//     la política PAM de sudo. Requiere que el usuario tenga privilegios sudo.
//   - build con `-tags pam`: usa libpam directamente vía msteinert/pam/v2
//     (requiere gcc y libpam-dev). Servicio "sudo" por defecto.
package authn

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

var (
	ErrAuthFailed  = errors.New("authn: contraseña del sistema incorrecta")
	ErrUnavailable = errors.New("authn: autenticador no disponible")
)

// Authenticator valida un secreto contra el mecanismo del sistema.
type Authenticator interface {
	Authenticate(password string) error
}

// Username devuelve el usuario Linux actual (para mensajes).
func Username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// runnerFunc ejecuta un comando inyectando stdin; devuelto: exit code y salida.
type runnerFunc func(name, stdin string, args ...string) (int, string)

var systemRun runnerFunc = func(name, stdin string, args ...string) (int, string) {
	if _, err := exec.LookPath(name); err != nil {
		return -1, err.Error()
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), string(out)
		}
		return -1, string(out)
	}
	return 0, string(out)
}

// SudoAuthenticator valida vía `sudo` (política PAM de sudo).
type SudoAuthenticator struct {
	run runnerFunc
}

func NewSudo() *SudoAuthenticator { return &SudoAuthenticator{run: systemRun} }

func (a *SudoAuthenticator) Authenticate(password string) error {
	code, out := a.run("sudo", password+"\n", "-k", "-S", "-p", "", "-v")
	switch {
	case code < 0:
		return fmt.Errorf("%w: %s", ErrUnavailable, strings.TrimSpace(out))
	case code != 0:
		return fmt.Errorf("%w: %s", ErrAuthFailed, strings.TrimSpace(out))
	default:
		return nil
	}
}
