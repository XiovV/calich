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
    {
      size,
      color,
      className,
      children,
      type = "button",
      title,
      "aria-label": ariaLabel,
      ...rest
    },
    ref,
  ) {
    return (
      <button
        ref={ref}
        type={type}
        aria-label={ariaLabel}
        // An icon alone rarely says what the button does, so the aria-label
        // doubles as the hover tooltip unless the caller supplies its own
        // title. Screen readers still take the name from aria-label — title
        // is ignored for the accessible name once aria-label is present.
        title={title ?? ariaLabel}
        className={iconButtonClasses({ size, color, className })}
        {...rest}
      >
        {children}
      </button>
    );
  },
);
