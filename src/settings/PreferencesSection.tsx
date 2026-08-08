import { useAuthStore } from "../lib/authStore";
import type { ActiveView } from "../lib/shellStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { Select } from "../components/ui/Select";

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

// The Settings page's Preferences section (#128, #129, ADR-0039): per-User
// display settings. Week start and Default view are wired up so far — Time
// format and Working hours land in #130, #131. Auto-saves on change, matching
// ReminderDeliverySection: no Save button, here or in the follow-ups.
//
// Default view seeds Active view only at the next Session's bootstrap/login
// (authStore) — it deliberately does not change the Active view the caller
// is looking at right now.
export function PreferencesSection() {
  const weekStartsOn = useWeekStartsOn();
  const defaultView = useAuthStore((state) => state.user?.defaultView ?? "week");
  const updateWeekStart = useAuthStore((state) => state.updateWeekStart);
  const updateDefaultView = useAuthStore((state) => state.updateDefaultView);

  const { error, run } = useAsyncAction();

  async function handleWeekStartChange(value: WeekStartOption) {
    await run(() => updateWeekStart(Number(value)));
  }

  async function handleDefaultViewChange(value: ActiveView) {
    await run(() => updateDefaultView(value));
  }

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
      </div>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
    </section>
  );
}
