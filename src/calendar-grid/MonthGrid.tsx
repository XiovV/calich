import { useRef, useState } from "react";
import { addDays, format, isSameDay, startOfDay } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { getCalendarById } from "../lib/calendar";
import { getCalendarBlockStyle } from "../lib/calendarColors";
import { buildMonthGrid, getOccurrencesForDay } from "../lib/monthGrid";
import { computeMoveToDate } from "../lib/gridTime";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { usePointerDrag, type DragState } from "../hooks/usePointerDrag";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import { MonthDayCell } from "./MonthDayCell";
import { MonthEventDragPreview } from "./MonthEventDragPreview";
import { ScopePicker } from "./ScopePicker";
import { useOccurrenceDragCommit } from "./useOccurrenceDragCommit";
import type { DraftBlock } from "../lib/gridTime";

interface MonthGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

function getCellDateAtPoint(
  cells: { date: Date }[],
  clientX: number,
  clientY: number,
): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const cellElement = target?.closest<HTMLElement>("[data-cell-date]");
  const dateKey = cellElement?.dataset.cellDate;
  if (!dateKey) return null;

  const cell = cells.find((c) => c.date.toDateString() === dateKey);
  return cell?.date ?? null;
}

export function MonthGrid({ onDraftCreated, onOccurrenceClick }: MonthGridProps) {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const weekStartsOn = useWeekStartsOn();
  const calendars = useCalendarsStore((state) => state.calendars);
  const dragCommit = useOccurrenceDragCommit();

  const [hoveredCellKey, setHoveredCellKey] = useState<string | null>(null);

  // A drag that ends back inside its origin cell still fires a native "click"
  // on the cell (mousedown and mouseup share a common ancestor there), which
  // would otherwise reopen the create modal right after a move. Suppress the
  // next cell click when a real drag (not just a click-to-edit) just finished.
  const suppressNextCellClickRef = useRef(false);

  function handleDraftCreated(day: Date, draft: DraftBlock) {
    if (suppressNextCellClickRef.current) return;
    onDraftCreated(day, draft);
  }

  const cells = buildMonthGrid(selectedDate, weekStartsOn);

  // The grid renders Occurrences, not raw Events: each master is expanded over
  // the six-week window the grid displays. A non-recurring Event yields exactly
  // one Occurrence, so nothing changes for it.
  const windowStart = startOfDay(cells[0].date);
  const windowEnd = startOfDay(addDays(cells[cells.length - 1].date, 1));
  const visibleOccurrences = useVisibleOccurrences(
    windowStart.getTime(),
    windowEnd.getTime(),
  );

  const now = new Date();

  const eventDrag = usePointerDrag<Occurrence>({
    onClick: (occurrence) => {
      setHoveredCellKey(null);
      onOccurrenceClick(occurrence);
    },
    onDrag: (occurrence, state: DragState) => {
      setHoveredCellKey(null);
      suppressNextCellClickRef.current = true;
      setTimeout(() => {
        suppressNextCellClickRef.current = false;
      }, 0);

      const targetDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      if (targetDate) {
        const { start, end } = computeMoveToDate(occurrence.start, occurrence.end, targetDate);
        dragCommit.commit(occurrence, start, end);
      }
    },
    onMove: (_occurrence, state: DragState) => {
      const hoveredDate = getCellDateAtPoint(cells, state.position.x, state.position.y);
      setHoveredCellKey(hoveredDate ? hoveredDate.toDateString() : null);
    },
  });

  function handleOccurrenceDragStart(
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) {
    eventDrag.start(occurrence, clientX, clientY);
  }

  const dragCalendar = eventDrag.active
    ? getCalendarById(calendars, eventDrag.active.event.calendarId)
    : undefined;
  const dragBlockStyle = getCalendarBlockStyle(dragCalendar);
  const draggingKey = eventDrag.active ? occurrenceKey(eventDrag.active) : null;

  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-7 border-t border-border">
        {cells.slice(0, 7).map(({ date }) => (
          <div
            key={date.toISOString()}
            className="border-b border-l border-border py-2 text-center text-label-sm text-ink-muted"
          >
            {format(date, "EEE")}
          </div>
        ))}
      </div>
      <div className="grid flex-1 grid-cols-7 grid-rows-6">
        {cells.map(({ date, inCurrentMonth }) => (
          <MonthDayCell
            key={date.toISOString()}
            date={date}
            inCurrentMonth={inCurrentMonth}
            isToday={isSameDay(date, now)}
            isDragHover={hoveredCellKey === date.toDateString()}
            occurrences={getOccurrencesForDay(visibleOccurrences, date).filter(
              (occurrence) => occurrenceKey(occurrence) !== draggingKey,
            )}
            onDraftCreated={handleDraftCreated}
            onOccurrenceClick={onOccurrenceClick}
            onOccurrenceDragStart={handleOccurrenceDragStart}
          />
        ))}
      </div>
      {eventDrag.active && (
        <MonthEventDragPreview
          x={eventDrag.position.x}
          y={eventDrag.position.y}
          title={eventDrag.active.event.title}
          start={eventDrag.active.start}
          blockStyle={dragBlockStyle}
        />
      )}
      {dragCommit.isScopePickerOpen && (
        <ScopePicker
          action="Edit"
          onConfirm={dragCommit.confirmScope}
          onClose={dragCommit.cancel}
        />
      )}
    </div>
  );
}
