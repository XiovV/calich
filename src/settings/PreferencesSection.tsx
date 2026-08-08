import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { Select } from "../components/ui/Select";

type WeekStartOption = "0" | "1" | "6";

const WEEK_START_OPTIONS: { value: WeekStartOption; label: string }[] = [
  { value: "0", label: "Sunday" },
  { value: "1", label: "Monday" },
  { value: "6", label: "Saturday" },
];

// The Settings page's Preferences section (#128, ADR-0039): per-User display
// settings. Week start is the only one wired up so far — Default view, Time
// format, and Working hours land in #129, #130, #131. Auto-saves on change,
// matching ReminderDeliverySection: no Save button, here or in the follow-ups.
export function PreferencesSection() {
  const weekStartsOn = useWeekStartsOn();
  const updateWeekStart = useAuthStore((state) => state.updateWeekStart);

  const { error, run } = useAsyncAction();

  async function handleChange(value: WeekStartOption) {
    await run(() => updateWeekStart(Number(value)));
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Preferences</h2>
      <p className="mt-1 text-body text-ink-muted">
        How the calendar is displayed to you. These settings follow you to every browser you log
        in from.
      </p>

      <div className="mt-4">
        <Select<WeekStartOption>
          label="Week start"
          value={String(weekStartsOn) as WeekStartOption}
          onValueChange={handleChange}
          options={WEEK_START_OPTIONS}
        />
      </div>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
    </section>
  );
}
