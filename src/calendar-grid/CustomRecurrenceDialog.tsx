import { useState } from "react";
import { addMonths, format } from "date-fns";
import { Dialog } from "@base-ui/react/dialog";
import {
  buildCustomRule,
  type CustomEnd,
  type CustomRecurrence,
  type MonthlyMode,
  type RecurrenceUnit,
} from "../lib/customRecurrence";
import { ORDINALS, nthWeekdayOfMonth } from "../lib/rruleParts";
import { useWeekStartsOn } from "../hooks/useWeekStartsOn";
import { Select } from "../components/ui/Select";
import { Input } from "../components/ui/Input";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";

interface CustomRecurrenceDialogProps {
  initial: CustomRecurrence;
  start: Date;
  onConfirm: (rrule: string) => void;
  onClose: () => void;
}

const UNIT_ORDER: RecurrenceUnit[] = ["day", "week", "month", "year"];
// Indexed by Date.getDay() (0 = Sunday), same as `weekdays`. Display order
// alone rotates to the viewer's Week start (below) — this is never read in
// that order. Single letters need full aria labels.
const WEEKDAY_CHIPS = [
  { index: 0, letter: "S", name: "Sunday" },
  { index: 1, letter: "M", name: "Monday" },
  { index: 2, letter: "T", name: "Tuesday" },
  { index: 3, letter: "W", name: "Wednesday" },
  { index: 4, letter: "T", name: "Thursday" },
  { index: 5, letter: "F", name: "Friday" },
  { index: 6, letter: "S", name: "Saturday" },
];

function unitLabel(unit: RecurrenceUnit, interval: number): string {
  return interval > 1 ? `${unit}s` : unit;
}

function toDateInputValue(date: Date): string {
  return format(date, "yyyy-MM-dd");
}

function fromDateInputValue(value: string): Date | null {
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) return null;
  return new Date(year, month - 1, day);
}

export function CustomRecurrenceDialog({
  initial,
  start,
  onConfirm,
  onClose,
}: CustomRecurrenceDialogProps) {
  const weekStartsOn = useWeekStartsOn();
  const orderedWeekdayChips = [
    ...WEEKDAY_CHIPS.slice(weekStartsOn),
    ...WEEKDAY_CHIPS.slice(0, weekStartsOn),
  ];
  const [repeatInterval, setRepeatInterval] = useState(initial.interval);
  const [unit, setUnit] = useState<RecurrenceUnit>(initial.unit);
  const [weekdays, setWeekdays] = useState<number[]>(initial.weekdays);
  const [monthlyMode, setMonthlyMode] = useState<MonthlyMode>(
    initial.monthlyMode,
  );
  const [endType, setEndType] = useState<CustomEnd["type"]>(initial.end.type);
  const [endDate, setEndDate] = useState<Date>(
    initial.end.type === "on" ? initial.end.date : addMonths(start, 1),
  );
  const [endCount, setEndCount] = useState<number>(
    initial.end.type === "after" ? initial.end.count : 13,
  );

  function toggleWeekday(index: number) {
    setWeekdays((current) =>
      current.includes(index)
        ? current.filter((day) => day !== index)
        : [...current, index],
    );
  }

  function handleDone() {
    const end: CustomEnd =
      endType === "never"
        ? { type: "never" }
        : endType === "on"
          ? { type: "on", date: endDate }
          : { type: "after", count: Math.max(1, endCount) };

    const fields: CustomRecurrence = {
      interval: Math.max(1, repeatInterval),
      unit,
      weekdays,
      monthlyMode,
      end,
    };
    onConfirm(buildCustomRule(fields, start));
  }

  const monthlyOptions = [
    { value: "day" as const, label: `Monthly on day ${start.getDate()}` },
    {
      value: "weekday" as const,
      label: `Monthly on the ${ORDINALS[nthWeekdayOfMonth(start) - 1]} ${format(start, "EEEE")}`,
    },
  ];

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            Custom recurrence
          </Dialog.Title>

          <div className="mt-4 flex items-end gap-3">
            <Input
              label="Repeat every"
              type="number"
              min={1}
              value={repeatInterval}
              onChange={(event) => setRepeatInterval(Number(event.target.value))}
              className="w-24"
            />
            <Select
              aria-label="Repeat unit"
              value={unit}
              onValueChange={(value) => setUnit(value as RecurrenceUnit)}
              options={UNIT_ORDER.map((value) => ({
                value,
                label: unitLabel(value, repeatInterval),
              }))}
            />
          </div>

          {unit === "week" && (
            <div className="mt-4">
              <p className="mb-1.5 text-label-sm text-ink-muted">Repeat on</p>
              <div className="flex gap-1.5">
                {orderedWeekdayChips.map((chip) => {
                  const selected = weekdays.includes(chip.index);
                  return (
                    <button
                      key={chip.index}
                      type="button"
                      aria-label={chip.name}
                      aria-pressed={selected}
                      onClick={() => toggleWeekday(chip.index)}
                      className={`flex size-8 items-center justify-center rounded-shell-pill text-label-sm ${
                        selected
                          ? "bg-accent text-ink-inverse"
                          : "bg-surface-sunken text-ink ring-1 ring-border hover:bg-surface-hover"
                      }`}
                    >
                      {chip.letter}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {unit === "month" && (
            <div className="mt-4">
              <Select
                aria-label="Monthly repeat mode"
                value={monthlyMode}
                onValueChange={(value) => setMonthlyMode(value as MonthlyMode)}
                options={monthlyOptions}
              />
            </div>
          )}

          <div className="mt-5">
            <p className="mb-1.5 text-label-sm text-ink-muted">Ends</p>
            <div className="flex flex-col gap-2">
              <label className="flex items-center gap-2 text-body text-ink">
                <input
                  type="radio"
                  name="ends"
                  checked={endType === "never"}
                  onChange={() => setEndType("never")}
                  className="accent-accent"
                />
                Never
              </label>

              <label className="flex items-center gap-2 text-body text-ink">
                <input
                  type="radio"
                  name="ends"
                  checked={endType === "on"}
                  onChange={() => setEndType("on")}
                  className="accent-accent"
                />
                <span className="w-12">On</span>
                <input
                  type="date"
                  value={toDateInputValue(endDate)}
                  min={toDateInputValue(start)}
                  onChange={(event) => {
                    const parsed = fromDateInputValue(event.target.value);
                    if (parsed) setEndDate(parsed);
                    setEndType("on");
                  }}
                  className="flex-1 rounded-shell-md bg-surface-sunken px-3 py-1.5 text-body text-ink ring-1 ring-border outline-none focus:ring-2 focus:ring-accent"
                />
              </label>

              <label className="flex items-center gap-2 text-body text-ink">
                <input
                  type="radio"
                  name="ends"
                  checked={endType === "after"}
                  onChange={() => setEndType("after")}
                  className="accent-accent"
                />
                <span className="w-12">After</span>
                <input
                  type="number"
                  min={1}
                  value={endCount}
                  onChange={(event) => {
                    setEndCount(Number(event.target.value));
                    setEndType("after");
                  }}
                  className="w-20 rounded-shell-md bg-surface-sunken px-3 py-1.5 text-body text-ink ring-1 ring-border outline-none focus:ring-2 focus:ring-accent"
                />
                <span className="text-ink-muted">occurrences</span>
              </label>
            </div>
          </div>

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
            <Button size="small" onClick={handleDone}>
              Done
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
