import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { appPasswordsApi } from "../lib/appPasswordsApi";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";

// The Settings page's Account section (#57, #125): username and the
// Email-Channel Reminder recipient (ADR-0021). The Email Channel only
// becomes selectable in the event modal once an email is set *and* the
// self-hoster has SMTP configured — see reminderChannelOptions.
export function AccountSection() {
  const user = useAuthStore((state) => state.user);
  const accessToken = useAuthStore((state) => state.accessToken);
  const updateUsername = useAuthStore((state) => state.updateUsername);
  const updateEmail = useAuthStore((state) => state.updateEmail);

  const [username, setUsername] = useState(user?.username ?? "");
  const [usernameSaved, setUsernameSaved] = useState(false);
  const usernameAction = useAsyncAction();

  const [email, setEmail] = useState(user?.email ?? "");
  const [emailSaved, setEmailSaved] = useState(false);
  const emailAction = useAsyncAction();

  const isUsernameUnchanged = username === (user?.username ?? "");
  const isEmailUnchanged = email === (user?.email ?? "");

  async function handleSubmitUsername(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isUsernameUnchanged || !username.trim()) return;

    await usernameAction.run(async () => {
      setUsernameSaved(false);

      // Renaming breaks CalDAV sync on every device already configured with
      // the old username until it's updated there too (#125) — the App
      // passwords themselves stay valid. Only worth interrupting for when
      // there's actually a synced device to warn about.
      if (accessToken) {
        const appPasswords = await appPasswordsApi.list(accessToken);
        if (appPasswords.length > 0) {
          const count = appPasswords.length;
          const proceed = window.confirm(
            `You have ${count} app password${count === 1 ? "" : "s"} in use. Renaming will ` +
              `stop those devices syncing until the username is updated in each. The app ` +
              `passwords themselves stay valid.`,
          );
          if (!proceed) return;
        }
      }

      await updateUsername(username.trim());
      setUsernameSaved(true);
    });
  }

  async function handleSubmitEmail(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isEmailUnchanged) return;

    await emailAction.run(async () => {
      setEmailSaved(false);
      await updateEmail(email);
      setEmailSaved(true);
    });
  }

  return (
    <section>
      <h2 className="text-heading font-medium text-ink">Account</h2>

      <form onSubmit={handleSubmitUsername} className="mt-4 flex items-end gap-2">
        <Input
          label="Username"
          value={username}
          onChange={(domEvent) => {
            setUsername(domEvent.target.value);
            setUsernameSaved(false);
          }}
          className="w-72"
        />
        <Button
          type="submit"
          disabled={isUsernameUnchanged || !username.trim()}
          loading={usernameAction.isSubmitting}
        >
          Save
        </Button>
      </form>
      {usernameAction.error && (
        <p className="mt-2 text-label-sm text-danger">{usernameAction.error}</p>
      )}
      {usernameSaved && !usernameAction.error && (
        <p className="mt-2 text-label-sm text-ink-muted">Saved.</p>
      )}

      <p className="mt-6 text-body text-ink-muted">Used to deliver Email-channel Reminders.</p>
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
    </section>
  );
}
