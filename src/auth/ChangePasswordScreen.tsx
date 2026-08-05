import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

export function ChangePasswordScreen() {
  const changePassword = useAuthStore((state) => state.changePassword);
  const pendingUsername = useAuthStore((state) => state.pendingUsername);

  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  const passwordsMatch = newPassword !== "" && newPassword === confirmPassword;
  const canSubmit = newPassword.trim() !== "" && passwordsMatch;

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    // The current password isn't asked for: a forced change only ever happens
    // on the fixed, publicly documented bootstrap default, so the backend
    // doesn't require it back (see ADR-0010).
    await run(() => changePassword("", newPassword));
  }

  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <form
        onSubmit={handleSubmit}
        className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2"
      >
        <h1 className="text-heading font-medium text-ink">
          Change your password
        </h1>
        <p className="mt-1 text-body text-ink-muted">
          {pendingUsername ? `Signed in as ${pendingUsername}. ` : ""}
          You must set a new password before continuing.
        </p>

        <Input
          label="New password"
          type="password"
          value={newPassword}
          onChange={(domEvent) => setNewPassword(domEvent.target.value)}
          className="mt-5"
        />

        <Input
          label="Confirm new password"
          type="password"
          value={confirmPassword}
          onChange={(domEvent) => setConfirmPassword(domEvent.target.value)}
          invalid={confirmPassword !== "" && !passwordsMatch}
          className="mt-4"
        />
        {confirmPassword !== "" && !passwordsMatch && (
          <p className="mt-1 text-label-sm text-danger">
            Passwords don't match.
          </p>
        )}

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button
          type="submit"
          fullWidth
          disabled={!canSubmit}
          loading={isSubmitting}
          className="mt-5"
        >
          {isSubmitting ? "Updating…" : "Update password"}
        </Button>
      </form>
    </div>
  );
}
