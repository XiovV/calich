import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";
import { Checkbox } from "../ui/Checkbox";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { Textarea } from "../ui/Textarea";
import { useAsyncAction } from "../../hooks/useAsyncAction";
import { useAuthStore } from "../../lib/authStore";
import { authApi } from "../../lib/authApi";
import { useAvailabilitySchedulesStore } from "../../lib/availabilitySchedulesStore";
import { useBookingLinksStore } from "../../lib/bookingLinksStore";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { canWriteCalendarEvents } from "../../lib/calendar";
import { detectBrowserTimeZone } from "../../lib/timezones";
import type { BookingLink, BookingLinkVisibility, BookingLinkWrite } from "../../lib/bookingLinksApi";

const DEFAULT_MINIMUM_NOTICE_MINUTES = 240;
const DEFAULT_BOOKING_HORIZON_DAYS = 60;

const DURATION_PRESETS = ["15", "30", "45", "60"] as const;
type DurationChoice = (typeof DURATION_PRESETS)[number] | "custom";

const DURATION_OPTIONS: { value: DurationChoice; label: string }[] = [
  { value: "15", label: "15 minutes" },
  { value: "30", label: "30 minutes" },
  { value: "45", label: "45 minutes" },
  { value: "60", label: "60 minutes" },
  { value: "custom", label: "Custom" },
];

const VISIBILITY_OPTIONS: { value: BookingLinkVisibility; label: string }[] = [
  { value: "public", label: "Public — listed and bookable" },
  { value: "private", label: "Private — unlisted, bookable by URL" },
  { value: "paused", label: "Paused — unlisted, refusing bookings" },
];

function durationChoiceFor(minutes: number): DurationChoice {
  return (DURATION_PRESETS as readonly string[]).includes(String(minutes)) ? (String(minutes) as DurationChoice) : "custom";
}

