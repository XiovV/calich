import { describe, expect, it } from "vitest";
import { isBrokenSubscription, isSubscribedCalendar, type Calendar } from "./calendar";

function makeCalendar(overrides: Partial<Calendar> = {}): Calendar {
  return { id: "cal-1", name: "Personal", color: "#12809CFF", ...overrides };
}

describe("isSubscribedCalendar", () => {
  it("is false for an ordinary Calendar with no sourceUrl", () => {
    expect(isSubscribedCalendar(makeCalendar())).toBe(false);
  });

  it("is true for a Calendar carrying a sourceUrl", () => {
    expect(
      isSubscribedCalendar(makeCalendar({ sourceUrl: "https://example.com/feed.ics" })),
    ).toBe(true);
  });
});

describe("isBrokenSubscription", () => {
  it("is false for a healthy Subscription", () => {
    expect(
      isBrokenSubscription(makeCalendar({ sourceUrl: "https://example.com/feed.ics" })),
    ).toBe(false);
  });

  it("is true when errorClass is needs_attention", () => {
    expect(
      isBrokenSubscription(
        makeCalendar({
          sourceUrl: "https://example.com/feed.ics",
          errorClass: "needs_attention",
          errorMessage: "the calendar feed rejected the credentials",
        }),
      ),
    ).toBe(true);
  });

  it("is true when errorClass is retrying", () => {
    expect(
      isBrokenSubscription(
        makeCalendar({
          sourceUrl: "https://example.com/feed.ics",
          errorClass: "retrying",
          errorMessage: "failed to fetch the calendar feed",
        }),
      ),
    ).toBe(true);
  });

  it("is false for undefined", () => {
    expect(isBrokenSubscription(undefined)).toBe(false);
  });
});
