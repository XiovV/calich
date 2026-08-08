import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Select } from "../components/ui/Select";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useAuthStore } from "../lib/authStore";
import { accountsApi, type Account, type AccountDisposition, type DeleteImpact } from "../lib/accountsApi";
import { errorMessage } from "../lib/errorMessage";

interface DeleteAccountDialogProps {
  account: Account;
  transferCandidates: Account[];
  onDelete: (disposition: AccountDisposition, transferTo?: number) => Promise<void>;
  onClose: () => void;
}

// The delete-account flow from #121 (ADR-0037): before an Admin can commit
// to removing an account, they see what it would cost (which owned
// Calendars have Shares, and how many people would lose Access), then must
// explicitly choose a disposition for those Calendars — there is no default,
// so a shared family Calendar is never destroyed because a dialog had one
// pre-selected and someone clicked through.
export function DeleteAccountDialog({
  account,
  transferCandidates,
  onDelete,
  onClose,
}: DeleteAccountDialogProps) {
  const accessToken = useAuthStore((state) => state.accessToken);

  const [impact, setImpact] = useState<DeleteImpact | null>(null);
  const [impactError, setImpactError] = useState<string | null>(null);
  const [disposition, setDisposition] = useState<AccountDisposition | null>(null);
  const [transferToId, setTransferToId] = useState<string>("");
  const { isSubmitting, error, run } = useAsyncAction();

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    accountsApi
      .deleteImpact(accessToken, account.id)
      .then((result) => {
        if (!cancelled) setImpact(result);
      })
      .catch((err) => {
        if (!cancelled) setImpactError(errorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [accessToken, account.id]);

  const sharedCalendars = impact?.calendars.filter((c) => c.shareCount > 0) ?? [];
  // The impact must be shown before a disposition can be chosen (#121: "First
  // the impact... Then the disposition") — an admin who never saw the cost
  // (still loading, or the fetch failed) can't pick transfer/delete yet.
  const impactShown = impact !== null || impactError !== null;
  const canConfirm =
    impactShown &&
    (disposition === "delete" || (disposition === "transfer" && transferToId !== ""));

  async function handleConfirm() {
    if (!canConfirm || !disposition) return;

    await run(async () => {
      await onDelete(disposition, disposition === "transfer" ? Number(transferToId) : undefined);
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
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            Delete {account.username}?
          </Dialog.Title>

          {impactError && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {impactError}
            </p>
          )}

          {!impact && !impactError && (
            <p className="mt-2 text-label-sm text-ink-muted">Loading…</p>
          )}

          {impact && (
            <div className="mt-2 text-body text-ink-muted">
              {sharedCalendars.length === 0 ? (
                <p>None of {account.username}'s calendars are shared with anyone.</p>
              ) : (
                <>
                  <p>
                    {impact.affectedUserCount} other{" "}
                    {impact.affectedUserCount === 1 ? "person" : "people"} would lose Access to a
                    calendar they don't own:
                  </p>
                  <ul className="mt-1.5 flex flex-col gap-0.5">
                    {sharedCalendars.map((calendar) => (
                      <li key={calendar.id} className="text-label-sm">
                        {calendar.name} — {calendar.shareCount}{" "}
                        {calendar.shareCount === 1 ? "person" : "people"}
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </div>
          )}

          {impactShown && (
            <div className="mt-4 flex flex-col gap-2">
              <p className="text-label-sm font-medium text-ink">
                What should happen to {account.username}'s calendars?
              </p>
              <div className="flex gap-2">
                <Button
                  variant={disposition === "transfer" ? "filled" : "outline"}
                  color="secondary"
                  size="small"
                  onClick={() => setDisposition("transfer")}
                >
                  Transfer them
                </Button>
                <Button
                  variant={disposition === "delete" ? "filled" : "outline"}
                  color="danger"
                  size="small"
                  onClick={() => setDisposition("delete")}
                >
                  Delete them
                </Button>
              </div>

              {disposition === "transfer" && (
                <>
                  {transferCandidates.length > 0 ? (
                    <Select
                      label="Transfer to"
                      value={transferToId}
                      onValueChange={setTransferToId}
                      options={transferCandidates.map((candidate) => ({
                        value: String(candidate.id),
                        label: candidate.username,
                      }))}
                      aria-label="Transfer calendars to"
                    />
                  ) : (
                    <p className="text-label-sm text-danger">
                      There is no other account to transfer these calendars to.
                    </p>
                  )}
                  <p className="text-label-sm text-ink-muted">
                    Events already written to these calendars, by anyone, are kept.
                  </p>
                </>
              )}

              {disposition === "delete" && (
                <p className="text-label-sm text-danger">
                  This permanently deletes {account.username}'s calendars and every event in them,
                  including events written by other people.
                </p>
              )}
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
