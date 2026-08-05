package service

import "testing"

func TestNormalizeColor_ValidForms_WidenAndCanonicalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"3-digit widens with FF alpha", "#f00", "#FF0000FF"},
		{"6-digit widens with FF alpha", "#12809c", "#12809CFF"},
		{"8-digit keeps its own alpha", "#12809c80", "#12809C80"},
		{"already canonical", "#12809CFF", "#12809CFF"},
		{"leading/trailing whitespace trimmed", "  #12809c  ", "#12809CFF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeColor(tt.input)
			if !ok {
				t.Fatalf("expected %q to be valid", tt.input)
			}
			if got != tt.want {
				t.Fatalf("NormalizeColor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeColor_InvalidForms_NotOK(t *testing.T) {
	tests := []string{
		"12809c",   // missing "#"
		"#12809",   // odd digit count (5)
		"#1280",    // wrong length (4)
		"#12809cf", // wrong length (7)
		"#gg0000",  // non-hex chars
		"",
		"not-a-color",
		"peacock", // the old enum member is no longer a valid color
	}
	for _, hex := range tests {
		t.Run(hex, func(t *testing.T) {
			if _, ok := NormalizeColor(hex); ok {
				t.Fatalf("expected %q to be rejected", hex)
			}
		})
	}
}
