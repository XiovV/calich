import { describe, expect, it } from "vitest";
import { isValidEmail } from "./isValidEmail";

describe("isValidEmail", () => {
  it("returns true for a well-formed email", () => {
    expect(isValidEmail("test@example.com")).toBe(true);
  });

  it("returns false for an empty string", () => {
    expect(isValidEmail("")).toBe(false);
  });

  it("returns false when missing an @", () => {
    expect(isValidEmail("testexample.com")).toBe(false);
  });

  it("returns false when missing a domain", () => {
    expect(isValidEmail("test@")).toBe(false);
  });

  it("returns false when missing a local part", () => {
    expect(isValidEmail("@example.com")).toBe(false);
  });

  it("returns false when the domain has no dot", () => {
    expect(isValidEmail("test@example")).toBe(false);
  });

  it("returns false for a string containing spaces", () => {
    expect(isValidEmail("test @example.com")).toBe(false);
  });
});
