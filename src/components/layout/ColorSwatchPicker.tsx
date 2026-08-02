import {
  CALENDAR_COLORS,
  getCalendarColorClass,
  type CalendarColor,
} from "../../lib/calendarColors";

interface ColorSwatchPickerProps {
  value: CalendarColor;
  onValueChange: (color: CalendarColor) => void;
}

export function ColorSwatchPicker({ value, onValueChange }: ColorSwatchPickerProps) {
  return (
    <div role="radiogroup" aria-label="Color" className="flex flex-wrap gap-2">
      {CALENDAR_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          role="radio"
          aria-checked={color === value}
          aria-label={color}
          onClick={() => onValueChange(color)}
          className={`size-7 rounded-shell-pill ${getCalendarColorClass(color)} ${
            color === value
              ? "ring-2 ring-accent ring-offset-2 ring-offset-surface"
              : ""
          }`}
        />
      ))}
    </div>
  );
}
