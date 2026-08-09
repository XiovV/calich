import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";

// The one screen a Disabled User can reach (ADR-0044): with no
// instance-wide Admin left to re-activate someone else's account, Login
// still issues a Session for a Disabled account so its owner can get back
// here and undo it themselves.
export function ReactivateAccountScreen() {
  const reactivateAccount = useAuthStore((state) => state.reactivateAccount);
  const logout = useAuthStore((state) => state.logout);
  const { isSubmitting, error, run } = useAsyncAction();

  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <div className="w-96 rounded-shell-lg bg-surface p-6 shadow-elevation-2">
        <h1 className="text-heading font-medium text-ink">Your account is disabled</h1>
        <p className="mt-1 text-body text-ink-muted">
          You disabled this account yourself. Re-activate it to get back to your calendar.
        </p>

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button
          type="button"
          fullWidth
          loading={isSubmitting}
          className="mt-5"
          onClick={() => run(() => reactivateAccount())}
        >
          {isSubmitting ? "Re-activating…" : "Re-activate account"}
        </Button>

        <button
          type="button"
          className="mt-3 w-full text-center text-label-sm text-ink-muted underline"
          onClick={() => logout()}
        >
          Sign out
        </button>
      </div>
    </div>
  );
}
