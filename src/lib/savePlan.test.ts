import { describe, expect, it } from "vitest";
import type { Reminder } from "./event";
import type { MasterFieldChanges } from "./seriesOperation";
import {
  planEventSave,
  type CreateSaveInput,
  type EditSaveInput,
  type PlanEventSaveDeps,
} from "./savePlan";

const START = new Date(2026, 7, 3, 9, 0);
const END = new Date(2026, 7, 3, 10, 0);

const DETERMINISTIC: PlanEventSaveDeps = {
  newId: () => "new-id",
  viewerZone: () => "Europe/Berlin",
};

const NOTIFY_10: Reminder = { offsetMinutes: 10, channel: "notification" };
const EMAIL_30: Reminder = { offsetMinutes: 30, channel: "email" };

function fields(overrides: Partial<MasterFieldChanges> = {}): MasterFieldChanges {
  return {
    calendarId: "cal-1",
    title: "Standup",
    start: START,
    end: END,
    ...overrides,
  };
}

function createInput(
  overrides: Partial<Omit<CreateSaveInput, "mode">> = {},
): CreateSaveInput {
  return {
    mode: "create",
    changes: fields(),
    reminders: [],
    stagedAttendees: [],
    pendingAttachments: [],
    hasCreated: false,
    isSaving: false,
    ...overrides,
  };
}

function editInput(
  overrides: Partial<Omit<EditSaveInput, "mode">> = {},
): EditSaveInput {
  return {
    mode: "edit",
    changes: fields(),
    reminders: [],
    eventId: "evt-1",
    isReadOnly: false,
    isRecurring: false,
    masterRrule: undefined,
    masterHasChildren: false,
    original: fields(),
    originalReminders: [],
    ...overrides,
  };
}

