import { describe, expect, it } from "vitest";
import { timePattern } from "./timeFormat";

describe("timePattern", () => {
  it("returns the 12-hour date-fns pattern for 12h", () => {
    expect(timePattern("12h")).toBe("h:mm a");
  });

  it("returns the 24-hour date-fns pattern for 24h", () => {
    expect(timePattern("24h")).toBe("HH:mm");
  });
});
