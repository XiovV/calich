package service

import "strings"

// NormalizeColor validates an arbitrary Calendar color and returns its
// canonical form: uppercase "#RRGGBBAA" (ADR-0029). "#RGB" and "#RRGGBB"
// widen with an "FF" (fully opaque) alpha; "#RRGGBBAA" is kept as given,
// alpha included, so a native client's exact string round-trips
// byte-identical up to case. ok is false for anything else — missing "#",
// any other digit count, or non-hex digits.
func NormalizeColor(input string) (hex string, ok bool) {
	s := strings.TrimSpace(input)
	if len(s) == 0 || s[0] != '#' {
		return "", false
	}
	digits := s[1:]

	var rgb, alpha string
	switch len(digits) {
	case 3:
		rgb = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
		alpha = "ff"
	case 6:
		rgb = digits
		alpha = "ff"
	case 8:
		rgb = digits[:6]
		alpha = digits[6:8]
	default:
		return "", false
	}

	if !isHex(rgb) || !isHex(alpha) {
		return "", false
	}

	return "#" + strings.ToUpper(rgb+alpha), true
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
