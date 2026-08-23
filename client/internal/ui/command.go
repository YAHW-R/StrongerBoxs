package ui

import (
	"strings"
)

// parseCommand divide una línea tipo ex ("new Lista de compras")
// en nombre y argumentos. Tolera el prefijo ':' y mayúsculas.
// Devuelve ok=false si la línea está vacía (solo cerrar la barra).
func parseCommand(raw string) (name, args string, ok bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), ":"))
	if raw == "" {
		return "", "", false
	}
	fields := strings.SplitN(raw, " ", 2)
	name = strings.ToLower(fields[0])
	if len(fields) == 2 {
		args = strings.TrimSpace(fields[1])
	}
	return name, args, true
}

// paletteNames nombres de color aceptados por :color.
var paletteNames = map[string]string{
	"amarillo": "#F9AB00", "yellow": "#F9AB00",
	"verde": "#34A853", "green": "#34A853",
	"azul": "#4285F4", "blue": "#4285F4",
	"rojo": "#EA4335", "red": "#EA4335",
	"violeta": "#7C4DFF", "violet": "#7C4DFF",
	"turquesa": "#00BFA5", "teal": "#00BFA5",
	"rosa": "#F06292", "pink": "#F06292",
}

func colorByName(s string) (string, bool) {
	v, ok := paletteNames[strings.ToLower(strings.TrimSpace(s))]
	return v, ok
}

func colorNamesList() string {
	names := make([]string, 0, len(paletteNames)/2)
	for _, es := range []string{"amarillo", "verde", "azul", "rojo", "violeta", "turquesa", "rosa"} {
		names = append(names, es)
	}
	return strings.Join(names, ", ")
}

const helpText = `Comandos de Strongboxs (estilo ex):

:new [título]        crea nota (o entrada de vault según la vista)
:e | :edit           edita la selección
:d | :del            borra la selección (soft-delete)
:pin | :arch | :all  fijar · archivar · ver archivadas (notas)
:color <nombre>      amarillo, verde, azul, rojo, violeta, turquesa, rosa
:v [notas|secretos]  alterna entre NOTAS y VAULT (tab)
:find <texto>        aplica filtro ('/' lo abre en vivo)
:help                esta ayuda

Dentro del editor:
:w                   guarda sin cerrar
:wq | :x             guarda y cierra
:q | :q!             cierra sin guardar

Teclas del tablero:
j/k, g/G             mover la selección
/                    búsqueda incremental (esc limpia)
tab                  alterna NOTAS ↔ VAULT
v                    revela contraseñas en las tarjetas (vault)
y                    copia la contraseña seleccionada al portapapeles
ctrl+r               revela la contraseña en el editor
q                    salir`
