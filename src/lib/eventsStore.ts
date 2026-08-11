import { create } from "zustand";
import { ApiError } from "./apiClient";
import { useAuthStore } from "./authStore";
import { accessChangeMessage } from "./calendarsStore";
import type { Event } from "./event";
import { eventsApi } from "./eventsApi";
import { resolveMaster, type Occurrence } from "./occurrence";
import type { EditScope } from "./recurrenceScope";
import {
  applySeriesOps,
  dispatchSeriesOps,
  planDeleteOccurrence,
  planEditOccurrence,
  type MasterFieldChanges,
} from "./seriesOperation";
import { toast } from "./toast";

type EventChanges = Partial<Omit<Event, "id">>;

// StagedCreateAttendees is Create's optional Attendee payload (#187,
// ADR-0055): a Group id expands to its current members server-side, inside
// the same transaction as the Event row itself.
export interface StagedCreateAttendees {
  attendeeUserIds?: number[];
  attendeeGroupIds?: number[];
}

function hasStagedAttendees(attendees: StagedCreateAttendees | undefined): boolean {
  return Boolean(attendees?.attendeeUserIds?.length || attendees?.attendeeGroupIds?.length);
}

interface EventsState {
  events: Event[];
  fetchEvents: () => Promise<void>;
  /** Creates event. With no Attendees staged, this is the existing
   * fire-and-forget optimistic path: insert first, roll back and toast on
   * failure. With Attendees staged, the create is awaited before anything
   * is painted on the grid, and a failure is rethrown (never toasted) so
   * the caller — EventModal, which needs to show its own banner and keep
   * the dialog open — can react to it directly (#187, ADR-0055). */
  addEvent: (event: Event, attendees?: StagedCreateAttendees) => Promise<void>;
  updateEvent: (id: string, changes: EventChanges) => Promise<void>;
  removeEvent: (id: string) => Promise<void>;
  removeEventsByCalendarId: (calendarId: string) => void;
  /** Applies a scoped edit (This event/This and following/All events) to a
   * recurring Occurrence (ADR-0016). `changes.rrule` only takes effect for
   * "all" — the backend then discards existing Overrides/Exceptions if the
   * rule changed. */
  editOccurrence: (occurrence: Occurrence, scope: EditScope, changes: MasterFieldChanges) => Promise<void>;
  /** Applies a scoped delete to a recurring Occurrence (ADR-0016). */
  deleteOccurrence: (occurrence: Occurrence, scope: EditScope) => Promise<void>;
}

// isAccessChangeError reports whether error is the shape the server uses for
// a write refused because the caller's Access changed underneath them — the
// Calendar's Share was revoked or downgraded (403 "forbidden") or the Event
// itself is gone (404 "not found") — as opposed to validation or network
// failure, which stay a generic "failed to ..." toast (#116).
function isAccessChangeError(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 403 || error.status === 404);
}

// handleWriteFailure is every write action's shared catch-block tail, after
// its own optimistic-state rollback: an access-change error explains itself
// by naming the Calendar and refetches Calendars (#116); anything else
// (validation, network) keeps the action's own generic message. calendarId
// is undefined only for removeEvent's already-gone-locally edge case, which
// falls back to the generic message since there's nothing to name. The
// Event dialog that triggered the write already closes unconditionally on
// its own, regardless of success or failure, so no separate dismissal is
// needed here.
async function handleWriteFailure(
  error: unknown,
  calendarId: string | undefined,
  fallbackMessage: string,
): Promise<void> {
  if (calendarId && isAccessChangeError(error)) {
    toast.error(await accessChangeMessage(calendarId));
  } else {
    toast.error(fallbackMessage);
  }
}

function requireAccessToken(): string {
  const accessToken = useAuthStore.getState().accessToken;
  if (!accessToken) throw new Error("Not authenticated.");
  return accessToken;
}

/** `resolveMaster` plus whether the Occurrence is itself an Override, and the
 * original start (RECURRENCE-ID) it replaces — distinct from
 * `occurrence.start` when it's already an Override that's been moved to a
 * different time. */
