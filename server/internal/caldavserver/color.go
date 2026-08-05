// color.go maps this app's fixed 8-color Calendar enum onto the Apple/DAVx⁵
// calendar-color WebDAV extension property (ADR-0028): reads always serve
// the canonical hex for the stored enum color, and writes (proppatch.go)
// snap an arbitrary client hex to its nearest enum color and persist that,
// never the client's raw value — extending ADR-0026's "columns are the sole
// source of truth, serve the normalized reconstruction" stance from the
// calendar-object resource up to the Calendar-collection resource.
package caldavserver

import (
	"context"
	"strconv"
)

const calendarColorNamespace = "http://apple.com/ns/ical/"

// calendarColorHex mirrors the 8 --color-calendar-* custom properties in
// src/index.css. Kept in sync by hand — the same cross-language duplication
// service.CalendarColors already has against src/lib/calendarColors.ts.
var calendarColorHex = map[string]string{
	"tomato":    "#e2483d",
	"flamingo":  "#e67c9a",
	"banana":    "#e4c441",
	"sage":      "#6b9071",
	"peacock":   "#12809c",
	"blueberry": "#3f51b5",
	"grape":     "#8e44ad",
	"graphite":  "#6b7280",
}

// calendarColorOrder is calendarColorHex's keys in service.CalendarColors'
// declared order, so nearestColor can break RGB-distance ties
// deterministically (earliest declared color wins).
var calendarColorOrder = []string{
	"tomato", "flamingo", "banana", "sage", "peacock", "blueberry", "grape", "graphite",
}

// hexForColor returns color's canonical hex. ok is false for anything not
// in calendarColorHex — shouldn't happen for a value CalendarService has
// already validated.
func hexForColor(color string) (hex string, ok bool) {
	hex, ok = calendarColorHex[color]
	return hex, ok
}

// parseHexColor parses "#RGB", "#RRGGBB", or "#RRGGBBAA" into 0-255 RGB. Any
// alpha digits are parsed (to validate them) but discarded — this app's
// Color column has no opacity concept. ok is false for a missing "#", any
// other digit count, or non-hex digits.
func parseHexColor(s string) (r, g, b uint8, ok bool) {
	if len(s) == 0 || s[0] != '#' {
		return 0, 0, 0, false
	}
	digits := s[1:]

	var rs, gs, bs string
	switch len(digits) {
	case 3:
		rs, gs, bs = digits[0:1]+digits[0:1], digits[1:2]+digits[1:2], digits[2:3]+digits[2:3]
	case 6, 8:
		rs, gs, bs = digits[0:2], digits[2:4], digits[4:6]
	default:
		return 0, 0, 0, false
	}

	rv, err := strconv.ParseUint(rs, 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	gv, err := strconv.ParseUint(gs, 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	bv, err := strconv.ParseUint(bs, 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	if len(digits) == 8 {
		if _, err := strconv.ParseUint(digits[6:8], 16, 8); err != nil {
			return 0, 0, 0, false
		}
	}

	return uint8(rv), uint8(gv), uint8(bv), true
}

// nearestColor maps an arbitrary hex string to the service.CalendarColors
// enum member whose canonical RGB is closest by squared Euclidean distance.
// ok is false only when hex fails to parse at all; once parsed, some enum
// color is always returned. Ties are broken by calendarColorOrder's
// declared order.
func nearestColor(hex string) (color string, ok bool) {
	r, g, b, parsed := parseHexColor(hex)
	if !parsed {
		return "", false
	}

	best := ""
	bestDist := -1
	for _, name := range calendarColorOrder {
		cr, cg, cb, _ := parseHexColor(calendarColorHex[name])
		dist := squaredDistance(r, g, b, cr, cg, cb)
		if bestDist == -1 || dist < bestDist {
			best = name
			bestDist = dist
		}
	}
	return best, true
}

func squaredDistance(r1, g1, b1, r2, g2, b2 uint8) int {
	dr := int(r1) - int(r2)
	dg := int(g1) - int(g2)
	db := int(b1) - int(b2)
	return dr*dr + dg*dg + db*db
}

// injectCalendarColor rewrites every <response> block in body whose href
// resolves via colorFor to carry a 200 calendar-color propstat, the same
// way injectGetCTag does for getctag (propertyinject.go).
func injectCalendarColor(ctx context.Context, body []byte, colorFor propertyValueFunc) []byte {
	return injectProperty(ctx, body, "calendar-color", calendarColorNamespace, colorFor)
}
