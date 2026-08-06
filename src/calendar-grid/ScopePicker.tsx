import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import type { EditScope } from "../lib/recurrenceScope";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";

const SCOPE_OPTIONS: { value: EditScope; label: string }[] = [
  { value: "this", label: "This event" },
  { value: "following", label: "This and following events" },
  { value: "all", label: "All events" },
];

interface ScopePickerProps {
  /** "Edit", "Delete", or "Download" — used in the title and the confirm
   * button's label. */
  action: string;
  /** Which of the three scopes to offer — defaults to all three. Download
   * only offers This event/All events, since "This and following" doesn't
   * make sense for a single file export (issue #78). */
  scopes?: EditScope[];
  onConfirm: (scope: EditScope) => void;
  onClose: () => void;
}

/**
 * The This event/This and following/All events prompt shown when editing or
 * deleting a recurring Occurrence, mirroring Google Calendar (ADR-0016).
 * Defaults to "This event".
 */
export function ScopePicker({ action, scopes, onConfirm, onClose }: ScopePickerProps) {
  const options = scopes
    ? SCOPE_OPTIONS.filter((option) => scopes.includes(option.value))
    : SCOPE_OPTIONS;
  const [scope, setScope] = useState<EditScope>(options[0]?.value ?? "this");

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            {action} recurring event
          </Dialog.Title>

          <RadioGroup
            value={scope}
            onValueChange={(value) => setScope(value as EditScope)}
            className="mt-4 flex flex-col gap-3"
          >
            {options.map((option) => (
              <label
                key={option.value}
                className="flex items-center gap-2 text-body text-ink"
              >
                <Radio.Root
                  value={option.value}
                  className="flex size-4 items-center justify-center rounded-full border border-border data-[checked]:border-accent"
                >
                  <Radio.Indicator className="size-2 rounded-full bg-accent" />
                </Radio.Root>
                {option.label}
              </label>
            ))}
          </RadioGroup>

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({
                variant: "outline",
                color: "secondary",
                size: "small",
              })}
            >
              Cancel
            </Dialog.Close>
            <Button size="small" onClick={() => onConfirm(scope)}>
              {action}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
