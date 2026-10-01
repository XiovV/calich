import { addDays, format, isSameDay, startOfDay } from "date-fns";
import { getCalendarById, type Calendar } from "./calendar";
import { resolveOccurrenceColor } from "./calendarColors";
import { formatDragDuration } from "./dragReadout";
import { occurrenceKey, type Occurrence } from "./occurrence";

// A Gap's shown duration must be at least this long (#334) — shorter ones
// are dropped so back-to-back rows with a sliver between them don't fill
// the list with noise.
const AGENDA_GAP_MIN_MINUTES = 30;

// Agenda's tracer-bullet window (#330): the list always begins at today and
// runs for a fixed 15 days (today plus the next 14) — infinite scroll,
// gaps/overlaps, the now-line, multi-day segments and Tasks are later
// tickets that extend buildAgenda (#329's Implementation Decisions).
export const AGENDA_WINDOW_DAYS = 15;

export interface AgendaRow {
  key: string;
  occurrence: Occurrence;
  allDay: boolean;
  timeLabel: string;
  title: string;
  /** The resolved render color: the Event's own override, else its
   * Calendar's color as resolved for the viewer (ADR-0043, ADR-0038). */
  color: string;
  calendarName: string;
  location?: string;
  hasConferenceUrl: boolean;
  // The grid's own dimming rule (#332, EventBlock's `isPast`): the
  // Occurrence's end has passed. An in-progress row (started, not yet
  // ended) is therefore not dimmed — that's what marks it current — and an
  // all-day row is never dimmed, whatever day it's on.
  dimmed: boolean;
  // #333: this row starts before the latest end among the earlier timed
  // rows of its day. States a fact about the list, not a scheduling
  // conflict — Busy plays no part, and an all-day row is never flagged.
  overlaps: boolean;
}

// AgendaNowLine marks where today's amber now-line sits within its group's
// `rows` (#332): immediately before `index`, or at the end of the group
// (`index === rows.length`) once every row has started — including the
// empty-today case, where `index` is always 0.
export interface AgendaNowLine {
  index: number;
  label: string;
}

// AgendaGapRow is a muted "free" row the view inserts immediately before
// `rows[beforeIndex]` (#334) — the same before-this-row convention as
// `AgendaNowLine.index`. A Gap only ever sits between two timed rows, so
// `beforeIndex` is always a valid index into `rows`, never `rows.length`.
export interface AgendaGapRow {
  key: string;
  beforeIndex: number;
  label: string;
}

export interface AgendaDayGroup {
  kind: "day";
  date: Date;
  isToday: boolean;
  eventCount: number;
  rows: AgendaRow[];
  // Only set for today's group (#332) — every other group is entirely in
  // the future, so no now-line ever applies to it.
  nowLine: AgendaNowLine | null;
  // Gaps between this day's timed rows (#334) — see the Gap entry in
  // CONTEXT.md. Computed for every day, not only today.
  gaps: AgendaGapRow[];
}

export interface AgendaEmptyRun {
  kind: "emptyRun";
  startDate: Date;
  endDate: Date;
}

export type AgendaItem = AgendaDayGroup | AgendaEmptyRun;

// resolveRowColor mirrors EventModal's attendeeOnlyCalendarColor fallback
// (ADR-0046): when the viewer has no calendarsStore entry for this Event's
// Calendar (visible only as an Attendee), the wire-provided calendarColor
// stands in so the row still renders its real color instead of the generic
// unresolved gray.
function resolveRowColor(occurrence: Occurrence, calendars: Calendar[]): string {
  const calendar = getCalendarById(calendars, occurrence.event.calendarId);
  const fallback =
    calendar ?? (occurrence.event.calendarColor ? { color: occurrence.event.calendarColor } : undefined);
  return resolveOccurrenceColor(occurrence.event, fallback);
}

function resolveRowCalendarName(occurrence: Occurrence, calendars: Calendar[]): string {
  return (
    getCalendarById(calendars, occurrence.event.calendarId)?.name ??
    occurrence.event.calendarName ??
    "Unknown"
  );
}

