import { useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

export function LoginPage() {
  const status = useAuthStore((state) => state.status);
  const login = useAuthStore((state) => state.login);
  const navigate = useNavigate();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { isSubmitting, error, run } = useAsyncAction();

  if (status === "loading") {
    return null;
  }

  if (status === "authenticated" || status === "must-change-password") {
    return <Navigate to="/" replace />;
  }

  const canSubmit = username.trim() !== "" && password.trim() !== "";

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit) return;

    await run(async () => {
      await login(username, password);
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
          label="Username"
          type="text"
          value={username}
          onChange={(domEvent) => setUsername(domEvent.target.value)}
          placeholder="admin"
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
      </form>
    </div>
  );
}
