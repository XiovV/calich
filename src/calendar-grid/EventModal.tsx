import { useState, type KeyboardEvent } from "react";
import { format, setHours, setMinutes, startOfDay } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import type { DraftBlock } from "../lib/gridTime";
import type { Event } from "../lib/event";
import {
  RECURRENCE_PRESETS,
  buildRule,
  matchPreset,
  presetLabel,
  type RecurrencePreset,
} from "../lib/recurrencePresets";
import {
  defaultCustomRecurrence,
  parseCustomRule,
  summarizeCustomRule,
} from "../lib/customRecurrence";
import { getCheckedCalendars } from "../lib/calendar";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { DeleteEventConfirmation } from "./DeleteEventConfirmation";
import { CustomRecurrenceDialog } from "./CustomRecurrenceDialog";

// The Repeat dropdown offers the fixed presets plus a "Custom…" entry that opens
// the Custom recurrence dialog (issue #35).
type RepeatChoice = RecurrencePreset | "custom";

type EventModalProps =
  | { mode: "create"; day: Date; draft: DraftBlock; onClose: () => void }
  | { mode: "edit"; event: Event; onClose: () => void };

interface InitialFormState {
  day: Date;
  startTime: string;
  endTime: string;
  title: string;
  calendarId: string;
  repeat: RepeatChoice;
  /** The stored rule when `repeat` is "custom"; undefined otherwise. */
  customRule: string | undefined;
}

function timeStringToDate(day: Date, time: string): Date {
  const [hours, minutes] = time.split(":").map(Number);
  return setMinutes(setHours(day, hours), minutes);
}

function deriveInitialFormState(
  props: EventModalProps,
  firstCheckedCalendarId: string,
): InitialFormState {
  if (props.mode === "edit") {
    const { event } = props;
    // A stored rule is either one of the presets or a custom recurrence; edit
    // re-opens whichever it was, with the custom dialog re-populated from it.
    const matched = matchPreset(event.rrule, event.start);
    const repeat: RepeatChoice = !event.rrule ? "none" : (matched ?? "custom");
    return {
      day: startOfDay(event.start),
      startTime: format(event.start, "HH:mm"),
      endTime: format(event.end, "HH:mm"),
      title: event.title,
      calendarId: event.calendarId,
      repeat,
      customRule: repeat === "custom" ? event.rrule : undefined,
    };
  }

  return {
    day: props.day,
    startTime: format(props.draft.start, "HH:mm"),
    endTime: format(props.draft.end, "HH:mm"),
    title: "",
    calendarId: firstCheckedCalendarId,
    repeat: "none",
    customRule: undefined,
  };
}