describe("planEventSave", () => {
  describe("refusals", () => {
    it("blocks an empty title", () => {
      const plan = planEventSave(createInput({ changes: fields({ title: "  " }) }));
      expect(plan).toEqual({ kind: "blocked", reason: "invalid" });
    });

    it("blocks an unselected Calendar", () => {
      const plan = planEventSave(createInput({ changes: fields({ calendarId: "" }) }));
      expect(plan).toEqual({ kind: "blocked", reason: "invalid" });
    });

    it("blocks an end at or before its start", () => {
      const plan = planEventSave(createInput({ changes: fields({ end: START }) }));
      expect(plan).toEqual({ kind: "blocked", reason: "invalid" });
    });

    it("does not range-check an all-day Event, which has no time inputs", () => {
      const plan = planEventSave(
        createInput({ changes: fields({ end: START, allDay: true }) }),
        DETERMINISTIC,
      );
      expect(plan.kind).toBe("createAndClose");
    });

    it("blocks a read-only Event whose Reminders draft is untouched", () => {
      const plan = planEventSave(
        editInput({
          isReadOnly: true,
          reminders: [NOTIFY_10],
          originalReminders: [NOTIFY_10],
        }),
      );
      expect(plan).toEqual({ kind: "blocked", reason: "readOnlyUnchanged" });
    });

    it("blocks a second create once one has already succeeded", () => {
      const plan = planEventSave(createInput({ hasCreated: true }));
      expect(plan).toEqual({ kind: "blocked", reason: "alreadyCreated" });
    });

    it("blocks a create while one carrying Attendees is in flight", () => {
      const plan = planEventSave(createInput({ isSaving: true }));
      expect(plan).toEqual({ kind: "blocked", reason: "inFlight" });
    });

    it("reports the more specific reason when a guard and invalidity coincide", () => {
      const plan = planEventSave(
        createInput({ hasCreated: true, changes: fields({ title: "" }) }),
      );
      expect(plan).toEqual({ kind: "blocked", reason: "alreadyCreated" });
    });
  });

  describe("a read-only Event", () => {
    it("writes the acting User's Reminders and nothing else", () => {
      const plan = planEventSave(
        editInput({
          isReadOnly: true,
          eventId: "evt-9",
          reminders: [NOTIFY_10],
          originalReminders: [],
        }),
      );
      expect(plan).toEqual({
        kind: "writeReminders",
        eventId: "evt-9",
        reminders: [NOTIFY_10],
      });
    });

    it("ignores a reordered but otherwise identical Reminders draft", () => {
      const plan = planEventSave(
        editInput({
          isReadOnly: true,
          reminders: [EMAIL_30, NOTIFY_10],
          originalReminders: [NOTIFY_10, EMAIL_30],
        }),
      );
      expect(plan).toEqual({ kind: "blocked", reason: "readOnlyUnchanged" });
    });

    it("is refused before its other fields are validated", () => {
      const plan = planEventSave(
        editInput({
          isReadOnly: true,
          changes: fields({ title: "" }),
          reminders: [NOTIFY_10],
          originalReminders: [],
        }),
      );
      expect(plan.kind).toBe("writeReminders");
    });
  });

  describe("create", () => {
    it("creates and closes when nothing is staged", () => {
      const plan = planEventSave(createInput(), DETERMINISTIC);
      expect(plan).toEqual({
        kind: "createAndClose",
        event: {
          id: "new-id",
          calendarId: "cal-1",
          title: "Standup",
          start: START,
          end: END,
          allDay: undefined,
          rrule: undefined,
          tzid: "Europe/Berlin",
          description: undefined,
          location: undefined,
          url: undefined,
          color: undefined,
          reminders: [],
        },
      });
    });

    it("leaves an all-day Event timezone-free", () => {
      const plan = planEventSave(
        createInput({ changes: fields({ allDay: true }) }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({ event: { tzid: undefined, allDay: true } });
    });

    it("normalizes an explicit reset to Calendar colour to absent", () => {
      const plan = planEventSave(
        createInput({ changes: fields({ color: null }) }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({ event: { color: undefined } });
    });

    it("carries the acting User's Reminders draft onto the new Event", () => {
      const plan = planEventSave(
        createInput({ reminders: [NOTIFY_10] }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({ event: { reminders: [NOTIFY_10] } });
    });

    it("splits staged Attendees by target kind", () => {
      const plan = planEventSave(
        createInput({
          stagedAttendees: [
            { kind: "user", userId: 7, name: "Ada" },
            { kind: "email", email: "grace@example.com" },
            { kind: "group", groupId: 2, name: "Design" },
            { kind: "user", userId: 9, name: "Alan" },
          ],
        }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({
        kind: "createWithAttendees",
        attendees: {
          attendeeUserIds: [7, 9],
          attendeeGroupIds: [2],
          attendeeEmails: ["grace@example.com"],
        },
        pendingAttachments: [],
      });
    });

    it("carries pending Attachments through the Attendee-bearing create too", () => {
      const attachment = { draftId: "d1", file: new File(["x"], "notes.txt") };
      const plan = planEventSave(
        createInput({
          stagedAttendees: [{ kind: "user", userId: 7, name: "Ada" }],
          pendingAttachments: [attachment],
        }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({
        kind: "createWithAttendees",
        pendingAttachments: [attachment],
      });
    });

    it("awaits the create when only Attachments are staged", () => {
      const attachment = { draftId: "d1", file: new File(["x"], "notes.txt") };
      const plan = planEventSave(
        createInput({ pendingAttachments: [attachment] }),
        DETERMINISTIC,
      );
      expect(plan).toMatchObject({
        kind: "createThenUpload",
        pendingAttachments: [attachment],
      });
    });
  });

  describe("editing a non-recurring Event", () => {
    it("updates the Event, leaving untouched Reminders alone", () => {
      const changes = fields({ title: "Renamed" });
      const plan = planEventSave(editInput({ changes, eventId: "evt-3" }));
      expect(plan).toEqual({
        kind: "updateEvent",
        id: "evt-3",
        changes,
        reminders: undefined,
      });
    });

    it("carries the Reminders draft when it changed", () => {
      const plan = planEventSave(
        editInput({ reminders: [NOTIFY_10], originalReminders: [] }),
      );
      expect(plan).toMatchObject({
        kind: "updateEvent",
        reminders: [NOTIFY_10],
      });
    });

    it("still writes when nothing changed — there is no scope to spare", () => {
      const plan = planEventSave(editInput());
      expect(plan.kind).toBe("updateEvent");
    });
  });

  describe("editing a recurring Occurrence whose rule changed", () => {
    const ruleChange = {
      isRecurring: true,
      masterRrule: "FREQ=WEEKLY;BYDAY=MO",
      changes: fields({ rrule: "FREQ=DAILY" }),
      original: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO" }),
    };

    it("warns before discarding a series' existing Overrides", () => {
      const plan = planEventSave(
        editInput({ ...ruleChange, masterHasChildren: true }),
      );
      expect(plan).toEqual({ kind: "confirmDiscard" });
    });

    it("forces All events silently when there is nothing to lose", () => {
      const plan = planEventSave(
        editInput({ ...ruleChange, masterHasChildren: false }),
      );
      expect(plan).toMatchObject({ kind: "editSeries", scope: "all" });
    });

    it("proceeds once the discard is confirmed", () => {
      const plan = planEventSave(
        editInput({
          ...ruleChange,
          masterHasChildren: true,
          discardConfirmed: true,
        }),
      );
      expect(plan).toMatchObject({ kind: "editSeries", scope: "all" });
    });

    it("never asks for a scope — a rule change is always All events", () => {
      const plan = planEventSave(
        editInput({ ...ruleChange, masterHasChildren: false, resolvedScope: "this" }),
      );
      expect(plan).toMatchObject({ kind: "editSeries", scope: "all" });
    });

    it("ignores an end-condition-only rule change", () => {
      const plan = planEventSave(
        editInput({
          isRecurring: true,
          masterRrule: "FREQ=WEEKLY;BYDAY=MO",
          changes: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO;COUNT=5" }),
          original: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO" }),
          masterHasChildren: true,
        }),
      );
      expect(plan).toEqual({ kind: "askScope" });
    });
  });

  describe("editing a recurring Occurrence whose rule is unchanged", () => {
    const recurring = {
      isRecurring: true,
      masterRrule: "FREQ=WEEKLY;BYDAY=MO",
      original: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO" }),
    };

    it("closes on a no-op save rather than prompting for a scope", () => {
      const plan = planEventSave(
        editInput({ ...recurring, changes: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO" }) }),
      );
      expect(plan).toEqual({ kind: "close" });
    });

    it("asks which Occurrences a real change applies to", () => {
      const plan = planEventSave(
        editInput({
          ...recurring,
          changes: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO", title: "Renamed" }),
        }),
      );
      expect(plan).toEqual({ kind: "askScope" });
    });

    it("asks even when only the Reminders draft changed", () => {
      const plan = planEventSave(
        editInput({
          ...recurring,
          changes: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO" }),
          reminders: [NOTIFY_10],
          originalReminders: [],
        }),
      );
      expect(plan).toEqual({ kind: "askScope" });
    });

    it("applies the scope once the User has chosen one", () => {
      const changes = fields({ rrule: "FREQ=WEEKLY;BYDAY=MO", title: "Renamed" });
      const plan = planEventSave(
        editInput({ ...recurring, changes, resolvedScope: "following" }),
      );
      expect(plan).toEqual({
        kind: "editSeries",
        scope: "following",
        changes,
        reminders: undefined,
      });
    });

    it("carries a changed Reminders draft alongside the scoped edit", () => {
      const plan = planEventSave(
        editInput({
          ...recurring,
          changes: fields({ rrule: "FREQ=WEEKLY;BYDAY=MO", title: "Renamed" }),
          reminders: [EMAIL_30],
          originalReminders: [],
          resolvedScope: "this",
        }),
      );
      expect(plan).toMatchObject({
        kind: "editSeries",
        scope: "this",
        reminders: [EMAIL_30],
      });
    });
  });
});
