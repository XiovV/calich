import { useEffect, useRef, useState } from "react";
import { addDays, isSameDay, startOfDay } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import {
  buildMonthGrid,
  computeMoveToDate,
  getOccurrencesForDay,
} from "../lib/monthGrid";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { occurrenceKey, seriesEditChanges, type Occurrence } from "../lib/occurrence";
import { WEEKDAY_SHORT_NAMES } from "../lib/rruleParts";
import { MonthDayCell } from "./MonthDayCell";
import { MonthEventDragPreview } from "./MonthEventDragPreview";
import type { Event } from "../lib/event";
import type { DraftBlock } from "../lib/gridTime";

const CLICK_DISTANCE_THRESHOLD_PX = 4;

interface MonthGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

interface EventDragOrigin {
  occurrence: Occurrence;
  startClientX: number;
  startClientY: number;
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

export function MonthGrid({ onDraftCreated, onEventClick }: MonthGridProps) {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const calendars = useCalendarsStore((state) => state.calendars);

  const dragOriginRef = useRef<EventDragOrigin | null>(null);
  const [activeDrag, setActiveDrag] = useState<EventDragOrigin | null>(null);
  const [dragPosition, setDragPosition] = useState({ x: 0, y: 0 });
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

  const cells = buildMonthGrid(selectedDate);

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

  function handleOccurrenceDragStart(
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) {
    const origin: EventDragOrigin = {
      occurrence,
      startClientX: clientX,
      startClientY: clientY,
    };
    dragOriginRef.current = origin;
    setDragPosition({ x: clientX, y: clientY });
    setActiveDrag(origin);
  }

  useEffect(() => {
    if (!dragOriginRef.current) return;
    const origin = dragOriginRef.current;

    function handleMouseMove(domEvent: MouseEvent) {
      setDragPosition({ x: domEvent.clientX, y: domEvent.clientY });
      const hoveredDate = getCellDateAtPoint(cells, domEvent.clientX, domEvent.clientY);
      setHoveredCellKey(hoveredDate ? hoveredDate.toDateString() : null);
    }

    function handleMouseUp(domEvent: MouseEvent) {
      const deltaX = domEvent.clientX - origin.startClientX;
      const deltaY = domEvent.clientY - origin.startClientY;
      const distance = Math.max(Math.abs(deltaX), Math.abs(deltaY));

      if (distance < CLICK_DISTANCE_THRESHOLD_PX) {
        onEventClick(origin.occurrence.event);
      } else {
        suppressNextCellClickRef.current = true;
        setTimeout(() => {
          suppressNextCellClickRef.current = false;
        }, 0);

        const targetDate = getCellDateAtPoint(cells, domEvent.clientX, domEvent.clientY);
        if (targetDate) {
          const { start, end } = computeMoveToDate(
            origin.occurrence.start,
            origin.occurrence.end,
            targetDate,
          );
          updateEvent(
            origin.occurrence.event.id,
            seriesEditChanges(origin.occurrence.event, start, end),
          );
        }
      }

      dragOriginRef.current = null;
      setActiveDrag(null);
      setHoveredCellKey(null);
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [activeDrag, cells, onEventClick, updateEvent]);

  const dragCalendar = activeDrag
    ? getCalendarById(calendars, activeDrag.occurrence.event.calendarId)
    : undefined;
  const dragColorClass = dragCalendar
    ? getCalendarColorClass(dragCalendar.color)
    : "bg-calendar-graphite";
  const draggingKey = activeDrag ? occurrenceKey(activeDrag.occurrence) : null;

  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-7 border-t border-border">
        {WEEKDAY_SHORT_NAMES.map((label) => (
          <div
            key={label}
            className="border-b border-l border-border py-2 text-center text-label-sm text-ink-muted"
          >
            {label}
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
            onOccurrenceClick={(occurrence) => onEventClick(occurrence.event)}
            onOccurrenceDragStart={handleOccurrenceDragStart}
          />
        ))}
      </div>
      {activeDrag && (
        <MonthEventDragPreview
          x={dragPosition.x}
          y={dragPosition.y}
          title={activeDrag.occurrence.event.title}
          start={activeDrag.occurrence.start}
          colorClass={dragColorClass}
        />
      )}
    </div>
  );
}
