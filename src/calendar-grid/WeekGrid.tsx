import { useEffect, useRef, useState } from "react";
import { addDays, format, isSameDay, startOfDay, startOfWeek } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import type { Event } from "../lib/mockEvents";
import {
  PIXELS_PER_HOUR,
  computeMovedEventTimes,
  computeResizedEventTimes,
  durationToHeight,
  timeToY,
  type DraftBlock,
} from "../lib/gridTime";
import { TimeAxis } from "./TimeAxis";
import { DayColumn } from "./DayColumn";
import type { EventDragKind } from "./EventBlock";

const NOW_REFRESH_INTERVAL_MS = 60_000;
const CLICK_DISTANCE_THRESHOLD_PX = 4;

interface WeekGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

interface EventDragOrigin {
  event: Event;
  kind: EventDragKind;
  startClientX: number;
  startClientY: number;
  columnWidth: number;
}

function computeDragTimes(
  origin: EventDragOrigin,
  deltaX: number,
  deltaY: number,
) {
  const minuteOffset = (deltaY / PIXELS_PER_HOUR) * 60;

  if (origin.kind === "move") {
    const dayOffset =
      origin.columnWidth > 0 ? Math.round(deltaX / origin.columnWidth) : 0;
    return computeMovedEventTimes(
      origin.event.start,
      origin.event.end,
      dayOffset,
      minuteOffset,
    );
  }

  const edge = origin.kind === "resize-start" ? "start" : "end";
  return computeResizedEventTimes(
    origin.event.start,
    origin.event.end,
    edge,
    minuteOffset,
  );
}

export function WeekGrid({ onDraftCreated, onEventClick }: WeekGridProps) {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const scrollRef = useRef<HTMLDivElement>(null);
  const daysContainerRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => new Date());

  const dragOriginRef = useRef<EventDragOrigin | null>(null);
  const [activeDrag, setActiveDrag] = useState<EventDragOrigin | null>(null);
  const [dragDelta, setDragDelta] = useState({ x: 0, y: 0 });

  const weekStart = startOfWeek(selectedDate);
  const weekDays = Array.from({ length: 7 }, (_, index) =>
    addDays(weekStart, index),
  );
  const visibleEvents = events.filter((event) =>
    checkedCalendarIds.has(event.calendarId),
  );

  useEffect(() => {
    const interval = setInterval(
      () => setNow(new Date()),
      NOW_REFRESH_INTERVAL_MS,
    );
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    const container = scrollRef.current;
    if (!container) return;
    const nowY = timeToY(new Date(), PIXELS_PER_HOUR);
    container.scrollTop = Math.max(0, nowY - container.clientHeight / 2);
  }, []);

  function handleEventDragStart(
    event: Event,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) {
    const columnWidth =
      (daysContainerRef.current?.getBoundingClientRect().width ?? 0) / 7;
    const origin: EventDragOrigin = {
      event,
      kind,
      startClientX: clientX,
      startClientY: clientY,
      columnWidth,
    };
    dragOriginRef.current = origin;
    setDragDelta({ x: 0, y: 0 });
    setActiveDrag(origin);
  }

  useEffect(() => {
    if (!dragOriginRef.current) return;
    const origin = dragOriginRef.current!;

    function handleMouseMove(domEvent: MouseEvent) {
      setDragDelta({
        x: domEvent.clientX - origin.startClientX,
        y: domEvent.clientY - origin.startClientY,
      });
    }

    function handleMouseUp(domEvent: MouseEvent) {
      const deltaX = domEvent.clientX - origin.startClientX;
      const deltaY = domEvent.clientY - origin.startClientY;
      const distance = Math.max(Math.abs(deltaX), Math.abs(deltaY));

      if (origin.kind === "move" && distance < CLICK_DISTANCE_THRESHOLD_PX) {
        onEventClick(origin.event);
      } else if (distance >= CLICK_DISTANCE_THRESHOLD_PX) {
        const { start, end } = computeDragTimes(origin, deltaX, deltaY);
        updateEvent(origin.event.id, { start, end });
      }

      dragOriginRef.current = null;
      setActiveDrag(null);
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [activeDrag, onEventClick, updateEvent]);

  let dragPreviewDay: Date | null = null;
  let dragPreviewTop = 0;
  let dragPreviewHeight = 0;

  if (activeDrag) {
    const { start, end } = computeDragTimes(
      activeDrag,
      dragDelta.x,
      dragDelta.y,
    );
    dragPreviewDay =
      activeDrag.kind === "move" ? startOfDay(start) : startOfDay(activeDrag.event.start);
    dragPreviewTop = timeToY(start, PIXELS_PER_HOUR);
    dragPreviewHeight = durationToHeight(start, end, PIXELS_PER_HOUR);
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex border-b border-border">
        <div className="w-14 shrink-0" />
        {weekDays.map((day) => (
          <div
            key={day.toISOString()}
            className="flex-1 border-l border-border py-2 text-center"
          >
            <p className="text-label-sm text-ink-muted">{format(day, "EEE")}</p>
            <p className="text-body text-ink">{format(day, "d")}</p>
          </div>
        ))}
      </div>
      <div ref={scrollRef} className="flex flex-1 overflow-y-auto">
        <TimeAxis pixelsPerHour={PIXELS_PER_HOUR} />
        <div ref={daysContainerRef} className="flex flex-1">
          {weekDays.map((day) => (
            <DayColumn
              key={day.toISOString()}
              day={day}
              events={visibleEvents}
              pixelsPerHour={PIXELS_PER_HOUR}
              now={now}
              onDraftCreated={onDraftCreated}
              onEventClick={onEventClick}
              onEventDragStart={handleEventDragStart}
              eventDragPreview={
                dragPreviewDay && isSameDay(dragPreviewDay, day)
                  ? { top: dragPreviewTop, height: dragPreviewHeight }
                  : null
              }
            />
          ))}
        </div>
      </div>
    </div>
  );
}
