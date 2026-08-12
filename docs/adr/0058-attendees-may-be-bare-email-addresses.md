# An Attendee is a User or a bare email address

Status: accepted — supersedes ADR-0046's "there is no cross-Workspace or external-guest invite in this round"; extends ADR-0047 and ADR-0055

ADR-0046 fenced external guests out explicitly, and CONTEXT.md carried the reason as a flat statement of fact: *"an address with no account behind it is unrepresentable here."* Arranging a meeting with anyone who doesn't already have an account on this instance is the case that breaks against, and it is not an edge case — it is most meetings. This ADR makes such an address representable.

## The Attendee widens rather than gaining a sibling

`attendees` gains a nullable `email` alongside a now-nullable `user_id`, with a CHECK that exactly one is set. One table, one Response state machine, one list endpoint, one `ATTENDEE` line.

The alternative — a second entity beside Attendee — was rejected on the grounds that the two would differ in nothing a caller cares about. The invitation is identical, the four `PARTSTAT` values are identical, and the thing being modelled is *a party invited to this meeting*. What differs is only whether this instance happens to hold an account for them, which is a fact about us, not about them. A sibling entity would force every rule to be stated twice and every list query to be a union forever, and it would go stale the moment the conversion below fires.

Routing outside addresses through a Workspace Invite instead was rejected as a different feature: it makes a stranger a Member of your Workspace in order to attend one meeting.

## Resolution is Workspace-scoped, and happens at write time

A typed address is matched against the Members of the Event's own Workspace before the row is written. A match writes a `user_id` row; anything else writes an `email` row.

Resolution is not optional. An email-shaped row for someone who *does* have an account here is a second-class invite — no in-app Notification, no Reminder fan-out (`ListUserIDsByEventIDs` joins on `user_id`), no Event on their grid — and the organizer has no way to see that it happened. Resolving at write time also gives the row a real key, which is what stops the same person being invited twice under both shapes.

Scoping the search to the Workspace rather than the whole instance keeps ADR-0046's target boundary intact: a typed address never reaches across Workspaces to grant Event visibility inside the app. An account that exists on this instance in a *different* Workspace is treated as an outsider and gets email — a deliberate, if slightly odd, consequence of holding the boundary.

## An outstanding invite converts when the address gets an account

Accepting a Workspace Invite sweeps every email-shaped Attendee row matching that address, on Events in that Workspace, into `user_id` rows, carrying the Response across.

This is not in tension with ADR-0055's snapshot rule for Group expansion. A Group expansion snapshots *who was in a set* — a genuine judgement about membership at a moment. An email row is not a judgement; it is a fallback written because we could not find their account. When the account appears, the fallback has no reason to persist, and "I joined the workspace and my invite is still stuck in my inbox" is not a state worth being able to represent.

Both shapes can coexist by then — invited by email, then invited again from the member picker. The merge rule: the `user_id` row survives, the email row is deleted, and if the `user_id` row is still `needs-action` while the email row carries a real answer, the answer moves over. Answering once counts.

## Email is case-insensitive everywhere

Emails are trimmed and `mail.ParseAddress`-validated today but never folded, and `users.email UNIQUE` uses SQLite's default case-sensitive collation — so `Damir@x.com` and `damir@x.com` are already two accounts. A feature whose key is an email address cannot be built on that.

Case is folded on write across the app and `users.email` becomes `COLLATE NOCASE`: one address, one account, one Attendee, at the login form as much as at the invite field. Taken now because ADR-0048 keeps migrations squashed until first release, so it costs a line rather than a data migration. RFC 5321 does permit case-sensitive local parts; no mainstream provider implements it and no calendar product honours it.

Uniqueness becomes `UNIQUE(event_id, user_id)` and `UNIQUE(event_id, email)` — the old `(event_id, user_id)` primary key constrains nothing once NULLs are allowed, since SQLite treats every NULL as distinct.

## Typed addresses are strict, the way explicit targets already are

`attendeeEmails` joins ADR-0055's create request, staged and written inside the same transaction. ADR-0055's asymmetry holds — explicit targets strict, Group expansion lenient — and a typed address is explicit:

- **Malformed address**: 400, transaction rolled back, no Event. It was typed by hand and it is the one error the user can fix.
- **The Organizer's own address**: rejected, not silently dropped. ADR-0055 made the Organizer structurally not an Attendee; admitting the row is one `SetResponse` away from an Organizer who declined their own meeting. Google no-ops here; this repo already chose loudness for explicit targets, and silently dropping means believing you invited someone you didn't. Same rule when an Editor types the Organizer's address.
- **A Disabled Member's address**: rejected, not downgraded to an email row. Downgrading would route around the account state and mail someone whose access was deliberately switched off. Group expansion keeps skipping them silently.

## Consequences

- **An Attendee who isn't a Member gets no Reminders.** ADR-0046's fan-out ("everyone with Access, unioned with every Attendee") stays User-shaped. They hold the Invitation in their own calendar and their own client reminds them from its own default alarm — firing a server-side reminder at a client we know is holding a copy is ADR-0027's double-fire in its most extreme form. It would also mean inventing, for someone with no account, every per-User answer ADR-0036 and ADR-0039 made per-User on purpose.
- **No new glossary term.** CONTEXT.md refuses a word for the email-shaped kind, on the same grounds it refuses one for "a Calendar someone shared with you".
- **Linked Calendars still don't mirror their Provider's attendees**, but the stated reason changes, because the old one is now false. The new one: a Refresh writes Attendee rows, and after ADR-0059 writing an Attendee row is what sends an Invitation — so mirroring must first make "a Refresh-written Attendee cannot generate mail" a tested guarantee rather than an assumption. The Access clamp a Source applies (ADR-0052) does not cover it, because the Refresh path bypasses Access checks by construction.
- **Inviting is still Editor-gated** (ADR-0055), unchanged for typed addresses. Any Editor can now make this instance send mail to an address of their choosing, which is a volume problem rather than an authorization one; the mitigation is a rate limit on outbound Invitations per User per hour, not a narrower role. A per-Workspace kill switch remains available if that proves insufficient, and would be honest about being policy rather than permission.
- **On an instance with no SMTP the affordance is absent entirely** — the picker stays Members-only, as today. An email invitee with no email delivery is not a degraded invite, it is a row that means nothing.
