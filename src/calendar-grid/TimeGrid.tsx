import { useEffect, useRef, useState } from "react";
import { addDays, format, isSameDay, startOfDay } from "date-fns";
import { useCalendarsStore } from "../lib/calendarsStore";
import { getCalendarById, type Calendar } from "../lib/calendar";
import { getOccurrenceBlockStyle } from "../lib/calendarColors";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { columnLayoutToBox } from "../lib/eventBlockGeometry";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { usePointerDrag, type DragState } from "../hooks/usePointerDrag";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import {
  PIXELS_PER_HOUR,
  computeMoveToDate,
  computeMovedEventTimes,
  computeResizedEventTimes,
  durationToHeight,
  timeToY,
  type DraftBlock,
} from "../lib/gridTime";
import { TimeAxis } from "./TimeAxis";
import { AllDayLane } from "./AllDayLane";
import { AllDayEventDragPreview } from "./AllDayEventDragPreview";
import { DayColumn, type EventDragPreviewData } from "./DayColumn";
import type { EventDragKind } from "./EventBlock";
import { ScopePicker } from "./ScopePicker";
import { useOccurrenceDragCommit } from "./useOccurrenceDragCommit";

const NOW_REFRESH_INTERVAL_MS = 60_000;

interface TimeGridProps {
  daysToShow: Date[];
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

interface EventDragPayload {
  occurrence: Occurrence;
  kind: EventDragKind;
  columnWidth: number;
}

function getAllDayDateAtPoint(
  days: Date[],
  clientX: number,
  clientY: number,
): Date | null {
  const target = document.elementFromPoint(clientX, clientY);
  const columnElement = target?.closest<HTMLElement>("[data-allday-date]");
  const dateKey = columnElement?.dataset.alldayDate;
  if (!dateKey) return null;

  return days.find((day) => day.toDateString() === dateKey) ?? null;
}

function computeDragTimes(
  payload: EventDragPayload,
  deltaX: number,
  deltaY: number,
) {
  const minuteOffset = (deltaY / PIXELS_PER_HOUR) * 60;

  if (payload.kind === "move") {
    const dayOffset =
      payload.columnWidth > 0 ? Math.round(deltaX / payload.columnWidth) : 0;
    return computeMovedEventTimes(
      payload.occurrence.start,
      payload.occurrence.end,
      dayOffset,
      minuteOffset,
    );
  }

  const edge = payload.kind === "resize-start" ? "start" : "end";
  return computeResizedEventTimes(
    payload.occurrence.start,
    payload.occurrence.end,
    edge,
    minuteOffset,
  );
}

interface EventDragPreview {
  day: Date;
  data: EventDragPreviewData;
}

function computeEventDragPreview(
  activeDrag: EventDragPayload,
  dragDelta: { x: number; y: number },
  visibleOccurrences: Occurrence[],
  calendars: Calendar[],
): EventDragPreview {
  const { start, end } = computeDragTimes(activeDrag, dragDelta.x, dragDelta.y);
  const originDay = startOfDay(activeDrag.occurrence.start);
  const day = activeDrag.kind === "move" ? startOfDay(start) : originDay;

  const originDayOccurrences = visibleOccurrences.filter((occurrence) =>
    isSameDay(occurrence.start, originDay),
  );
  const activeKey = occurrenceKey(activeDrag.occurrence);
  const originLayout = layoutOverlappingEvents(originDayOccurrences).find(
    (layout) => occurrenceKey(layout.occurrence) === activeKey,
  );
  const { left, width } = originLayout
    ? columnLayoutToBox(originLayout.column, originLayout.columnCount)
    : columnLayoutToBox(0, 1);

  const calendar = getCalendarById(
    calendars,
    activeDrag.occurrence.event.calendarId,
  );
  const blockStyle = getOccurrenceBlockStyle(activeDrag.occurrence.event, calendar);

  return {
    day,
    data: {
      top: timeToY(start, PIXELS_PER_HOUR),
      height: durationToHeight(start, end, PIXELS_PER_HOUR),
      left,
      width,
      title: activeDrag.occurrence.event.title,
      start,
      end,
      blockStyle,
    },
  };
}

export function TimeGrid({
  daysToShow,
  onDraftCreated,
  onOccurrenceClick,
}: TimeGridProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const dragCommit = useOccurrenceDragCommit();
  const scrollRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const daysContainerRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => new Date());

  const [allDayHoverDateKey, setAllDayHoverDateKey] = useState<string | null>(null);

