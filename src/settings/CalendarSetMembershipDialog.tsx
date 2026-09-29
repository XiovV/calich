import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Checkbox } from "../components/ui/Checkbox";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useConnectionsStore } from "../lib/connectionsStore";
import { useCalendarSetsStore } from "../lib/calendarSetsStore";
import { connectionGroupLabel, groupCalendarsForSidebar } from "../lib/calendarGrouping";
import { errorMessage } from "../lib/errorMessage";
import type { CalendarSet } from "../lib/calendarSetsApi";
import type { Calendar } from "../lib/calendar";

interface CalendarSetMembershipDialogProps {
  calendarSet: CalendarSet;
  onClose: () => void;
}

// The per-Set membership editor (#302, ADR-0082): every Calendar of the
// active Workspace, grouped exactly as the sidebar groups them (My
// calendars, Subscribed, one heading per Connection, Shared with me —
// calendarGrouping.ts), toggled in or out of this Set one at a time with
// immediate effect and no save step. Mirrors GroupMembersDialog's shape
// (#167), except there's nothing to fetch on demand: membership rides
// along on every calendarSetsStore Set already (ADR-0082's List).
export function CalendarSetMembershipDialog({ calendarSet, onClose }: CalendarSetMembershipDialogProps) {
  const calendars = useCalendarsStore((state) => state.calendars);
  const connections = useConnectionsStore((state) => state.connections);
  const calendarSets = useCalendarSetsStore((state) => state.calendarSets);
  const addCalendarToSet = useCalendarSetsStore((state) => state.addCalendarToSet);
  const removeCalendarFromSet = useCalendarSetsStore((state) => state.removeCalendarFromSet);

  const [busyCalendarId, setBusyCalendarId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  // Reads the live Set from the store rather than the calendarSet prop, so
  // a toggle made from this same dialog reflects immediately.
  const current = calendarSets.find((s) => s.id === calendarSet.id) ?? calendarSet;
  const memberIds = new Set(current.calendarIds);

  const { myCalendars, subscribedCalendars, linkedByConnection, sharedCalendars } = groupCalendarsForSidebar(calendars);

  async function handleToggle(calendar: Calendar, isMember: boolean) {
    setBusyCalendarId(calendar.id);
    setActionError(null);
    try {
      if (isMember) {
        await removeCalendarFromSet(calendarSet.id, calendar.id);
      } else {
        await addCalendarToSet(calendarSet.id, calendar.id);
      }
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      setBusyCalendarId(null);
    }
  }

  function renderRow(calendar: Calendar) {
    const isMember = memberIds.has(calendar.id);
    return (
      <li key={calendar.id} className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5">
        <span className="min-w-0 truncate text-body text-ink">{calendar.name}</span>
        <Checkbox
          checked={isMember}
          onCheckedChange={() => handleToggle(calendar, isMember)}
          disabled={busyCalendarId === calendar.id}
          aria-label={
            isMember
              ? `Remove ${calendar.name} from ${calendarSet.name}`
              : `Add ${calendar.name} to ${calendarSet.name}`
          }
        />
      </li>
    );
  }

  function renderGroup(label: string, list: Calendar[]) {
    if (list.length === 0) return null;
    return (
      <div key={label} className="mt-3 first:mt-0">
        <p className="px-2 text-label-sm font-medium text-ink-muted">{label}</p>
        <ul className="mt-1">{list.map(renderRow)}</ul>
      </div>
    );
  }

  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 max-h-[85vh] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">{calendarSet.name}</Dialog.Title>
          <Dialog.Description className="mt-1 text-body text-ink-muted">
            Choose which of this workspace's calendars belong to this set.
          </Dialog.Description>

          {actionError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {actionError}
            </p>
          )}

          <div className="mt-4">
            {renderGroup("My calendars", myCalendars)}
            {renderGroup("Subscribed calendars", subscribedCalendars)}
            {Array.from(linkedByConnection.entries()).map(([connectionId, group]) =>
              renderGroup(connectionGroupLabel(connectionId, group, connections), group),
            )}
            {renderGroup("Shared with me", sharedCalendars)}
          </div>

          {calendars.length === 0 && (
            <p className="mt-2 text-label-sm text-ink-muted">No calendars in this workspace yet.</p>
          )}

          <div className="mt-5 flex justify-end">
            <Dialog.Close className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}>
              Done
            </Dialog.Close>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
