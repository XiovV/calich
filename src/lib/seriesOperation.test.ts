import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./eventsApi", () => ({
  eventsApi: {
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    addException: vi.fn(),
    reparentSeries: vi.fn(),
  },
}));

const { eventsApi } = await import("./eventsApi");
const { applySeriesOps, dispatchSeriesOps, planDeleteOccurrence, planEditOccurrence } =
  await import("./seriesOperation");

const recurringMaster = {
  id: "master-1",
  calendarId: "cal-1",
  title: "Standup",
  start: new Date("2026-01-01T09:00:00Z"),
  end: new Date("2026-01-01T09:30:00Z"),
  rrule: "FREQ=DAILY",
};

function occurrenceOf(event: typeof recurringMaster, start: Date) {
  const durationMs = event.end.getTime() - event.start.getTime();
  return { event, start, end: new Date(start.getTime() + durationMs) };
}

const nextId = () => "generated-id";

beforeEach(() => {
  vi.clearAllMocks();
});

describe("planEditOccurrence", () => {
  it('scope "this": plans a new override for an un-overridden Occurrence', () => {
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const ops = planEditOccurrence(
      {
        master: recurringMaster,
        occurrence: occurrenceOf(recurringMaster, occurrenceStart),
        isOverride: false,
        originalStart: occurrenceStart,
        scope: "this",
        changes: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          // The modal always includes the series' rrule; the planned
          // Override must never carry it (ADR-0016).
          rrule: "FREQ=DAILY",
        },
      },
      nextId,
    );

    expect(ops).toEqual([
      {
        kind: "overrideOccurrence",
        id: "generated-id",
        isNew: true,
        parentId: "master-1",
        recurrenceId: occurrenceStart,
        fields: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          allDay: undefined,
        },
      },
    ]);
  });

  it('scope "this": plans an in-place update when the Occurrence is already an Override', () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(override as never, override.start),
      isOverride: true,
      originalStart: override.recurrenceId,
      scope: "this",
      changes: {
        calendarId: "cal-1",
        title: "Renamed again",
        start: override.start,
        end: override.end,
        rrule: "FREQ=DAILY",
      },
    });

    expect(ops).toEqual([
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: false,
        parentId: "master-1",
        recurrenceId: override.recurrenceId,
        fields: {
          calendarId: "cal-1",
          title: "Renamed again",
          start: override.start,
          end: override.end,
          allDay: undefined,
        },
      },
    ]);
  });

  it('scope "this": a fresh override inherits the master\'s color when untouched (ADR-0043)', () => {
    const coloredMaster = { ...recurringMaster, color: "#12809CFF" };
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const ops = planEditOccurrence(
      {
        master: coloredMaster,
        occurrence: occurrenceOf(coloredMaster, occurrenceStart),
        isOverride: false,
        originalStart: occurrenceStart,
        scope: "this",
        changes: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops[0]).toMatchObject({ fields: { color: "#12809CFF" } });
  });

  it('scope "all": keeps the rule and does not discard children when the rule is unchanged', () => {
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Renamed",
        start: recurringMaster.start,
        end: recurringMaster.end,
      },
    });

    expect(ops).toEqual([
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Renamed",
          start: recurringMaster.start,
          end: recurringMaster.end,
          allDay: undefined,
          rrule: "FREQ=DAILY",
        },
        discardChildren: false,
      },
    ]);
  });

  it('scope "all": discards children when the rule\'s pattern changes', () => {
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Standup",
        start: recurringMaster.start,
        end: recurringMaster.end,
        rrule: "FREQ=WEEKLY;BYDAY=TH",
      },
    });

    expect(ops).toEqual([
      expect.objectContaining({
        kind: "putEvent",
        fields: expect.objectContaining({ rrule: "FREQ=WEEKLY;BYDAY=TH" }),
        discardChildren: true,
      }),
    ]);
  });

  it('scope "all": an explicit undefined rrule ("Does not repeat") wins over the reanchored rule', () => {
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Standup",
        start: recurringMaster.start,
        end: recurringMaster.end,
        rrule: undefined,
      },
    });

    expect(ops).toEqual([
      expect.objectContaining({
        kind: "putEvent",
        fields: expect.objectContaining({ rrule: undefined }),
        discardChildren: true,
      }),
    ]);
  });

  it('scope "all": editing from a later Occurrence keeps the master anchored on its own date', () => {
    const openedOccurrenceStart = new Date("2026-01-05T09:00:00Z");
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, openedOccurrenceStart),
      isOverride: false,
      originalStart: openedOccurrenceStart,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Standup",
        start: new Date("2026-01-05T10:00:00Z"),
        end: new Date("2026-01-05T10:30:00Z"),
      },
    });

    expect(ops).toEqual([
      expect.objectContaining({
        fields: expect.objectContaining({
          start: new Date("2026-01-01T10:00:00Z"),
          end: new Date("2026-01-01T10:30:00Z"),
        }),
      }),
    ]);
  });

  it('scope "this" (new override): starts the Override\'s Reminders as a copy of the master\'s (ADR-0020)', () => {
    const remindedMaster = {
      ...recurringMaster,
      reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    };
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const ops = planEditOccurrence(
      {
        master: remindedMaster,
        occurrence: occurrenceOf(remindedMaster, occurrenceStart),
        isOverride: false,
        originalStart: occurrenceStart,
        scope: "this",
        changes: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops[0]).toMatchObject({ fields: { reminders: remindedMaster.reminders } });
  });

  it('scope "this" (in-place Override update): applies the authored Reminders to only that Override\'s row', () => {
    const remindedMaster = {
      ...recurringMaster,
      reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    };
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
      reminders: [{ offsetMinutes: 5, channel: "email" as const }],
    };
    const authoredReminders = [{ offsetMinutes: 30, channel: "email" as const }];

    const ops = planEditOccurrence({
      master: remindedMaster,
      occurrence: occurrenceOf(override as never, override.start),
      isOverride: true,
      originalStart: override.recurrenceId,
      scope: "this",
      changes: {
        calendarId: "cal-1",
        title: "Renamed again",
        start: override.start,
        end: override.end,
        reminders: authoredReminders,
      },
    });

    expect(ops[0]).toMatchObject({ fields: { reminders: authoredReminders } });
  });

  it('scope "all": carries the authored Reminders onto the master', () => {
    const authoredReminders = [{ offsetMinutes: 15, channel: "notification" as const }];
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Renamed",
        start: recurringMaster.start,
        end: recurringMaster.end,
        reminders: authoredReminders,
      },
    });

    expect(ops[0]).toMatchObject({ fields: { reminders: authoredReminders } });
  });

  it('scope "all": carries an authored color onto the master (ADR-0043)', () => {
    const ops = planEditOccurrence({
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Renamed",
        start: recurringMaster.start,
        end: recurringMaster.end,
        color: "#FF6B35FF",
      },
    });

    expect(ops[0]).toMatchObject({ fields: { color: "#FF6B35FF" } });
  });

  it('scope "all": an explicit color reset clears the master to absent (ADR-0043)', () => {
    const coloredMaster = { ...recurringMaster, color: "#12809CFF" };
    const ops = planEditOccurrence({
      master: coloredMaster,
      occurrence: occurrenceOf(coloredMaster, coloredMaster.start),
      isOverride: false,
      originalStart: coloredMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Renamed",
        start: coloredMaster.start,
        end: coloredMaster.end,
        color: null,
      },
    });

    expect(ops[0]).toMatchObject({ fields: { color: undefined } });
  });

  it('scope "following": plans a truncated old master, a new master, and the reparent boundary', () => {
    const splitStart = new Date("2026-01-03T09:00:00Z");
    const masterWithExdates = {
      ...recurringMaster,
      exdates: [new Date("2026-01-02T09:00:00Z"), new Date("2026-01-04T09:00:00Z")],
    };

    const ops = planEditOccurrence(
      {
        master: masterWithExdates,
        occurrence: occurrenceOf(masterWithExdates, splitStart),
        isOverride: false,
        originalStart: splitStart,
        scope: "following",
        changes: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops).toEqual([
      {
        kind: "reanchorSeries",
        oldMasterId: "master-1",
        oldMasterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        truncatedRrule: expect.stringContaining("UNTIL=20260102"),
        keptExdates: [new Date("2026-01-02T09:00:00Z")],
        newMasterId: "generated-id",
        newMaster: expect.objectContaining({ title: "Standup (renamed)", rrule: "FREQ=DAILY" }),
        movedExdates: [new Date("2026-01-04T09:00:00Z")],
        reparentFromStart: splitStart,
      },
    ]);
  });

  it('scope "following": preserves the old master\'s own Reminders and carries them into the new master (ADR-0020)', () => {
    const remindedMaster = {
      ...recurringMaster,
      reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    };
    const splitStart = new Date("2026-01-03T09:00:00Z");

    const ops = planEditOccurrence(
      {
        master: remindedMaster,
        occurrence: occurrenceOf(remindedMaster, splitStart),
        isOverride: false,
        originalStart: splitStart,
        scope: "following",
        changes: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops[0]).toMatchObject({
      oldMasterFields: { reminders: remindedMaster.reminders },
      newMaster: { reminders: remindedMaster.reminders },
    });
  });
});

