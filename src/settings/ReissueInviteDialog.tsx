import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Button } from "../components/ui/Button";
import { buttonClasses } from "../components/ui/buttonClasses";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useWorkspaceMembersStore } from "../lib/workspaceMembersStore";
import { buildWorkspaceInviteLink } from "../lib/workspaceInviteLink";
import { InviteLinkReveal } from "./InviteLinkReveal";
import type { OutstandingWorkspaceInvite } from "../lib/workspaceInvitesApi";

interface ReissueInviteDialogProps {
  invite: OutstandingWorkspaceInvite;
  onClose: () => void;
}

// Replaces invite's outstanding token with a fresh one and a reset 7-day
// expiry (ADR-0044, #165). The Owner/Admin must explicitly confirm before
// this fires, since it invalidates any link already sent — the new link is
// only generated on confirm, then shown once, same as CreateInvite's
// response.
export function ReissueInviteDialog({ invite, onClose }: ReissueInviteDialogProps) {
  const reissueInvite = useWorkspaceMembersStore((state) => state.reissueInvite);
  const [reissuedToken, setReissuedToken] = useState<string | null>(null);
  const { isSubmitting, error, run } = useAsyncAction();

  async function handleReissue() {
    await run(async () => {
      const result = await reissueInvite(invite.id);
      setReissuedToken(result.token);
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
        <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            Reissue the invite to {invite.email}
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-body text-ink-muted">
            {reissuedToken
              ? "The previous invite link no longer works."
              : "This replaces the outstanding invite with a new link and resets its 7-day expiry. Any link already sent stops working."}
          </Dialog.Description>

          {reissuedToken && (
            <InviteLinkReveal className="mt-4" link={buildWorkspaceInviteLink(reissuedToken)} />
          )}

          {error && (
            <p className="mt-2 text-label-sm text-danger" role="alert">
              {error}
            </p>
          )}

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close className={buttonClasses({ variant: "outline", color: "secondary", size: "small" })}>
              {reissuedToken ? "Done" : "Cancel"}
            </Dialog.Close>
            {!reissuedToken && (
              <Button size="small" loading={isSubmitting} onClick={handleReissue}>
                Reissue invite
              </Button>
            )}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
