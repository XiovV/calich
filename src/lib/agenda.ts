import { addDays, addYears, differenceInCalendarDays, format, isSameDay, startOfDay } from "date-fns";
import { getCalendarById, type Calendar } from "./calendar";
import { resolveCalendarFill, resolveOccurrenceColor } from "./calendarColors";
import { formatDragDuration } from "./dragReadout";
import { occurrenceKey, type Occurrence } from "./occurrence";
import { getDaySegments, type OccurrenceDaySegment } from "./occurrenceSegments";
import { taskDeadlineFallsOnDay } from "./taskScheduling";
import { getTaskListById, type TaskList } from "./taskListsApi";
import type { Task } from "./tasksApi";
import { getTaskTimeBlockDaySegments, taskTimeBlockEnd, type TaskTimeBlockSegment } from "./taskTimeBlockSegments";

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
  // Discriminates a calendar Occurrence row from a Task row (#337) — the
  // unused one of `occurrence`/`task` is always null, rather than a
  // discriminated union, so existing Occurrence-only call sites don't have
  // to narrow before reading `.occurrence`.
  kind: "occurrence" | "task";
  occurrence: Occurrence | null;
  task: Task | null;
  // True for a Deadline-only Task row and an all-day Occurrence row alike
  // (#330, #337) — both sort ahead of every timed row, a Deadline-only Task
  // after every all-day Occurrence (see `rowSortGroup`).
  allDay: boolean;
  // "Due" for a Deadline-only Task row (#337), regardless of what time of
  // day its Deadline actually falls at — Agenda places it by day only, the
  // same as the all-day lane does.
  timeLabel: string;
  // "Day N of M" when this row is one day of a multi-day Occurrence or
  // Time-blocked Task (midnight-crossing timed, or a multi-day All-day
  // Event; #335, #337), else null. Rendered ahead of the second line's
  // video icon/Calendar name.
  dayLabel: string | null;
  title: string;
  /** The resolved render color: the Event's own override, else its
   * Calendar's color as resolved for the viewer (ADR-0043, ADR-0038) — or,
   * for a Task row, its Task List's color (#337). */
  color: string;
  /** The Calendar name for an Occurrence row, the Task List name for a Task
   * row (#337) — `agendaSecondLineText`'s shared label, whichever kind of
   * row it's reading off. */
  calendarName: string;
  location?: string;
  hasConferenceUrl: boolean;
  // The grid's own dimming rule (#332, EventBlock's `isPast`): the
  // Occurrence's end has passed. An in-progress row (started, not yet
  // ended) is therefore not dimmed — that's what marks it current — and an
  // all-day row (including a Deadline-only Task's, #337) is never dimmed,
  // whatever day it's on. Always the Occurrence's/Task's *true* end, even on
  // a midnight-crossing row's first day (#335) — never the day's clipped
  // segment end.
  dimmed: boolean;
  // #333: this row starts before the latest end among the earlier timed
  // rows of its day. States a fact about the list, not a scheduling
  // conflict — Busy plays no part, and neither an all-day row nor a
  // completed Time-blocked Task's row (#337, `rowBoundsGapsAndOverlaps`) is
  // ever flagged.
  overlaps: boolean;
  // This day's clipped portion of the Occurrence's/Task's span (#335, #337)
  // — what Gaps/Overlaps are computed from, so a multi-day one's later days
  // (clipped to start at that day's midnight) don't drag their true start
  // into a day they don't belong to. Equal to `occurrence.start`/`.end` (or
  // the Task's Time block bounds) for a same-day one; pinned to that day's
  // midnight for an all-day Occurrence or a Deadline-only Task, so the
  // now-line (which only ever reads `segmentStart`) always treats it as
  // already under way.
  segmentStart: Date;
  segmentEnd: Date;
  // #337: true for a completed Task row — struck through, but (per
  // `rowBoundsGapsAndOverlaps`) no longer bounding a Gap or counting toward
  // an Overlap, since that time is no longer committed. Always false for an
  // Occurrence row.
  completed: boolean;
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
  // #337: Tasks rendered in this day's group — Time-blocked and
  // Deadline-only alike — kept apart from `eventCount` so the header can
  // read "2 events · 1 task" rather than folding Tasks into the Occurrence
  // count.
  taskCount: number;
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

