import { addDays, startOfWeek } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import type { Occurrence } from "../lib/occurrence";
import type { DraftBlock } from "../lib/gridTime";
import { TimeGrid } from "./TimeGrid";
import { MonthGrid } from "./MonthGrid";
import { YearGrid } from "./YearGrid";

interface CalendarViewProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

export function CalendarView({ onDraftCreated, onOccurrenceClick }: CalendarViewProps) {
  const activeView = useShellStore((state) => state.activeView);
  const selectedDate = useShellStore((state) => state.selectedDate);

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
      const weekStart = startOfWeek(selectedDate);
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
  }
}
