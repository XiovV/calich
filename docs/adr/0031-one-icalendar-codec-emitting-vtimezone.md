# One iCalendar codec, now emitting VTIMEZONE

Status: accepted

The iCalendar encoder and decoder move out of `caldavserver` into `internal/icalendar`, shared by CalDAV and ICS import/export, and the encoder starts emitting a `VTIMEZONE` component for every `TZID` it references — **in both outputs, from one unconditional code path**.

## Why extract

ICS export needs byte-identical serialization to what CalDAV serves: a Calendar object is a Calendar object whichever door it leaves by. Leaving the codec in `caldavserver` would mean non-CalDAV features importing a package named for the CalDAV server, and the alternative — a second, looser parser for foreign files — is exactly the drift that ADR-0016 and ADR-0020 store domain concepts in their iCalendar shape to avoid. Import still needs one new layer *above* the codec: `parseCalendarObject` requires a single `UID` per call, while an export file carries many, so grouping VEVENTs by `UID` happens before the codec is invoked.

## Why VTIMEZONE

The encoder previously wrote `DTSTART;TZID=Europe/Berlin` with no `VTIMEZONE` definition in the file. CalDAV clients tolerate this — they carry their own zone database — but an export file is consumed by arbitrary importers, and strict ones (Outlook desktop especially) reject or misread a `TZID` they cannot resolve locally. RFC 4791 expects the definition to travel with the object.

Converting everything to UTC `Z` on export was rejected despite being universally accepted: it is *wrong for recurrence*. A weekly 09:00 Berlin standup exported as `08:00Z` silently becomes 10:00 Berlin after the DST switch. ADR-0019 exists precisely to prevent that.

## How the blocks are generated

Go does not expose zone transitions — `Location.tx` and the TZ extend string are unexported, and no Go library generates VTIMEZONE from IANA data. So the generator **probes** `t.In(loc).Zone()` across the current year to find the transitions, then **infers a `FREQ=YEARLY` rule** from each (`BYMONTH=3;BYDAY=-1SU` and so on), emitting one `STANDARD` plus one `DAYLIGHT` observance — or a single `STANDARD` for a zone without DST.

The known cost: **events that predate the zone's current rule get today's rule applied**, so a 2005 US event can display an hour out in a strict consumer. This is what Google and Apple emit too. The alternatives were emitting historical transitions as `RDATE`s (whose behaviour past the last date is undefined — clients freeze the final offset, breaking open-ended recurring events, which is worse than the inference error) or vendoring a vzic-generated zone database (historically exact, but a megabyte of embedded data plus a regeneration chore whose staleness is its own silent bug).

## Consequences

- **Every CalDAV ETag changes once.** ETags derive from the normalized reconstruction (ADR-0026's re-sync-loop guard), so adding VTIMEZONE to the shared encoder makes every connected device refetch every object on the next sync. This converges rather than loops — precisely because of that ETag rule — and is accepted deliberately in exchange for one code path. A future debugger seeing "all ETags changed on one deploy" should look here, not for a bug.
- Emitting from one path rather than a caller flag is the point: a codec producing two different byte-streams depending on who asked is two encoders wearing a trenchcoat, and they would drift.
