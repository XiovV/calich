import { useEffect, useMemo, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Select } from "../components/ui/Select";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useAuthStore } from "../lib/authStore";
import { accountApi, type CalendarDisposition, type DeleteImpact } from "../lib/accountApi";
import type { CalendarDispositionChoice } from "../lib/accountApi";
import { errorMessage } from "../lib/errorMessage";

interface DeleteAccountDialogProps {
  onClose: () => void;
}

interface RowState {
  disposition: CalendarDisposition | null;
  transferTo: string;
}

// The self-Delete flow (ADR-0044): every Calendar the caller owns, across
// every Workspace they belong to, needs its own explicit transfer-or-delete
// choice — there is no default, so a shared calendar is never destroyed or
// handed off because a dialog had one pre-selected.
export function DeleteAccountDialog({ onClose }: DeleteAccountDialogProps) {
  const accessToken = useAuthStore((state) => state.accessToken);
  const deleteAccount = useAuthStore((state) => state.deleteAccount);

  const [impact, setImpact] = useState<DeleteImpact | null>(null);
  const [impactError, setImpactError] = useState<string | null>(null);
  const [rows, setRows] = useState<Record<string, RowState>>({});
  const { isSubmitting, error, run } = useAsyncAction();

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    accountApi
      .deleteImpact(accessToken)
      .then((result) => {
        if (cancelled) return;
        setImpact(result);
        setRows(
          Object.fromEntries(
            result.calendars.map((c) => [c.id, { disposition: null, transferTo: "" }]),
          ),
        );
      })
      .catch((err) => {
        if (!cancelled) setImpactError(errorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [accessToken]);

  const calendars = useMemo(() => impact?.calendars ?? [], [impact]);

  // Requires a successfully loaded impact, even when it turns out to list no
  // calendars — otherwise a failed fetch (impact stays null) would look
  // identical to "you own nothing" and let this destructive action's confirm
  // button enable without ever having shown what it's about to affect.
  const canConfirm = useMemo(() => {
    if (!impact) return false;
    return calendars.every((c) => {
      const row = rows[c.id];
      if (!row || !row.disposition) return false;
      if (row.disposition === "transfer") return row.transferTo !== "";
      return true;
    });
  }, [impact, calendars, rows]);

  function setDisposition(calendarId: string, disposition: CalendarDisposition) {
    setRows((prev) => ({ ...prev, [calendarId]: { disposition, transferTo: prev[calendarId]?.transferTo ?? "" } }));
  }

  function setTransferTo(calendarId: string, transferTo: string) {
    setRows((prev) => ({ ...prev, [calendarId]: { disposition: "transfer", transferTo } }));
  }

  async function handleConfirm() {
    if (!canConfirm) return;

    const choices: CalendarDispositionChoice[] = calendars.map((c) => {
      const row = rows[c.id];
      return {
        calendarId: c.id,
        disposition: row.disposition as CalendarDisposition,
        ...(row.disposition === "transfer" ? { transferTo: Number(row.transferTo) } : {}),
      };
    });

    await run(async () => {
      await deleteAccount(choices);
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
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-[30rem] max-h-[85vh] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">Delete your account?</Dialog.Title>
          <p className="mt-1 text-body text-ink-muted">
            This permanently deletes your account. It cannot be undone.
          </p>

          {impactError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {impactError}
            </p>
          )}

          {!impact && !impactError && (
            <p className="mt-2 text-label-sm text-ink-muted">Loading…</p>
          )}

          {impact && calendars.length === 0 && (
            <p className="mt-2 text-body text-ink-muted">You don't own any calendars.</p>
          )}

          {calendars.length > 0 && (
            <div className="mt-4 flex flex-col gap-4">
              {calendars.map((c) => {
                const row = rows[c.id] ?? { disposition: null, transferTo: "" };
                return (
                  <div key={c.id} className="rounded-md border border-border p-3">
                    <p className="text-label-sm font-medium text-ink">{c.name}</p>
                    <p className="text-label-sm text-ink-muted">
                      {c.workspaceName}
                      {c.shareCount > 0 &&
                        ` — shared with ${c.shareCount} ${c.shareCount === 1 ? "person" : "people"}`}
                    </p>

                    <div className="mt-2 flex gap-2">
                      <Button
                        variant={row.disposition === "transfer" ? "filled" : "outline"}
                        color="secondary"
                        size="small"
                        onClick={() => setDisposition(c.id, "transfer")}
                      >
                        Transfer
                      </Button>
                      <Button
                        variant={row.disposition === "delete" ? "filled" : "outline"}
                        color="danger"
                        size="small"
                        onClick={() => setDisposition(c.id, "delete")}
                      >
                        Delete
                      </Button>
                    </div>

                    {row.disposition === "transfer" && (
                      <>
                        {c.transferCandidates.length > 0 ? (
                          <Select
                            label="Transfer to"
                            value={row.transferTo}
                            onValueChange={(value) => setTransferTo(c.id, value)}
                            options={c.transferCandidates.map((candidate) => ({
                              value: String(candidate.id),
                              label: candidate.username,
                            }))}
                            aria-label={`Transfer ${c.name} to`}
                            className="mt-2"
                          />
                        ) : (
                          <p className="mt-2 text-label-sm text-danger">
                            There is no other member of {c.workspaceName} to transfer this calendar to.
                          </p>
                        )}
                      </>
                    )}

                    {row.disposition === "delete" && c.shareCount > 0 && (
                      <p className="mt-2 text-label-sm text-danger">
                        This permanently deletes this calendar and every event in it, including events
                        written by other people.
                      </p>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {error && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
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
            <Button
              color="danger"
              size="small"
              disabled={!canConfirm}
              loading={isSubmitting}
              onClick={handleConfirm}
            >
              Delete account
            </Button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
