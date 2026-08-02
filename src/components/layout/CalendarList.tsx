import { useState } from "react";
import { Plus } from "lucide-react";
import { Checkbox } from "../ui/Checkbox";
import { getCalendarColorClass } from "../../lib/calendarColors";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useShellStore } from "../../lib/shellStore";
import { CreateCalendarModal } from "./CreateCalendarModal";

export function CalendarList() {
  const calendars = useCalendarsStore((state) => state.calendars);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const toggleCalendarChecked = useShellStore(
    (state) => state.toggleCalendarChecked,
  );
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  return (
    <div>
      <div className="flex items-center justify-between px-4 py-1.5">
        <p className="text-label-sm font-medium text-ink-muted">
          My calendars
        </p>
        <button
          type="button"
          onClick={() => setIsCreateOpen(true)}
          aria-label="Add calendar"
          className="rounded-shell-pill p-1 text-ink-muted hover:bg-surface-hover"
        >
          <Plus className="size-4" />
        </button>
      </div>
      <ul>
        {calendars.map((calendar) => (
          <li key={calendar.id} className="flex items-center gap-2 px-4 py-1.5">
            <span
              aria-hidden="true"
              className={`size-2.5 shrink-0 rounded-shell-sm ${getCalendarColorClass(calendar.color)}`}
            />
            <span className="flex-1 truncate text-body text-ink">
              {calendar.name}
            </span>
            <Checkbox
              checked={checkedCalendarIds.has(calendar.id)}
              onCheckedChange={() => toggleCalendarChecked(calendar.id)}
              aria-label={calendar.name}
            />
          </li>
        ))}
      </ul>
      {isCreateOpen && (
        <CreateCalendarModal onClose={() => setIsCreateOpen(false)} />
      )}
    </div>
  );
}