describe("planEditOccurrence preserves the Anchor zone (ADR-0019)", () => {
  const zonedMaster = { ...recurringMaster, tzid: "Europe/Berlin" };

  it('scope "this" (new override): carries the master\'s tzid unchanged', () => {
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const ops = planEditOccurrence(
      {
        master: zonedMaster,
        occurrence: occurrenceOf(zonedMaster, occurrenceStart),
        isOverride: false,
        originalStart: occurrenceStart,
        scope: "this",
        changes: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops[0]).toMatchObject({ fields: { tzid: "Europe/Berlin" } });
  });

  it('scope "this" (in-place Override update): keeps the Override\'s own tzid unchanged', () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
      tzid: "Europe/Berlin",
    };

    const ops = planEditOccurrence({
      master: zonedMaster,
      occurrence: occurrenceOf(override as never, override.start),
      isOverride: true,
      originalStart: override.recurrenceId,
      scope: "this",
      changes: {
        calendarId: "cal-1",
        title: "Renamed again",
        start: override.start,
        end: override.end,
      },
    });

    expect(ops[0]).toMatchObject({ fields: { tzid: "Europe/Berlin" } });
  });

  it('scope "all": keeps the master\'s tzid unchanged', () => {
    const ops = planEditOccurrence({
      master: zonedMaster,
      occurrence: occurrenceOf(zonedMaster, zonedMaster.start),
      isOverride: false,
      originalStart: zonedMaster.start,
      scope: "all",
      changes: {
        calendarId: "cal-1",
        title: "Renamed",
        start: zonedMaster.start,
        end: zonedMaster.end,
      },
    });

    expect(ops[0]).toMatchObject({ fields: { tzid: "Europe/Berlin" } });
  });

  it('scope "following": both the truncated old master and the new master keep the tzid unchanged', () => {
    const splitStart = new Date("2026-01-03T09:00:00Z");
    const ops = planEditOccurrence(
      {
        master: zonedMaster,
        occurrence: occurrenceOf(zonedMaster, splitStart),
        isOverride: false,
        originalStart: splitStart,
        scope: "following",
        changes: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
      nextId,
    );

    expect(ops[0]).toMatchObject({
      oldMasterFields: { tzid: "Europe/Berlin" },
      newMaster: { tzid: "Europe/Berlin" },
    });
  });
});

