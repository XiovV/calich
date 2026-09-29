import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { IconButton } from "../components/ui/IconButton";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useAvailabilitySchedulesStore } from "../lib/availabilitySchedulesStore";
import type { AvailabilityRange, AvailabilitySchedule } from "../lib/availabilitySchedulesApi";
import { listTimeZones } from "../lib/timezones";

const WEEKDAY_LABELS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

// <input type="time"> emits/expects "HH:mm" — minutes-since-midnight is the
// wire/store unit, the same convention PreferencesSection's Working hours
// pair uses.
function minutesToTimeString(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return `${String(hours).padStart(2, "0")}:${String(mins).padStart(2, "0")}`;
}

function timeStringToMinutes(time: string): number {
  const [hours, minutes] = time.split(":").map(Number);
  return hours * 60 + minutes;
}

// A range as edited in this dialog, before it has an id of its own — key is
// local-only, for React's list identity, and is never sent to the server.
interface DraftRange extends AvailabilityRange {
  key: string;
}

let nextDraftKey = 0;
function newDraftKey(): string {
  nextDraftKey += 1;
  return `draft-${nextDraftKey}`;
}

function toDraftRanges(ranges: AvailabilityRange[]): DraftRange[] {
  return ranges.map((range) => ({ ...range, key: newDraftKey() }));
}

const TIME_ZONE_OPTIONS = listTimeZones().map((tz) => ({ value: tz, label: tz }));

interface AvailabilityScheduleDialogProps {
  // Absent for "create a new schedule"; present for "edit this one".
  schedule?: AvailabilitySchedule;
  initialTzid: string;
  onClose: () => void;
}

// The Availability Schedule editor (#320, ADR-0085): name, IANA timezone,
// and the weekly pattern of ranges a Booking Link's slots will later be
// derived from (#323). Several ranges per weekday are allowed, so a split
// day (mornings and late afternoons) is representable. Edits the whole
// Schedule as one form — Save replaces name, timezone and the entire range
// set at once, matching AvailabilityScheduleService.Update's own wholesale
// replace.
export function AvailabilityScheduleDialog({ schedule, initialTzid, onClose }: AvailabilityScheduleDialogProps) {
  const createSchedule = useAvailabilitySchedulesStore((state) => state.createSchedule);
  const updateSchedule = useAvailabilitySchedulesStore((state) => state.updateSchedule);

  const [name, setName] = useState(schedule?.name ?? "");
  const [tzid, setTzid] = useState(schedule?.tzid ?? initialTzid);
  const [ranges, setRanges] = useState<DraftRange[]>(() => toDraftRanges(schedule?.ranges ?? []));
  const { isSubmitting, error, run } = useAsyncAction();

  const isEditing = schedule !== undefined;
  const invalidRangeKeys = new Set(ranges.filter((r) => r.startMinute >= r.endMinute).map((r) => r.key));
  const canSave = name.trim() !== "" && invalidRangeKeys.size === 0;

  function addRange(weekday: number) {
    setRanges((current) => [...current, { key: newDraftKey(), weekday, startMinute: 9 * 60, endMinute: 17 * 60 }]);
  }

  function removeRange(key: string) {
    setRanges((current) => current.filter((r) => r.key !== key));
  }

  function updateRange(key: string, field: "startMinute" | "endMinute", minutes: number) {
    setRanges((current) => current.map((r) => (r.key === key ? { ...r, [field]: minutes } : r)));
  }

  async function handleSave() {
    if (!canSave) return;

    const payload: AvailabilityRange[] = ranges.map(({ weekday, startMinute, endMinute }) => ({
      weekday,
      startMinute,
      endMinute,
    }));

    await run(async () => {
      if (isEditing) {
        await updateSchedule(schedule.id, name.trim(), tzid, payload);
      } else {
        await createSchedule(name.trim(), tzid, payload);
      }
      onClose();
    });
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 max-h-[85vh] w-[30rem] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            {isEditing ? `Edit ${schedule.name}` : "New availability schedule"}
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-body text-ink-muted">
            When you're willing to be booked, separate from your working hours shading.
          </Dialog.Description>

          <div className="mt-4 flex flex-col gap-4">
            <Input label="Name" placeholder="Default" value={name} onChange={(e) => setName(e.target.value)} />

            <Select
              label="Timezone"
              value={tzid}
              onValueChange={setTzid}
              options={TIME_ZONE_OPTIONS}
            />

            <div>
              <p className="mb-2 text-label-sm font-medium text-ink">Weekly hours</p>
              <div className="flex flex-col gap-3">
                {WEEKDAY_LABELS.map((label, weekday) => {
                  const dayRanges = ranges.filter((r) => r.weekday === weekday);
                  return (
                    <div key={weekday}>
                      <div className="flex items-center justify-between">
                        <p className="text-body text-ink">{label}</p>
                        <IconButton
                          size="small"
                          aria-label={`Add a range on ${label}`}
                          onClick={() => addRange(weekday)}
                        >
                          <Plus className="size-4" />
                        </IconButton>
                      </div>
                      {dayRanges.length === 0 ? (
                        <p className="mt-1 text-label-sm text-ink-muted">Unavailable</p>
                      ) : (
                        <ul className="mt-1 flex flex-col gap-1.5">
                          {dayRanges.map((range) => (
                            <li key={range.key} className="flex items-center gap-2">
                              <Input
                                aria-label={`${label} range start`}
                                type="time"
                                size="small"
                                value={minutesToTimeString(range.startMinute)}
                                invalid={invalidRangeKeys.has(range.key)}
                                onChange={(e) => updateRange(range.key, "startMinute", timeStringToMinutes(e.target.value))}
                                className="flex-1"
                              />
                              <Input
                                aria-label={`${label} range end`}
                                type="time"
                                size="small"
                                value={minutesToTimeString(range.endMinute)}
                                invalid={invalidRangeKeys.has(range.key)}
                                onChange={(e) => updateRange(range.key, "endMinute", timeStringToMinutes(e.target.value))}
                                className="flex-1"
                              />
                              <IconButton
                                size="small"
                                color="danger"
                                aria-label={`Remove this range on ${label}`}
                                onClick={() => removeRange(range.key)}
                              >
                                <Trash2 className="size-4" />
                              </IconButton>
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                  );
                })}
              </div>
              {ranges.length === 0 && (
                <p className="mt-2 text-label-sm text-ink-muted">
                  No ranges yet — this schedule will offer no slots until you add one.
                </p>
              )}
            </div>
          </div>

          {error && (
            <p className="mt-3 text-label-sm text-danger" role="alert">
              {error}
            </p>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
              disabled={isSubmitting}
            >
              Cancel
            </Dialog.Close>
            <Button size="small" loading={isSubmitting} disabled={!canSave} onClick={handleSave}>
              Save
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
