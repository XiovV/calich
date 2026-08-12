import { useEffect, useState } from "react";
import { startOfDay } from "date-fns";
import { Outlet } from "react-router";
import { TopBar } from "./TopBar";
import { Sidebar } from "./Sidebar";
import { CalendarView } from "../../calendar-grid/CalendarView";
import { EventModal } from "../../calendar-grid/EventModal";
import { computeDefaultDraft, type DraftBlock } from "../../lib/gridTime";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useEventsStore } from "../../lib/eventsStore";
import { useShellStore } from "../../lib/shellStore";
import { useWorkspacesStore } from "../../lib/workspacesStore";
import { occurrenceKey, type Occurrence } from "../../lib/occurrence";

type EventModalState =
  | { mode: "create"; day: Date; draft: DraftBlock }
  | { mode: "edit"; occurrence: Occurrence }
  | null;

export function AppShell() {
  const [eventModalState, setEventModalState] = useState<EventModalState>(null);
  const fetchCalendars = useCalendarsStore((state) => state.fetchCalendars);
  const fetchEvents = useEventsStore((state) => state.fetchEvents);
  const reconcileCheckedCalendarIds = useShellStore(
    (state) => state.reconcileCheckedCalendarIds,
  );
  const fetchWorkspaces = useWorkspacesStore((state) => state.fetchWorkspaces);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  useEffect(() => {
    fetchWorkspaces();
  }, [fetchWorkspaces]);

  useEffect(() => {
    // A click on an invite Notification (NotificationBell) requests an
    // Event by id rather than a specific Occurrence — an invite concerns
    // the whole series (ADR-0061) — so it's opened at its own start/end
    // rather than any particular Occurrence's. Subscribed directly (rather
    // than read via the hook and reacted to in an effect body) so opening
    // the modal happens in the subscription callback, not synchronously
    // during a render-driven effect.
    return useShellStore.subscribe((state) => {
      if (!state.requestedEventId) return;
      const event = useEventsStore
        .getState()
        .events.find((e) => e.id === state.requestedEventId);
      if (event) {
        setEventModalState({
          mode: "edit",
          occurrence: { event, start: event.start, end: event.end },
        });
      }
      useShellStore.getState().clearRequestedEventOpen();
    });
  }, []);

  useEffect(() => {
    // Refetches on mount, whenever the tab regains focus (so a Share granted
    // or revoked while it was in the background is reflected without a
    // reload, #116), and whenever the active Workspace changes (#155,
    // ADR-0045) — switching Workspaces must change the visible Calendar list
    // without a reload, same as any other externally-caused change to it.
    // reconcileCheckedCalendarIds (rather than replacing the checked set
    // outright) is what keeps a deliberately-unchecked Calendar unchecked
    // across a later refetch while still auto-checking one seen for the
    // first time.
    function refetch() {
      fetchCalendars().then(() => {
        reconcileCheckedCalendarIds(
          useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
        );
      });
      fetchEvents();
    }

    if (activeWorkspaceId === null) return;
    refetch();

    function handleVisibilityChange() {
      if (document.visibilityState === "visible") refetch();
    }
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () =>
      document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, [fetchCalendars, fetchEvents, reconcileCheckedCalendarIds, activeWorkspaceId]);

  function handleCreateClick() {
    const draft = computeDefaultDraft(new Date());
    setEventModalState({ mode: "create", day: startOfDay(draft.start), draft });
  }

  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="h-16 shrink-0 border-b border-border bg-surface">
        <TopBar />
      </header>
      <div className="flex flex-1 overflow-hidden">
        <nav className="w-72 shrink-0 border-r border-border bg-surface">
          <Sidebar onCreateClick={handleCreateClick} />
        </nav>
        <main className="m-3 flex-1 overflow-hidden rounded-shell-lg bg-surface shadow-elevation-1">
          <CalendarView
            onDraftCreated={(day, draft) =>
              setEventModalState({ mode: "create", day, draft })
            }
            onOccurrenceClick={(occurrence) =>
              setEventModalState({ mode: "edit", occurrence })
            }
          />
        </main>
      </div>
      {eventModalState?.mode === "create" && (
        <EventModal
          key={eventModalState.draft.start.getTime()}
          mode="create"
          day={eventModalState.day}
          draft={eventModalState.draft}
          onClose={() => setEventModalState(null)}
        />
      )}
      {eventModalState?.mode === "edit" && (
        <EventModal
          key={occurrenceKey(eventModalState.occurrence)}
          mode="edit"
          occurrence={eventModalState.occurrence}
          onClose={() => setEventModalState(null)}
        />
      )}
      <Outlet />
    </div>
  );
}
