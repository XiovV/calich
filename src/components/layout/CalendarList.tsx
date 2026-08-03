import { useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Checkbox } from "../ui/Checkbox";
import { IconButton } from "../ui/IconButton";
import type { Calendar } from "../../lib/calendar";
import { getCalendarColorClass } from "../../lib/calendarColors";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useEventsStore } from "../../lib/eventsStore";
import { useShellStore } from "../../lib/shellStore";
import { deleteCalendarCascade } from "../../lib/deleteCalendarCascade";
import { CalendarModal } from "./CalendarModal";
import { DeleteCalendarConfirmation } from "./DeleteCalendarConfirmation";

export function CalendarList() {
  const calendars = useCalendarsStore((state) => state.calendars);
  const events = useEventsStore((state) => state.events);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const toggleCalendarChecked = useShellStore(
    (state) => state.toggleCalendarChecked,
  );
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingCalendar, setEditingCalendar] = useState<Calendar | null>(null);
  const [deletingCalendarId, setDeletingCalendarId] = useState<string | null>(
    null,
  );

  const deletingCalendar = calendars.find(
    (calendar) => calendar.id === deletingCalendarId,
  );

  function handleConfirmDelete() {
    if (!deletingCalendar) return;
    deleteCalendarCascade(deletingCalendar.id);
    setDeletingCalendarId(null);
  }

  return (
    <div>
      <div className="flex items-center justify-between py-2 ps-5 pe-2">
        <p className="text-label-sm font-medium text-ink-muted">
          My calendars
        </p>
        <IconButton
          size="tiny"
          onClick={() => setIsCreateOpen(true)}
          aria-label="Add calendar"
        >
          <Plus className="size-4" />
        </IconButton>
      </div>
      <ul>
        {calendars.map((calendar) => (
          <li
            key={calendar.id}
            className="group flex items-center gap-2 rounded-e-full py-2 ps-5 pe-2 transition-colors hover:bg-surface-hover"
          >
            <span
              aria-hidden="true"
              className={`size-2.5 shrink-0 rounded-shell-pill ${getCalendarColorClass(calendar.color)}`}
            />
            <span className="flex-1 truncate text-body text-ink">
              {calendar.name}
            </span>
            <IconButton
              size="tiny"
              onClick={() => setEditingCalendar(calendar)}
              aria-label={`Edit ${calendar.name}`}
              className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
            >
              <Pencil className="size-3.5" />
            </IconButton>
            <IconButton
              size="tiny"
              onClick={() => setDeletingCalendarId(calendar.id)}
              aria-label={`Delete ${calendar.name}`}
              className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
            >
              <Trash2 className="size-3.5" />
            </IconButton>
            <Checkbox
              checked={checkedCalendarIds.has(calendar.id)}
              onCheckedChange={() => toggleCalendarChecked(calendar.id)}
              aria-label={calendar.name}
            />
          </li>
        ))}
      </ul>
      {isCreateOpen && (
        <CalendarModal mode="create" onClose={() => setIsCreateOpen(false)} />
      )}
      {editingCalendar && (
        <CalendarModal
          mode="edit"
          calendar={editingCalendar}
          onClose={() => setEditingCalendar(null)}
        />
      )}
      {deletingCalendar && (
        <DeleteCalendarConfirmation
          calendar={deletingCalendar}
          eventCount={
            events.filter((event) => event.calendarId === deletingCalendar.id)
              .length
          }
          onConfirm={handleConfirmDelete}
          onClose={() => setDeletingCalendarId(null)}
        />
      )}
    </div>
  );
}
