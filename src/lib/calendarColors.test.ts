import { describe, expect, it } from "vitest";
import { getNextUnusedColor, type CalendarColor } from "./calendarColors";

describe("getNextUnusedColor", () => {
  it("returns the first palette color when none are used", () => {
    expect(getNextUnusedColor([])).toBe("tomato");
  });

  it("skips colors already in use", () => {
    expect(getNextUnusedColor(["tomato", "flamingo"])).toBe("banana");
  });

  it("skips used colors even when not given in palette order", () => {
    expect(getNextUnusedColor(["banana", "tomato"])).toBe("flamingo");
  });

  it("falls back to the first palette color when all colors are used", () => {
    const allColors: CalendarColor[] = [
      "tomato",
      "flamingo",
      "banana",
      "sage",
      "peacock",
      "blueberry",
      "grape",
      "graphite",
    ];
    expect(getNextUnusedColor(allColors)).toBe("tomato");
  });
});
