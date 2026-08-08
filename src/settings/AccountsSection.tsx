import { useEffect, useState } from "react";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { useAccountsStore } from "../lib/accountsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import type { Account } from "../lib/accountsApi";

function statusLabels(account: Account): string[] {
  const labels: string[] = [];
  if (account.isAdmin) labels.push("Admin");
  if (account.isDisabled) labels.push("Disabled");
  if (account.mustChangePassword) labels.push("Password change pending");
  return labels;
}

// The Settings page's Accounts section (#119, ADR-0037): visible only to an
// Admin, per getSettingsSections. Lists every account on the instance and
// lets an Admin create a new one with a username and a temporary password.
// Reset password, promotion/demotion, disabling and deletion are #120 and
// #121.
export function AccountsSection() {
  const accounts = useAccountsStore((state) => state.accounts);
  const fetchAccounts = useAccountsStore((state) => state.fetchAccounts);
  const createAccount = useAccountsStore((state) => state.createAccount);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { isSubmitting, error, setError, run } = useAsyncAction();

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
          return (
            <li
              key={account.id}
              className="flex items-center justify-between rounded-md border border-border px-3 py-2"
            >
              <span className="text-body text-ink">{account.username}</span>
              {labels.length > 0 && (
                <span className="flex gap-2">
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
            </li>
          );
        })}
        {accounts.length === 0 && (
          <p className="text-label-sm text-ink-muted">No accounts yet.</p>
        )}
      </ul>
    </section>
  );
}
