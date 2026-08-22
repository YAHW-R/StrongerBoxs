package ui

import "testing"

func TestParseCommand(t *testing.T) {
	cases := []struct {
		raw  string
		name string
		args string
		ok   bool
	}{
		{"new Compras", "new", "Compras", true},
		{":new Compras", "new", "Compras", true},
		{"  :E  ", "e", "", true},
		{"PIN", "pin", "", true},
		{"color teal", "color", "teal", true},
		{"del   extra   args", "del", "extra   args", true},
		{"", "", "", false},
		{"   ", "", "", false},
		{":", "", "", false},
	}
	for _, tc := range cases {
		name, args, ok := parseCommand(tc.raw)
		if name != tc.name || args != tc.args || ok != tc.ok {
			t.Errorf("parseCommand(%q) = (%q, %q, %v), quiero (%q, %q, %v)",
				tc.raw, name, args, ok, tc.name, tc.args, tc.ok)
		}
	}
}

func TestColorByName(t *testing.T) {
	if hex, ok := colorByName("TURQUESA"); !ok || hex != "#00BFA5" {
		t.Errorf("turquesa → %q, %v", hex, ok)
	}
	if _, ok := colorByName("dorado"); ok {
		t.Error("'dorado' no debería existir en la paleta")
	}
}
