import { Select as BaseSelect } from "@base-ui/react/select";
import { ChevronDown } from "lucide-react";

interface SelectOption<T extends string> {
  value: T;
  label: string;
}

interface SelectProps<T extends string> {
  value: T;
  onValueChange: (value: T) => void;
  options: SelectOption<T>[];
  "aria-label"?: string;
}

export function Select<T extends string>({
  value,
  onValueChange,
  options,
  "aria-label": ariaLabel,
}: SelectProps<T>) {
  return (
    <BaseSelect.Root value={value} onValueChange={(next) => onValueChange(next as T)}>
      <BaseSelect.Trigger
        aria-label={ariaLabel}
        className="flex items-center gap-1 rounded-shell-sm border border-border bg-surface px-3 py-1.5 text-body text-ink hover:bg-surface-hover"
      >
        <BaseSelect.Value>
          {(selected: T) => options.find((option) => option.value === selected)?.label}
        </BaseSelect.Value>
        <BaseSelect.Icon>
          <ChevronDown className="size-4 text-ink-muted" />
        </BaseSelect.Icon>
      </BaseSelect.Trigger>
      <BaseSelect.Portal>
        <BaseSelect.Positioner sideOffset={4}>
          <BaseSelect.Popup className="rounded-shell-md border border-border bg-surface py-1 shadow-elevation-2">
            {options.map((option) => (
              <BaseSelect.Item
                key={option.value}
                value={option.value}
                className="cursor-default px-3 py-1.5 text-body text-ink data-[highlighted]:bg-surface-hover"
              >
                <BaseSelect.ItemText>{option.label}</BaseSelect.ItemText>
              </BaseSelect.Item>
            ))}
          </BaseSelect.Popup>
        </BaseSelect.Positioner>
      </BaseSelect.Portal>
    </BaseSelect.Root>
  );
}
