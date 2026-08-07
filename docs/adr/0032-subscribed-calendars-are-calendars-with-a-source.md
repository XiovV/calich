# Subscribed Calendars are Calendars with a Source

Status: accepted

A **Subscribed Calendar** is a row in `calendars` carrying a source URL, not a parallel entity beside it. Its Events are ordinary `events` rows. Everything a User can see about it — name, Calendar color, the sidebar checkbox, Occurrence expansion, the CalDAV collection — is the machinery Calendars already have. The single thing that makes it different is that it is **read-only**: only Refresh writes its Events.

## Why

The alternative was a separate `subscriptions` + `subscribed_events` pair, where writing to it is structurally impossible because no code path exists. That safety is real, and we turned it down anyway.

A subscribed feed needs the genuinely hard half of this app, not the easy half: Recurrence rule expansion, Anchor zones and DST, the half-open all-day convention, Override and Exception reconstruction, overlap layout, Month and Year rendering, and the Calendar object mapping CalDAV addresses (ADR-0025). A parallel table means a second implementation of all of it, or a generalization pass over all of it. Reuse here is not skin-deep, and the precedent already exists: ICS import writes foreign `VEVENT`s into ordinary `events` rows and it works (ADR-0030).

## Considered Options

- **A Calendar with a Source (chosen).** Free rendering, free expansion, free CalDAV surface. The cost is that read-only becomes an invariant the schema does not enforce.
- **Separate `subscriptions` and `subscribed_events` tables.** Writes impossible by construction; every read path needs a second source merged in, and the expansion/CalDAV work is duplicated or generalized.
- **No persistence — proxy the feed on read.** Rejected outright: Reminders fire from a server-side scheduler over a `(lastTick, now]` window (ADR-0021) and CalDAV clients never touch the web API, so data that exists only during a browser request is invisible to both.

## Decisions that follow from it

- **The invariant is enforced in `EventService`, not at the edges.** Each mutating method refuses a Subscribed Calendar. Both the REST handlers and the CalDAV backend already funnel through that layer, so one guard covers both by construction, and the next entry point added is guarded by default rather than unguarded by default. Refresh writes through a single, deliberately-named bypass documented as the only legitimate writer.
- **Native clients see them, marked read-only.** They appear as CalDAV collections (they already would — `ListCalendars` derives from `CalendarService.List`) advertising a `current-user-privilege-set` without write. Hiding them would defeat the point: a subscription invisible on the phone just gets added again on the phone, and now it is managed in two places on two schedules. Client support for the privilege set is good but not universal, so it makes the UI honest — it does not replace the guards.
- **`PROPPATCH` is rejected on them.** Calendar rename over CalDAV (#70) does not apply to a collection we have just told the client is read-only. Renaming and recolouring stay web-app actions.
- **Name and Calendar color follow the publisher until the User touches them.** The last-seen feed values are stored in shadow columns; a Refresh updates the displayed value only while it still equals its shadow. That is the "overridden" flag, without a flag: an untouched Calendar tracks the publisher's renames, a renamed one stays renamed, and there is no third state. Absent an `X-APPLE-CALENDAR-COLOR`, the import color rotation supplies one.
- **The feed's `VALARM`s are opt-in per Subscription, off by default, and both Channels are honoured when on.** Off by default because the ADR-0030 guard does not survive here — that ADR let alarms import faithfully *because a human was watching a one-time act*, and a poller restores exactly the sync-daemon conditions (no human, repeated, automatic) that made silent normalization right for CalDAV in ADR-0026. Opt-in restores the missing consent moment, and once given we honour Email as well as Notification, keeping import and Refresh behaving identically rather than making the User learn two rules. The residual risk is named rather than designed away: a publisher can add an `ACTION:EMAIL` alarm to a recurring Event next month, and this instance's mailer will send it with no fresh confirmation.
- **Subscribed Calendars are excluded from both export surfaces,** and the bulk action is relabelled "Download my calendars" so the omission is disclosed rather than silent. A frozen snapshot is the wrong artifact for the job bulk export serves — migrating to another app wants the *URL*, so the zip carries the Subscription URLs as a text entry instead.
- **User-supplied URLs are fetched without private-address blocking,** and may carry HTTP Basic credentials in their userinfo, stored as given.

## Consequences

- **Read-only is a convention the database will not defend.** Its failure mode is a User editing an Event that the next Refresh obliterates. The guard is unit-tested per mutating method for that reason, not for coverage.
- **A third-party password can sit in the SQLite file in plaintext.** Nothing here can hash it — it has to be replayed on every fetch — and there is no keychain to defer to. Rejecting credentialed URLs was the alternative; it does not remove the secret, it just relocates it somewhere less examined. Every surface that renders a Subscription URL must mask the password, including error messages and tooltips.
- **Not blocking private addresses is safe only while this is a single-user instance.** The person supplying the URL already has authority over the machine, so there is no privilege boundary for SSRF to cross, and blocking would break the plausible LAN-feed case. **The day a second User can add a Subscription, this becomes a live vulnerability** — internal hosts become probeable through response timing and error text. ADR-0010 deliberately left that door open, so this is the tripwire on it. If blocking is ever added it must re-check after every redirect, not only the URL as typed.
