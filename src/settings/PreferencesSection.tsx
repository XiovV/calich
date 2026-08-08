import { useEffect, useState } from "react";
import { format } from "date-fns";
import { useAuthStore } from "../lib/authStore";
import type { ActiveView } from "../lib/shellStore";
import type { TimeFormat } from "../lib/authApi";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { useTimePattern } from "../hooks/useTimePattern";
import { Select } from "../components/ui/Select";
import { Button } from "../components/ui/Button";

type WeekStartOption = "0" | "1" | "6";
type HourOption = string;

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

function hourOptions(timePattern: string): { value: HourOption; label: string }[] {
  return Array.from({ length: 24 }, (_, hour) => ({
    value: String(hour),
    label: format(new Date(2000, 0, 1, hour), timePattern),
  }));
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
  const timePattern = useTimePattern();

  const { error, run } = useAsyncAction();

  // Local draft so an in-progress edit (e.g. dragging the end hour below the
  // start hour) never reaches the server — only once the pair is valid does
  // it PATCH (ADR-0039). null renders as a blank dropdown rather than
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

  async function handleWorkingHoursStartChange(value: HourOption) {
    const start = Number(value);
    setDraftStart(start);
    if (draftEnd !== null && start < draftEnd) {
      await run(() => updateWorkingHours({ start, end: draftEnd }));
    }
  }

  async function handleWorkingHoursEndChange(value: HourOption) {
    const end = Number(value);
    setDraftEnd(end);
    if (draftStart !== null && draftStart < end) {
      await run(() => updateWorkingHours({ start: draftStart, end }));
    }
  }

  async function handleClearWorkingHours() {
    await run(() => updateWorkingHours(null));
  }

  const hasWorkingHours = workingHoursStart !== null && workingHoursEnd !== null;
  const HOUR_OPTIONS = hourOptions(timePattern);

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
          <div className="flex flex-wrap items-center gap-2">
            <Select<HourOption>
              aria-label="Working hours start"
              value={draftStart === null ? "" : String(draftStart)}
              onValueChange={handleWorkingHoursStartChange}
              options={HOUR_OPTIONS}
              className="min-w-0 flex-1 basis-32"
            />
            <span className="text-body text-ink-muted">to</span>
            <Select<HourOption>
              aria-label="Working hours end"
              value={draftEnd === null ? "" : String(draftEnd)}
              onValueChange={handleWorkingHoursEndChange}
              options={HOUR_OPTIONS}
              className="min-w-0 flex-1 basis-32"
            />
            {hasWorkingHours && (
              <Button variant="ghost" color="secondary" size="small" onClick={handleClearWorkingHours}>
                Clear
              </Button>
            )}
          </div>
        </div>
      </div>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
    </section>
  );
}
