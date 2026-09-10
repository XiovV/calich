# A Linked Calendar is not exposed over CalDAV at all

Status: superseded by ADR-0080

A Linked Calendar appears in **no** principal's CalDAV home-set: not its Owner's, not that of a Workspace Member it was Shared to. `ListCalendars` filters out every Calendar whose Source is a Connection, unconditionally.

## Why the viewer-dependent rule goes

ADR-0054 got the Owner half right and it stands: the person who connected Google almost certainly syncs Google natively on the same phone, and ADR-0033 mints a fresh UUID per row precisely so `{masterId}.ics` stays well-formed — so the native copy and this app's copy share no `UID`, no client can dedupe them, and the user sees every event twice. That reasoning is untouched here.

What changes is the accessor half. ADR-0054 included Shared Linked Calendars in an accessor's home-set because that Member has no native Google copy and nothing to duplicate. True, and it costs a rule where **`ListCalendars` becomes a function of the requesting principal** — ADR-0054 named that cost itself, and banked on #163's synthetic Invitations collection paying it first.

That trade was struck when Linked Calendars were read-only mirrors. They are now writable in both directions, which is where the V1 budget went. Principal-dependent home-set construction is a second subtle rule landing in the same release as write-back, conflict handling and the series mapping — and it is the one whose failure mode is quiet: a home-set that is *correct for the Owner* and wrong for an accessor passes every test written from the Owner's seat. ADR-0054 said as much ("this must be tested from both sides").

Hiding from everyone is the rule that cannot be half-implemented.

## Considered Options

- **Hidden from every principal (chosen).** One rule, no principal-dependence, no duplicates. Costs the accessor case outright: a Shared Linked Calendar is visible in the web app and absent from that Member's phone, with no toggle to fix it.
- **ADR-0054's viewer-dependent defaults.** Right answer per viewer. Costs a principal-dependent `ListCalendars` in the same release as write-back.
- **Exposed to everyone, like Subscriptions (ADR-0032).** Simplest rule of the three, and delivers duplicate-everything on the flagship use case.

## Consequences

- **The accessor gap is real and is accepted, not solved.** A Member Shared a Linked Calendar sees it in the web app and not on their phone, and nothing in the UI explains the asymmetry. If that becomes the top complaint, ADR-0054 is the design to return to — it was not wrong, it was expensive.
- **This is a V1 scope decision wearing an ADR's clothes**, which is why it is written down: the next person to read ADR-0054 will otherwise build the viewer-dependent rule it specifies.
- **The read-only CalDAV privilege set for Linked Calendars becomes dead code before it is written.** Nothing to advertise when nothing is served. Subscribed Calendars keep theirs (ADR-0032) unchanged.
