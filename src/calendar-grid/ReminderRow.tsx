import { useState } from "react";
import { X } from "lucide-react";
import type { Reminder } from "../lib/event";
import {
  matchPreset,
  normalizeCustomOffset,
  presetOffsetMinutes,
  reminderChannelOptions,
  reminderPresetLabel,
  reminderPresets,
  splitCustomOffset,
  type ReminderOffsetUnit,
  type ReminderPreset,
} from "../lib/reminderPresets";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { IconButton } from "../components/ui/IconButton";

type OffsetChoice = ReminderPreset | "custom";

const UNIT_OPTIONS: { value: ReminderOffsetUnit; label: string }[] = [
  { value: "minutes", label: "minutes" },
  { value: "hours", label: "hours" },
  { value: "days", label: "days" },
  { value: "weeks", label: "weeks" },
];

interface ReminderRowProps {
  reminder: Reminder;
  allDay: boolean;
  // Whether the Email Channel can be offered — the user has an account
  // email set *and* the self-hoster has SMTP configured (ADR-0021).
  emailAvailable: boolean;
  onChange: (reminder: Reminder) => void;
  onRemove: () => void;
}

// One Reminder row in the event modal's Reminders section: an offset control
// (preset dropdown + a custom amount/unit entry, mirroring the Custom
// recurrence dialog) and a Channel dropdown. Toggling `allDay` switches the
// offered presets/labels to the 09:00-anchored set (ADR-0020).
export function ReminderRow({ reminder, allDay, emailAvailable, onChange, onRemove }: ReminderRowProps) {
  const matched = matchPreset(reminder.offsetMinutes, allDay);
  const offsetChoice: OffsetChoice = matched ?? "custom";

  // Seeded once from the incoming offset so the fields don't jump around
  // while the user is typing; only relevant while "Custom…" is selected.
  const [customAmount, setCustomAmount] = useState(
    () => splitCustomOffset(reminder.offsetMinutes).amount,
  );
  const [customUnit, setCustomUnit] = useState<ReminderOffsetUnit>(
    () => splitCustomOffset(reminder.offsetMinutes).unit,
  );

  const offsetOptions = [
    ...reminderPresets(allDay).map((preset) => ({
      value: preset as OffsetChoice,
      label: reminderPresetLabel(preset, allDay),
    })),
    { value: "custom" as OffsetChoice, label: "Custom…" },
  ];

  function handleOffsetChoiceChange(value: OffsetChoice) {
    if (value === "custom") {
      onChange({ ...reminder, offsetMinutes: normalizeCustomOffset(customAmount, customUnit) });
      return;
    }
    onChange({ ...reminder, offsetMinutes: presetOffsetMinutes(value) });
  }

  function handleCustomAmountChange(amount: number) {
    setCustomAmount(amount);
    onChange({ ...reminder, offsetMinutes: normalizeCustomOffset(amount, customUnit) });
  }

  function handleCustomUnitChange(unit: ReminderOffsetUnit) {
    setCustomUnit(unit);
    onChange({ ...reminder, offsetMinutes: normalizeCustomOffset(customAmount, unit) });
  }

  return (
    <div className="flex items-end gap-2">
      <div className="flex flex-1 flex-wrap items-end gap-2">
        <Select
          aria-label="Reminder offset"
          value={offsetChoice}
          onValueChange={handleOffsetChoiceChange}
          options={offsetOptions}
        />
        {offsetChoice === "custom" && (
          <>
            <Input
              aria-label="Custom offset amount"
              type="number"
              min={0}
              value={customAmount}
              onChange={(event) => handleCustomAmountChange(Number(event.target.value))}
              className="w-20"
            />
            <Select
              aria-label="Custom offset unit"
              value={customUnit}
              onValueChange={handleCustomUnitChange}
              options={UNIT_OPTIONS}
            />
          </>
        )}
        <Select
          aria-label="Reminder channel"
          value={reminder.channel}
          onValueChange={(channel) => onChange({ ...reminder, channel })}
          options={reminderChannelOptions(emailAvailable)}
        />
      </div>
      <IconButton aria-label="Remove reminder" onClick={onRemove}>
        <X className="size-4" />
      </IconButton>
    </div>
  );
}