describe("applySeriesOps", () => {
  it("appends a new Override for an overrideOccurrence create op", () => {
    const events = applySeriesOps([recurringMaster], [
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: true,
        parentId: "master-1",
        recurrenceId: new Date("2026-01-03T09:00:00Z"),
        fields: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
    ]);

    expect(events).toHaveLength(2);
    const override = events.find((e) => e.id === "override-1")!;
    expect(override.parentId).toBe("master-1");
    expect(override.recurrenceId).toEqual(new Date("2026-01-03T09:00:00Z"));
    expect(override.title).toBe("Standup (moved)");
    expect(override.rrule).toBeUndefined();
  });

  it("replaces an existing Override in place for an overrideOccurrence update op", () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const events = applySeriesOps([recurringMaster, override], [
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: false,
        parentId: "master-1",
        recurrenceId: override.recurrenceId,
        fields: { calendarId: "cal-1", title: "Renamed again", start: override.start, end: override.end },
      },
    ]);

    expect(events).toHaveLength(2);
    const updated = events.find((e) => e.id === "override-1")!;
    expect(updated.title).toBe("Renamed again");
  });

  it("updates the master's fields for a putEvent op and drops children when discardChildren is set", () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const kept = applySeriesOps([recurringMaster, override], [
      {
        kind: "putEvent",
        id: "master-1",
        fields: { calendarId: "cal-1", title: "Renamed", start: recurringMaster.start, end: recurringMaster.end },
        discardChildren: false,
      },
    ]);
    expect(kept.find((e) => e.id === "master-1")?.title).toBe("Renamed");
    expect(kept.find((e) => e.id === "override-1")).toBeDefined();

    const discarded = applySeriesOps([recurringMaster, override], [
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
          rrule: "FREQ=WEEKLY;BYDAY=TH",
        },
        discardChildren: true,
      },
    ]);
    expect(discarded).toHaveLength(1);
    expect(discarded[0].rrule).toBe("FREQ=WEEKLY;BYDAY=TH");
  });

  it("truncates the old master, creates the new one, reparents children, and partitions exdates for a reanchorSeries op", () => {
    const overrideBefore = {
      id: "override-before",
      calendarId: "cal-1",
      title: "Before",
      start: new Date("2026-01-02T10:00:00Z"),
      end: new Date("2026-01-02T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-02T09:00:00Z"),
    };
    const overrideAfter = {
      id: "override-after",
      calendarId: "cal-1",
      title: "After",
      start: new Date("2026-01-04T10:00:00Z"),
      end: new Date("2026-01-04T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-04T09:00:00Z"),
    };
    const reparentFromStart = new Date("2026-01-03T09:00:00Z");

    const events = applySeriesOps([recurringMaster, overrideBefore, overrideAfter], [
      {
        kind: "reanchorSeries",
        oldMasterId: "master-1",
        oldMasterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [new Date("2026-01-02T09:00:00Z")],
        newMasterId: "new-master-1",
        newMaster: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          rrule: "FREQ=DAILY",
        },
        movedExdates: [new Date("2026-01-04T09:00:00Z")],
        reparentFromStart,
      },
    ]);

    expect(events).toHaveLength(4);
    const oldMaster = events.find((e) => e.id === "master-1")!;
    expect(oldMaster.rrule).toBe("FREQ=DAILY;UNTIL=20260102T090000Z");
    expect(oldMaster.exdates).toEqual([new Date("2026-01-02T09:00:00Z")]);
    const newMaster = events.find((e) => e.id === "new-master-1")!;
    expect(newMaster.title).toBe("Standup (renamed)");
    expect(newMaster.exdates).toEqual([new Date("2026-01-04T09:00:00Z")]);
    expect(events.find((e) => e.id === "override-before")?.parentId).toBe("master-1");
    expect(events.find((e) => e.id === "override-after")?.parentId).toBe("new-master-1");
  });

  it("carries Reminders through for overrideOccurrence, putEvent, and reanchorSeries ops (ADR-0020)", () => {
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];

    const overridden = applySeriesOps([recurringMaster], [
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: true,
        parentId: "master-1",
        recurrenceId: new Date("2026-01-03T09:00:00Z"),
        fields: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          reminders,
        },
      },
    ]);
    expect(overridden.find((e) => e.id === "override-1")?.reminders).toEqual(reminders);

    const put = applySeriesOps([recurringMaster], [
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Renamed",
          start: recurringMaster.start,
          end: recurringMaster.end,
          reminders,
        },
        discardChildren: false,
      },
    ]);
    expect(put.find((e) => e.id === "master-1")?.reminders).toEqual(reminders);

    // The old master's cache row keeps its own reminders unchanged (it
    // already has `reminders` here) — oldMasterFields only exists to keep
    // them off the wire's omitted-field wipe (ADR-0020), not to drive the
    // cache projection.
    const remindedMaster = { ...recurringMaster, reminders };
    const reanchored = applySeriesOps([remindedMaster], [
      {
        kind: "reanchorSeries",
        oldMasterId: "master-1",
        oldMasterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
          reminders,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [],
        newMasterId: "new-master-1",
        newMaster: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          rrule: "FREQ=DAILY",
          reminders,
        },
        movedExdates: [],
        reparentFromStart: new Date("2026-01-03T09:00:00Z"),
      },
    ]);
    expect(reanchored.find((e) => e.id === "master-1")?.reminders).toEqual(reminders);
    expect(reanchored.find((e) => e.id === "new-master-1")?.reminders).toEqual(reminders);
  });
});

