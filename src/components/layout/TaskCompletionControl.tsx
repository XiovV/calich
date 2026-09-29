import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check } from "lucide-react";

interface TaskCompletionControlProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  "aria-label": string;
}

// TaskCompletionControl is the circular completion control every Task row
// carries (#310, ADR-0068, ADR-0083): one click completes with no
// confirmation, another un-completes. Circular so it reads as "done" rather
// than "selected" — Checkbox.tsx's square shape is reserved for selection,
// this is the Completion glossary entry's own control.
export function TaskCompletionControl({
  checked,
  onCheckedChange,
  "aria-label": ariaLabel,
}: TaskCompletionControlProps) {
  return (
    <BaseCheckbox.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      aria-label={ariaLabel}
      className="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-full border border-border bg-surface data-[checked]:border-accent-ink data-[checked]:bg-accent"
    >
      <BaseCheckbox.Indicator className="flex items-center justify-center text-on-accent">
        <Check className="size-3" />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}
