// Class-name builder for the Immich-styled Button, kept in its own module so it
// can be applied to base-ui's Dialog.Close / AlertDialog.Close trigger elements
// (which render their own <button>) and so Button.tsx only exports a component.

export type ButtonVariant = "filled" | "outline" | "ghost";
export type ButtonColor =
  | "primary"
  | "secondary"
  | "danger"
  | "success"
  | "warning"
  | "info";
export type ButtonSize = "small" | "medium";
export type ButtonShape = "round" | "semi-round" | "rectangle";

const BASE =
  "inline-flex items-center justify-center gap-1.5 font-medium transition-colors outline-offset-2 focus-visible:outline-2 cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 aria-disabled:opacity-50 aria-disabled:cursor-not-allowed";

const SHAPE: Record<ButtonShape, string> = {
  round: "rounded-shell-pill",
  "semi-round": "rounded-shell-md",
  rectangle: "rounded-none",
};

const SIZE: Record<ButtonSize, string> = {
  small: "px-4 py-1.5 text-body",
  medium: "px-5 py-2 text-body",
};

const FOCUS_OUTLINE: Record<ButtonColor, string> = {
  primary: "outline-accent",
  secondary: "outline-ink",
  danger: "outline-danger",
  success: "outline-success",
  warning: "outline-warning",
  info: "outline-info",
};

const FILLED: Record<ButtonColor, string> = {
  primary: "bg-primary text-ink-inverse hover:bg-primary/80",
  secondary: "bg-ink text-ink-inverse hover:bg-ink/80",
  danger: "bg-danger text-ink-inverse hover:bg-danger/80",
  success: "bg-success text-ink-inverse hover:bg-success/80",
  warning: "bg-warning text-ink-inverse hover:bg-warning/80",
  info: "bg-info text-ink-inverse hover:bg-info/80",
};

const OUTLINE: Record<ButtonColor, string> = {
  primary: "border border-primary bg-primary/10 text-primary hover:bg-primary/20",
  secondary: "border border-border bg-surface text-ink hover:bg-surface-hover",
  danger: "border border-danger bg-danger/10 text-danger hover:bg-danger/20",
  success: "border border-success bg-success/10 text-success hover:bg-success/20",
  warning: "border border-warning bg-warning/10 text-warning hover:bg-warning/20",
  info: "border border-info bg-info/10 text-info hover:bg-info/20",
};

const GHOST: Record<ButtonColor, string> = {
  primary: "text-primary hover:bg-primary-50",
  secondary: "text-ink hover:bg-surface-hover",
  danger: "text-danger hover:bg-danger-50",
  success: "text-success hover:bg-success-50",
  warning: "text-warning hover:bg-warning-50",
  info: "text-info hover:bg-info-50",
};

const VARIANT: Record<ButtonVariant, Record<ButtonColor, string>> = {
  filled: FILLED,
  outline: OUTLINE,
  ghost: GHOST,
};

export interface ButtonClassOptions {
  variant?: ButtonVariant;
  color?: ButtonColor;
  size?: ButtonSize;
  shape?: ButtonShape;
  fullWidth?: boolean;
  className?: string;
}

export function buttonClasses({
  variant = "filled",
  color = "primary",
  size = "medium",
  shape = "round",
  fullWidth = false,
  className,
}: ButtonClassOptions = {}): string {
  return [
    BASE,
    SHAPE[shape],
    SIZE[size],
    FOCUS_OUTLINE[color],
    VARIANT[variant][color],
    fullWidth && "w-full",
    className,
  ]
    .filter(Boolean)
    .join(" ");
}
