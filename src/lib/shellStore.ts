import { create } from "zustand";
import { useCalendarsStore } from "./calendarsStore";
import { useTaskListsStore } from "./taskListsStore";
import { reconcileCheckedIds } from "./reconcileCheckedIds";

export type ActiveView = "day" | "week" | "month" | "year";

interface ShellState {
  selectedDate: Date;
  activeView: ActiveView;
  // activeCalendarSetId is the Active Calendar Set (#303, ADR-0082): null
  // means "All calendars", the absence of a Set rather than a row of its
  // own. Session state like selectedDate and activeView, but with no
  // reconcile behaviour of its own and no effect on checkedCalendarIds —
  // narrowing what exists and narrowing what's toggled on are kept
  // orthogonal. See inScopeCalendars.
  activeCalendarSetId: number | null;
  checkedCalendarIds: Set<string>;
  // knownCalendarIds is the Calendar ids seen as of the last reconcile — the
  // record that lets it tell "wasn't there last time" (auto-check) apart
  // from "was there and deliberately unchecked" (leave alone), which
  // checkedCalendarIds alone can't distinguish once an id is unchecked
  // (#116). Deliberately a snapshot, not an ever-growing set: a Calendar
  // that disappears (revoked) drops out of it, so a later re-Share is
  // "unseen" again and gets auto-checked rather than staying invisible.
  knownCalendarIds: Set<string>;
  // tasksPanelOpen is the Tasks panel's own open/closed state (#317,
  // ADR-0083): closed by default, session state like activeCalendarSetId
  // with no Preference behind it and nothing persisted between loads.
  tasksPanelOpen: boolean;
  // checkedTaskListIds/knownTaskListIds are the Lists filter's own
  // checked/known pair — the Task List counterpart to
  // checkedCalendarIds/knownCalendarIds, carrying the identical reconcile
  // rule via reconcileCheckedIds (#317, ADR-0083).
  checkedTaskListIds: Set<number>;
  knownTaskListIds: Set<number>;
  // showCompletedTasks is "Show completed" (#310, ADR-0083): session state
  // like tasksPanelOpen, closed (hidden) by default and with no Preference
  // behind it — reopening the panel later starts hidden again.
  showCompletedTasks: boolean;
  // requestedEventId is set by a click on an invite Notification (the
  // NotificationBell has no reach into AppShell's own eventModalState) and
  // cleared once AppShell has resolved it into an opened EventModal — a
  // one-shot request, not an ongoing "which event is open" record.
  requestedEventId: string | null;
  requestEventOpen: (eventId: string) => void;
  clearRequestedEventOpen: () => void;
  setSelectedDate: (date: Date) => void;
  setActiveView: (view: ActiveView) => void;
  setActiveCalendarSetId: (id: number | null) => void;
  setTasksPanelOpen: (open: boolean) => void;
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
  // reconcileCheckedTaskListIds/toggleTaskListChecked/addCheckedTaskListId
  // are checkedCalendarIds' three counterparts above, over Task List ids
  // (#317, ADR-0083).
  reconcileCheckedTaskListIds: (ids: Iterable<number>) => void;
  toggleTaskListChecked: (id: number) => void;
  addCheckedTaskListId: (id: number) => void;
  setShowCompletedTasks: (show: boolean) => void;
}

export const useShellStore = create<ShellState>((set) => ({
  selectedDate: new Date(),
  activeView: "week",
  activeCalendarSetId: null,
  checkedCalendarIds: new Set(
    useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
  ),
  knownCalendarIds: new Set(
    useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
  ),
  tasksPanelOpen: false,
  // Unlike checkedCalendarIds/knownCalendarIds above, not seeded from the
  // store at module-init time: taskListsStore sits downstream of authStore,
  // which itself imports shellStore, so reading it synchronously here would
  // close a circular import the wrong way round depending on which module
  // loads first. Starting empty costs nothing — taskListsStore itself
  // starts empty until refetchTaskListsAndReconcile below runs — and the
  // import stays safe because it's only ever touched lazily, inside that
  // function's body.
  checkedTaskListIds: new Set<number>(),
  knownTaskListIds: new Set<number>(),
  showCompletedTasks: false,
  requestedEventId: null,
  requestEventOpen: (eventId) => set({ requestedEventId: eventId }),
  clearRequestedEventOpen: () => set({ requestedEventId: null }),
  setSelectedDate: (date) => set({ selectedDate: date }),
  setActiveView: (view) => set({ activeView: view }),
  setActiveCalendarSetId: (id) => set({ activeCalendarSetId: id }),
  setTasksPanelOpen: (open) => set({ tasksPanelOpen: open }),
  setCheckedCalendarIds: (ids) => set({ checkedCalendarIds: new Set(ids) }),
  reconcileCheckedCalendarIds: (ids) =>
    set((state) => {
      const { checkedIds, knownIds } = reconcileCheckedIds(
        state.knownCalendarIds,
        state.checkedCalendarIds,
        ids,
      );
      return { checkedCalendarIds: checkedIds, knownCalendarIds: knownIds };
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
  reconcileCheckedTaskListIds: (ids) =>
    set((state) => {
      const { checkedIds, knownIds } = reconcileCheckedIds(
        state.knownTaskListIds,
        state.checkedTaskListIds,
        ids,
      );
      return { checkedTaskListIds: checkedIds, knownTaskListIds: knownIds };
    }),
  toggleTaskListChecked: (id) =>
    set((state) => {
      const next = new Set(state.checkedTaskListIds);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return { checkedTaskListIds: next };
    }),
  addCheckedTaskListId: (id) =>
    set((state) => ({
      checkedTaskListIds: new Set(state.checkedTaskListIds).add(id),
      knownTaskListIds: new Set(state.knownTaskListIds).add(id),
    })),
  setShowCompletedTasks: (show) => set({ showCompletedTasks: show }),
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

// refetchTaskListsAndReconcile is refetchCalendarsAndReconcile's counterpart
// for Task Lists (#317, ADR-0083).
export async function refetchTaskListsAndReconcile(): Promise<void> {
  await useTaskListsStore.getState().fetchTaskLists();
  useShellStore
    .getState()
    .reconcileCheckedTaskListIds(
      useTaskListsStore.getState().taskLists.map((taskList) => taskList.id),
    );
}
