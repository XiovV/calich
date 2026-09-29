# Exposure: a Calendar's CalDAV home-set membership becomes a per-User choice

Status: accepted — supersedes ADR-0074, reintroducing ADR-0054's viewer-dependent design

Whether a Calendar appears in a User's own CalDAV home-set is now that User's own choice, resolved per `(calendar_id, user_id)` from `calendar_exposures` — a table reusing `calendar_user_colors`' shape verbatim (ADR-0038): no indirection, cascading on both Calendar and User. Absent a row, the default is exposed, except a Linked Calendar's own Owner, who defaults to unexposed. The home-set filter and the direct-path lookup both consult it for **every** Calendar, not only Connection-sourced ones, even though only a Linked Calendar gets a UI toggle today (Not in scope, below). Read-only when this landed — a device's own write surviving Exposure is ADR-0081 (#299).

## Why the cost ADR-0074 refused to pay is already paid

ADR-0074 rejected ADR-0054's viewer-dependent home-set for one named reason: it would make `ListCalendars` "a function of the requesting principal, not just of the Calendar set," and landing that in the same release as write-back, conflict handling, and the series mapping risked a quiet failure mode — a home-set correct for the Owner and wrong for an accessor, passing every test written from the Owner's seat alone. ADR-0054 had named the same cost itself and bet that #163's Invitations collection would pay it first.

That bet paid off before this ADR needed it to. #163 shipped a principal-dependent `ListCalendars` for its own, unrelated reason: `caldavserver/calendars.go`'s `ListCalendars` already runs `ListAccessible` and `ListAttendeeOnlySeries` side by side and appends a synthetic Invitations collection only for a principal who actually holds an Attendee-only Event. The shape ADR-0074 was trying to avoid being first to introduce has already shipped, under a different name, for a different reason — reintroducing it for Exposure adds a second branch to code that already has one, not a new kind of risk to the codebase.

This is why this reads as a return to ADR-0054 rather than a reversal of ADR-0074's taste: the thing ADR-0074 refused to be first to risk is a bill this codebase already paid.

## What's actually different from ADR-0054's design

ADR-0054 specified a Calendar-scoped toggle, overridable "per Linked Calendar" — a column or sibling row keyed on the Calendar alone would have made sense for a single-User instance, but conflates two different people's answers to two different questions: the Owner's own preference and a Share-holder's own preference are not the same fact, and a Workspace can hold several of the latter for one Calendar. Exposure is keyed `(calendar_id, user_id)` instead, one independent answer per person who can see the Calendar at all — the shape `calendar_user_colors` already established for exactly this reason (ADR-0038).

## Decision

- `calendar_exposures(calendar_id, user_id, exposed)`, keyed and cascaded exactly like `calendar_user_colors`. Lands as a new append-only migration (`00007_calendar_exposure.sql`) rather than hand-edited into `00001_init.sql`, matching how `00002`–`00006` already added onto the last squash instead of folding into it — ADR-0048's periodic squash is a deliberate, occasional act, not something every schema change since the last one performs. Editing `00001_init.sql` directly would silently strand any database that already applied it through version 6: goose tracks applied versions by number, not content, so it would never re-run a changed version 1.
- Absent a row, the default resolves as: ordinary Calendar → exposed; Subscribed Calendar → exposed (ADR-0032 unchanged); Linked Calendar, requesting principal is Owner → not exposed; Linked Calendar, requesting principal holds a Share → exposed.
- The home-set filter (`ListCalendars`) and every entry point that resolves a Calendar straight off a request path — `GetCalendar` (PROPFIND), calendar-query, calendar-multiget, sync-collection, and PROPPATCH — resolve Exposure for every Calendar the caller can otherwise reach, mirroring the shape #163 established for the Invitations collection: not exposed reads as absent from the listing and 404 on a guessed, bookmarked, or already-synced collection URL, identically. A device that discovered and cached the collection before Exposure was turned off must lose it there too, not just on its next PROPFIND.
- **Access resolves first, Exposure second.** A Calendar the caller has no Access to is never in the accessible set Exposure is even asked about, so a revoked Share drops it regardless of what any leftover Exposure row says. `RevokeShare` and `LeaveShare` clear the caller's own Exposure row alongside their colour override, for the same reason ADR-0038 gives: a choice with no Access behind it would otherwise linger, invisible, until Access was ever granted again.
- The setting is read/write on its own endpoint (`PUT /api/calendars/{id}/exposure`), open to any User with Access — Owner and accessor alike — writing only the caller's own row. This is *not* folded into the general Calendar-update endpoint the way ADR-0038's colour override is: colour has an Owner's-own-value to fall back on when the caller isn't the Owner, and Exposure has none — every principal, Owner included, only ever writes their own row, so there is no owner-write/accessor-override branch to reuse.
- The UI toggle lives in a Linked Calendar's existing sidebar row menu, the one surface both the Owner and an accessor already reach — not Settings → Connections, where an accessor has no Connection to find it under and the Owner's own seat would default to on, which is backwards. Its label speaks in terms of devices, never CalDAV.
- The first App Password a User creates nudges them toward Exposure if they own a Linked Calendar still on the unexposed default — the same moment and pattern ADR-0027 established for the reminder-delivery toggle: the point where a User first demonstrates they have a device to expose *to*.

## Considered Options

- **Reintroduce ADR-0054's viewer-dependent defaults, now that #163 already pays the shared cost (chosen).**
- **Keep ADR-0074's blanket hide.** Cheapest to leave alone, but leaves the accessor's phone permanently empty and gives the Owner who has dropped native Google sync no way back onto CalDAV at all.
- **A per-Connection bulk toggle instead of a per-User, per-Calendar row.** Coarser and cheaper to build, but the wrong shape: the Owner's own preference and an accessor's own preference are different people answering different questions about the same Calendar, and one switch on the Connection can't hold both independently.

## Consequences

- **The accessor gap ADR-0074 accepted is closed.** A Workspace Member Shared a Linked Calendar now sees it on their phone by default, with no toggle to find and no dependency on the Owner doing anything on their behalf.
- **The Owner who has moved off native Google sync gets their own Linked Calendar back on CalDAV**, discoverable at the moment (first App Password) where its absence would otherwise be a silent surprise.
- **`linked_calendar_hidden_test.go`'s accessor-seat assertions invert**: the Calendar that test used to prove was absent from a Shared Member's home-set is now proven present by default, while the Owner's own exclusion — the same test file's other half — stays true as a default rather than an unconditional rule.
- **Ordinary and Subscribed Calendars carry Exposure too, with no UI to set it.** Accepted rather than special-cased away: the resolution logic asks the same question of every Calendar, so a future ADR can turn on a toggle for them without touching this one again.
- **The read-only CalDAV privilege set for a Linked Calendar, dead code since ADR-0074, is live again** for exactly the principals Exposure lets reach a collection at all — asserted directly, per #297's acceptance criteria, since it has never actually run against a Linked Calendar before now.
