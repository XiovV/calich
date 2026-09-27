# A booking is an ordinary Event, not its own entity

Status: accepted — reuses ADR-0058's email-shaped Attendee and ADR-0059's iMIP path wholesale; gates the public surface on SMTP, and deliberately adds no fourth Notification producer

A confirmed booking creates an ordinary Event on the Booking Link's Book-into Calendar: Busy, Organizer the host, with the visitor as an email-shaped Attendee. Nothing else is stored. There is no `bookings` table.

## Why there is no booking entity

The obvious design is a `bookings` row holding the visitor's name, email, answers and a cancel token, pointing at the Event it produced. Every scheduling product has one, and it buys custom questions and a record that outlives the Event.

It was refused because the two rows would have to be kept true about each other forever, and every question about divergence has a bad answer. The host deletes the Event from the grid — is the booking cancelled? The host drags it to another Calendar or another hour — does the booking still describe reality? The host edits the title? Each of those is a rule to invent and then defend, and the host has no idea a second row exists.

The decisive point is that **the model already holds everything a booking is**. ADR-0058 established an Attendee identified by a bare email address, for exactly the case where no account here matches. ADR-0059 already emails that address a `METHOD:REQUEST` which a mail client renders as an invite card, already re-sends on material change with an incremented `SEQUENCE`, and already withdraws with `METHOD:CANCEL`. A booking is a person with no account being invited to an Event. That is the existing feature, reached from a different door.

So: **the Event is the booking.** Deleting it cancels it, because there is nothing else that could have been the truth.

The cost is accepted and real: custom questions on the booking form have nowhere to live, so they are unmodelled. A booking also leaves no trace once its Event is deleted — there is no history to consult about who booked what last quarter.

## Why the visitor's Attendee row is not a security hole

An Attendee is a visibility grant scoped to one Event (ADR-0046), and an email-shaped one has no account, so it resolves no Access and reads nothing. It receives the Invitation and nothing further: no Notifications, no Reminders here, no view of the host's calendar. If that address later joins the Workspace, the existing conversion rule turns the row into their User, which is correct — they are the same person, and they were already on that Event.

## Why publishing requires SMTP

SMTP is optional in this app; `app.go` keeps a deliberately nil `InvitationMailer` when none is configured, and Email-Channel Reminders simply do not fire. That degradation is acceptable for a feature a User uses on their own calendar. It is not acceptable here.

With no SMTP, a stranger fills in a form, sees a success screen, and receives nothing: no invite card, no confirmation, no cancel link, nothing to put in their own calendar, and no way to know it worked. The host gets an Event; the visitor gets a blank. The failure is entirely borne by someone who cannot diagnose it and has no relationship with the instance.

So **Visibility is clamped to Paused whenever SMTP is absent.** Links may be created and configured; they cannot be reached. The self-hoster is told why in Settings. The rest of the app is untouched.

The same clamp covers a second case: the host losing Owner or Editor Access to the Book-into Calendar after publishing. A stranger's booking must not be the thing that discovers a revoked Share, so Access is re-checked at booking time, not only at creation.

## Why cancellation is a signed link and not an inbound decline

The visitor holds an Invitation, so the apparent answer is to let them decline it in their mail client. That reply is ingested as a Response by the reply poller — **when one is running**, which requires IMAP, which is also optional (`app.go`: "ReplyWorker is nil when no IMAP transport is configured"). On a typical instance the decline reaches nobody. Worse, even where it lands, a Declined Attendee does not free the slot: the Event is still Busy, so a cancelled meeting keeps blocking the calendar until the host notices and deletes it by hand.

So the confirmation mail carries a **signed cancel link** — an HMAC over the Event id, on a public rate-limited endpoint (ADR-0070). Following it deletes the Event, which emits the `METHOD:CANCEL` the Attendee machinery already sends and frees the slot immediately, and emails the host. Cancelling after the Event has started is refused.

Reschedule is deliberately not built: it is cancel-then-rebook with twice the public surface and twice the token handling, and the visitor can already do both in two clicks.

**The host is told by email, not by a Notification.** The Notification entry in CONTEXT.md names exactly three producers and argues at length for that limit; a booking cancellation does not clear that bar, and adding it would start the same erosion ADR-0061 widened once already.

