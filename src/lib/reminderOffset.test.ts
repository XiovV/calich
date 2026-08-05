import { describe, expect, it } from "vitest";
import { normalizeCustomOffset, reminderChannelOptions, splitCustomOffset } from "./reminderOffset";

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
