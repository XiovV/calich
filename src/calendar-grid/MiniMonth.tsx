import { format } from "date-fns";
import { DayPicker, type MonthCaptionProps } from "react-day-picker";
import "react-day-picker/style.css";
import { useShellStore } from "../lib/shellStore";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { dayKey } from "../lib/yearGrid";

interface MiniMonthProps {
  month: Date;
  daysWithEvents: Set<string>;
}

/**
 * The clickable Mini-month header. Opens the Month view for its month through
 * the shell store — the Year view's only month-level navigation.
 */
function MiniMonthCaption({ calendarMonth }: MonthCaptionProps) {
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const setActiveView = useShellStore((state) => state.setActiveView);

  return (
    <button
      type="button"
      onClick={() => {
        setSelectedDate(calendarMonth.date);
        setActiveView("month");
      }}
      className="cursor-pointer rounded-shell-sm px-1 py-0.5 text-label-sm font-medium text-ink hover:bg-surface-hover"
    >
      {format(calendarMonth.date, "MMMM")}
    </button>
  );
}

/**
 * A single read-only month in the Year view: day numbers only, an Event-presence
 * dot on days that have a visible Event, and a clickable header. Clicking a day
 * opens the Day view for that date. No create, edit, or drag interactions.
 */
export function MiniMonth({ month, daysWithEvents }: MiniMonthProps) {
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const setActiveView = useShellStore((state) => state.setActiveView);
  const weekStartsOn = useWeekStartsOn();

  return (
    <DayPicker
      month={month}
      hideNavigation
      showOutsideDays={false}
      weekStartsOn={weekStartsOn}
      onDayClick={(date) => {
        setSelectedDate(date);
        setActiveView("day");
      }}
      modifiers={{ hasEvent: (date) => daysWithEvents.has(dayKey(date)) }}
      modifiersClassNames={{ hasEvent: "rdp-day-has-event" }}
      components={{ MonthCaption: MiniMonthCaption }}
    />
  );
}
