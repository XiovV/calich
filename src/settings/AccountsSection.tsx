import { useEffect, useState } from "react";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { useAccountsStore } from "../lib/accountsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { buildInviteLink } from "../lib/inviteLink";
import { toast } from "../lib/toast";
import { CancelInviteDialog } from "./CancelInviteDialog";
import { DeleteAccountDialog } from "./DeleteAccountDialog";
import { InviteLinkReveal } from "./InviteLinkReveal";
import { ReissueInviteDialog } from "./ReissueInviteDialog";
import { RenameAccountDialog } from "./RenameAccountDialog";
import { ResetPasswordDialog } from "./ResetPasswordDialog";
import type { Account, InviteResult } from "../lib/accountsApi";

// Pending/Expired are a state of their own, not a flavor of Disabled
// (ADR-0042) — the glossary term is "Pending", not "invited".
function inviteStatusLabel(account: Account): "Pending" | "Expired" | null {
  if (!account.isPending) return null;
  if (account.inviteExpiresAt && new Date(account.inviteExpiresAt).getTime() <= Date.now()) {
    return "Expired";
  }
  return "Pending";
}

function statusLabels(account: Account): string[] {
  const labels: string[] = [];
  const inviteLabel = inviteStatusLabel(account);
  if (inviteLabel) labels.push(inviteLabel);
  if (account.isAdmin) labels.push("Admin");
  if (account.isDisabled) labels.push("Disabled");
  if (account.mustChangePassword) labels.push("Password change pending");
  return labels;
}

type AddUserMode = "password" | "invite";

