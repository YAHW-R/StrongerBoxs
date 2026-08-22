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
	"verde":    "#34A853", "green": "#34A853",
	"azul":     "#4285F4", "blue": "#4285F4",
	"rojo":     "#EA4335", "red": "#EA4335",
	"violeta":  "#7C4DFF", "violet": "#7C4DFF",
	"turquesa": "#00BFA5", "teal": "#00BFA5",
	"rosa":     "#F06292", "pink": "#F06292",
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

:new [título]        crea una nota y abre el editor
:e | :edit           edita la nota seleccionada
:d | :del            borra la nota seleccionada
:pin                 fija/desfija la nota seleccionada
:arch                archiva/restaura la nota seleccionada
:all                 alterna mostrar archivadas
:color <nombre>      amarillo, verde, azul, rojo, violeta, turquesa, rosa

Dentro del editor:
:w                   guarda sin cerrar
:wq | :x             guarda y cierra
:q | :q!             cierra sin guardar

Otras teclas: j/k mover · enter/:e editar · ? ayuda · q salir`
