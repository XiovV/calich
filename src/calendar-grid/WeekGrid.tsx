import { useEffect, useRef, useState } from "react";
import { addDays, format, startOfWeek } from "date-fns";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import type { Event } from "../lib/mockEvents";
import { PIXELS_PER_HOUR, timeToY, type DraftBlock } from "../lib/gridTime";
import { TimeAxis } from "./TimeAxis";
import { DayColumn } from "./DayColumn";

const NOW_REFRESH_INTERVAL_MS = 60_000;

interface WeekGridProps {
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onEventClick: (event: Event) => void;
}

export function WeekGrid({ onDraftCreated, onEventClick }: WeekGridProps) {
  const selectedDate = useShellStore((state) => state.selectedDate);
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => new Date());

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
        {weekDays.map((day) => (
          <DayColumn
            key={day.toISOString()}
            day={day}
            events={visibleEvents}
            pixelsPerHour={PIXELS_PER_HOUR}
            now={now}
            onDraftCreated={onDraftCreated}
            onEventClick={onEventClick}
          />
        ))}
      </div>
    </div>
  );
}
