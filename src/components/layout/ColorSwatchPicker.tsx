import { useRef } from "react";
import { SWATCHES, normalizeCalendarColor, toOpaqueHex } from "../../lib/calendarColors";

interface ColorSwatchPickerProps {
  value: string;
  onValueChange: (color: string) => void;
  disabled?: boolean;
}

// Accessible labels only — a Swatch has no name in the domain model
// (ADR-0029 deliberately avoids reintroducing a color enum), but a screen
// reader still needs to distinguish eight otherwise-identical radio
// buttons. Paired with SWATCHES by index.
const SWATCH_LABELS = [
  "Tomato",
  "Flamingo",
  "Banana",
  "Sage",
  "Peacock",
  "Blueberry",
  "Grape",
  "Graphite",
];

// A rainbow-gradient hint on the custom tile while a Swatch is selected —
// clicking it opens the native color panel to pick something else. Derived
// from SWATCHES so it can't drift from the actual Swatch list.
const CUSTOM_TILE_GRADIENT = `linear-gradient(135deg, ${SWATCHES.map(toOpaqueHex).join(", ")})`;

export function ColorSwatchPicker({ value, onValueChange, disabled }: ColorSwatchPickerProps) {
  const nativeInputRef = useRef<HTMLInputElement>(null);
  // Selection is hex equality, not name equality — a client-set color that
  // matches no Swatch must still show as selected on the custom tile
  // instead of silently rendering nothing selected (ADR-0029).
  const matchesSwatch = SWATCHES.includes(value);

  function handleNativeColorChange(event: React.ChangeEvent<HTMLInputElement>) {
    const normalized = normalizeCalendarColor(event.target.value);
    if (normalized) onValueChange(normalized);
  }

  return (
    <div role="radiogroup" aria-label="Color" className="flex flex-wrap gap-2">
      {SWATCHES.map((swatch, index) => (
        <button
          key={swatch}
          type="button"
          role="radio"
          aria-checked={swatch === value}
          aria-label={SWATCH_LABELS[index]}
          onClick={() => onValueChange(swatch)}
          disabled={disabled}
          style={{ backgroundColor: toOpaqueHex(swatch) }}
          className={`size-7 cursor-pointer rounded-shell-pill disabled:cursor-not-allowed disabled:opacity-50 ${
            swatch === value
              ? "ring-2 ring-accent-ink ring-offset-2 ring-offset-surface"
              : ""
          }`}
        />
      ))}
      <button
        type="button"
        role="radio"
        aria-checked={!matchesSwatch}
        aria-label="Custom color"
        onClick={() => nativeInputRef.current?.click()}
        disabled={disabled}
        style={matchesSwatch ? { backgroundImage: CUSTOM_TILE_GRADIENT } : { backgroundColor: toOpaqueHex(value) }}
        className={`relative size-7 cursor-pointer overflow-hidden rounded-shell-pill disabled:cursor-not-allowed disabled:opacity-50 ${
          !matchesSwatch ? "ring-2 ring-accent-ink ring-offset-2 ring-offset-surface" : ""
        }`}
      >
        <input
          ref={nativeInputRef}
          type="color"
          value={toOpaqueHex(value)}
          onChange={handleNativeColorChange}
          aria-label="Custom color picker"
          tabIndex={-1}
          disabled={disabled}
          className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
        />
      </button>
    </div>
  );
}
