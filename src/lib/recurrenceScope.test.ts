import { RRule } from "rrule";
import { fromZonedTime } from "date-fns-tz";
import { describe, expect, it } from "vitest";
import type { Event } from "./event";
import { toFloating, viewerZone } from "./floatingTime";
import {
  hasFieldChanges,
  makeException,
  makeOverride,
  remindersEqual,
  resolveColor,
  shouldDiscardChildren,
  splitFollowing,
} from "./recurrenceScope";

const master: Event = {
  id: "master-1",
  calendarId: "cal-1",
  title: "Standup",
  start: new Date(2026, 0, 1, 9, 0),
  end: new Date(2026, 0, 1, 9, 30),
  rrule: "FREQ=DAILY",
};

describe("makeOverride", () => {
  it("inherits the master's Anchor zone unchanged (ADR-0019)", () => {
    const zonedMaster: Event = { ...master, tzid: "Europe/Berlin" };
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-2",
      title: "Standup (moved)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
    };

    expect(makeOverride(zonedMaster, occurrenceStart, changes).tzid).toBe(
      "Europe/Berlin",
    );
  });

  it("inherits the master's color when the edit doesn't touch it (ADR-0043)", () => {
    const coloredMaster: Event = { ...master, color: "#12809CFF" };
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: occurrenceStart,
      end: new Date(2026, 0, 3, 9, 30),
    };

    expect(makeOverride(coloredMaster, occurrenceStart, changes).color).toBe("#12809CFF");
  });

  it("starts a fresh Override's URL as a copy of the master's, independent once set (ADR-0063)", () => {
    const linkedMaster: Event = { ...master, url: "https://master.example/link" };
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: occurrenceStart,
      end: new Date(2026, 0, 3, 9, 30),
    };

    expect(makeOverride(linkedMaster, occurrenceStart, changes).url).toBe(
      "https://master.example/link",
    );
    expect(
      makeOverride(linkedMaster, occurrenceStart, {
        ...changes,
        url: "https://override.example/link",
      }).url,
    ).toBe("https://override.example/link");
  });

  it("keys the override to the parent and the replaced Occurrence's start", () => {
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-2",
      title: "Standup (moved)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
    };

    expect(makeOverride(master, occurrenceStart, changes)).toEqual({
      parentId: master.id,
      recurrenceId: occurrenceStart,
      ...changes,
    });
  });

  it("never carries a rule of its own, even if the caller's changes object has one", () => {
    // The event modal's form state always carries an rrule field (for the
    // "all" scope's use); an Override must never pick it up (ADR-0016) — a
    // non-empty rrule here is what the backend rejects with a 400.
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changesWithRrule = {
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
      rrule: "FREQ=DAILY",
    };

    const override = makeOverride(master, occurrenceStart, changesWithRrule);
    expect(override).not.toHaveProperty("rrule");
  });

  // Reminders no longer flow through makeOverride at all (ADR-0064): the
  // backend copies every User's rows from the Master onto a fresh Override
  // server-side, and the acting User's own edit, if any, travels through
  // eventsApi.setReminders instead of this pure field compiler.
  it("never carries a reminders field", () => {
    const remindedMaster: Event = {
      ...master,
      reminders: [{ offsetMinutes: 10, channel: "notification" }],
    };
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
    };

    expect(makeOverride(remindedMaster, occurrenceStart, changes)).not.toHaveProperty(
      "reminders",
    );
  });
});

describe("makeException", () => {
  it("cancels a single Occurrence, keyed to the parent and its start", () => {
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);

    expect(makeException(master, occurrenceStart)).toEqual({
      parentId: master.id,
      occurrenceStart,
    });
  });
});

