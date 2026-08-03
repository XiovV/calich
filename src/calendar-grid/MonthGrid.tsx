import { useEffect, useRef, useState } from "react";
import { isSameDay } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { getCalendarById } from "../lib/calendar";
import { getCalendarColorClass } from "../lib/calendarColors";
import { buildMonthGrid, computeMoveToDate, getEventsForDay } from "../lib/monthGrid";
import { MonthDayCell } from "./MonthDayCell";
import { MonthEventDragPreview } from "./MonthEventDragPreview";
import type { Event } from "../lib/event";
import type { DraftBlock } from "../lib/gridTime";

const WEEKDAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const CLICK_DISTANCE_THRESHOLD_PX = 4;

interface MonthGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

interface EventDragOrigin {
  event: Event;
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
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);
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

  const visibleEvents = events.filter((event) =>
    checkedCalendarIds.has(event.calendarId),
  );
  const cells = buildMonthGrid(selectedDate);
  const now = new Date();

  function handleEventDragStart(event: Event, clientX: number, clientY: number) {
    const origin: EventDragOrigin = { event, startClientX: clientX, startClientY: clientY };
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
        onEventClick(origin.event);
      } else {
        suppressNextCellClickRef.current = true;
        setTimeout(() => {
          suppressNextCellClickRef.current = false;
        }, 0);

        const targetDate = getCellDateAtPoint(cells, domEvent.clientX, domEvent.clientY);
        if (targetDate) {
          const { start, end } = computeMoveToDate(
            origin.event.start,
            origin.event.end,
            targetDate,
          );
          updateEvent(origin.event.id, { start, end });
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
    ? getCalendarById(calendars, activeDrag.event.calendarId)
    : undefined;
  const dragColorClass = dragCalendar
    ? getCalendarColorClass(dragCalendar.color)
    : "bg-calendar-graphite";

  return (
    <div className="flex h-full flex-col">
      <div className="grid grid-cols-7 border-t border-border">
        {WEEKDAY_LABELS.map((label) => (
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
            events={getEventsForDay(visibleEvents, date).filter(
              (event) => event.id !== activeDrag?.event.id,
            )}
            onDraftCreated={handleDraftCreated}
            onEventClick={onEventClick}
            onEventDragStart={handleEventDragStart}
          />
        ))}
      </div>
      {activeDrag && (
        <MonthEventDragPreview
          x={dragPosition.x}
          y={dragPosition.y}
          title={activeDrag.event.title}
          start={activeDrag.event.start}
          colorClass={dragColorClass}
        />
      )}
    </div>
  );
}