// The Settings page's Accounts section (#119/#120/#121/#145, ADR-0037,
// ADR-0042): visible only to an Admin, per getSettingsSections. Lists every
// account on the instance — Active, Disabled, and Pending side by side — lets
// an Admin add a new one either with a temporary password or by Invite, and
// manages an existing one's lifecycle. The last-remaining-Admin guards are
// enforced server-side; this section just surfaces the explanation the
// server sends back.
export function AccountsSection() {
  const accounts = useAccountsStore((state) => state.accounts);
  const fetchAccounts = useAccountsStore((state) => state.fetchAccounts);
  const createAccount = useAccountsStore((state) => state.createAccount);
  const createInvite = useAccountsStore((state) => state.createInvite);
  const reissueInvite = useAccountsStore((state) => state.reissueInvite);
  const sendInviteEmail = useAccountsStore((state) => state.sendInviteEmail);
  const cancelInvite = useAccountsStore((state) => state.cancelInvite);
  const resetPassword = useAccountsStore((state) => state.resetPassword);
  const setAdmin = useAccountsStore((state) => state.setAdmin);
  const setDisabled = useAccountsStore((state) => state.setDisabled);
  const renameAccount = useAccountsStore((state) => state.renameAccount);
  const deleteAccount = useAccountsStore((state) => state.deleteAccount);

  const [addUserMode, setAddUserMode] = useState<AddUserMode>("password");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteEmail, setInviteEmail] = useState("");
  const [invited, setInvited] = useState<InviteResult | null>(null);
  const { isSubmitting, error, setError, run } = useAsyncAction();

  const [resettingAccount, setResettingAccount] = useState<Account | null>(null);
  const [renamingAccount, setRenamingAccount] = useState<Account | null>(null);
  const [deletingAccount, setDeletingAccount] = useState<Account | null>(null);
  const [reissuingAccount, setReissuingAccount] = useState<Account | null>(null);
  const [cancelingInvite, setCancelingInvite] = useState<Account | null>(null);
  const [busyAccountId, setBusyAccountId] = useState<number | null>(null);

  useEffect(() => {
    fetchAccounts().catch((err) => {
      setError(errorMessage(err));
    });
  }, [fetchAccounts, setError]);

  function switchMode(mode: AddUserMode) {
    setAddUserMode(mode);
    setError(null);
    setInvited(null);
  }

  async function handleCreate(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!username.trim() || !password) return;

    await run(async () => {
      await createAccount(username.trim(), password);
      setUsername("");
      setPassword("");
    });
  }

  async function handleSendInvite(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!username.trim()) return;

    await run(async () => {
      const result = await createInvite(username.trim(), inviteEmail.trim());
      setInvited(result);
      setUsername("");
      setInviteEmail("");
    });
  }

  // A standalone "send email" row action needs a live token to build a link
  // from, and the plaintext token is never retrievable once CreateInvite's
  // response is handled (ADR-0042) — so this reissues in the background
  // (invalidating any link already distributed) before emailing the fresh
  // one, rather than exposing that link to the Admin as a copy step.
  async function handleSendInviteEmail(account: Account) {
    setBusyAccountId(account.id);
    try {
      const result = await reissueInvite(account.id);
      await sendInviteEmail(result.account.id, buildInviteLink(result.token));
      toast.success("Invite email sent.");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusyAccountId(null);
    }
  }

  async function handleToggleAdmin(account: Account) {
    setBusyAccountId(account.id);
    try {
      await setAdmin(account.id, !account.isAdmin);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusyAccountId(null);
    }
  }

  async function handleToggleDisabled(account: Account) {
    setBusyAccountId(account.id);
    try {
      await setDisabled(account.id, !account.isDisabled);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusyAccountId(null);
    }
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Accounts</h2>
      <p className="mt-1 text-body text-ink-muted">
        Create and review accounts on this instance. A new account is forced to change its
        temporary password on first login, unless it was invited instead.
      </p>

      <div className="mt-4 flex gap-2">
        <Button
          variant={addUserMode === "password" ? "filled" : "outline"}
          color="secondary"
          size="small"
          onClick={() => switchMode("password")}
        >
          Create with temp password
        </Button>
        <Button
          variant={addUserMode === "invite" ? "filled" : "outline"}
          color="secondary"
          size="small"
          onClick={() => switchMode("invite")}
        >
          Send an invite
        </Button>
      </div>

      {addUserMode === "password" ? (
        <form onSubmit={handleCreate} className="mt-3 flex items-end gap-2">
          <Input
            label="Username"
            value={username}
            onChange={(domEvent) => setUsername(domEvent.target.value)}
            className="w-56"
          />
          <Input
            label="Temporary password"
            type="password"
            value={password}
            onChange={(domEvent) => setPassword(domEvent.target.value)}
            className="w-56"
          />
          <Button type="submit" disabled={!username.trim() || !password} loading={isSubmitting}>
            Create account
          </Button>
        </form>
      ) : (
        <form onSubmit={handleSendInvite} className="mt-3 flex items-end gap-2">
          <Input
            label="Username"
            value={username}
            onChange={(domEvent) => setUsername(domEvent.target.value)}
            className="w-56"
          />
          <Input
            label="Email (optional)"
            type="email"
            value={inviteEmail}
            onChange={(domEvent) => setInviteEmail(domEvent.target.value)}
            className="w-56"
          />
          <Button type="submit" disabled={!username.trim()} loading={isSubmitting}>
            Send invite
          </Button>
        </form>
      )}

      {addUserMode === "invite" && invited && (
        <InviteLinkReveal
          className="mt-3"
          link={buildInviteLink(invited.token)}
          emailAvailable={invited.account.inviteEmailAvailable}
          onSendEmail={() => sendInviteEmail(invited.account.id, buildInviteLink(invited.token))}
        />
      )}

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}

      <ul className="mt-4 flex flex-col gap-2">
        {accounts.map((account) => {
          const labels = statusLabels(account);
          const isBusy = busyAccountId === account.id;
          return (
            <li
              key={account.id}
              className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2"
            >
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-body text-ink">{account.username}</span>
                {labels.length > 0 && (
                  <span className="flex shrink-0 gap-2">
                    {labels.map((label) => (
                      <span
                        key={label}
                        className="rounded-full bg-surface-raised px-2 py-0.5 text-label-sm text-ink-muted"
                      >
                        {label}
                      </span>
                    ))}
                  </span>
                )}
              </div>

              <div className="flex shrink-0 items-center gap-2">
                {account.isPending ? (
                  <>
                    <Button
                      variant="outline"
                      color="secondary"
                      size="small"
                      disabled={isBusy}
                      onClick={() => setReissuingAccount(account)}
                    >
                      Reissue invite
                    </Button>
                    {account.inviteEmailAvailable && (
                      <Button
                        variant="outline"
                        color="secondary"
                        size="small"
                        disabled={isBusy}
                        onClick={() => handleSendInviteEmail(account)}
                      >
                        Send invite email
                      </Button>
                    )}
                    <Button
                      variant="outline"
                      color="danger"
                      size="small"
                      disabled={isBusy}
                      onClick={() => setCancelingInvite(account)}
                    >
                      Cancel invite
                    </Button>
                  </>
                ) : (
                  <>
                    <Button
                      variant="outline"
                      color="secondary"
                      size="small"
                      disabled={isBusy}
                      onClick={() => setResettingAccount(account)}
                    >
                      Reset password
                    </Button>
                    <Button
                      variant="outline"
                      color="secondary"
                      size="small"
                      disabled={isBusy}
                      onClick={() => setRenamingAccount(account)}
                    >
                      Rename
                    </Button>
                    <Button
                      variant="outline"
                      color="secondary"
                      size="small"
                      disabled={isBusy}
                      onClick={() => handleToggleAdmin(account)}
                    >
                      {account.isAdmin ? "Remove admin" : "Make admin"}
                    </Button>
                    <Button
                      variant="outline"
                      color={account.isDisabled ? "secondary" : "danger"}
                      size="small"
                      disabled={isBusy}
                      onClick={() => handleToggleDisabled(account)}
                    >
                      {account.isDisabled ? "Enable" : "Disable"}
                    </Button>
                    <Button
                      variant="outline"
                      color="danger"
                      size="small"
                      disabled={isBusy}
                      onClick={() => setDeletingAccount(account)}
                    >
                      Delete
                    </Button>
                  </>
                )}
              </div>
            </li>
          );
        })}
        {accounts.length === 0 && (
          <p className="text-label-sm text-ink-muted">No accounts yet.</p>
        )}
      </ul>

      {resettingAccount && (
        <ResetPasswordDialog
          account={resettingAccount}
          onReset={(newPassword) => resetPassword(resettingAccount.id, newPassword)}
          onClose={() => setResettingAccount(null)}
        />
      )}

      {renamingAccount && (
        <RenameAccountDialog
          account={renamingAccount}
          onRename={(newUsername) => renameAccount(renamingAccount.id, newUsername)}
          onClose={() => setRenamingAccount(null)}
        />
      )}

      {deletingAccount && (
        <DeleteAccountDialog
          account={deletingAccount}
          transferCandidates={accounts.filter((a) => a.id !== deletingAccount.id)}
          onDelete={(disposition, transferTo) =>
            deleteAccount(deletingAccount.id, disposition, transferTo)
          }
          onClose={() => setDeletingAccount(null)}
        />
      )}

      {reissuingAccount && (
        <ReissueInviteDialog account={reissuingAccount} onClose={() => setReissuingAccount(null)} />
      )}

      {cancelingInvite && (
        <CancelInviteDialog
          account={cancelingInvite}
          onCancel={() => cancelInvite(cancelingInvite.id)}
          onClose={() => setCancelingInvite(null)}
        />
      )}
    </section>
  );
}