// daySpan is the number of calendar days a `[start, end)` span touches
// (#335, generalized for #337 to any such span, Occurrence or Task Time
// block alike) — M in "Day N of M". A same-day span (including one ending
// exactly at midnight) is 1. `end` is exclusive, so the last touched moment
// is 1ms before it.
function daySpan(start: Date, end: Date): number {
  const lastTouchedMoment = new Date(end.getTime() - 1);
  return differenceInCalendarDays(lastTouchedMoment, start) + 1;
}

// dayIndex is N in "Day N of M" — `day`'s 1-based position among the
// calendar days a span starting at `start` touches (#335, #337).
function dayIndex(start: Date, day: Date): number {
  return differenceInCalendarDays(day, start) + 1;
}

function occurrenceDaySpan(occurrence: Occurrence): number {
  return daySpan(occurrence.start, occurrence.end);
}

function occurrenceDayIndex(occurrence: Occurrence, day: Date): number {
  return dayIndex(occurrence.start, day);
}

// A Time-blocked Task's counterpart to occurrenceDaySpan/occurrenceDayIndex
// (#337): the same "Day N of M" math, read off the Task's own Time block
// bounds (`task.start`/`taskTimeBlockEnd`) instead of an Occurrence's.
function taskTimeBlockDaySpan(task: Task): number {
  return daySpan(task.start as Date, taskTimeBlockEnd(task));
}

function taskTimeBlockDayIndex(task: Task, day: Date): number {
  return dayIndex(task.start as Date, day);
}

// buildTimeLabel is a row's time label for a single day of a (possibly
// multi-day) `[start, end)` span (#335, generalized for #337 to a Task's
// Time block as well as an Occurrence's): an all-day row always reads "All
// day"; a same-day timed row keeps its full range; a multi-day timed row's
// first day shows its full true range (not clipped to midnight), its last
// day "Until HH:MM" (its true end), and every day between "All day" —
// sorted among the timed rows regardless (see `rowSortGroup`), since it's a
// segment of a timed thing, not an all-day one.
function buildTimeLabel(
  start: Date,
  end: Date,
  allDay: boolean,
  dayIdx: number,
  totalDays: number,
  timePattern: string,
): string {
  if (allDay) return "All day";
  if (totalDays === 1 || dayIdx === 1) {
    return `${format(start, timePattern)} – ${format(end, timePattern)}`;
  }
  if (dayIdx === totalDays) return `Until ${format(end, timePattern)}`;
  return "All day";
}

function buildOccurrenceRow(
  segment: OccurrenceDaySegment,
  day: Date,
  calendars: Calendar[],
  timePattern: string,
  now: Date,
): AgendaRow {
  const occurrence = segment.occurrence;
  const allDay = Boolean(occurrence.event.allDay);
  const totalDays = occurrenceDaySpan(occurrence);
  const idx = occurrenceDayIndex(occurrence, day);
  return {
    key: occurrenceKey(occurrence),
    kind: "occurrence",
    occurrence,
    task: null,
    allDay,
    timeLabel: buildTimeLabel(occurrence.start, occurrence.end, allDay, idx, totalDays, timePattern),
    dayLabel: totalDays > 1 ? `Day ${idx} of ${totalDays}` : null,
    title: occurrence.event.title,
    color: resolveRowColor(occurrence, calendars),
    calendarName: resolveRowCalendarName(occurrence, calendars),
    location: occurrence.event.location,
    hasConferenceUrl: Boolean(occurrence.event.conferenceUrl),
    dimmed: !allDay && occurrence.end < now,
    overlaps: false,
    segmentStart: segment.start,
    segmentEnd: segment.end,
    completed: false,
  };
}