function buildRow(
  occurrence: Occurrence,
  calendars: Calendar[],
  timePattern: string,
  now: Date,
  overlaps: boolean,
): AgendaRow {
  const allDay = Boolean(occurrence.event.allDay);
  return {
    key: occurrenceKey(occurrence),
    occurrence,
    allDay,
    timeLabel: allDay
      ? "All day"
      : `${format(occurrence.start, timePattern)} – ${format(occurrence.end, timePattern)}`,
    title: occurrence.event.title,
    color: resolveRowColor(occurrence, calendars),
    calendarName: resolveRowCalendarName(occurrence, calendars),
    location: occurrence.event.location,
    hasConferenceUrl: Boolean(occurrence.event.conferenceUrl),
    dimmed: !allDay && occurrence.end < now,
    overlaps,
  };
}

// computeOverlapFlags flags a timed row when it starts before the latest end
// among the earlier timed rows of its day (#333) — the running latest end,
// not simply the row directly above, so a three-row cluster measures against
// whichever earlier row reaches furthest. All-day rows never count toward an
// overlap and are never themselves flagged. Busy plays no part: this states
// a fact about list order ("this starts before the one above has ended"),
// not a scheduling conflict. `occurrences` must already be in display order
// (compareOccurrences), so index-aligned with the rows built from it.
function computeOverlapFlags(occurrences: Occurrence[]): boolean[] {
  let latestEnd: Date | null = null;
  return occurrences.map((occurrence) => {
    if (occurrence.event.allDay) return false;
    const overlaps = latestEnd !== null && occurrence.start < latestEnd;
    if (latestEnd === null || occurrence.end > latestEnd) latestEnd = occurrence.end;
    return overlaps;
  });
}

// computeNowLine places today's now-line immediately before the first row
// that hasn't started yet (start is still ahead of `now`), or at the end of
// the group once every row has started — `rows` is already in display order
// (all-day first, so an all-day row's midnight start always counts as
// started and never traps the line above it), so a plain `findIndex`
// suffices (#332).
function computeNowLine(rows: AgendaRow[], now: Date, timePattern: string): AgendaNowLine {
  const index = rows.findIndex((row) => row.occurrence.start > now);
  return {
    index: index === -1 ? rows.length : index,
    label: format(now, timePattern),
  };
}

// computeGapRows finds this day's Gaps (#334, CONTEXT.md's Gap entry): a
// stretch covered by no timed row, whatever its Busy value — all-day rows
// never bound one. Covered intervals merge (the running latest end, as in
// computeOverlapFlags), so a Gap starts at the latest end so far rather
// than the row directly above, and none is ever placed before the day's
// first timed row or after its last.
//
// On today, the Gap immediately after the now-line additionally clips its
// start to `now` and reads "… free until HH:MM" instead of "… free" — but
// only when nothing is in progress, i.e. `now` itself falls inside the gap.
// `rows` must already be in display order (compareOccurrences), so
// index-aligned with `beforeIndex`.
function computeGapRows(
  rows: AgendaRow[],
  isToday: boolean,
  now: Date,
  timePattern: string,
): AgendaGapRow[] {
  const gaps: AgendaGapRow[] = [];
  let latestEnd: Date | null = null;

  rows.forEach((row, index) => {
    if (row.allDay) return;

    if (latestEnd !== null && row.occurrence.start > latestEnd) {
      let gapStart = latestEnd;
      let untilLabel: string | null = null;
      if (isToday && gapStart <= now && now < row.occurrence.start) {
        gapStart = now;
        untilLabel = format(row.occurrence.start, timePattern);
      }

      const minutes = Math.round((row.occurrence.start.getTime() - gapStart.getTime()) / 60_000);
      if (minutes >= AGENDA_GAP_MIN_MINUTES) {
        gaps.push({
          key: `gap-${row.key}`,
          beforeIndex: index,
          label: untilLabel
            ? `${formatDragDuration(minutes)} free until ${untilLabel}`
            : `${formatDragDuration(minutes)} free`,
        });
      }
    }

    if (latestEnd === null || row.occurrence.end > latestEnd) latestEnd = row.occurrence.end;
  });

  return gaps;
}

