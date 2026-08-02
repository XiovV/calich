import { useState } from "react";
import { Field } from "@base-ui/react/field";
import { useAuthStore } from "../lib/authStore";

export function ChangePasswordScreen() {
  const changePassword = useAuthStore((state) => state.changePassword);
  const pendingUsername = useAuthStore((state) => state.pendingUsername);

  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const passwordsMatch = newPassword !== "" && newPassword === confirmPassword;
  const canSubmit = newPassword.trim() !== "" && passwordsMatch;

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit || isSubmitting) return;

    setIsSubmitting(true);
    setError(null);
    try {
      // The current password isn't asked for: a forced change only ever
      // happens on the fixed, publicly documented bootstrap default, so the
      // backend doesn't require it back (see ADR-0010).
      await changePassword("", newPassword);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong.");
    } finally {
      setIsSubmitting(false);
    }
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

        <Field.Root className="mt-5">
          <Field.Label className="block text-label-sm text-ink-muted">
            New password
          </Field.Label>
          <Field.Control
            type="password"
            value={newPassword}
            onChange={(domEvent) => setNewPassword(domEvent.target.value)}
            className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
          />
        </Field.Root>

        <Field.Root className="mt-4">
          <Field.Label className="block text-label-sm text-ink-muted">
            Confirm new password
          </Field.Label>
          <Field.Control
            type="password"
            value={confirmPassword}
            onChange={(domEvent) => setConfirmPassword(domEvent.target.value)}
            className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
          />
          {confirmPassword !== "" && !passwordsMatch && (
            <p className="mt-1 text-label-sm text-calendar-tomato">
              Passwords don't match.
            </p>
          )}
        </Field.Root>

        {error && (
          <p className="mt-3 text-label-sm text-calendar-tomato">{error}</p>
        )}

        <button
          type="submit"
          disabled={!canSubmit || isSubmitting}
          className="mt-5 w-full rounded-shell-sm bg-accent px-3 py-2 text-body text-ink-inverse hover:bg-accent-hover disabled:opacity-50"
        >
          {isSubmitting ? "Updating…" : "Update password"}
        </button>
      </form>
    </div>
  );
}
