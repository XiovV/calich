import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Loader2 } from "lucide-react";
import { buttonClasses, type ButtonClassOptions } from "./buttonClasses";

// A React port of Immich's @immich/ui Button (internal/Button.svelte), expressed
// in this app's semantic tokens. The class logic lives in ./buttonClasses so it
// can also be reused on base-ui trigger elements (e.g. Dialog.Close).

export interface ButtonProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "color">,
    Omit<ButtonClassOptions, "className"> {
  loading?: boolean;
  leadingIcon?: ReactNode;
  trailingIcon?: ReactNode;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant,
    color,
    size,
    shape,
    fullWidth,
    loading = false,
    leadingIcon,
    trailingIcon,
    className,
    children,
    disabled,
    type = "button",
    ...rest
  },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      disabled={disabled || loading}
      className={buttonClasses({ variant, color, size, shape, fullWidth, className })}
      {...rest}
    >
      {loading ? <Loader2 className="size-4 animate-spin" /> : leadingIcon}
      {children}
      {trailingIcon}
    </button>
  );
});
