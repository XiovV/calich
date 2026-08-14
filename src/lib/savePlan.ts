import type { Event, Reminder } from "./event";
import type { StagedCreateAttendees } from "./eventsStore";
import { viewerZone } from "./floatingTime";
import {
  hasFieldChanges,
  remindersEqual,
  resolveColor,
  shouldDiscardChildren,
  type EditScope,
} from "./recurrenceScope";
import type { MasterFieldChanges } from "./seriesOperation";

/**
 * What pressing Save on the Event modal compiles down to, before anything is
 * written — the seam between the modal's form state and the writes it
 * eventually performs (ADR-0066, CONTEXT.md's Save plan).
 *
 * One tier above a Series operation: this decides *which* write happens (and
 * whether the User has to be asked something first); `planEditOccurrence`
 * decides what a scoped edit compiles to once the scope is settled.
 *
 * Closing the dialog is implied by the case rather than carried as a flag.
 * Every terminal case closes except `createWithAttendees` and
 * `createThenUpload`, which stay open so per-file upload progress and retry
 * have somewhere to live (#132, ADR-0040).
 */
export type SavePlan =
  /** Nothing is written and the dialog stays open. `reason` is what
   * distinguishes the four refusals from each other; the modal reads only
   * `kind`, to disable Save. */
  | { kind: "blocked"; reason: BlockedReason }
  /** Nothing to write — close. The no-op save on a recurring Occurrence
   * (#141): prompting for a scope that controls nothing is worse than
   * doing nothing, e.g. a save where only an Attachment changed, which
   * applies to the Master immediately and independently of Save (ADR-0040). */
  | { kind: "close" }
  /** The acting User's own Reminders, and nothing else — the one write a
   * read-only Event still accepts (#211, ADR-0064). No scope picker: a
   * read-only caller can't create an Override to scope "this event" to, so
   * `eventId` (the Master for an unoverridden instance, or the existing
   * Override row) is the only row there is to write to. */
  | { kind: "writeReminders"; eventId: string; reminders: Reminder[] }
  /** Nothing staged: the optimistic fire-and-forget create, then close. */
  | { kind: "createAndClose"; event: Event }
  /** Attendees staged (#187, ADR-0055): the create must be awaited before
   * anything is painted on the grid, and its rejection shown as the
   * Attendees section's own banner rather than a toast. `pendingAttachments`
   * may be empty — in which case the modal closes on success like
   * `createAndClose`, and otherwise stays open like `createThenUpload`. */
  | {
      kind: "createWithAttendees";
      event: Event;
      attendees: StagedCreateAttendees;
      pendingAttachments: PendingAttachment[];
    }
  /** Attachments staged but no Attendees (#132, ADR-0040): the row must
   * exist before an upload can reference it, so the create is awaited, and
   * the dialog stays open for upload progress. */
  | { kind: "createThenUpload"; event: Event; pendingAttachments: PendingAttachment[] }
  /** A non-recurring Event's plain update. `reminders` is present only when
   * the acting User actually touched their own draft (ADR-0064) — an
   * untouched save writes none rather than resetting fired-history for no
   * reason. */
  | {
      kind: "updateEvent";
      id: string;
      changes: MasterFieldChanges;
      reminders: Reminder[] | undefined;
    }
  /** A scoped edit of a recurring Occurrence, with the scope now settled —
   * either chosen by the User, or forced to "all" by a rule change. */
  | {
      kind: "editSeries";
      scope: EditScope;
      changes: MasterFieldChanges;
      reminders: Reminder[] | undefined;
    }
  /** Ask which Occurrences this edit applies to, then plan again with the
   * answer as `resolvedScope`. */
  | { kind: "askScope" }
  /** A rule change forces "all" and invalidates the series' existing
   * Overrides/Exceptions (ADR-0016) — warn first, then plan again with
   * `discardConfirmed`. Only raised when there is actually something to
   * lose. */
  | { kind: "confirmDiscard" };

/**
 * Why a save was refused. Never rendered — the modal derives Save's disabled
 * state from `kind` alone — but it is what keeps the four refusals separable
 * at this interface, so a test can assert *which* one fired rather than just
 * that something did.
 */
export type BlockedReason =
  /** Title empty, no Calendar selected, or end at-or-before start. */
  | "invalid"
  /** A read-only Event whose Reminders draft matches what the modal opened
   * with: there is nothing this caller may write. */
  | "readOnlyUnchanged"
  /** A create has already succeeded in this modal session — Enter must not
   * create a second Event while its Attachments finish uploading. */
  | "alreadyCreated"
  /** A create carrying staged Attendees is still awaiting its response. */
  | "inFlight";