// buildTaskGridRow is buildOccurrenceRow's counterpart for a Time-blocked
// Task (#337, placement precedence's `grid` surface, ADR-0083): a timed row
// in its Task List's color, following the same midnight-crossing
// "Day N of M" splitting as an Occurrence (via `getTaskTimeBlockDaySegments`
// — the existing Time-block segment helpers the issue calls for). `segment`
// is already day-clipped, exactly as an Occurrence's is.
function buildTaskGridRow(
  segment: TaskTimeBlockSegment,
  day: Date,
  taskLists: TaskList[],
  timePattern: string,
  now: Date,
): AgendaRow {
  const task = segment.task;
  const taskEnd = taskTimeBlockEnd(task);
  const totalDays = taskTimeBlockDaySpan(task);
  const idx = taskTimeBlockDayIndex(task, day);
  const taskList = getTaskListById(taskLists, task.taskListId);
  return {
    key: `task-${task.id}`,
    kind: "task",
    occurrence: null,
    task,
    allDay: false,
    timeLabel: buildTimeLabel(task.start as Date, taskEnd, false, idx, totalDays, timePattern),
    dayLabel: totalDays > 1 ? `Day ${idx} of ${totalDays}` : null,
    title: task.title,
    color: resolveCalendarFill(taskList),
    calendarName: taskList?.name ?? "Unknown",
    location: undefined,
    hasConferenceUrl: false,
    dimmed: taskEnd < now,
    overlaps: false,
    segmentStart: segment.start,
    segmentEnd: segment.end,
    completed: task.completed,
  };
}

// buildTaskDeadlineRow is a Deadline-only Task's row (#337, placement
// precedence's `allDay` surface, ADR-0083): sits among this day's all-day
// rows, after every all-day Occurrence (`rowSortGroup`), labelled "Due"
// rather than carrying a time of day — Agenda places a Deadline by day only,
// the same as the all-day lane does. `segmentStart`/`segmentEnd` are pinned
// to this day's midnight (not the Deadline's own time of day) so the
// now-line, which only reads `segmentStart`, always treats it as already
// under way, the same as an all-day Occurrence's row.
function buildTaskDeadlineRow(task: Task, day: Date, taskLists: TaskList[]): AgendaRow {
  const taskList = getTaskListById(taskLists, task.taskListId);
  const dayStart = startOfDay(day);
  return {
    key: `task-${task.id}`,
    kind: "task",
    occurrence: null,
    task,
    allDay: true,
    timeLabel: "Due",
    dayLabel: null,
    title: task.title,
    color: resolveCalendarFill(taskList),
    calendarName: taskList?.name ?? "Unknown",
    location: undefined,
    hasConferenceUrl: false,
    dimmed: false,
    overlaps: false,
    segmentStart: dayStart,
    segmentEnd: dayStart,
    completed: task.completed,
  };
}

// rowBoundsGapsAndOverlaps (#337) is whether `row` counts toward Gaps and
// Overlaps — the same "every timed Occurrence, whatever its Busy value"
// rule #333/#334 already apply, extended to Time-blocked Tasks: an all-day
// row (an Occurrence's or a Deadline-only Task's "Due" row alike) never
// bounds either, and neither does a *completed* Time-blocked Task's row —
// that time is no longer committed, the same reason completing one leaves
// the Conflict set alone. An incomplete Time-blocked Task's row bounds both,
// same as a timed Occurrence.
function rowBoundsGapsAndOverlaps(row: AgendaRow): boolean {
  return !row.allDay && !(row.kind === "task" && row.completed);
}

// computeOverlapFlags flags a timed row when it starts before the latest end
// among the earlier boundable rows of its day (#333, #337) — the running
// latest end, not simply the row directly above, so a three-row cluster
// measures against whichever earlier row reaches furthest. Busy plays no
// part: this states a fact about list order ("this starts before the one
// above has ended"), not a scheduling conflict. Measured on each row's
// day-clipped segment (#335), so a multi-day Occurrence's/Task's portion on
// this day is what counts, not its true (possibly other-day) start. `rows`
// must already be in display order (`rowSortGroup`'s comparator).
function computeOverlapFlags(rows: AgendaRow[]): boolean[] {
  let latestEnd: Date | null = null;
  return rows.map((row) => {
    if (!rowBoundsGapsAndOverlaps(row)) return false;
    const overlaps = latestEnd !== null && row.segmentStart < latestEnd;
    if (latestEnd === null || row.segmentEnd > latestEnd) latestEnd = row.segmentEnd;
    return overlaps;
  });
}

