import { useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

export function LoginPage() {
  const status = useAuthStore((state) => state.status);
  const login = useAuthStore((state) => state.login);
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  if (status === "loading") {
    return null;
  }

  if (status === "authenticated" || status === "must-change-password" || status === "account-disabled") {
    return <Navigate to="/" replace />;
  }

  // A fresh instance with no accounts has nothing to sign in to (#169,
  // ADR-0047) — send a direct visit to /login straight to Register instead.
  if (status === "needs-setup") {
    return <Navigate to="/register" replace />;
  }

  const canSubmit = email.trim() !== "" && password.trim() !== "";

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    await run(async () => {
      await login(email, password);
      navigate("/", { replace: true });
    });
  }

  return (
    <div className="flex h-screen items-center justify-center bg-surface-sunken">
      <form
        onSubmit={handleSubmit}
        className="w-80 rounded-shell-lg bg-surface p-6 shadow-elevation-2"
      >
        <h1 className="text-heading font-medium text-ink">Sign in</h1>
        <p className="mt-1 text-body text-ink-muted">
          Sign in to access your calendar.
        </p>

        <Input
          label="Email"
          type="email"
          value={email}
          onChange={(domEvent) => setEmail(domEvent.target.value)}
          placeholder="you@example.com"
          className="mt-5"
        />

        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(domEvent) => setPassword(domEvent.target.value)}
          placeholder="••••••••"
          className="mt-4"
        />

        {error && <p className="mt-3 text-label-sm text-danger">{error}</p>}

        <Button
          type="submit"
          fullWidth
          disabled={!canSubmit}
          loading={isSubmitting}
          className="mt-5"
        >
          {isSubmitting ? "Signing in…" : "Sign in"}
        </Button>

        <p className="mt-4 text-label-sm text-ink-muted">
          No account yet? <Link to="/register" className="text-accent">Create one</Link>
        </p>
      </form>
    </div>
  );
}
