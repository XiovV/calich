# Calendar Providers integrate over their own APIs, not over CalDAV

Status: accepted

Google Calendar is reached through **Calendar API v3**, behind a **Provider** interface that Microsoft Graph will implement next. This instance already speaks CalDAV fluently — it *is* a CalDAV server (ADR-0023) and owns one iCalendar codec (ADR-0031) — so writing a CalDAV *client* and pointing it at Google is the obvious move. It is not the one we made.

## Why

Google publishes a CalDAV v2 endpoint over OAuth 2.0 and it works. Pointing a CalDAV client at it would reuse the codec verbatim: Events arrive as `VEVENT`, and Master/Override/Exception reconstruction is a solved problem the Refresh reconciler already performs (ADR-0033). The same client would then reach iCloud, Fastmail, Nextcloud and any generic CalDAV server for free — arguably worth more to a self-hoster than Outlook is.

**Microsoft has never implemented CalDAV.** Not Outlook.com, not Microsoft 365, not at any point. The Outlook REST API v2.0 was decommissioned in March 2024 and Graph is the only remaining door. So the moment Outlook is in scope — and it was in scope from the first sentence of this feature's request — a per-Provider interface exists regardless. That collapses the codec-reuse argument from "reuse across all Providers" to "reuse for exactly one Provider, while the other one needs a bespoke JSON mapper anyway".

Once the interface is being built either way, the question is only which side of it Google sits on, and the API wins on merit:

- **`syncToken` incremental sync.** An unchanged calendar costs one request returning an empty item list. CalDAV's equivalent depends on Google's `sync-collection` support, which is less exercised and less documented than the API path.
- **Metadata CalDAV cannot express.** `accessRole` (needed to know a calendar was shared *to* the user), the `selected` flag mirroring Google's own sidebar (the default for calendar selection, ADR-0052), `conferenceData` for the Meet link, and `colorId`. All of these are load-bearing for the feature as specified; none survives a trip through `VEVENT`.
- **Google's CalDAV endpoint is a maintenance backwater.** The `https://www.google.com/calendar/dav` endpoint is already dead, and the v2 endpoint's quotas were re-cut in May 2026. It is supported, not invested in.

## Considered Options

- **Google over Calendar API v3, behind a Provider interface (chosen).** Costs a JSON-to-domain mapper written from scratch, and a second one for Graph.
- **Google over a CalDAV client.** Reuses the codec and the reconciler almost verbatim, and delivers iCloud, Fastmail and Nextcloud as a side effect. Loses `syncToken`, `accessRole` and every field above. Does not move Outlook one inch closer.
- **CalDAV client first, native adapters later.** Widest coverage soonest, at the price of two Google code paths and a migration between them for anyone who connected early.

A generic CalDAV client remains a good idea *on its own merits*, as a third Source kind serving iCloud and Nextcloud. It is not the way to reach Google, and this ADR does not rule it out.

## Decisions that follow from it

- **The interface is the seam, and it is narrow on purpose.** A Provider lists the calendars an authorized account can see, and returns a change set for one calendar given an optional cursor — nothing more. Token exchange and refresh sit behind it too, because Graph's and Google's differ. Everything downstream of the change set — reconciliation, tombstones, expansion, rendering, CalDAV exposure — is Provider-agnostic and shared, which is what makes the second Provider cheap.
- **The scopes requested are the narrowest pair that works:** `calendar.calendarlist.readonly` and `calendar.events.readonly`, not the broad `calendar.readonly`. Write scope is deliberately *not* requested ahead of need, accepting that adding write-back later forces a re-consent. Holding a write grant this app cannot yet use is the worse trade — the consent screen would overstate what the app does at exactly the moment a user is deciding whether to trust it.
- **The mapper is a pure function and is table-tested.** Provider JSON in, domain series out, no clock and no database — following `recurrence.Expand`, `reminder.DueAll` and the ADR-0033 reconciler. This is where every Provider's edge cases will land.

## Consequences

- **Two mappers exist forever, and they will drift.** Nothing structural prevents the Google mapper from handling a case the Graph mapper does not. The shared change-set type is the only forcing function, so it must stay expressive enough that a Provider cannot smuggle behaviour past it.
- **`RDATE` and `EXRULE` have no home.** Google's `recurrence` array may carry either; this app models `RRULE` plus `EXDATE` (ADR-0016) and nothing else. These must be *counted and surfaced* rather than silently dropped, in the spirit of the Import summary (ADR-0030) — a recurring Event that quietly loses half its instances is the worst possible failure here, because it looks like it worked.
- **The all-day and timezone mappings are free.** Google's `end.date` on an all-day event is already exclusive, matching ADR-0017's half-open convention exactly, and `start.timeZone` maps straight onto the Anchor zone of ADR-0019. Neither needed a conversion layer, which is worth recording so nobody adds one.
