# ICS import is copy-in, not restore

Status: accepted

Importing a Calendar file mints a **fresh UUID for every series and discards the file's own `UID`**, so an import is a copy of foreign data into this instance, never a reconstruction of state that came from it. Re-importing the same file therefore produces a second copy of every Event, and we say so in the UI rather than deduplicating.

## Why

Backup/restore was explicitly ruled out of scope for ICS: the backup story for a self-hosted single-user instance (ADR-0010) is the SQLite file in the Data directory, not a lossy iCalendar round-trip. The jobs this feature does serve — migrating in from another app, sending one Event to a person, receiving one emailed invite — never need a foreign `UID` to survive.

Preserving the incoming `UID` would also break an existing invariant: an Event's id *is* its CalDAV `UID` and its `{masterId}.ics` path segment (ADR-0025), and the API rejects any id that isn't a UUID. Foreign UIDs (`…@google.com`, Outlook's 200-character hex) would put arbitrary strings into sync URLs and force collision handling, all to serve a job we cut.

## Decisions that follow from it

- **No dedupe on import.** Nothing is matched against existing Events — not by UID, not by title+time. Consistent with ADR-0020's no-dedupe stance on Reminders.
- **Undo is "delete the Calendar".** Import defaults to creating a new Calendar (name from `X-WR-CALNAME`, falling back to the filename), so a mistaken import is removed by deleting it. A `.zip` always creates one new Calendar per entry; only a single-file `.ics` import may target an existing Calendar, which is the emailed-invite case.
- **Failures are per-series, not per-file.** A series is skipped and reported with a reason — whether the file's bytes couldn't be decoded into one, or the series decoded perfectly and this app has no way to store it (a blank `SUMMARY`, a `DTEND` before its `DTSTART`) — and the rest import. All-or-nothing would let one malformed 2003 VEVENT block an entire decade-long migration the user cannot edit. The write is still a single transaction taking a single `change_seq`, so connected CalDAV clients see one change batch. A fact about the *request* rather than about one Event — an unknown target Calendar, a `.zip` entry with no target — is still fatal to the whole upload, since there is no per-series answer to give it.
- **A `VEVENT` with no `DTEND` gets RFC 5545's own default**, one day for a date-valued `DTSTART` and zero for a date-time one (and `DURATION` is honoured where present). Such a file is valid iCalendar and common in real exports, so it imports rather than being skipped. This app cannot store the zero-length case — an Event ends after it starts, and the grid draws duration as height — so a zero-length Event is given 30 minutes (an all-day one, a day) and counted as *adjusted*, on the same degrade-and-report footing as the `TZID` case below rather than being dropped.
- **The degrades are import's, not CalDAV's.** The no-`DTEND` decode is shared with CalDAV PUT, so a device's `DTSTART`-only object resolves the same RFC 5545 way there; the zero-length *repair* is not, so a PUT that would store a zero-length Event is still refused outright. A human watching a one-time import is told what was changed and can act on it; a sync daemon would only see its object silently come back a different length, which is ADR-0026's territory, not this one.
- **An unresolvable `TZID` degrades to a Floating Event** rather than skipping the Event. Go resolves `TZID` through `time.LoadLocation`, which fails on the non-IANA names Outlook and older clients emit; keeping the wall-clock time is right for the common case (a user importing their own calendar in their own zone) and is reported, not silent.
- **`VALARM`s import faithfully, both channels.** An imported `ACTION:EMAIL` alarm becomes an Email-Channel Reminder that this instance's scheduler will really send mail for (ADR-0021). The guard is disclosure, not downgrade: the preview breaks reminder counts out by Channel before the user confirms. Past Occurrences cannot retro-fire — the scheduler's window is `(lastTick, now]`.

## Consequences

- The same per-series skip serves a **Subscribed Calendar**'s fetches (ADR-0032, ADR-0033), where it matters more: nobody subscribing to a feed can edit the VEVENT that would otherwise break it wholesale, and a skipped series' own UID keeps a Refresh able to tell "present in the feed but unstorable" apart from "gone from the feed", so the stored row survives instead of being tombstoned.
- The **Import summary** is part of the contract, not decoration: skipped / adjusted / ignored counts are the only way a user learns what a lossy import did to their data. ADR-0026 chose silent normalization for CalDAV, where the counterparty is a sync daemon; import reverses that default because a human is watching a one-time act.
- If backup/restore ever becomes a real job, this decision is what has to be reopened — most likely via an `external_uid` column and upsert-on-import, not by changing the id.
