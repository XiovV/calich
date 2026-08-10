import { useState } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

// The first-run bootstrap form and self-registration page (ADR-0044): the
// server decides whether the request is allowed — the very first account on
// the instance always succeeds, and any later one only while ENABLE_SIGNUPS
// is true — so this form doesn't need to know which case it's in, only show
// the "self-registration is disabled" error the server returns otherwise.
export function RegisterPage() {
  const status = useAuthStore((state) => state.status);
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

  const canSubmit = name.trim() !== "" && email.trim() !== "" && password.trim() !== "";

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
          Already have an account? <Link to="/login" className="text-accent">Sign in</Link>
        </p>
      </form>
    </div>
  );
}