describe("dispatchSeriesOps", () => {
  it("dispatches an overrideOccurrence create op via eventsApi.create with no rrule leak", async () => {
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);

    await dispatchSeriesOps("token-123", [
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: true,
        parentId: "master-1",
        recurrenceId: new Date("2026-01-03T09:00:00Z"),
        fields: {
          calendarId: "cal-1",
          title: "Standup (moved)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
    ]);

    const call = vi.mocked(eventsApi.create).mock.calls[0][1];
    expect(call).toMatchObject({ parentId: "master-1", recurrenceId: new Date("2026-01-03T09:00:00Z") });
    expect(call).not.toHaveProperty("rrule");
  });

  it("dispatches an overrideOccurrence update op via eventsApi.update with rrule stripped", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await dispatchSeriesOps("token-123", [
      {
        kind: "overrideOccurrence",
        id: "override-1",
        isNew: false,
        parentId: "master-1",
        recurrenceId: new Date("2026-01-03T09:00:00Z"),
        fields: {
          calendarId: "cal-1",
          title: "Renamed again",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
        },
      },
    ]);

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "override-1",
      expect.objectContaining({ title: "Renamed again", rrule: undefined }),
    );
  });

  it("dispatches a putEvent op's color to eventsApi.update verbatim (ADR-0043)", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await dispatchSeriesOps("token-123", [
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Renamed",
          start: recurringMaster.start,
          end: recurringMaster.end,
          color: "#FF6B35FF",
        },
        discardChildren: false,
      },
    ]);

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ color: "#FF6B35FF" }),
    );
  });

  it("dispatches a putEvent op's cleared color (undefined) to eventsApi.update, not the prior value", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await dispatchSeriesOps("token-123", [
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Renamed",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        discardChildren: false,
      },
    ]);

    const call = vi.mocked(eventsApi.update).mock.calls[0][2];
    expect(call.color).toBeUndefined();
  });

  it("dispatches a reanchorSeries op as truncate -> create -> reparent, in order", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);
    vi.mocked(eventsApi.reparentSeries).mockResolvedValue(undefined);

    const reparentFromStart = new Date("2026-01-03T09:00:00Z");
    await dispatchSeriesOps("token-123", [
      {
        kind: "reanchorSeries",
        oldMasterId: "master-1",
        oldMasterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [],
        newMasterId: "new-master-1",
        newMaster: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          rrule: "FREQ=DAILY",
        },
        movedExdates: [],
        reparentFromStart,
      },
    ]);

    const updateOrder = vi.mocked(eventsApi.update).mock.invocationCallOrder[0];
    const createOrder = vi.mocked(eventsApi.create).mock.invocationCallOrder[0];
    const reparentOrder = vi.mocked(eventsApi.reparentSeries).mock.invocationCallOrder[0];
    expect(updateOrder).toBeLessThan(createOrder);
    expect(createOrder).toBeLessThan(reparentOrder);

    expect(eventsApi.update).toHaveBeenCalledWith("token-123", "master-1", {
      calendarId: "cal-1",
      title: "Standup",
      start: recurringMaster.start,
      end: recurringMaster.end,
      rrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
    });
    expect(eventsApi.create).toHaveBeenCalledWith("token-123", {
      id: "new-master-1",
      calendarId: "cal-1",
      title: "Standup (renamed)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      rrule: "FREQ=DAILY",
    });
    expect(eventsApi.reparentSeries).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      "new-master-1",
      reparentFromStart,
    );
  });

  it("dispatches a putEvent op via eventsApi.update", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);

    await dispatchSeriesOps("token-123", [
      {
        kind: "putEvent",
        id: "master-1",
        fields: {
          calendarId: "cal-1",
          title: "Renamed",
          start: recurringMaster.start,
          end: recurringMaster.end,
          rrule: "FREQ=DAILY",
        },
        discardChildren: false,
      },
    ]);

    expect(eventsApi.update).toHaveBeenCalledWith("token-123", "master-1", {
      calendarId: "cal-1",
      title: "Renamed",
      start: recurringMaster.start,
      end: recurringMaster.end,
      rrule: "FREQ=DAILY",
    });
  });

  it("sends the old master's own Reminders on a reanchorSeries update, so a 'following' edit never wipes them (ADR-0020)", async () => {
    // The update API replaces Reminders wholesale on any omitted field
    // (ADR-0020) — the old master's own reminders must ride along even
    // though nothing about them changed, or the split would silently clear
    // them.
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.create).mockResolvedValue({} as never);
    vi.mocked(eventsApi.reparentSeries).mockResolvedValue(undefined);
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];

    await dispatchSeriesOps("token-123", [
      {
        kind: "reanchorSeries",
        oldMasterId: "master-1",
        oldMasterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
          reminders,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [],
        newMasterId: "new-master-1",
        newMaster: {
          calendarId: "cal-1",
          title: "Standup (renamed)",
          start: new Date("2026-01-03T10:00:00Z"),
          end: new Date("2026-01-03T10:30:00Z"),
          rrule: "FREQ=DAILY",
          reminders,
        },
        movedExdates: [],
        reparentFromStart: new Date("2026-01-03T09:00:00Z"),
      },
    ]);

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ reminders }),
    );
    expect(eventsApi.create).toHaveBeenCalledWith(
      "token-123",
      expect.objectContaining({ reminders }),
    );
  });
});

