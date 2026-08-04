import { create } from "zustand";
import { useAuthStore } from "./authStore";
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
  editOccurrence: (occurrence: Occurrence, scope: EditScope, changes: MasterFieldChanges) => Promise<void>;
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
    } catch {
      set({ events: previousEvents });
      toast.error("Failed to delete event.");
    }
  },
}));