// applyOverlapFlags patches each already-built, already-sorted row's
// `overlaps` with computeOverlapFlags' verdict — kept as a pass over the
// finished rows, rather than threaded through each row's own builder, so
// every row kind (Occurrence, Time-blocked Task, Deadline-only Task) shares
// one computation without each builder needing to know about the others.
function applyOverlapFlags(rows: AgendaRow[]): AgendaRow[] {
  const flags = computeOverlapFlags(rows);
  return rows.map((row, index) => ({ ...row, overlaps: flags[index] }));
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
// stretch covered by no boundable row (`rowBoundsGapsAndOverlaps`, #337) —
// whatever its Busy value for an Occurrence, regardless of completion for a
// Time-blocked Task's row, but never an all-day row, including a
// Deadline-only Task's "Due" one. Covered intervals merge (the running
// latest end, as in computeOverlapFlags), so a Gap starts at the latest end
// so far rather than the row directly above, and none is ever placed before
// the day's first boundable row or after its last. Measured on each row's
// day segment (#335), so a multi-day Occurrence's/Task's portion on this day
// is what a Gap runs against.
//
// On today, the Gap immediately after the now-line additionally clips its
// start to `now` and reads "… free until HH:MM" instead of "… free" — but
// only when nothing is in progress, i.e. `now` itself falls inside the gap.
// `rows` must already be in display order (`rowSortGroup`'s comparator), so
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
    if (!rowBoundsGapsAndOverlaps(row)) return;

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

// rowSortGroup is a row's place in a day group's three-tier order (#330,
// #337): all-day Occurrences, then Deadline-only Tasks' "Due" rows, then
// every timed row (an Occurrence's or a Time-blocked Task's alike) mixed
// together by start/end. Lower sorts first.
function rowSortGroup(row: Pick<AgendaRow, "allDay" | "kind">): number {
  if (!row.allDay) return 2;
  return row.kind === "task" ? 1 : 0;
}

// compareRows orders a day group's rows (#330, #335, #337): `rowSortGroup`
// first, then by start and then end — each row's day-clipped segment
// (#335), so a later day of a multi-day Occurrence/Task (clipped to that
// day's midnight) sorts as if it started then, not at its true, other-day
// start.
function compareRows(a: AgendaRow, b: AgendaRow): number {
  const groupDiff = rowSortGroup(a) - rowSortGroup(b);
  if (groupDiff !== 0) return groupDiff;
  if (a.segmentStart.getTime() !== b.segmentStart.getTime()) {
    return a.segmentStart.getTime() - b.segmentStart.getTime();
  }
  return a.segmentEnd.getTime() - b.segmentEnd.getTime();
}

// agendaEventCountLabel is a day group header's pluralized Occurrence count,
// e.g. "1 event" / "3 events" (#330).
export function agendaEventCountLabel(count: number): string {
  return `${count} ${count === 1 ? "event" : "events"}`;
}

// agendaTaskCountLabel is agendaEventCountLabel's counterpart for a day
// group's Tasks (#337), e.g. "1 task" / "3 tasks".
export function agendaTaskCountLabel(count: number): string {
  return `${count} ${count === 1 ? "task" : "tasks"}`;
}

// agendaHeaderLabel is a day group header's full count text (#337): both
// counts joined when both are nonzero ("2 events · 1 task"), whichever one
// is nonzero alone otherwise, and the zero-Occurrence form ("0 events") for
// an empty day group — zero parts are omitted, never rendered as "0 tasks".
export function agendaHeaderLabel(eventCount: number, taskCount: number): string {
  const parts: string[] = [];
  if (eventCount > 0) parts.push(agendaEventCountLabel(eventCount));
  if (taskCount > 0) parts.push(agendaTaskCountLabel(taskCount));
  return parts.length > 0 ? parts.join(" · ") : agendaEventCountLabel(0);
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

// AgendaTaskInput bundles buildAgenda's Task-related inputs (#337): Tasks
// already narrowed by the shared Task visibility rule (`visibleCalendarTasks`,
// #338) into the same two placement-precedence pools Day/Week and Month
// read from — `gridTasks` (Time-blocked) and `deadlineTasks` (Deadline-only)
// — plus their Task Lists (for color/name) and the viewer's zone (for
// `taskDeadlineFallsOnDay`'s per-day narrowing). Bundled as one object,
// defaulting to no Tasks at all, so every pre-existing call site (and test)
// that only ever dealt with Occurrences keeps working unchanged.
export interface AgendaTaskInput {
  gridTasks: Task[];
  deadlineTasks: Task[];
  taskLists: TaskList[];
  viewerZone: string;
}

const NO_TASKS: AgendaTaskInput = {
  gridTasks: [],
  deadlineTasks: [],
  taskLists: [],
  viewerZone: "UTC",
};

/**
 * The Agenda view's single source of rules (#330, ADR per #329; Tasks added
 * by #337): `occurrences` (already narrowed to what the grid would show —
 * the Active Calendar Set intersected with checked, per
 * `useVisibleOccurrences`), `taskInput` (already narrowed by the shared Task
 * visibility rule, #338) and `now` in, an ordered list of day groups,
 * collapsed empty runs and (possibly) a terminal end-of-list item out. The
 * React view is a thin renderer of this list.
 *
 * `windowDays` is the requested window's size in days from today —
 * `AGENDA_WINDOW_DAYS` by default, grown by the caller as the list's bottom
 * is reached (#336). When the days right before that boundary are empty,
 * this keeps walking past it in search of the next row to load straight
 * through to — at most a year past the boundary — rather than stopping on
 * an empty stretch; `occurrences`/`taskInput` must therefore cover whatever
 * of that year's worth of days might actually be searched, not just the
 * nominal window. If nothing is found within that year, the list ends with
 * a single terminal item instead of an open-ended empty run.
 */
export function buildAgenda(
  occurrences: Occurrence[],
  calendars: Calendar[],
  now: Date,
  timePattern: string,
  windowDays: number = AGENDA_WINDOW_DAYS,
  taskInput: AgendaTaskInput = NO_TASKS,
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
    // A multi-day Occurrence/Task Time block yields a segment on every day
    // it touches (#335, #337), so it appears — and counts toward
    // `eventCount`/`taskCount` — in each of those days' groups, not only the
    // one its start falls on.
    const occurrenceSegments = getDaySegments(occurrences, date);
    const gridTaskSegments = getTaskTimeBlockDaySegments(taskInput.gridTasks, date);
    const dayDeadlineTasks = taskInput.deadlineTasks.filter(
      (task) => task.due && taskDeadlineFallsOnDay(task.due, date, taskInput.viewerZone),
    );

    const isEmpty =
      occurrenceSegments.length === 0 && gridTaskSegments.length === 0 && dayDeadlineTasks.length === 0;

    if (isEmpty && !isToday) {
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
    const rows = applyOverlapFlags(
      [
        ...occurrenceSegments.map((segment) =>
          buildOccurrenceRow(segment, date, calendars, timePattern, now),
        ),
        ...gridTaskSegments.map((segment) =>
          buildTaskGridRow(segment, date, taskInput.taskLists, timePattern, now),
        ),
        ...dayDeadlineTasks.map((task) => buildTaskDeadlineRow(task, date, taskInput.taskLists)),
      ].sort(compareRows),
    );
    items.push({
      kind: "day",
      date,
      isToday,
      eventCount: occurrenceSegments.length,
      taskCount: gridTaskSegments.length + dayDeadlineTasks.length,
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
