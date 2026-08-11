# Attendees are named when an Event is created, and the Organizer is not one of them

Status: accepted — extends ADR-0046

ADR-0046 gave Events Attendees but said nothing about *when* they can be named. The implementation that followed made invites reachable only through the edit modal, so inviting anyone meant creating the Event, closing the modal, reopening it, and inviting from there — the invite is part of arranging a meeting, not an afterthought to it. Attendees now travel in the create request (`attendeeUserIds`, `attendeeGroupIds`) and are written inside the transaction that writes the Event. Deciding that surfaced two things worth recording: how a bad target behaves, and what the creator is.

## The Organizer is not an Attendee

Once the Attendee list is visible while creating an Event, the person creating it is conspicuously missing from it, and the obvious fix is to insert them as an Attendee with a response of Accepted — which is roughly what Google Calendar appears to do, showing the organizer inside the guest list.

We don't. The creator stays on `events.created_by` and is rendered as a non-removable **Organizer** row above the Attendees. No Attendee row, no `role` column.

The reason is that Organizer and Attendee are different roles, and iCalendar has kept them as different properties (`ORGANIZER`, `ATTENDEE`) since RFC 5545 — Outlook and Apple Calendar both keep them visually separate too, so the mainstream convention is the split, not the collapse. This app already *has* the split in its schema; `created_by` is the Organizer. Inserting the creator into `attendees` would collapse it, and the collapse is not cosmetic: `attendees` has no role, so the creator becomes an ordinary row that `RemoveAttendee` will happily delete and `SetResponse` will happily set to Declined. An Organizer who has declined their own Event is not a state worth being able to represent.

It also keeps ADR-0046's Reminder fan-out arithmetic honest. "Everyone with Access, unioned with every Attendee" stays a union of two disjoint-in-intent sets rather than one that always contains the creator twice.

This is the narrow version. A real Organizer role — reassignment, an `ORGANIZER` property in the codec, guards on removal and response — remains deferred, as ADR-0046 left it.

## Explicit targets fail the create; Group members are skipped

A create carrying Attendees can be rejected for reasons the Event itself is fine: an unknown user, a target outside the Workspace, a bad group id. Two policies apply, depending on who named the target.

**Explicit targets are strict.** Any invalid `attendeeUserId` or `attendeeGroupId` fails the whole request — 400, transaction rolled back, no Event created — and fails on the first one found rather than collecting them.

**Group expansion stays lenient.** Inside an otherwise-valid Group, the skip rules `AddGroupAttendee` already applies hold: disabled members and existing Attendees are passed over silently. A User named both individually and through a Group is deduplicated without complaint, as the `(event_id, user_id)` primary key requires.

The asymmetry is the point. The caller named that individual, so refusing loudly is the correct answer; they did not name each member of the Group, they named the Group, and the Group's own snapshot semantics govern what that expands to. Failing a fourteen-person team invite because one member's account is disabled would be a worse answer than the one ADR-0046 already settled on.

## Consequences

- **Create is no longer fire-and-forget when Attendees are staged.** The POST is awaited and the optimistic insert moves behind it, so a rejected create never paints an Event on the grid and never discards what the user typed. This mirrors the path staged Attachments already take.
- **Attendee inserts move inside `EventService.Create`'s transaction.** The membership and disabled-user checks must therefore use the transaction-bound repositories — a pooled call inside `withTx` deadlocks against the single-connection test database.
- **The error envelope is unchanged, so the client cannot say *which* target was rejected.** Responses are `{"error": {code, message}}` everywhere, with no structured-detail channel; the modal shows a banner naming the kind of failure and refreshes its member list. Since the picker only ever offers current Workspace members, a rejected explicit target means the list went stale, which the refresh corrects. Extending the envelope app-wide to point at a row was judged out of proportion to that.
- **Staging applies to creation only.** In the edit modal an invite still commits when it is issued, not when the Event is saved, matching how Attachments already behave there. Cancelling an edit therefore does not rescind an invite made during it.
- **A staged Group expands at save, not at pick.** "Invite time" in ADR-0046 means the write, so a Group picked and saved minutes later snapshots the membership as of the save.

## Considered Options

- **Client-side fan-out after create (rejected).** Await `POST /api/events`, then issue one attendee call per target — no backend change, and an exact copy of the Attachment flow. Rejected because it makes "the Event exists, the invites silently didn't" reachable on every create, with an optimistically inserted Event already on the grid and no way to unwind it. Putting the writes in the create transaction makes the failure mode "nothing happened" instead.
- **Client-side Group expansion (rejected).** Expanding a Group at pick time would let the user see the fourteen people they are about to invite and drop one before saving — genuinely better feedback. Rejected because it duplicates the disabled-member filter on the client and invents a partially-expanded Group invite, which is no longer a snapshot of the Group and is not a thing the edit path can produce.
- **Uniform strictness, either direction (rejected).** Strict everywhere fails a whole team invite over one disabled account. Lenient everywhere returns an Event quietly missing the guest the caller explicitly asked for, which is the outcome the transaction was chosen to prevent.
