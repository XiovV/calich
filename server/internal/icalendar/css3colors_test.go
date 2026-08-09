package icalendar

import "testing"

func TestNearestCSS3Keyword_ExactMatch(t *testing.T) {
	if got := nearestCSS3Keyword(0xFF, 0x00, 0x00); got != "red" {
		t.Fatalf("expected red, got %q", got)
	}
}

func TestNearestCSS3Keyword_ClosestByDistance(t *testing.T) {
	// One unit off pure red — still nearer to "red" than any other keyword.
	if got := nearestCSS3Keyword(0xFE, 0x01, 0x01); got != "red" {
		t.Fatalf("expected red, got %q", got)
	}
}

func TestCSS3KeywordRGB_CaseInsensitive(t *testing.T) {
	r, g, b, ok := css3KeywordRGB("ReD")
	if !ok || r != 0xFF || g != 0x00 || b != 0x00 {
		t.Fatalf("expected case-insensitive match to red, got (%v,%v,%v,%v)", r, g, b, ok)
	}
}

func TestCSS3KeywordRGB_Unknown(t *testing.T) {
	if _, _, _, ok := css3KeywordRGB("not-a-color"); ok {
		t.Fatalf("expected an unrecognized keyword to report ok=false")
	}
}

func TestParseHexRGB_SixAndEightDigit(t *testing.T) {
	r, g, b, err := parseHexRGB("#6495ED")
	if err != nil || r != 0x64 || g != 0x95 || b != 0xED {
		t.Fatalf("parseHexRGB(#6495ED) = (%v,%v,%v,%v)", r, g, b, err)
	}
	r, g, b, err = parseHexRGB("#6495EDFF")
	if err != nil || r != 0x64 || g != 0x95 || b != 0xED {
		t.Fatalf("parseHexRGB(#6495EDFF) = (%v,%v,%v,%v)", r, g, b, err)
	}
}

func TestParseHexRGB_Invalid(t *testing.T) {
	if _, _, _, err := parseHexRGB("#FFF"); err == nil {
		t.Fatalf("expected an error for a 3-digit hex color")
	}
}
