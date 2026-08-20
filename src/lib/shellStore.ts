import { create } from "zustand";
import { useCalendarsStore } from "./calendarsStore";

export type ActiveView = "day" | "week" | "month" | "year";

interface ShellState {
  selectedDate: Date;
  activeView: ActiveView;
  checkedCalendarIds: Set<string>;
  // knownCalendarIds is the Calendar ids seen as of the last reconcile — the
  // record that lets it tell "wasn't there last time" (auto-check) apart
  // from "was there and deliberately unchecked" (leave alone), which
  // checkedCalendarIds alone can't distinguish once an id is unchecked
  // (#116). Deliberately a snapshot, not an ever-growing set: a Calendar
  // that disappears (revoked) drops out of it, so a later re-Share is
  // "unseen" again and gets auto-checked rather than staying invisible.
  knownCalendarIds: Set<string>;
  // requestedEventId is set by a click on an invite Notification (the
  // NotificationBell has no reach into AppShell's own eventModalState) and
  // cleared once AppShell has resolved it into an opened EventModal — a
  // one-shot request, not an ongoing "which event is open" record.
  requestedEventId: string | null;
  requestEventOpen: (eventId: string) => void;
  clearRequestedEventOpen: () => void;
  setSelectedDate: (date: Date) => void;
  setActiveView: (view: ActiveView) => void;
  setCheckedCalendarIds: (ids: Iterable<string>) => void;
  // reconcileCheckedCalendarIds auto-checks only ids not previously known —
  // e.g. a Calendar shared with the caller while the tab was in the
  // background — so a Calendar the caller deliberately unchecked stays
  // unchecked across a focus refetch (#116). Unlike setCheckedCalendarIds,
  // never removes an id: a Calendar that disappears (e.g. a revoked Share)
  // is filtered out by getCheckedCalendars via the calendars list itself.
  reconcileCheckedCalendarIds: (ids: Iterable<string>) => void;
  toggleCalendarChecked: (id: string) => void;
  removeCheckedCalendarId: (id: string) => void;
  // addCheckedCalendarId force-checks id and, unlike toggleCalendarChecked,
  // also marks it known — for a Calendar the caller just created or
  // Subscribed to, which must count as "already seen" so a later reconcile
  // doesn't treat it as unseen and re-check it after a deliberate uncheck
  // (#175).
  addCheckedCalendarId: (id: string) => void;
}

export const useShellStore = create<ShellState>((set) => ({
  selectedDate: new Date(),
  activeView: "week",
  checkedCalendarIds: new Set(
    useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
  ),
  knownCalendarIds: new Set(
    useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
  ),
  requestedEventId: null,
  requestEventOpen: (eventId) => set({ requestedEventId: eventId }),
  clearRequestedEventOpen: () => set({ requestedEventId: null }),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setActiveView: (view) => set({ activeView: view }),
  setCheckedCalendarIds: (ids) => set({ checkedCalendarIds: new Set(ids) }),
  reconcileCheckedCalendarIds: (ids) =>
    set((state) => {
      const idList = Array.from(ids);
      const nextChecked = new Set(state.checkedCalendarIds);
      for (const id of idList) {
        if (!state.knownCalendarIds.has(id)) {
          nextChecked.add(id);
        }
      }
      return { checkedCalendarIds: nextChecked, knownCalendarIds: new Set(idList) };
    }),
  toggleCalendarChecked: (id) =>
    set((state) => {
      const next = new Set(state.checkedCalendarIds);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return { checkedCalendarIds: next };
    }),
  removeCheckedCalendarId: (id) =>
    set((state) => {
      const next = new Set(state.checkedCalendarIds);
      next.delete(id);
      return { checkedCalendarIds: next };
    }),
  addCheckedCalendarId: (id) =>
    set((state) => ({
      checkedCalendarIds: new Set(state.checkedCalendarIds).add(id),
      knownCalendarIds: new Set(state.knownCalendarIds).add(id),
    })),
}));

// Shared by every caller that refetches Calendars and needs the checked set
// to pick up whatever changed — a newly-Shared Calendar, or (#229) one an
// Import just created — without disturbing a Calendar the caller had
// deliberately unchecked. reconcileCheckedCalendarIds (rather than
// setCheckedCalendarIds) is what gives that guarantee; see its own comment.
export async function refetchCalendarsAndReconcile(): Promise<void> {
  await useCalendarsStore.getState().fetchCalendars();
  useShellStore
    .getState()
    .reconcileCheckedCalendarIds(
      useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
    );
}