describe("planDeleteOccurrence", () => {
  it('scope "this": plans a cancelOccurrence for an un-overridden Occurrence', () => {
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const ops = planDeleteOccurrence({
      events: [recurringMaster],
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, occurrenceStart),
      isOverride: false,
      originalStart: occurrenceStart,
      scope: "this",
    });

    expect(ops).toEqual([{ kind: "cancelOccurrence", parentId: "master-1", occurrenceStart }]);
  });

  it('scope "this": plans a removeSeries for an Occurrence that is already an Override', () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const ops = planDeleteOccurrence({
      events: [recurringMaster, override],
      master: recurringMaster,
      occurrence: occurrenceOf(override as never, override.start),
      isOverride: true,
      originalStart: override.recurrenceId,
      scope: "this",
    });

    expect(ops).toEqual([{ kind: "removeSeries", id: "override-1" }]);
  });

  it('scope "all": plans a removeSeries for the master', () => {
    const ops = planDeleteOccurrence({
      events: [recurringMaster],
      master: recurringMaster,
      occurrence: occurrenceOf(recurringMaster, recurringMaster.start),
      isOverride: false,
      originalStart: recurringMaster.start,
      scope: "all",
    });

    expect(ops).toEqual([{ kind: "removeSeries", id: "master-1" }]);
  });

  it('scope "following": plans a truncateSeries with the orphaned children at-or-after the split', () => {
    const overrideBefore = {
      id: "override-before",
      calendarId: "cal-1",
      title: "Before",
      start: new Date("2026-01-02T10:00:00Z"),
      end: new Date("2026-01-02T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-02T09:00:00Z"),
    };
    const overrideAfter = {
      id: "override-after",
      calendarId: "cal-1",
      title: "After",
      start: new Date("2026-01-04T10:00:00Z"),
      end: new Date("2026-01-04T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-04T09:00:00Z"),
    };
    const splitStart = new Date("2026-01-03T09:00:00Z");
    const masterWithExdates = {
      ...recurringMaster,
      exdates: [new Date("2026-01-02T09:00:00Z"), new Date("2026-01-04T09:00:00Z")],
    };

    const ops = planDeleteOccurrence({
      events: [masterWithExdates, overrideBefore, overrideAfter],
      master: masterWithExdates,
      occurrence: occurrenceOf(masterWithExdates, splitStart),
      isOverride: false,
      originalStart: splitStart,
      scope: "following",
    });

    expect(ops).toEqual([
      {
        kind: "truncateSeries",
        masterId: "master-1",
        masterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        truncatedRrule: expect.stringContaining("UNTIL=20260102"),
        keptExdates: [new Date("2026-01-02T09:00:00Z")],
        orphanedIds: ["override-after"],
      },
    ]);
  });

  it('scope "following": preserves the master\'s own Reminders in the truncated master (ADR-0020)', () => {
    const remindedMaster = {
      ...recurringMaster,
      reminders: [{ offsetMinutes: 10, channel: "notification" as const }],
    };
    const splitStart = new Date("2026-01-03T09:00:00Z");

    const ops = planDeleteOccurrence({
      events: [remindedMaster],
      master: remindedMaster,
      occurrence: occurrenceOf(remindedMaster, splitStart),
      isOverride: false,
      originalStart: splitStart,
      scope: "following",
    });

    expect(ops[0]).toMatchObject({ masterFields: { reminders: remindedMaster.reminders } });
  });
});

