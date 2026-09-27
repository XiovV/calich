# Availability Schedules are separate from the Working hours Preference

Status: accepted — preserves the boundary ADR-0039 drew around Preferences; the Working hours entry in CONTEXT.md keeps its "carries no availability meaning" clause unchanged

A Booking Link derives its slots from an **Availability Schedule**: a named, per-User weekly pattern of time ranges carrying its own IANA timezone. One is auto-created per User, named **Default** and seeded once from that User's Working hours Preference, and the two are separate objects from that moment on.

## Why not simply promote the Preference

The Working hours Preference already stores a daily range and already means "my working day", so reusing it is the obvious move. It is also nearly free: two existing columns, plus a weekday mask they currently lack.

It was refused because it changes what a field is, not just what it does. Today Working hours shades the region outside it in Day and Week view and nothing else — CONTEXT.md says so twice, and ADR-0039 put it among the Preferences on the explicit grounds that "none of them changes what an Event *is*". Promoting it makes a **display preference into a public contract**. A User who nudges the shading because they want to see more of their morning has, without being told, opened themselves to strangers at 07:00. The gesture that changes it is a visual one; the consequence is not.

The reverse failure is as bad and less obvious: someone who wants their bookable window narrower than their working day cannot have it, because the same number does both jobs.

There is a structural objection too. Working hours is **one range per day**, identical Monday to Sunday. Real availability is at least a weekly pattern — the mockup itself reads "Mon–Fri, 9–17" — and frequently a split one (09:00–12:00 and 14:00–17:00). Extending the Preference to carry weekdays and multiple ranges per day is no longer a Preference; it is this entity, stored in the wrong place and reachable from the wrong screen.

## Why the Schedule carries a timezone and the Preference does not

A Preference renders for one person on one screen, so it can be interpreted in whatever zone that person's browser reports. A Schedule is read by strangers to decide when someone is free, so it needs a fixed anchor for the same reason an Event does (ADR-0019): "9–17" must mean the same hours across a DST change, and must not move when its owner opens a laptop in another country. The zone on a Schedule is an Anchor zone in everything but name.

Slots are derived in the Schedule's zone and rendered in the visitor's own, detected from their browser and correctable on the page. The visitor's zone is render-time only and is stored nowhere, which is exactly the Viewer zone rule already in the model.

## Why it is seeded, and why only once

Seeding costs one insert and makes the feature true on first sight: a User whose Working hours are 09:00–17:00 opens the new-link modal and reads back the hours they already told the app about. Not seeding means the first Booking Link anyone creates is unbookable until they go and configure something else.

**The link is then cut.** A live link — the Schedule tracking the Preference until touched — is the shadow-tracking pattern from ADR-0032, and it is wrong here for the same reason the promotion was: it leaves a grid-shading control silently wired to a public contract, and it does so in the hardest way to notice, because the wire is invisible until the day someone changes their shading.

The seeded Schedule is named **Default**, not "Working hours". Two different objects sharing one name in one UI is the confusion this entire ADR exists to prevent, and the User may rename it.

## Decision

- **`availability_schedules`**, per User: name, IANA `tzid`, and a set of weekly ranges (weekday, start minute, end minute), several per weekday permitted. Append-only migration per ADR-0048.
- **One auto-created per User named "Default"**, seeded from `working_hours_start` / `working_hours_end` across Mon–Fri when those are set, and 09:00–17:00 Mon–Fri when they are not. Created lazily, at that User's first Booking Link.
- **Seeded once, never synchronised.** Editing the Working hours Preference afterwards has no effect on any Schedule, ever.
- **Reusable.** A Booking Link references a Schedule; several links may share one. Deleting a Schedule still referenced is refused.
- **Working hours is unchanged** — same columns, same meaning, same "visual hint only, carries no availability meaning" clause in CONTEXT.md, same absence of a weekday dimension.
- **Schedules live in Settings**, beside the other per-User state, not inside the Booking Link modal, which picks one from a dropdown.

## Considered Options

- **A separate reusable entity with its own timezone, seeded once (chosen).**
- **Promote the Working hours Preference** by adding weekdays. Cheapest, and it turns a display control into a public contract with no gesture marking the change.
- **A live-tracking Schedule** that follows the Preference until touched, after ADR-0032's shadow-tracking. Same hazard as promotion, hidden behind one more indirection.
- **Inline hours on each Booking Link**, with no reusable entity. Fine at one link; at five, changing your hours means five edits and one of them gets missed.
- **Seed nothing.** No confusion, and the first link anyone creates is unbookable until they discover a second screen.

## Consequences

- **Two things in the UI now describe a working day**, and a User will eventually change one expecting the other to follow. The naming ("Default", not "Working hours") and their placement in different Settings Sections are the only defences; this is the accepted cost of not wiring them together.
- **Schedules are the second per-User entity carrying a timezone**, after the Event's Anchor zone. DST correctness must be tested against them independently — a range spanning a transition is the case to cover.
- **Deriving availability is a server-side job.** The public page has no Session, so slot derivation runs on the backend, expanding Busy Events with `recurrence.ExpandOccurrences` — which already exists and which the reminder scheduler already uses, so ADR-0016's frontend-expansion position is untouched for the authenticated app.
- **Nothing forces a User to have a sensible Schedule.** An empty one is legal and yields a link with no slots. The UI should say so; the model does not prevent it.
