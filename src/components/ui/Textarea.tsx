import { forwardRef, useId } from "react";
import type { TextareaHTMLAttributes } from "react";
import { fieldContainerClasses, fieldLabelClass } from "./fieldStyles";

// A multi-line counterpart to Input, for free-text fields that can run to a
// few lines (e.g. an Event's description).

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  invalid?: boolean;
  /** Extra classes for the outer wrapper (e.g. spacing / flex sizing). */
  className?: string;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { label, invalid, disabled, id, className, ...rest },
  ref,
) {
  const reactId = useId();
  const textareaId = id ?? reactId;

  return (
    <div className={["flex flex-col gap-1.5", className].filter(Boolean).join(" ")}>
      {label && (
        <label htmlFor={textareaId} className={fieldLabelClass}>
          {label}
        </label>
      )}
      <div className={fieldContainerClasses({ invalid, disabled })}>
        <textarea
          ref={ref}
          id={textareaId}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          rows={3}
          className="w-full flex-1 resize-none bg-transparent px-4 py-2.5 text-body text-ink outline-none placeholder:text-ink-muted disabled:cursor-not-allowed"
          {...rest}
        />
      </div>
    </div>
  );
});
