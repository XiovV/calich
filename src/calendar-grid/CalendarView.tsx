import { addDays, startOfWeek } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import type { Event } from "../lib/event";
import type { DraftBlock } from "../lib/gridTime";
import { TimeGrid } from "./TimeGrid";
import { MonthGrid } from "./MonthGrid";

interface CalendarViewProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

function ComingSoonPlaceholder({ label }: { label: string }) {
  return (
    <div className="flex h-full items-center justify-center text-body text-ink-muted">
      {label} view is coming soon.
    </div>
  );
}

export function CalendarView({ onDraftCreated, onEventClick }: CalendarViewProps) {
  const activeView = useShellStore((state) => state.activeView);
  const selectedDate = useShellStore((state) => state.selectedDate);

  switch (activeView) {
    case "day":
      return (
        <TimeGrid
          daysToShow={[selectedDate]}
          onDraftCreated={onDraftCreated}
          onEventClick={onEventClick}
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
          onEventClick={onEventClick}
        />
      );
    }
    case "month":
      return (
        <MonthGrid onDraftCreated={onDraftCreated} onEventClick={onEventClick} />
      );
    case "year":
      return <ComingSoonPlaceholder label="Year" />;
  }
}
