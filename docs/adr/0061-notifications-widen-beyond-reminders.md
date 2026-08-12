# A Notification is any in-app record that concerns a User, not only a fired Reminder

Status: accepted — reopens ADR-0021's definition of a Notification

ADR-0021 defined a Notification as what a Notification-Channel Reminder produces when it fires, and the table matches that definition literally: `occurrence_start NOT NULL`, `title`, `fired_at`, written only by `reminder.NotificationDispatcher`. Being invited to an Event should reach the same feed, and doesn't fit that shape — an invite is about the *series*, not one Occurrence, so a recurring standup produces one record rather than one per expansion, and `occurrence_start` has nothing to say.

## The term widens; the table gains a `kind`

A Notification becomes the persistent in-app record of something that concerns one User. `notifications` gains `kind` (`reminder` | `invite`) and `occurrence_start` becomes nullable, meaningful for `reminder` only. One feed, one unseen count, one `MarkAllSeen`, one poll.

**Not derived from `attendees`.** Unioning "rows where I'm `needs-action`" into the feed writes nothing and stays automatically in sync, and fails on its own terms: the entry vanishes the instant you RSVP, it cannot be marked seen independently of answering it, and it makes "you were invited to this" stop being true retroactively — which it isn't. You were.

**Not a second table.** The bell, the seen flag, the fetch and the API shape are identical either way; a separate table means maintaining two of each to avoid one nullable column.

## Only being invited produces one

An `invite` Notification is written when an Attendee row naming a User is written, and nothing else in this round produces a Notification:

- **Not being removed as an Attendee.** Removal is usually a mistake corrected seconds later, and "you're no longer invited" is a message most products deliberately don't send.
- **Not the Event moving or being cancelled.** Genuinely missing behaviour, but it is a change-notification feature with its own questions — what counts as material, whether it re-opens your Response the way an iCalendar `SEQUENCE` bump does — and it deserves its own decision rather than arriving as a rider.
- **Not a Response arriving.** A fourteen-person team invite would become fourteen bell entries over the following day. The Attendee list in the modal already shows every Response with a live tally, which is a strictly better pull surface than fourteen pushes.

The Organizer never receives an `invite` Notification for their own Event, which follows from ADR-0055 rather than being a rule of its own: an Organizer is never an Attendee.

## Consequences

- **ADR-0021's clean line stops being true.** "A Notification is what a Notification-Channel Reminder produces" no longer holds, and `Channel` no longer explains where every Notification comes from — `kind` does.
- **An accepted asymmetry with ADR-0059.** An Attendee who isn't a Member gets mail when the meeting moves or is cancelled; a Member gets nothing in the app for the same events. The person with an account is, in that narrow sense, less informed than the stranger. Accepted because a Member sees the change on their grid and a recipient has no grid — but it is a choice, and it is the strongest argument for picking up the deferred change-notification work.
