import { useState } from "react";
import { DayPicker } from "react-day-picker";
import "react-day-picker/style.css";
import { useShellStore } from "../../lib/shellStore";
import { useWeekStartsOn } from "../../hooks/useWeekStartsOn";

export function MiniCalendar() {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const weekStartsOn = useWeekStartsOn();
  const [displayMonth, setDisplayMonth] = useState(selectedDate);
  const [syncedSelectedDate, setSyncedSelectedDate] = useState(selectedDate);

  if (selectedDate !== syncedSelectedDate) {
    setSyncedSelectedDate(selectedDate);
    setDisplayMonth(selectedDate);
  }

  return (
    <DayPicker
      mode="single"
      selected={selectedDate}
      onSelect={(date) => date && setSelectedDate(date)}
      month={displayMonth}
      onMonthChange={setDisplayMonth}
      weekStartsOn={weekStartsOn}
      className="ps-4 pe-1"
    />
  );
}
