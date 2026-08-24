# Strongboxs — Manual del cliente TUI

Guía completa de uso de la aplicación de terminal: modos, comandos,
bóveda con plantillas, búsqueda, sesión y configuración.

> Para requisitos, compilación, stack y visión general del proyecto,
> consulta el [`README.md` raíz](../README.md).

---

## 1. Filosofía de la interfaz

Strongboxs funciona por **modos**, como Neovim:

```
NORMAL ── ':' ──▶ COMANDOS (barra ex) ── enter ──▶ ejecutar
   │
   ├── '/'  ──▶ BÚSQUEDA incremental (filtra en vivo)
   ├── tab  ──▶ alterna vistas NOTAS ↔ VAULT
   └── enter/:e/:new ──▶ EDITOR modal (nota · entrada · plantilla)
```

- **NORMAL**: navegas la cuadrícula con `j/k`; las teclas no imprimibles
  disparan acciones; `:` abre la barra de comandos.
- **COMANDOS**: escribes órdenes tipo ex (`:new web`, `:pin`, `:q`).
- **BÚSQUEDA**: filtra tarjetas mientras tecleas.
- **EDITOR**: formulario modal para crear/editar.

La barra inferior siempre indica dónde estás: vista activa (`NOTAS`/`VAULT`),
resultados de búsqueda (`🔍 "café" 2/5`) y minutos de sesión restantes
(`🔓 14 min`).

## 2. Primer arranque

1. Ejecuta `strongboxs`. Al no existir bóveda verás **🔑 Primera ejecución**.
2. Escribe una contraseña maestra (mínimo **8 caracteres**) y confírmala.
   Esta contraseña descifra tu bóveda local: **nunca sale de tu máquina**.
3. Entras al tablero NOTAS vacío, con la pista `:new <título>`.

En los siguientes arranques aparecerá la **🔒 lock-screen**: introduce la
maestra para continuar. Tras **15 minutos** sin actividad la app se
auto-bloquea sola (cada operación renueva el contador) y todo texto en
claro se elimina de memoria.

## 3. Atajos globales y paleta de comandos

### Atajos directos (tablero)

| Tecla | Acción |
|---|---|
| `ctrl+o` | **nuevo**: nota (en NOTAS) o entrada simple (en VAULT) |
| `ctrl+e` | editar la selección |
| `ctrl+s` | sincronizar cambios pendientes ahora |
| `ctrl+d` | borrar la selección → **pide confirmación** (`y`/`n`) |
| `tab` | alternar vistas **NOTAS ↔ VAULT** |
| `j` `k` `g` `G` | mover la selección |
| `/` | búsqueda incremental |
| `enter` | editar la selección (alias de `ctrl+e`) |
| `v` / `y` *(vault)* | revelar valores · copiar secreto |
| `?` | ayuda · `q` salir |

### Paleta de comandos — `ctrl+k`

Ventana emergente estilo opencode/Claude Code: escribe para filtrar,
navega con `↑/↓`, ejecuta con `enter`, cierra con `esc`.

Centraliza todos los comandos menos frecuentes, **conscientes del contexto**:

- En NOTAS: nueva nota, convertir nota a bóveda, colores por nombre
  (`Color: rojo`…), fijar/archivar, ver archivadas.
- En VAULT: nueva entrada con cada plantilla (`Nueva entrada: web`,
  `: email`, tus plantillas propias…), nueva plantilla, copiar valor,
  listar plantillas.
- Siempre disponibles: editar/borrar selección, buscar, cambiar de vista,
  sincronizar ahora, ayuda, salir.

Las utilidades clásicas se mantienen: `tab` cambia de vista, `enter`
edita, `/` busca.

## 4. Búsqueda

- `/` abre el filtro; los resultados se reducen **mientras tecleas**
  (notas: título+cuerpo; vault: título+usuario+url+campos).
- `enter` aplica y conserva el filtro; `esc` lo limpia del todo.
- `:find texto` equivale a `/` + texto + enter.
- La barra muestra coincidencias sobre el total: `🔍 "prod" 3/17`.
- El cursor navega solo entre resultados; `g/G` también respetan el filtro.

