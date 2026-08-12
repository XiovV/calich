import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { appPasswordsApi } from "../lib/appPasswordsApi";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { errorMessage } from "../lib/errorMessage";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { DeleteAccountDialog } from "./DeleteAccountDialog";

// The Settings page's Account section (#57, #125, ADR-0047): Name (a display
// label) and Email (the login identifier and Email-Channel Reminder
// recipient, ADR-0021). The Email Channel only becomes selectable in the
// event modal once the self-hoster has SMTP configured — see
// reminderChannelOptions. Also where ADR-0059's IMAP disclosure lives,
// beside the SMTP-backed fields rather than as a warning on every invite
// (#202): plain text, shown only when Invitations are actually being sent
// but nothing reads Responses back.
export function AccountSection() {
  const user = useAuthStore((state) => state.user);
  const accessToken = useAuthStore((state) => state.accessToken);
  const updateName = useAuthStore((state) => state.updateName);
  const updateEmail = useAuthStore((state) => state.updateEmail);
  const disableAccount = useAuthStore((state) => state.disableAccount);

  const [name, setName] = useState(user?.name ?? "");
  const [nameSaved, setNameSaved] = useState(false);
  const nameAction = useAsyncAction();

  const [email, setEmail] = useState(user?.email ?? "");
  const [emailSaved, setEmailSaved] = useState(false);
  const emailAction = useAsyncAction();

  const [disableError, setDisableError] = useState<string | null>(null);
  const [isDisabling, setIsDisabling] = useState(false);
  const [deletingAccount, setDeletingAccount] = useState(false);

  const isNameUnchanged = name === (user?.name ?? "");
  const isEmailUnchanged = email === (user?.email ?? "");

  async function handleSubmitName(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isNameUnchanged || !name.trim()) return;

    await nameAction.run(async () => {
      setNameSaved(false);
      await updateName(name.trim());
      setNameSaved(true);
    });
  }

  async function handleSubmitEmail(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isEmailUnchanged) return;

    await emailAction.run(async () => {
      setEmailSaved(false);

      // Changing the login identifier breaks CalDAV sync on every device
      // already configured with the old email until it's updated there too
      // (ADR-0047) — the App passwords themselves stay valid. Only worth
      // interrupting for when there's actually a synced device to warn about.
      if (accessToken) {
        const appPasswords = await appPasswordsApi.list(accessToken);
        if (appPasswords.length > 0) {
          const count = appPasswords.length;
          const proceed = window.confirm(
            `You have ${count} app password${count === 1 ? "" : "s"} in use. Changing your ` +
              `email will stop those devices syncing until it's updated in each. The app ` +
              `passwords themselves stay valid.`,
          );
          if (!proceed) return;
        }
      }

      await updateEmail(email);
      setEmailSaved(true);
    });
  }

  // Disabling is self-reversible (re-activate by logging back in, ADR-0044)
  // — a lighter confirmation than delete's, which is why this stays a plain
  // window.confirm rather than its own dialog.
  async function handleDisable() {
    if (!window.confirm("Disable your account? You can re-activate it later by logging back in.")) {
      return;
    }

    setIsDisabling(true);
    setDisableError(null);
    try {
      await disableAccount();
    } catch (err) {
      setDisableError(errorMessage(err));
    } finally {
      setIsDisabling(false);
    }
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Account</h2>

      <form onSubmit={handleSubmitName} className="mt-4 flex items-end gap-2">
        <Input
          label="Name"
          value={name}
          onChange={(domEvent) => {
            setName(domEvent.target.value);
            setNameSaved(false);
          }}
          className="w-72"
        />
        <Button
          type="submit"
          disabled={isNameUnchanged || !name.trim()}
          loading={nameAction.isSubmitting}
        >
          Save
        </Button>
      </form>
      {nameAction.error && (
        <p className="mt-2 text-label-sm text-danger">{nameAction.error}</p>
      )}
      {nameSaved && !nameAction.error && (
        <p className="mt-2 text-label-sm text-ink-muted">Saved.</p>
      )}

      <p className="mt-6 text-body text-ink-muted">
        Used to sign in and to deliver Email-channel Reminders.
      </p>
      <form onSubmit={handleSubmitEmail} className="mt-2 flex items-end gap-2">
        <Input
          label="Email"
          type="email"
          value={email}
          onChange={(domEvent) => {
            setEmail(domEvent.target.value);
            setEmailSaved(false);
          }}
          className="w-72"
        />
        <Button type="submit" disabled={isEmailUnchanged} loading={emailAction.isSubmitting}>
          Save
        </Button>
      </form>
      {emailAction.error && <p className="mt-2 text-label-sm text-danger">{emailAction.error}</p>}
      {emailSaved && !emailAction.error && (
        <p className="mt-2 text-label-sm text-ink-muted">Saved.</p>
      )}

      {user?.emailReminderChannelAvailable && !user.invitationRepliesConfigured && (
        <p className="mt-2 text-label-sm text-ink-muted">
          Invitations are emailed to Attendees, but this instance has no IMAP configured, so
          Accept/Decline/Tentative replies from their mail client never come back — those
          Attendees stay Needs-Action.
        </p>
      )}

      <div className="mt-8 border-t border-border pt-6">
        <h3 className="text-body font-medium text-ink">Danger zone</h3>

        <div className="mt-3 flex items-center justify-between gap-3">
          <div>
            <p className="text-body text-ink">Disable account</p>
            <p className="text-label-sm text-ink-muted">
              Log out everywhere. Re-activate anytime by logging back in.
            </p>
          </div>
          <Button
            variant="outline"
            color="danger"
            size="small"
            loading={isDisabling}
            onClick={handleDisable}
          >
            Disable
          </Button>
        </div>
        {disableError && <p className="mt-2 text-label-sm text-danger">{disableError}</p>}

        <div className="mt-4 flex items-center justify-between gap-3">
          <div>
            <p className="text-body text-ink">Delete account</p>
            <p className="text-label-sm text-ink-muted">
              Permanently delete your account and choose what happens to each calendar you own.
            </p>
          </div>
          <Button
            variant="outline"
            color="danger"
            size="small"
            onClick={() => setDeletingAccount(true)}
          >
            Delete
          </Button>
        </div>
      </div>

      {deletingAccount && <DeleteAccountDialog onClose={() => setDeletingAccount(false)} />}
    </section>
  );
}
