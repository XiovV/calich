import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useConnectionsStore } from "../lib/connectionsStore";
import { type PickerCalendar } from "../lib/connectionsApi";
import { useWorkspacesStore } from "../lib/workspacesStore";
import { errorMessage } from "../lib/errorMessage";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Checkbox } from "../components/ui/Checkbox";

interface CalendarPickerModalProps {
  connectionId: number;
  onClose: () => void;
}

// The Calendar picker (#286): opens right after authorizing a Google
// account (ConnectionsSection) so a User chooses which of the account's
// calendars come in, and is reachable nowhere else yet — re-opening it
// later to add a calendar is a later ticket's. Lists everything the account
// can see, Google's own selected flag pre-checking the default working set,
// with a read-only badge for a row Google reports the account can't write
// to there.
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

  useEffect(() => {
    if (activeWorkspaceId === null) return;

    let cancelled = false;
    listPickerCalendars(connectionId)
      .then((result) => {
        if (cancelled) return;
        setCalendars(result);
        // Google's own selection flag drives the default checked state
        // (#286's acceptance criteria) — the default matches the working
        // set the User already keeps at Google.
        setCheckedIds(new Set(result.filter((c) => c.selected).map((c) => c.id)));
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

  function toggle(id: string) {
    setCheckedIds((ids) => {
      const next = new Set(ids);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function handleConfirm() {
    setIsImporting(true);
    setError(null);
    try {
      await importCalendars(connectionId, Array.from(checkedIds));
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
        if (!open) onClose();
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
                    onCheckedChange={() => toggle(calendar.id)}
                    aria-label={calendar.name}
                  />
                  <span
                    className="size-3 shrink-0 rounded-full"
                    style={{ backgroundColor: calendar.color }}
                    aria-hidden
                  />
                  <span className="min-w-0 flex-1 truncate text-body text-ink">
                    {calendar.name}
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
              Add {checkedIds.size > 0 ? checkedIds.size : ""} calendar
              {checkedIds.size === 1 ? "" : "s"}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
