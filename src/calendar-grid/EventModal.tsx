import { useRef, useState, type DragEvent, type KeyboardEvent } from "react";
import { addDays, format, setHours, setMinutes, startOfDay } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import { Download } from "lucide-react";
import type { DraftBlock } from "../lib/gridTime";
import type { Attachment, Event, Reminder } from "../lib/event";
import { attachmentsApi } from "../lib/attachmentsApi";
import { downloadBlob } from "../lib/downloadBlob";
import { errorMessage } from "../lib/errorMessage";
import { isRecurringOccurrence, resolveMaster, type Occurrence } from "../lib/occurrence";
import { viewerZone } from "../lib/floatingTime";
import {
  hasFieldChanges,
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
import {
  calendarHasOtherRecipients,
  calendarPickerEmptyReason,
  calendarPickerLabel,
  calendarReadOnlyReason,
  canWriteCalendarEvents,
  defaultCalendarId,
  getCalendarById,
  getCheckedCalendars,
  type CalendarPickerEmptyReason,
} from "../lib/calendar";
import { resolveCalendarFill } from "../lib/calendarColors";
import { ColorSwatchPicker } from "../components/layout/ColorSwatchPicker";
import { CalendarModal } from "../components/layout/CalendarModal";
import { useShellStore } from "../lib/shellStore";
import { useEventsStore } from "../lib/eventsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useAuthStore } from "../lib/authStore";
import { icsApi, type ExportSummary } from "../lib/icsApi";
import { toast } from "../lib/toast";
import { ExportSummaryDialog } from "../components/ExportSummaryDialog";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Textarea } from "../components/ui/Textarea";
import { Checkbox } from "../components/ui/Checkbox";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { buttonClasses } from "../components/ui/buttonClasses";
import { DeleteEventConfirmation } from "./DeleteEventConfirmation";
import { CustomRecurrenceDialog } from "./CustomRecurrenceDialog";
import { ScopePicker } from "./ScopePicker";
import { DiscardRecurrenceWarning } from "./DiscardRecurrenceWarning";
import { ReminderRow } from "./ReminderRow";
import { ReminderOverrideControl } from "./ReminderOverrideControl";
import { AttachmentRow, type AttachmentDraft } from "./AttachmentRow";
import { EventAttendeesSection } from "./EventAttendeesSection";

/** A Reminder plus a local id, so its row keeps stable identity across
 * add/remove/reorder in the Reminders section (Reminder itself has no id —
 * it's just an offset + channel pair, ADR-0020). */
type ReminderDraft = Reminder & { draftId: string };

// The Repeat dropdown offers the fixed presets plus a "Custom…" entry that opens
// the Custom recurrence dialog (issue #35).
type RepeatChoice = RecurrencePreset | "custom";

// Copy for the Calendar picker's empty state (#174), keyed the same as
// CalendarPickerEmptyReason — one map instead of a branch per render site.
const CALENDAR_EMPTY_STATE_COPY: Record<CalendarPickerEmptyReason, string> = {
  none: "You don't have any calendars yet.",
  hidden: "Your calendars are all hidden. Show one in the sidebar to add events to it.",
  unwritable:
    "You don't have a calendar you can add events to — everything you can see is shared with view-only access.",
};

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
  description: string;
  location: string;
  /** This Event's own color override (ADR-0043) — undefined means inherited,
   * never the Calendar's resolved hex, so a later Reset has an absent value
   * to fall back to instead of copying whatever was already showing. */
  color: string | undefined;
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
      description: event.description ?? "",
      location: event.location ?? "",
      color: event.color,
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
    description: "",
    location: "",
    color: undefined,
  };
}

