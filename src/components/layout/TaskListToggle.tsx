import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check } from "lucide-react";
import { getContrastTextColor, toOpaqueHex } from "../../lib/calendarColors";

interface TaskListToggleProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  color: string;
  "aria-label": string;
}

// TaskListToggle is the Lists filter's own show/hide control (#317,
// ADR-0083) — CalendarToggle's counterpart for a Task List rather than a
// Calendar, kept as its own small component instead of reusing
// CalendarToggle directly: a Task List is deliberately not a Calendar
// (ADR-0083), and naming the control after the wrong domain concept would
// misdescribe what it toggles even though the two happen to render
// identically today.
export function TaskListToggle({
  checked,
  onCheckedChange,
  color,
  "aria-label": ariaLabel,
}: TaskListToggleProps) {
  const opaqueColor = toOpaqueHex(color);
  return (
    <BaseCheckbox.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      aria-label={ariaLabel}
      className="flex size-4 shrink-0 items-center justify-center rounded-[4px] border"
      style={{
        borderColor: opaqueColor,
        backgroundColor: checked ? opaqueColor : "transparent",
      }}
    >
      <BaseCheckbox.Indicator
        className="flex items-center justify-center"
        style={{ color: getContrastTextColor(opaqueColor) }}
      >
        <Check className="size-3" />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}
