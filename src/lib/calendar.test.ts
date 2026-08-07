import { describe, expect, it } from "vitest";
import {
  calendarReadOnlyReason,
  canManageCalendar,
  canWriteCalendarEvents,
  isBrokenSubscription,
  isSubscribedCalendar,
  type Calendar,
} from "./calendar";

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

// The permission seam (#111, ADR-0034): a table over Access x ownership x
// Subscription presence, since these three predicates are the only place
// the web app expresses permission.
describe("the permission predicates", () => {
  const subscribed = { sourceUrl: "https://example.com/feed.ics" };

  const cases: Array<{
    name: string;
    overrides: Partial<Calendar>;
    canWrite: boolean;
    canManage: boolean;
    reason: "subscription" | "viewer" | undefined;
  }> = [
    {
      name: "owner of an ordinary calendar",
      overrides: { access: "owner", isOwner: true },
      canWrite: true,
      canManage: true,
      reason: undefined,
    },
    {
      name: "editor share on an ordinary calendar",
      overrides: { access: "editor", isOwner: false },
      canWrite: true,
      canManage: false,
      reason: undefined,
    },
    {
      name: "viewer share on an ordinary calendar",
      overrides: { access: "viewer", isOwner: false },
      canWrite: false,
      canManage: false,
      reason: "viewer",
    },
    {
      // The trap this predicate split exists to avoid: a Subscription
      // clamps the Owner's Access to "viewer", but management must survive.
      name: "owner of a subscribed calendar",
      overrides: { access: "viewer", isOwner: true, ...subscribed },
      canWrite: false,
      canManage: true,
      reason: "subscription",
    },
    {
      name: "editor share on a subscribed calendar",
      overrides: { access: "viewer", isOwner: false, ...subscribed },
      canWrite: false,
      canManage: false,
      reason: "subscription",
    },
    {
      name: "viewer share on a subscribed calendar",
      overrides: { access: "viewer", isOwner: false, ...subscribed },
      canWrite: false,
      canManage: false,
      reason: "subscription",
    },
    {
      name: "no access at all",
      overrides: { access: undefined, isOwner: false },
      canWrite: false,
      canManage: false,
      reason: "viewer",
    },
  ];

  for (const tc of cases) {
    it(`${tc.name}: write=${tc.canWrite}, manage=${tc.canManage}, reason=${tc.reason}`, () => {
      const calendar = makeCalendar(tc.overrides);
      expect(canWriteCalendarEvents(calendar)).toBe(tc.canWrite);
      expect(canManageCalendar(calendar)).toBe(tc.canManage);
      expect(calendarReadOnlyReason(calendar)).toBe(tc.reason);
    });
  }

  it("canWriteCalendarEvents is false for undefined", () => {
    expect(canWriteCalendarEvents(undefined)).toBe(false);
  });

  it("canManageCalendar is false for undefined", () => {
    expect(canManageCalendar(undefined)).toBe(false);
  });
});
