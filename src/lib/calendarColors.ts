// A Calendar color is an arbitrary sRGB value, not a choice from a fixed
// set (ADR-0029). Canonical form is uppercase "#RRGGBBAA". Swatches below
// are one-click quick picks, not a constraint on what a color may be — this
// list is allowed to drift from the backend's seed hexes
// (service.defaultCalendars in calendar.go).
export const SWATCHES: readonly string[] = [
  "#E2483DFF", // tomato
  "#E67C9AFF", // flamingo
  "#E4C441FF", // banana
  "#6B9071FF", // sage
  "#12809CFF", // peacock
  "#3F51B5FF", // blueberry
  "#8E44ADFF", // grape
  "#6B7280FF", // graphite
];

// The fallback for an Event whose Calendar can't be resolved — the former
// "graphite" enum member's hex, now a literal constant (ADR-0029).
const UNRESOLVED_CALENDAR_COLOR = "#6B7280FF";

// normalizeCalendarColor validates an arbitrary Calendar color and returns
// its canonical form: uppercase "#RRGGBBAA". "#RGB" and "#RRGGBB" widen
// with an "FF" (fully opaque) alpha; "#RRGGBBAA" is kept as given, alpha
// included. Returns null for anything else — missing "#", any other digit
// count, or non-hex digits. Mirrors the backend's service.NormalizeColor.
export function normalizeCalendarColor(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed.startsWith("#")) return null;
  const digits = trimmed.slice(1);

  let rgb: string;
  let alpha: string;
  if (digits.length === 3) {
    rgb = digits
      .split("")
      .map((c) => c + c)
      .join("");
    alpha = "FF";
  } else if (digits.length === 6) {
    rgb = digits;
    alpha = "FF";
  } else if (digits.length === 8) {
    rgb = digits.slice(0, 6);
    alpha = digits.slice(6, 8);
  } else {
    return null;
  }

  if (!/^[0-9a-fA-F]{6}$/.test(rgb) || !/^[0-9a-fA-F]{2}$/.test(alpha)) return null;
  return `#${(rgb + alpha).toUpperCase()}`;
}

export function getNextUnusedColor(usedColors: string[]): string {
  const unused = SWATCHES.find((swatch) => !usedColors.includes(swatch));
  return unused ?? SWATCHES[0];
}

// toOpaqueHex strips a canonical color's alpha channel for rendering: alpha
// is carried for fidelity but deliberately not honoured at render — a
// translucent fill would blend into the surface and undo the contrast
// calculation below (ADR-0029).
export function toOpaqueHex(hex: string): string {
  return hex.slice(0, 7);
}

function relativeLuminance(opaqueHex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(opaqueHex.slice(i, i + 2), 16) / 255);
  const linear = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
}

// contrastRatio is the WCAG formula: (lighter + 0.05) / (darker + 0.05).
function contrastRatio(luminanceA: number, luminanceB: number): number {
  const lighter = Math.max(luminanceA, luminanceB);
  const darker = Math.min(luminanceA, luminanceB);
  return (lighter + 0.05) / (darker + 0.05);
}

// getContrastTextColor picks whichever of black/white contrasts more with
// opaqueHex by WCAG relative luminance — never a color other than the fill
// actually chosen. A mid-tone gray lands under WCAG AA against both; that's
// accepted (ADR-0029) rather than rendering a color the user didn't pick.
export function getContrastTextColor(opaqueHex: string): "#000000" | "#FFFFFF" {
  const luminance = relativeLuminance(opaqueHex);
  return contrastRatio(luminance, 0) > contrastRatio(luminance, 1) ? "#000000" : "#FFFFFF";
}

export interface CalendarBlockStyle {
  backgroundColor: string;
  color: string;
  border: string;
}

// A low-alpha border so a pale fill keeps its edge against bg-surface in
// light theme (ADR-0029) — a fixed value, not derived from the fill, so it
// stays subtle regardless of what color it outlines.
const CALENDAR_BLOCK_BORDER = "1px solid rgba(0, 0, 0, 0.08)";

export function resolveCalendarFill(calendar: { color: string } | undefined): string {
  return calendar?.color ?? UNRESOLVED_CALENDAR_COLOR;
}

// getCalendarBlockStyle is the single seam every event-rendering surface
// (timed blocks, the all-day lane, month day cells, drag previews, the
// sidebar Calendar list) goes through for a Calendar's rendered appearance:
// the stored fill (opaque), a computed contrasting text color, and a
// low-alpha border. An arbitrary hex can't be a Tailwind utility class, so
// this is applied as inline style rather than a class.
export function getCalendarBlockStyle(
  calendar: { color: string } | undefined,
): CalendarBlockStyle {
  const backgroundColor = toOpaqueHex(resolveCalendarFill(calendar));
  return {
    backgroundColor,
    color: getContrastTextColor(backgroundColor),
    border: CALENDAR_BLOCK_BORDER,
  };
}
