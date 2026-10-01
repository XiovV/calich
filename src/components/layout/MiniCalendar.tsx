import { useState } from "react";
import { startOfDay } from "date-fns";
import { DayPicker } from "react-day-picker";
import "react-day-picker/style.css";
import { useShellStore } from "../../lib/shellStore";
import { useWeekStartsOn } from "../../hooks/useWeekStartsOn";

export function MiniCalendar() {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const activeView = useShellStore((state) => state.activeView);
  const requestAgendaScrollToDate = useShellStore((state) => state.requestAgendaScrollToDate);
  const weekStartsOn = useWeekStartsOn();
  const [displayMonth, setDisplayMonth] = useState(selectedDate);
  const [syncedSelectedDate, setSyncedSelectedDate] = useState(selectedDate);

  if (selectedDate !== syncedSelectedDate) {
    setSyncedSelectedDate(selectedDate);
    setDisplayMonth(selectedDate);
  }

  // Selected date is always set, same as every other view. In Agenda, a
  // click at or after today additionally scrolls the list to that day,
  // loading up to it if needed (#336) — a past date leaves the list where
  // it is, since Agenda has no past to scroll into (CONTEXT.md's Active
  // view: Agenda ignores Selected date as its own anchor either way).
  const handleSelect = (date: Date) => {
    setSelectedDate(date);
    if (activeView === "agenda" && date >= startOfDay(new Date())) {
      requestAgendaScrollToDate(date);
    }
  };

  return (
    <DayPicker
      mode="single"
      selected={selectedDate}
      onSelect={(date) => date && handleSelect(date)}
      month={displayMonth}
      onMonthChange={setDisplayMonth}
      weekStartsOn={weekStartsOn}
      className="ps-4 pe-1"
    />
  );
}
