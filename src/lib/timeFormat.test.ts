import { afterEach, describe, expect, it, vi } from "vitest";
import { detectBrowserTimeFormat, formatDateTime, timePattern } from "./timeFormat";

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

// #324: the public Booking Link page has no Session and so no Preference to
// read a Time format from — it seeds from the browser's own locale instead,
// the same way timezones.ts' detectBrowserTimeZone seeds the viewer zone.
describe("detectBrowserTimeFormat", () => {
  const originalDateTimeFormat = Intl.DateTimeFormat;
  afterEach(() => {
    Intl.DateTimeFormat = originalDateTimeFormat;
  });

  function mockHourCycle(hourCycle: string) {
    vi.spyOn(Intl, "DateTimeFormat").mockImplementation(function () {
      return { resolvedOptions: () => ({ hourCycle }) } as unknown as Intl.DateTimeFormat;
    });
  }

  it("returns 24h when the locale's resolved hourCycle is h23", () => {
    mockHourCycle("h23");
    expect(detectBrowserTimeFormat()).toBe("24h");
  });

  it("returns 12h when the locale's resolved hourCycle is h12", () => {
    mockHourCycle("h12");
    expect(detectBrowserTimeFormat()).toBe("12h");
  });

  it("falls back to 12h when Intl throws", () => {
    vi.spyOn(Intl, "DateTimeFormat").mockImplementation(() => {
      throw new Error("unsupported");
    });
    expect(detectBrowserTimeFormat()).toBe("12h");
  });
});
