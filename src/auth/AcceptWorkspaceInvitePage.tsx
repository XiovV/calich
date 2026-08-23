import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { authApi } from "../lib/authApi";
import { errorMessage } from "../lib/errorMessage";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

const invalidInviteMessage = "This invite link is invalid or has expired.";

type Preview = { workspaceName: string; email: string; userExists: boolean };

// The public landing page for a Workspace Invite link (ADR-0044). Which of
// two accept paths it offers depends on the preview: an email with no
// existing account gets a name/password form that creates the account and
// joins the Workspace in one step; an email with an existing account is
// asked to log in (if not already) and then just confirms joining — no new
// account, no password step.
export function AcceptWorkspaceInvitePage() {
  const status = useAuthStore((state) => state.status);
  const acceptWorkspaceInvite = useAuthStore((state) => state.acceptWorkspaceInvite);
  const joinWorkspaceInvite = useAuthStore((state) => state.joinWorkspaceInvite);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token") ?? "";

  const [preview, setPreview] = useState<Preview | null>(null);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  // An empty token is invalid on its face — no fetch needed to know that,
  // so it's derived here rather than written into state from the Effect
  // below (https://react.dev/learn/you-might-not-need-an-effect).
  const previewError = token === "" ? invalidInviteMessage : fetchError;

  useEffect(() => {
    if (token === "") return;

    authApi
      .previewWorkspaceInvite(token)
      .then(setPreview)
      .catch((err: unknown) => setFetchError(errorMessage(err)));
  }, [token]);

  if (status === "loading") {
    return null;
  }

  if (previewError) {
    return (
      <CenteredCard>
        <h1 className="text-heading font-medium text-ink">Invite invalid</h1>
        <p className="mt-3 text-label-sm text-danger" role="alert">
          {previewError}
        </p>
      </CenteredCard>
    );
  }

  if (!preview) {
    return null;
  }

  // This invite's email already has an account: accepting it happens while
  // logged in as that User (ADR-0044) — an authenticated caller confirms
  // joining, anyone else is sent to log in first.
  if (preview.userExists) {
    if (status === "authenticated") {
      return (
        <CenteredCard>
          <h1 className="text-heading font-medium text-ink">Join {preview.workspaceName}</h1>
          <p className="mt-1 text-body text-ink-muted">
            You're signed in. Accept this invite to join {preview.workspaceName}.
          </p>

          {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

          <Button
            fullWidth
            loading={isSubmitting}
            className="mt-5"
            onClick={() =>
              run(async () => {
                await joinWorkspaceInvite(token);
                navigate("/", { replace: true });
              })
            }
          >
            {isSubmitting ? "Joining…" : `Join ${preview.workspaceName}`}
          </Button>
        </CenteredCard>
      );
    }

    return (
      <CenteredCard>
        <h1 className="text-heading font-medium text-ink">Join {preview.workspaceName}</h1>
        <p className="mt-1 text-body text-ink-muted">
          An account already exists for {preview.email}. Log in, then come back to this link to
          accept the invite.
        </p>
        <Link to="/login">
          <Button fullWidth className="mt-5">
            Log in
          </Button>
        </Link>
      </CenteredCard>
    );
  }

  // This invite's email has no account yet, but the caller is signed in as
  // somebody else's — creating a new account here would conflict with their
  // current session, so ask them to log out first rather than routing them
  // into either accept path.
  if (status === "authenticated") {
    return (
      <CenteredCard>
        <h1 className="text-heading font-medium text-ink">Join {preview.workspaceName}</h1>
        <p className="mt-1 text-body text-ink-muted">
          This invite is for {preview.email}, a new account. You're signed in as someone else —
          log out first to accept it.
        </p>
      </CenteredCard>
    );
  }

  const passwordsMatch = password !== "" && password === confirmPassword;
  const canSubmit = name.trim() !== "" && passwordsMatch;

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    await run(async () => {
      await acceptWorkspaceInvite(token, name, password);
      navigate("/", { replace: true });
    });
  }

  return (
    <CenteredCard>
      <form onSubmit={handleSubmit}>
        <h1 className="text-heading font-medium text-ink">Join {preview.workspaceName}</h1>
        <p className="mt-1 text-body text-ink-muted">
          Set up your account to join {preview.workspaceName}.
        </p>

        <Input label="Email" type="email" value={preview.email} disabled className="mt-5" />

        <Input
          label="Name"
          type="text"
          value={name}
          onChange={(domEvent) => setName(domEvent.target.value)}
          placeholder="Jane Smith"
          className="mt-4"
        />

        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(domEvent) => setPassword(domEvent.target.value)}
          className="mt-4"
        />
        {/* #247: the 8-character floor. #241: bcrypt's own 72-byte cap,
          hit well before 72 visible characters for anyone not typing plain
          ASCII — stated in bytes rather than characters for that reason. */}
        <p className="mt-1 text-label-sm text-ink-muted">
          At least 8 characters, up to 72 bytes.
        </p>

        <Input
          label="Confirm password"
          type="password"
          value={confirmPassword}
          onChange={(domEvent) => setConfirmPassword(domEvent.target.value)}
          invalid={confirmPassword !== "" && !passwordsMatch}
          className="mt-4"
        />
        {confirmPassword !== "" && !passwordsMatch && (
          <p className="mt-1 text-label-sm text-danger">Passwords don't match.</p>
        )}

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button type="submit" fullWidth disabled={!canSubmit} loading={isSubmitting} className="mt-5">
          {isSubmitting ? "Creating account…" : "Create account and join"}
        </Button>
      </form>
    </CenteredCard>
  );
}

function CenteredCard({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <div className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2">{children}</div>
    </div>
  );
}
