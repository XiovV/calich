package service

import (
	"fmt"
	"math"
	"math/rand/v2"
)

// calendarSwatches mirrors the frontend's SWATCHES (calendarColors.ts) in
// the same fixed order — pickFreeColor tries them in this order, the same
// shape as the frontend's own getNextUnusedColor (ADR-0057). The two lists
// are allowed to drift, same as service.defaultCalendars already does, but
// keeping them in sync is what makes an auto-assigned colour recognizable as
// "just a Swatch" rather than something exotic.
var calendarSwatches = []string{
	"#E2483DFF", // tomato
	"#E67C9AFF", // flamingo
	"#E4C441FF", // banana
	"#6B9071FF", // sage
	"#12809CFF", // peacock
	"#3F51B5FF", // blueberry
	"#8E44ADFF", // grape
	"#6B7280FF", // graphite
}

// fallbackSaturationMin/Max and fallbackLightnessMin/Max are the range of
// saturation and lightness calendarSwatches occupies (computed offline from
// their hex values) — the bounds pickFreeColor's random fallback draws
// within once every Swatch is taken, so the fallback lands somewhere a
// Swatch plausibly could rather than on something washed-out or muddy
// (ADR-0057).
const (
	fallbackSaturationMin = 0.09
	fallbackSaturationMax = 0.79
	fallbackLightnessMin  = 0.34
	fallbackLightnessMax  = 0.69
)

// pickFreeColor returns the first of calendarSwatches absent from used, or a
// random fallback hex outside the Swatch set once all 8 are taken
// (ADR-0057). Comparison is exact-string, so used must already be in
// canonical "#RRGGBBAA" form — every caller of resolveDisplayColor already
// deals in that form (NormalizeColor's output), so no normalization happens
// here.
func pickFreeColor(used []string) string {
	taken := make(map[string]struct{}, len(used))
	for _, color := range used {
		taken[color] = struct{}{}
	}
	for _, swatch := range calendarSwatches {
		if _, ok := taken[swatch]; !ok {
			return swatch
		}
	}
	return randomFallbackColor()
}

// randomFallbackColor draws a hue uniformly at random and a saturation and
// lightness within the range calendarSwatches occupies, converts to RGB,
// and returns it in canonical "#RRGGBBAA" form with a fully opaque alpha —
// pickFreeColor's fallback once every Swatch in a Workspace is already in
// use (ADR-0057).
func randomFallbackColor() string {
	hue := rand.Float64() * 360
	saturation := fallbackSaturationMin + rand.Float64()*(fallbackSaturationMax-fallbackSaturationMin)
	lightness := fallbackLightnessMin + rand.Float64()*(fallbackLightnessMax-fallbackLightnessMin)
	r, g, b := hslToRGB(hue, saturation, lightness)
	return fmt.Sprintf("#%02X%02X%02XFF", r, g, b)
}

// hslToRGB converts hue (degrees, [0, 360)), saturation and lightness
// (fractions, [0, 1]) to 8-bit RGB channels — the standard HSL-to-RGB
// formula, needed because Go has no HSL type of its own.
func hslToRGB(hue, saturation, lightness float64) (r, g, b uint8) {
	c := (1 - math.Abs(2*lightness-1)) * saturation
	hPrime := hue / 60
	x := c * (1 - math.Abs(math.Mod(hPrime, 2)-1))
	m := lightness - c/2

	var r1, g1, b1 float64
	switch {
	case hPrime < 1:
		r1, g1, b1 = c, x, 0
	case hPrime < 2:
		r1, g1, b1 = x, c, 0
	case hPrime < 3:
		r1, g1, b1 = 0, c, x
	case hPrime < 4:
		r1, g1, b1 = 0, x, c
	case hPrime < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}

	return uint8(math.Round((r1 + m) * 255)), uint8(math.Round((g1 + m) * 255)), uint8(math.Round((b1 + m) * 255))
}
