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
