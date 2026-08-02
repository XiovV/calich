import { useEffect, useRef, useState } from "react";
import { isSameDay } from "date-fns";
import type { Event } from "../lib/mockEvents";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { HOURS_IN_DAY, computeDraftBlock, type DraftBlock } from "../lib/gridTime";
import { EventBlock } from "./EventBlock";
import { CurrentTimeLine } from "./CurrentTimeLine";
import { DraftBlockPreview } from "./DraftBlockPreview";

interface DayColumnProps {
  day: Date;
  events: Event[];
  pixelsPerHour: number;
  now: Date;
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

export function DayColumn({
  day,
  events,
  pixelsPerHour,
  now,
  onDraftCreated,
  onEventClick,
}: DayColumnProps) {
  const dayEvents = events.filter((event) => isSameDay(event.start, day));
  const layouts = layoutOverlappingEvents(dayEvents);
  const isToday = isSameDay(day, now);

  const columnRef = useRef<HTMLDivElement>(null);
  const [dragStartY, setDragStartY] = useState<number | null>(null);
  const [dragCurrentY, setDragCurrentY] = useState<number | null>(null);

  function offsetYFromEvent(clientY: number): number {
    const rect = columnRef.current?.getBoundingClientRect();
    return rect ? clientY - rect.top : 0;
  }

  function handleMouseDown(event: React.MouseEvent) {
    const offsetY = offsetYFromEvent(event.clientY);
    setDragStartY(offsetY);
    setDragCurrentY(offsetY);
  }

  useEffect(() => {
    if (dragStartY === null) return;
    const startY = dragStartY;

    function handleMouseMove(event: MouseEvent) {
      setDragCurrentY(offsetYFromEvent(event.clientY));
    }

    function handleMouseUp(event: MouseEvent) {
      const endY = offsetYFromEvent(event.clientY);
      const draft = computeDraftBlock(day, startY, endY, pixelsPerHour);
      onDraftCreated(day, draft);
      setDragStartY(null);
      setDragCurrentY(null);
    }

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [dragStartY, day, pixelsPerHour, onDraftCreated]);

  return (
    <div
      ref={columnRef}
      onMouseDown={handleMouseDown}
      className="relative flex-1 border-l border-border select-none"
      style={{ height: pixelsPerHour * HOURS_IN_DAY }}
    >
      {Array.from({ length: HOURS_IN_DAY }, (_, hour) => (
        <div
          key={hour}
          className="absolute inset-x-0 border-t border-border"
          style={{ top: hour * pixelsPerHour }}
        />
      ))}
      {layouts.map((layout) => (
        <EventBlock
          key={layout.event.id}
          layout={layout}
          pixelsPerHour={pixelsPerHour}
          onEventClick={onEventClick}
        />
      ))}
      {isToday && <CurrentTimeLine now={now} pixelsPerHour={pixelsPerHour} />}
      {dragStartY !== null && dragCurrentY !== null && (
        <DraftBlockPreview
          top={Math.min(dragStartY, dragCurrentY)}
          height={Math.abs(dragCurrentY - dragStartY)}
        />
      )}
    </div>
  );
}
