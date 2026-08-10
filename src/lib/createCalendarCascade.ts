import { useCalendarsStore } from "./calendarsStore";
import { useShellStore } from "./shellStore";
import type { Calendar } from "./calendar";

// createCalendarCascade checks calendar the moment it's created — a Calendar
// the caller just created is an explicit act, not the "auto-check things I
// haven't seen" heuristic reconcileCheckedCalendarIds applies on refetch, so
// it must be checked immediately (#175). Marking it known alongside checked
// (via addCheckedCalendarId, not toggleCalendarChecked) matters just as
// much: without it, the next reconcile treats the id as unseen and
// re-checks it even if the caller had since deliberately unchecked it.
// addCalendar is optimistic with its own rollback on failure — the checked
// state follows the same discipline, rolling back here too.
export async function createCalendarCascade(calendar: Calendar): Promise<void> {
  useShellStore.getState().addCheckedCalendarId(calendar.id);

  const succeeded = await useCalendarsStore.getState().addCalendar(calendar);
  if (succeeded) return;

  useShellStore.getState().removeCheckedCalendarId(calendar.id);
}
