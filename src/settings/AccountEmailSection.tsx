import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

// The Settings page's account-email section (#57): the Email-Channel
// Reminder recipient (ADR-0021). The Email Channel only becomes selectable
// in the event modal once this is set *and* the self-hoster has SMTP
// configured — see reminderChannelOptions.
export function AccountEmailSection() {
  const user = useAuthStore((state) => state.user);
  const updateEmail = useAuthStore((state) => state.updateEmail);

  const [email, setEmail] = useState(user?.email ?? "");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const isUnchanged = email === (user?.email ?? "");

  async function handleSubmit(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isSubmitting || isUnchanged) return;

    setIsSubmitting(true);
    setError(null);
    setSaved(false);
    try {
      await updateEmail(email);
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Account email</h2>
      <p className="mt-1 text-body text-ink-muted">
        Used to deliver Email-channel Reminders.
      </p>

      <form onSubmit={handleSubmit} className="mt-4 flex items-end gap-2">
        <Input
          label="Email"
          type="email"
          value={email}
          onChange={(domEvent) => {
            setEmail(domEvent.target.value);
            setSaved(false);
          }}
          className="w-72"
        />
        <Button type="submit" disabled={isUnchanged} loading={isSubmitting}>
          Save
        </Button>
      </form>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
      {saved && !error && (
        <p className="mt-2 text-label-sm text-ink-muted">Saved.</p>
      )}
    </section>
  );
}