function resolveEditContext(
  events: Event[],
  occurrence: Occurrence,
): { master: Event; isOverride: boolean; originalStart: Date } | null {
  const master = resolveMaster(events, occurrence);
  if (!master) return null;

  const isOverride = Boolean(occurrence.event.parentId);
  const originalStart = isOverride ? occurrence.event.recurrenceId! : occurrence.start;
  return { master, isOverride, originalStart };
}

export const useEventsStore = create<EventsState>((set, get) => ({
  events: [],

  fetchEvents: async () => {
    const events = await eventsApi.list(requireAccessToken());
    set({ events });
  },

  addEvent: async (event, attendees) => {
    if (!hasStagedAttendees(attendees)) {
      set((state) => ({ events: [...state.events, event] }));

      try {
        await eventsApi.create(requireAccessToken(), event);
      } catch (error) {
        set((state) => ({
          events: state.events.filter((e) => e.id !== event.id),
        }));
        await handleWriteFailure(error, event.calendarId, `Failed to create event "${event.title}".`);
      }
      return;
    }

    // Attendees staged: await the create before painting anything on the
    // grid, so a rejected explicit target never shows an Event whose
    // invites silently didn't go out, and never discards what the user
    // typed by rolling back an optimistic insert (#187, ADR-0055). A
    // rejection is rethrown rather than toasted — EventModal shows its own
    // banner and keeps the dialog open.
    await eventsApi.create(requireAccessToken(), { ...event, ...attendees });
    set((state) => ({ events: [...state.events, event] }));
  },

  updateEvent: async (id, changes) => {
    const previousEvents = get().events;
    const current = previousEvents.find((event) => event.id === id);
    if (!current) return;

    const updated: Event = { ...current, ...changes };
    set((state) => ({
      events: state.events.map((event) => (event.id === id ? updated : event)),
    }));

    try {
      await eventsApi.update(requireAccessToken(), id, {
        calendarId: updated.calendarId,
        title: updated.title,
        start: updated.start,
        end: updated.end,
        allDay: updated.allDay,
        rrule: updated.rrule,
        tzid: updated.tzid,
        reminders: updated.reminders,
        description: updated.description,
        location: updated.location,
        color: updated.color,
      });
    } catch (error) {
      set({ events: previousEvents });
      await handleWriteFailure(error, updated.calendarId, "Failed to update event.");
    }
  },

  removeEvent: async (id) => {
    const previousEvents = get().events;
    const current = previousEvents.find((event) => event.id === id);
    set((state) => ({
      events: state.events.filter((event) => event.id !== id),
    }));

    try {
      await eventsApi.remove(requireAccessToken(), id);
    } catch (error) {
      set({ events: previousEvents });
      await handleWriteFailure(error, current?.calendarId, "Failed to delete event.");
    }
  },

  removeEventsByCalendarId: (calendarId) =>
    set((state) => ({
      events: state.events.filter((event) => event.calendarId !== calendarId),
    })),

  editOccurrence: async (occurrence, scope, changes) => {
    const previousEvents = get().events;
    const resolved = resolveEditContext(previousEvents, occurrence);
    if (!resolved) return;
    const { master, isOverride, originalStart } = resolved;
    const accessToken = requireAccessToken();

    const ops = planEditOccurrence({
      master,
      occurrence,
      isOverride,
      originalStart,
      scope,
      changes,
    });
    set((state) => ({ events: applySeriesOps(state.events, ops) }));

    try {
      await dispatchSeriesOps(accessToken, ops);
    } catch (error) {
      set({ events: previousEvents });
      await handleWriteFailure(error, master.calendarId, "Failed to update event.");
    }
  },

  deleteOccurrence: async (occurrence, scope) => {
    const previousEvents = get().events;
    const resolved = resolveEditContext(previousEvents, occurrence);
    if (!resolved) return;
    const { master, isOverride, originalStart } = resolved;
    const accessToken = requireAccessToken();

    const ops = planDeleteOccurrence({
      events: previousEvents,
      master,
      occurrence,
      isOverride,
      originalStart,
      scope,
    });
    set((state) => ({ events: applySeriesOps(state.events, ops) }));

    try {
      await dispatchSeriesOps(accessToken, ops);
    } catch (error) {
      set({ events: previousEvents });
      await handleWriteFailure(error, master.calendarId, "Failed to delete event.");
    }
  },
}));
