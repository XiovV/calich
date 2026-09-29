import type { CalendarSet } from "../../lib/calendarSetsApi";
import { Checkbox } from "../ui/Checkbox";

interface AddToActiveSetCheckboxProps {
  activeCalendarSet: CalendarSet | null;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}

// The three creation dialogs' add-to-Set checkbox (#308, ADR-0082):
// unticked by default so a Set's membership only ever changes because the
// User asked, absent entirely on "All calendars" since there's nothing to
// opt into. Renders nothing itself when there's no Active Calendar Set, so
// callers don't need their own guard around it.
export function AddToActiveSetCheckbox({
  activeCalendarSet,
  checked,
  onCheckedChange,
}: AddToActiveSetCheckboxProps) {
  if (!activeCalendarSet) return null;

  return (
    <label className="mt-4 flex items-start gap-2 text-label-sm text-ink">
      <Checkbox
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-label={`Add to ${activeCalendarSet.name}`}
      />
      <span>Add to {activeCalendarSet.name}</span>
    </label>
  );
}
