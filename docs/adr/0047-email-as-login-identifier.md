# Email is the login identifier; Name is a non-unique display label

Status: accepted — amends ADR-0024's Basic-auth identifier and ADR-0010/ADR-0044's bootstrap story

`username` used to do two jobs at once: it was the account's login identifier (`NOT NULL UNIQUE`, resolved by web sign-in, CalDAV Basic auth, and Share-target lookup) *and* the value a person typed as their name. Register and Accept-Invite already held that value in state called `name` and sent it to the server as `name` — but validated and labelled it as a username: rejecting whitespace and `:` because CalDAV Basic auth (`net/http.Request.BasicAuth`) splits credentials on the first colon. A display name that can't contain a space isn't a name.

**Email now identifies an account. Name is a display label with no identifier constraints.**

- **Email** — required, unique instance-wide, well-formed, and (per the constraint it inherits from the old `username`) free of `:` and whitespace, since it's what travels in a CalDAV Basic-auth header. It is what a person types to sign in, what CalDAV authenticates against, and what a Share or Workspace Invite names to resolve an account. Sign-in failure stays indistinguishable between "no such email" and "wrong password" — this doesn't change.
- **Name** — required, length-bounded, *not* unique, and may contain anything a person would put in their own name, including spaces and colons. "Jane Smith" is a valid Name. Two accounts may share one.

The `users.username` column is renamed to `users.name`; `NOT NULL UNIQUE` moves from it to `users.email`, which drops the "optional, non-unique, Email-Channel-Reminder-only" shape it had since ADR-0021.

## Migration: fail loudly, never invent an email

Making `email` `NOT NULL UNIQUE` has to contend with existing rows that have no email (the bootstrap account never got one — see below) or share one. The migration is deliberately **fail-loud, no auto-backfill**: it rebuilds the `users` table inside one transaction, and if any existing row can't satisfy `NOT NULL UNIQUE` on `email`, the `INSERT` fails the constraint and the whole migration rolls back — the instance stays on its old schema until an operator fixes the offending rows and re-runs it.

The alternative — synthesizing a placeholder email for a row that doesn't have one — was rejected outright: a placeholder isn't something the account holder can actually sign in with, which directly contradicts the requirement that no existing account is silently left unable to sign in. Failing loudly, with no half-applied state, is the only shape that can't strand someone.

## Bootstrap: `INITIAL_USERNAME` → `INITIAL_EMAIL` + `INITIAL_NAME`

The bootstrap account (ADR-0010, ADR-0044) previously came from `INITIAL_USERNAME`/`INITIAL_PASSWORD` and had no email at all — itself now a data point the migration above would refuse to let through. Bootstrap's env-configured path is a clean break, not an aliased rename:

- `INITIAL_EMAIL` (required, alongside `INITIAL_PASSWORD`, for Bootstrap to create the first account) replaces `INITIAL_USERNAME` as the identifier.
- `INITIAL_NAME` is new, optional, and defaults to `"Admin"` — the display name has no bearing on whether Bootstrap runs at all.

## Every surface that authenticates or resolves a person moves to Email

- **Web sign-in** (`AuthService.Login`) resolves by `GetByEmail` instead of `GetByUsername`.
- **CalDAV Basic auth** (`AppPasswordService.Authenticate`, ADR-0024) resolves by `GetByEmail`. The `:`/whitespace rejection that used to live on `validateUsername` moves to `validateEmail` — this is a relocation of an existing CalDAV-safety rule, not a new restriction; ordinary email addresses never contain either character.
- **Share-target resolution** (`CalendarService.Share`) resolves the person typed into a Share by `GetByEmail`, not by username. The Share and Workspace-member response shapes now carry both `Email` (what was typed/matched) and `Name` (what's displayed) — a caller can no longer assume they're the same value.
- **Register**, **Accept-Invite**, and **`UpdateName`** validate the display value with `validateName` (trim, non-empty, length-bounded, no character restriction) instead of `validateUsername`. `UpdateEmail` validates with `validateEmail` and can no longer be called with an empty string to clear the account's email, since email is mandatory now.

## First-run redirect to Register

Split from the Email/Name change but shipped in the same ticket (#169): a fresh instance with zero accounts has nothing to sign in to, so an unauthenticated visitor is redirected to `/register` instead of `/login`. This needs an unauthenticated "does this instance have any accounts yet" signal — `GET /api/auth/setup-status`, backed by `AuthService.HasAnyAccounts` (a thin wrapper over the existing `UserRepository.Count`). The frontend's `authStore.bootstrap()` calls it only after `refresh()` fails (i.e. only when it would otherwise show Sign-in), so a returning, already-logged-in visitor never touches it, and a first-run visitor never sees a Sign-in flash before the redirect.

## Side effect: Email-Channel Reminder opt-out

Before this ADR, clearing an account's email (`UpdateEmail("")`) was how a person opted out of the Email-Channel Reminder (ADR-0021) — "no email set" meant "nothing to send to." Email can no longer be cleared. Reminder delivery on the Email channel now relies solely on the reminder's own per-Reminder channel toggle; "has an email" is always true and carries no opt-out meaning anymore. `meResponse.emailReminderChannelAvailable` collapses accordingly: it no longer depends on the User having an email (they always do) and reads purely off whether the self-hoster has SMTP configured.

## Considered and rejected

- **Sign in by either Email or Name.** Two identifiers double the surface for enumeration and username-squatting-style bugs, and Name being non-unique makes "sign in by Name" ambiguous on its face. Rejected — Email only.
- **Synthesizing a placeholder email during migration.** See above — rejected because it can leave an account looking migrated while nobody can actually sign into it.
- **Keeping the `users.username` column name and just relaxing its constraints.** Considered to shrink the diff, but a column called `username` holding a value that is explicitly not used to log in is exactly the confusion this ADR exists to resolve. Renamed to `name`.

## Consequences

- `users.username` → `users.name` (`NOT NULL`, not unique); `users.email` gains `NOT NULL UNIQUE`.
- `repository.ErrUsernameTaken` → `ErrEmailTaken`; `service.ErrInvalidUsername` → `ErrInvalidDisplayName`; a new `service.ErrInvalidEmail`/`ErrEmailRequired` pair covers the identifier side.
- `INITIAL_USERNAME` is gone; self-hosters must set `INITIAL_EMAIL` (and may set `INITIAL_NAME`) to keep env-configured bootstrap working across the upgrade.
- Every wire shape that used to carry `username` (sign-in, Share, Workspace member listing, User directory, Event/Attachment attribution) now carries `name` and, where the identifier itself needs to travel, `email` too.
