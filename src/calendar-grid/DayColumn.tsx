import { isSameDay } from "date-fns";
import type { Event } from "../lib/mockEvents";
import { layoutOverlappingEvents } from "../lib/layoutOverlappingEvents";
import { HOURS_IN_DAY } from "../lib/gridTime";
import { EventBlock } from "./EventBlock";
import { CurrentTimeLine } from "./CurrentTimeLine";

interface DayColumnProps {
  day: Date;
  events: Event[];
  pixelsPerHour: number;
  now: Date;
}

export function DayColumn({ day, events, pixelsPerHour, now }: DayColumnProps) {
  const dayEvents = events.filter((event) => isSameDay(event.start, day));
  const layouts = layoutOverlappingEvents(dayEvents);
  const isToday = isSameDay(day, now);

  return (
    <div
      className="relative flex-1 border-l border-border"
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
        />
      ))}
      {isToday && <CurrentTimeLine now={now} pixelsPerHour={pixelsPerHour} />}
    </div>
  );
}
