import { useEffect, useRef, useState } from "react";
import { format } from "date-fns";
import { AttachmentIndicator } from "./AttachmentIndicator";
import { MonthTaskChip } from "./MonthTaskChip";
import { WriteBackErrorIndicator } from "./WriteBackErrorIndicator";
import { occurrenceKey, type Occurrence } from "../lib/occurrence";
import type { DraftBlock } from "../lib/gridTime";
import { canWriteCalendarEvents, getCalendarById } from "../lib/calendar";
import { getOccurrenceBlockStyle } from "../lib/calendarColors";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useShellStore } from "../lib/shellStore";
import { useTaskListsStore } from "../lib/taskListsStore";
import { getTaskListById } from "../lib/taskListsApi";
import type { Task } from "../lib/tasksApi";
import { computeCellDraft, computeChipCapacity } from "../lib/monthGrid";
import { useTimePattern } from "../hooks/useTimePattern";

const CHIP_ROW_HEIGHT_PX = 20;
const MORE_ROW_HEIGHT_PX = 20;

interface MonthDayCellProps {
  date: Date;
  inCurrentMonth: boolean;
  isToday: boolean;
  isDragHover: boolean;
  occurrences: Occurrence[];
  /** Every Task placed on this date (#315, ADR-0083) — the caller has
   * already applied placement precedence (Time block's date, else the
   * Deadline's), "Show tasks on calendar" and "Show completed", the same
   * division `TimeGrid` makes before handing `DayColumn`/`AllDayLane` their
   * own day-scoped Task lists. */
  tasks: Task[];
  onDraftCreated: (day: Date, draft: DraftBlock) => void;
  onOccurrenceClick: (occurrence: Occurrence) => void;
  onOccurrenceDragStart: (
    occurrence: Occurrence,
    clientX: number,
    clientY: number,
  ) => void;
  onTaskDragStart: (task: Task, clientX: number, clientY: number) => void;
}

// One cell's chips, Occurrences and Tasks merged into a single chronological
// list (#315) so a User planning a month sees both in the order they'd
// actually happen that day, rather than every Occurrence before every Task
// regardless of time. A Deadline-only Task (no Time block) sorts by its
// Deadline instant the same way an all-day Occurrence sorts by midnight —
// near the top, since both carry no real time of day.
type MonthChipItem =
  | { kind: "occurrence"; occurrence: Occurrence; time: number }
  | { kind: "task"; task: Task; time: number };

export function MonthDayCell({
  date,
  inCurrentMonth,
  isToday,
  isDragHover,
  occurrences,
  tasks,
  onDraftCreated,
  onOccurrenceClick,
  onOccurrenceDragStart,
  onTaskDragStart,
}: MonthDayCellProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const setSelectedDate = useShellStore((state) => state.setSelectedDate);
  const setActiveView = useShellStore((state) => state.setActiveView);
  const timePattern = useTimePattern();

  const eventsContainerRef = useRef<HTMLDivElement>(null);
  const [availableHeight, setAvailableHeight] = useState(0);

  useEffect(() => {
    const container = eventsContainerRef.current;
    if (!container) return;

    const observer = new ResizeObserver(([entry]) => {
      setAvailableHeight(entry.contentRect.height);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  function openDayView() {
    setSelectedDate(date);
    setActiveView("day");
  }

  function handleCellClick() {
    onDraftCreated(date, computeCellDraft(date, new Date()));
  }

  const chipItems: MonthChipItem[] = [
    ...occurrences.map((occurrence) => ({
      kind: "occurrence" as const,
      occurrence,
      time: occurrence.start.getTime(),
    })),
    // A Task here is already guaranteed to carry a Time block or a Deadline
    // (never neither) by placement precedence — the caller only ever hands
    // this cell a Task that fell through `taskFallsOnMonthDay`.
    ...tasks.map((task) => ({
      kind: "task" as const,
      task,
      time: (task.start ?? (task.due as Date)).getTime(),
    })),
  ].sort((a, b) => a.time - b.time);

  const { visibleCount, overflowCount } = computeChipCapacity(
    chipItems.length,
    availableHeight,
    CHIP_ROW_HEIGHT_PX,
    MORE_ROW_HEIGHT_PX,
  );
  const visibleChipItems = chipItems.slice(0, visibleCount);

  return (
    <div
      onClick={handleCellClick}
      data-cell-date={date.toDateString()}
      className={`flex min-h-0 flex-col gap-1 overflow-hidden border-b border-l border-border p-1 ${inCurrentMonth ? "" : "bg-surface-sunken"} ${isDragHover ? "bg-accent-soft" : ""}`}
    >
      <button
        type="button"
        onClick={(domEvent) => {
          domEvent.stopPropagation();
          openDayView();
        }}
        className={`shrink-0 self-start text-label-sm ${inCurrentMonth ? "text-ink" : "text-ink-muted"} ${
          isToday
            ? "flex h-5 w-5 items-center justify-center rounded-shell-pill bg-accent text-on-accent"
            : "cursor-pointer rounded-shell-sm px-1 hover:bg-surface-hover"
        }`}
      >
        {format(date, "d")}
      </button>
      <div
        ref={eventsContainerRef}
        className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-hidden"
      >
        {visibleChipItems.map((item) => {
          if (item.kind === "task") {
            return (
              <MonthTaskChip
                key={`task-${item.task.id}`}
                task={item.task}
                taskList={getTaskListById(taskLists, item.task.taskListId)}
                timePattern={timePattern}
                onDragStart={onTaskDragStart}
              />
            );
          }

          const occurrence = item.occurrence;
          const calendar = getCalendarById(
            calendars,
            occurrence.event.calendarId,
          );
          const blockStyle = getOccurrenceBlockStyle(occurrence.event, calendar);
          // An Occurrence on a Calendar the caller can't write doesn't drag
          // (#111, ADR-0034) — the gesture below is never started for one,
          // so its real mouse clicks fall through to the keyboard-only
          // branch instead.
          const isReadOnly = !canWriteCalendarEvents(calendar);
          return (
            <button
              key={occurrenceKey(occurrence)}
              type="button"
              onMouseDown={(domEvent) => {
                domEvent.stopPropagation();
                if (isReadOnly) return;
                onOccurrenceDragStart(
                  occurrence,
                  domEvent.clientX,
                  domEvent.clientY,
                );
              }}
              onClick={(domEvent) => {
                domEvent.stopPropagation();
                // Real mouse clicks are handled by the mousedown/mouseup
                // drag-vs-click distance check started above; only keyboard
                // activation (Enter/Space, event.detail === 0) reaches here.
                if (domEvent.detail === 0 || isReadOnly) {
                  onOccurrenceClick(occurrence);
                }
              }}
              style={blockStyle}
              className="flex w-full cursor-pointer items-center gap-1 rounded-shell-sm px-1 text-left text-label-sm"
            >
              <span className="truncate">
                {occurrence.event.allDay
                  ? occurrence.event.title
                  : `${format(occurrence.start, timePattern)} ${occurrence.event.title}`}
              </span>
              <WriteBackErrorIndicator reason={occurrence.event.writeBackError} />
              <AttachmentIndicator
                hasAttachments={Boolean(occurrence.event.attachments?.length)}
              />
            </button>
          );
        })}
        {overflowCount > 0 && (
          <button
            type="button"
            onClick={(domEvent) => {
              domEvent.stopPropagation();
              openDayView();
            }}
            className="cursor-pointer truncate rounded-shell-sm px-1 text-left text-label-sm text-ink-muted hover:text-ink"
          >
            +{overflowCount} more
          </button>
        )}
      </div>
    </div>
  );
}
