import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { useConnectionsStore } from "../lib/connectionsStore";
import { useCalendarsStore } from "../lib/calendarsStore";
import { useEventsStore } from "../lib/eventsStore";
import { useShellStore } from "../lib/shellStore";
import { type DisconnectDisposition, type DisconnectImpact } from "../lib/connectionsApi";
import { errorMessage } from "../lib/errorMessage";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";

interface DisconnectConnectionModalProps {
  connectionId: number;
  accountEmail: string;
  onClose: () => void;
}

// Disconnecting a Connection demands an explicit disposition for its Linked
// Calendars (#295) — deletion is unrecoverable, the mirror may be the
// User's last copy if they're leaving the Provider, and a Linked Calendar is
// Shareable, so deleting one strips it out of other Members' sidebars. The
// default is to keep the Calendars as ordinary owned ones.
export function DisconnectConnectionModal({
  connectionId,
  accountEmail,
  onClose,
}: DisconnectConnectionModalProps) {
  const disconnectImpact = useConnectionsStore((state) => state.disconnectImpact);
  const disconnect = useConnectionsStore((state) => state.disconnect);
  const fetchCalendars = useCalendarsStore((state) => state.fetchCalendars);

  const [impact, setImpact] = useState<DisconnectImpact | null>(null);
  const [disposition, setDisposition] = useState<DisconnectDisposition>("keep");
  const [isLoading, setIsLoading] = useState(true);
  const [isDisconnecting, setIsDisconnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    disconnectImpact(connectionId)
      .then((result) => {
        if (!cancelled) setImpact(result);
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
  }, [connectionId, disconnectImpact]);

  const linkedCalendars = impact?.linkedCalendars ?? [];
  const sharedCount = linkedCalendars.filter((c) => c.shareCount > 0).length;

  async function handleConfirm() {
    setIsDisconnecting(true);
    setError(null);
    try {
      await disconnect(connectionId, disposition);
      // A "delete" disposition has already removed these Calendars and their
      // Events server-side — but an Event already loaded into eventsStore
      // doesn't know that, and its Calendar no longer resolving is what
      // renders it gray (calendarColors.ts's UNRESOLVED_CALENDAR_COLOR
      // fallback) instead of dropping it, same failure deleteCalendarCascade
      // and leaveCalendarCascade exist to avoid for their own delete paths.
      // No rollback needed here, unlike those two: disconnect() has already
      // succeeded by this point, so there's nothing to revert to.
      if (disposition === "delete") {
        for (const calendar of linkedCalendars) {
          useEventsStore.getState().removeEventsByCalendarId(calendar.id);
          useShellStore.getState().removeCheckedCalendarId(calendar.id);
        }
      }
      // A "delete" disposition removes Calendars from the sidebar; a "keep"
      // one turns them into ordinary owned Calendars. Either way the
      // sidebar's Connection heading is gone, so re-fetch — best-effort,
      // since the disconnect itself has already committed.
      await fetchCalendars().catch(() => {});
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsDisconnecting(false);
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
            Disconnect {accountEmail}?
          </Dialog.Title>

          {error && (
            <p className="mt-3 text-label-sm text-danger" role="alert">
              {error}
            </p>
          )}

          {isLoading ? (
            <p className="mt-4 text-label-sm text-ink-muted">Checking what this affects…</p>
          ) : linkedCalendars.length === 0 ? (
            <Dialog.Description className="mt-2 text-body text-ink-muted">
              This account has no calendars here. Disconnecting just removes the grant.
            </Dialog.Description>
          ) : (
            <>
              <Dialog.Description className="mt-2 text-body text-ink-muted">
                {linkedCalendars.length === 1
                  ? "1 calendar came in from this account:"
                  : `${linkedCalendars.length} calendars came in from this account:`}
              </Dialog.Description>
              <ul className="mt-2 flex flex-col gap-1">
                {linkedCalendars.map((calendar) => (
                  <li key={calendar.id} className="flex items-center justify-between text-body text-ink">
                    <span className="min-w-0 truncate">{calendar.name}</span>
                    {calendar.shareCount > 0 && (
                      <span className="shrink-0 text-label-sm text-ink-muted">
                        shared with {calendar.shareCount}
                      </span>
                    )}
                  </li>
                ))}
              </ul>

              <RadioGroup
                value={disposition}
                onValueChange={(value) => setDisposition(value as DisconnectDisposition)}
                className="mt-4 flex flex-col gap-3"
              >
                <label className="flex items-start gap-2 text-body text-ink">
                  <Radio.Root
                    value="keep"
                    className="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border border-border data-[checked]:border-accent-ink"
                  >
                    <Radio.Indicator className="size-2 rounded-full bg-accent-ink" />
                  </Radio.Root>
                  <span className="flex flex-col">
                    Keep the calendars
                    <span className="text-label-sm text-ink-muted">
                      They stay as ordinary calendars you own, no longer syncing with Google.
                    </span>
                  </span>
                </label>
                <label className="flex items-start gap-2 text-body text-ink">
                  <Radio.Root
                    value="delete"
                    className="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border border-border data-[checked]:border-danger"
                  >
                    <Radio.Indicator className="size-2 rounded-full bg-danger" />
                  </Radio.Root>
                  <span className="flex flex-col">
                    Delete the calendars and their events
                    <span className="text-label-sm text-ink-muted">
                      This cannot be undone.
                      {sharedCount > 0 &&
                        ` ${sharedCount === 1 ? "1 calendar is" : `${sharedCount} calendars are`} shared with other people — they will lose ${sharedCount === 1 ? "it" : "them"} too.`}
                    </span>
                  </span>
                </label>
              </RadioGroup>
            </>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
            >
              Cancel
            </Dialog.Close>
            <Button
              size="small"
              color={disposition === "delete" ? "danger" : "primary"}
              onClick={handleConfirm}
              disabled={isLoading}
              loading={isDisconnecting}
            >
              {disposition === "delete" ? "Disconnect and delete" : "Disconnect"}
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