  // The grid renders Occurrences, not raw Events: each master is expanded over
  // the visible window (first day 00:00 to the day after the last). A
  // non-recurring Event yields exactly one Occurrence, so nothing changes for it.
  const windowStart = startOfDay(daysToShow[0]);
  const windowEnd = startOfDay(addDays(daysToShow[daysToShow.length - 1], 1));
  const allOccurrences = useVisibleOccurrences(
    windowStart.getTime(),
    windowEnd.getTime(),
  );
  // All-day Occurrences render in the pinned all-day lane, not the hourly
  // grid (ADR-0017).
  const allDayOccurrences = allOccurrences.filter((occurrence) => occurrence.event.allDay);
  const visibleOccurrences = allOccurrences.filter((occurrence) => !occurrence.event.allDay);

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
    const headerHeight = headerRef.current?.offsetHeight ?? 0;
    const nowY = headerHeight + timeToY(new Date(), PIXELS_PER_HOUR);
    container.scrollTop = Math.max(0, nowY - container.clientHeight / 2);
  }, []);

  const eventDrag = usePointerDrag<EventDragPayload>({
    onClick: (payload) => {
      if (payload.kind === "move") onOccurrenceClick(payload.occurrence);
    },
    onDrag: (payload, state: DragState) => {
      const { start, end } = computeDragTimes(payload, state.delta.x, state.delta.y);
      dragCommit.commit(payload.occurrence, start, end);
    },
  });

  function handleOccurrenceDragStart(
    occurrence: Occurrence,
    kind: EventDragKind,
    clientX: number,
    clientY: number,
  ) {
    const columnWidth =
      (daysContainerRef.current?.getBoundingClientRect().width ?? 0) /
      daysToShow.length;
    eventDrag.start({ occurrence, kind, columnWidth }, clientX, clientY);
  }

  const dragPreview = eventDrag.active
    ? computeEventDragPreview(
        eventDrag.active,
        eventDrag.delta,
        visibleOccurrences,
        calendars,
      )
    : null;

  const allDayDrag = usePointerDrag<Occurrence>({
    onClick: (occurrence) => {
      setAllDayHoverDateKey(null);
      onOccurrenceClick(occurrence);
    },
    onDrag: (occurrence, state: DragState) => {
      setAllDayHoverDateKey(null);
      const targetDate = getAllDayDateAtPoint(daysToShow, state.position.x, state.position.y);
      if (targetDate) {
        const { start, end } = computeMoveToDate(occurrence.start, occurrence.end, targetDate);
        dragCommit.commit(occurrence, start, end);
      }
    },
    onMove: (_occurrence, state: DragState) => {
      const hoveredDay = getAllDayDateAtPoint(daysToShow, state.position.x, state.position.y);
      setAllDayHoverDateKey(hoveredDay ? hoveredDay.toDateString() : null);
    },
  });

  function handleAllDayDragStart(
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) {
    allDayDrag.start(occurrence, clientX, clientY);
  }

  const allDayDragCalendar = allDayDrag.active
    ? getCalendarById(calendars, allDayDrag.active.event.calendarId)
    : undefined;
  const allDayDragBlockStyle = getOccurrenceBlockStyle(
    allDayDrag.active?.event,
    allDayDragCalendar,
  );

  return (
    <div className="flex h-full flex-col">
      <div ref={scrollRef} className="flex-1 overflow-y-auto">
        <div
          ref={headerRef}
          className="sticky top-0 z-10 border-b border-border bg-surface"
        >
          <div className="flex">
            <div className="w-14 shrink-0" />
            {daysToShow.map((day) => (
              <div
                key={day.toISOString()}
                className="flex-1 border-l border-border py-2 text-center"
              >
                <p className="text-label-sm text-ink-muted">{format(day, "EEE")}</p>
                <p className="text-body text-ink">{format(day, "d")}</p>
              </div>
            ))}
          </div>
          <AllDayLane
            daysToShow={daysToShow}
            occurrences={allDayOccurrences}
            onOccurrenceClick={onOccurrenceClick}
            onOccurrenceDragStart={handleAllDayDragStart}
            draggingKey={
              allDayDrag.active ? occurrenceKey(allDayDrag.active) : null
            }
            dragHoverDateKey={allDayHoverDateKey}
          />
        </div>
        <div className="flex">
          <TimeAxis pixelsPerHour={PIXELS_PER_HOUR} />
          <div ref={daysContainerRef} className="flex flex-1">
            {daysToShow.map((day) => (
              <DayColumn
                key={day.toISOString()}
                day={day}
                occurrences={visibleOccurrences}
                pixelsPerHour={PIXELS_PER_HOUR}
                now={now}
                onDraftCreated={onDraftCreated}
                onOccurrenceClick={onOccurrenceClick}
                onOccurrenceDragStart={handleOccurrenceDragStart}
                draggingKey={
                  eventDrag.active ? occurrenceKey(eventDrag.active.occurrence) : null
                }
                eventDragPreview={
                  dragPreview && isSameDay(dragPreview.day, day)
                    ? dragPreview.data
                    : null
                }
              />
            ))}
          </div>
        </div>
      </div>
      {allDayDrag.active && (
        <AllDayEventDragPreview
          x={allDayDrag.position.x}
          y={allDayDrag.position.y}
          title={allDayDrag.active.event.title}
          blockStyle={allDayDragBlockStyle}
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
