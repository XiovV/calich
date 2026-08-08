import { useEffect, useState } from "react";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { useAccountsStore } from "../lib/accountsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { toast } from "../lib/toast";
import { DeleteAccountDialog } from "./DeleteAccountDialog";
import { ResetPasswordDialog } from "./ResetPasswordDialog";
import type { Account } from "../lib/accountsApi";

function statusLabels(account: Account): string[] {
  const labels: string[] = [];
  if (account.isAdmin) labels.push("Admin");
  if (account.isDisabled) labels.push("Disabled");
  if (account.mustChangePassword) labels.push("Password change pending");
  return labels;
}

// The Settings page's Accounts section (#119/#120/#121, ADR-0037): visible
// only to an Admin, per getSettingsSections. Lists every account on the
// instance, lets an Admin create a new one, and manages an existing one's
// lifecycle — reset its password, grant or revoke Admin, disable or
// re-enable it, and delete it with an explicit disposition for the
// Calendars it owned. The last-remaining-Admin guards are enforced
// server-side; this section just surfaces the explanation the server sends
// back.
export function AccountsSection() {
  const accounts = useAccountsStore((state) => state.accounts);
  const fetchAccounts = useAccountsStore((state) => state.fetchAccounts);
  const createAccount = useAccountsStore((state) => state.createAccount);
  const resetPassword = useAccountsStore((state) => state.resetPassword);
  const setAdmin = useAccountsStore((state) => state.setAdmin);
  const setDisabled = useAccountsStore((state) => state.setDisabled);
  const deleteAccount = useAccountsStore((state) => state.deleteAccount);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { isSubmitting, error, setError, run } = useAsyncAction();

  const [resettingAccount, setResettingAccount] = useState<Account | null>(null);
  const [deletingAccount, setDeletingAccount] = useState<Account | null>(null);
  const [busyAccountId, setBusyAccountId] = useState<number | null>(null);

  useEffect(() => {
    fetchAccounts().catch((err) => {
      setError(errorMessage(err));
    });
  }, [fetchAccounts, setError]);

  async function handleCreate(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!username.trim() || !password) return;

    await run(async () => {
      await createAccount(username.trim(), password);
      setUsername("");
      setPassword("");
    });
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
        temporary password on first login.
      </p>

      <form onSubmit={handleCreate} className="mt-4 flex items-end gap-2">
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
    </section>
  );
}
