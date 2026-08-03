// Class-name builder for square, ghost icon buttons — the round `variant=ghost
// color=secondary` IconButtons Immich uses throughout its navigation bar. Kept
// in a plain module so it can be applied to base-ui Menu.Trigger elements and so
// IconButton.tsx only exports a component.

export type IconButtonSize = "tiny" | "small" | "medium";
export type IconButtonColor = "secondary" | "primary" | "danger";

const SIZE: Record<IconButtonSize, string> = {
  tiny: "size-7",
  small: "size-8",
  medium: "size-9",
};

const COLOR: Record<IconButtonColor, string> = {
  secondary: "text-ink-muted hover:bg-surface-hover hover:text-ink",
  primary: "text-primary hover:bg-primary-50",
  danger: "text-danger hover:bg-danger-50",
};

export interface IconButtonClassOptions {
  size?: IconButtonSize;
  color?: IconButtonColor;
  className?: string;
}

export function iconButtonClasses({
  size = "medium",
  color = "secondary",
  className,
}: IconButtonClassOptions = {}): string {
  return [
    "inline-flex shrink-0 items-center justify-center rounded-shell-pill outline-offset-2 transition-colors focus-visible:outline-2 disabled:cursor-not-allowed disabled:opacity-50",
    SIZE[size],
    COLOR[color],
    className,
  ]
    .filter(Boolean)
    .join(" ");
}
