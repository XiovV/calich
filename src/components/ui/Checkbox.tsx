import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check } from "lucide-react";

interface CheckboxProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  "aria-label": string;
}

export function Checkbox({
  checked,
  onCheckedChange,
  "aria-label": ariaLabel,
}: CheckboxProps) {
  return (
    <BaseCheckbox.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      aria-label={ariaLabel}
      className="flex size-4 items-center justify-center rounded-shell-sm border border-border bg-surface data-[checked]:border-accent data-[checked]:bg-accent"
    >
      <BaseCheckbox.Indicator className="text-ink-inverse">
        <Check className="size-3" />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}
