import { forwardRef, useId } from "react";
import type { InputHTMLAttributes, ReactNode } from "react";
import { fieldContainerClasses, fieldLabelClass } from "./fieldStyles";

// A React port of Immich's @immich/ui Input (Input.svelte): a label + optional
// description above a filled, ringed container that holds the control and
// optional leading/trailing icon slots.

// `size` is omitted from the underlying attributes deliberately: the HTML
// one is a number (a text field's visible character width), which nothing
// here sets, and this component's own `size` is a height token. Redeclaring
// it without the Omit is what made this interface not actually extend
// InputHTMLAttributes.
export interface InputProps
  extends Omit<InputHTMLAttributes<HTMLInputElement>, "size"> {
  label?: string;
  description?: string;
  leadingIcon?: ReactNode;
  trailingIcon?: ReactNode;
  invalid?: boolean;
  /** Extra classes for the outer wrapper (e.g. spacing / flex sizing). */
  className?: string;
  /** "small" matches Select's/Button's pill height (py-1.5), for rows that
   * pair an Input with a Select (e.g. ReminderRow's offset amount). Defaults
   * to "medium" (py-2.5), the standalone-field height used everywhere else. */
  size?: "small" | "medium";
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  {
    label,
    description,
    leadingIcon,
    trailingIcon,
    invalid,
    disabled,
    id,
    className,
    size = "medium",
    ...rest
  },
  ref,
) {
  const reactId = useId();
  const inputId = id ?? reactId;
  const descriptionId = description ? `${inputId}-description` : undefined;

  return (
    <div className={["flex flex-col gap-1.5", className].filter(Boolean).join(" ")}>
      {label && (
        <label htmlFor={inputId} className={fieldLabelClass}>
          {label}
        </label>
      )}
      {description && (
        <p id={descriptionId} className="text-label-sm text-ink-muted">
          {description}
        </p>
      )}
      <div className={fieldContainerClasses({ invalid, disabled })}>
        {leadingIcon && (
          <span className="flex shrink-0 items-center pl-3 text-ink-muted">
            {leadingIcon}
          </span>
        )}
        <input
          ref={ref}
          id={inputId}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          aria-describedby={descriptionId}
          className={[
            "w-full flex-1 bg-transparent text-body text-ink outline-none placeholder:text-ink-muted disabled:cursor-not-allowed",
            size === "small" ? "py-1.5" : "py-2.5",
            leadingIcon ? "pl-2" : "pl-4",
            trailingIcon ? "pr-2" : "pr-4",
          ].join(" ")}
          {...rest}
        />
        {trailingIcon && (
          <span className="flex shrink-0 items-center pr-3 text-ink-muted">
            {trailingIcon}
          </span>
        )}
      </div>
    </div>
  );
});
