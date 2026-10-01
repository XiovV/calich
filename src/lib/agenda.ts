import { addDays, addYears, differenceInCalendarDays, format, isSameDay, startOfDay } from "date-fns";
import { getCalendarById, type Calendar } from "./calendar";
import { resolveOccurrenceColor } from "./calendarColors";
import { formatDragDuration } from "./dragReadout";
import { occurrenceKey, type Occurrence } from "./occurrence";
import { getDaySegments, type OccurrenceDaySegment } from "./occurrenceSegments";

// A Gap's shown duration must be at least this long (#334) — shorter ones
// are dropped so back-to-back rows with a sliver between them don't fill
// the list with noise.
const AGENDA_GAP_MIN_MINUTES = 30;

// AGENDA_WINDOW_DAYS is Agenda's initial window (#330): the list always
// begins at today and starts out covering today plus the next 14 days.
// Reaching the list's bottom grows the window by AGENDA_LOAD_CHUNK_DAYS at a
// time (#336) — both are day-counts from today, kept as session state in
// shellStore, rather than fixed dates, so the window keeps the same shape
// as "today" rolls forward across a session left open overnight.
export const AGENDA_WINDOW_DAYS = 15;

// AGENDA_LOAD_CHUNK_DAYS is how far each further load extends the window
// once the list's bottom is reached (#336).
export const AGENDA_LOAD_CHUNK_DAYS = 14;

// agendaSearchLimit is how far past the requested window buildAgenda will
// ever look for the next row (#336) — exported so the caller can size its
// occurrence query to match exactly what buildAgenda might search, rather
// than each independently re-deriving the one-year rule.
export function agendaSearchLimit(now: Date, windowDays: number): Date {
  return addYears(addDays(startOfDay(now), windowDays), 1);
}

