import { Collapsible } from "../ui/Collapsible";
import { Checkbox } from "../ui/Checkbox";
import { getCalendarColorClass } from "../../lib/calendarColors";
import { mockCalendarSections } from "../../lib/mockCalendars";
import { useShellStore } from "../../lib/shellStore";

export function CalendarList() {
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const toggleCalendarChecked = useShellStore(
    (state) => state.toggleCalendarChecked,
  );

  return (
    <div>
      {mockCalendarSections.map((section) => (
        <Collapsible key={section.label} label={section.label}>
          <ul>
            {section.calendars.map((calendar) => (
              <li
                key={calendar.id}
                className="flex items-center gap-2 px-4 py-1.5"
              >
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
        </Collapsible>
      ))}
    </div>
  );
}
