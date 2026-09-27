import { describe, expect, it } from "vitest";
import { daysWithSlots, formatSlotTime, slotsForDay, zonedDayKey } from "./publicBookingSlots";

describe("zonedDayKey", () => {
  it("keys an instant by its calendar date in the given zone", () => {
    // 2026-08-20 23:30 UTC is still 2026-08-20 in New York (UTC-4) but
    // already 2026-08-21 in Berlin (UTC+2) — the whole reason this can't be
    // a bare date-fns format() over the raw Date.
    const instant = new Date("2026-08-20T23:30:00Z");
    expect(zonedDayKey(instant, "America/New_York")).toBe("2026-08-20");
    expect(zonedDayKey(instant, "Europe/Berlin")).toBe("2026-08-21");
  });
});

describe("daysWithSlots", () => {
  it("collapses multiple slots on the same zoned day into one key", () => {
    const slots = [new Date("2026-08-20T13:00:00Z"), new Date("2026-08-20T14:00:00Z"), new Date("2026-08-21T13:00:00Z")];
    expect(daysWithSlots(slots, "Etc/UTC")).toEqual(new Set(["2026-08-20", "2026-08-21"]));
  });

  it("marks a day by its local date in zone, not the slot's UTC date", () => {
    // 22:00 UTC on the 20th is already the 21st in Tokyo (UTC+9).
    const slots = [new Date("2026-08-20T22:00:00Z")];
    expect(daysWithSlots(slots, "Asia/Tokyo")).toEqual(new Set(["2026-08-21"]));
  });
});

describe("slotsForDay", () => {
  const slots = [
    new Date("2026-08-20T09:00:00Z"),
    new Date("2026-08-20T13:00:00Z"),
    new Date("2026-08-21T09:00:00Z"),
  ];

  it("returns only the slots on day, sorted chronologically", () => {
    const day = new Date("2026-08-20T00:00:00Z");
    const result = slotsForDay(slots, day, "Etc/UTC");
    expect(result).toEqual([slots[0], slots[1]]);
  });

  it("compares day in the given zone rather than UTC", () => {
    // 09:00 UTC on the 20th is already 2026-08-20 18:00 in Tokyo — still the
    // 20th there too, but 13:00 UTC is 22:00 Tokyo, still the 20th; the
    // 21st's 09:00 UTC slot is 18:00 Tokyo on the 21st, so it's excluded.
    const day = new Date("2026-08-20T00:00:00Z");
    const result = slotsForDay(slots, day, "Asia/Tokyo");
    expect(result).toEqual([slots[0], slots[1]]);
  });
});

describe("formatSlotTime", () => {
  const slot = new Date("2026-08-20T18:47:00Z");

  it("formats in zone rather than UTC, 12-hour", () => {
    expect(formatSlotTime(slot, "America/New_York", "12h")).toBe("2:47 PM");
  });

  it("formats in zone rather than UTC, 24-hour", () => {
    expect(formatSlotTime(slot, "Europe/Berlin", "24h")).toBe("20:47");
  });
});
