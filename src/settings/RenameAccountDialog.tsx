import { useEffect, useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Input } from "../components/ui/Input";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useAuthStore } from "../lib/authStore";
import { accountsApi, type Account } from "../lib/accountsApi";
import { errorMessage } from "../lib/errorMessage";

interface RenameAccountDialogProps {
  account: Account;
  onRename: (username: string) => Promise<void>;
  onClose: () => void;
}

// The rename-account flow (#125, ADR-0037): CalDAV auth is keyed by
// username, so renaming somebody else's account stops every device already
// configured with the old one from syncing until it's updated there too —
// the App passwords themselves stay valid. That's only worth interrupting
// for when the account actually holds one, so the impact is fetched first
// and the confirmation only shown once it's known to be non-zero.
export function RenameAccountDialog({ account, onRename, onClose }: RenameAccountDialogProps) {
  const accessToken = useAuthStore((state) => state.accessToken);

  const [username, setUsername] = useState(account.username);
  const [appPasswordCount, setAppPasswordCount] = useState<number | null>(null);
  const [impactError, setImpactError] = useState<string | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const { isSubmitting, error, run } = useAsyncAction();

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    accountsApi
      .usernameImpact(accessToken, account.id)
      .then((result) => {
        if (!cancelled) setAppPasswordCount(result.appPasswordCount);
      })
      .catch((err) => {
        if (!cancelled) setImpactError(errorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [accessToken, account.id]);

  const impactShown = appPasswordCount !== null || impactError !== null;
  const isUnchanged = username.trim() === account.username;
  const needsConfirmation = (appPasswordCount ?? 0) >= 1;

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isUnchanged || !username.trim() || !impactShown) return;

    if (needsConfirmation && !confirmed) {
      setConfirmed(true);
      return;
    }

    await run(async () => {
      await onRename(username.trim());
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
            Rename {account.username}
          </Dialog.Title>

          <form onSubmit={handleSubmit}>
            <Input
              label="Username"
              value={username}
              onChange={(domEvent) => {
                setUsername(domEvent.target.value);
                setConfirmed(false);
              }}
              className="mt-4"
              autoFocus
            />

            {impactError && (
              <p className="mt-2 text-label-sm text-danger" role="alert">
                {impactError}
              </p>
            )}

            {needsConfirmation && confirmed && (
              <p className="mt-2 text-label-sm text-danger" role="alert">
                {account.username} has {appPasswordCount} app password
                {appPasswordCount === 1 ? "" : "s"} in use. Renaming will stop those devices
                syncing until the username is updated in each. The app passwords themselves stay
                valid.
              </p>
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
                type="submit"
                size="small"
                color={needsConfirmation && confirmed ? "danger" : "primary"}
                disabled={isUnchanged || !username.trim() || !impactShown}
                loading={isSubmitting}
              >
                {needsConfirmation && confirmed ? "Rename anyway" : "Rename"}
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
