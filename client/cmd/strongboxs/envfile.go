package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// envFilePath devuelve el archivo de entorno por defecto:
// $STRONGBOXS_ENV_FILE, o ~/.config/strongboxs/sync.env.
func envFilePath() string {
	if p := os.Getenv("STRONGBOXS_ENV_FILE"); p != "" {
		return p
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "strongboxs", "sync.env")
	}
	return ""
}

// loadEnvFile carga pares KEY=VALUE (con # comentarios y comillas
// opcionales) SIN sobreescribir variables ya exportadas en la shell:
// el entorno real siempre gana sobre el archivo.
// Un archivo inexistente no es error: simplemente no hay config extra.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

var syncEnvKeys = []string{
	"STRONGBOXS_SYNC_URL",
	"STRONGBOXS_SYNC_USER",
	"STRONGBOXS_SYNC_PASSWORD",
}

// SaveSyncEnv escribe/actualiza las claves de sincronización en el archivo
// de entorno preservando el resto de líneas y comentarios. Escritura
// atómica (tmp+rename) con permisos 0600.
func SaveSyncEnv(path string, url, user, pass string) error {
	values := map[string]string{
		"STRONGBOXS_SYNC_URL":      url,
		"STRONGBOXS_SYNC_USER":     user,
		"STRONGBOXS_SYNC_PASSWORD": pass,
	}

	var out []string
	done := map[string]bool{}

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			replaced := false
			for _, k := range syncEnvKeys {
				if trimmed == k || strings.HasPrefix(trimmed, k+"=") ||
					strings.HasPrefix(trimmed, k+" =") {
					out = append(out, k+"="+values[k])
					done[k] = true
					replaced = true
					break
				}
			}
			if !replaced && line != "" {
				out = append(out, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, k := range syncEnvKeys {
		if !done[k] {
			out = append(out, k+"="+values[k])
		}
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
