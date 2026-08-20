import { describe, expect, it } from "vitest";
import { formatDateTime, timePattern } from "./timeFormat";

describe("timePattern", () => {
  it("returns the 12-hour date-fns pattern for 12h", () => {
    expect(timePattern("12h")).toBe("h:mm a");
  });

  it("returns the 24-hour date-fns pattern for 24h", () => {
    expect(timePattern("24h")).toBe("HH:mm");
  });
});

// #236: the Notification feed renders occurrenceStart through this rather
// than Date.toLocaleString(), which fell back to the browser locale (a
// DD/MM/YYYY date, 24-hour with seconds) regardless of the User's Preference.
describe("formatDateTime", () => {
  const moment = new Date(2026, 7, 20, 18, 47);

  it("renders a 12-hour time with the app's short date, no seconds", () => {
    expect(formatDateTime(moment, "12h")).toBe("Aug 20, 2026, 6:47 PM");
  });

  it("renders a 24-hour time with the app's short date, no seconds", () => {
    expect(formatDateTime(moment, "24h")).toBe("Aug 20, 2026, 18:47");
  });
});
