import { useEffect, useState } from "react";
import { useAuthStore } from "../lib/authStore";
import type { ActiveView } from "../lib/shellStore";
import type { TimeFormat } from "../lib/authApi";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Button } from "../components/ui/Button";

type WeekStartOption = "0" | "1" | "6";

const WEEK_START_OPTIONS: { value: WeekStartOption; label: string }[] = [
  { value: "0", label: "Sunday" },
  { value: "1", label: "Monday" },
  { value: "6", label: "Saturday" },
];

const DEFAULT_VIEW_OPTIONS: { value: ActiveView; label: string }[] = [
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
  { value: "year", label: "Year" },
];

const TIME_FORMAT_OPTIONS: { value: TimeFormat; label: string }[] = [
  { value: "24h", label: "24-hour" },
  { value: "12h", label: "12-hour" },
];

// <input type="time"> emits/expects "HH:mm" in the browser's own locale
// regardless of the app's Time format setting (see authApi's TimeFormat
// comment) — minutes-since-midnight is the wire/store unit (ADR-0039, #136).
function minutesToTimeString(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return `${String(hours).padStart(2, "0")}:${String(mins).padStart(2, "0")}`;
}

function timeStringToMinutes(time: string): number {
  const [hours, minutes] = time.split(":").map(Number);
  return hours * 60 + minutes;
}

// The Settings page's Preferences section (#128, #129, #130, #131, ADR-0039):
// per-User display settings. Auto-saves on change, matching
// ReminderDeliverySection: no Save button — except Working hours, which
// holds its pair in local state and only PATCHes once start < end, so the
// server never sees a temporarily-invalid range while the two dropdowns are
// mid-edit.
//
// Default view seeds Active view only at the next Session's bootstrap/login
// (authStore) — it deliberately does not change the Active view the caller
// is looking at right now.
export function PreferencesSection() {
  const weekStartsOn = useWeekStartsOn();
  const defaultView = useAuthStore((state) => state.user?.defaultView ?? "week");
  const timeFormat = useAuthStore((state) => state.user?.timeFormat ?? "24h");
  const workingHoursStart = useAuthStore((state) => state.user?.workingHoursStart ?? null);
  const workingHoursEnd = useAuthStore((state) => state.user?.workingHoursEnd ?? null);
  const updateWeekStart = useAuthStore((state) => state.updateWeekStart);
  const updateDefaultView = useAuthStore((state) => state.updateDefaultView);
  const updateTimeFormat = useAuthStore((state) => state.updateTimeFormat);
  const updateWorkingHours = useAuthStore((state) => state.updateWorkingHours);

  const { error, run } = useAsyncAction();

  // Local draft so an in-progress edit (e.g. picking an end time before the
  // start time) never reaches the server — only once the pair is valid does
  // it PATCH (ADR-0039). null renders as a blank time input rather than
  // guessing a starting range, so a User who has never opted in doesn't see
  // a preset that looks already-configured. Resyncs whenever the stored
  // Preference changes underneath it (initial load, or another device
  // setting it).
  const [draftStart, setDraftStart] = useState<number | null>(workingHoursStart);
  const [draftEnd, setDraftEnd] = useState<number | null>(workingHoursEnd);

  useEffect(() => {
    setDraftStart(workingHoursStart);
    setDraftEnd(workingHoursEnd);
  }, [workingHoursStart, workingHoursEnd]);

  async function handleWeekStartChange(value: WeekStartOption) {
    await run(() => updateWeekStart(Number(value)));
  }

  async function handleDefaultViewChange(value: ActiveView) {
    await run(() => updateDefaultView(value));
  }

  async function handleTimeFormatChange(value: TimeFormat) {
    await run(() => updateTimeFormat(value));
  }

  async function handleWorkingHoursStartChange(event: React.ChangeEvent<HTMLInputElement>) {
    if (!event.target.value) {
      setDraftStart(null);
      return;
    }
    const start = timeStringToMinutes(event.target.value);
    setDraftStart(start);
    if (draftEnd !== null && start < draftEnd) {
      await run(() => updateWorkingHours({ start, end: draftEnd }));
    }
  }

  async function handleWorkingHoursEndChange(event: React.ChangeEvent<HTMLInputElement>) {
    if (!event.target.value) {
      setDraftEnd(null);
      return;
    }
    const end = timeStringToMinutes(event.target.value);
    setDraftEnd(end);
    if (draftStart !== null && draftStart < end) {
      await run(() => updateWorkingHours({ start: draftStart, end }));
    }
  }

  async function handleClearWorkingHours() {
    setDraftStart(null);
    setDraftEnd(null);
    if (hasWorkingHours) {
      await run(() => updateWorkingHours(null));
    }
  }

  const hasWorkingHours = workingHoursStart !== null && workingHoursEnd !== null;
  const hasAnyDraftValue = draftStart !== null || draftEnd !== null;
  const workingHoursInvalid =
    draftStart !== null && draftEnd !== null && draftStart >= draftEnd;

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Preferences</h2>
      <p className="mt-1 text-body text-ink-muted">
        How the calendar is displayed to you. These settings follow you to every browser you log
        in from.
      </p>

      <div className="mt-4 space-y-4">
        <Select<WeekStartOption>
          label="Week start"
          value={String(weekStartsOn) as WeekStartOption}
          onValueChange={handleWeekStartChange}
          options={WEEK_START_OPTIONS}
        />

        <Select<ActiveView>
          label="Default view"
          value={defaultView}
          onValueChange={handleDefaultViewChange}
          options={DEFAULT_VIEW_OPTIONS}
        />

        <Select<TimeFormat>
          label="Time format"
          value={timeFormat}
          onValueChange={handleTimeFormatChange}
          options={TIME_FORMAT_OPTIONS}
        />

        <div>
          <p className="mb-1.5 text-body font-medium text-ink">Working hours</p>
          <p className="mb-2 text-label-sm text-ink-muted">
            Shades the hours outside this range in Day and Week view. Nothing is hidden — it's a
            visual hint only.
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex gap-3">
              <Input
                aria-label="Working hours start"
                type="time"
                value={draftStart === null ? "" : minutesToTimeString(draftStart)}
                onChange={handleWorkingHoursStartChange}
                invalid={workingHoursInvalid}
                className="flex-1"
              />
              <Input
                aria-label="Working hours end"
                type="time"
                value={draftEnd === null ? "" : minutesToTimeString(draftEnd)}
                onChange={handleWorkingHoursEndChange}
                invalid={workingHoursInvalid}
                className="flex-1"
              />
            </div>
            {hasAnyDraftValue && (
              <Button variant="ghost" color="secondary" size="small" onClick={handleClearWorkingHours}>
                Clear
              </Button>
            )}
          </div>
          {workingHoursInvalid && (
            <p className="mt-1 text-label-sm text-danger">End time must be after start time.</p>
          )}
        </div>
      </div>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
    </section>
  );
}
