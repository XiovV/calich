import { useState, type KeyboardEvent } from "react";
import { addDays, format, setHours, setMinutes, startOfDay } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import type { DraftBlock } from "../lib/gridTime";
import type { Event, Reminder } from "../lib/event";
import { isRecurringOccurrence, resolveMaster, type Occurrence } from "../lib/occurrence";
import { viewerZone } from "../lib/floatingTime";
import {
  shouldDiscardChildren,
  type EditScope,
  type EventFieldChanges,
} from "../lib/recurrenceScope";
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
import { useAuthStore } from "../lib/authStore";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Checkbox } from "../components/ui/Checkbox";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { DeleteEventConfirmation } from "./DeleteEventConfirmation";
import { CustomRecurrenceDialog } from "./CustomRecurrenceDialog";
import { ScopePicker } from "./ScopePicker";
import { DiscardRecurrenceWarning } from "./DiscardRecurrenceWarning";
import { ReminderRow } from "./ReminderRow";

/** A Reminder plus a local id, so its row keeps stable identity across
 * add/remove/reorder in the Reminders section (Reminder itself has no id —
 * it's just an offset + channel pair, ADR-0020). */
type ReminderDraft = Reminder & { draftId: string };

// The Repeat dropdown offers the fixed presets plus a "Custom…" entry that opens
// the Custom recurrence dialog (issue #35).
type RepeatChoice = RecurrencePreset | "custom";

type EventModalProps =
  | { mode: "create"; day: Date; draft: DraftBlock; onClose: () => void }
  | { mode: "edit"; occurrence: Occurrence; onClose: () => void };

interface InitialFormState {
  day: Date;
  startTime: string;
  endTime: string;
  title: string;
  calendarId: string;
  repeat: RepeatChoice;
  /** The stored rule when `repeat` is "custom"; undefined otherwise. */
  customRule: string | undefined;
  allDay: boolean;
  reminders: Reminder[];
}

function timeStringToDate(day: Date, time: string): Date {
  const [hours, minutes] = time.split(":").map(Number);
  return setMinutes(setHours(day, hours), minutes);
}

