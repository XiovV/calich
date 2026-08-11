import { useEffect, useState } from "react";
import { Mail, RotateCw, Shield, ShieldOff, UserX, X } from "lucide-react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { Input } from "../components/ui/Input";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { useAuthStore } from "../lib/authStore";
import { useWorkspacesStore } from "../lib/workspacesStore";
import { useWorkspaceMembersStore } from "../lib/workspaceMembersStore";
import type { OutstandingWorkspaceInvite } from "../lib/workspaceInvitesApi";
import type { WorkspaceMember } from "../lib/workspaceMembersApi";
import { InviteLinkReveal } from "./InviteLinkReveal";
import { buildWorkspaceInviteLink } from "../lib/workspaceInviteLink";
import { RemoveMemberDialog } from "./RemoveMemberDialog";
import { CancelInviteDialog } from "./CancelInviteDialog";
import { ReissueInviteDialog } from "./ReissueInviteDialog";

function isExpired(invite: OutstandingWorkspaceInvite): boolean {
  return new Date(invite.inviteExpiresAt).getTime() < Date.now();
}

// The Members settings screen (#165, #190): the Owner or Admin manages
// membership — sending an invite, seeing outstanding invites alongside
// active Members, promoting/demoting Admin, and removing a Member, with the
// transfer-or-delete Calendar disposition flow (#160) surfaced before a
// removal completes. Mirrors the shape of the retired instance-wide Admin
// account list (#145), scoped to the currently active Workspace (#153)
// instead of the whole instance.
export function MembersSection() {
  const user = useAuthStore((state) => state.user);
  const activeWorkspaceId = useWorkspacesStore((state) => state.activeWorkspaceId);

  const members = useWorkspaceMembersStore((state) => state.members);
  const invites = useWorkspaceMembersStore((state) => state.invites);
  const fetchAll = useWorkspaceMembersStore((state) => state.fetchAll);
  const setRole = useWorkspaceMembersStore((state) => state.setRole);
  const createInvite = useWorkspaceMembersStore((state) => state.createInvite);
  const cancelInvite = useWorkspaceMembersStore((state) => state.cancelInvite);

  const [loadError, setLoadError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [revealedInvite, setRevealedInvite] = useState<{ email: string; token: string } | null>(null);
  const [busyUserId, setBusyUserId] = useState<number | null>(null);
  const [removeTarget, setRemoveTarget] = useState<WorkspaceMember | null>(null);
  const [cancelTarget, setCancelTarget] = useState<OutstandingWorkspaceInvite | null>(null);
  const [reissueTarget, setReissueTarget] = useState<OutstandingWorkspaceInvite | null>(null);
  const { isSubmitting: isInviting, error: inviteError, setError: setInviteError, run: runInvite } =
    useAsyncAction();

  useEffect(() => {
    if (activeWorkspaceId === null) return;
    fetchAll()
      .then(() => setLoadError(null))
      .catch((err) => setLoadError(errorMessage(err)));
  }, [activeWorkspaceId, fetchAll]);

  const self = members.find((m) => m.userId === user?.id) ?? null;
  const canManage = self?.role === "owner" || self?.role === "admin";
  const isOwner = self?.role === "owner";

  async function handleInvite(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!email.trim()) return;

    await runInvite(async () => {
      const created = await createInvite(email.trim());
      setRevealedInvite({ email: created.email, token: created.token });
      setEmail("");
    });
  }

  async function handlePromote(member: WorkspaceMember) {
    setBusyUserId(member.userId);
    try {
      await setRole(member.userId, "admin");
    } catch (err) {
      setInviteError(errorMessage(err));
    } finally {
      setBusyUserId(null);
    }
  }

  async function handleDemote(member: WorkspaceMember) {
    setBusyUserId(member.userId);
    try {
      await setRole(member.userId, "member");
    } catch (err) {
      setInviteError(errorMessage(err));
    } finally {
      setBusyUserId(null);
    }
  }

  function canRemove(member: WorkspaceMember): boolean {
    if (!canManage) return false;
    if (member.role === "owner") return false;
    if (self?.role === "admin" && member.role === "admin") return false;
    return true;
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Members</h2>
      <p className="mt-1 text-body text-ink-muted">
        Manage who belongs to this workspace, their role, and outstanding invites.
      </p>

      {loadError && <p className="mt-2 text-label-sm text-danger">{loadError}</p>}

      {canManage && (
        <>
          <form onSubmit={handleInvite} className="mt-4 flex items-end gap-2">
            <Input
              label="Invite by email"
              type="email"
              placeholder="teammate@example.com"
              value={email}
              onChange={(domEvent) => setEmail(domEvent.target.value)}
              className="w-72"
            />
            <Button type="submit" disabled={!email.trim()} loading={isInviting} leadingIcon={<Mail className="size-4" />}>
              Invite
            </Button>
          </form>

          {inviteError && <p className="mt-2 text-label-sm text-danger">{inviteError}</p>}

          {revealedInvite && (
            <div className="mt-4">
              <p className="text-label-sm text-ink-muted">Invite link for {revealedInvite.email}:</p>
              <InviteLinkReveal className="mt-2" link={buildWorkspaceInviteLink(revealedInvite.token)} />
            </div>
          )}
        </>
      )}

      <ul className="mt-4 flex flex-col gap-2">
        {members.map((member) => {
          const isSelf = member.userId === user?.id;
          return (
            <li
              key={`member-${member.userId}`}
              className="flex items-center justify-between rounded-md border border-border px-3 py-2"
            >
              <div>
                <p className="text-body text-ink">
                  {member.name}
                  {isSelf && " (you)"}
                </p>
                <p className="text-label-sm text-ink-muted capitalize">{member.role}</p>
              </div>
              <div className="flex items-center gap-1">
                {isOwner && !isSelf && member.role !== "owner" && (
                  <IconButton
                    onClick={() => (member.role === "admin" ? handleDemote(member) : handlePromote(member))}
                    disabled={busyUserId === member.userId}
                    aria-label={member.role === "admin" ? `Revoke admin from ${member.name}` : `Make ${member.name} an admin`}
                  >
                    {member.role === "admin" ? <ShieldOff className="size-4" /> : <Shield className="size-4" />}
                  </IconButton>
                )}
                {canRemove(member) && (
                  <IconButton
                    onClick={() => setRemoveTarget(member)}
                    disabled={busyUserId === member.userId}
                    aria-label={`Remove ${member.name}`}
                  >
                    <UserX className="size-4" />
                  </IconButton>
                )}
              </div>
            </li>
          );
        })}

        {invites.map((invite) => (
          <li
            key={`invite-${invite.id}`}
            className="flex items-center justify-between rounded-md border border-border border-dashed px-3 py-2"
          >
            <div>
              <p className="text-body text-ink">{invite.email}</p>
              <p className="text-label-sm text-ink-muted">{isExpired(invite) ? "Expired" : "Invited"}</p>
            </div>
            {canManage && (
              <div className="flex items-center gap-1">
                <IconButton onClick={() => setReissueTarget(invite)} aria-label={`Resend invite to ${invite.email}`}>
                  <RotateCw className="size-4" />
                </IconButton>
                <IconButton onClick={() => setCancelTarget(invite)} aria-label={`Cancel invite to ${invite.email}`}>
                  <X className="size-4" />
                </IconButton>
              </div>
            )}
          </li>
        ))}

        {members.length === 0 && invites.length === 0 && !loadError && (
          <p className="text-label-sm text-ink-muted">Loading…</p>
        )}
      </ul>

      {removeTarget && <RemoveMemberDialog member={removeTarget} onClose={() => setRemoveTarget(null)} />}
      {cancelTarget && (
        <CancelInviteDialog
          invite={cancelTarget}
          onCancel={() => cancelInvite(cancelTarget.id)}
          onClose={() => setCancelTarget(null)}
        />
      )}
      {reissueTarget && <ReissueInviteDialog invite={reissueTarget} onClose={() => setReissueTarget(null)} />}
    </section>
  );
}