export function EventModal(props: EventModalProps) {
  const { mode, onClose } = props;

  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const addEvent = useEventsStore((state) => state.addEvent);
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const removeEvent = useEventsStore((state) => state.removeEvent);
  const calendars = useCalendarsStore((state) => state.calendars);

  const checkedCalendars = getCheckedCalendars(calendars, checkedCalendarIds);
  // An event's calendar may have been unchecked (hidden) since it was created —
  // still include it as an option so its current selection isn't silently lost.
  const calendarOptions =
    mode === "edit" && !checkedCalendars.some((c) => c.id === props.event.calendarId)
      ? [
          ...checkedCalendars,
          ...calendars.filter((c) => c.id === props.event.calendarId),
        ]
      : checkedCalendars;

  const [initial] = useState(() =>
    deriveInitialFormState(props, checkedCalendars[0]?.id ?? ""),
  );
  const { day } = initial;

  const [title, setTitle] = useState(initial.title);
  const [startTime, setStartTime] = useState(initial.startTime);
  const [endTime, setEndTime] = useState(initial.endTime);
  const [calendarId, setCalendarId] = useState(initial.calendarId);
  const [repeat, setRepeat] = useState<RepeatChoice>(initial.repeat);
  const [customRule, setCustomRule] = useState<string | undefined>(
    initial.customRule,
  );
  const [isCustomDialogOpen, setIsCustomDialogOpen] = useState(false);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  // Preset labels are derived from the event's start date, so e.g. "Weekly on
  // Tuesday" tracks the day the event lives on.
  const startForRule = timeStringToDate(day, startTime);
  const repeatOptions = [
    ...RECURRENCE_PRESETS.map((preset) => ({
      value: preset as RepeatChoice,
      label: presetLabel(preset, startForRule),
    })),
    {
      value: "custom" as RepeatChoice,
      label: customRule
        ? summarizeCustomRule(customRule, startForRule)
        : "Custom…",
    },
  ];

  function handleRepeatChange(value: RepeatChoice) {
    // Selecting "Custom…" opens the dialog rather than committing immediately;
    // the choice is confirmed only when the dialog's Done button fires.
    if (value === "custom") {
      setIsCustomDialogOpen(true);
      return;
    }
    setRepeat(value);
  }

  function handleCustomConfirm(rule: string) {
    setCustomRule(rule);
    setRepeat("custom");
    setIsCustomDialogOpen(false);
  }

  const isTimeRangeValid =
    timeStringToDate(day, endTime) > timeStringToDate(day, startTime);
  const canSave = title.trim() !== "" && calendarId !== "" && isTimeRangeValid;

  function handleSave() {
    if (!canSave) return;

    const start = timeStringToDate(day, startTime);
    const end = timeStringToDate(day, endTime);
    const rrule =
      repeat === "custom" ? customRule : buildRule(repeat, start);

    if (mode === "edit") {
      updateEvent(props.event.id, {
        calendarId,
        title: title.trim(),
        start,
        end,
        rrule,
      });
    } else {
      addEvent({
        id: crypto.randomUUID(),
        calendarId,
        title: title.trim(),
        start,
        end,
        rrule,
      });
    }
    onClose();
  }

  function handleConfirmDelete() {
    if (mode !== "edit") return;
    removeEvent(props.event.id);
    onClose();
  }

  // Submit on Enter from anywhere in the dialog (base-ui may leave focus on the
  // popup rather than a field — e.g. when editing, where inputs are pre-filled),
  // but not from buttons (Cancel/Delete/Select) or a textarea.
  function handleEnterToSave(event: KeyboardEvent) {
    const target = event.target as HTMLElement;
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      target.tagName !== "BUTTON" &&
      target.tagName !== "TEXTAREA"
    ) {
      event.preventDefault();
      handleSave();
    }
  }

  return (
    <>
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      >
        <Dialog.Portal>
          <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
          <Dialog.Popup
            onKeyDown={handleEnterToSave}
            className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3"
          >
            <Dialog.Title className="text-heading font-medium text-ink">
              {mode === "edit" ? "Edit event" : "New event"}
            </Dialog.Title>

            <form
              onSubmit={(event) => {
                event.preventDefault();
                handleSave();
              }}
            >
            <Input
              label="Title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="Add title"
              className="mt-4"
            />

            <div className="mt-4 flex gap-3">
              <Input
                label="Start"
                type="time"
                value={startTime}
                onChange={(event) => setStartTime(event.target.value)}
                invalid={!isTimeRangeValid}
                className="flex-1"
              />
              <Input
                label="End"
                type="time"
                value={endTime}
                onChange={(event) => setEndTime(event.target.value)}
                invalid={!isTimeRangeValid}
                className="flex-1"
              />
            </div>
            {!isTimeRangeValid && (
              <p className="mt-1 text-label-sm text-danger">
                End time must be after start time.
              </p>
            )}

            <div className="mt-4">
              <Select
                label="Calendar"
                value={calendarId}
                onValueChange={setCalendarId}
                options={calendarOptions.map((calendar) => ({
                  value: calendar.id,
                  label: calendar.name,
                }))}
              />
            </div>

            <div className="mt-4">
              <Select
                label="Repeat"
                value={repeat}
                onValueChange={handleRepeatChange}
                options={repeatOptions}
              />
              {repeat === "custom" && (
                <button
                  type="button"
                  onClick={() => setIsCustomDialogOpen(true)}
                  className="mt-1.5 text-label-sm text-accent hover:underline"
                >
                  Edit custom recurrence
                </button>
              )}
            </div>

            <div className="mt-5 flex items-center justify-between gap-2">
              {mode === "edit" ? (
                <Button
                  variant="outline"
                  color="danger"
                  size="small"
                  onClick={() => setIsDeleteConfirmOpen(true)}
                >
                  Delete
                </Button>
              ) : (
                <span />
              )}
              <div className="flex gap-2">
                <Dialog.Close
                  className={buttonClasses({
                    variant: "outline",
                    color: "secondary",
                    size: "small",
                  })}
                >
                  Cancel
                </Dialog.Close>
                <Button type="submit" size="small" disabled={!canSave}>
                  Save
                </Button>
              </div>
            </div>
            </form>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      {isCustomDialogOpen && (
        <CustomRecurrenceDialog
          start={startForRule}
          initial={
            customRule
              ? parseCustomRule(customRule, startForRule)
              : defaultCustomRecurrence(startForRule)
          }
          onConfirm={handleCustomConfirm}
          onClose={() => setIsCustomDialogOpen(false)}
        />
      )}
      {mode === "edit" && isDeleteConfirmOpen && (
        <DeleteEventConfirmation
          event={props.event}
          onConfirm={handleConfirmDelete}
          onClose={() => setIsDeleteConfirmOpen(false)}
        />
      )}
    </>
  );
}