describe("splitFollowing", () => {
  it("truncates the old master with UNTIL at the last Occurrence before the split", () => {
    const splitStart = new Date(2026, 0, 3, 9, 0); // the 3rd daily Occurrence
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: splitStart,
      end: new Date(2026, 0, 3, 9, 30),
    };

    const result = splitFollowing(master, splitStart, changes);

    // The last Occurrence before the split is Jan 2 09:00 — UNTIL must be
    // at-or-after it but strictly before Jan 3 09:00, so the split boundary
    // Occurrence is excluded from the old master.
    expect(result.truncatedMaster.rrule).toContain("UNTIL=20260102");
    expect(result.truncatedMaster.rrule).not.toContain("UNTIL=20260103");
  });

  it("truncates a zoned master at its own Anchor-zone wall-clock, across a DST transition, regardless of the ambient zone (ADR-0019)", () => {
    // Daily 10:00 Europe/Berlin, split after EU spring-forward (2026-03-29).
    const zonedMaster: Event = {
      ...master,
      start: fromZonedTime(new Date(2026, 2, 2, 10, 0), "Europe/Berlin"),
      end: fromZonedTime(new Date(2026, 2, 2, 10, 30), "Europe/Berlin"),
      rrule: "FREQ=DAILY",
      tzid: "Europe/Berlin",
    };
    const splitStart = fromZonedTime(new Date(2026, 2, 31, 10, 0), "Europe/Berlin");
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: splitStart,
      end: fromZonedTime(new Date(2026, 2, 31, 10, 30), "Europe/Berlin"),
    };

    const result = splitFollowing(zonedMaster, splitStart, changes);

    // The last Occurrence before the split is Mar 30 10:00 Berlin wall-clock
    // (CEST, post-transition) — the floating-frame UNTIL literal carries
    // that wall-clock verbatim, not a UTC instant.
    expect(result.truncatedMaster.rrule).toContain("UNTIL=20260330T100000Z");
    // The new master carries the old master's Anchor zone unchanged.
    expect(result.newMaster.tzid).toBe("Europe/Berlin");
  });

  it("carries the same recurrence pattern into the new master, anchored at the split", () => {
    const splitStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup (renamed)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
    };

    const result = splitFollowing(master, splitStart, changes);

    expect(result.newMaster).toEqual({
      calendarId: changes.calendarId,
      title: changes.title,
      start: changes.start,
      end: changes.end,
      rrule: master.rrule,
    });
  });

  it("reparents overrides/exceptions at-or-after the split, not the edited start", () => {
    const splitStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: new Date(2026, 0, 3, 14, 0), // edited to a different time of day
      end: new Date(2026, 0, 3, 14, 30),
    };

    const result = splitFollowing(master, splitStart, changes);

    expect(result.reparentFromStart).toEqual(splitStart);
  });

  it("truncates the old master to produce no Occurrences when splitting at the first one", () => {
    const splitStart = master.start; // the very first Occurrence
    const changes = {
      calendarId: master.calendarId,
      title: master.title,
      start: splitStart,
      end: master.end,
    };

    const result = splitFollowing(master, splitStart, changes);

    // UNTIL must land before the master's own start, so it generates nothing.
    const rule = new RRule({
      ...RRule.parseString(result.truncatedMaster.rrule),
      dtstart: toFloating(master.start, viewerZone()),
    });
    expect(rule.all()).toEqual([]);
  });

  it("preserves any UNTIL/COUNT-free preset rule verbatim in the new master", () => {
    const weeklyMaster: Event = { ...master, rrule: "FREQ=WEEKLY;BYDAY=TU,TH" };
    const splitStart = new Date(2026, 0, 6, 9, 0);
    const changes = {
      calendarId: master.calendarId,
      title: master.title,
      start: splitStart,
      end: new Date(2026, 0, 6, 9, 30),
    };

    const result = splitFollowing(weeklyMaster, splitStart, changes);

    expect(result.newMaster.rrule).toBe("FREQ=WEEKLY;BYDAY=TU,TH");
  });

  // The new Master's Reminders are copied from the old Master server-side
  // (ADR-0064, via copyRemindersFrom) — splitFollowing itself never touches
  // them.
  it("never carries a reminders field on the new master", () => {
    const remindedMaster: Event = {
      ...master,
      reminders: [{ offsetMinutes: 10, channel: "notification" }],
    };
    const splitStart = new Date(2026, 0, 3, 9, 0);
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: splitStart,
      end: new Date(2026, 0, 3, 9, 30),
    };

    const result = splitFollowing(remindedMaster, splitStart, changes);

    expect(result.newMaster).not.toHaveProperty("reminders");
  });
});


describe("shouldDiscardChildren", () => {
  it("is false when the rule is unchanged", () => {
    expect(shouldDiscardChildren("FREQ=DAILY", "FREQ=DAILY")).toBe(false);
  });

  it("treats undefined and empty string as the same non-recurring rule", () => {
    expect(shouldDiscardChildren(undefined, "")).toBe(false);
    expect(shouldDiscardChildren("", undefined)).toBe(false);
  });

  it("is true when the rule's pattern changes", () => {
    expect(shouldDiscardChildren("FREQ=DAILY", "FREQ=WEEKLY;BYDAY=TU")).toBe(true);
  });

  it("is true when the rule is removed", () => {
    expect(shouldDiscardChildren("FREQ=DAILY", undefined)).toBe(true);
  });

  it("is true when a rule is added to a previously non-recurring event", () => {
    expect(shouldDiscardChildren(undefined, "FREQ=DAILY")).toBe(true);
  });

  it("ignores a changed end condition (UNTIL/COUNT) when the pattern is the same", () => {
    expect(
      shouldDiscardChildren("FREQ=DAILY", "FREQ=DAILY;UNTIL=20260101T085959Z"),
    ).toBe(false);
    expect(shouldDiscardChildren("FREQ=DAILY;COUNT=5", "FREQ=DAILY")).toBe(false);
  });
});

