import { Fragment, useEffect, useRef, useState } from "react";
import { format, startOfDay } from "date-fns";
import {
  agendaEventCountLabel,
  agendaSearchLimit,
  buildAgenda,
  findAgendaItemForDate,
  formatAgendaEmptyRunLabel,
  formatAgendaEndOfListLabel,
  type AgendaItem,
} from "../lib/agenda";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useShellStore } from "../lib/shellStore";
import { useTimePattern } from "../hooks/useTimePattern";
import { useVisibleOccurrences } from "../hooks/useVisibleOccurrences";
import { NOW_REFRESH_INTERVAL_MS } from "../lib/gridTime";
import type { Occurrence } from "../lib/occurrence";
import { AgendaEventRow } from "./AgendaEventRow";
import { AgendaGapRow } from "./AgendaGapRow";
import { AgendaNowLineRow } from "./AgendaNowLineRow";

interface AgendaViewProps {
  onOccurrenceClick: (occurrence: Occurrence) => void;
}

// How far below the viewport the sentinel still counts as "near the
// bottom" (#336) — the next chunk loads slightly ahead of the literal
// scroll end, not only once it's already visible.
const AGENDA_LOAD_THRESHOLD_PX = 300;

// agendaItemKey is shared by each item's React `key` and its ref-map key
// (#336), so a scroll-to-date request can look an item's DOM node back up
// by the same identity it was rendered with.
function agendaItemKey(item: AgendaItem): string {
  if (item.kind === "day") return item.date.toISOString();
  if (item.kind === "emptyRun") return item.startDate.toISOString();
  return "end-of-list";
}

/**
 * The Agenda view (#330, infinite scroll #336): a scrolling, day-grouped
 * list that always begins at today, however the Selected date is navigated
 * elsewhere (CONTEXT.md's Active view), and grows as its bottom is reached.
 * Entirely a thin renderer of `buildAgenda`'s output — every rule about
 * grouping, ordering, labelling and loading lives there; this component
 * only decides when to ask for more and where to scroll.
 */
export function AgendaView({ onOccurrenceClick }: AgendaViewProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const timePattern = useTimePattern();
  const scrollToTopSignal = useShellStore((state) => state.agendaScrollToTopSignal);
  const windowDays = useShellStore((state) => state.agendaWindowDays);
  const growAgendaWindow = useShellStore((state) => state.growAgendaWindow);
  const scrollTargetDate = useShellStore((state) => state.agendaScrollTargetDate);
  const scrollTargetSignal = useShellStore((state) => state.agendaScrollTargetSignal);

  const containerRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const itemRefs = useRef(new Map<string, HTMLElement>());
  // Live now-line/dimming (#332): refreshed on the same interval as the
  // grid's CurrentTimeLine, so rows cross the line and dim without a reload.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const interval = setInterval(() => setNow(new Date()), NOW_REFRESH_INTERVAL_MS);
    return () => clearInterval(interval);
  }, []);

  const windowStart = startOfDay(now);
  // Occurrences are expanded exactly as far as buildAgenda might search
  // (#336) — agendaSearchLimit is the same rule buildAgenda uses internally
  // to decide when to give up, shared rather than re-derived here, so the
  // two can never drift apart.
  const searchLimit = agendaSearchLimit(now, windowDays);

  const visibleOccurrences = useVisibleOccurrences(windowStart.getTime(), searchLimit.getTime());
  const items = buildAgenda(visibleOccurrences, calendars, now, timePattern, windowDays);
  const ended = items[items.length - 1]?.kind === "endOfList";
  // Read fresh inside the IntersectionObserver callback below, which is set
  // up once and outlives any single render (#336) — a plain state/prop
  // value captured in its closure would go stale the moment `ended` flips.
  const endedRef = useRef(ended);
  useEffect(() => {
    endedRef.current = ended;
  }, [ended]);

  // Reaching the list's bottom grows the window by one chunk (#336). A
  // sentinel + IntersectionObserver, not an onScroll threshold check: the
  // observer only fires on an actual visibility transition, so one scroll
  // gesture reliably requests one chunk rather than however many native
  // scroll events the gesture happens to dispatch before the window grows.
  useEffect(() => {
    const container = containerRef.current;
    const sentinel = sentinelRef.current;
    if (!container || !sentinel) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && !endedRef.current) growAgendaWindow();
      },
      { root: container, rootMargin: `${AGENDA_LOAD_THRESHOLD_PX}px` },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [growAgendaWindow]);

  // Today scrolls the list to the top (#330) — a one-shot signal rather than
  // state, since the same press twice must still re-fire this effect.
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
  }, [scrollToTopSignal]);

  // A mini-calendar click at or after today scrolls the list to that day's
  // group (#336) — only the signal should re-trigger this, so `items` and
  // `scrollTargetDate` are deliberately left out of the dependency array:
  // by the time the signal fires, agendaWindowDays has already grown (if
  // needed) and this render's `items` already reflects it, so the target is
  // guaranteed to exist among the current refs.
  useEffect(() => {
    if (!scrollTargetDate) return;
    const target = findAgendaItemForDate(items, scrollTargetDate);
    if (!target) return;
    itemRefs.current.get(agendaItemKey(target))?.scrollIntoView({ block: "start" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scrollTargetSignal]);

  const setItemRef = (key: string) => (node: HTMLElement | null) => {
    if (node) itemRefs.current.set(key, node);
    else itemRefs.current.delete(key);
  };

  return (
    <div ref={containerRef} className="h-full overflow-auto p-2">
      {items.map((item) => {
        const key = agendaItemKey(item);

        if (item.kind === "endOfList") {
          return (
            <p key={key} ref={setItemRef(key)} className="px-2 py-2 text-label-sm text-ink-muted">
              {formatAgendaEndOfListLabel(item)}
            </p>
          );
        }

        if (item.kind === "emptyRun") {
          return (
            <p key={key} ref={setItemRef(key)} className="px-2 py-2 text-label-sm text-ink-muted">
              {formatAgendaEmptyRunLabel(item)}
            </p>
          );
        }

        return (
          <div key={key} ref={setItemRef(key)} className="mb-2">
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
            {item.rows.length === 0 && (
              <p className="px-2 py-1.5 text-label-sm text-ink-muted">Nothing scheduled</p>
            )}
            {(item.rows.length > 0 || item.nowLine) && (
              <div className="flex flex-col">
                {item.rows.map((row, index) => {
                  const gap = item.gaps.find((candidate) => candidate.beforeIndex === index);
                  return (
                    <Fragment key={row.key}>
                      {item.nowLine?.index === index && (
                        <AgendaNowLineRow label={item.nowLine.label} />
                      )}
                      {gap && <AgendaGapRow label={gap.label} />}
                      <AgendaEventRow row={row} onClick={() => onOccurrenceClick(row.occurrence)} />
                    </Fragment>
                  );
                })}
                {item.nowLine?.index === item.rows.length && (
                  <AgendaNowLineRow label={item.nowLine.label} />
                )}
              </div>
            )}
          </div>
        );
      })}
      {/* Observed by the IntersectionObserver above (#336) — renders at the
          list's bottom whatever it currently is, so it keeps triggering the
          next chunk until `ended` silences it. */}
      <div ref={sentinelRef} aria-hidden className="h-px" />
    </div>
  );
}
