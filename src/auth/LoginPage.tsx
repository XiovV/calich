import { useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { Field } from "@base-ui/react/field";
import { useAuthStore } from "../lib/authStore";
import { isValidEmail } from "../lib/isValidEmail";

export function LoginPage() {
  const session = useAuthStore((state) => state.session);
  const login = useAuthStore((state) => state.login);
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (session) {
    return <Navigate to="/" replace />;
  }

  const canSubmit =
    email.trim() !== "" && isValidEmail(email) && password.trim() !== "";

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmit || isSubmitting) return;

    setIsSubmitting(true);
    setError(null);
    try {
      await login(email, password);
      navigate("/", { replace: true });
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
        <h1 className="text-heading font-medium text-ink">Sign in</h1>
        <p className="mt-1 text-body text-ink-muted">
          Sign in to access your calendar.
        </p>

        <Field.Root className="mt-5">
          <Field.Label className="block text-label-sm text-ink-muted">
            Email
          </Field.Label>
          <Field.Control
            type="email"
            value={email}
            onChange={(domEvent) => setEmail(domEvent.target.value)}
            placeholder="you@example.com"
            className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
          />
        </Field.Root>

        <Field.Root className="mt-4">
          <Field.Label className="block text-label-sm text-ink-muted">
            Password
          </Field.Label>
          <Field.Control
            type="password"
            value={password}
            onChange={(domEvent) => setPassword(domEvent.target.value)}
            placeholder="••••••••"
            className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
          />
        </Field.Root>

        {error && (
          <p className="mt-3 text-label-sm text-calendar-tomato">{error}</p>
        )}

        <button
          type="submit"
          disabled={!canSubmit || isSubmitting}
          className="mt-5 w-full rounded-shell-sm bg-accent px-3 py-2 text-body text-ink-inverse hover:bg-accent-hover disabled:opacity-50"
        >
          {isSubmitting ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </div>
  );
}
