import { useState } from "react";
import { useAuthStore } from "../lib/authStore";
import { appPasswordsApi } from "../lib/appPasswordsApi";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useSyncedField } from "../hooks/useSyncedField";
import { errorMessage } from "../lib/errorMessage";
import { hasMinPasswordLength } from "../lib/passwordPolicy";
import { Button } from "../components/ui/Button";
import { Input } from "../components/ui/Input";
import { DeleteAccountDialog } from "./DeleteAccountDialog";

// The Settings page's Account section (#57, #125, #234, ADR-0047): Name (a
// display label), Email (the login identifier and Email-Channel Reminder
// recipient, ADR-0021), and Change password — the self-service route to
// changePassword, previously reachable only from the forced change-password
// screen. The Email Channel only becomes selectable in the event modal once
// the self-hoster has SMTP configured — see reminderChannelOptions. Also
// where ADR-0059's IMAP disclosure lives, beside the SMTP-backed fields
// rather than as a warning on every invite (#202): plain text, shown only
// when Invitations are actually being sent but nothing reads Responses back.
export function AccountSection() {
  const user = useAuthStore((state) => state.user);
  const accessToken = useAuthStore((state) => state.accessToken);
  const updateName = useAuthStore((state) => state.updateName);
  const updateEmail = useAuthStore((state) => state.updateEmail);
  const changePassword = useAuthStore((state) => state.changePassword);
  const disableAccount = useAuthStore((state) => state.disableAccount);

  // `user` in the store is replaced wholesale by every save this session
  // makes — including ones from another tab, since each save's response is
  // the full canonical record, not just the field it touched (#245
  // reopened). useSyncedField keeps Name/Email tracking it while pristine,
  // without clobbering an unsaved edit; a passive resync also clears the
  // stale "Saved." message it would otherwise leave attached to a value
  // this tab never actually submitted.
  const [nameSaved, setNameSaved] = useState(false);
  const name = useSyncedField(user?.name ?? "", () => setNameSaved(false));
  const nameAction = useAsyncAction();

  const [emailSaved, setEmailSaved] = useState(false);
  const email = useSyncedField(user?.email ?? "", () => setEmailSaved(false));
  const emailAction = useAsyncAction();

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordChanged, setPasswordChanged] = useState(false);
  const passwordAction = useAsyncAction();

  const [disableError, setDisableError] = useState<string | null>(null);
  const [isDisabling, setIsDisabling] = useState(false);
  const [deletingAccount, setDeletingAccount] = useState(false);

  const isNameUnchanged = name.value === (user?.name ?? "");
  const isEmailUnchanged = email.value === (user?.email ?? "");

  // Both sides of the mismatch check share the same non-empty rule
  // canSubmitPassword applies, so there's no whitespace-only value that
  // reads as "matching" here yet still leaves Submit disabled unexplained.
  const newPasswordsMatch = newPassword.trim() !== "" && newPassword === confirmPassword;
  // Mirrors the hint text below the field client-side (#255) so a too-short
  // password is caught before the round trip that already catches it.
  const canSubmitPassword =
    currentPassword.trim() !== "" && hasMinPasswordLength(newPassword) && newPasswordsMatch;

  async function handleSubmitName(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (isNameUnchanged || !name.value.trim()) return;

    await nameAction.run(async () => {
      setNameSaved(false);
      const updatedUser = await updateName(name.value.trim());
      name.markSaved(updatedUser.name);
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

      const updatedUser = await updateEmail(email.value);
      email.markSaved(updatedUser.email);
      setEmailSaved(true);
    });
  }

  async function handleSubmitPassword(domEvent: React.FormEvent) {
    domEvent.preventDefault();
    if (!canSubmitPassword) return;

    await passwordAction.run(async () => {
      setPasswordChanged(false);
      await changePassword(currentPassword, newPassword);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setPasswordChanged(true);
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
          value={name.value}
          onChange={(domEvent) => {
            name.setValue(domEvent.target.value);
            setNameSaved(false);
          }}
          disabled={nameAction.isSubmitting}
          className="w-72"
        />
        <Button
          type="submit"
          disabled={isNameUnchanged || !name.value.trim()}
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
          value={email.value}
          onChange={(domEvent) => {
            email.setValue(domEvent.target.value);
            setEmailSaved(false);
          }}
          disabled={emailAction.isSubmitting}
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

      {/* #234: current + new, so this stays a deliberate action and not a
        stray keystroke — App passwords aren't touched (ADR-0024), so a
        synced device keeps working through this the same way it does
        through Name/Email above. */}
      <div className="mt-8 border-t border-border pt-6">
        <h3 className="text-body font-medium text-ink">Change password</h3>
        <p className="mt-1 text-label-sm text-ink-muted">
          Signs you out of every other device.
        </p>

        <form onSubmit={handleSubmitPassword} className="mt-3 flex flex-col gap-3">
          {/* #246: password managers key a saved credential off a username
            field — without one here they have no way to tell which stored
            entry this change applies to, so the update after Save never
            reaches it. `sr-only` keeps it out of the visual layout without
            display:none/visibility:hidden, which is how password-manager
            heuristics decide a field isn't fillable and skip it.
            #250: that same visibility is what makes it a stop in the tab
            order with no accessible name. `tabIndex={-1}` takes it out of
            tab order without touching the DOM/autocomplete cues password
            managers key off of; `aria-hidden` is only safe once it's
            unfocusable, which tabIndex={-1} guarantees. */}
          <input
            type="text"
            name="username"
            autoComplete="username"
            value={user?.email ?? ""}
            readOnly
            tabIndex={-1}
            aria-hidden="true"
            className="sr-only"
          />
          <Input
            label="Current password"
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(domEvent) => {
              setCurrentPassword(domEvent.target.value);
              setPasswordChanged(false);
            }}
            disabled={passwordAction.isSubmitting}
            className="w-72"
          />
          <Input
            label="New password"
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(domEvent) => {
              setNewPassword(domEvent.target.value);
              setPasswordChanged(false);
            }}
            disabled={passwordAction.isSubmitting}
            className="w-72"
          />
          {/* #247: the 8-character floor. #241: bcrypt's own 72-byte cap,
            hit well before 72 visible characters for anyone not typing
            plain ASCII (e.g. emoji, accented letters) — stated up front
            instead of only surfacing as a rejection after Update password
            is pressed. */}
          <p className="-mt-2 text-label-sm text-ink-muted">
            At least 8 characters, up to 72 bytes.
          </p>
          <Input
            label="Confirm new password"
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(domEvent) => {
              setConfirmPassword(domEvent.target.value);
              setPasswordChanged(false);
            }}
            invalid={confirmPassword !== "" && !newPasswordsMatch}
            disabled={passwordAction.isSubmitting}
            className="w-72"
          />
          {confirmPassword !== "" && !newPasswordsMatch && (
            <p className="text-label-sm text-danger">Passwords don't match.</p>
          )}
          <div>
            <Button
              type="submit"
              disabled={!canSubmitPassword}
              loading={passwordAction.isSubmitting}
            >
              Update password
            </Button>
          </div>
        </form>
        {passwordAction.error && (
          <p className="mt-2 text-label-sm text-danger">{passwordAction.error}</p>
        )}
        {passwordChanged && !passwordAction.error && (
          <p className="mt-2 text-label-sm text-ink-muted">Password updated.</p>
        )}
      </div>

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
