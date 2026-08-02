import { useCalendarsStore } from "./calendarsStore";
import { useEventsStore } from "./eventsStore";
import { useShellStore } from "./shellStore";

export async function deleteCalendarCascade(id: string): Promise<void> {
  const removedEvents = useEventsStore
    .getState()
    .events.filter((event) => event.calendarId === id);
  const wasChecked = useShellStore.getState().checkedCalendarIds.has(id);

  useEventsStore.getState().removeEventsByCalendarId(id);
  useShellStore.getState().removeCheckedCalendarId(id);

  const succeeded = await useCalendarsStore.getState().removeCalendar(id);
  if (succeeded) return;

  // The calendar delete was rolled back — undo the local cascade too, or
  // the calendar reappears with its events and checked state gone for good.
  useEventsStore.setState((state) => ({
    events: [...state.events, ...removedEvents],
  }));
  if (wasChecked) {
    useShellStore.getState().toggleCalendarChecked(id);
  }
}
