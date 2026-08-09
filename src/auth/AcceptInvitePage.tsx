import { useEffect, useState } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { authApi } from "../lib/authApi";
import { errorMessage } from "../lib/errorMessage";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

const invalidInviteMessage = "This invite link is invalid or has expired.";

// The public landing page for an Invite link (ADR-0042): choosing a password
// sets it, flips the account from Pending to Active, and logs the caller
// straight in — there is no separate "invite accepted, now please log in"
// step, matching ChangePasswordScreen's forced-change flow. The username is
// previewed on load (read-only, via AuthService.PreviewInvite) so the
// invitee can see which pre-chosen account they're activating before they
// commit to a password — the Admin picked the username, not them.
export function AcceptInvitePage() {
  const status = useAuthStore((state) => state.status);
  const acceptInvite = useAuthStore((state) => state.acceptInvite);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") ?? "";

  const [username, setUsername] = useState<string | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  useEffect(() => {
    if (token === "") {
      setPreviewError(invalidInviteMessage);
      return;
    }

    authApi
      .previewInvite(token)
      .then(setUsername)
      .catch((err: unknown) => setPreviewError(errorMessage(err)));
  }, [token]);

  if (status === "loading") {
    return null;
  }

  if (status === "authenticated") {
    return <Navigate to="/" replace />;
  }

  const passwordsMatch = password !== "" && password === confirmPassword;
  const canSubmit = username !== null && password.trim() !== "" && passwordsMatch;

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    await run(async () => {
      await acceptInvite(token, password);
      navigate("/", { replace: true });
    });
  }

  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <form
        onSubmit={handleSubmit}
        className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2"
      >
        <h1 className="text-heading font-medium text-ink">Set your password</h1>
        <p className="mt-1 text-body text-ink-muted">Set your password to get started.</p>

        {previewError && (
          <p className="mt-3 text-label-sm text-danger" role="alert">
            {previewError}
          </p>
        )}

        {username !== null && (
          <Input label="Username" type="text" value={username} disabled className="mt-5" />
        )}

        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(domEvent) => setPassword(domEvent.target.value)}
          disabled={username === null}
          className="mt-4"
        />

        <Input
          label="Confirm password"
          type="password"
          value={confirmPassword}
          onChange={(domEvent) => setConfirmPassword(domEvent.target.value)}
          invalid={confirmPassword !== "" && !passwordsMatch}
          disabled={username === null}
          className="mt-4"
        />
        {confirmPassword !== "" && !passwordsMatch && (
          <p className="mt-1 text-label-sm text-danger">Passwords don't match.</p>
        )}

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button
          type="submit"
          fullWidth
          disabled={!canSubmit}
          loading={isSubmitting}
          className="mt-5"
        >
          {isSubmitting ? "Setting password…" : "Set password and sign in"}
        </Button>
      </form>
    </div>
  );
}
