import { forwardRef, useId } from "react";
import type { InputHTMLAttributes, ReactNode } from "react";
import { fieldContainerClasses, fieldLabelClass } from "./fieldStyles";

// A React port of Immich's @immich/ui Input (Input.svelte): a label + optional
// description above a filled, ringed container that holds the control and
// optional leading/trailing icon slots.

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  description?: string;
  leadingIcon?: ReactNode;
  trailingIcon?: ReactNode;
  invalid?: boolean;
  /** Extra classes for the outer wrapper (e.g. spacing / flex sizing). */
  className?: string;
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
            "w-full flex-1 bg-transparent py-2.5 text-body text-ink outline-none placeholder:text-ink-muted disabled:cursor-not-allowed",
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
