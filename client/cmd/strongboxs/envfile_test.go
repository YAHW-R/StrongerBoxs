package main

import (
	"os"
	"path/filepath"
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
