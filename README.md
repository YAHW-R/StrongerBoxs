# Strongboxs

Gestor local de **notas** (estilo Google Keep) y **contraseñas** en la
terminal, con cifrado fuerte, sesión estilo *sudo*, una TUI de modos al
estilo Neovim y **sincronización Zero-Knowledge por fechas** en segundo
plano.

> Zero-Knowledge: el servidor solo almacena ciphertext opaco. Tus claves
> y el contenido legible jamás salen de tu máquina.

```
┌──────────────────────────────┐         ┌───────────────────────────┐
│ CLIENTE · Go                 │  HTTPS  │ SERVIDOR · FastAPI        │
│ TUI BubbleTea + Lipgloss     │ ──────► │ API REST dockerizada      │
│ AES-256-GCM + Argon2id       │ ct only │ PostgreSQL 16             │
│ SQLite sin CGO               │         │ JWT + Argon2(verifier)    │
│ Sesión sudo-style (TTL)      │         │ LWW por fecha + tombstones│
└──────────────────────────────┘         └───────────────────────────┘
```

## Rasgos generales

| Área | Detalle |
|---|---|
| Notas | Cuadrícula masonry responsive, fijar/archivar, colores, búsqueda incremental |
| Bóveda | Plantillas de campos (incluidas y propias), máscara/revelado, copiar al portapapeles |
| Cifrado | Argon2id (64 MiB) → KEK; DEK aleatoria envuelta; campos como sobres `sb1.*` |
| Cambio de maestra | Solo re-envuelve la DEK (cero recifrado); autoriza contra Linux vía sudo/PAM |
| Sesión | Auto-bloqueo a los 15 min de inactividad; wipe de texto en claro de memoria |
| Sincronización | **Por peticiones**: push tras cada guardado; pull de fondo solo en el tablero; conflictos por fecha |
| Interfaz | Modos vim (normal/comandos/búsqueda/editor), comandos `:` estilo ex |

## Stack y lenguajes

- **Cliente**: Go 1.26+ — `charmbracelet/bubbletea`, `bubbles`, `lipgloss`;
  `modernc.org/sqlite` (SQLite 100 % Go, sin CGO); `golang.org/x/crypto`
  (Argon2id); `x/term`; portapapeles vía `atotto/clipboard`.
  Binario estático: sin dependencias de sistema para el cifrado.
- **Servidor**: Python 3.12+ — FastAPI + Uvicorn, SQLAlchemy 2.x sobre
  PostgreSQL (psycopg 3), PyJWT, argon2-cffi, pydantic-settings.
- **Infra**: Docker Compose (api + postgres:16-alpine + volumen persistente);
  unidad systemd opcional para autoarranque.
- **Tests**: `go test` (+ `-race`) y pytest con SQLite embebido.

## Modelo de seguridad (resumen)

```
contraseña maestra ──Argon2id(salt_local)──▶ KEK
DEK aleatoria 256-bit ──AES-GCM(KEK)──▶ guardada envuelta (vault_meta)
campos sensibles ─────AES-GCM(DEK)──▶ BLOBs "sb1.<base64>"
```

- La maestra nunca sale del equipo; el servidor ni la ve ni necesita derivarla.
- La cuenta de sincronización usa credenciales **distintas**: el cliente envía
  `SHA256(Argon2id(pass_cuenta, salt_del_servidor))` y el servidor guarda su
  propio hash Argon2id de ese valor.
- El borrado es lógico (tombstones) para replicarse sin pérdida.
- Validación ZK en la API: un campo sensible en claro ⇒ HTTP 422.

## Estructura del repo

```
client/                  Cliente TUI en Go (módulo propio)
  cmd/strongboxs/        entry point + carga de sync.env + subcomando passwd
  internal/
    ui/                  modos vim, comandos ex, editores dinámicos, masonry
    store/               SQLite: notes, secrets, templates, vault_meta
    crypto/              bóveda (Create/Open/Lock/ChangePassword/Seal/Unseal)
    session/             sesión sudo-style con TTL y eventos de bloqueo
    authn/               validación Linux: sudo -S (default) / libpam (-tags pam)
    sync/                motor background pull→merge→push por fechas
server/                  API FastAPI Zero-Knowledge + tests pytest
packaging/               .desktop + unidad systemd opcional
docker-compose.yml       api :8000 + postgres:16-alpine
Makefile                 build/install/uninstall/server-up/down/test…
docs/                    reservado para documentación adicional
```

## Instalación rápida

### 0) Requisitos previos

Debian/Ubuntu:

```bash
sudo apt update
sudo apt install -y golang-go make git python3 python3-venv \
                    docker.io docker-compose-v2 sqlite3 xclip
sudo usermod -aG docker "$USER" && newgrp docker   # docker sin sudo
sudo systemctl enable --now docker
```

Arch:

```bash
sudo pacman -S go make git python docker docker-compose sqlite xclip
```

### 1) Compilar y verificar

```bash
git clone <tu-repo> strongboxs && cd strongboxs
make build          # cliente → client/bin/strongboxs
make server-venv    # entorno Python del servidor
make test           # suite completa: Go + pytest
```

### 2) Servidor (Docker)

```bash
cp .env.example .env     # define STRONGBOXS_SECRET_KEY
make server-up           # API :8000 + PostgreSQL
curl localhost:8000/health
make server-down         # parar · make server-logs para logs
```

Autoarranque al boot (opcional): ver `packaging/strongboxs-server.service`.

### 3) Cliente en el sistema

```bash
sudo make install        # /usr/local/bin/strongboxs + menú de apps
strongboxs               # primer arranque: crea tu contraseña maestra
```

### 4) Activar sincronización (opcional)

```bash
mkdir -p ~/.config/strongboxs
cat > ~/.config/strongboxs/sync.env <<'EOF'
STRONGBOXS_SYNC_URL=http://localhost:8000
STRONGBOXS_SYNC_USER=tu_usuario
STRONGBOXS_SYNC_PASSWORD=passDeCuenta
EOF
chmod 600 ~/.config/strongboxs/sync.env
```

## Tareas Make

| Target | Acción |
|---|---|
| `make build` | compila el binario del cliente |
| `sudo make install` / `uninstall` | instala/desinstala binario + lanzador |
| `make server-up` / `down` / `logs` | ciclo de vida Docker |
| `make server-venv` | crea `server/.venv` con dependencias Python |
| `make test` | suite Go + pytest |
| `make clean` | limpia artefactos |

## Documentación

- **Uso profundo del cliente TUI** (comandos, plantillas, bóveda,
  búsqueda, atajos): [`client/README.md`](client/README.md)
- API REST: `GET /health`, `/auth/{salt,register,login}`,
  `/items/{push,pull}` — contrato detallado en `server/app/routers/`.

## Estado y hoja de ruta

Funciona de punta a punta: notas y bóveda cifradas localmente, plantillas
propias, conversión nota→vault, sincronización bidireccional por fechas.
Pendiente en roadmap: etiquetas/etiquetado cruzado, export/import cifrado,
refresh tokens para sync de larga duración.
