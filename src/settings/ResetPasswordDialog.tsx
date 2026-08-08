import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { Input } from "../components/ui/Input";
import { useAsyncAction } from "../hooks/useAsyncAction";
import type { Account } from "../lib/accountsApi";

interface ResetPasswordDialogProps {
  account: Account;
  onReset: (password: string) => Promise<void>;
  onClose: () => void;
}

// The Reset password action from #120 (ADR-0037): an Admin sets a new
// temporary password for another User without knowing their current one.
// The forced change on next login is entirely a backend concern (the reset
// re-sets must_change_password) — this dialog only collects the value.
export function ResetPasswordDialog({ account, onReset, onClose }: ResetPasswordDialogProps) {
  const [password, setPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!password) return;

    await run(async () => {
      await onReset(password);
      onClose();
    });
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
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            Reset {account.username}'s password
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            {account.username} will be forced to change this temporary password on next login.
          </Dialog.Description>

          <form onSubmit={handleSubmit}>
            <Input
              label="Temporary password"
              type="password"
              value={password}
              onChange={(domEvent) => setPassword(domEvent.target.value)}
              className="mt-4"
              autoFocus
            />

            {error && (
              <p className="mt-2 text-label-sm text-danger" role="alert">
                {error}
              </p>
            )}

            <div className="mt-5 flex justify-end gap-2">
              <Dialog.Close
                className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}
              >
                Cancel
              </Dialog.Close>
              <Button type="submit" size="small" disabled={!password} loading={isSubmitting}>
                Reset password
              </Button>
            </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
