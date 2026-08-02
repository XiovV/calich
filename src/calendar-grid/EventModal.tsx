import { useState } from "react";
import { format, setHours, setMinutes, startOfDay } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import { Field } from "@base-ui/react/field";
import type { DraftBlock } from "../lib/gridTime";
import type { Event } from "../lib/mockEvents";
import { getCheckedCalendars } from "../lib/mockCalendars";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { REMINDER_PRESETS } from "../lib/reminderPresets";
import { Select } from "../components/ui/Select";
import { Checkbox } from "../components/ui/Checkbox";
import { DeleteEventConfirmation } from "./DeleteEventConfirmation";

type EventModalProps =
  | { mode: "create"; day: Date; draft: DraftBlock; onClose: () => void }
  | { mode: "edit"; event: Event; onClose: () => void };

interface InitialFormState {
  day: Date;
  startTime: string;
  endTime: string;
  title: string;
  calendarId: string;
  reminders: number[];
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
    return {
      day: startOfDay(event.start),
      startTime: format(event.start, "HH:mm"),
      endTime: format(event.end, "HH:mm"),
      title: event.title,
      calendarId: event.calendarId,
      reminders: event.reminders,
    };
  }

  return {
    day: props.day,
    startTime: format(props.draft.start, "HH:mm"),
    endTime: format(props.draft.end, "HH:mm"),
    title: "",
    calendarId: firstCheckedCalendarId,
    reminders: [],
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
  const [selectedReminders, setSelectedReminders] = useState<number[]>(
    initial.reminders,
  );
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  const isTimeRangeValid =
    timeStringToDate(day, endTime) > timeStringToDate(day, startTime);
  const canSave = title.trim() !== "" && calendarId !== "" && isTimeRangeValid;

  function toggleReminder(minutes: number) {
    setSelectedReminders((current) =>
      current.includes(minutes)
        ? current.filter((value) => value !== minutes)
        : [...current, minutes],
    );
  }

  function handleSave() {
    if (!canSave) return;

    const start = timeStringToDate(day, startTime);
    const end = timeStringToDate(day, endTime);

    if (mode === "edit") {
      updateEvent(props.event.id, {
        calendarId,
        title: title.trim(),
        start,
        end,
        reminders: selectedReminders,
      });
    } else {
      addEvent({
        id: crypto.randomUUID(),
        calendarId,
        title: title.trim(),
        start,
        end,
        reminders: selectedReminders,
      });
    }
    onClose();
  }

  function handleConfirmDelete() {
    if (mode !== "edit") return;
    removeEvent(props.event.id);
    onClose();
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
          <Dialog.Backdrop className="fixed inset-0 bg-ink/20" />
          <Dialog.Popup className="fixed top-1/2 left-1/2 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
            <Dialog.Title className="text-heading font-medium text-ink">
              {mode === "edit" ? "Edit event" : "New event"}
            </Dialog.Title>

            <Field.Root className="mt-4">
              <Field.Label className="block text-label-sm text-ink-muted">
                Title
              </Field.Label>
              <Field.Control
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="Add title"
                className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
              />
            </Field.Root>

            <div className="mt-4 flex gap-3">
              <Field.Root className="flex-1">
                <Field.Label className="block text-label-sm text-ink-muted">
                  Start
                </Field.Label>
                <Field.Control
                  type="time"
                  value={startTime}
                  onChange={(event) => setStartTime(event.target.value)}
                  className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
                />
              </Field.Root>
              <Field.Root className="flex-1">
                <Field.Label className="block text-label-sm text-ink-muted">
                  End
                </Field.Label>
                <Field.Control
                  type="time"
                  value={endTime}
                  onChange={(event) => setEndTime(event.target.value)}
                  className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
                />
              </Field.Root>
            </div>
            {!isTimeRangeValid && (
              <p className="mt-1 text-label-sm text-calendar-tomato">
                End time must be after start time.
              </p>
            )}

            <div className="mt-4">
              <p className="text-label-sm text-ink-muted">Calendar</p>
              <div className="mt-1">
                <Select
                  value={calendarId}
                  onValueChange={setCalendarId}
                  options={calendarOptions.map((calendar) => ({
                    value: calendar.id,
                    label: calendar.name,
                  }))}
                  aria-label="Calendar"
                />
              </div>
            </div>

            <div className="mt-4">
              <p className="text-label-sm text-ink-muted">Reminders</p>
              <ul className="mt-1 flex flex-col gap-1.5">
                {REMINDER_PRESETS.map((preset) => (
                  <li key={preset.minutes} className="flex items-center gap-2">
                    <Checkbox
                      checked={selectedReminders.includes(preset.minutes)}
                      onCheckedChange={() => toggleReminder(preset.minutes)}
                      aria-label={preset.label}
                    />
                    <span className="text-body text-ink">{preset.label}</span>
                  </li>
                ))}
              </ul>
            </div>

            <div className="mt-5 flex items-center justify-between gap-2">
              {mode === "edit" ? (
                <button
                  type="button"
                  onClick={() => setIsDeleteConfirmOpen(true)}
                  className="rounded-shell-sm border border-border px-3 py-1.5 text-body text-calendar-tomato hover:bg-surface-hover"
                >
                  Delete
                </button>
              ) : (
                <span />
              )}
              <div className="flex gap-2">
                <Dialog.Close className="rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink hover:bg-surface-hover">
                  Cancel
                </Dialog.Close>
                <button
                  type="button"
                  onClick={handleSave}
                  disabled={!canSave}
                  className="rounded-shell-sm bg-accent px-3 py-1.5 text-body text-ink-inverse hover:bg-accent-hover disabled:opacity-50"
                >
                  Save
                </button>
              </div>
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
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