// slugify turns free text into a Slug candidate — lowercase, hyphens in
// place of anything else, no leading/trailing/doubled hyphen — mirroring
// the server's own slugPattern (booking_link_validation.go) closely enough
// that what this produces almost always validates as-is.
function slugify(text: string): string {
  return text
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

type BookingLinkModalProps =
  | { mode: "create"; onClose: () => void }
  | { mode: "edit"; bookingLink: BookingLink; onClose: () => void };

// The Booking Link create/edit modal (#322, ADR-0084, ADR-0087): Title,
// Duration, Slug, Availability Schedule and Book-into Calendar on the face;
// Visibility, Location, Description, minimum notice, booking horizon and
// the Tasks half of the Conflict set behind More options (ADR-0056). A
// link saved without opening More options gets ADR-0087's own stated
// defaults. The Conflict set's Calendar half is edited with immediate
// effect from the edit modal only — there is no Booking Link id yet for a
// create-time toggle to attach to, so a create seeds it automatically
// (every owned Calendar plus Book-into) and it becomes editable the moment
// the link exists.
export function BookingLinkModal(props: BookingLinkModalProps) {
  const { mode, onClose } = props;
  const bookingLink = mode === "edit" ? props.bookingLink : undefined;
  const isEditing = mode === "edit";

  const createBookingLink = useBookingLinksStore((state) => state.createBookingLink);
  const updateBookingLink = useBookingLinksStore((state) => state.updateBookingLink);
  const addConflictCalendar = useBookingLinksStore((state) => state.addConflictCalendar);
  const removeConflictCalendar = useBookingLinksStore((state) => state.removeConflictCalendar);

  const schedules = useAvailabilitySchedulesStore((state) => state.schedules);
  const fetchSchedules = useAvailabilitySchedulesStore((state) => state.fetchSchedules);
  // The Schedule picker needs at least the lazily-seeded "Default" Schedule
  // (#320, ADR-0085) — fetched here rather than assumed already loaded,
  // since this modal is reachable straight from the sidebar without ever
  // visiting Settings first.
  useEffect(() => {
    fetchSchedules(detectBrowserTimeZone()).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const calendars = useCalendarsStore((state) => state.calendars);
  const user = useAuthStore((state) => state.user);
  const accessToken = useAuthStore((state) => state.accessToken);

  const [title, setTitle] = useState(bookingLink?.title ?? "");
  const [slug, setSlug] = useState(bookingLink?.slug ?? "");
  // Once the caller edits the Slug directly, Title no longer overwrites it
  // — the same touched-tracking every prefill-from-another-field pattern in
  // this app needs.
  const [slugTouched, setSlugTouched] = useState(isEditing);
  const [durationChoice, setDurationChoice] = useState<DurationChoice>(
    durationChoiceFor(bookingLink?.durationMinutes ?? 30),
  );
  const [customDuration, setCustomDuration] = useState(
    durationChoiceFor(bookingLink?.durationMinutes ?? 30) === "custom" ? String(bookingLink?.durationMinutes) : "",
  );
  const [scheduleId, setScheduleId] = useState<number | null>(bookingLink?.availabilityScheduleId ?? null);
  const [bookIntoCalendarId, setBookIntoCalendarId] = useState<string | null>(bookingLink?.bookIntoCalendarId ?? null);

  const [visibility, setVisibility] = useState<BookingLinkVisibility>(bookingLink?.visibility ?? "public");
  const [location, setLocation] = useState(bookingLink?.location ?? "");
  const [description, setDescription] = useState(bookingLink?.description ?? "");
  const [minimumNoticeMinutes, setMinimumNoticeMinutes] = useState(
    bookingLink?.minimumNoticeMinutes ?? DEFAULT_MINIMUM_NOTICE_MINUTES,
  );
  const [bookingHorizonDays, setBookingHorizonDays] = useState(
    bookingLink?.bookingHorizonDays ?? DEFAULT_BOOKING_HORIZON_DAYS,
  );
  const [tasksInConflictSet, setTasksInConflictSet] = useState(bookingLink?.tasksInConflictSet ?? true);

  // ADR-0056's own rule: auto-expand only if editing something that already
  // has a secondary field populated; create always starts collapsed.
  const [isExpanded, setIsExpanded] = useState(() =>
    bookingLink
      ? bookingLink.visibility !== "public" ||
        bookingLink.location !== "" ||
        bookingLink.description !== "" ||
        bookingLink.minimumNoticeMinutes !== DEFAULT_MINIMUM_NOTICE_MINUTES ||
        bookingLink.bookingHorizonDays !== DEFAULT_BOOKING_HORIZON_DAYS ||
        !bookingLink.tasksInConflictSet
      : false,
  );

  // The URL preview needs a Handle even before one is claimed — fetched
  // once, the same suggestion the create-first-link flow would go on to
  // claim automatically (#321, ADR-0084).
  const [handleSuggestion, setHandleSuggestion] = useState<string | null>(null);
  useEffect(() => {
    if (user?.handle || !accessToken) return;
    authApi
      .getHandleSuggestion(accessToken)
      .then(setHandleSuggestion)
      .catch(() => setHandleSuggestion(""));
    // Runs once at mount; user?.handle and accessToken don't change over
    // this modal's lifetime in a way that should refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { isSubmitting, error, run } = useAsyncAction();

  const durationMinutes = durationChoice === "custom" ? Number(customDuration) : Number(durationChoice);
  const canSubmit =
    title.trim() !== "" &&
    slug.trim() !== "" &&
    Number.isFinite(durationMinutes) &&
    durationMinutes > 0 &&
    scheduleId !== null &&
    bookIntoCalendarId !== null;

  const writableCalendars = calendars.filter((c) => canWriteCalendarEvents(c));
  const handle = user?.handle ?? handleSuggestion ?? "";
  const previewUrl = `${window.location.origin}/${handle || "…"}/${slug || "…"}`;

  // A fresh create defaults Schedule and Book-into to the first option the
  // moment each list has one, rather than leaving both Selects on an empty
  // placeholder a User must resolve by hand before they can even glance at
  // the rest of the face (#322: "a link created without opening More
  // options is complete and usable" implies the face itself should already
  // read as a usable default, not just what's behind More options).
  if (!isEditing && scheduleId === null && schedules.length > 0) {
    setScheduleId(schedules[0].id);
  }
  if (!isEditing && bookIntoCalendarId === null && writableCalendars.length > 0) {
    setBookIntoCalendarId(writableCalendars[0].id);
  }

  function handleTitleChange(value: string) {
    setTitle(value);
    if (!slugTouched) setSlug(slugify(value));
  }

  function handleSlugChange(value: string) {
    setSlug(value);
    setSlugTouched(true);
  }

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit || scheduleId === null || bookIntoCalendarId === null) return;

    // Renaming (as opposed to a first publish) breaks every URL published
    // under the old Slug, with nothing forwarding (ADR-0084) — only worth
    // interrupting for when there's an old Slug to lose.
    if (isEditing && bookingLink && slug !== bookingLink.slug) {
      const proceed = window.confirm(
        `Changing the slug breaks the URL published under "${bookingLink.slug}" — nothing forwards, and this cannot be undone.`,
      );
      if (!proceed) return;
    }

    const write: BookingLinkWrite = {
      title: title.trim(),
      slug: slug.trim(),
      durationMinutes,
      visibility,
      availabilityScheduleId: scheduleId,
      bookIntoCalendarId,
      location,
      description,
      minimumNoticeMinutes,
      bookingHorizonDays,
      tasksInConflictSet,
    };

    await run(async () => {
      if (isEditing && bookingLink) {
        await updateBookingLink(bookingLink.id, write);
      } else {
        await createBookingLink(write);
      }
      onClose();
    });
  }

  // Immediate effect, no save step, matching CalendarSetMembershipDialog —
  // only reachable once the link exists (edit mode), since create has no id
  // yet to attach a Conflict set toggle to.
  async function handleToggleConflictCalendar(calendarId: string, isMember: boolean) {
    if (!bookingLink) return;
    if (isMember) {
      await removeConflictCalendar(bookingLink.id, calendarId).catch(() => {});
    } else {
      await addConflictCalendar(bookingLink.id, calendarId).catch(() => {});
    }
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 max-h-[85vh] w-[32rem] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            {isEditing ? `Edit ${bookingLink?.title}` : "New booking link"}
          </Dialog.Title>

          <form onSubmit={handleSubmit} className="mt-4 flex flex-col gap-4">
            <Input label="Title" value={title} onChange={(e) => handleTitleChange(e.target.value)} />

            <Select<DurationChoice>
              label="Duration"
              value={durationChoice}
              onValueChange={setDurationChoice}
              options={DURATION_OPTIONS}
            />
            {durationChoice === "custom" && (
              <Input
                label="Custom duration (minutes)"
                type="number"
                min={1}
                value={customDuration}
                onChange={(e) => setCustomDuration(e.target.value)}
              />
            )}

            <Input label="Slug" value={slug} onChange={(e) => handleSlugChange(e.target.value)} />
            <p className="-mt-2 text-label-sm text-ink-muted">{previewUrl}</p>

            <Select<string>
              label="Availability schedule"
              value={scheduleId !== null ? String(scheduleId) : ""}
              onValueChange={(value) => setScheduleId(Number(value))}
              options={schedules.map((s) => ({ value: String(s.id), label: s.name }))}
              placeholder="Choose a schedule"
            />

            <Select<string>
              label="Book into"
              value={bookIntoCalendarId ?? ""}
              onValueChange={setBookIntoCalendarId}
              options={writableCalendars.map((c) => ({ value: c.id, label: c.name }))}
              placeholder="Choose a calendar"
            />

            <Button
              type="button"
              variant="ghost"
              color="secondary"
              size="small"
              className="self-start"
              onClick={() => setIsExpanded(true)}
              disabled={isExpanded}
            >
              More options
            </Button>

            {isExpanded && (
              <div className="flex flex-col gap-4 border-t border-border pt-4">
                <Select<BookingLinkVisibility>
                  label="Visibility"
                  value={visibility}
                  onValueChange={setVisibility}
                  options={VISIBILITY_OPTIONS}
                />

                <Input label="Location" value={location} onChange={(e) => setLocation(e.target.value)} />
                <Textarea
                  label="Description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                />

                <div className="flex gap-3">
                  <Input
                    label="Minimum notice (minutes)"
                    type="number"
                    min={0}
                    value={minimumNoticeMinutes}
                    onChange={(e) => setMinimumNoticeMinutes(Number(e.target.value))}
                    className="flex-1"
                  />
                  <Input
                    label="Booking horizon (days)"
                    type="number"
                    min={1}
                    value={bookingHorizonDays}
                    onChange={(e) => setBookingHorizonDays(Number(e.target.value))}
                    className="flex-1"
                  />
                </div>

                <label className="flex items-center gap-2">
                  <Checkbox
                    checked={tasksInConflictSet}
                    onCheckedChange={setTasksInConflictSet}
                    aria-label="Include my Tasks in the conflict set"
                  />
                  <span className="text-body text-ink">
                    An incomplete Task's Time block also closes a slot
                  </span>
                </label>

                <div>
                  <p className="mb-1.5 text-label-sm font-medium text-ink">Conflict set</p>
                  {isEditing && bookingLink ? (
                    <ul className="flex flex-col gap-1.5">
                      {calendars.map((c) => {
                        const isBookInto = c.id === bookingLink.bookIntoCalendarId;
                        const isMember = isBookInto || bookingLink.conflictCalendarIds.includes(c.id);
                        return (
                          <li key={c.id} className="flex items-center gap-2">
                            <Checkbox
                              checked={isMember}
                              disabled={isBookInto}
                              onCheckedChange={() => handleToggleConflictCalendar(c.id, isMember)}
                              aria-label={
                                isMember ? `Remove ${c.name} from the conflict set` : `Add ${c.name} to the conflict set`
                              }
                            />
                            <span className="text-body text-ink">
                              {c.name}
                              {isBookInto && <span className="text-ink-muted"> (book-into, always included)</span>}
                            </span>
                          </li>
                        );
                      })}
                    </ul>
                  ) : (
                    <p className="text-label-sm text-ink-muted">
                      Defaults to every calendar you own in this workspace, plus whichever one you book into. You
                      can customize it once the link is created.
                    </p>
                  )}
                </div>
              </div>
            )}

            {error && (
              <p className="text-label-sm text-danger" role="alert">
                {error}
              </p>
            )}

            <div className="mt-1 flex justify-end gap-2">
              <Dialog.Close
                className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
                disabled={isSubmitting}
              >
                Cancel
              </Dialog.Close>
              <Button type="submit" size="small" loading={isSubmitting} disabled={!canSubmit}>
                Save
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
