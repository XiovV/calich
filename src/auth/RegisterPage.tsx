import { useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { hasMinPasswordLength } from "../lib/passwordPolicy";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

// The first-run bootstrap form and self-registration page (ADR-0044): the
// very first account on the instance always succeeds, and any later one
// only while ENABLE_SIGNUPS is true. The server is still the authority that
// enforces this (registerErrors' ErrSignupsDisabled) regardless of what this
// page does — but #235 wants the closed case to look closed rather than
// asking a User to fill in a form that only fails at the very end, so this
// page mirrors that gate client-side too (via signupsEnabled) rather than
// just surfacing the server's rejection after the fact.
export function RegisterPage() {
  const status = useAuthStore((state) => state.status);
  const signupsEnabled = useAuthStore((state) => state.signupsEnabled);
  const register = useAuthStore((state) => state.register);
  const navigate = useNavigate();

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  if (status === "loading") {
    return null;
  }

  if (status === "authenticated" || status === "must-change-password") {
    return <Navigate to="/" replace />;
  }

  // "needs-setup" is the very first account, which Register always allows
  // regardless of ENABLE_SIGNUPS (ADR-0044) — only a later visit, once an
  // account already exists, is gated on signupsEnabled (#235).
  if (status !== "needs-setup" && !signupsEnabled) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-sunken">
        <div className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2">
          <h1 className="text-heading font-medium text-ink">Registration is closed</h1>
          <p className="mt-1 text-body text-ink-muted">
            This instance isn't accepting new accounts. Ask an existing member for a Workspace
            invite, or sign in if you already have an account.
          </p>

          <p className="mt-4 text-label-sm text-ink-muted">
            <Link to="/login" className="text-accent-ink">Sign in</Link>
          </p>
        </div>
      </div>
    );
  }

  // hasMinPasswordLength mirrors the hint text below the field client-side
  // (#255) so a too-short password is caught before the round trip that
  // already catches it. password.trim() !== "" stays alongside it — an
  // all-whitespace password can be 8+ code points long and still fail the
  // server's isVisibleRune check (#251).
  const canSubmit =
    name.trim() !== "" &&
    email.trim() !== "" &&
    password.trim() !== "" &&
    hasMinPasswordLength(password);

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    await run(async () => {
      await register(name, email, password);
      navigate("/", { replace: true });
    });
  }

  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <form
        onSubmit={handleSubmit}
        className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2"
      >
        <h1 className="text-heading font-medium text-ink">Create your account</h1>
        <p className="mt-1 text-body text-ink-muted">
          Set up your account and workspace.
        </p>

        <Input
          label="Name"
          type="text"
          value={name}
          onChange={(domEvent) => setName(domEvent.target.value)}
          placeholder="Jane Smith"
          className="mt-5"
        />

        <Input
          label="Email"
          type="email"
          value={email}
          onChange={(domEvent) => setEmail(domEvent.target.value)}
          placeholder="jane@example.com"
          className="mt-4"
        />

        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(domEvent) => setPassword(domEvent.target.value)}
          placeholder="••••••••"
          className="mt-4"
        />
        {/* #247's 8-character floor only. bcrypt's 72-byte cap (#241) is
          deliberately not stated here: it's a limit in a unit nobody can
          count, and restating it as "72 characters" would be a lie for
          anyone not typing plain ASCII — CJK hits it at 24. It's enforced
          server-side and surfaces as a rejection if anyone reaches it. */}
        <p className="mt-1 text-label-sm text-ink-muted">At least 8 characters.</p>

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button
          type="submit"
          fullWidth
          disabled={!canSubmit}
          loading={isSubmitting}
          className="mt-5"
        >
          {isSubmitting ? "Creating account…" : "Create account"}
        </Button>

        <p className="mt-4 text-label-sm text-ink-muted">
          Already have an account? <Link to="/login" className="text-accent-ink">Sign in</Link>
        </p>
      </form>
    </div>
  );
}
