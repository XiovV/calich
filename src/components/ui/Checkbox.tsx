import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check } from "lucide-react";

interface CheckboxProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  "aria-label": string;
  disabled?: boolean;
}

export function Checkbox({
  checked,
  onCheckedChange,
  "aria-label": ariaLabel,
  disabled,
}: CheckboxProps) {
  return (
    <BaseCheckbox.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      aria-label={ariaLabel}
      disabled={disabled}
      className="flex size-4 cursor-pointer items-center justify-center rounded-shell-sm border border-border bg-surface data-[checked]:border-accent-ink data-[checked]:bg-accent data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60"
    >
      <BaseCheckbox.Indicator className="text-on-accent">
        <Check className="size-3" />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}
