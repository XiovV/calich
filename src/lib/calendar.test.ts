import { describe, expect, it } from "vitest";
import {
  calendarHasOtherRecipients,
  calendarReadOnlyReason,
  canManageCalendar,
  canWriteCalendarEvents,
  defaultCalendarId,
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

// The personal Reminder override control (#117) renders only when more than
// one person would be notified — an Owner's own single-user Calendar must
// never show it, since the setting couldn't mean anything there.
describe("calendarHasOtherRecipients", () => {
  const cases: { name: string; overrides: Partial<Calendar>; expected: boolean }[] = [
    { name: "owned, no Shares", overrides: { isOwner: true, shareCount: 0 }, expected: false },
    { name: "owned, with a Share", overrides: { isOwner: true, shareCount: 1 }, expected: true },
    {
      name: "shared with me as Viewer",
      overrides: { isOwner: false, access: "viewer" },
      expected: true,
    },
    {
      name: "shared with me as Editor",
      overrides: { isOwner: false, access: "editor" },
      expected: true,
    },
    {
      name: "the Owner of a Subscribed Calendar with no Shares",
      overrides: { isOwner: true, access: "viewer", sourceUrl: "https://example.com/feed.ics" },
      expected: false,
    },
  ];

  for (const tc of cases) {
    it(`${tc.name}: ${tc.expected}`, () => {
      expect(calendarHasOtherRecipients(makeCalendar(tc.overrides))).toBe(tc.expected);
    });
  }

  it("is false for undefined", () => {
    expect(calendarHasOtherRecipients(undefined)).toBe(false);
  });
});

// A new Event must default onto a Calendar the caller owns rather than
// whichever writable Calendar happens to sort first, which may now be a
// Calendar someone else shared with Editor Access (#115).
describe("defaultCalendarId", () => {
  it("picks the owned calendar even when it sorts after a shared one", () => {
    const shared = makeCalendar({ id: "cal-shared", isOwner: false, access: "editor" });
    const owned = makeCalendar({ id: "cal-owned", isOwner: true, access: "owner" });

    expect(defaultCalendarId([shared, owned])).toBe("cal-owned");
  });

  it("falls back to the first calendar when none is owned", () => {
    const first = makeCalendar({ id: "cal-1", isOwner: false, access: "editor" });
    const second = makeCalendar({ id: "cal-2", isOwner: false, access: "editor" });

    expect(defaultCalendarId([first, second])).toBe("cal-1");
  });

  it("returns an empty string for an empty list", () => {
    expect(defaultCalendarId([])).toBe("");
  });
});