## 5. Editor de notas

Campos: **Título** y **Cuerpo** (multilínea).

| Tecla | Acción |
|---|---|
| `tab` / `shift+tab` | campo siguiente/anterior |
| `ctrl+s` | guardar sin cerrar |
| `:` + `:w` | ídem (estilo ex) |
| `:` + `:wq` / `:x` | guardar y cerrar |
| `:` + `:q!` | cerrar SIN guardar |
| `esc` | cancelar sin guardar |

Título vacío al guardar ⇒ `(sin título)`. Todo lo guardado se cifra al
instante en disco: no existe botón "guardar".

## 6. La bóveda (VAULT) y sus plantillas

Cada entrada sigue una **plantilla** que define sus campos. Tarjeta e
editor se generan desde ella.

### Plantillas incluidas

| Nombre | Icono | Campos |
|---|---|---|
| `simple` ⭐ | 🔑 | Usuario · Valor(secreto) — **predeterminada de `:new`** |
| `web` | 🌐 | URL · Usuario · Contraseña(secreto) · Notas(multi) |
| `email` | ✉️ | Email · Contraseña(secreto) · Webmail · Notas(multi) |
| `nota` | 🗒 | Contenido(multi) — usada por `:tovault` |

### Crear entradas

```text
:new                → simple (usuario + valor)
:new web            → credencial de página
:new email          → cuenta de correo
```

El editor muestra un campo por línea de la plantilla. En campos secretos
verás `••••••••`: pulsa `ctrl+r` para revelarlos mientras editas (y otra
vez para volver a tapar).

En la cuadrícula, los valores sensibles aparecen como `••••••••` de
longitud fija (no filtran el tamaño real); `v` los revela solo en pantalla
y `y` copia el primer valor sensible al portapapeles.

### Tipos de campo admitidos en plantillas propias

| Tipo | Comportamiento |
|---|---|
| `Texto` | valor normal visible |
| `Secreto` | enmascarado en tarjeta, revelable, copiable con `y` |
| `Configuración` | texto técnico (visible) |
| `Título` | su valor encabeza la tarjeta de la entrada |
| `Multilínea` | área de texto (máximo 1 por plantilla) |

### Crear plantillas: `:newp`

`:newp miservidores` abre un formulario seguro (**sin barra de comandos**):

```text
🧩 NUEVA PLANTILLA
▸ Nombre interno (para :new)
  miservidores

▸ Servidor    [Texto]
  Clave Acceso ‹ Secreto ›      ← ←/→ rota el tipo
  Puerto     [Configuración]
```

| Tecla | Acción |
|---|---|
| `tab` / `shift+tab` | recorrer nombre → etiqueta → tipo… |
| `←` `→` `espacio` | rotar el tipo de la fila enfocada |
| `enter` | de la etiqueta salta al selector de tipo |
| `ctrl+n` | añadir campo |
| `ctrl+d` | borrar el campo actual |
| `ctrl+s` | crear la plantilla y salir |
| `esc` | cancelar |

Reglas: etiquetas duplicadas no; un solo campo Multilínea; los espacios de
la etiqueta pasan a `_` en la clave interna. Creada la plantilla, úsala con
`:new miservidores`. `:deltemplate miservidores` la elimina (las integradas
no se pueden borrar).

### Convertir notas en entradas

Selecciona una nota y ejecuta `:tovault` (o `:tv`, `:cifrar`): se crea una
entrada cifrada con plantilla `nota`, la nota original se borra y saltas al
VAULT con ella seleccionada. Ambos cambios se sincronizan.

## 7. Sesión estilo sudo

- Cada operación sensible renueva un temporizador de **15 minutos**.
- Al expirar: lock-screen automática, wipe de plaintext en memoria y
  limpieza del campo de contraseña.
