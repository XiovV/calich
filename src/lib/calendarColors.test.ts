import { describe, expect, it } from "vitest";
import {
  SWATCHES,
  getContrastTextColor,
  getNextUnusedColor,
  normalizeCalendarColor,
  resolveOccurrenceColor,
} from "./calendarColors";

describe("getNextUnusedColor", () => {
  it("returns the first swatch when none are used", () => {
    expect(getNextUnusedColor([])).toBe(SWATCHES[0]);
  });

  it("skips swatches already in use", () => {
    expect(getNextUnusedColor([SWATCHES[0], SWATCHES[1]])).toBe(SWATCHES[2]);
  });

  it("skips used swatches even when not given in palette order", () => {
    expect(getNextUnusedColor([SWATCHES[2], SWATCHES[0]])).toBe(SWATCHES[1]);
  });

  it("falls back to the first swatch when all are used", () => {
    expect(getNextUnusedColor([...SWATCHES])).toBe(SWATCHES[0]);
  });

  it("treats a Calendar's color matching no Swatch as simply unmatched, not an error", () => {
    expect(getNextUnusedColor(["#123456FF"])).toBe(SWATCHES[0]);
  });
});

describe("normalizeCalendarColor", () => {
  it("widens a 3-digit hex with FF alpha", () => {
    expect(normalizeCalendarColor("#f00")).toBe("#FF0000FF");
  });

  it("widens a 6-digit hex with FF alpha", () => {
    expect(normalizeCalendarColor("#12809c")).toBe("#12809CFF");
  });

  it("keeps an 8-digit hex's own alpha", () => {
    expect(normalizeCalendarColor("#12809c80")).toBe("#12809C80");
  });

  it("uppercases an already-canonical hex", () => {
    expect(normalizeCalendarColor("#12809cff")).toBe("#12809CFF");
  });

  it("trims surrounding whitespace", () => {
    expect(normalizeCalendarColor("  #12809c  ")).toBe("#12809CFF");
  });

  it.each([
    "12809c", // missing "#"
    "#12809", // odd digit count (5)
    "#1280", // wrong length (4)
    "#12809cf", // wrong length (7)
    "#gg0000", // non-hex chars
    "",
    "peacock", // the old enum member is no longer a valid color
  ])("rejects %s", (input) => {
    expect(normalizeCalendarColor(input)).toBeNull();
  });
});

describe("getContrastTextColor", () => {
  it("picks white text on a dark fill", () => {
    expect(getContrastTextColor("#000000")).toBe("#FFFFFF");
  });

  it("picks black text on a light fill", () => {
    expect(getContrastTextColor("#FFFFFF")).toBe("#000000");
  });

  it("picks white text on a saturated, clearly dark blue", () => {
    expect(getContrastTextColor("#0A2540")).toBe("#FFFFFF");
  });

  it("picks black text on a bright swatch-adjacent yellow", () => {
    expect(getContrastTextColor("#FFEE00")).toBe("#000000");
  });
});

// ADR-0043's ResolveColor: an Event's own color wins outright over its
// Calendar's when set; the Calendar's color is consulted only when absent.
describe("resolveOccurrenceColor", () => {
  it("wins outright with the Event's own color when set", () => {
    expect(resolveOccurrenceColor({ color: "#FF6B35FF" }, { color: "#12809CFF" })).toBe(
      "#FF6B35FF",
    );
  });

  it("falls through to the Calendar's (already per-viewer resolved) color when absent", () => {
    expect(resolveOccurrenceColor({}, { color: "#12809CFF" })).toBe("#12809CFF");
  });

  it("falls through to the unresolved-Calendar fallback when both are absent", () => {
    expect(resolveOccurrenceColor({}, undefined)).toBe("#6B7280FF");
  });
});
