package caldavserver

import "testing"

func TestHexForColor_AllEightEnumColors_ReturnCanonicalHex(t *testing.T) {
	want := map[string]string{
		"tomato":    "#e2483d",
		"flamingo":  "#e67c9a",
		"banana":    "#e4c441",
		"sage":      "#6b9071",
		"peacock":   "#12809c",
		"blueberry": "#3f51b5",
		"grape":     "#8e44ad",
		"graphite":  "#6b7280",
	}
	for color, hex := range want {
		got, ok := hexForColor(color)
		if !ok {
			t.Fatalf("expected hexForColor(%q) to be ok", color)
		}
		if got != hex {
			t.Fatalf("hexForColor(%q) = %q, want %q", color, got, hex)
		}
	}
}

func TestHexForColor_UnknownColor_NotOK(t *testing.T) {
	if _, ok := hexForColor("chartreuse"); ok {
		t.Fatalf("expected an unknown color to not be ok")
	}
}

func TestParseHexColor_ValidForms(t *testing.T) {
	tests := []struct {
		name    string
		hex     string
		r, g, b uint8
	}{
		{"3-digit", "#f00", 0xff, 0x00, 0x00},
		{"6-digit", "#12809c", 0x12, 0x80, 0x9c},
		{"8-digit with alpha", "#12809cff", 0x12, 0x80, 0x9c},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, ok := parseHexColor(tt.hex)
			if !ok {
				t.Fatalf("expected %q to parse", tt.hex)
			}
			if r != tt.r || g != tt.g || b != tt.b {
				t.Fatalf("parseHexColor(%q) = (%d,%d,%d), want (%d,%d,%d)", tt.hex, r, g, b, tt.r, tt.g, tt.b)
			}
		})
	}
}

func TestParseHexColor_InvalidForms(t *testing.T) {
	tests := []string{
		"12809c",   // missing "#"
		"#12809",   // odd digit count (5)
		"#1280",    // wrong length (4)
		"#12809cf", // wrong length (7)
		"#gg0000",  // non-hex chars
		"",
	}
	for _, hex := range tests {
		t.Run(hex, func(t *testing.T) {
			if _, _, _, ok := parseHexColor(hex); ok {
				t.Fatalf("expected %q to be rejected", hex)
			}
		})
	}
}

func TestNearestColor_ExactMatch_ReturnsSameColor(t *testing.T) {
	for _, color := range calendarColorOrder {
		hex, _ := hexForColor(color)
		got, ok := nearestColor(hex)
		if !ok {
			t.Fatalf("expected nearestColor(%q) to be ok", hex)
		}
		if got != color {
			t.Fatalf("nearestColor(%q) = %q, want %q", hex, got, color)
		}
	}
}

func TestNearestColor_CloseButNotExact_ReturnsClosestEnumColor(t *testing.T) {
	// One unit off peacock (#12809c -> 18,128,156) in every channel.
	got, ok := nearestColor("#13819d")
	if !ok {
		t.Fatalf("expected nearestColor to be ok")
	}
	if got != "peacock" {
		t.Fatalf("expected the nearest color to be peacock, got %q", got)
	}
}

func TestNearestColor_EquidistantTie_ReturnsEarliestDeclaredColor(t *testing.T) {
	// The exact RGB midpoint between tomato (#e2483d) and banana (#e4c441):
	// (227,134,63) = #e3863f. Both are equidistant (squared distance 3849)
	// and no other enum color is closer; tomato is declared before banana in
	// calendarColorOrder, so it must win the tie.
	got, ok := nearestColor("#e3863f")
	if !ok {
		t.Fatalf("expected nearestColor to be ok")
	}
	if got != "tomato" {
		t.Fatalf("expected the tie to break toward tomato (declared first), got %q", got)
	}
}

func TestNearestColor_MalformedHex_NotOK(t *testing.T) {
	if _, ok := nearestColor("not-a-color"); ok {
		t.Fatalf("expected a malformed hex to not be ok")
	}
}