- El indicador `🔓 Xm` de la barra baja muestra el tiempo restante.
- Cambiar la maestra: `strongboxs passwd` — pide primero tu contraseña del
  sistema Linux (política sudo/PAM), luego dos veces la nueva. Los datos NO
  se recifran: solo se re-envuelve la DEK.

## 8. Sincronización por peticiones

### Primer uso: el asistente

Al llegar al tablero sin sincronización configurada, se abre un
**asistente emergente**: Servidor · Usuario · Contraseña de cuenta.
`ctrl+s` valida, **escribe `~/.config/strongboxs/sync.env` por ti**
(preservando otras líneas) y arranca el motor al momento — sin reiniciar.

- `esc` = "más tarde": no vuelve a preguntar en esta instalación.
- Puedes abrirlo cuando quieras: `ctrl+k` → *"Configurar sincronización…"*.
- El usuario se normaliza a minúsculas automáticamente.

También puedes editar `sync.env` a mano o usar variables de entorno
exportadas: tienen prioridad y la app las detecta al arranque.

### Cómo funciona

| Evento | Qué pasa |
|---|---|
| Guardas (`ctrl+s`, `:w`, `:wq`), creas, borras, fijas, archivas, conviertes | **PUSH inmediato** (debounce ~1,2 s agrupa ráfagas) |
| Sales de la app | *flush* final de 5 s sube lo pendiente |
| Editor/asistente abiertos | **PULL pausado**: el servidor no pisa lo que escribes |
| Tablero, cada 90 s | PULL de fondo (`STRONGBOXS_SYNC_INTERVAL_SECS`) |

Conflictos por **fecha**: gana el `updated_at` más nuevo. Sin conexión los
cambios quedan pendientes (`dirty`) y suben solos al reconectar. Viaja solo
ciphertext; errores y estado viven en los Stats del motor (no ensucian pantalla).

## 10. Compilación e instalación

```bash
cd client
go build -o strongboxs ./cmd/strongboxs                  # sin CGO
go build -tags pam -o strongboxs-pam ./cmd/strongboxs    # PAM (gcc+libpam-dev)
sudo make install                                        # binario+menú (desde raíz)
./strongboxs passwd                                      # cambio de maestra autorizado
```

Datos locales: `$XDG_DATA_HOME/strongboxs/strongboxs.db` (0600, WAL).
Reset total: borra ese archivo (¡pierdes todo!).

## 11. Pruebas

Automáticas:

```bash
go test ./...                       # suite completa
go test -race ./internal/ui ./internal/session
go test ./internal/crypto -v
```

Checklist manual sugerido:

1. Setup: maestra corta → error; no coinciden → reinicio del flujo.
2. `:new Compras` → escribir → `ctrl+s` → `esc` → salir con `q` y reentrar:
   la nota sigue ahí tras la lock-screen.
3. `/caf` filtra en vivo; `esc` limpia; `:find` equivalente.
4. `:newp banco` → campos `Banco:texto`, `Titular:texto`,
   `Clave:secreto` → `ctrl+s`; luego `:new banco` y rellenar.
5. `y` copia · `v` revela · `ctrl+r` en editor.
6. `:tovault` sobre una nota; verificar que desaparece de NOTAS.
7. Esperar auto-bloqueo (o bajar `DefaultTTL`) → lock-screen correcta.
8. `sqlite3 ~/.local/share/strongboxs/strongboxs.db \
   "select title from notes limit 1;"` → ciphertext ilegible.

## 12. Solución de problemas

| Síntoma | Causa/solución |
|---|---|
| No puedo escribir en un campo | actualiza a esta versión; si persiste, reporta con tu tamaño de terminal |
| `[sync] servidor 422 … claves no permitidas` | reconstruye el servidor: `docker compose up -d --build` |
| `portapapeles no disponible` | instala `xclip` (X11) o `wl-clipboard` (Wayland) |
| `Contraseña del sistema incorrecta` en passwd | tu usuario necesita sudoers (esa política valida Linux) |
| Colores apagados | terminal sin truecolor (`COLORTERM=truecolor`) |
| Olvidé la maestra | sin recuperación (ZK): reset total de la BD |