/** One picked-or-dropped file that has no Event row to upload against yet
 * (ADR-0040). `draftId` is the row it is already rendering as, so the
 * modal's upload writes progress back onto the same row. */
export interface PendingAttachment {
  draftId: string;
  file: File;
}

/**
 * A create-mode invite that hasn't been sent yet (#187, ADR-0055, #200,
 * ADR-0058). Declared here rather than imported from
 * `calendar-grid/EventAttendeesSection`, which owns the picker that produces
 * one: this module may not depend on a component. That section's own
 * identical declaration should re-point here once it is next touched.
 */
export type StagedAttendeeTarget =
  | { kind: "user"; userId: number; name: string }
  | { kind: "group"; groupId: number; name: string }
  | { kind: "email"; email: string };

interface CommonSaveInput {
  /** The form's fields, already normalized — the same shape a scoped edit
   * travels in, so this module and `recurrenceScope` speak one vocabulary.
   * Time strings and the Repeat preset are the form's business and are
   * resolved before they get here. */
  changes: MasterFieldChanges;
  /** The acting User's own Reminders draft (ADR-0064). Never anyone else's,
   * and never part of an Event write. */
  reminders: Reminder[];
}

export interface CreateSaveInput extends CommonSaveInput {
  mode: "create";
  stagedAttendees: StagedAttendeeTarget[];
  pendingAttachments: PendingAttachment[];
  /** Whether this modal session's create POST has already succeeded. */
  hasCreated: boolean;
  /** Whether a create carrying staged Attendees is in flight right now. */
  isSaving: boolean;
}

export interface EditSaveInput extends CommonSaveInput {
  mode: "edit";
  /** The row this Occurrence renders — the Master for an unoverridden
   * instance, or the Override that replaced it. */
  eventId: string;
  /** Whether every Event-field write is refused for this caller: a
   * Subscribed Calendar, Viewer Access, or an Attendee-only Event
   * (#111, ADR-0034, ADR-0046). Their Reminders section stays live (#211). */
  isReadOnly: boolean;
  isRecurring: boolean;
  /** The series' rule, from the Master — an Override never carries one. */
  masterRrule: string | undefined;
  /** Whether the Master has Overrides or Exceptions that a rule change
   * would invalidate. */
  masterHasChildren: boolean;
  /** What the modal opened with, in the same normalized shape as `changes`,
   * so a no-op save can be recognized (#141). */
  original: MasterFieldChanges;
  originalReminders: Reminder[];
  /** Set when re-planning after the User answered the scope picker. */
  resolvedScope?: EditScope;
  /** Set when re-planning after the User confirmed the discard warning. */
  discardConfirmed?: boolean;
}

export type PlanEventSaveInput = CreateSaveInput | EditSaveInput;

/** The ambient values a plan needs but must not read for itself, so a test
 * can pin both. Defaulted, so production callers pass nothing. */
export interface PlanEventSaveDeps {
  newId?: () => string;
  viewerZone?: () => string;
}

/** Whether the acting User's own Reminders draft differs from what the modal
 * opened with — order-independent (ADR-0064). */
function remindersChanged(draft: Reminder[], original: Reminder[]): boolean {
  return !remindersEqual(draft, original);
}

/**
 * The three target kinds a staged invite list splits into, as the create
 * endpoint takes them (#187, ADR-0055): a Group id expands to its current
 * members server-side, and a bare email is resolved against the Calendar's
 * own Workspace, both inside the create's own transaction (#200, ADR-0058).
 */
function splitStagedAttendees(
  targets: StagedAttendeeTarget[],
): StagedCreateAttendees {
  return {
    attendeeUserIds: targets
      .filter((t): t is Extract<StagedAttendeeTarget, { kind: "user" }> => t.kind === "user")
      .map((t) => t.userId),
    attendeeGroupIds: targets
      .filter((t): t is Extract<StagedAttendeeTarget, { kind: "group" }> => t.kind === "group")
      .map((t) => t.groupId),
    attendeeEmails: targets
      .filter((t): t is Extract<StagedAttendeeTarget, { kind: "email" }> => t.kind === "email")
      .map((t) => t.email),
  };
}

/** Whether `changes` is savable at all: the same three conditions the Save
 * button has always been disabled by. All-day skips the range check entirely
 * — there are no time inputs to validate (ADR-0017). */
function isValid(changes: MasterFieldChanges): boolean {
  const timeRangeValid = Boolean(changes.allDay) || changes.end > changes.start;
  return changes.title.trim() !== "" && changes.calendarId !== "" && timeRangeValid;
}

