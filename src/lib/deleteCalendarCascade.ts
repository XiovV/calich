import { useCalendarsStore } from "./calendarsStore";
import { useEventsStore } from "./eventsStore";
import { useShellStore } from "./shellStore";

// Returns whether the calendar was actually deleted. removeCalendar signals
// failure by return value (it catches the error, reverts the store, and
// shows a toast), never by throwing — so a caller that needs to know cannot
// rely on try/catch and must read this boolean.
export async function deleteCalendarCascade(id: string): Promise<boolean> {
  const removedEvents = useEventsStore
    .getState()
    .events.filter((event) => event.calendarId === id);
  const wasChecked = useShellStore.getState().checkedCalendarIds.has(id);

  useEventsStore.getState().removeEventsByCalendarId(id);
  useShellStore.getState().removeCheckedCalendarId(id);

  const succeeded = await useCalendarsStore.getState().removeCalendar(id);
  if (succeeded) return true;

  // The calendar delete was rolled back — undo the local cascade too, or
  // the calendar reappears with its events and checked state gone for good.
  useEventsStore.setState((state) => ({
    events: [...state.events, ...removedEvents],
  }));
  if (wasChecked) {
    useShellStore.getState().toggleCalendarChecked(id);
  }
  return false;
}
