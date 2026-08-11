import { useId } from "react";
import { Select as BaseSelect } from "@base-ui/react/select";
import { Check, ChevronDown } from "lucide-react";
import { fieldLabelClass } from "./fieldStyles";

interface SelectOption<T extends string> {
  value: T;
  label: string;
}

interface SelectProps<T extends string> {
  value: T;
  onValueChange: (value: T) => void;
  options: SelectOption<T>[];
  label?: string;
  "aria-label"?: string;
  disabled?: boolean;
  // Merged onto the trigger; pass `min-w-0` + a flex-basis utility to let a
  // row of Selects shrink and truncate instead of wrapping onto a new line.
  className?: string;
}

// Styled after Immich's @immich/ui Select: a pill trigger with a ring that
// lights up on open, a chevron, and a popup that picks up an indigo tint in
// dark mode (dark:bg-primary-100) with a check on the selected item.
export function Select<T extends string>({
  value,
  onValueChange,
  options,
  label,
  "aria-label": ariaLabel,
  disabled,
  className,
}: SelectProps<T>) {
  const labelId = useId();
  return (
    <BaseSelect.Root
      value={value}
      onValueChange={(next) => onValueChange(next as T)}
      disabled={disabled}
    >
      {label && (
        <label id={labelId} className={`mb-1.5 block ${fieldLabelClass}`}>
          {label}
        </label>
      )}
      <BaseSelect.Trigger
        aria-label={ariaLabel}
        aria-labelledby={label ? labelId : undefined}
        className={`flex cursor-pointer items-center gap-1.5 rounded-shell-pill bg-surface-sunken px-4 py-1.5 text-body text-ink ring-1 ring-border transition-colors outline-none hover:bg-surface-hover data-[popup-open]:ring-2 data-[popup-open]:ring-accent data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60 data-[disabled]:hover:bg-surface-sunken ${className ?? ""}`}
      >
        <BaseSelect.Value className="min-w-0 truncate">
          {(selected: T) => options.find((option) => option.value === selected)?.label}
        </BaseSelect.Value>
        <BaseSelect.Icon>
          <ChevronDown className="size-4 text-ink-muted" />
        </BaseSelect.Icon>
      </BaseSelect.Trigger>
      <BaseSelect.Portal>
        <BaseSelect.Positioner sideOffset={4} className="z-[60]">
          <BaseSelect.Popup className="min-w-[--anchor-width] rounded-shell-md border border-border bg-surface py-1.5 shadow-elevation-2 dark:bg-primary-100">
            {options.map((option) => (
              <BaseSelect.Item
                key={option.value}
                value={option.value}
                className="mx-1.5 flex cursor-pointer items-center gap-3 rounded-shell-sm px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover dark:data-[highlighted]:bg-primary-200"
              >
                <BaseSelect.ItemText>{option.label}</BaseSelect.ItemText>
                <BaseSelect.ItemIndicator className="ms-auto flex">
                  <Check className="size-4 text-accent" />
                </BaseSelect.ItemIndicator>
              </BaseSelect.Item>
            ))}
          </BaseSelect.Popup>
        </BaseSelect.Positioner>
      </BaseSelect.Portal>
    </BaseSelect.Root>
  );
}
