# Strongboxs · Cliente TUI

Cliente local de Strongboxs: notas estilo Google Keep y bóveda de contraseñas,
con cifrado AES-256-GCM, sesión estilo *sudo* e interfaz de terminal
(BubbleTea) con modos y comandos al estilo Neovim.

---

## 1. Requisitos

| Requisito | Notas |
|---|---|
| Go ≥ 1.26 | compilación sin CGO (binario estático) |
| Linux | probado en bash/zsh; terminal con soporte truecolor recomendado |
| `xclip`/`xsel` (X11) o `wl-clipboard` (Wayland) | solo para copiar contraseñas (`y`) |
| `gcc` + `libpam-dev` | **opcional**, solo si compilas con `-tags pam` |

## 2. Compilación e instalación

### Rápida (desarrollo)

```bash
cd client

# dependencias
go mod download

# binario por defecto (sin CGO; valida contra el SO vía sudo)
go build -o strongboxs ./cmd/strongboxs

# variante PAM nativo (requiere gcc + libpam-dev)
go build -tags pam -o strongboxs-pam ./cmd/strongboxs

# ejecutar
./strongboxs            # o: go run ./cmd/strongboxs
```

Subcomando adicional:

```bash
./strongboxs passwd     # cambia la contraseña maestra (validando contra Linux)
```

### Instalación en el sistema (recomendada)

Desde la **raíz del repo**:

```bash
sudo make install      # binario en /usr/local/bin + lanzador en el menú de apps
make uninstall         # desinstalar
```

Tras esto, `strongboxs` funciona desde cualquier terminal y aparece en el
menú de aplicaciones (se abre en tu terminal por defecto).

## 3. Variables de entorno (config persistente)

No hace falta exportar nada en cada terminal: la app lee automáticamente
`~/.config/strongboxs/sync.env` (o la ruta que apunte `STRONGBOXS_ENV_FILE`).
Las variables ya exportadas en la shell tienen **prioridad** sobre el archivo.

```bash
mkdir -p ~/.config/strongboxs
cat > ~/.config/strongboxs/sync.env <<'EOF'
# Sincronización (opcional; sin este bloque la app es 100% local)
STRONGBOXS_SYNC_URL=http://localhost:8000
STRONGBOXS_SYNC_USER=tu_usuario        # minúsculas/números/. _ -  (3-64)
STRONGBOXS_SYNC_PASSWORD=passDeCuenta
STRONGBOXS_SYNC_INTERVAL_SECS=60       # opcional
STRONGBOXS_SYNC_DEBUG=1                # opcional: log de ciclos por stdout
EOF
chmod 600 ~/.config/strongboxs/sync.env   # contiene una contraseña
```

| Variable | Significado |
|---|---|
| `STRONGBOXS_SYNC_URL` | base de la API; si se omite, modo 100% local |
| `STRONGBOXS_SYNC_USER` | cuenta de sync (**se normaliza a minúsculas**) |
| `STRONGBOXS_SYNC_PASSWORD` | contraseña de cuenta (no viaja; solo su derivado Argon2id+SHA256) |
| `STRONGBOXS_SYNC_INTERVAL_SECS` | segundos entre ciclos (60 por defecto) |
| `STRONGBOXS_SYNC_DEBUG` | `1` = log de ciclos en stdout |
| `STRONGBOXS_ENV_FILE` | ruta alternativa del archivo de entorno |

La **contraseña maestra** (descifra tu bóveda local) y la **contraseña de
cuenta** (autentica contra el servidor) son credenciales distintas.

## Sincronización en segundo plano (opcional)

El cliente sincroniza solo, sin tocar la TUI: cada ciclo hace
pull→merge→push resolviendo conflictos **por fecha** (gana el `updated_at`
más nuevo). Funciona incluso con la bóveda bloqueada: viaja ciphertext puro.
Sin internet, los cambios quedan pendientes (`dirty`) y suben al reconectar.

## Servidor Docker (API + PostgreSQL)

Desde la raíz del repo:

```bash
make server-up      # docker compose up -d --build  (API :8000)
make server-logs    # seguir logs de la API
make server-down    # parar la pila
cp .env.example .env && $EDITOR .env   # STRONGBOXS_SECRET_KEY en producción
```

**Arranque automático al boot (opcional)** con systemd:

```bash
sudo cp -r . /opt/strongboxs                     # o clona ahí el repo
sudo cp packaging/strongboxs-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now strongboxs-server
systemctl status strongboxs-server               # comprobar
journalctl -u strongboxs-server -f               # logs
```

## 3. Primer uso

1. Al arrancar sin bóveda aparece **🔑 Primera ejecución**:
   escribe una contraseña maestra (mínimo 8 caracteres) y confírmala.
2. Se crea la BD local con permisos restrictivos:

   ```
   $XDG_DATA_HOME/strongboxs/strongboxs.db      # o ~/.local/share/…
   ```

3. En siguientes arranques aparece la **🔒 lock-screen**: pide la maestra
   (el campo se enmascara; un fallo limpia el input y muestra el error).
4. La sesión se **auto-bloquea tras 15 min** de inactividad (TTL renovado
   por cada operación). Al bloquearse, todo texto en claro sale de memoria.

## 4. Uso diario

### Modos

```
NORMAL ── ':' ──▶ COMANDOS (ex) ── enter ──▶ ejecutar
   │
   ├── '/'  ──▶ BÚSQUEDA incremental (filtrado en vivo)
   └── enter/e/:e ──▶ EDITOR modal (nota o entrada del vault)
```

### Comandos (`:` + enter)

