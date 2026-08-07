import { useState } from "react";
import { X } from "lucide-react";
import type { Reminder } from "../lib/event";
import {
  normalizeCustomOffset,
  reminderChannelOptions,
  splitCustomOffset,
  type ReminderOffsetUnit,
} from "../lib/reminderOffset";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { IconButton } from "../components/ui/IconButton";

const UNIT_OPTIONS: { value: ReminderOffsetUnit; label: string }[] = [
  { value: "minutes", label: "minutes" },
  { value: "hours", label: "hours" },
  { value: "days", label: "days" },
  { value: "weeks", label: "weeks" },
];

interface ReminderRowProps {
  reminder: Reminder;
  // Whether the Email Channel can be offered — the user has an account
  // email set *and* the self-hoster has SMTP configured (ADR-0021).
  emailAvailable: boolean;
  onChange: (reminder: Reminder) => void;
  onRemove: () => void;
  // A read-only Event's Reminders are still legible but not editable (#94,
  // #111, ADR-0032, ADR-0034) — a Subscribed Calendar's Refresh is the
  // collection's only legitimate writer, and a Calendar the caller has no
  // more than Viewer Access to isn't theirs to edit either.
  disabled?: boolean;
}

// One Reminder row in the event modal's Reminders section: a Channel dropdown
// and an always-editable offset entry (amount + unit), matching Google/Proton
// Calendar rather than a preset list (ADR-0022, superseding ADR-0020's preset
// list).
export function ReminderRow({
  reminder,
  emailAvailable,
  onChange,
  onRemove,
  disabled,
}: ReminderRowProps) {
  // Seeded once from the incoming offset so the fields don't jump around
  // while the user is typing.
  const [amount, setAmount] = useState(() => splitCustomOffset(reminder.offsetMinutes).amount);
  const [unit, setUnit] = useState<ReminderOffsetUnit>(
    () => splitCustomOffset(reminder.offsetMinutes).unit,
  );

  function handleAmountChange(nextAmount: number) {
    setAmount(nextAmount);
    onChange({ ...reminder, offsetMinutes: normalizeCustomOffset(nextAmount, unit) });
  }

  function handleUnitChange(nextUnit: ReminderOffsetUnit) {
    setUnit(nextUnit);
    onChange({ ...reminder, offsetMinutes: normalizeCustomOffset(amount, nextUnit) });
  }

  return (
    <div className="flex items-end gap-2">
      <Select
        aria-label="Reminder channel"
        value={reminder.channel}
        onValueChange={(channel) => onChange({ ...reminder, channel })}
        options={reminderChannelOptions(emailAvailable)}
        disabled={disabled}
        className="min-w-0 shrink truncate"
      />
      <Input
        aria-label="Reminder offset amount"
        type="number"
        min={0}
        value={amount}
        onChange={(event) => handleAmountChange(Number(event.target.value))}
        disabled={disabled}
        className="w-16 shrink-0"
      />
      <Select
        aria-label="Reminder offset unit"
        value={unit}
        onValueChange={handleUnitChange}
        options={UNIT_OPTIONS}
        disabled={disabled}
        className="min-w-0 shrink truncate"
      />
      <IconButton aria-label="Remove reminder" onClick={onRemove} disabled={disabled}>
        <X className="size-4" />
      </IconButton>
    </div>
  );
}
