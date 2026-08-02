import { useCalendarsStore } from "./calendarsStore";
import { useEventsStore } from "./eventsStore";
import { useShellStore } from "./shellStore";

export function deleteCalendarCascade(id: string): void {
  useEventsStore.getState().removeEventsByCalendarId(id);
  useCalendarsStore.getState().removeCalendar(id);
  useShellStore.getState().removeCheckedCalendarId(id);
}
