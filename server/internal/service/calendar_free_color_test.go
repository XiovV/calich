// calendar_free_color_test.go covers pickFreeColor's own logic in isolation
// (#194, ADR-0057): trying calendarSwatches in fixed order, falling back to
// a random hex once they're exhausted, and staying within the range
// calendarSwatches occupies when it does.
package service

import (
	"math"
	"strconv"
	"testing"
)

func TestPickFreeColor_ReturnsFirstSwatchWhenNoneUsed(t *testing.T) {
	color := pickFreeColor(nil)
	if color != calendarSwatches[0] {
		t.Fatalf("pickFreeColor(nil) = %q, want the first Swatch %q", color, calendarSwatches[0])
	}
}

func TestPickFreeColor_SkipsUsedSwatchesInFixedOrder(t *testing.T) {
	used := []string{calendarSwatches[0], calendarSwatches[1]}
	color := pickFreeColor(used)
	if color != calendarSwatches[2] {
		t.Fatalf("pickFreeColor = %q, want the third Swatch %q", color, calendarSwatches[2])
	}
}

func TestPickFreeColor_IgnoresUsedColorsOutsideTheSwatchSet(t *testing.T) {
	// A used colour that matches no Swatch (an Owner's own arbitrary pick,
	// or a prior random fallback) shouldn't block any Swatch from being
	// offered — only a collision with a Swatch itself does.
	color := pickFreeColor([]string{"#123456FF", "#ABCDEFFF"})
	if color != calendarSwatches[0] {
		t.Fatalf("pickFreeColor = %q, want the first Swatch %q", color, calendarSwatches[0])
	}
}

func TestPickFreeColor_FallsBackToRandomHexOnceEverySwatchIsTaken(t *testing.T) {
	color := pickFreeColor(calendarSwatches)
	for _, swatch := range calendarSwatches {
		if color == swatch {
			t.Fatalf("pickFreeColor with every Swatch used returned a Swatch %q instead of a fallback", color)
		}
	}
	assertCanonicalHex(t, color)
	assertWithinSwatchSaturationAndLightness(t, color)
}

// The fallback is drawn fresh each call — exhausting the palette repeatedly
// shouldn't ever hand back the same random hex twice in a row for practical
// purposes, confirming the hue really is randomised rather than fixed.
func TestPickFreeColor_FallbackHueVaries(t *testing.T) {
	first := pickFreeColor(calendarSwatches)
	distinct := false
	for range 20 {
		if pickFreeColor(calendarSwatches) != first {
			distinct = true
			break
		}
	}
	if !distinct {
		t.Fatalf("expected the random fallback to vary across calls, got %q every time", first)
	}
}

func assertCanonicalHex(t *testing.T, color string) {
	t.Helper()
	if len(color) != 9 || color[0] != '#' {
		t.Fatalf("color %q is not canonical \"#RRGGBBAA\"", color)
	}
	if color[7:9] != "FF" {
		t.Fatalf("color %q is not fully opaque", color)
	}
}

// assertWithinSwatchSaturationAndLightness re-derives HSL from the returned
// hex and checks it lands within the range calendarSwatches occupies
// (ADR-0057) — not raw random RGB that could land on something washed-out
// or muddy.
func assertWithinSwatchSaturationAndLightness(t *testing.T, color string) {
	t.Helper()
	r, g, b := hexToRGB(t, color)
	_, s, l := rgbToHSL(r, g, b)

	const epsilon = 0.01
	if s < fallbackSaturationMin-epsilon || s > fallbackSaturationMax+epsilon {
		t.Fatalf("color %q has saturation %.3f outside [%.2f, %.2f]", color, s, fallbackSaturationMin, fallbackSaturationMax)
	}
	if l < fallbackLightnessMin-epsilon || l > fallbackLightnessMax+epsilon {
		t.Fatalf("color %q has lightness %.3f outside [%.2f, %.2f]", color, l, fallbackLightnessMin, fallbackLightnessMax)
	}
}

func hexToRGB(t *testing.T, color string) (r, g, b float64) {
	t.Helper()
	ri, err := strconv.ParseInt(color[1:3], 16, 0)
	if err != nil {
		t.Fatalf("parse red channel of %q: %v", color, err)
	}
	gi, err := strconv.ParseInt(color[3:5], 16, 0)
	if err != nil {
		t.Fatalf("parse green channel of %q: %v", color, err)
	}
	bi, err := strconv.ParseInt(color[5:7], 16, 0)
	if err != nil {
		t.Fatalf("parse blue channel of %q: %v", color, err)
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255
}

// rgbToHSL is the inverse of hslToRGB, used only by this test file to
// verify the fallback's random draw actually lands in the range it was
// asked to.
func rgbToHSL(r, g, b float64) (h, s, l float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l = (max + min) / 2

	if max == min {
		return 0, 0, l
	}

	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}

	switch max {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}
