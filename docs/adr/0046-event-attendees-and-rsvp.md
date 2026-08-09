# Events carry Attendees with an RSVP, independent of Calendar Access

Status: accepted — reopens and narrows ADR-0034's stated non-goal ("no invitations, no RSVP, no iTIP/iMIP scheduling"); extends ADR-0035 and ADR-0036

ADR-0034 deliberately fenced attendee invitations out of Calendar sharing: *"this is Calendar sharing, not event attendees... naming them as non-goals now keeps this ADR from being read as covering them."* Workspaces (ADR-0044) and Groups (ADR-0045) make the underlying need concrete — inviting "the Tech team" to a single meeting shouldn't require first sharing the whole Calendar it lives on with them. This ADR narrowly reopens that non-goal: real per-Event invitations with a response, nothing broader (still no free/busy lookup, no full iTIP/iMIP scheduling protocol).

## Attendee invites are independent of Calendar Access

An Event gains **Attendees** — Users invited to that specific Event, targeting either an individual User or a Group (expanded to its current members at invite time, producing one Attendee row per person; unlike a Group Share, an Attendee's own response has to survive that Group's membership changing later, so it cannot be resolved dynamically the way ADR-0045's Group Shares are).

**Being an Attendee grants visibility to that one Event only, and requires no Calendar Access.** Inviting someone to "Discuss tech stack" does not require they already hold a Share on the Calendar it lives on — the invite itself is the grant, scoped to that Event. This mirrors how attendee invites work in every mainstream calendar product, and was chosen deliberately over requiring pre-existing Access: gating invites behind Calendar Access would force an organizer to either share their whole Calendar just to invite one ad hoc guest, or set up a Group Share in advance of every one-off meeting — friction the actual use case (ad hoc meetings) doesn't tolerate.

Both Share targets (ADR-0045) and Attendee targets are restricted to members of the Event's/Calendar's own Workspace — there is no cross-Workspace or external-guest invite in this round.

## Response state: the full iCalendar `PARTSTAT` set

An Attendee's response is one of **Needs-Action** (default, unanswered), **Accepted**, **Declined**, or **Tentative** — iCalendar's `PARTSTAT` values on `ATTENDEE`. This app already commits to modelling iCalendar concepts fully rather than a convenient subset, specifically so CalDAV round-trips don't silently lose data (ADR-0016's `RRULE`, ADR-0020's `VALARM`; ADR-0026 tracks the cases where a round-trip *is* lossy as a deliberate, called-out exception, not a default). A native client responding to an invite can send `TENTATIVE`; modelling only Accepted/Declined would force that response to be coerced or dropped, an unaccounted-for lossy round-trip. The web UI leads with two buttons, Accept and Decline, with Tentative available as a secondary option — that is a UI-surface choice, not a reason to shrink the underlying state.

## Consequences

- **Extends ADR-0035's home-set model.** A User's CalDAV home-set must now also expose Events they're an Attendee of, even when they have no Access to the Calendar those Events live on — a genuine extension to "a User's home-set is everything they have Access to," not a special case layered outside it.
- **Extends ADR-0036's Reminder fan-out.** "Everyone with Access to the Calendar" becomes "everyone with Access to the Calendar, unioned with every Attendee of this Event." An Attendee with no Calendar Access still gets Reminders and Notifications for the Event they were invited to, and still gets the same per-User override behavior ADR-0036 already defines.
- **A departed Attendee's response is not retroactively cleared.** If someone is later removed as an Attendee (event edited, invite rescinded), what happens to their historical response and any Notifications already sent is not decided by this ADR and is left to implementation.
- **No free/busy, no delegation, no iTIP/iMIP wire protocol.** This ADR covers only the Attendee/response model inside this app; interoperating with external calendar systems' scheduling protocol remains out of scope.

## Considered Options

- **Require existing Calendar Access before an invite (rejected).** Keeps a single, uniform authorization question ("does this User have Access to this Calendar") with no second visibility mechanism beside it. Rejected once weighed against the end-user cost: it makes the common ad hoc case — inviting one person to one meeting — require setting up sharing plumbing first, which is worse for the primary use case than the authorization-model cost of a second, narrower visibility grant.
- **Accepted/Declined only, no Tentative (rejected).** Simpler two-state UI. Rejected in favor of lossless CalDAV round-tripping, consistent with how every other iCalendar-backed concept in this app is modelled.