export function EventModal(props: EventModalProps) {
  const { mode, onClose } = props;

  const checkedCalendarIds = useShellStore((state) => state.checkedCalendarIds);
  const events = useEventsStore((state) => state.events);
  const fetchEvents = useEventsStore((state) => state.fetchEvents);
  const addEvent = useEventsStore((state) => state.addEvent);
  const updateEvent = useEventsStore((state) => state.updateEvent);
  const removeEvent = useEventsStore((state) => state.removeEvent);
  const editOccurrence = useEventsStore((state) => state.editOccurrence);
  const deleteOccurrence = useEventsStore((state) => state.deleteOccurrence);
  const calendars = useCalendarsStore((state) => state.calendars);
  const emailAvailable = useAuthStore(
    (state) => state.user?.emailReminderChannelAvailable ?? false,
  );
  const accessToken = useAuthStore((state) => state.accessToken);

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
  // Only a Calendar the caller may write Events to is a valid write target
  // (#111, ADR-0034) — a Subscribed Calendar (Refresh is its only
  // legitimate writer, #84, ADR-0032) and a Calendar the caller has no more
  // than Viewer Access to both never appear as Calendar picker options,
  // whether checked or not.
  const writableCalendars = calendars.filter((c) => canWriteCalendarEvents(c));
  const checkedCalendars = getCheckedCalendars(writableCalendars, checkedCalendarIds);
  // An event's calendar may have been unchecked (hidden) since it was created —
  // still include it as an option so its current selection isn't silently lost.
  const calendarOptions =
    eventForOptions && !checkedCalendars.some((c) => c.id === eventForOptions.calendarId)
      ? [
          ...checkedCalendars,
          ...writableCalendars.filter((c) => c.id === eventForOptions.calendarId),
        ]
      : checkedCalendars;

  // An Occurrence whose own Calendar the caller can't write can't be saved
  // or deleted — every write to it is refused server-side (#111, ADR-0034)
  // — so the form offers neither affordance rather than letting Save/Delete
  // fail. readOnlyReason distinguishes why, for copy only (#111): a
  // subscriber already knows their feed is read-only; a Viewer without an
  // Owner's context does not (#109).
  const editedCalendar = eventForOptions
    ? getCalendarById(calendars, eventForOptions.calendarId)
    : undefined;
  // Only an edited Event can be read-only — a new one is always being
  // created on a Calendar the picker already limited to writableCalendars
  // above, so editedCalendar (undefined in create mode) must never make
  // canWriteCalendarEvents' undefined-is-unwritable default apply here too.
  const isReadOnlyEvent = mode === "edit" && !canWriteCalendarEvents(editedCalendar);
  const readOnlyReason = calendarReadOnlyReason(editedCalendar);
  // resolveCalendarFill's fallback for an Attendee-only Event (ADR-0046):
  // editedCalendar is undefined (no calendarsStore entry, no Calendar
  // Access), so the Color swatch would otherwise default to the generic
  // unresolved gray even when the wire already told us the real color.
  const attendeeOnlyCalendarColor: { color: string } | undefined =
    mode === "edit" && !editedCalendar && props.occurrence.event.calendarColor
      ? { color: props.occurrence.event.calendarColor }
      : undefined;
  // The personal Reminder override control (#117) is only meaningful once
  // there's someone else to distinguish "for everyone" from "for me" —
  // absent on a Calendar nobody else can see. Only an existing Event has an
  // id to key an override to.
  const showReminderOverride = mode === "edit" && calendarHasOtherRecipients(editedCalendar);
  // Attribution (#118) uses the same predicate as the Reminder override
  // control: absent on a Calendar nobody else can see, since it would only
  // tell the caller what they already know. Also absent when the Event
  // carries no creator — a deleted account or an Event predating the
  // column — rather than rendering an empty "Created by" line.
  const createdByName =
    mode === "edit" && calendarHasOtherRecipients(editedCalendar)
      ? props.occurrence.event.createdByName
      : undefined;
  const showAttachmentUploader = mode === "edit" && calendarHasOtherRecipients(editedCalendar);

  // #174: when no Calendar is a valid write target, the picker's empty
  // state names why instead of rendering an empty dropdown next to a
  // silently disabled Save — the remedy differs per reason (create one,
  // show one, or neither applies).
  const calendarEmptyReason =
    calendarOptions.length === 0
      ? calendarPickerEmptyReason(calendars, checkedCalendarIds)
      : undefined;

  const [initial] = useState(() =>
    deriveInitialFormState(props, master, defaultCalendarId(checkedCalendars)),
  );
  const { day } = initial;

  // Attachments (#132, ADR-0040) belong to the Master, never an Override —
  // master (not props.occurrence.event) is deliberately what every
  // attachment action below targets, edit mode or create. createdEventId is
  // set once a brand-new Event's create POST has succeeded, since an
  // upload needs a row to reference; until then, a picked/dropped file just
  // sits in attachmentDrafts as "pending" (ADR-0040).
  const [createdEventId, setCreatedEventId] = useState<string | null>(null);
  const attachmentEventId = mode === "edit" ? master?.id : (createdEventId ?? undefined);
  const [attachmentDrafts, setAttachmentDrafts] = useState<AttachmentDraft[]>(() =>
    (master?.attachments ?? []).map((attachment) => ({
      draftId: attachment.id,
      status: "uploaded" as const,
      attachment,
    })),
  );
  const [isDraggingOverAttachments, setIsDraggingOverAttachments] = useState(false);
  const attachmentInputRef = useRef<HTMLInputElement>(null);

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
  const [description, setDescription] = useState(initial.description);
  const [location, setLocation] = useState(initial.location);
  const [color, setColor] = useState<string | undefined>(initial.color);
  const [isCustomDialogOpen, setIsCustomDialogOpen] = useState(false);
  const [isCreateCalendarOpen, setIsCreateCalendarOpen] = useState(false);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);
  const [pendingEditChanges, setPendingEditChanges] = useState<
    (EventFieldChanges & { rrule?: string }) | null
  >(null);
  const [isDiscardWarningOpen, setIsDiscardWarningOpen] = useState(false);
  const [isDeleteScopePickerOpen, setIsDeleteScopePickerOpen] = useState(false);
  const [isDownloadScopePickerOpen, setIsDownloadScopePickerOpen] = useState(false);
  const [pendingExportSummary, setPendingExportSummary] = useState<ExportSummary | null>(null);
  const [isConfirmingExport, setIsConfirmingExport] = useState(false);

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

  // startAttachmentUpload drives one file's XHR upload (attachmentsApi —
  // the app's one fetch-free call, for real progress, #132 ADR-0040),
  // reusing existingDraftId so a retry updates the same row rather than
  // adding a new one. Every state update first checks the draft is still
  // present, since removing a row mid-upload doesn't cancel the request —
  // there is nothing to write the result onto if it finishes anyway.
  function startAttachmentUpload(file: File, eventId: string, existingDraftId?: string) {
    const draftId = existingDraftId ?? crypto.randomUUID();
    setAttachmentDrafts((current) => {
      const uploading: AttachmentDraft = {
        draftId,
        status: "uploading",
        filename: file.name,
        sizeBytes: file.size,
        progress: 0,
      };
      const index = current.findIndex((draft) => draft.draftId === draftId);
      if (index === -1) return [...current, uploading];
      return current.map((draft) => (draft.draftId === draftId ? uploading : draft));
    });

    attachmentsApi
      .upload(accessToken!, eventId, file, (progress) => {
        setAttachmentDrafts((current) =>
          current.map((draft) =>
            draft.draftId === draftId && draft.status === "uploading"
              ? { ...draft, progress }
              : draft,
          ),
        );
      })
      .then((attachment) => {
        setAttachmentDrafts((current) => {
          if (!current.some((draft) => draft.draftId === draftId)) return current;
          return current.map((draft) =>
            draft.draftId === draftId ? { draftId, status: "uploaded", attachment } : draft,
          );
        });
        // Keeps the store's copy of the Event in sync, so reopening it later
        // (or another view rendering it) sees the Attachment too, without a
        // dedicated store mutation just for this one field.
        void fetchEvents();
      })
      .catch((err) => {
        setAttachmentDrafts((current) => {
          if (!current.some((draft) => draft.draftId === draftId)) return current;
          return current.map((draft) =>
            draft.draftId === draftId
              ? {
                  draftId,
                  status: "error",
                  filename: file.name,
                  sizeBytes: file.size,
                  file,
                  message: errorMessage(err),
                }
              : draft,
          );
        });
      });
  }

  // Uploads immediately once attachmentEventId exists (an existing Event,
  // or a just-created one); otherwise stages a "pending" draft, since
  // there's no row yet to upload against — the create POST must succeed
  // first (ADR-0040).
  function handleAddAttachmentFiles(files: FileList | File[]) {
    if (isReadOnlyEvent) return;
    for (const file of Array.from(files)) {
      if (attachmentEventId) {
        startAttachmentUpload(file, attachmentEventId);
      } else {
        setAttachmentDrafts((current) => [
          ...current,
          {
            draftId: crypto.randomUUID(),
            status: "pending",
            filename: file.name,
            sizeBytes: file.size,
            file,
          },
        ]);
      }
    }
  }

  function handleRetryAttachment(draft: AttachmentDraft) {
    if (draft.status !== "error" || !attachmentEventId) return;
    startAttachmentUpload(draft.file, attachmentEventId, draft.draftId);
  }

  // No confirmation dialog — inline remove, like ReminderRow (ADR-0040).
  function handleRemoveAttachment(draft: AttachmentDraft) {
    setAttachmentDrafts((current) => current.filter((d) => d.draftId !== draft.draftId));
    if (draft.status === "uploaded" && attachmentEventId && accessToken) {
      attachmentsApi
        .remove(accessToken, attachmentEventId, draft.attachment.id)
        .then(() => void fetchEvents())
        .catch(() => {
          toast.error("Failed to remove attachment.");
          setAttachmentDrafts((current) => [...current, draft]);
        });
    }
  }

  async function handleDownloadAttachment(attachment: Attachment) {
    if (!accessToken || !attachmentEventId) return;
    try {
      await downloadBlob(
        accessToken,
        attachmentsApi.downloadUrl(attachmentEventId, attachment.id),
        attachment.filename,
      );
    } catch {
      toast.error("Failed to download attachment.");
    }
  }

  // Dropping anywhere on the modal attaches the file rather than letting the
  // browser's default take over wherever it landed — including a text
  // field, which would otherwise paste a path (#132, ADR-0040). Handlers
  // live on the Popup itself so every child's drop bubbles here unless the
  // child claims its own (none in this file do).
  function handleModalDragOver(event: DragEvent<HTMLDivElement>) {
    if (isReadOnlyEvent) return;
    event.preventDefault();
    setIsDraggingOverAttachments(true);
  }

  function handleModalDragLeave() {
    setIsDraggingOverAttachments(false);
  }

  function handleModalDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setIsDraggingOverAttachments(false);
    handleAddAttachmentFiles(event.dataTransfer.files);
  }

  // All-day skips the time-range check entirely — there's no time inputs to
  // validate (ADR-0017).
  const isTimeRangeValid =
    allDay || timeStringToDate(day, endTime) > timeStringToDate(day, startTime);
  const canSave =
    !isReadOnlyEvent && title.trim() !== "" && calendarId !== "" && isTimeRangeValid;

  async function handleSave() {
    if (!canSave) return;
    // Once a create has already gone through and its Attachments are
    // uploading, this Event is done — Enter shouldn't try to create a
    // second one (the Save button is already hidden by then too).
    if (mode !== "edit" && createdEventId) return;

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
      description: description.trim(),
      location: location.trim(),
      color,
    };

    if (mode !== "edit") {
      // A newly created Event is stamped with the creator's Viewer zone; an
      // all-day Event stays timezone-free (ADR-0017, ADR-0019).
      const tzid = allDay ? undefined : viewerZone();
      const id = crypto.randomUUID();

      const pendingFiles = attachmentDrafts.filter(
        (draft): draft is Extract<AttachmentDraft, { status: "pending" }> =>
          draft.status === "pending",
      );
      if (pendingFiles.length === 0) {
        addEvent({ id, ...changes, tzid });
        onClose();
        return;
      }

      // The row must exist before an upload can reference it, so the
      // create POST is awaited here rather than fired-and-forgotten like
      // the branch above — cancelling the modal (closing without files
      // staged) still uploads nothing, since this whole branch is skipped
      // when there's nothing pending (#132, ADR-0040). The dialog stays
      // open afterwards so per-file progress/retry has somewhere to live;
      // its footer switches to a single Done button once createdEventId is
      // set.
      await addEvent({ id, ...changes, tzid });
      if (!useEventsStore.getState().events.some((event) => event.id === id)) {
        // addEvent already rolled back its optimistic add and toasted the
        // failure — the Event was never saved, so there's nothing to
        // upload against.
        onClose();
        return;
      }
      setCreatedEventId(id);
      for (const draft of pendingFiles) {
        startAttachmentUpload(draft.file, id, draft.draftId);
      }
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

    // The rule is unchanged — ask which Occurrences the edit applies to,
    // but only if something actually differs from the Occurrence's current
    // values. A no-op save (e.g. one where only an Attachment was
    // added/removed — that applies to the Master immediately and
    // independently of Save, ADR-0040) would otherwise misleadingly prompt
    // for a scope that controls nothing (#141).
    if (
      master &&
      !hasFieldChanges(changes, {
        calendarId: initial.calendarId,
        title: initial.title,
        start: props.occurrence.start,
        end: props.occurrence.end,
        allDay: initial.allDay,
        rrule: master.rrule,
        reminders: initial.reminders,
        description: initial.description,
        location: initial.location,
        color: initial.color,
      })
    ) {
      onClose();
      return;
    }
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

  async function downloadEvent(scope: "all" | "occurrence") {
    if (mode !== "edit" || !accessToken) return;
    try {
      await icsApi.downloadEvent(
        accessToken,
        props.occurrence.event.id,
        props.occurrence.event.title,
        scope === "occurrence"
          ? { type: "occurrence", occurrenceStart: props.occurrence.start }
          : { type: "all" },
      );
    } catch {
      toast.error("Failed to download event.");
    }
  }

  // The Export summary pre-flight (#134, ADR-0041): a flattened Occurrence
  // never carries an Attachment, so only scope "all" needs the check. When
  // nothing is oversized this stays a single click, same as before.
  async function runDownload(scope: "all" | "occurrence") {
    if (mode !== "edit" || !accessToken) return;
    if (scope === "occurrence") {
      await downloadEvent("occurrence");
      return;
    }
    try {
      const summary = await icsApi.eventOversizedAttachments(accessToken, props.occurrence.event.id);
      if (summary.count === 0) {
        await downloadEvent("all");
      } else {
        setPendingExportSummary(summary);
      }
    } catch {
      toast.error("Failed to check attachments before download.");
    }
  }

  function handleDownloadClick() {
    if (isRecurring) {
      setIsDownloadScopePickerOpen(true);
      return;
    }
    void runDownload("all");
  }

  function handleDownloadScopeConfirm(scope: EditScope) {
    setIsDownloadScopePickerOpen(false);
    void runDownload(scope === "all" ? "all" : "occurrence");
  }

  async function handleConfirmExport() {
    setIsConfirmingExport(true);
    try {
      await downloadEvent("all");
    } finally {
      setIsConfirmingExport(false);
      setPendingExportSummary(null);
    }
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
      void handleSave();
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
            onDragOver={handleModalDragOver}
            onDragLeave={handleModalDragLeave}
            onDrop={handleModalDrop}
            className="fixed top-1/2 left-1/2 z-50 max-h-[85vh] w-[28rem] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3"
          >
            <div className="flex items-center justify-between gap-2">
              <Dialog.Title className="text-heading font-medium text-ink">
                {mode === "edit" ? "Edit event" : "New event"}
              </Dialog.Title>
              {mode === "edit" && (
                <IconButton
                  size="tiny"
                  onClick={handleDownloadClick}
                  aria-label="Download event"
                >
                  <Download className="size-3.5" />
                </IconButton>
              )}
            </div>

            <form
              onSubmit={(event) => {
                event.preventDefault();
                void handleSave();
              }}
            >
            <Input
              label="Title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="Add title"
              disabled={isReadOnlyEvent}
              className="mt-4"
            />

            <Input
              label="Location"
              value={location}
              onChange={(event) => setLocation(event.target.value)}
              placeholder="Add location"
              disabled={isReadOnlyEvent}
              className="mt-4"
            />

            <Textarea
              label="Description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Add description"
              disabled={isReadOnlyEvent}
              className="mt-4"
            />

            <label className="mt-4 flex items-center gap-2 text-label-sm text-ink">
              <Checkbox
                checked={allDay}
                onCheckedChange={setAllDay}
                aria-label="All day"
                disabled={isReadOnlyEvent}
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
                    disabled={isReadOnlyEvent}
                    className="flex-1"
                  />
                  <Input
                    label="End"
                    type="time"
                    value={endTime}
                    onChange={(event) => setEndTime(event.target.value)}
                    invalid={!isTimeRangeValid}
                    disabled={isReadOnlyEvent}
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
              {isReadOnlyEvent && editedCalendar ? (
                <p className="text-label-sm text-ink-muted">
                  {readOnlyReason === "subscription"
                    ? `Calendar: ${editedCalendar.name} (subscribed) — read-only; only Refresh can update it.`
                    : `Calendar: ${editedCalendar.name} — read-only; you have Viewer access.`}
                </p>
              ) : mode === "edit" && !editedCalendar ? (
                // editedCalendar is undefined: this Event's Calendar carries
                // no Access row for the caller at all — an Attendee-only
                // Event (ADR-0046), never reachable in create mode, where
                // the picker only ever offers writableCalendars. calendarName
                // is the wire's own fallback for exactly this case.
                <p className="text-label-sm text-ink-muted">
                  {`Calendar: ${props.occurrence.event.calendarName ?? "Unknown"} — you can see this event because you're invited as an Attendee.`}
                </p>
              ) : calendarEmptyReason ? (
                <div>
                  <p className="text-label-sm text-ink-muted">
                    {CALENDAR_EMPTY_STATE_COPY[calendarEmptyReason]}
                  </p>
                  {calendarEmptyReason !== "hidden" && (
                    <button
                      type="button"
                      onClick={() => setIsCreateCalendarOpen(true)}
                      className="mt-1.5 text-label-sm text-accent hover:underline"
                    >
                      Create a calendar
                    </button>
                  )}
                </div>
              ) : (
                <Select
                  label="Calendar"
                  value={calendarId}
                  onValueChange={setCalendarId}
                  options={calendarOptions.map((calendar) => ({
                    value: calendar.id,
                    label: calendarPickerLabel(calendar),
                  }))}
                />
              )}
            </div>

            <div className="mt-4">
              <p className="mb-1.5 text-label-sm text-ink-muted">Color</p>
              <ColorSwatchPicker
                value={
                  color ??
                  resolveCalendarFill(
                    getCalendarById(calendars, calendarId) ?? attendeeOnlyCalendarColor,
                  )
                }
                onValueChange={setColor}
                disabled={isReadOnlyEvent}
              />
              {color !== undefined && !isReadOnlyEvent && (
                <button
                  type="button"
                  onClick={() => setColor(undefined)}
                  className="mt-1.5 text-label-sm text-accent hover:underline"
                >
                  Reset to Calendar color
                </button>
              )}
            </div>

            <div className="mt-4">
              <Select
                label="Repeat"
                value={repeat}
                onValueChange={handleRepeatChange}
                options={repeatOptions}
                disabled={isReadOnlyEvent}
              />
              {repeat === "custom" && !isReadOnlyEvent && (
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
              <p className="mb-1.5 text-label-sm text-ink-muted">
                {showReminderOverride ? "Event reminders (for everyone)" : "Reminders"}
              </p>
              <div className="flex flex-col gap-2">
                {reminders.map((reminder) => (
                  <ReminderRow
                    key={reminder.draftId}
                    reminder={reminder}
                    emailAvailable={emailAvailable}
                    onChange={(next) => handleReminderChange(reminder.draftId, next)}
                    onRemove={() => handleRemoveReminder(reminder.draftId)}
                    disabled={isReadOnlyEvent}
                  />
                ))}
              </div>
              {!isReadOnlyEvent && (
                <button
                  type="button"
                  onClick={handleAddReminder}
                  className="mt-1.5 text-label-sm text-accent hover:underline"
                >
                  Add reminder
                </button>
              )}
            </div>

            {showReminderOverride && (
              <ReminderOverrideControl
                eventId={props.occurrence.event.id}
                emailAvailable={emailAvailable}
              />
            )}

            {createdByName && (
              <p className="mt-4 text-label-sm text-ink-muted">
                Created by {createdByName}
              </p>
            )}

            <div
              className={`rounded-shell-md p-1.5 transition-colors ${
                isDraggingOverAttachments && !isReadOnlyEvent
                  ? "mt-4 bg-surface-hover outline-2 outline-dashed outline-accent"
                  : "mt-2.5 -mx-1.5 -mb-1.5"
              }`}
            >
              <p className="mb-1.5 text-label-sm text-ink-muted">Attachments</p>
              {attachmentDrafts.length > 0 && (
                <div className="flex flex-col gap-2">
                  {attachmentDrafts.map((draft) => (
                    <AttachmentRow
                      key={draft.draftId}
                      draft={draft}
                      showUploader={showAttachmentUploader}
                      onDownload={() =>
                        draft.status === "uploaded" && handleDownloadAttachment(draft.attachment)
                      }
                      onRemove={() => handleRemoveAttachment(draft)}
                      onRetry={() => handleRetryAttachment(draft)}
                      disabled={isReadOnlyEvent}
                    />
                  ))}
                </div>
              )}
              {!isReadOnlyEvent && (
                <>
                  <input
                    ref={attachmentInputRef}
                    type="file"
                    multiple
                    className="hidden"
                    onChange={(event) => {
                      if (event.target.files) handleAddAttachmentFiles(event.target.files);
                      event.target.value = "";
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => attachmentInputRef.current?.click()}
                    className="mt-1.5 text-label-sm text-accent hover:underline"
                  >
                    Add attachment
                  </button>
                </>
              )}
            </div>

            <EventAttendeesSection
              eventId={master?.id}
              canManage={mode === "edit" && !isReadOnlyEvent}
            />

            <div className="mt-5 flex items-center justify-between gap-2">
              {mode === "edit" && !isReadOnlyEvent ? (
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
                {mode !== "edit" && createdEventId ? (
                  <Dialog.Close className={buttonClasses({ size: "small" })}>Done</Dialog.Close>
                ) : isReadOnlyEvent ? (
                  <Dialog.Close
                    className={buttonClasses({
                      variant: "outline",
                      color: "secondary",
                      size: "small",
                    })}
                  >
                    Close
                  </Dialog.Close>
                ) : (
                  <>
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
                  </>
                )}
              </div>
            </div>
            </form>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      {isCreateCalendarOpen && (
        <CalendarModal mode="create" onClose={() => setIsCreateCalendarOpen(false)} />
      )}
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
      {mode === "edit" && isDownloadScopePickerOpen && (
        <ScopePicker
          action="Download"
          scopes={["this", "all"]}
          onConfirm={handleDownloadScopeConfirm}
          onClose={() => setIsDownloadScopePickerOpen(false)}
        />
      )}
      {pendingExportSummary && (
        <ExportSummaryDialog
          summary={pendingExportSummary}
          isSubmitting={isConfirmingExport}
          onConfirm={handleConfirmExport}
          onClose={() => setPendingExportSummary(null)}
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
