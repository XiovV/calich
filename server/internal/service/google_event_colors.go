// google_event_colors.go maps Google's fixed set of eleven event colour ids
// to the arbitrary-hex value space this app stores an Event colour in (#289,
// ADR-0029, ADR-0075).
//
// Google's events.list carries a `colorId` — a string "1".."11" — not a hex,
// and the id-to-hex table is the one Google itself serves from its `colors`
// endpoint. That endpoint's values are stable (they are an API contract, not
// the Calendar web UI's own palette, which drifts), so this app hardcodes
// them rather than spending a request per Refresh to fetch a table that does
// not change. An Event with no colorId, or one this table does not know,
// maps to nil — "inherit the Calendar's colour" (ADR-0043).
//
// The mapping is deliberately one-way. ADR-0075: this app's Event colour is
// an arbitrary hex and Google offers a fixed eleven, so a Write-back that
// pushed colour would snap a User's choice to the nearest of eleven. Colour
// is seeded inbound and shadow-tracked; it is never sent back.
package service

// googleEventColors is Google's `colors.get` event palette, keyed by the
// colorId events.list reports. Values are lower-case 6-digit hex exactly as
// Google serves them; googleEventColorHex normalizes them to this app's
// canonical form.
var googleEventColors = map[string]string{
	"1":  "#a4bdfc", // Lavender
	"2":  "#7ae7bf", // Sage
	"3":  "#dbadff", // Grape
	"4":  "#ff887c", // Flamingo
	"5":  "#fbd75b", // Banana
	"6":  "#ffb878", // Tangerine
	"7":  "#46d6db", // Peacock
	"8":  "#e1e1e1", // Graphite
	"9":  "#5484ed", // Blueberry
	"10": "#51b749", // Basil
	"11": "#dc2127", // Tomato
}

// googleEventColorHex maps one events.list colorId to the canonical hex this
// app stores an Event colour in, or nil when the id is empty or unrecognised
// ("inherit the Calendar's colour", ADR-0043). The result is already
// NormalizeColor's canonical "#RRGGBBAA" form, the same value space a local
// recolour produces, so the shadow comparison in reconcile.go is a plain
// equality.
func googleEventColorHex(colorID string) *string {
	hex, ok := googleEventColors[colorID]
	if !ok {
		return nil
	}
	normalized, ok := NormalizeColor(hex)
	if !ok {
		return nil
	}
	return &normalized
}
