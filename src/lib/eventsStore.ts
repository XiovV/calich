import { create } from "zustand";
import { useAuthStore } from "./authStore";
import type { Event } from "./event";
import { eventsApi } from "./eventsApi";
import { resolveMaster, seriesEditChanges, type Occurrence } from "./occurrence";
import {
  makeException,
  makeOverride,
  resolveAllDay,
  shouldDiscardChildren,
  splitFollowing,
  truncateSeriesBefore,
  type EditScope,
  type EventFieldChanges,
} from "./recurrenceScope";
import { toast } from "./toast";

type EventChanges = Partial<Omit<Event, "id">>;

interface EventsState {
  events: Event[];
  fetchEvents: () => Promise<void>;
  addEvent: (event: Event) => Promise<void>;
  updateEvent: (id: string, changes: EventChanges) => Promise<void>;
  removeEvent: (id: string) => Promise<void>;
  removeEventsByCalendarId: (calendarId: string) => void;
  /** Applies a scoped edit (This event/This and following/All events) to a
   * recurring Occurrence (ADR-0016). `changes.rrule` only takes effect for
   * "all" — the backend then discards existing Overrides/Exceptions if the
   * rule changed. */
  editOccurrence: (
    occurrence: Occurrence,
    scope: EditScope,
    changes: EventFieldChanges & { rrule?: string },
  ) => Promise<void>;
  /** Applies a scoped delete to a recurring Occurrence (ADR-0016). */
  deleteOccurrence: (occurrence: Occurrence, scope: EditScope) => Promise<void>;
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

/** Splits `dates` into [before boundary, at-or-after boundary]. */
function partitionByBoundary(dates: Date[], boundary: Date): [Date[], Date[]] {
  const before: Date[] = [];
  const atOrAfter: Date[] = [];
  for (const date of dates) {
    (date >= boundary ? atOrAfter : before).push(date);
  }
  return [before, atOrAfter];
}

export const useEventsStore = create<EventsState>((set, get) => ({
  events: [],

  fetchEvents: async () => {
    const events = await eventsApi.list(requireAccessToken());
    set({ events });
  },

  addEvent: async (event) => {
    set((state) => ({ events: [...state.events, event] }));

    try {
      await eventsApi.create(requireAccessToken(), event);
    } catch {
      set((state) => ({
        events: state.events.filter((e) => e.id !== event.id),
      }));
      toast.error(`Failed to create event "${event.title}".`);
    }
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
      });
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to update event.");
    }
  },

