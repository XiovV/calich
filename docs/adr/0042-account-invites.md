# Admin-issued Invites let a Pending User set their own password

Status: accepted — supersedes ADR-0037's "no self-registration and no invite tokens" line; superseded by ADR-0044 (Invites become Workspace-scoped rather than instance-wide and Admin-issued; Pending as an account state is retired, since an invited email either has no account yet — and creates one, choosing its own password immediately — or already has one and simply gains a new Workspace membership)

ADR-0037 rejected invite tokens alongside self-registration, in one breath: "a self-hosted household instance has an Admin present, and an open endpoint is a liability with no corresponding benefit." That conflated two different things. Self-registration is an *unauthenticated* endpoint — anyone who can reach the server can create an account, which is the liability ADR-0037 was right to reject and this ADR does not revisit. An Invite is not that: only an Admin can create one, for a username the Admin already chose, exactly as `AccountService.Create` requires today. What it removes is the step where the Admin makes up a temporary password and relays it out of band. That step was never load-bearing for the no-open-endpoint guarantee — it was just the only mechanism ADR-0037 had described.

## What stays true from ADR-0037

- Only an Admin creates an account. There is still no endpoint an unauthenticated caller can hit to make a User exist.
- The Admin still picks the username at creation time, Invite or not.
- Everything else ADR-0037 decided — Admin has no data access, Disable/Delete semantics, the last-Admin guard — is unchanged.

## Pending: a third account state, not a flavor of Disabled

Inviting a User creates its row immediately, the same as today's temp-password `Create`, so it shows up in the Admin's account list right away rather than living in a separate, easy-to-forget table. It starts **Pending**: cannot log in, cannot refresh a Session, cannot authenticate over CalDAV — the same three paths ADR-0037 named for Disabled, blocked for the same reason.

Pending is deliberately a state of its own rather than reusing Disabled's meaning, even though both block login. Disabled is something an Admin does *to* a User who was active. Pending is a User who has never been active — nobody took anything away from them. Conflating the two would make "why can't I log in" ambiguous in the Admin's own account list, and would put the last-Admin guard and other Disabled-specific rules in scope for a state they were never written for. (The two may share a boolean column in the schema; that is an implementation detail and not the reason they're distinct — see the Consequences section.)

A Pending User has no password yet, so `must_change_password` does not apply to it — that gate exists to force a *chosen-by-someone-else* password to be replaced, and an Invite never sets one in the first place.

## The Invite itself

A single-use, hashed token stored on the User's row (mirroring `PasswordHash` and app-password hashing — nothing here introduces a new secret-storage pattern), with a 7-day expiry. Seven days, not the Refresh token's 30 (ADR-0009), because this token's blast radius is bigger: it doesn't extend a Session, it *creates* one — anyone holding a live Invite link can become the User outright. A shorter window bounds how long a leaked or forwarded link stays dangerous, at the cost of the Admin occasionally having to reissue one for someone who took their time.

An Admin may reissue an Invite for the same Pending User at any point — before or after expiry — which overwrites the token hash and resets the expiry. This is a deliberate alternative to delete-and-recreate: it preserves the username already chosen and the Calendar defaults `EnsureDefaults` already provisioned, and it means an expired Invite is not a dead end requiring the Admin to start over.

Cancelling a Pending invite is an ordinary delete of the row. ADR-0037's transfer-or-delete disposition for a deleted User's Calendars does not apply here, because a Pending User owns nothing yet.

## Accepting an Invite

Opening the link and choosing a password does three things atomically: sets `PasswordHash`, flips Pending → Active, and establishes a Session — the User lands in the app already logged in, the same way completing a forced password change today proceeds a User into the app rather than bouncing them back to a login screen. There is no separate "invite accepted, now please log in" step; the form that sets the password has already proven who's asking.

## Email stays optional

`User.Email` was already nullable and used only for Reminder delivery (ADR-0021), never required to create an account. An Invite doesn't change that: the Admin gets a copyable link to distribute however they already would (in person, a chat app — whatever channel carried today's temp password). If an email is on file *and* SMTP is configured (`Config.SMTPConfigured()`), sending the Invite by email becomes an available convenience, not a requirement. Making email mandatory for Invites specifically — while leaving it optional for direct creation — would tie half of one feature to optional infrastructure most self-hosters don't run, for no benefit over a link the Admin copies themselves.

## Rejected alternatives

- **A separate `Invitation` entity, no `User` row until accepted.** Keeps a never-accepted Invite from leaving a row behind, but splits "who has access to my server" across two tables and needs its own uniqueness/cleanup story. A Pending row the Admin can see and delete like any other account is simpler, and an abandoned Invite is a one-click delete, not a leak.
- **Invitee picks their own username.** Moves a judgment call ADR-0037 already gives the Admin — who gets to exist on this server, under what name — into an unauthenticated form, and reopens username collisions between two pending invitees wanting the same name. The Admin already names the account today; an Invite shouldn't change who decides that.
- **Indefinite-lifetime Invite links.** Cheaper to implement, but a token with no expiry that grants full account takeover is a standing liability with no offsetting benefit, unlike a Share link or similar that only grants read access.

## Consequences

- `users` gains `invite_token_hash` and `invite_expires_at`, alongside whatever column already distinguishes Pending from Active (reusing `is_disabled` is one option, since the two states are mutually exclusive in practice — Pending precedes any Active period, Disabled follows one). The glossary distinction between Pending and Disabled is a domain one; it does not require two separate columns if one boolean plus the presence of a password can tell them apart.
- The Admin's account list needs a third visual state (`Invited` / `Expired`) alongside Active and Disabled, with actions scoped to what a Pending row can meaningfully do (copy link, resend, send email, cancel) rather than the Active/Disabled row actions (disable, delete-with-disposition, reset password).
- Direct creation with an Admin-set temporary password remains available alongside Invites — a User created and handed a password on the spot never passes through Pending at all.
