import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check } from "lucide-react";
import { getContrastTextColor, toOpaqueHex } from "../../lib/calendarColors";

interface CalendarToggleProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  color: string;
  "aria-label": string;
}

// CalendarToggle is the sidebar's own show/hide control (#188) — it replaced
// the always-coloured dot, so it carries the Calendar's colour itself rather
// than a theme accent: filled with a legible check when showing, an
// unfilled ring in that colour when hidden, so colour identity survives
// hiding a Calendar the way the dot used to guarantee. That makes it data,
// not a mode of the shared Checkbox (ui/Checkbox.tsx), which stays a
// neutral form input for its six other call sites.
export function CalendarToggle({
  checked,
  onCheckedChange,
  color,
  "aria-label": ariaLabel,
}: CalendarToggleProps) {
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
