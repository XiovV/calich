import { describe, expect, it } from "vitest";
import {
  matchPreset,
  normalizeCustomOffset,
  presetOffsetMinutes,
  reminderChannelOptions,
  reminderPresetLabel,
  reminderPresets,
  splitCustomOffset,
  type ReminderPreset,
} from "./reminderPresets";

describe("reminderPresets", () => {
  it("offers the minute-granular set for timed events", () => {
    expect(reminderPresets(false)).toEqual([
      "at-start",
      "5-minutes",
      "10-minutes",
      "15-minutes",
      "30-minutes",
      "1-hour",
      "1-day",
      "1-week",
    ]);
  });

  it("offers the day-granular, 09:00-anchored set for all-day events", () => {
    expect(reminderPresets(true)).toEqual(["at-start", "1-day", "2-days", "1-week"]);
  });
});

describe("reminderChannelOptions", () => {
  it("offers only Notification when email is unavailable", () => {
    expect(reminderChannelOptions(false)).toEqual([
      { value: "notification", label: "Notification" },
    ]);
  });

  it("offers Notification and Email when email is available", () => {
    expect(reminderChannelOptions(true)).toEqual([
      { value: "notification", label: "Notification" },
      { value: "email", label: "Email" },
    ]);
  });
});

describe("presetOffsetMinutes", () => {
  it("maps each preset to its offset in minutes", () => {
    expect(presetOffsetMinutes("at-start")).toBe(0);
    expect(presetOffsetMinutes("5-minutes")).toBe(5);
    expect(presetOffsetMinutes("10-minutes")).toBe(10);
    expect(presetOffsetMinutes("15-minutes")).toBe(15);
    expect(presetOffsetMinutes("30-minutes")).toBe(30);
    expect(presetOffsetMinutes("1-hour")).toBe(60);
    expect(presetOffsetMinutes("1-day")).toBe(1440);
    expect(presetOffsetMinutes("2-days")).toBe(2880);
    expect(presetOffsetMinutes("1-week")).toBe(10080);
  });

  it("stores the same offset regardless of allDay — only labels/sets differ", () => {
    expect(presetOffsetMinutes("1-day")).toBe(1440);
  });
});

describe("reminderPresetLabel", () => {
  it("labels timed presets relative to the event start", () => {
    expect(reminderPresetLabel("at-start", false)).toBe("At time of event");
    expect(reminderPresetLabel("10-minutes", false)).toBe("10 minutes before");
    expect(reminderPresetLabel("1-hour", false)).toBe("1 hour before");
    expect(reminderPresetLabel("1-week", false)).toBe("1 week before");
  });

  it("labels all-day presets naming the 09:00 anchor", () => {
    expect(reminderPresetLabel("at-start", true)).toBe("On the day, 9:00 AM");
    expect(reminderPresetLabel("1-day", true)).toBe("1 day before, 9:00 AM");
    expect(reminderPresetLabel("2-days", true)).toBe("2 days before, 9:00 AM");
    expect(reminderPresetLabel("1-week", true)).toBe("1 week before, 9:00 AM");
  });
});

describe("matchPreset", () => {
  it("round-trips every timed preset's offset back to its preset", () => {
    for (const preset of reminderPresets(false)) {
      expect(matchPreset(presetOffsetMinutes(preset), false)).toBe(preset);
    }
  });

  it("round-trips every all-day preset's offset back to its preset", () => {
    for (const preset of reminderPresets(true)) {
      expect(matchPreset(presetOffsetMinutes(preset), true)).toBe(preset);
    }
  });

  it("returns null for an offset outside the offered set — a custom offset", () => {
    expect(matchPreset(20, false)).toBeNull();
    expect(matchPreset(5, true)).toBeNull();
  });

  it("distinguishes timed and all-day sets for the same offset", () => {
    // 5 minutes is a timed preset but not an all-day one.
    expect(matchPreset(5, false)).toBe("5-minutes" as ReminderPreset);
    expect(matchPreset(5, true)).toBeNull();
  });
});

describe("normalizeCustomOffset", () => {
  it("converts an amount+unit into minutes", () => {
    expect(normalizeCustomOffset(45, "minutes")).toBe(45);
    expect(normalizeCustomOffset(3, "hours")).toBe(180);
    expect(normalizeCustomOffset(2, "days")).toBe(2880);
    expect(normalizeCustomOffset(1, "weeks")).toBe(10080);
  });

  it("clamps negative or fractional amounts to a non-negative whole number", () => {
    expect(normalizeCustomOffset(-5, "minutes")).toBe(0);
    expect(normalizeCustomOffset(1.7, "minutes")).toBe(2);
  });
});

describe("splitCustomOffset", () => {
  it("picks the largest unit that divides the offset evenly", () => {
    expect(splitCustomOffset(10080)).toEqual({ amount: 1, unit: "weeks" });
    expect(splitCustomOffset(2880)).toEqual({ amount: 2, unit: "days" });
    expect(splitCustomOffset(180)).toEqual({ amount: 3, unit: "hours" });
    expect(splitCustomOffset(45)).toEqual({ amount: 45, unit: "minutes" });
  });

  it("falls back to minutes for zero", () => {
    expect(splitCustomOffset(0)).toEqual({ amount: 0, unit: "minutes" });
  });
});
