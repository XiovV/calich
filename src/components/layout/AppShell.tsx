import { useEffect, useState } from "react";
import { startOfDay } from "date-fns";
import { TopBar } from "./TopBar";
import { Sidebar } from "./Sidebar";
import { Toaster } from "../ui/Toaster";
import { CalendarView } from "../../calendar-grid/CalendarView";
import { EventModal } from "../../calendar-grid/EventModal";
import { computeDefaultDraft, type DraftBlock } from "../../lib/gridTime";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useShellStore } from "../../lib/shellStore";
import type { Event } from "../../lib/mockEvents";

type EventModalState =
  | { mode: "create"; day: Date; draft: DraftBlock }
  | { mode: "edit"; event: Event }
  | null;

export function AppShell() {
  const [eventModalState, setEventModalState] = useState<EventModalState>(null);
  const fetchCalendars = useCalendarsStore((state) => state.fetchCalendars);
  const setCheckedCalendarIds = useShellStore(
    (state) => state.setCheckedCalendarIds,
  );

  useEffect(() => {
    fetchCalendars().then(() => {
      setCheckedCalendarIds(
        useCalendarsStore.getState().calendars.map((calendar) => calendar.id),
      );
    });
  }, [fetchCalendars, setCheckedCalendarIds]);

  function handleCreateClick() {
    const draft = computeDefaultDraft(new Date());
    setEventModalState({ mode: "create", day: startOfDay(draft.start), draft });
  }

  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="h-16 shrink-0 border-b border-border bg-surface shadow-elevation-1">
        <TopBar />
      </header>
      <div className="flex flex-1 overflow-hidden">
        <nav className="w-72 shrink-0 border-r border-border bg-surface-sunken">
          <Sidebar onCreateClick={handleCreateClick} />
        </nav>
        <main className="m-3 flex-1 overflow-hidden rounded-shell-lg bg-surface shadow-elevation-1">
          <CalendarView
            onDraftCreated={(day, draft) =>
              setEventModalState({ mode: "create", day, draft })
            }
            onEventClick={(event) => setEventModalState({ mode: "edit", event })}
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
          key={eventModalState.event.id}
          mode="edit"
          event={eventModalState.event}
          onClose={() => setEventModalState(null)}
        />
      )}
      <Toaster />
    </div>
  );
}
