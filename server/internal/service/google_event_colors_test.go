package service

import (
	"strconv"
	"testing"
)

func TestGoogleEventColorHex_KnownColorIDMapsToCanonicalHex(t *testing.T) {
	got := googleEventColorHex("5")
	if got == nil {
		t.Fatalf("expected colorId 5 to map to a hex, got nil")
	}
	if *got != "#FBD75BFF" {
		t.Fatalf("expected the canonical NormalizeColor form, got %q", *got)
	}
}

func TestGoogleEventColorHex_EmptyColorIDMapsToNil(t *testing.T) {
	if got := googleEventColorHex(""); got != nil {
		t.Fatalf("expected no colorId to map to nil (inherit the Calendar's colour), got %v", *got)
	}
}

func TestGoogleEventColorHex_UnrecognisedColorIDMapsToNil(t *testing.T) {
	if got := googleEventColorHex("99"); got != nil {
		t.Fatalf("expected an unrecognised colorId to map to nil, got %v", *got)
	}
}

func TestGoogleEventColorHex_EveryDocumentedColorIDMapsToAValue(t *testing.T) {
	for id := 1; id <= 11; id++ {
		colorID := strconv.Itoa(id)
		if got := googleEventColorHex(colorID); got == nil {
			t.Fatalf("expected colorId %q to map to a hex", colorID)
		}
	}
}
