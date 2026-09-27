# Busy is TRANSP, and all-day Events default to Free

Status: accepted — adds the first Event field that exists to be read by someone who is not a User; deliberately departs from RFC 5545's default for all-day Events

An Event carries **Busy** or **Free**, stored as iCalendar's `TRANSP` (`OPAQUE` / `TRANSPARENT`). Only a Busy Event removes a slot from a Booking Link. A timed Event defaults to **Busy**; an all-day Event defaults to **Free**.

## Why the field is `TRANSP` and not an invention

Free/busy transparency is already modelled in iCalendar, already supported by every CalDAV client, and already present on Google's API as `transparency`. Storing anything else means translating at three boundaries — CalDAV out, Google in, Google out — and losing information at each. ADR-0026 accepts lossy round-trips where there is no alternative; here there is one, for free.

The glossary term is **Busy** rather than `TRANSP`, following the same rule that gave us Anchor zone over `TZID`, Deadline over `DUE` and Completion over `STATUS`: the wire name is not the domain name.

## Why all-day Events default to Free

RFC 5545 defaults every `VEVENT` to `OPAQUE`, and the consistent choice is to inherit that for both kinds. This ADR does not, and the departure is the reason it exists.

Almost every all-day Event in a real calendar is an annotation rather than an occupation. Birthdays, name days, public holidays, "launch week", "Q3 planning", someone else's leave — all all-day, none of them a reason a stranger cannot have thirty minutes. Meanwhile a single **Subscribed Calendar** of public holidays is a handful of clicks and a dozen all-day Events a year, and ADR-0012 and ADR-0032 made such feeds easy on purpose.

Under the RFC default, subscribing to a holiday feed silently closes twelve whole days a User never thought about, on a public page they cannot see themselves. The damage is invisible from inside the app: the grid looks fine, the link looks fine, and the only symptom is bookings that never arrive.

Under the chosen default, the failure inverts: a User who takes a day off by creating an all-day "PTO" Event, and never opens **More options**, stays bookable through it. That failure is **visible and self-correcting** — a booking lands in their calendar on the day they are away, they see it, they flip the Event to Busy. The first failure produces silence, which nobody debugs.

So the rule is: the default is chosen for the common case (annotations, which outnumber occupations heavily), and the uncommon case fails loudly rather than quietly.

## Decision

- **`transp` on `events`**, defaulting to `OPAQUE` at the column level, with the **all-day path writing `TRANSPARENT`** on create — so the departure lives in one place and the column keeps the RFC's own default for everything else.
- **Independently set on a Master and on an Override**, since an Override is already a complete standalone instance (ADR-0016). "The Monday standup is Busy, except on the 14th" needs no Exception.
- **Behind More options in the Event modal**, per ADR-0056's progressive disclosure. It is not on the default face; most Events never need it touched.
- **Round-trips in both directions.** The codec emits it (ADR-0031), CalDAV serves it, the Google mapper reads and writes `transparency`, and an imported Event keeps whatever its file said — including an all-day one marked `OPAQUE`, which is honoured rather than re-defaulted.
- **A Task has no Busy.** A Task has no Calendar, never reaches CalDAV or a Provider (ADR-0083), and is never shown to a stranger; whether its Time block closes a slot is answered per Booking Link by the Conflict set, not per Task.

## Considered Options

- **`TRANSP`, timed defaults Busy, all-day defaults Free (chosen).**
- **`TRANSP` with the RFC default for both.** One rule, faithful to the spec, and it hands the holiday-feed footgun to every self-hoster who subscribes to one.
- **All-day defaults Free, and Subscribed and Linked Calendars excluded from busy entirely.** Solves the feed problem harder, and breaks the case the feature exists for: a User whose real calendar is Google needs Google conflicts to count.
- **A per-Calendar "count this calendar as busy" flag** instead of a per-Event field. Coarser, does not round-trip, and cannot express "this one meeting is optional" — which is most of what the field is for day to day.
- **No field; every Event blocks.** Ships sooner and makes optional Events, holiday feeds and tentative blocks indistinguishable from real commitments.

## Consequences

- **An imported or synced Event may disagree with the default.** An all-day `OPAQUE` Event arriving from a native client is honoured, so two all-day Events on one grid may behave differently with nothing on screen distinguishing them until the modal is opened. This is correct — the remote client meant it — and is the price of lossless round-tripping.
- **Every existing Event becomes Busy on migration**, including existing all-day ones, because backfilling `TRANSPARENT` onto them would silently change the meaning of data a User already entered. Only Events created after this ships get the all-day default.
- **The field is invisible until someone publishes a Booking Link.** Until then it changes nothing anyone can see, which means it will be wrong on a lot of Events by the time it first matters.
- **Free/busy is still not exposed as a CalDAV `VFREEBUSY` report.** The field is now there to support one if it is ever wanted; nothing serves it in this cut.
