import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { Checkbox } from "../components/ui/Checkbox";

// The Settings page's reminder-delivery section (#68, ADR-0027): resolves
// the double-fire question between a server-side in-app Notification and a
// synced device's own pop-up from the same VALARM. Defaults off so web-only
// users are unaffected; AppPasswordsSection nudges the user here after they
// create their first app password.
export function ReminderDeliverySection() {
  const user = useAuthStore((state) => state.user);
  const updateSyncedDeviceReminders = useAuthStore((state) => state.updateSyncedDeviceReminders);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleChange(checked: boolean) {
    if (isSubmitting) return;

    setIsSubmitting(true);
    setError(null);
    try {
      await updateSyncedDeviceReminders(checked);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <section id="reminder-delivery" className="mt-8">
      <h2 className="text-heading font-medium text-ink">Reminder delivery</h2>
      <p className="mt-1 text-body text-ink-muted">
        A synced device fires its own pop-up for a reminder it already has. Turn this on once
        you've connected one to stop getting the same reminder twice.
      </p>

      <label className="mt-4 flex items-center gap-2 text-label-sm text-ink">
        <Checkbox
          checked={user?.syncedDeviceRemindersEnabled ?? false}
          onCheckedChange={handleChange}
          aria-label="Let my synced devices show reminder pop-ups"
        />
        Let my synced devices show reminder pop-ups (disable in-app reminder notifications)
      </label>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}
    </section>
  );
}