export interface AgendaRow {
  key: string;
  occurrence: Occurrence;
  allDay: boolean;
  timeLabel: string;
  // "Day N of M" when this row is one day of a multi-day Occurrence
  // (midnight-crossing timed, or a multi-day All-day Event), else null
  // (#335). Rendered ahead of the second line's video icon/Calendar name.
  dayLabel: string | null;
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
  // all-day row is never dimmed, whatever day it's on. Always the
  // Occurrence's *true* end, even on a midnight-crossing row's first day
  // (#335) — never the day's clipped segment end.
  dimmed: boolean;
  // #333: this row starts before the latest end among the earlier timed
  // rows of its day. States a fact about the list, not a scheduling
  // conflict — Busy plays no part, and an all-day row is never flagged.
  overlaps: boolean;
  // This day's clipped portion of the Occurrence's span (#335) — what
  // Gaps/Overlaps are computed from, so a multi-day Occurrence's later days
  // (clipped to start at that day's midnight) don't drag their true start
  // into a day they don't belong to. Equal to `occurrence.start`/`.end` for
  // a same-day Occurrence.
  segmentStart: Date;
  segmentEnd: Date;
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

// AgendaEndOfList is the list's terminal item (#336): the one-year lookahead
// past the requested window found nothing, so there is nothing left to load.
// `afterDate` is the last day actually searched, read by
// formatAgendaEndOfListLabel as "Nothing scheduled after <date>".
export interface AgendaEndOfList {
  kind: "endOfList";
  afterDate: Date;
}

export type AgendaItem = AgendaDayGroup | AgendaEmptyRun | AgendaEndOfList;

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

// occurrenceDaySpan is the number of calendar days an Occurrence touches
// (#335) — M in "Day N of M". A same-day Occurrence (including one ending
// exactly at midnight, which `occurrenceIntersectsDay` already excludes from
// the next day) is 1. `end` is exclusive, so the last touched moment is 1ms
// before it.
function occurrenceDaySpan(occurrence: Occurrence): number {
  const lastTouchedMoment = new Date(occurrence.end.getTime() - 1);
  return differenceInCalendarDays(lastTouchedMoment, occurrence.start) + 1;
}

// occurrenceDayIndex is N in "Day N of M" — `day`'s 1-based position among
// the calendar days the Occurrence touches (#335).
function occurrenceDayIndex(occurrence: Occurrence, day: Date): number {
  return differenceInCalendarDays(day, occurrence.start) + 1;
}

// buildRow's time label for a single day of a (possibly multi-day)
// Occurrence (#335): an all-day row always reads "All day"; a same-day timed
// row keeps its full range; a multi-day timed row's first day shows its full
// true range (not clipped to midnight), its last day "Until HH:MM" (its true
// end), and every day between "All day" — sorted among the timed rows
// regardless (see `compareSegments`), since it's an Occurrence segment, not
// an All-day Event.
function buildTimeLabel(
  occurrence: Occurrence,
  allDay: boolean,
  dayIndex: number,
  totalDays: number,
  timePattern: string,
): string {
  if (allDay) return "All day";
  if (totalDays === 1 || dayIndex === 1) {
    return `${format(occurrence.start, timePattern)} – ${format(occurrence.end, timePattern)}`;
  }
  if (dayIndex === totalDays) return `Until ${format(occurrence.end, timePattern)}`;
  return "All day";
}

function buildRow(
  segment: OccurrenceDaySegment,
  day: Date,
  calendars: Calendar[],
  timePattern: string,
  now: Date,
  overlaps: boolean,
): AgendaRow {
  const occurrence = segment.occurrence;
  const allDay = Boolean(occurrence.event.allDay);
  const totalDays = occurrenceDaySpan(occurrence);
  const dayIndex = occurrenceDayIndex(occurrence, day);
  return {
    key: occurrenceKey(occurrence),
    occurrence,
    allDay,
    timeLabel: buildTimeLabel(occurrence, allDay, dayIndex, totalDays, timePattern),
    dayLabel: totalDays > 1 ? `Day ${dayIndex} of ${totalDays}` : null,
    title: occurrence.event.title,
    color: resolveRowColor(occurrence, calendars),
    calendarName: resolveRowCalendarName(occurrence, calendars),
    location: occurrence.event.location,
    hasConferenceUrl: Boolean(occurrence.event.conferenceUrl),
    dimmed: !allDay && occurrence.end < now,
    overlaps,
    segmentStart: segment.start,
    segmentEnd: segment.end,
  };
}

// computeOverlapFlags flags a timed row when it starts before the latest end
// among the earlier timed rows of its day (#333) — the running latest end,
// not simply the row directly above, so a three-row cluster measures against
// whichever earlier row reaches furthest. All-day rows never count toward an
// overlap and are never themselves flagged. Busy plays no part: this states
// a fact about list order ("this starts before the one above has ended"),
// not a scheduling conflict. Measured on each segment's day-clipped bounds
// (#335), so a multi-day Occurrence's portion on this day is what counts,
// not its true (possibly other-day) start. `segments` must already be in
// display order (compareSegments), so index-aligned with the rows built
// from it.
function computeOverlapFlags(segments: OccurrenceDaySegment[]): boolean[] {
  let latestEnd: Date | null = null;
  return segments.map((segment) => {
    if (segment.occurrence.event.allDay) return false;
    const overlaps = latestEnd !== null && segment.start < latestEnd;
    if (latestEnd === null || segment.end > latestEnd) latestEnd = segment.end;
    return overlaps;
  });
}

// computeNowLine places today's now-line immediately before the first row
// that hasn't started yet (its start is still ahead of `now`), or at the end
// of the group once every row has started — `rows` is already in display
// order (all-day first, so an all-day row's midnight start always counts as
// started and never traps the line above it), so a plain `findIndex`
// suffices (#332). Measured against each row's day segment (#335): a later
// day of a multi-day Occurrence is clipped to start at that day's midnight,
// which has necessarily already passed once that day is reached.
function computeNowLine(rows: AgendaRow[], now: Date, timePattern: string): AgendaNowLine {
  const index = rows.findIndex((row) => row.segmentStart > now);
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
// first timed row or after its last. Measured on each row's day segment
// (#335), so a multi-day Occurrence's portion on this day is what a Gap
// runs against.
//
// On today, the Gap immediately after the now-line additionally clips its
// start to `now` and reads "… free until HH:MM" instead of "… free" — but
// only when nothing is in progress, i.e. `now` itself falls inside the gap.
// `rows` must already be in display order (compareSegments), so
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

    if (latestEnd !== null && row.segmentStart > latestEnd) {
      let gapStart = latestEnd;
      let untilLabel: string | null = null;
      if (isToday && gapStart <= now && now < row.segmentStart) {
        gapStart = now;
        untilLabel = format(row.segmentStart, timePattern);
      }

      const minutes = Math.round((row.segmentStart.getTime() - gapStart.getTime()) / 60_000);
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

    if (latestEnd === null || row.segmentEnd > latestEnd) latestEnd = row.segmentEnd;
  });

  return gaps;
}

