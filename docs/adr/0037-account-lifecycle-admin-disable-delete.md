# Admin manages accounts, not data; accounts are disabled or deleted with an explicit disposition

Status: accepted — supersedes ADR-0010's "no invites" rule

Multiple accounts need someone empowered to create them. `users` gains `is_admin`; the bootstrapped account (ADR-0010) becomes the first Admin.

**An Admin manages accounts. An Admin has no data access whatsoever.** To read or write Alice's Family calendar, an Admin needs a Share exactly like anyone else.

## Why Admin grants no data access

The tempting alternative — Admin as true superuser, useful for oversight and for debugging your own server — would make every permission check in ADR-0034 grow an `or is_admin` branch, and Access would stop being the whole story. Two parallel authorization systems is one more than can be kept correct, and the bypass path is the one that never gets tested. It is also a privacy surprise on a family instance, where the Admin is a parent and the data is a teenager's.

Accounts and data are therefore fully orthogonal: Admin is authority over *who exists*, Access is authority over *what they can see*.

## Account creation

An Admin creates an account with a username and a temporary password, reusing ADR-0010's existing `must_change_password` gate — the same forced-change machinery that closes the bootstrap window closes this one. No self-registration and no invite tokens: a self-hosted household instance has an Admin present, and an open endpoint is a liability with no corresponding benefit. `EnsureDefaults` runs per new User rather than only at bootstrap, so a new account starts with default Calendars rather than an empty sidebar.

## Disable

**Disable is a reversible account state.** It blocks three separate paths, not one:

- `AuthService.Login`
- `AuthService.Refresh` — otherwise a 30-day refresh cookie keeps minting access tokens
- **CalDAV Basic auth** (`httpauth.RequireCalDAVAuth`) — app passwords are an entirely separate authenticator (ADR-0024), so a disabled User's phone would otherwise keep syncing indefinitely

Live sessions are deleted, exactly as `ChangePassword` already does. The scheduler stops firing the disabled User's Notifications and Emails.

**Everything they own stays live for everyone else.** Ownership stays with the disabled User; their Calendars, their Events, and every Share on them are untouched. Disable is a property of the account, not of the data — "turn off this login" must not silently degrade other people's calendars. If the data should go, delete is the operation that says so.

One gap is accepted knowingly: `AuthService.Authenticate` is a stateless JWT check that never touches the database, so a disabled User keeps a working access token for up to its 15-minute TTL. Closing it would mean a database read on every authenticated request, which is a poor trade for 15 minutes.

## Delete

**Deleting a User requires an explicit disposition for the Calendars they own:** transfer them to a named User, or delete them.

```
DELETE /api/users/{id}
  { "ownedCalendars": "transfer", "transferTo": 2 }
  { "ownedCalendars": "delete" }
```

Today's `ON DELETE CASCADE` would take a shared Family calendar — including Events other people wrote — out from under everyone with a Share, silently, at the exact moment nobody is paying attention. That is close enough to data loss to be worth one deliberate decision point. The UI warns which Calendars have Shares and how many people lose access.

Rejected: always cascading (zero new code, silent destruction); blocking deletion while any owned Calendar has Shares (safe, but two round trips for one conceptual action); and always transferring to the deleting Admin (never destroys anything, but quietly hands the Admin data they may have no business holding — which contradicts this ADR's central rule).

## Guards

- An Admin cannot disable or delete the **last remaining Admin**, or an instance becomes unadministrable.
- A disabled User is hidden from the share picker; their existing Shares keep working.
- A User may revoke their own Share — leave a Calendar — without involving the Owner.

## Consequences

- **ADR-0032's SSRF tripwire fires here.** That ADR accepted fetching user-supplied Subscription URLs without private-address blocking, on the explicit grounds that "the person supplying the URL already has authority over the machine, so there is no privilege boundary for SSRF to cross," and named the condition for revisiting: *"The day a second User can add a Subscription, this becomes a live vulnerability."* **This ADR is that day.** A non-Admin household member can now point a Subscription at `http://192.168.1.1/` or a cloud metadata endpoint and probe internal hosts through response timing and error text. Blocking must re-check after every redirect, not only the URL as typed. This is a prerequisite of shipping multi-user, not a follow-up to it.
- **Stored third-party credentials gain a second reader.** ADR-0032 accepted that a Subscription URL may carry HTTP Basic credentials in plaintext in the SQLite file. With multiple accounts, the masking requirement it imposed — every surface rendering a Subscription URL, including error messages and tooltips — now protects users from *each other*, not only from shoulder-surfers.
- **`is_admin` is a boolean, not a role enum.** Two levels is what a household needs; a third would be inventing a hierarchy nobody asked for. Widening it later is a cheap migration precisely because Admin grants no data access — there is no permission graph entangled with it.
