package ui

import (
	"fmt"
	"strings"
	"time"
)

// joinStatus une fragmentos de la barra inferior con separadores.
func joinStatus(parts []string) string {
	return helpStyle.Render(strings.Join(parts, "  ·  "))
}

// humanizeTime formatea un timestamp relativo ("ahora", "hace 5 min", …).
func humanizeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "ahora"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %d h", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("hace %d d", int(d.Hours()/24))
	default:
		return t.Format("02/01/2006")
	}
}
