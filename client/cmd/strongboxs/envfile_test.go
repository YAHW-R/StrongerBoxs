package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sync.env")
	content := "# comentario\n" +
		"STRONGBOXS_SYNC_URL=http://localhost:8000\n" +
		"STRONGBOXS_SYNC_USER = 'TuUsuario'\n" +
		`STRONGBOXS_SYNC_PASSWORD="pass-123"` + "\n" +
		"LÍNEA_MALA_SIN_IGUAL\n" +
		"EXISTENTE=debe-mantenerse\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXISTENTE", "original")
	t.Cleanup(func() {
		for _, k := range []string{"STRONGBOXS_SYNC_URL", "STRONGBOXS_SYNC_USER", "STRONGBOXS_SYNC_PASSWORD"} {
			os.Unsetenv(k)
		}
	})

	if err := loadEnvFile(p); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("STRONGBOXS_SYNC_URL"); got != "http://localhost:8000" {
		t.Errorf("URL = %q", got)
	}
	if got := os.Getenv("STRONGBOXS_SYNC_USER"); got != "TuUsuario" {
		t.Errorf("USER = %q (las comillas deben quitarse)", got)
	}
	if got := os.Getenv("STRONGBOXS_SYNC_PASSWORD"); got != "pass-123" {
		t.Errorf("PASSWORD = %q", got)
	}
	if got := os.Getenv("EXISTENTE"); got != "original" {
		t.Errorf("el entorno real debe ganar: %q", got)
	}
}

func TestLoadEnvFileMissingIsNoop(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "no-existe.env")); err != nil {
		t.Fatalf("archivo ausente no debería ser error: %v", err)
	}
}

func TestEnvFilePathDefaultUnderConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.Unsetenv("STRONGBOXS_ENV_FILE")
	want := filepath.Join(dir, "strongboxs", "sync.env")
	if got := envFilePath(); got != want {
		t.Errorf("path = %q, quiero %q", got, want)
	}
}

func TestSaveSyncEnvCreatesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sync.env")

	// Creación desde cero.
	if err := SaveSyncEnv(p, "http://localhost:8000", "Pepe", "clave-123"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	txt := string(data)
	for _, want := range []string{
		"STRONGBOXS_SYNC_URL=http://localhost:8000",
		"STRONGBOXS_SYNC_USER=Pepe",
		"STRONGBOXS_SYNC_PASSWORD=clave-123",
	} {
		if !strings.Contains(txt, want+"\n") && !strings.HasSuffix(txt, want) {
			t.Errorf("falta %q en:\\n%s", want, txt)
		}
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("permisos = %v", fi.Mode())
	}

	// Actualización preservando líneas ajenas y comentarios.
	os.WriteFile(p, []byte("# mi config\nOTRA=1\nSTRONGBOXS_SYNC_USER=viejo\n"), 0o600)
	if err := SaveSyncEnv(p, "http://nuevo", "otro", "pass-9"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	txt = string(data)
	if !strings.Contains(txt, "# mi config") || !strings.Contains(txt, "OTRA=1") {
		t.Error("debió preservar líneas ajenas")
	}
	if strings.Contains(txt, "viejo") {
		t.Error("usuario viejo debió actualizarse")
	}
	if !strings.Contains(txt, "STRONGBOXS_SYNC_USER=otro") ||
		!strings.Contains(txt, "STRONGBOXS_SYNC_URL=http://nuevo") {
		t.Errorf("actualización incompleta:\\n%s", txt)
	}

	// El loader debe leer exactamente lo guardado.
	t.Setenv("EXISTENTE_X", "x")
	if err := loadEnvFile(p); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("STRONGBOXS_SYNC_URL")
	defer os.Unsetenv("STRONGBOXS_SYNC_USER")
	defer os.Unsetenv("STRONGBOXS_SYNC_PASSWORD")
	if got := os.Getenv("STRONGBOXS_SYNC_URL"); got != "http://nuevo" {
		t.Errorf("roundtrip loader=%q", got)
	}
	os.Unsetenv("EXISTENTE_X")
}
