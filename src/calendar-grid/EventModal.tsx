import { useState } from "react";
import { format, setHours, setMinutes } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import { Field } from "@base-ui/react/field";
import type { DraftBlock } from "../lib/gridTime";
import { getCheckedCalendars } from "../lib/mockCalendars";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { REMINDER_PRESETS } from "../lib/reminderPresets";
import { Select } from "../components/ui/Select";
import { Checkbox } from "../components/ui/Checkbox";

interface EventModalProps {
  day: Date;
  draft: DraftBlock;
  onClose: () => void;
}

function timeStringToDate(day: Date, time: string): Date {
  const [hours, minutes] = time.split(":").map(Number);
  return setMinutes(setHours(day, hours), minutes);
}

export function EventModal({ day, draft, onClose }: EventModalProps) {
  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const addEvent = useEventsStore((state) => state.addEvent);
  const calendars = useCalendarsStore((state) => state.calendars);

  const checkedCalendars = getCheckedCalendars(calendars, checkedCalendarIds);

  const [title, setTitle] = useState("");
  const [startTime, setStartTime] = useState(() => format(draft.start, "HH:mm"));
  const [endTime, setEndTime] = useState(() => format(draft.end, "HH:mm"));
  const [calendarId, setCalendarId] = useState(
    () => checkedCalendars[0]?.id ?? "",
  );
  const [selectedReminders, setSelectedReminders] = useState<number[]>([]);

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

    addEvent({
      id: crypto.randomUUID(),
      calendarId,
      title: title.trim(),
      start: timeStringToDate(day, startTime),
      end: timeStringToDate(day, endTime),
      reminders: selectedReminders,
    });
    onClose();
  }

  return (
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
            New event
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
                options={checkedCalendars.map((calendar) => ({
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

          <div className="mt-5 flex justify-end gap-2">
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
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
