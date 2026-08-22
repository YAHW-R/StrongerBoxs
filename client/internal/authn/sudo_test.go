package authn

import (
	"errors"
	"strings"
	"testing"
)

func TestSudoAuthenticateSuccess(t *testing.T) {
	var gotArgs []string
	var gotStdin string
	a := &SudoAuthenticator{run: func(name, stdin string, args ...string) (int, string) {
		if name != "sudo" {
			t.Errorf("comando = %q, quiero sudo", name)
		}
		gotArgs, gotStdin = args, stdin
		return 0, ""
	}}

	if err := a.Authenticate("mi-clave-linux"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	want := []string{"-k", "-S", "-p", "", "-v"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, quiero %v", gotArgs, want)
	}
	if gotStdin != "mi-clave-linux\n" {
		t.Errorf("stdin = %q, quiero contraseña + salto de línea", gotStdin)
	}
}

func TestSudoAuthenticateWrongPassword(t *testing.T) {
	a := &SudoAuthenticator{run: func(name, stdin string, args ...string) (int, string) {
		return 1, "sudo: 1 incorrect password attempt"
	}}
	err := a.Authenticate("mala")
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("esperaba ErrAuthFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "incorrect password attempt") {
		t.Errorf("el error debería incluir la salida de sudo: %v", err)
	}
}

func TestSudoUnavailable(t *testing.T) {
	a := &SudoAuthenticator{run: func(name, stdin string, args ...string) (int, string) {
		return -1, "exec: \"sudo\": executable file not found in $PATH"
	}}
	err := a.Authenticate("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("esperaba ErrUnavailable, got %v", err)
	}
}

func TestSystemRunRealCommand(t *testing.T) {
	code, out := systemRun("true", "")
	if code != 0 || out != "" {
		t.Fatalf("`true` debería funcionar: code=%d out=%q", code, out)
	}
}
