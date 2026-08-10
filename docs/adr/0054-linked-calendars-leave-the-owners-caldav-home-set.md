# A Linked Calendar leaves its Owner's CalDAV home-set, but stays in an accessor's

Status: accepted

A Linked Calendar is excluded by default from the CalDAV home-set of the User whose Connection produced it, and included by default in the home-set of any Workspace Member it was Shared to. Home-set membership therefore depends on who is asking. Both defaults are overridable per Linked Calendar.

## Why ADR-0032's reasoning inverts here

ADR-0032 exposed Subscribed Calendars over CalDAV and argued the point directly: hiding them "would defeat the point — a subscription invisible on the phone just gets added again on the phone, and now it is managed in two places on two schedules."

That argument depends entirely on the calendar **not already being on the phone**. For a Google calendar it already is. iOS and Android both sync Google natively, and the person most likely to connect Google to this app is the person most likely to have Google on their phone. Expose the Linked Calendar over CalDAV as well, and the moment they add this instance to their device they see **every Google event twice**.

There is no client-side rescue. ADR-0033 mints a fresh UUID for each row precisely so that `{masterId}.ics` stays well-formed, so the native copy and this app's copy share no `UID` and no client can dedupe them. Users will read this as a bug in this app, and they will be about right.

So ADR-0032's premise — *"absent here means re-added there"* — becomes *"present here means duplicated there"*. Same reasoning, opposite input, opposite conclusion.

## Why it inverts again for an accessor

A Workspace Member who was *Shared* a Linked Calendar has no native Google copy of it — they are not the one with the Google account. For them there is nothing to duplicate, and ADR-0035 already places Shared Calendars in the accessor's home-set. Excluding it would just mean a Calendar they can see in the web app silently missing from their phone, with the fix buried in someone else's settings.

The duplicate risk belongs specifically to the connecting User, so the exclusion should too.

## Considered Options

- **Viewer-dependent defaults (chosen).** Each default is right for whoever is asking. Costs a rule that is genuinely subtle to hold in your head.
- **Exposed to everyone, like Subscriptions.** One rule for every Source kind, nothing viewer-dependent. Costs an out-of-the-box duplicate-everything experience on the flagship use case.
- **Hidden from everyone, opt-in per Linked Calendar.** One simple rule, no duplicates by default. Costs the accessor case: a Shared Linked Calendar is missing from their phone until its Owner thinks to flip a toggle on their behalf.

## Consequences

- **`ListCalendars` becomes a function of the requesting principal, not just of the Calendar set** — a real complication in a path ADR-0032 could treat as a straight derivation from `CalendarService.List`. It is cheaper than it first appears, because #163 already crosses that line for its own reasons: the synthetic Invitations collection appears only for principals who have an Attendee-only Event. Whichever of the two lands first pays the cost, and the second should reuse the shape rather than adding a second principal-dependent branch beside it.
- **This must be tested from both sides.** A Linked Calendar's Owner and a Member it is Shared to hitting the same home-set and getting different collections is the assertion; testing only the Owner's view would pass while the accessor case is broken.
- **The default is a default, not a policy.** Someone who has removed Google from their phone and uses this app as their only client needs the Owner-side toggle, and it should be discoverable at the point where the duplication would otherwise surprise them.
- **Neither default changes the read-only privilege set.** A Linked Calendar that *is* exposed advertises `current-user-privilege-set` without write, and rejects `PROPPATCH`, exactly as ADR-0032 established for Subscribed Calendars.
