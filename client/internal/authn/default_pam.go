//go:build pam

package authn

import (
	"fmt"

	"github.com/msteinert/pam/v2"
)

// Default devuelve el autenticador disponible según la compilación.
// Con `-tags pam`: libpam directa contra el servicio "sudo".
func Default() Authenticator { return NewPAM("sudo") }

type pamAuthenticator struct {
	service string
}

// NewPAM crea un autenticador PAM contra el servicio dado
// (debe existir en /etc/pam.d/).
func NewPAM(service string) Authenticator {
	return pamAuthenticator{service: service}
}

func (a pamAuthenticator) Authenticate(password string) error {
	tx, err := pam.StartFunc(a.service, Username(), pam.ConversationFunc(
		func(style pam.Style, msg string) (string, error) {
			if style == pam.PromptEchoOff {
				return password, nil
			}
			return "", fmt.Errorf("authn: estilo PAM %v no soportado (%s)", style, msg)
		}))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer tx.End()

	if err := tx.Authenticate(0); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthFailed, err)
	}
	return nil
}
