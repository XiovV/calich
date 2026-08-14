import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { canManageCalendar, isSubscribedCalendar, type Calendar } from "../../lib/calendar";
import { getNextUnusedColor } from "../../lib/calendarColors";
import { calendarsApi } from "../../lib/calendarsApi";
import { useAuthStore } from "../../lib/authStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { createCalendarCascade } from "../../lib/createCalendarCascade";
import type { Reminder } from "../../lib/event";
import { remindersEqual } from "../../lib/recurrenceScope";
import { toast } from "../../lib/toast";
import {
  ReminderListEditor,
  fromReminderDrafts,
  toReminderDrafts,
  type ReminderDraft,
} from "../../calendar-grid/ReminderListEditor";
import { CalendarSharingSection } from "./CalendarSharingSection";
import { ColorSwatchPicker } from "./ColorSwatchPicker";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";
import { Checkbox } from "../ui/Checkbox";
import { Input } from "../ui/Input";
import { fieldLabelClass } from "../ui/fieldStyles";

type CalendarModalProps =
  | { mode: "create"; onClose: () => void }
  | {
      mode: "edit";
      calendar: Calendar;
      onClose: () => void;
      // initialFocus: "sharing" scrolls straight to the sharing section on
      // open (#126) — the sidebar's share-count badge answers "who can see
      // my stuff", and clicking through to a dialog that opens scrolled
      // somewhere else wouldn't feel like an answer.
      initialFocus?: "sharing";
    };