## Why the slot is re-derived inside the write transaction

Availability is derived, so nothing reserves anything, and two visitors can validate the same slot against the same read and both submit. ADR-0018 already provides a transaction seam for atomic multi-table writes, so the derivation runs inside it: derive busy, check the slot, insert the Event, commit. The loser is told the time was just taken and shown refreshed slots.

Holding a slot while the visitor fills in the form was rejected: it is a reservation entity with an expiry, a sweeper, and a habit of leaking held slots whenever someone closes a tab — the second stored entity this ADR exists to avoid.

## Decision

- **A booking writes one Event** on the Book-into Calendar: Busy, `title = "<link title> — <visitor name>"`, the link's Location and Description copied into `location` and `description`, Anchor zone the Schedule's zone, Organizer the host.
- **The visitor becomes an email-shaped Attendee** and receives the ordinary Invitation (ADR-0059). The booking form asks name and email, both required, and nothing else.
- **No `bookings` table.** No custom questions, no booking history.
- **Publishing requires SMTP.** Absent it, every link behaves as Paused regardless of its Visibility, and Settings says so.
- **Access is re-checked at booking time.** A host without Owner or Editor Access to the Book-into Calendar has a Paused link.
- **Cancellation is a signed HMAC link** in the confirmation mail, rate-limited per ADR-0070, refused after the Event's start. It deletes the Event and emails the host. **No new Notification producer.**
- **The slot is re-derived inside the write transaction** (ADR-0018). No reservations, no holds.
- **A Linked Calendar is a legal Book-into target**, and the Event reaches the Provider through the ordinary Write-back queue (ADR-0075). The visitor is confirmed before the push lands; a permanently failing push raises the existing ADR-0081 Notification on the host.
- **The Conflict set is per Booking Link** — Calendars, plus one row standing for Tasks — seeded from the Calendars that User owns in the Workspace, with Book-into pinned and unremovable. **Calendar toggle, the Active Calendar Set and Exposure never participate.**
- **Slots tile at the link's Duration** from the start of each Schedule range, minus a minimum notice (default 4 hours) and bounded by a booking horizon (default 60 days).

## Considered Options

- **The Event is the booking; no second entity (chosen).**
- **A `bookings` table beside the Event.** Buys custom questions and history; costs a second lifecycle with no good answer for any way the two can diverge.
- **The Event plus a minimal token row** for cancellation only. Nearly chosen; an HMAC over the Event id needs no row at all, so the row bought nothing.
- **Inbound declines as the cancellation path.** Reuses the reply poller and requires IMAP, which most instances will not have; and a decline does not free the slot anyway.
- **Slot holds during form entry.** Strictly correct under concurrency, at the cost of exactly the entity and lifecycle this ADR set out to avoid.
- **Refusing Linked Calendars as Book-into targets.** Removes the confirmed-before-pushed gap, and makes the feature useless to anyone whose real calendar is at a Provider.
- **Synchronous push to the Provider before confirming.** Puts a Provider's latency and outages on a stranger's critical path; a Google incident would mean nobody can book.

## Consequences

- **A booking's Event is indistinguishable from a hand-created one**, by design. The host can edit, move or delete it like anything else — and moving it does not tell the visitor, beyond the `SEQUENCE`-bumped Invitation ADR-0059 already re-sends.
- **Availability is only as fresh as the last Refresh.** A conflict created at a Provider minutes ago may not be visible, so a booking can collide with a Google event this instance has not pulled. Accepted knowingly; the local Event commits immediately either way, so the app never double-books against itself.
- **The first public write endpoint exists.** It creates rows, sends mail and is reachable without a Session, so rate limiting, HMAC verification and Visibility resolution are load-bearing rather than defensive.
- **An incomplete Task's Time block can now be inferred by a stranger.** With Tasks in the Conflict set, a missing slot reveals that its owner is busy then — never what the Task is, but the window is visible. This is the only path by which a private Task reaches anyone else, and it is opt-out per link.
- **There is no record of a cancelled booking.** The Event is gone, and nothing says it ever existed. Anyone wanting an audit trail needs the `bookings` table this ADR declined.