// Order within a day group (#330): all-day rows first, then timed rows by
// start and then end — each segment's day-clipped bounds (#335), so a later
// day of a multi-day Occurrence (clipped to that day's midnight) sorts as if
// it started then, not at its true, other-day start.
function compareSegments(a: OccurrenceDaySegment, b: OccurrenceDaySegment): number {
  const allDayA = Boolean(a.occurrence.event.allDay);
  const allDayB = Boolean(b.occurrence.event.allDay);
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

// agendaDayLabelPrefix is a multi-day row's "Day N of M ·" lead-in, ahead of
// the second line's video icon/Calendar name (#335) — "" when the row isn't
// part of a multi-day Occurrence, matching agendaSecondLineText's convention
// of owning this text so the component only handles conditional rendering.
export function agendaDayLabelPrefix(row: Pick<AgendaRow, "dayLabel">): string {
  return row.dayLabel ? `${row.dayLabel} ·` : "";
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

// formatAgendaEndOfListLabel is the terminal row's text (#336) — the
// one-year lookahead past the requested window found nothing, and the list
// stops extending.
export function formatAgendaEndOfListLabel(item: AgendaEndOfList): string {
  return `Nothing scheduled after ${format(item.afterDate, "MMM d, yyyy")}`;
}

// findAgendaItemForDate locates whichever rendered item a given date falls
// under — its own day group, the collapsed run it was folded into, or the
// terminal end-of-list item when the run that would have covered it was
// discarded in favor of that single row (#336) — so a mini-calendar click
// can scroll the list to it. A date genuinely outside every rendered item
// (e.g. past the one-year cap) returns undefined.
export function findAgendaItemForDate(
  items: AgendaItem[],
  date: Date,
): AgendaDayGroup | AgendaEmptyRun | AgendaEndOfList | undefined {
  const day = startOfDay(date);
  for (const item of items) {
    if (item.kind === "day" && isSameDay(item.date, day)) return item;
    if (item.kind === "emptyRun" && item.startDate <= day && day <= item.endDate) return item;
  }
  const last = items[items.length - 1];
  return last?.kind === "endOfList" ? last : undefined;
}

/**
 * The Agenda view's single source of rules (#330, ADR per #329): `occurrences`
 * (already narrowed to what the grid would show — the Active Calendar Set
 * intersected with checked, per `useVisibleOccurrences`) and `now` in, an
 * ordered list of day groups, collapsed empty runs and (possibly) a
 * terminal end-of-list item out. The React view is a thin renderer of this
 * list.
 *
 * `windowDays` is the requested window's size in days from today —
 * `AGENDA_WINDOW_DAYS` by default, grown by the caller as the list's bottom
 * is reached (#336). When the days right before that boundary are empty,
 * this keeps walking past it in search of the next row to load straight
 * through to — at most a year past the boundary — rather than stopping on
 * an empty stretch; `occurrences` must therefore cover whatever of that
 * year's worth of days might actually be searched, not just the nominal
 * window. If nothing is found within that year, the list ends with a single
 * terminal item instead of an open-ended empty run.
 */
export function buildAgenda(
  occurrences: Occurrence[],
  calendars: Calendar[],
  now: Date,
  timePattern: string,
  windowDays: number = AGENDA_WINDOW_DAYS,
): AgendaItem[] {
  const today = startOfDay(now);
  const windowEnd = addDays(today, windowDays);
  // The one-year lookahead is anchored to this call's windowEnd — the
  // boundary actually requested — so a later call with a grown windowDays
  // gets its own fresh year to search (#336).
  const searchLimit = agendaSearchLimit(now, windowDays);

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

  for (let date = today; date < searchLimit; date = addDays(date, 1)) {
    // Past what was actually requested, with no open run chasing the next
    // row to load through to: the window ends cleanly here (#336).
    if (date >= windowEnd && runStart === null) return items;

    const isToday = isSameDay(date, today);
    // A multi-day Occurrence yields a segment on every day it touches
    // (#335), so it appears — and counts toward `eventCount` — in each of
    // those days' groups, not only the one its start falls on.
    const daySegments = getDaySegments(occurrences, date).sort(compareSegments);

    if (daySegments.length === 0 && !isToday) {
      // Today is always its own group, even when empty (#330) — it never
      // folds into a run. Every other empty day joins or opens one, whether
      // that run sits inside the requested window or is being extended past
      // it in search of the next row (#336) — recomputing fresh from today
      // each time means two runs can never end up adjacent in the output.
      if (!runStart) runStart = date;
      runEnd = date;
      continue;
    }

    flushRun();
    const overlapFlags = computeOverlapFlags(daySegments);
    const rows = daySegments.map((segment, index) =>
      buildRow(segment, date, calendars, timePattern, now, overlapFlags[index]),
    );
    items.push({
      kind: "day",
      date,
      isToday,
      eventCount: daySegments.length,
      rows,
      nowLine: isToday ? computeNowLine(rows, now, timePattern) : null,
      gaps: computeGapRows(rows, isToday, now, timePattern),
    });

    // Found the next row while loading through an empty stretch past the
    // window (#336): stop right here rather than continuing to walk all the
    // way to searchLimit.
    if (date >= windowEnd) return items;
  }

  // Walked all the way to the one-year cap while still searching for the
  // next row past the window (#336). The open run is discarded in favor of
  // this single terminal item, so a calendar with nothing ahead ends
  // cleanly rather than trailing off as an endless empty stretch.
  items.push({ kind: "endOfList", afterDate: addDays(searchLimit, -1) });
  return items;
}
