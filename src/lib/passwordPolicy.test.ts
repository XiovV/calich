import { describe, expect, it } from "vitest";
import { hasMinPasswordLength, MIN_PASSWORD_LENGTH } from "./passwordPolicy";

describe("hasMinPasswordLength (#255)", () => {
  it("rejects a password shorter than the minimum", () => {
    expect(hasMinPasswordLength("a".repeat(MIN_PASSWORD_LENGTH - 1))).toBe(false);
  });

  it("accepts a password exactly at the minimum", () => {
    expect(hasMinPasswordLength("a".repeat(MIN_PASSWORD_LENGTH))).toBe(true);
  });

  it("accepts a password longer than the minimum", () => {
    expect(hasMinPasswordLength("a".repeat(MIN_PASSWORD_LENGTH + 5))).toBe(true);
  });

  // Astral-plane emoji are a surrogate pair each — two UTF-16 code units per
  // visible character. .length would count 4 emoji as 8 and wrongly pass a
  // password the server (which counts runes) rejects as too short.
  it("counts an astral-plane emoji as one character, not two", () => {
    const fourEmoji = "😀".repeat(4);
    expect(fourEmoji).toHaveLength(8);
    expect(hasMinPasswordLength(fourEmoji)).toBe(false);
  });
});