  removeEvent: async (id) => {
    const previousEvents = get().events;
    set((state) => ({
      events: state.events.filter((event) => event.id !== id),
    }));

    try {
      await eventsApi.remove(requireAccessToken(), id);
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to delete event.");
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

    if (scope === "all") {
      // The whole series shifts by the delta `occurrence` moved by (see
      // seriesEditChanges) — this covers both a drag (an explicit new
      // date+time) and the modal (a time-only edit, so the delta has no day
      // component and the master's own date survives untouched).
      const seriesChanges = seriesEditChanges(master, occurrence, changes.start, changes.end);
      // "rrule" may be present-but-undefined (the user picked "Does not
      // repeat"), which must win over falling back to seriesChanges' own
      // reanchored rule — hence an `in` check rather than `??`.
      const rrule = "rrule" in changes ? changes.rrule : seriesChanges.rrule;
      const discardsChildren = shouldDiscardChildren(master.rrule, rrule);
      const patch = {
        calendarId: changes.calendarId,
        title: changes.title,
        start: seriesChanges.start,
        end: seriesChanges.end,
        allDay: resolveAllDay(changes, master),
        rrule,
      };
      const updatedMaster: Event = { ...master, ...patch };
      set((state) => ({
        events: state.events
          .map((event) => (event.id === master.id ? updatedMaster : event))
          .filter((event) => !discardsChildren || event.parentId !== master.id),
      }));

      try {
        await eventsApi.update(accessToken, master.id, patch);
      } catch {
        set({ events: previousEvents });
        toast.error("Failed to update event.");
      }
      return;
    }

    if (scope === "following") {
      const { truncatedMaster, newMaster, reparentFromStart } = splitFollowing(
        master,
        originalStart,
        changes,
      );
      const newMasterId = crypto.randomUUID();
      const [keptExdates, movedExdates] = partitionByBoundary(
        master.exdates ?? [],
        reparentFromStart,
      );
      const updatedOldMaster: Event = {
        ...master,
        rrule: truncatedMaster.rrule,
        exdates: keptExdates,
      };
      const createdNewMaster: Event = {
        id: newMasterId,
        ...newMaster,
        exdates: movedExdates,
      };

      set((state) => ({
        events: state.events
          .map((event) => (event.id === master.id ? updatedOldMaster : event))
          .concat(createdNewMaster)
          .map((event) =>
            event.parentId === master.id &&
            event.recurrenceId &&
            event.recurrenceId >= reparentFromStart
              ? { ...event, parentId: newMasterId }
              : event,
          ),
      }));

      try {
        await eventsApi.update(accessToken, master.id, {
          calendarId: master.calendarId,
          title: master.title,
          start: master.start,
          end: master.end,
          rrule: truncatedMaster.rrule,
        });
        await eventsApi.create(accessToken, { id: newMasterId, ...newMaster });
        await eventsApi.reparentSeries(
          accessToken,
          master.id,
          newMasterId,
          reparentFromStart,
        );
      } catch {
        set({ events: previousEvents });
        toast.error("Failed to update event.");
      }
      return;
    }

    // scope === "this"
    if (isOverride) {
      // An Override never carries a rule of its own (ADR-0016) — strip
      // `changes.rrule` (carried for the "all" scope's use) before updating it.
      await get().updateEvent(occurrence.event.id, {
        calendarId: changes.calendarId,
        title: changes.title,
        start: changes.start,
        end: changes.end,
        allDay: resolveAllDay(changes, master),
      });
      return;
    }

    const override = makeOverride(master, originalStart, changes);
    const overrideId = crypto.randomUUID();
    const createdOverride: Event = { id: overrideId, ...override };
    set((state) => ({ events: [...state.events, createdOverride] }));

    try {
      await eventsApi.create(accessToken, { id: overrideId, ...override });
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to update event.");
    }
  },

  deleteOccurrence: async (occurrence, scope) => {
    const previousEvents = get().events;
    const resolved = resolveEditContext(previousEvents, occurrence);
    if (!resolved) return;
    const { master, isOverride, originalStart } = resolved;
    const accessToken = requireAccessToken();

    if (scope === "all") {
      await get().removeEvent(master.id);
      // The backend cascades this deletion to Overrides/Exceptions; mirror it
      // locally so a stray Override doesn't linger in the cache unrendered.
      set((state) => ({
        events: state.events.filter((event) => event.parentId !== master.id),
      }));
      return;
    }

    if (scope === "following") {
      const truncatedRrule = truncateSeriesBefore(master, originalStart);
      const orphaned = previousEvents.filter(
        (event) =>
          event.parentId === master.id &&
          event.recurrenceId &&
          event.recurrenceId >= originalStart,
      );
      const [keptExdates] = partitionByBoundary(master.exdates ?? [], originalStart);

      set((state) => ({
        events: state.events
          .map((event) =>
            event.id === master.id
              ? { ...event, rrule: truncatedRrule, exdates: keptExdates }
              : event,
          )
          .filter((event) => !orphaned.some((o) => o.id === event.id)),
      }));

      try {
        await eventsApi.update(accessToken, master.id, {
          calendarId: master.calendarId,
          title: master.title,
          start: master.start,
          end: master.end,
          rrule: truncatedRrule,
        });
        for (const event of orphaned) {
          await eventsApi.remove(accessToken, event.id);
        }
      } catch {
        set({ events: previousEvents });
        toast.error("Failed to delete event.");
      }
      return;
    }

    // scope === "this"
    if (isOverride) {
      await get().removeEvent(occurrence.event.id);
      return;
    }

    const exception = makeException(master, originalStart);
    const updatedMaster: Event = {
      ...master,
      exdates: [...(master.exdates ?? []), exception.occurrenceStart],
    };
    set((state) => ({
      events: state.events.map((event) => (event.id === master.id ? updatedMaster : event)),
    }));

    try {
      await eventsApi.addException(accessToken, exception.parentId, exception.occurrenceStart);
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to delete event.");
    }
  },
}));