export function CalendarModal(props: CalendarModalProps) {
  const { mode, onClose } = props;

  const calendars = useCalendarsStore((state) => state.calendars);
  const updateCalendar = useCalendarsStore((state) => state.updateCalendar);
  const setCalendarColor = useCalendarsStore((state) => state.setCalendarColor);
  const accessToken = useAuthStore((state) => state.accessToken);
  const emailAvailable = useAuthStore(
    (state) => state.user?.emailReminderChannelAvailable ?? false,
  );

  const [name, setName] = useState(mode === "edit" ? props.calendar.name : "");
  const [color, setColor] = useState(() =>
    mode === "edit"
      ? props.calendar.color
      : getNextUnusedColor(calendars.map((calendar) => calendar.color)),
  );
  const [keepAlarms, setKeepAlarms] = useState(
    mode === "edit" ? (props.calendar.keepAlarms ?? false) : false,
  );
  // Default reminders (ADR-0064): the caller's own timed/all-day lists on
  // this Calendar, beside their colour override — open to any User with
  // Access like Color, not gated on canManage the way Name/URL/KeepAlarms
  // are. Fetched once the dialog opens on an existing Calendar; a brand new
  // one (mode "create") has no id yet to hold them, so the section stays
  // hidden until the next time this Calendar is edited.
  const calendarId = mode === "edit" ? props.calendar.id : undefined;
  const [timedReminders, setTimedReminders] = useState<ReminderDraft[]>([]);
  const [allDayReminders, setAllDayReminders] = useState<ReminderDraft[]>([]);
  const [initialTimedReminders, setInitialTimedReminders] = useState<Reminder[]>([]);
  const [initialAllDayReminders, setInitialAllDayReminders] = useState<Reminder[]>([]);

  useEffect(() => {
    if (!calendarId || !accessToken) return;
    let cancelled = false;
    calendarsApi
      .getDefaultReminders(accessToken, calendarId)
      .then((defaults) => {
        if (cancelled) return;
        setInitialTimedReminders(defaults.timed);
        setInitialAllDayReminders(defaults.allDay);
        setTimedReminders(toReminderDrafts(defaults.timed));
        setAllDayReminders(toReminderDrafts(defaults.allDay));
      })
      .catch(() => {
        if (!cancelled) toast.error("Failed to load default reminders.");
      });
    return () => {
      cancelled = true;
    };
  }, [calendarId, accessToken]);

  const isSubscribed = mode === "edit" && isSubscribedCalendar(props.calendar);
  // canManage decides whose colour this dialog is editing (ADR-0038, #122):
  // an Owner's picker is the Calendar's colour and the default everyone
  // inherits; anyone else's is their own personal override alone (auto-
  // assigned on first view per ADR-0057). Name, Subscription URL, KeepAlarms
  // and sharing stay Owner-only, so a non-Owner sees only the colour picker.
  const canManage = mode === "create" || canManageCalendar(props.calendar);
  const showSharing = mode === "edit" && canManage;
  // initialUrl is the masked value the dialog opened with (#88, ADR-0032:
  // a password in an edited URL is masked here, same as everywhere else a
  // Subscription URL is shown) — captured once so Save can tell whether
  // the User actually touched the field.
  const initialUrl =
    mode === "edit" ? (props.calendar.sourceUrl ?? "") : "";
  const [url, setUrl] = useState(initialUrl);
  const sharingSectionRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (mode === "edit" && props.initialFocus === "sharing") {
      sharingSectionRef.current?.scrollIntoView({ block: "nearest" });
    }
    // Only ever run on mount — a re-render must not re-scroll the dialog
    // out from under someone editing it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const canSave =
    mode === "edit" && !canManage
      ? true
      : name.trim() !== "" && (!isSubscribed || url.trim() !== "");

  // saveDefaultReminders writes back whichever of the two Default reminders
  // lists actually changed (ADR-0064) — independent of canManage, since a
  // default is per-User state beside the Calendar rather than a write to
  // it, same posture as the colour override. A no-op before the initial
  // fetch resolves, since drafts start out equal to (empty) initials.
  function saveDefaultReminders() {
    if (!calendarId || !accessToken) return;

    const timedDraft = fromReminderDrafts(timedReminders);
    if (!remindersEqual(timedDraft, initialTimedReminders)) {
      calendarsApi
        .setDefaultReminders(accessToken, calendarId, false, timedDraft)
        .catch(() => toast.error("Failed to save default reminders."));
    }

    const allDayDraft = fromReminderDrafts(allDayReminders);
    if (!remindersEqual(allDayDraft, initialAllDayReminders)) {
      calendarsApi
        .setDefaultReminders(accessToken, calendarId, true, allDayDraft)
        .catch(() => toast.error("Failed to save default reminders."));
    }
  }

  function handleSave() {
    if (!canSave) return;
    saveDefaultReminders();

    if (mode === "edit" && !canManage) {
      setCalendarColor(props.calendar.id, color);
    } else if (mode === "edit") {
      updateCalendar(props.calendar.id, {
        name: name.trim(),
        color,
        ...(isSubscribed ? { keepAlarms } : {}),
        ...(isSubscribed && url.trim() !== initialUrl
          ? { url: url.trim() }
          : {}),
      });
    } else {
      const id = crypto.randomUUID();
      // isOwner: true — a calendar the viewer just created is theirs, but
      // canManageCalendar (#111) treats an unset isOwner as false, which
      // would optimistically misclassify it into "Shared with me" (#114)
      // for the brief window before the server's response overwrites it.
      // access: "owner" — canWriteCalendarEvents (#175) reads access, not
      // isOwner, so without it the New-event modal's Calendar picker treats
      // the brand-new Calendar as unwritable and hides it until reload.
      createCalendarCascade({ id, name: name.trim(), color, isOwner: true, access: "owner" });
    }
    onClose();
  }

  // Submit on Enter from anywhere in the dialog (base-ui may leave focus on the
  // popup rather than a field — e.g. when editing, where the name is pre-filled),
  // but not from buttons (Cancel/Save/color swatches).
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
          className={`fixed top-1/2 left-1/2 z-50 ${isSubscribed || showSharing ? "w-96" : "w-80"} -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3`}
        >
          <Dialog.Title className="text-heading font-medium text-ink">
            {mode === "edit" ? "Edit calendar" : "New calendar"}
          </Dialog.Title>
          {mode === "edit" && !canManage && props.calendar.ownerName && (
            <p className="mt-1 text-label-sm text-ink-muted">
              Shared by {props.calendar.ownerName}
            </p>
          )}

          <form
            onSubmit={(event) => {
              event.preventDefault();
              handleSave();
            }}
          >
          {isSubscribed && canManage && (
            <Input
              label="Subscription URL"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              placeholder="https://example.com/calendar.ics"
              className="mt-4"
            />
          )}

          {canManage && (
            <Input
              label="Name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Calendar name"
              className="mt-4"
            />
          )}

          <div className="mt-4">
            <p className={fieldLabelClass}>Color</p>
            <div className="mt-2">
              <ColorSwatchPicker value={color} onValueChange={setColor} />
            </div>
          </div>

          {calendarId && (
            <>
              {/* Default reminders (ADR-0064): fire on every Event on this
                Calendar where the caller has saved no Reminders of their
                own — two independent lists, since an all-day Reminder's
                offset counts back from 09:00 rather than midnight. Never
                copied onto an Event; resolved wherever Reminders are read. */}
              <div className="mt-4">
                <p className={fieldLabelClass}>Default reminders</p>
                <div className="mt-2">
                  <ReminderListEditor
                    reminders={timedReminders}
                    onChange={setTimedReminders}
                    emailAvailable={emailAvailable}
                    defaultOffsetMinutes={10}
                  />
                </div>
              </div>

              <div className="mt-4">
                <p className={fieldLabelClass}>Default all-day reminders</p>
                <div className="mt-2">
                  <ReminderListEditor
                    reminders={allDayReminders}
                    onChange={setAllDayReminders}
                    emailAvailable={emailAvailable}
                    defaultOffsetMinutes={1440}
                  />
                </div>
              </div>
            </>
          )}

          {isSubscribed && canManage && (
            <label className="mt-4 flex items-start gap-2 text-label-sm text-ink">
              <Checkbox
                checked={keepAlarms}
                onCheckedChange={setKeepAlarms}
                aria-label="Keep this feed's reminders"
              />
              <span>
                Keep this feed&apos;s reminders, including email reminders
                this instance will send
              </span>
            </label>
          )}

          {showSharing && (
            <div ref={sharingSectionRef}>
              <CalendarSharingSection calendar={props.calendar} />
            </div>
          )}

          <div className="mt-5 flex justify-end gap-2">
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
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