// Order within a day group (#330): all-day rows first, then timed rows by
// start and then end.
function compareOccurrences(a: Occurrence, b: Occurrence): number {
  const allDayA = Boolean(a.event.allDay);
  const allDayB = Boolean(b.event.allDay);
  if (allDayA !== allDayB) return allDayA ? -1 : 1;
  if (a.start.getTime() !== b.start.getTime()) return a.start.getTime() - b.start.getTime();
  return a.end.getTime() - b.end.getTime();
}

// agendaEventCountLabel is a day group header's pluralized count, e.g.
// "1 event" / "3 events" (#330).
export function agendaEventCountLabel(count: number): string {
  return `${count} ${count === 1 ? "event" : "events"}`;
}

// agendaSecondLineText is a row's muted second line, minus the video icon
// (a component concern, driven by AgendaRow.hasConferenceUrl): the Calendar
// name, plus the Location when one is set (#330).
export function agendaSecondLineText(row: Pick<AgendaRow, "calendarName" | "location">): string {
  return row.location ? `${row.calendarName} · ${row.location}` : row.calendarName;
}

// formatAgendaEmptyRunLabel is a collapsed run of empty days' muted row text,
// e.g. "Sat, Oct 3 – Mon, Oct 5 · Nothing scheduled", collapsing to a single
// date when the run is only one day long (#330).
export function formatAgendaEmptyRunLabel(run: AgendaEmptyRun): string {
  const start = format(run.startDate, "EEE, MMM d");
  if (isSameDay(run.startDate, run.endDate)) {
    return `${start} · Nothing scheduled`;
  }
  return `${start} – ${format(run.endDate, "EEE, MMM d")} · Nothing scheduled`;
}

/**
 * The Agenda view's single source of rules (#330, ADR per #329): `occurrences`
 * (already narrowed to what the grid would show — the Active Calendar Set
 * intersected with checked, per `useVisibleOccurrences`) and `now` in, an
 * ordered list of day groups and collapsed empty runs out. The React view is
 * a thin renderer of this list.
 *
 * The window is fixed at `AGENDA_WINDOW_DAYS` starting today — this ticket's
 * tracer-bullet scope has no infinite scroll, so `occurrences` outside that
 * window (if any slip through) are simply never grouped, since no day in
 * `days` can match them.
 */
export function buildAgenda(
  occurrences: Occurrence[],
  calendars: Calendar[],
  now: Date,
  timePattern: string,
): AgendaItem[] {
  const today = startOfDay(now);
  const days = Array.from({ length: AGENDA_WINDOW_DAYS }, (_, index) => addDays(today, index));

  const dayGroups: AgendaDayGroup[] = days.map((date) => {
    const isToday = isSameDay(date, today);
    const dayOccurrences = occurrences
      .filter((occurrence) => isSameDay(occurrence.start, date))
      .sort(compareOccurrences);
    const overlapFlags = computeOverlapFlags(dayOccurrences);
    const rows = dayOccurrences.map((occurrence, index) =>
      buildRow(occurrence, calendars, timePattern, now, overlapFlags[index]),
    );
    return {
      kind: "day",
      date,
      isToday,
      eventCount: dayOccurrences.length,
      rows,
      nowLine: isToday ? computeNowLine(rows, now, timePattern) : null,
      gaps: computeGapRows(rows, isToday, now, timePattern),
    };
  });

  const items: AgendaItem[] = [];
  let runStart: Date | null = null;
  let runEnd: Date | null = null;

  function flushRun() {
    if (runStart && runEnd) {
      items.push({ kind: "emptyRun", startDate: runStart, endDate: runEnd });
    }
    runStart = null;
    runEnd = null;
  }

  for (const group of dayGroups) {
    // Today is always its own group, even when empty (#330) — it never
    // folds into a collapsed run.
    if (!group.isToday && group.rows.length === 0) {
      if (!runStart) runStart = group.date;
      runEnd = group.date;
      continue;
    }
    flushRun();
    items.push(group);
  }
  flushRun();

  return items;
}
