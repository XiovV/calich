// Shared styling for form fields, ported from Immich's @immich/ui Input/Label
// (styles.ts `inputContainerStyles` / `inputStyles` and Label.svelte). Kept in a
// plain module so the component files only export components.

// Immich labels: font-medium, small text, foreground (not muted) colour.
export const fieldLabelClass = "text-body font-medium text-ink";

export interface FieldContainerOptions {
  invalid?: boolean;
  disabled?: boolean;
  className?: string;
}

// The filled container with a ring that turns accent on focus — Immich's inputs
// have no visible border, just `bg` + `ring-1` + `focus-within:ring-primary`.
export function fieldContainerClasses({
  invalid,
  disabled,
  className,
}: FieldContainerOptions = {}): string {
  return [
    "flex w-full items-center rounded-shell-md bg-surface-sunken ring-1 transition-colors",
    invalid
      ? "ring-danger focus-within:ring-2 focus-within:ring-danger"
      : "ring-border focus-within:ring-2 focus-within:ring-accent-ink",
    disabled && "opacity-60",
    className,
  ]
    .filter(Boolean)
    .join(" ");
}
