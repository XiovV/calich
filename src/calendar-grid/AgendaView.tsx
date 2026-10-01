import { useEffect, useRef } from "react";
import { addDays, format, startOfDay } from "date-fns";
import {
  AGENDA_WINDOW_DAYS,
  agendaEventCountLabel,
  buildAgenda,
  formatAgendaEmptyRunLabel,
} from "../lib/agenda";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useShellStore } from "../lib/shellStore";
import { useTimePattern } from "../hooks/useTimePattern";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import type { Occurrence } from "../lib/occurrence";
import { AgendaEventRow } from "./AgendaEventRow";

interface AgendaViewProps {
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

/**
 * The tracer-bullet Agenda view (#330): a scrolling, day-grouped list that
 * always begins at today, however the Selected date is navigated elsewhere
 * (CONTEXT.md's Active view). Entirely a thin renderer of `buildAgenda`'s
 * output — every rule about grouping, ordering and labelling lives there.
 */
export function AgendaView({ onOccurrenceClick }: AgendaViewProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const timePattern = useTimePattern();
  const scrollToTopSignal = useShellStore((state) => state.agendaScrollToTopSignal);

  const containerRef = useRef<HTMLDivElement>(null);
  const now = new Date();
  const windowStart = startOfDay(now);
  const windowEnd = addDays(windowStart, AGENDA_WINDOW_DAYS);

  const visibleOccurrences = useVisibleOccurrences(windowStart.getTime(), windowEnd.getTime());
  const items = buildAgenda(visibleOccurrences, calendars, now, timePattern);

  // Today scrolls the list to the top (#330) — a one-shot signal rather than
  // state, since the same press twice must still re-fire this effect.
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
  }, [scrollToTopSignal]);

  return (
    <div ref={containerRef} className="h-full overflow-auto p-2">
      {items.map((item) => {
        if (item.kind === "emptyRun") {
          return (
            <p
              key={item.startDate.toISOString()}
              className="px-2 py-2 text-label-sm text-ink-muted"
            >
              {formatAgendaEmptyRunLabel(item)}
            </p>
          );
        }

        return (
          <div key={item.date.toISOString()} className="mb-2">
            <div className="flex items-baseline gap-1.5 px-2 py-1.5">
              {item.isToday ? (
                <span className="font-medium text-accent-ink">Today</span>
              ) : (
                <span className="font-medium text-ink">{format(item.date, "EEE, MMM d")}</span>
              )}
              <span className="text-label-sm text-ink-muted">
                · {agendaEventCountLabel(item.eventCount)}
              </span>
            </div>
            {item.rows.length === 0 ? (
              <p className="px-2 py-1.5 text-label-sm text-ink-muted">Nothing scheduled</p>
            ) : (
              <div className="flex flex-col">
                {item.rows.map((row) => (
                  <AgendaEventRow
                    key={row.key}
                    row={row}
                    onClick={() => onOccurrenceClick(row.occurrence)}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
