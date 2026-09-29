import { useEffect, useState } from "react";
import { format } from "date-fns";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { useTimePattern } from "../hooks/useTimePattern";
import { errorMessage } from "../lib/errorMessage";
import { useAvailabilitySchedulesStore } from "../lib/availabilitySchedulesStore";
import { detectBrowserTimeZone } from "../lib/timezones";
import type { AvailabilityRange, AvailabilitySchedule } from "../lib/availabilitySchedulesApi";
import { AvailabilityScheduleDialog } from "./AvailabilityScheduleDialog";
import { DeleteAvailabilityScheduleDialog } from "./DeleteAvailabilityScheduleDialog";

const WEEKDAY_SHORT_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function formatMinuteOfDay(minutes: number, pattern: string): string {
  const date = new Date(2000, 0, 1, Math.floor(minutes / 60), minutes % 60);
  return format(date, pattern);
}

function groupByWeekday(ranges: AvailabilityRange[]): [number, AvailabilityRange[]][] {
  const byWeekday = new Map<number, AvailabilityRange[]>();
  for (const range of ranges) {
    const existing = byWeekday.get(range.weekday) ?? [];
    existing.push(range);
    byWeekday.set(range.weekday, existing);
  }
  return Array.from(byWeekday.entries()).sort(([a], [b]) => a - b);
}

// The Availability Schedules Settings Section (#320, ADR-0085): a named
// weekly pattern of time ranges a User is willing to be booked on, separate
// from Working hours' grid-shading (PreferencesSection). Sits in Settings'
// Personal group beside Calendar sets — private outright, no sharing
// mechanism, no Role. The backend seeds a "Default" schedule the first time
// this section asks for the caller's schedules and finds none, copying it
// from Working hours (or Mon-Fri 09:00-17:00 when that's unset) and taking
// this browser's own detected timezone; editing Working hours afterwards
// never reaches it again.
export function AvailabilitySchedulesSection() {
  const schedules = useAvailabilitySchedulesStore((state) => state.schedules);
  const fetchSchedules = useAvailabilitySchedulesStore((state) => state.fetchSchedules);
  const timePattern = useTimePattern();

  const [loadError, setLoadError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [editingTarget, setEditingTarget] = useState<AvailabilitySchedule | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AvailabilitySchedule | null>(null);

  useEffect(() => {
    fetchSchedules(detectBrowserTimeZone())
      .then(() => setLoadError(null))
      .catch((err) => setLoadError(errorMessage(err)));
  }, [fetchSchedules]);

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Availability schedules</h2>
      <p className="mt-1 text-body text-ink-muted">
        When you're willing to be booked — separate from Working hours, which only shades the grid. Reusable
        across your booking links.
      </p>

      {loadError && <p className="mt-2 text-label-sm text-danger">{loadError}</p>}

      <div className="mt-4">
        <Button size="small" leadingIcon={<Plus className="size-4" />} onClick={() => setIsCreating(true)}>
          New schedule
        </Button>
      </div>

      <ul className="mt-4 flex flex-col gap-2">
        {schedules.map((schedule) => {
          const groups = groupByWeekday(schedule.ranges);
          return (
            <li key={schedule.id} className="rounded-md border border-border px-3 py-2">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="text-body font-medium text-ink">{schedule.name}</p>
                  <p className="text-label-sm text-ink-muted">{schedule.tzid}</p>
                </div>
                <div className="flex items-center gap-1">
                  <IconButton aria-label={`Edit ${schedule.name}`} onClick={() => setEditingTarget(schedule)}>
                    <Pencil className="size-4" />
                  </IconButton>
                  <IconButton aria-label={`Delete ${schedule.name}`} onClick={() => setDeleteTarget(schedule)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </div>
              </div>

              {groups.length === 0 ? (
                <p className="mt-2 text-label-sm text-ink-muted">No slots yet — this schedule offers nothing to book.</p>
              ) : (
                <ul className="mt-2 flex flex-col gap-0.5">
                  {groups.map(([weekday, dayRanges]) => (
                    <li key={weekday} className="text-label-sm text-ink-muted">
                      <span className="font-medium text-ink">{WEEKDAY_SHORT_LABELS[weekday]}</span>{" "}
                      {dayRanges
                        .map((r) => `${formatMinuteOfDay(r.startMinute, timePattern)}–${formatMinuteOfDay(r.endMinute, timePattern)}`)
                        .join(", ")}
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}

        {schedules.length === 0 && !loadError && (
          <p className="text-label-sm text-ink-muted">No availability schedules yet.</p>
        )}
      </ul>

      {isCreating && (
        <AvailabilityScheduleDialog initialTzid={detectBrowserTimeZone()} onClose={() => setIsCreating(false)} />
      )}
      {editingTarget && (
        <AvailabilityScheduleDialog
          schedule={editingTarget}
          initialTzid={editingTarget.tzid}
          onClose={() => setEditingTarget(null)}
        />
      )}
      {deleteTarget && (
        <DeleteAvailabilityScheduleDialog schedule={deleteTarget} onClose={() => setDeleteTarget(null)} />
      )}
    </section>
  );
}
