import { addDays, startOfWeek } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import type { Occurrence } from "../lib/occurrence";
import type { Task } from "../lib/tasksApi";
import type { DraftBlock } from "../lib/gridTime";
import { TimeGrid } from "./TimeGrid";
import { MonthGrid } from "./MonthGrid";
import { YearGrid } from "./YearGrid";
import { AgendaView } from "./AgendaView";

interface CalendarViewProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
  // Agenda-only for now (#337) — clicking a Task row there opens its Task
  // detail; Day/Week/Month don't yet offer that from their own Task chips.
  onTaskClick: (task: Task) => void;
}

export function CalendarView({ onDraftCreated, onOccurrenceClick, onTaskClick }: CalendarViewProps) {
  const activeView = useShellStore((state) => state.activeView);
  const selectedDate = useShellStore((state) => state.selectedDate);
  const weekStartsOn = useWeekStartsOn();

  switch (activeView) {
    case "day":
      return (
        <TimeGrid
          daysToShow={[selectedDate]}
          onDraftCreated={onDraftCreated}
          onOccurrenceClick={onOccurrenceClick}
        />
      );
    case "week": {
      const weekStart = startOfWeek(selectedDate, { weekStartsOn });
      const weekDays = Array.from({ length: 7 }, (_, index) =>
        addDays(weekStart, index),
      );
      return (
        <TimeGrid
          daysToShow={weekDays}
          onDraftCreated={onDraftCreated}
          onOccurrenceClick={onOccurrenceClick}
        />
      );
    }
    case "month":
      return (
        <MonthGrid onDraftCreated={onDraftCreated} onOccurrenceClick={onOccurrenceClick} />
      );
    case "year":
      return <YearGrid />;
    case "agenda":
      return <AgendaView onOccurrenceClick={onOccurrenceClick} onTaskClick={onTaskClick} />;
  }
}