describe("resolveColor", () => {
  const coloredMaster: Event = { ...master, color: "#12809CFF" };

  it("falls back to the reference's own color when the edit didn't touch it", () => {
    expect(resolveColor({}, coloredMaster)).toBe("#12809CFF");
  });

  it("sets the color when the edit provides one", () => {
    expect(resolveColor({ color: "#FF6B35FF" }, coloredMaster)).toBe("#FF6B35FF");
  });

  it("clears to absent on an explicit reset (null), rather than copying the reference's color (ADR-0043)", () => {
    expect(resolveColor({ color: null }, coloredMaster)).toBeUndefined();
  });
});

describe("hasFieldChanges", () => {
  const original = {
    calendarId: "cal-1",
    title: "Standup",
    start: new Date(2026, 0, 1, 9, 0),
    end: new Date(2026, 0, 1, 9, 30),
    allDay: false,
    rrule: "FREQ=DAILY",
    description: "Daily sync",
    location: "Room 1",
    url: "https://example.com/ticket",
  };
  const originalReminders = [{ offsetMinutes: 10, channel: "notification" as const }];

  it("is false when nothing differs (#141)", () => {
    expect(hasFieldChanges({ ...original }, original, originalReminders, originalReminders)).toBe(
      false,
    );
  });

  it("ignores Reminders order (a reorder isn't a change)", () => {
    const changesReminders = [
      { offsetMinutes: 5, channel: "notification" as const },
      { offsetMinutes: 10, channel: "notification" as const },
    ];
    const reorderedReminders = [
      { offsetMinutes: 10, channel: "notification" as const },
      { offsetMinutes: 5, channel: "notification" as const },
    ];
    expect(hasFieldChanges(original, original, changesReminders, reorderedReminders)).toBe(false);
  });

  it("is true when the title differs", () => {
    expect(
      hasFieldChanges(
        { ...original, title: "Standup (renamed)" },
        original,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("is true when the start or end time differs", () => {
    expect(
      hasFieldChanges(
        { ...original, start: new Date(2026, 0, 1, 10, 0) },
        original,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("is true when Reminders are actually added or removed", () => {
    expect(hasFieldChanges(original, original, [], originalReminders)).toBe(true);
  });

  it("is true when the rrule's end condition changes, even though shouldDiscardChildren ignores it", () => {
    expect(
      hasFieldChanges(
        { ...original, rrule: "FREQ=DAILY;COUNT=5" },
        original,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("treats undefined and empty string description/location/url as unchanged", () => {
    const withoutOptional = {
      ...original,
      description: undefined,
      location: undefined,
      url: undefined,
    };
    expect(
      hasFieldChanges(
        { ...original, description: "", location: "", url: "" },
        withoutOptional,
        originalReminders,
        originalReminders,
      ),
    ).toBe(false);
  });

  it("is true when the url differs", () => {
    expect(
      hasFieldChanges(
        { ...original, url: "https://example.com/other" },
        original,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("is true when a color is set", () => {
    expect(
      hasFieldChanges(
        { ...original, color: "#FF6B35FF" },
        original,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("is true when a color is explicitly reset to inherit", () => {
    const withColor = { ...original, color: "#FF6B35FF" };
    expect(
      hasFieldChanges(
        { ...withColor, color: null },
        withColor,
        originalReminders,
        originalReminders,
      ),
    ).toBe(true);
  });

  it("treats an absent color key as unchanged from another absent color key", () => {
    expect(hasFieldChanges({ ...original }, original, originalReminders, originalReminders)).toBe(
      false,
    );
  });
});

describe("remindersEqual", () => {
  it("is order-independent", () => {
    const a = [
      { offsetMinutes: 5, channel: "notification" as const },
      { offsetMinutes: 10, channel: "notification" as const },
    ];
    const b = [
      { offsetMinutes: 10, channel: "notification" as const },
      { offsetMinutes: 5, channel: "notification" as const },
    ];
    expect(remindersEqual(a, b)).toBe(true);
  });

  it("is false when the sets differ", () => {
    expect(
      remindersEqual(
        [{ offsetMinutes: 10, channel: "notification" }],
        [{ offsetMinutes: 30, channel: "email" }],
      ),
    ).toBe(false);
  });
});
