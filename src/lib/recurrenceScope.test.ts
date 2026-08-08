import { RRule } from "rrule";
import { fromZonedTime } from "date-fns-tz";
import { describe, expect, it } from "vitest";
import type { Event } from "./event";
import { toFloating, viewerZone } from "./floatingTime";
import {
  hasFieldChanges,
  makeException,
  makeOverride,
  resolveReminders,
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

  it("starts a fresh Override's Reminders as a copy of the master's (ADR-0020)", () => {
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

    expect(makeOverride(remindedMaster, occurrenceStart, changes).reminders).toEqual(
      remindedMaster.reminders,
    );
  });

  it("uses the caller's authored Reminders over the master's when both are present", () => {
    const remindedMaster: Event = {
      ...master,
      reminders: [{ offsetMinutes: 10, channel: "notification" }],
    };
    const occurrenceStart = new Date(2026, 0, 3, 9, 0);
    const authoredReminders = [{ offsetMinutes: 30, channel: "email" as const }];
    const changes = {
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date(2026, 0, 3, 10, 0),
      end: new Date(2026, 0, 3, 10, 30),
      reminders: authoredReminders,
    };

    expect(makeOverride(remindedMaster, occurrenceStart, changes).reminders).toEqual(
      authoredReminders,
    );
  });
});

describe("resolveReminders", () => {
  it("falls back to the reference's own Reminders when changes didn't touch them", () => {
    const reference = { reminders: [{ offsetMinutes: 10, channel: "notification" as const }] };
    expect(resolveReminders({}, reference)).toEqual(reference.reminders);
  });

  it("uses the changes' Reminders when present, even an empty array", () => {
    const reference = { reminders: [{ offsetMinutes: 10, channel: "notification" as const }] };
    expect(resolveReminders({ reminders: [] }, reference)).toEqual([]);
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

  it("carries the master's Reminders into the new master when changes didn't touch them", () => {
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

    expect(result.newMaster.reminders).toEqual(remindedMaster.reminders);
  });

  it("carries the caller's authored Reminders into the new master over the master's own", () => {
    const remindedMaster: Event = {
      ...master,
      reminders: [{ offsetMinutes: 10, channel: "notification" }],
    };
    const splitStart = new Date(2026, 0, 3, 9, 0);
    const authoredReminders = [{ offsetMinutes: 30, channel: "email" as const }];
    const changes = {
      calendarId: "cal-1",
      title: "Standup",
      start: splitStart,
      end: new Date(2026, 0, 3, 9, 30),
      reminders: authoredReminders,
    };

    const result = splitFollowing(remindedMaster, splitStart, changes);

    expect(result.newMaster.reminders).toEqual(authoredReminders);
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

describe("hasFieldChanges", () => {
  const original = {
    calendarId: "cal-1",
    title: "Standup",
    start: new Date(2026, 0, 1, 9, 0),
    end: new Date(2026, 0, 1, 9, 30),
    allDay: false,
    rrule: "FREQ=DAILY",
    reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    description: "Daily sync",
    location: "Room 1",
  };

  it("is false when nothing differs (#141)", () => {
    expect(hasFieldChanges({ ...original }, original)).toBe(false);
  });

  it("ignores Reminders order (a reorder isn't a change)", () => {
    const changes = {
      ...original,
      reminders: [
        { offsetMinutes: 5, channel: "notification" as const },
        { offsetMinutes: 10, channel: "notification" as const },
      ],
    };
    const reordered = {
      ...original,
      reminders: [
        { offsetMinutes: 10, channel: "notification" as const },
        { offsetMinutes: 5, channel: "notification" as const },
      ],
    };
    expect(hasFieldChanges(changes, reordered)).toBe(false);
  });

  it("is true when the title differs", () => {
    expect(hasFieldChanges({ ...original, title: "Standup (renamed)" }, original)).toBe(
      true,
    );
  });

  it("is true when the start or end time differs", () => {
    expect(
      hasFieldChanges({ ...original, start: new Date(2026, 0, 1, 10, 0) }, original),
    ).toBe(true);
  });

  it("is true when Reminders are actually added or removed", () => {
    expect(hasFieldChanges({ ...original, reminders: [] }, original)).toBe(true);
  });

  it("is true when the rrule's end condition changes, even though shouldDiscardChildren ignores it", () => {
    expect(
      hasFieldChanges(
        { ...original, rrule: "FREQ=DAILY;COUNT=5" },
        original,
      ),
    ).toBe(true);
  });

  it("treats undefined and empty string description/location as unchanged", () => {
    const withoutOptional = { ...original, description: undefined, location: undefined };
    expect(hasFieldChanges({ ...original, description: "", location: "" }, withoutOptional)).toBe(
      false,
    );
  });
});