describe("planDeleteOccurrence preserves the Anchor zone (ADR-0019)", () => {
  it('scope "following": the truncated master keeps its tzid unchanged', () => {
    const zonedMaster = { ...recurringMaster, tzid: "Europe/Berlin" };
    const splitStart = new Date("2026-01-03T09:00:00Z");

    const ops = planDeleteOccurrence({
      events: [zonedMaster],
      master: zonedMaster,
      occurrence: occurrenceOf(zonedMaster, splitStart),
      isOverride: false,
      originalStart: splitStart,
      scope: "following",
    });

    expect(ops[0]).toMatchObject({ masterFields: { tzid: "Europe/Berlin" } });
  });
});

describe("applySeriesOps (delete ops)", () => {
  it("appends an exdate to the master for a cancelOccurrence op", () => {
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");
    const events = applySeriesOps(
      [recurringMaster],
      [{ kind: "cancelOccurrence", parentId: "master-1", occurrenceStart }],
    );

    expect(events[0].exdates).toEqual([occurrenceStart]);
  });

  it("drops the row and any local children for a removeSeries op", () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const events = applySeriesOps(
      [recurringMaster, override],
      [{ kind: "removeSeries", id: "master-1" }],
    );

    expect(events).toEqual([]);
  });

  it("leaves other rows alone for a removeSeries op targeting a single Override", () => {
    const override = {
      id: "override-1",
      calendarId: "cal-1",
      title: "Standup (moved)",
      start: new Date("2026-01-03T10:00:00Z"),
      end: new Date("2026-01-03T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-03T09:00:00Z"),
    };

    const events = applySeriesOps(
      [recurringMaster, override],
      [{ kind: "removeSeries", id: "override-1" }],
    );

    expect(events.map((e) => e.id)).toEqual(["master-1"]);
  });

  it("truncates the master and drops orphaned children for a truncateSeries op", () => {
    const overrideBefore = {
      id: "override-before",
      calendarId: "cal-1",
      title: "Before",
      start: new Date("2026-01-02T10:00:00Z"),
      end: new Date("2026-01-02T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-02T09:00:00Z"),
    };
    const overrideAfter = {
      id: "override-after",
      calendarId: "cal-1",
      title: "After",
      start: new Date("2026-01-04T10:00:00Z"),
      end: new Date("2026-01-04T10:30:00Z"),
      parentId: "master-1",
      recurrenceId: new Date("2026-01-04T09:00:00Z"),
    };

    const events = applySeriesOps(
      [recurringMaster, overrideBefore, overrideAfter],
      [
        {
          kind: "truncateSeries",
          masterId: "master-1",
          masterFields: {
            calendarId: "cal-1",
            title: "Standup",
            start: recurringMaster.start,
            end: recurringMaster.end,
          },
          truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
          keptExdates: [new Date("2026-01-02T09:00:00Z")],
          orphanedIds: ["override-after"],
        },
      ],
    );

    expect(events.map((e) => e.id).sort()).toEqual(["master-1", "override-before"]);
    const master = events.find((e) => e.id === "master-1")!;
    expect(master.rrule).toBe("FREQ=DAILY;UNTIL=20260102T090000Z");
    expect(master.exdates).toEqual([new Date("2026-01-02T09:00:00Z")]);
  });
});