function deriveInitialFormState(
  props: EventModalProps,
  master: Event | undefined,
  firstCheckedCalendarId: string,
): InitialFormState {
  if (props.mode === "edit") {
    const { occurrence } = props;
    const { event } = occurrence;
    // The Repeat dropdown always reflects the series' actual rule (the
    // Master's), even when opening an Override, which never carries its own.
    const rrule = master?.rrule;
    const matched = matchPreset(rrule, occurrence.start);
    const repeat: RepeatChoice = !rrule ? "none" : (matched ?? "custom");
    return {
      day: startOfDay(occurrence.start),
      startTime: format(occurrence.start, "HH:mm"),
      endTime: format(occurrence.end, "HH:mm"),
      title: event.title,
      calendarId: event.calendarId,
      repeat,
      customRule: repeat === "custom" ? rrule : undefined,
      allDay: Boolean(event.allDay),
      reminders: event.reminders ?? [],
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
    allDay: false,
    reminders: [],
  };
}

export function EventModal(props: EventModalProps) {
  const { mode, onClose } = props;

  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);
  const addEvent = useEventsStore((state) => state.addEvent);
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const removeEvent = useEventsStore((state) => state.removeEvent);
  const editOccurrence = useEventsStore((state) => state.editOccurrence);
  const deleteOccurrence = useEventsStore((state) => state.deleteOccurrence);
  const calendars = useCalendarsStore((state) => state.calendars);
  const emailAvailable = useAuthStore(
    (state) => state.user?.emailReminderChannelAvailable ?? false,
  );

  const isRecurring = mode === "edit" && isRecurringOccurrence(props.occurrence);
  const master =
    mode === "edit"
      ? (resolveMaster(events, props.occurrence) ?? props.occurrence.event)
      : undefined;
  const masterHasChildren =
    master !== undefined &&
    (events.some((event) => event.parentId === master.id) ||
      (master.exdates?.length ?? 0) > 0);

  const eventForOptions = mode === "edit" ? props.occurrence.event : undefined;
  const checkedCalendars = getCheckedCalendars(calendars, checkedCalendarIds);
  // An event's calendar may have been unchecked (hidden) since it was created —
  // still include it as an option so its current selection isn't silently lost.
  const calendarOptions =
    eventForOptions && !checkedCalendars.some((c) => c.id === eventForOptions.calendarId)
      ? [
          ...checkedCalendars,
          ...calendars.filter((c) => c.id === eventForOptions.calendarId),
        ]
      : checkedCalendars;

  const [initial] = useState(() =>
    deriveInitialFormState(props, master, checkedCalendars[0]?.id ?? ""),
  );
  const { day } = initial;

  const [title, setTitle] = useState(initial.title);
  const [startTime, setStartTime] = useState(initial.startTime);
  const [endTime, setEndTime] = useState(initial.endTime);
  const [calendarId, setCalendarId] = useState(initial.calendarId);
  const [allDay, setAllDay] = useState(initial.allDay);
  const [repeat, setRepeat] = useState<RepeatChoice>(initial.repeat);
  const [customRule, setCustomRule] = useState<string | undefined>(
    initial.customRule,
  );
  const [reminders, setReminders] = useState<ReminderDraft[]>(() =>
    initial.reminders.map((reminder) => ({ ...reminder, draftId: crypto.randomUUID() })),
  );
  const [isCustomDialogOpen, setIsCustomDialogOpen] = useState(false);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);
  const [pendingEditChanges, setPendingEditChanges] = useState<
    (EventFieldChanges & { rrule?: string }) | null
  >(null);
  const [isDiscardWarningOpen, setIsDiscardWarningOpen] = useState(false);
  const [isDeleteScopePickerOpen, setIsDeleteScopePickerOpen] = useState(false);

  // Preset labels are derived from the event's start date, so e.g. "Weekly on
  // Tuesday" tracks the day the event lives on.
  const startForRule = allDay ? startOfDay(day) : timeStringToDate(day, startTime);
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

  function handleAddReminder() {
    setReminders((current) => [
      ...current,
      {
        draftId: crypto.randomUUID(),
        offsetMinutes: allDay ? 1440 : 10,
        channel: "notification",
      },
    ]);
  }

  function handleReminderChange(draftId: string, reminder: Reminder) {
    setReminders((current) =>
      current.map((r) => (r.draftId === draftId ? { ...reminder, draftId } : r)),
    );
  }

  function handleRemoveReminder(draftId: string) {
    setReminders((current) => current.filter((r) => r.draftId !== draftId));
  }

  // All-day skips the time-range check entirely — there's no time inputs to
  // validate (ADR-0017).
  const isTimeRangeValid =
    allDay || timeStringToDate(day, endTime) > timeStringToDate(day, startTime);
  const canSave = title.trim() !== "" && calendarId !== "" && isTimeRangeValid;

  function handleSave() {
    if (!canSave) return;

    // An all-day Event's start/end are whole dates: start is the day itself,
    // end is the exclusive next day (ADR-0017).
    const start = allDay ? startOfDay(day) : timeStringToDate(day, startTime);
    const end = allDay ? addDays(startOfDay(day), 1) : timeStringToDate(day, endTime);
    const rrule = repeat === "custom" ? customRule : buildRule(repeat, start);
    const reminderChanges: Reminder[] = reminders.map((r) => ({
      offsetMinutes: r.offsetMinutes,
      channel: r.channel,
    }));
    const changes = {
      calendarId,
      title: title.trim(),
      start,
      end,
      allDay,
      rrule,
      reminders: reminderChanges,
    };

    if (mode !== "edit") {
      // A newly created Event is stamped with the creator's Viewer zone; an
      // all-day Event stays timezone-free (ADR-0017, ADR-0019).
      const tzid = allDay ? undefined : viewerZone();
      addEvent({ id: crypto.randomUUID(), ...changes, tzid });
      onClose();
      return;
    }

    if (!isRecurring) {
      updateEvent(props.occurrence.event.id, changes);
      onClose();
      return;
    }

    if (master && shouldDiscardChildren(master.rrule, rrule)) {
      // A rule change is forced to "All events"; warn first only if there's
      // something to lose.
      if (masterHasChildren) {
        setPendingEditChanges(changes);
        setIsDiscardWarningOpen(true);
        return;
      }
      editOccurrence(props.occurrence, "all", changes);
      onClose();
      return;
    }

    // The rule is unchanged — ask which Occurrences the edit applies to.
    setPendingEditChanges(changes);
  }

  function handleConfirmDiscardWarning() {
    if (mode === "edit" && pendingEditChanges) {
      editOccurrence(props.occurrence, "all", pendingEditChanges);
    }
    setIsDiscardWarningOpen(false);
    setPendingEditChanges(null);
    onClose();
  }

  function handleScopeConfirm(scope: EditScope) {
    if (mode === "edit" && pendingEditChanges) {
      editOccurrence(props.occurrence, scope, pendingEditChanges);
    }
    setPendingEditChanges(null);
    onClose();
  }

  function handleDeleteClick() {
    // A recurring Occurrence's scope picker doubles as its delete
    // confirmation; a non-recurring Event still gets the plain "sure?" prompt.
    if (mode === "edit" && isRecurring) {
      setIsDeleteScopePickerOpen(true);
      return;
    }
    setIsDeleteConfirmOpen(true);
  }

  function handleConfirmDelete() {
    if (mode !== "edit") return;
    removeEvent(props.occurrence.event.id);
    onClose();
  }

  function handleDeleteScopeConfirm(scope: EditScope) {
    if (mode === "edit") {
      deleteOccurrence(props.occurrence, scope);
    }
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

            <label className="mt-4 flex items-center gap-2 text-label-sm text-ink">
              <Checkbox
                checked={allDay}
                onCheckedChange={setAllDay}
                aria-label="All day"
              />
              All day
            </label>

            {!allDay && (
              <>
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
              </>
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

            <div className="mt-4">
              <p className="mb-1.5 text-label-sm text-ink-muted">Reminders</p>
              <div className="flex flex-col gap-2">
                {reminders.map((reminder) => (
                  <ReminderRow
                    key={reminder.draftId}
                    reminder={reminder}
                    allDay={allDay}
                    emailAvailable={emailAvailable}
                    onChange={(next) => handleReminderChange(reminder.draftId, next)}
                    onRemove={() => handleRemoveReminder(reminder.draftId)}
                  />
                ))}
              </div>
              <button
                type="button"
                onClick={handleAddReminder}
                className="mt-1.5 text-label-sm text-accent hover:underline"
              >
                Add reminder
              </button>
            </div>

            <div className="mt-5 flex items-center justify-between gap-2">
              {mode === "edit" ? (
                <Button
                  variant="outline"
                  color="danger"
                  size="small"
                  onClick={handleDeleteClick}
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
          event={props.occurrence.event}
          onConfirm={handleConfirmDelete}
          onClose={() => setIsDeleteConfirmOpen(false)}
        />
      )}
      {mode === "edit" && isDeleteScopePickerOpen && (
        <ScopePicker
          action="Delete"
          onConfirm={handleDeleteScopeConfirm}
          onClose={() => setIsDeleteScopePickerOpen(false)}
        />
      )}
      {pendingEditChanges && !isDiscardWarningOpen && (
        <ScopePicker
          action="Edit"
          onConfirm={handleScopeConfirm}
          onClose={() => setPendingEditChanges(null)}
        />
      )}
      {isDiscardWarningOpen && (
        <DiscardRecurrenceWarning
          onConfirm={handleConfirmDiscardWarning}
          onClose={() => {
            setIsDiscardWarningOpen(false);
            setPendingEditChanges(null);
          }}
        />
      )}
    </>
  );
}
