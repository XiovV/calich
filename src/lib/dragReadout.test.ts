import { describe, expect, it } from "vitest";
import { formatDragDuration, formatDragReadout } from "./dragReadout";

describe("formatDragDuration", () => {
  it("formats sub-hour durations", () => {
    expect(formatDragDuration(15)).toBe("15m");
    expect(formatDragDuration(45)).toBe("45m");
  });

  it("formats an exact hour with no minutes part", () => {
    expect(formatDragDuration(60)).toBe("1h");
  });

  it("formats hour-plus-minutes", () => {
    expect(formatDragDuration(90)).toBe("1h 30m");
    expect(formatDragDuration(135)).toBe("2h 15m");
  });

  it("formats multi-hour durations with no minutes part", () => {
    expect(formatDragDuration(480)).toBe("8h");
  });
});

describe("formatDragReadout", () => {
  it("joins the formatted start, end and duration", () => {
    const start = new Date(2026, 0, 1, 9, 15);
    const end = new Date(2026, 0, 1, 9, 45);
    expect(formatDragReadout(start, end, "HH:mm")).toBe("09:15 – 09:45 · 30m");
  });

  it("honours the given time pattern", () => {
    const start = new Date(2026, 0, 1, 9, 15);
    const end = new Date(2026, 0, 1, 10, 45);
    expect(formatDragReadout(start, end, "h:mm a")).toBe(
      "9:15 AM – 10:45 AM · 1h 30m",
    );
  });

  it("formats an exact-hour duration", () => {
    const start = new Date(2026, 0, 1, 14, 0);
    const end = new Date(2026, 0, 1, 15, 0);
    expect(formatDragReadout(start, end, "HH:mm")).toBe("14:00 – 15:00 · 1h");
  });
});
