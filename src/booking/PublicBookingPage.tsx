import { useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { useParams } from "react-router";
import { DayPicker } from "react-day-picker";
import "react-day-picker/style.css";
import { addDays, format, startOfMonth } from "date-fns";
import { toZonedTime } from "date-fns-tz";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";
import { ApiError } from "../lib/apiClient";
import {
  publicBookingApi,
  isNotFoundError,
  isSlotTakenError,
  type BookingConfirmation,
  type PublicBookingLink,
} from "../lib/publicBookingApi";
import { detectBrowserTimeZone, listTimeZones } from "../lib/timezones";
import { detectBrowserTimeFormat } from "../lib/timeFormat";
import type { TimeFormat } from "../lib/authApi";
import { daysWithSlots, formatSlotTime, slotsForDay, zonedDayKey } from "../lib/publicBookingSlots";

// The public Booking Link page (#324, #326, ADR-0084, ADR-0087): a stranger
// with no account and no Session opens /:handle/:slug (routed in App.tsx,
// outside ProtectedRoute) and sees the host's offer, a month grid of
// availability, and that day's slots in their own detected timezone.
// Picking a slot reveals the booking form (name + email, both required);
// confirming replaces the page with a confirmation screen showing the
// booked time in the visitor's own timezone. A slot taken out from under the
// visitor — raced by someone else, or gone stale — re-fetches slots and asks
// them to pick again (ADR-0087).

const TIME_ZONE_OPTIONS = listTimeZones().map((tz) => ({ value: tz, label: tz }));
const TIME_FORMAT_OPTIONS: { value: TimeFormat; label: string }[] = [
  { value: "12h", label: "12-hour" },
  { value: "24h", label: "24-hour" },
];

type LinkResult =
  | { status: "ready"; link: PublicBookingLink }
  | { status: "not-found" }
  | { status: "error" };

interface SlotsResult {
  year: number;
  month: number;
  slots: Date[];
}

export function PublicBookingPage() {
  const { handle = "", slug = "" } = useParams<{ handle: string; slug: string }>();

  // null means "still loading (handle, slug)" — derived rather than a
  // separate written-from-the-effect status flag, the same "no fetch
  // needed to know that" posture AcceptWorkspaceInvitePage takes toward its
  // own preview fetch (https://react.dev/learn/you-might-not-need-an-effect).
  const [linkResult, setLinkResult] = useState<LinkResult | null>(null);

  // Per-visit view state only (#324's AC): detected once on mount,
  // correctable on the page, and persisted nowhere — a reload starts fresh.
  const [viewerZone, setViewerZone] = useState(() => detectBrowserTimeZone());
  const [timeFmt, setTimeFmt] = useState<TimeFormat>(() => detectBrowserTimeFormat());

  const [visibleMonth, setVisibleMonth] = useState(() => startOfMonth(new Date()));
  const [selectedDay, setSelectedDay] = useState<Date | undefined>(undefined);
  const [selectedSlot, setSelectedSlot] = useState<Date | null>(null);

  // Tagged by the (year, month) it answers, so a still-in-flight request for
  // a newly-paged-to month is "loading" by comparison rather than by a
  // second written flag.
  const [slotsResult, setSlotsResult] = useState<SlotsResult | null>(null);
  // Bumped whenever a slot turns out to be stale (isSlotTakenError below) to
  // force the slots effect to re-fetch the same (year, month) it already
  // has — none of that effect's other deps change on their own here.
  const [slotsRefreshKey, setSlotsRefreshKey] = useState(0);

  // The booking form (#326): visible once a slot is selected, gone once
  // confirmation is set. bookingError is the form's own inline message —
  // distinct from linkResult's page-level error, since a failed booking
  // leaves the rest of the page (and the visitor's typed name/email) intact.
  const [visitorName, setVisitorName] = useState("");
  const [visitorEmail, setVisitorEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [bookingError, setBookingError] = useState<string | null>(null);
  const [confirmation, setConfirmation] = useState<BookingConfirmation | null>(null);

  useEffect(() => {
    let cancelled = false;
    publicBookingApi
      .get(handle, slug)
      .then((result) => {
        if (!cancelled) setLinkResult({ status: "ready", link: result });
      })
      .catch((error: unknown) => {
        if (!cancelled) setLinkResult(isNotFoundError(error) ? { status: "not-found" } : { status: "error" });
      });
    return () => {
      cancelled = true;
    };
  }, [handle, slug]);

  const year = visibleMonth.getFullYear();
  const month = visibleMonth.getMonth() + 1;

  useEffect(() => {
    if (!linkResult || linkResult.status !== "ready" || linkResult.link.paused) return;

    let cancelled = false;
    publicBookingApi
      .slots(handle, slug, year, month)
      .then((slots) => {
        if (!cancelled) setSlotsResult({ year, month, slots });
      })
      .catch(() => {
        if (!cancelled) setSlotsResult({ year, month, slots: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [linkResult, handle, slug, year, month, slotsRefreshKey]);

  const slotsLoading = !slotsResult || slotsResult.year !== year || slotsResult.month !== month;
  const slots = useMemo(
    () => (slotsLoading ? [] : slotsResult.slots),
    [slotsLoading, slotsResult],
  );

  // Zone conversion and day grouping live in publicBookingSlots.ts, not here
  // (#324's AC) — this component only ever reads their result.
  const availableDays = useMemo(() => daysWithSlots(slots, viewerZone), [slots, viewerZone]);
  const daySlots = useMemo(
    () => (selectedDay ? slotsForDay(slots, selectedDay, viewerZone) : []),
    [slots, selectedDay, viewerZone],
  );

  if (!linkResult) {
    return (
      <PageShell>
        <p className="text-body text-ink-muted">Loading…</p>
      </PageShell>
    );
  }

  if (linkResult.status === "not-found") {
    return (
      <PageShell>
        <h1 className="text-heading font-medium text-ink">This page doesn't exist.</h1>
      </PageShell>
    );
  }

  if (linkResult.status === "error") {
    return (
      <PageShell>
        <h1 className="text-heading font-medium text-ink">Something went wrong.</h1>
        <p className="mt-1 text-body text-ink-muted">Please try again in a moment.</p>
      </PageShell>
    );
  }

  const link = linkResult.link;

  async function handleBook(event: FormEvent) {
    event.preventDefault();
    if (!selectedSlot || submitting) return;

    setSubmitting(true);
    setBookingError(null);
    try {
      const result = await publicBookingApi.book(handle, slug, selectedSlot, visitorName, visitorEmail);
      setConfirmation(result);
    } catch (error) {
      if (isSlotTakenError(error)) {
        setBookingError("That time was just taken. Please pick another.");
        setSelectedSlot(null);
        setSlotsRefreshKey((key) => key + 1);
      } else if (error instanceof ApiError) {
        setBookingError(error.message);
      } else {
        setBookingError("Something went wrong. Please try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (confirmation) {
    const zonedStart = toZonedTime(confirmation.start, viewerZone);
    return (
      <PageShell>
        <h1 className="text-heading-lg font-medium text-ink">You're booked!</h1>
        <p className="mt-2 text-body text-ink">
          {link.title} with {link.hostName}
        </p>
        <p className="mt-1 text-body text-ink-muted">
          {format(zonedStart, "EEEE, MMMM d, yyyy")} · {formatSlotTime(confirmation.start, viewerZone, timeFmt)}
        </p>
        <p className="mt-4 text-label-sm text-ink-muted">
          A confirmation has been sent to {visitorEmail}.
        </p>
      </PageShell>
    );
  }

  // Month paging bounded by the booking horizon (#324's AC): the DayPicker
  // itself refuses navigation past either bound.
  const startMonth = startOfMonth(new Date());
  const endMonth = startOfMonth(addDays(new Date(), link.bookingHorizonDays));

  return (
    <PageShell>
      <h1 className="text-heading-lg font-medium text-ink">{link.title}</h1>
      <p className="mt-1 text-body text-ink-muted">
        {link.hostName} · {link.durationMinutes} min{link.location ? ` · ${link.location}` : ""}
      </p>
      {link.description && <p className="mt-3 text-body text-ink">{link.description}</p>}
      <p className="mt-1 text-label-sm text-ink-muted">Host timezone: {link.hostTimezone}</p>

      {link.paused ? (
        <p className="mt-6 text-body text-ink" role="status">
          This link isn't accepting bookings right now.
        </p>
      ) : (
        <div className="mt-6 flex flex-col gap-6 sm:flex-row">
          <div className="sm:w-[22rem]">
            <DayPicker
              mode="single"
              month={visibleMonth}
              onMonthChange={setVisibleMonth}
              startMonth={startMonth}
              endMonth={endMonth}
              weekStartsOn={1}
              selected={selectedDay}
              onSelect={(day) => {
                setSelectedDay(day);
                setSelectedSlot(null);
              }}
              disabled={(date) => !availableDays.has(zonedDayKey(date, viewerZone))}
              modifiers={{ available: (date) => availableDays.has(zonedDayKey(date, viewerZone)) }}
              modifiersClassNames={{ available: "rdp-day-available" }}
            />

            <div className="mt-4 flex flex-wrap gap-3">
              <Select label="Timezone" value={viewerZone} onValueChange={setViewerZone} options={TIME_ZONE_OPTIONS} />
              <Select label="Time format" value={timeFmt} onValueChange={setTimeFmt} options={TIME_FORMAT_OPTIONS} />
            </div>
          </div>

          <div className="flex-1">
            <h2 className="text-label-sm font-medium text-ink">
              {selectedDay ? format(selectedDay, "EEEE, MMMM d") : "Select a day"}
            </h2>

            {!selectedDay ? (
              <p className="mt-2 text-label-sm text-ink-muted">Pick an available day to see its times.</p>
            ) : slotsLoading ? (
              <p className="mt-2 text-label-sm text-ink-muted">Loading times…</p>
            ) : daySlots.length === 0 ? (
              <p className="mt-2 text-label-sm text-ink-muted">No times available this day.</p>
            ) : (
              <ul className="mt-2 flex flex-wrap gap-2">
                {daySlots.map((slot) => (
                  <li key={slot.toISOString()}>
                    <Button
                      variant={selectedSlot?.getTime() === slot.getTime() ? "filled" : "outline"}
                      size="small"
                      aria-pressed={selectedSlot?.getTime() === slot.getTime()}
                      onClick={() => {
                        setSelectedSlot(slot);
                        setBookingError(null);
                      }}
                    >
                      {formatSlotTime(slot, viewerZone, timeFmt)}
                    </Button>
                  </li>
                ))}
              </ul>
            )}

            {/* Rendered outside the form itself (below) so it survives a
                slot-taken refusal clearing selectedSlot and unmounting the
                form — the visitor still needs to see why their pick just
                vanished. */}
            {bookingError && (
              <p className="mt-4 text-label-sm text-danger" role="alert">
                {bookingError}
              </p>
            )}

            {selectedSlot && (
              <form className="mt-4 flex max-w-sm flex-col gap-3" onSubmit={handleBook}>
                <h2 className="text-label-sm font-medium text-ink">
                  Booking {formatSlotTime(selectedSlot, viewerZone, timeFmt)} on {format(toZonedTime(selectedSlot, viewerZone), "EEEE, MMMM d")}
                </h2>
                <Input
                  label="Name"
                  value={visitorName}
                  onChange={(e) => setVisitorName(e.target.value)}
                  required
                  disabled={submitting}
                />
                <Input
                  label="Email"
                  type="email"
                  value={visitorEmail}
                  onChange={(e) => setVisitorEmail(e.target.value)}
                  required
                  disabled={submitting}
                />
                <Button type="submit" variant="filled" disabled={submitting}>
                  {submitting ? "Booking…" : "Confirm booking"}
                </Button>
              </form>
            )}
          </div>
        </div>
      )}
    </PageShell>
  );
}

function PageShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-surface px-4 py-10 sm:px-8">
      <div className="mx-auto max-w-3xl">{children}</div>
    </div>
  );
}
