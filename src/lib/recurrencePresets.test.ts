import { describe, expect, it } from "vitest";
import {
  buildRule,
  presetFromRule,
  presetLabel,
  reanchorRule,
  type RecurrencePreset,
} from "./recurrencePresets";

// 2026-08-18 is a Tuesday and the third Tuesday of August 2026.
const TUESDAY = new Date(2026, 7, 18, 9, 0);
// 2026-08-19 is a Wednesday.
const WEDNESDAY = new Date(2026, 7, 19, 9, 0);

describe("buildRule", () => {
  it("does not repeat for the 'none' preset", () => {
    expect(buildRule("none", TUESDAY)).toBeUndefined();
  });

  it("builds a daily rule", () => {
    expect(buildRule("daily", TUESDAY)).toBe("FREQ=DAILY");
  });

  it("builds a weekly rule from the start's weekday", () => {
    expect(buildRule("weekly", TUESDAY)).toBe("FREQ=WEEKLY;BYDAY=TU");
    expect(buildRule("weekly", WEDNESDAY)).toBe("FREQ=WEEKLY;BYDAY=WE");
  });

  it("builds a monthly rule as the nth weekday of the month", () => {
    expect(buildRule("monthly", TUESDAY)).toBe("FREQ=MONTHLY;BYDAY=+3TU");
  });

  it("builds a yearly rule anchored by the start date", () => {
    expect(buildRule("yearly", TUESDAY)).toBe("FREQ=YEARLY");
  });

  it("builds an every-weekday rule regardless of the start's weekday", () => {
    expect(buildRule("weekday", TUESDAY)).toBe("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR");
  });
});

describe("presetLabel", () => {
  it("regenerates the weekly label from the selected start date", () => {
    expect(presetLabel("weekly", TUESDAY)).toBe("Weekly on Tuesday");
    expect(presetLabel("weekly", WEDNESDAY)).toBe("Weekly on Wednesday");
  });

  it("labels the monthly preset with the nth weekday", () => {
    expect(presetLabel("monthly", TUESDAY)).toBe("Monthly on the third Tuesday");
  });

  it("labels the yearly preset with the start's month and day", () => {
    expect(presetLabel("yearly", TUESDAY)).toBe("Annually on August 18");
  });

  it("labels the fixed presets", () => {
    expect(presetLabel("none", TUESDAY)).toBe("Does not repeat");
    expect(presetLabel("daily", TUESDAY)).toBe("Daily");
    expect(presetLabel("weekday", TUESDAY)).toBe(
      "Every weekday (Monday to Friday)",
    );
  });
});

describe("presetFromRule", () => {
  it("round-trips every preset's rule back to its preset", () => {
    const presets: RecurrencePreset[] = [
      "none",
      "daily",
      "weekly",
      "monthly",
      "yearly",
      "weekday",
    ];
    for (const preset of presets) {
      expect(presetFromRule(buildRule(preset, TUESDAY))).toBe(preset);
    }
  });

  it("reads an absent rule as 'none'", () => {
    expect(presetFromRule(undefined)).toBe("none");
    expect(presetFromRule("")).toBe("none");
  });

  it("distinguishes every-weekday from a single weekly day", () => {
    expect(presetFromRule("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR")).toBe("weekday");
    expect(presetFromRule("FREQ=WEEKLY;BYDAY=TU")).toBe("weekly");
  });
});

describe("reanchorRule", () => {
  it("moves a weekly series' weekday to the new start's weekday", () => {
    expect(reanchorRule("FREQ=WEEKLY;BYDAY=TU", WEDNESDAY)).toBe(
      "FREQ=WEEKLY;BYDAY=WE",
    );
  });

  it("re-derives a monthly series' nth weekday from the new start", () => {
    // 2026-08-05 is the first Wednesday of the month.
    const firstWednesday = new Date(2026, 7, 5, 9, 0);
    expect(reanchorRule("FREQ=MONTHLY;BYDAY=+3TU", firstWednesday)).toBe(
      "FREQ=MONTHLY;BYDAY=+1WE",
    );
  });

  it("leaves the every-weekday preset unchanged", () => {
    expect(reanchorRule("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", WEDNESDAY)).toBe(
      "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
    );
  });

  it("returns undefined for a non-recurring event", () => {
    expect(reanchorRule(undefined, WEDNESDAY)).toBeUndefined();
  });
});
