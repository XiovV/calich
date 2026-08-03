import { forwardRef } from "react";
import type { ButtonHTMLAttributes } from "react";
import {
  iconButtonClasses,
  type IconButtonClassOptions,
} from "./iconButtonClasses";

// A React port of Immich's @immich/ui IconButton: a square, round, ghost button
// that holds a single icon (passed as children, which sizes itself).

export interface IconButtonProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "color">,
    Omit<IconButtonClassOptions, "className"> {}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(
  function IconButton(
    { size, color, className, children, type = "button", ...rest },
    ref,
  ) {
    return (
      <button
        ref={ref}
        type={type}
        className={iconButtonClasses({ size, color, className })}
        {...rest}
      >
        {children}
      </button>
    );
  },
);