describe("dispatchSeriesOps (delete ops)", () => {
  it("dispatches a cancelOccurrence op via eventsApi.addException", async () => {
    vi.mocked(eventsApi.addException).mockResolvedValue(undefined);
    const occurrenceStart = new Date("2026-01-03T09:00:00Z");

    await dispatchSeriesOps("token-123", [
      { kind: "cancelOccurrence", parentId: "master-1", occurrenceStart },
    ]);

    expect(eventsApi.addException).toHaveBeenCalledWith("token-123", "master-1", occurrenceStart);
  });

  it("dispatches a removeSeries op via eventsApi.remove", async () => {
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);

    await dispatchSeriesOps("token-123", [{ kind: "removeSeries", id: "master-1" }]);

    expect(eventsApi.remove).toHaveBeenCalledWith("token-123", "master-1");
  });

  it("dispatches a truncateSeries op as update -> remove(s), in order", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);

    await dispatchSeriesOps("token-123", [
      {
        kind: "truncateSeries",
        masterId: "master-1",
        masterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [],
        orphanedIds: ["override-after", "override-later"],
      },
    ]);

    const updateOrder = vi.mocked(eventsApi.update).mock.invocationCallOrder[0];
    const removeOrders = vi.mocked(eventsApi.remove).mock.invocationCallOrder;
    expect(updateOrder).toBeLessThan(removeOrders[0]);
    expect(removeOrders[0]).toBeLessThan(removeOrders[1]);

    expect(eventsApi.update).toHaveBeenCalledWith("token-123", "master-1", {
      calendarId: "cal-1",
      title: "Standup",
      start: recurringMaster.start,
      end: recurringMaster.end,
      rrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
    });
    expect(eventsApi.remove).toHaveBeenNthCalledWith(1, "token-123", "override-after");
    expect(eventsApi.remove).toHaveBeenNthCalledWith(2, "token-123", "override-later");
  });

  it("sends the master's own Reminders on a truncateSeries update, so a 'following' delete never wipes them (ADR-0020)", async () => {
    vi.mocked(eventsApi.update).mockResolvedValue({} as never);
    vi.mocked(eventsApi.remove).mockResolvedValue(undefined);
    const reminders = [{ offsetMinutes: 10, channel: "notification" as const }];

    await dispatchSeriesOps("token-123", [
      {
        kind: "truncateSeries",
        masterId: "master-1",
        masterFields: {
          calendarId: "cal-1",
          title: "Standup",
          start: recurringMaster.start,
          end: recurringMaster.end,
          reminders,
        },
        truncatedRrule: "FREQ=DAILY;UNTIL=20260102T090000Z",
        keptExdates: [],
        orphanedIds: [],
      },
    ]);

    expect(eventsApi.update).toHaveBeenCalledWith(
      "token-123",
      "master-1",
      expect.objectContaining({ reminders }),
    );
  });
});
