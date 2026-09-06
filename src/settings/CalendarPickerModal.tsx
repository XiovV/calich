import { useEffect, useMemo, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useConnectionsStore } from "../lib/connectionsStore";
import { type PickerCalendar } from "../lib/connectionsApi";
import { useWorkspacesStore } from "../lib/workspacesStore";
import { deleteCalendarCascade } from "../lib/deleteCalendarCascade";
import { errorMessage } from "../lib/errorMessage";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Checkbox } from "../components/ui/Checkbox";
import { DeleteLinkedCalendarConfirmation } from "./DeleteLinkedCalendarConfirmation";

interface CalendarPickerModalProps {
  connectionId: number;
  onClose: () => void;
}

// The Calendar picker (#286, #295): a re-runnable dialog for choosing which
// of a Connection's calendars come in. Opens right after authorizing a
// Google account (ConnectionsSection), and again later from the Connection's
// row in Settings or its heading in the sidebar — every entry point renders
// this same component.
//
// It shows the state of the Workspace the User is in: a row already
// imported here is checked, and unchecking it deletes the Linked Calendar
// (behind a confirmation naming what is lost). A row imported into a
// different Workspace stays unchecked with a quiet note, so an unticked box
// is never a mystery. Newly-ticked rows are imported into the active
// Workspace on Confirm.
export function CalendarPickerModal({ connectionId, onClose }: CalendarPickerModalProps) {
  const listPickerCalendars = useConnectionsStore((state) => state.listPickerCalendars);
  const importCalendars = useConnectionsStore((state) => state.importCalendars);
  const fetchCalendars = useCalendarsStore((state) => state.fetchCalendars);
  // The picker's own trigger — Connect's redirect landing back on a fresh
  // full-page load — is the one moment nothing here can assume the active
  // Workspace has resolved yet: AppShell's own fetchWorkspaces() fires on
  // that same first render, and this effect would otherwise race it every
  // time, throwing "No active workspace" on the request workspaceHeaders()
  // builds before AppShell's fetch has had a chance to answer.
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  const [calendars, setCalendars] = useState<PickerCalendar[] | null>(null);
  const [checkedIds, setCheckedIds] = useState<Set<string>>(new Set());
  const [isLoading, setIsLoading] = useState(true);
  const [isImporting, setIsImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deletingCalendar, setDeletingCalendar] = useState<PickerCalendar | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  useEffect(() => {
    if (activeWorkspaceId === null) return;

    let cancelled = false;
    listPickerCalendars(connectionId)
      .then((result) => {
        if (cancelled) return;
        setCalendars(result);
        // A row already imported into this Workspace starts checked (#295);
        // for one imported nowhere, Google's own selection flag drives the
        // default. A row imported into another Workspace stays unchecked —
        // checking it would import a second copy here.
        setCheckedIds(
          new Set(
            result
              .filter((c) => c.importedHere || (!c.importedElsewhere && c.selected))
              .map((c) => c.id),
          ),
        );
      })
      .catch((err) => {
        if (!cancelled) setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectionId, activeWorkspaceId]);

  const byId = useMemo(() => {
    const map = new Map<string, PickerCalendar>();
    for (const c of calendars ?? []) map.set(c.id, c);
    return map;
  }, [calendars]);

  // Only rows that would be freshly created — a checked row not already
  // imported here. An already-imported row is removed by unchecking it, not
  // re-sent on Confirm.
  const toImport = useMemo(
    () => Array.from(checkedIds).filter((id) => !byId.get(id)?.importedHere),
    [checkedIds, byId],
  );

  function toggle(calendar: PickerCalendar) {
    if (checkedIds.has(calendar.id)) {
      // Unchecking a row that's already a Linked Calendar here is a delete
      // gesture — hold the confirmation before touching the checkbox.
      if (calendar.importedHere) {
        setDeletingCalendar(calendar);
        return;
      }
      setCheckedIds((ids) => {
        const next = new Set(ids);
        next.delete(calendar.id);
        return next;
      });
      return;
    }
    setCheckedIds((ids) => new Set(ids).add(calendar.id));
  }

  async function handleConfirmDelete() {
    if (!deletingCalendar?.localCalendarId) return;
    setIsDeleting(true);
    setError(null);
    // deleteCalendarCascade reports failure by return value, not by
    // throwing (it reverts the store and toasts on its own) — so a false
    // here means the calendar still exists and the picker's row must not be
    // flipped to "not imported".
    const deleted = await deleteCalendarCascade(deletingCalendar.localCalendarId);
    if (deleted) {
      // Reflect the deletion in the picker's own list so the row flips to
      // "not imported" and drops out of the checked set.
      setCalendars((prev) =>
        (prev ?? []).map((c) =>
          c.id === deletingCalendar.id
            ? { ...c, importedHere: false, localCalendarId: undefined, shareCount: 0 }
            : c,
        ),
      );
      setCheckedIds((ids) => {
        const next = new Set(ids);
        next.delete(deletingCalendar.id);
        return next;
      });
    } else {
      // deleteCalendarCascade has already reverted the store and toasted;
      // close the confirmation and leave the row checked, since the Linked
      // Calendar still exists.
      setError(`Couldn't remove "${deletingCalendar.name}". Try again.`);
    }
    setDeletingCalendar(null);
    setIsDeleting(false);
  }

  async function handleConfirm() {
    if (toImport.length === 0) {
      onClose();
      return;
    }
    setIsImporting(true);
    setError(null);
    try {
      await importCalendars(connectionId, toImport);
      // The picker's whole point is calendars appearing on the grid
      // immediately (#286) — a full re-fetch, not a local append, since the
      // server resolves each new Calendar's Access/isOwner/ownerName/
      // shareCount and this is a one-time action rather than a hot path
      // worth optimizing.
      await fetchCalendars();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsImporting(false);
    }
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !deletingCalendar) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            Choose calendars to bring in
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-label-sm text-ink-muted">
            Everything this Google account can see — its own calendars, the ones it&apos;s
            subscribed to, and the ones shared to it.
          </Dialog.Description>

          {error && (
            <p className="mt-3 text-label-sm text-danger" role="alert">
              {error}
            </p>
          )}

          {isLoading ? (
            <p className="mt-4 text-label-sm text-ink-muted">Loading calendars…</p>
          ) : calendars && calendars.length === 0 ? (
            <p className="mt-4 text-label-sm text-ink-muted">
              This account has no calendars to bring in.
            </p>
          ) : (
            <ul className="mt-4 flex max-h-80 flex-col gap-1 overflow-y-auto">
              {calendars?.map((calendar) => (
                <li key={calendar.id} className="flex items-center gap-2 py-1">
                  <Checkbox
                    checked={checkedIds.has(calendar.id)}
                    onCheckedChange={() => toggle(calendar)}
                    aria-label={calendar.name}
                  />
                  <span
                    className="size-3 shrink-0 rounded-full"
                    style={{ backgroundColor: calendar.color }}
                    aria-hidden
                  />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="min-w-0 truncate text-body text-ink">{calendar.name}</span>
                    {calendar.importedElsewhere && !calendar.importedHere && (
                      <span className="truncate text-label-sm text-ink-muted">
                        Already in another workspace
                      </span>
                    )}
                  </span>
                  {!calendar.writable && (
                    <span className="shrink-0 text-label-sm text-ink-muted">Read-only</span>
                  )}
                </li>
              ))}
            </ul>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
            >
              Cancel
            </Dialog.Close>
            <Button
              size="small"
              onClick={handleConfirm}
              disabled={isLoading || !calendars || calendars.length === 0}
              loading={isImporting}
            >
              {toImport.length === 0
                ? "Done"
                : `Add ${toImport.length} calendar${toImport.length === 1 ? "" : "s"}`}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>

      {deletingCalendar && (
        <DeleteLinkedCalendarConfirmation
          name={deletingCalendar.name}
          shareCount={deletingCalendar.shareCount}
          isDeleting={isDeleting}
          onConfirm={handleConfirmDelete}
          onClose={() => setDeletingCalendar(null)}
        />
      )}
    </Dialog.Root>
  );
}