/**
 * Pure planner: what pressing Save should do, given the form's current state.
 *
 * Recomputed from live state on every press rather than captured — which is
 * what lets the scope picker and the discard warning resume by calling this
 * again with `resolvedScope`/`discardConfirmed` set, instead of replaying a
 * payload frozen when Save was first pressed (ADR-0066). Both dialogs
 * therefore converge on the same `editSeries` case.
 *
 * Performs no effects and reads no ambient state: `newId` and `viewerZone`
 * are injectable for exactly that reason.
 */
export function planEventSave(
  input: PlanEventSaveInput,
  deps: PlanEventSaveDeps = {},
): SavePlan {
  const newId = deps.newId ?? (() => crypto.randomUUID());
  const resolveViewerZone = deps.viewerZone ?? viewerZone;

  if (input.mode === "edit") {
    // Checked before validity: a read-only Event's other fields are disabled
    // but populated, and this path never goes near them (#211, ADR-0064).
    if (input.isReadOnly) {
      if (!remindersChanged(input.reminders, input.originalReminders)) {
        return { kind: "blocked", reason: "readOnlyUnchanged" };
      }
      return {
        kind: "writeReminders",
        eventId: input.eventId,
        reminders: input.reminders,
      };
    }
  } else {
    // Re-entrancy guards, ahead of validity only so that the more specific
    // reason is the one reported — all four refusals behave identically.
    if (input.hasCreated) return { kind: "blocked", reason: "alreadyCreated" };
    if (input.isSaving) return { kind: "blocked", reason: "inFlight" };
  }

  if (!isValid(input.changes)) return { kind: "blocked", reason: "invalid" };

  if (input.mode === "create") {
    // A newly created Event is stamped with the creator's Viewer zone; an
    // all-day Event stays timezone-free (ADR-0017, ADR-0019).
    const event: Event = {
      id: newId(),
      calendarId: input.changes.calendarId,
      title: input.changes.title,
      start: input.changes.start,
      end: input.changes.end,
      allDay: input.changes.allDay,
      rrule: input.changes.rrule,
      tzid: input.changes.allDay ? undefined : resolveViewerZone(),
      description: input.changes.description,
      location: input.changes.location,
      url: input.changes.url,
      // Picked field-by-field rather than spread, like `makeOverride`: an
      // explicit "reset to Calendar colour" travels as null through
      // EventFieldChanges, but an Event's own colour is absent-or-set
      // (ADR-0043).
      color: resolveColor(input.changes, { color: undefined }),
      reminders: input.reminders,
    };

    const hasStagedAttendees = input.stagedAttendees.length > 0;
    if (input.pendingAttachments.length === 0 && !hasStagedAttendees) {
      return { kind: "createAndClose", event };
    }
    if (hasStagedAttendees) {
      return {
        kind: "createWithAttendees",
        event,
        attendees: splitStagedAttendees(input.stagedAttendees),
        pendingAttachments: input.pendingAttachments,
      };
    }
    return {
      kind: "createThenUpload",
      event,
      pendingAttachments: input.pendingAttachments,
    };
  }

  // Reminders travel separately from `changes`, and only when they actually
  // differ from what the modal opened with (ADR-0064).
  const reminders = remindersChanged(input.reminders, input.originalReminders)
    ? input.reminders
    : undefined;

  if (!input.isRecurring) {
    // Deliberately not gated on `hasFieldChanges`: a non-recurring save has
    // always written unconditionally, since there is no scope to prompt for
    // and so nothing for a no-op check to spare the User.
    return {
      kind: "updateEvent",
      id: input.eventId,
      changes: input.changes,
      reminders,
    };
  }

  if (shouldDiscardChildren(input.masterRrule, input.changes.rrule)) {
    // A rule change is forced to "All events"; warn first, but only if there
    // is actually something to lose.
    if (input.masterHasChildren && !input.discardConfirmed) {
      return { kind: "confirmDiscard" };
    }
    return { kind: "editSeries", scope: "all", changes: input.changes, reminders };
  }

  // The rule is unchanged — ask which Occurrences the edit applies to, but
  // only once something actually differs from the Occurrence's current
  // values (#141).
  if (
    !hasFieldChanges(
      input.changes,
      input.original,
      input.reminders,
      input.originalReminders,
    )
  ) {
    return { kind: "close" };
  }

  if (input.resolvedScope === undefined) return { kind: "askScope" };

  return {
    kind: "editSeries",
    scope: input.resolvedScope,
    changes: input.changes,
    reminders,
  };
}
