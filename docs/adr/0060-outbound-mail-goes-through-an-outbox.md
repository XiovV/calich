# Invitation mail is queued in an outbox, not sent in the request

Status: accepted — diverges from ADR-0021's no-catch-up posture; extends ADR-0055

ADR-0055 put Attendee writes *inside* `EventService.Create`'s transaction, deliberately, so that "the Event exists, the invites silently didn't" is unreachable. After ADR-0059 every one of those writes also owes the world an email, and an SMTP handshake is a multi-second call to a machine we don't control.

## The write and the intent to send commit together

An `outbox` row is written in the same transaction as the Attendee row it belongs to. A background ticker drains it: claims pending rows, sends, retries with backoff, records the outcome.

**Not inline.** An SMTP call inside `withTx` holds SQLite's single write connection open for the duration of a remote network round-trip — the same hazard ADR-0055 already documents for pooled calls inside a transaction, where the single-connection test database deadlocks outright. Even outside the transaction it holds the HTTP request open on a third party's availability.

**Not a goroutine after commit.** Fast and simple, and it discards the exact guarantee ADR-0055 was written to establish: the failure disappears into a log line while the organizer is told the invitation went out. A missed Reminder means someone is late; a missed Invitation means nobody knows there is a meeting.

## Ordering is the second reason

ADR-0059's lifecycle produces a *stream* per recipient — `REQUEST`, then a `SEQUENCE`-bumped `REQUEST`, then possibly `CANCEL`. Delivered out of order, a `CANCEL` that overtakes the `REQUEST` it cancels leaves a meeting on someone's calendar that this instance believes it withdrew. With concurrent goroutines that is reachable on any slow send; with a drained queue processed in order per recipient it is structurally impossible.

## Consequences

- **Deliberate divergence from ADR-0021.** The Reminder scheduler drops fires missed during downtime and never replays them, on the grounds that a Reminder is worthless once its moment passes. An Invitation to next Thursday is still worth sending after a restart, so the outbox survives downtime and resumes.
- **The organizer gets something true to display** — queued, sent, or failed — instead of an implied success.
- **A permanently failing message needs a terminal state.** Backoff cannot retry forever; a message that has exhausted its attempts is marked failed and surfaced, not silently re-queued.
- **A second background ticker joins the scheduler and the Refresh poller**, and the Invitation path now has a delivery seam of its own rather than calling `SMTPMailer.Send` from a service method.
