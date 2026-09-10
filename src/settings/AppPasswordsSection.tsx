import { Check, Copy, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { Input } from "../components/ui/Input";
import { useAppPasswordsStore } from "../lib/appPasswordsStore";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { useCalendarsStore } from "../lib/calendarsStore";
import { canManageCalendar, isLinkedCalendar } from "../lib/calendar";

// The Settings page's App passwords section (#62, ADR-0024): lets a user
// generate a per-device credential for native CalDAV clients, shown once with
// a copy affordance, and revoke any of them later.
export function AppPasswordsSection() {
  const appPasswords = useAppPasswordsStore((state) => state.appPasswords);
  const fetchAppPasswords = useAppPasswordsStore((state) => state.fetchAppPasswords);
  const createAppPassword = useAppPasswordsStore((state) => state.createAppPassword);
  const revokeAppPassword = useAppPasswordsStore((state) => state.revokeAppPassword);
  // calendars only informs the Exposure nudge below (#297, ADR-0080) — this
  // section otherwise has nothing to do with Calendars.
  const calendars = useCalendarsStore((state) => state.calendars);

  const [label, setLabel] = useState("");
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [revokingId, setRevokingId] = useState<number | null>(null);
  const [nudgeReminderDelivery, setNudgeReminderDelivery] = useState(false);
  // nudgeExposure fires the same moment and follows the same one-shot
  // "leaving this page means it won't be shown again" rule as
  // nudgeReminderDelivery (ADR-0027's pattern, reused by #297/ADR-0080): a
  // first App Password is the first proof this User has a device to expose
  // a Linked Calendar to at all.
  const [nudgeExposure, setNudgeExposure] = useState(false);
  const { isSubmitting, error, setError, run } = useAsyncAction();

  useEffect(() => {
    fetchAppPasswords().catch((err) => {
      setError(errorMessage(err));
    });
  }, [fetchAppPasswords, setError]);

  async function handleCreate(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!label.trim()) return;

    const isFirstAppPassword = appPasswords.length === 0;
    // Only a Linked Calendar this User owns and hasn't already exposed is
    // worth nudging about — an ordinary or Subscribed Calendar has no
    // unexposed default to surprise anyone with (#297, ADR-0080).
    const hasUnexposedLinkedCalendar = calendars.some(
      (calendar) => isLinkedCalendar(calendar) && canManageCalendar(calendar) && !calendar.exposed,
    );

    await run(async () => {
      const secret = await createAppPassword(label.trim());
      setRevealedSecret(secret);
      setCopied(false);
      setLabel("");
      setNudgeReminderDelivery(isFirstAppPassword);
      setNudgeExposure(isFirstAppPassword && hasUnexposedLinkedCalendar);
    });
  }

  // Not run through useAsyncAction: revoking tracks a per-row revokingId rather
  // than the shared isSubmitting, which drives the Generate button's spinner.
  async function handleRevoke(id: number) {
    if (!window.confirm("Revoke this app password? Any device using it will stop syncing.")) return;

    setRevokingId(id);
    setError(null);
    try {
      await revokeAppPassword(id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRevokingId(null);
    }
  }

  async function handleCopy() {
    if (!revealedSecret) return;
    await navigator.clipboard.writeText(revealedSecret);
    setCopied(true);
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">App passwords</h2>
      <p className="mt-1 text-body text-ink-muted">
        Generate a password for a native calendar app to sync over CalDAV. Each one can be revoked
        independently without affecting your login or other devices.
      </p>

      {revealedSecret && (
        <div className="mt-4 rounded-md border border-border bg-surface-raised p-3">
          <p className="text-label-sm text-ink-muted">
            Copy this now — it won't be shown again.
          </p>
          <div className="mt-2 flex items-center gap-2">
            <code className="flex-1 overflow-x-auto rounded bg-surface px-2 py-1 text-body text-ink">
              {revealedSecret}
            </code>
            <IconButton onClick={handleCopy} aria-label="Copy app password">
              {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
            </IconButton>
          </div>
          {nudgeReminderDelivery && (
            <p className="mt-2 text-label-sm text-ink-muted">
              This device will show its own reminder pop-ups. Copy the password above, then visit
              Reminder delivery in the left-hand nav to stop getting them twice — leaving this
              page means it won't be shown again.
            </p>
          )}
          {nudgeExposure && (
            <p className="mt-2 text-label-sm text-ink-muted">
              One of your linked calendars won't show up on this device yet — turn on "Show on my
              devices" from that calendar's menu in the sidebar to bring it over.
            </p>
          )}
        </div>
      )}

      <form onSubmit={handleCreate} className="mt-4 flex items-end gap-2">
        <Input
          label="Label"
          placeholder="e.g. iPhone"
          value={label}
          onChange={(domEvent) => setLabel(domEvent.target.value)}
          className="w-72"
        />
        <Button type="submit" disabled={!label.trim()} loading={isSubmitting}>
          Generate
        </Button>
      </form>

      {error && <p className="mt-2 text-label-sm text-danger">{error}</p>}

      <ul className="mt-4 flex flex-col gap-2">
        {appPasswords.map((appPassword) => (
          <li
            key={appPassword.id}
            className="flex items-center justify-between rounded-md border border-border px-3 py-2"
          >
            <div>
              <p className="text-body text-ink">{appPassword.label}</p>
              <p className="text-label-sm text-ink-muted">
                Created {new Date(appPassword.createdAt).toLocaleDateString()}
                {appPassword.lastUsedAt &&
                  ` · Last used ${new Date(appPassword.lastUsedAt).toLocaleDateString()}`}
              </p>
            </div>
            <IconButton
              onClick={() => handleRevoke(appPassword.id)}
              disabled={revokingId === appPassword.id}
              aria-label={`Revoke ${appPassword.label}`}
            >
              <Trash2 className="size-4" />
            </IconButton>
          </li>
        ))}
        {appPasswords.length === 0 && (
          <p className="text-label-sm text-ink-muted">No app passwords yet.</p>
        )}
      </ul>
    </section>
  );
}
