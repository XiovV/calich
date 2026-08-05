import { describe, expect, it } from "vitest";
import { errorMessage } from "./errorMessage";

describe("errorMessage", () => {
  it("uses an Error's own message", () => {
    expect(errorMessage(new Error("Invalid credentials."))).toBe(
      "Invalid credentials.",
    );
  });

  it("uses the message of an Error subclass", () => {
    class ApiError extends Error {}
    expect(errorMessage(new ApiError("Calendar not found."))).toBe(
      "Calendar not found.",
    );
  });

  it("falls back for a thrown non-Error", () => {
    expect(errorMessage("boom")).toBe("Something went wrong.");
    expect(errorMessage({ message: "boom" })).toBe("Something went wrong.");
    expect(errorMessage(undefined)).toBe("Something went wrong.");
    expect(errorMessage(null)).toBe("Something went wrong.");
  });

  // An Error carrying no message passes straight through, rendering as blank
  // text rather than the fallback. Pinned as current behaviour, not endorsed —
  // logged as an off-scope finding.
  it("passes an empty Error message through unchanged", () => {
    expect(errorMessage(new Error(""))).toBe("");
  });
});