| Comando | Efecto |
|---|---|
| `:new [título]` | crea nota **o** entrada del vault según la vista |
| `:e` | edita la selección |
| `:d` | borra (soft-delete) |
| `:pin` · `:arch` · `:all` | fijar · archivar · ver archivadas *(notas)* |
| `:color turquesa` | recolorea *(notas)*; ES/EN: amarillo, verde, azul, rojo, violeta, turquesa, rosa |
| `:v [notas\|secretos]` | alterna NOTAS ↔ VAULT |
| `:find <texto>` | aplica filtro |
| `:help` | referencia completa |
| `:q` | salir |

En el editor: `:w` guarda, `:wq`/`:x` guarda y cierra, `:q!` cierra sin guardar.

### Teclas

| Tecla | Contexto | Acción |
|---|---|---|
| `j/k`, `↑↓`, `g/G` | normal | mover selección |
| `tab` | normal | NOTAS ↔ VAULT |
| `/` | normal | búsqueda incremental (`esc` limpia) |
| `v` | vault | revelar contraseñas en tarjetas |
| `y` | vault | copiar contraseña al portapapeles |
| `enter`/`e` | normal | editar selección |
| `tab`/`shift+tab` | editor | campo siguiente/anterior |
| `ctrl+s` | editor | guardar |
| `ctrl+r` | editor vault | revelar contraseña del campo |
| `?` | normal | ayuda |
| `q` | normal | salir · `ctrl+c` siempre sale |

La barra inferior muestra el modo, contadores de búsqueda (`🔍 "café" 1/5`)
y minutos restantes de sesión (`🔓 14 min`).

## 5. Pruebas

### Automáticas

```bash
cd client

go test ./...                    # suite completa
go test -race ./internal/ui ./internal/session   # detección de carreras
go test ./internal/crypto -v     # paquete concreto, verbose
```

Qué cubre cada paquete:

| Paquete | Cobertura |
|---|---|
| `crypto` | roundtrip AES-GCM, clave errónea, tampering, cambio de maestra sin recifrar |
| `session` | primer inicio, TTL/auto-lock, reintentos, API programática, autorización SO |
| `authn` | argumentos de sudo, mapeo de errores, PAM por build tag |
| `store` | CRUD notas/secretos, soft-delete, flags de sync, meta KV |
| `ui` | setup/lock, flujos CRUD por comandos, búsqueda, vault completo |

### Checklist manual (para validar cada fase)

```bash
go run ./cmd/strongboxs
```

1. **Fase 6 · Setup**: crea la maestra; prueba corta (<8) → error; no coinciden → reinicia.
2. **Fase 7 · Notas**: `:new Compras` → escribe cuerpo → `ctrl+s` → `esc`.
   Sal con `q`, vuelve a entrar: la nota debe seguir (descifrada tras la maestra).
3. **Fase 7 · Comandos**: `:pin`, `:arch`, `:all`, `:color rojo`,
   `:e` + `:wq`. Verifica el overlay `:help`.
4. **Fase 6 · Lock-screen**: espera el auto-bloqueo (o reduce `DefaultTTL`
   temporalmente); introduce mal la clave → error y campo limpio.
5. **Fase 8 · Búsqueda**: `/caf` filtra en vivo; `esc` limpia; `:find texto` equivalente.
6. **Fase 8 · Vault**: `tab` o `:v s` → `:new Servidor prod` → rellena los
   5 campos (`ctrl+r` revela) → `:wq` → `v` alterna máscara en tarjetas →
   `y` copia → `:d` borra.
7. **Verificación en disco** (opcional):

   ```bash
   sqlite3 ~/.local/share/strongboxs/strongboxs.db \
     "select title,body from notes limit 1;"
   # → blobs ilegibles (ciphertext), jamás texto claro
   ```
8. **Cambio de maestra**: `./strongboxs passwd` → pide contraseña del
   sistema (política sudo/PAM), la actual y dos veces la nueva.
   Los datos NO se recifran (solo se re-envuelve la DEK).

### Reset total

```bash
rm ~/.local/share/strongboxs/strongboxs.db*   # ¡borra TODOS los datos!
```

## 6. Arquitectura (resumen)

```
contraseña maestra ─Argon2id(64MiB,t=1,p=4)→ KEK
DEK aleatoria 256bit ─AES-GCM(KEK)→ envuelta en BD (vault_meta)
campos sensibles ───AES-GCM(DEK)→ BLOBs ("sb1.<b64>")
```

```
client/
├── cmd/strongboxs/main.go   # entry point + subcomando passwd
└── internal/
    ├── authn/               # validación Linux: sudo -S (default) / libpam (-tags pam)
    ├── crypto/              # bóveda: Create/Open/Lock/ChangePassword/Seal/Unseal
    ├── session/             # sesión sudo-style: TTL 15min, Ensure/UnlockWith, LockEvents
    ├── store/               # SQLite sin CGO: notes, secrets, vault_meta
    └── ui/                  # BubbleTea: modos vim, comandos ex, editores, masonry
```

Cambiar la maestra solo re-envuelve la DEK; la sesión mantiene la DEK en
memoria con wipe al bloquear; el borrado es lógico (sync futuro).

## 7. Solución de problemas

| Síntoma | Causa/solución |
|---|---|
| `portapapeles no disponible` | instala `xclip` (X11) o `wl-clipboard` (Wayland) |
| `Contraseña del sistema incorrecta` en `passwd` | tu usuario necesita privilegios sudo (esa es la política validada) |
| Colores apagados | usa una terminal con truecolor (`COLORTERM=truecolor`) |
| Olvidé la maestra | no hay recuperación (Zero-Knowledge): reset total del punto 5 |
