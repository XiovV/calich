# Calendar

Domain vocabulary for the calendar app — navigation chrome, view state, and the sidebar calendar list.

## Language

**Active view**:
The granularity at which the calendar is currently displayed — Day, Week, Month, or Year. Drives the main content grid (later) and the top bar's date label format.
_Avoid_: mode, display mode

**Selected date**:
The single date the shell is currently focused on, shown in the mini calendar and driving the main grid. Distinct from "today" — navigating away doesn't change what "today" means.
_Avoid_: current date, active date

**Calendar**:
A named, independently-toggleable collection that groups events (e.g. "Work", "Personal"), carrying one Calendar color. Not the app itself, and not the view/grid.
_Avoid_: calendar list item, source

**Calendar color**:
The single color a Calendar is displayed in, inherited by every Occurrence it contains — an arbitrary sRGB value with an alpha channel, not a choice from a fixed set. Shared by every client connected to the instance rather than being each client's private preference: whoever writes last wins, there is no conflict detection, and no bound on how long two clients may disagree. See ADR-0029.
_Avoid_: calendar colour (in prose), event color (Events have none of their own), color enum

**Swatch**:
One of the eight curated colors the web app offers as one-click quick picks when choosing a Calendar color. A convenience for the person picking, never a constraint on what a Calendar color may be — a Calendar whose color matches no Swatch is ordinary, not invalid, and is what a native client setting its own color normally produces.
_Avoid_: palette color, preset, theme color

**Event**:
The stored, saved unit — a titled time block belonging to a single Calendar, with a start, an end, zero or more Reminders, and an optional Recurrence rule. Its start/end define the first occurrence and the duration every Occurrence inherits. A non-recurring Event is a series of one. Distinct from the Occurrences it produces on the grid.
_Avoid_: appointment, meeting, entry

**Recurrence rule**:
The rule on an Event describing how it repeats, stored as an iCalendar `RRULE` string (RFC 5545) so it round-trips through CalDAV unchanged. Absent on a non-recurring Event. See ADR-0016.
_Avoid_: repeat pattern, schedule, recurrence pattern

**Occurrence**:
A single dated instance produced by expanding an Event's Recurrence rule over a date window. Occurrences are computed, never stored; they are what the grid actually renders (an Event chip or timed block is an Occurrence, not an Event). A non-recurring Event yields exactly one Occurrence. Identified by `(eventId, occurrenceStart)`, where `occurrenceStart` is the datetime the rule produced (its iCalendar `RECURRENCE-ID`).

**Master**:
The Event row that carries the Recurrence rule and anchors a series. Its title/start/end/calendar are the defaults every Occurrence inherits unless an Override replaces them. A non-recurring Event is a Master with no rule.
_Avoid_: parent event, root event

**Override**:
A stored Event row that replaces one Occurrence of a series with its own full title/start/end/calendar, keyed to the Occurrence it replaces by its original start (iCalendar `RECURRENCE-ID`). Produced by editing "this event" on a recurring Occurrence. A complete standalone instance, not a diff.
_Avoid_: exception (that's the cancellation), detached event, instance edit

**Exception**:
A cancelled single Occurrence — the rule still generates that slot, but it is suppressed from the grid (iCalendar `EXDATE`). Produced by deleting "this event" on a recurring Occurrence. Distinct from an Override, which replaces rather than removes.
_Avoid_: deleted occurrence, skip, override

**Series operation**:
One of the ordered steps a scoped edit (This event/This and following/All events) on a recurring Occurrence compiles down to — override an Occurrence, re-anchor a series at a split point, or put a single Event's fields wholesale. The same operation list is realized two ways: applied to the local cache, and dispatched to the API in order, so the two can never drift apart. See ADR-0016.
_Avoid_: edit step, mutation, transform

**All-day Event**:
An Event that occupies whole dates rather than a time range, flagged by `allDay`. Stored with the iCalendar half-open date convention — `start` is the date, `end` is the exclusive next day (a single-day all-day Event spans one day). Its start/end are wall-clock dates, serialized as date-only strings and never timezone-converted. Rendered in the all-day lane, not on the hourly grid. Multi-day all-day Events are out of scope for now.
_Avoid_: full-day event, date event

**All-day lane**:
The horizontal strip above the hourly grid in Day and Week view where all-day Occurrences are shown, separate from the timed grid below it.
_Avoid_: all-day row, header row

**Draft block**:
A proposed time window for a not-yet-saved Event, before it is confirmed via the creation modal — whether it originated from dragging on the grid or from a default (e.g. the sidebar's Create button). Discarded, not saved, if the modal is cancelled.
_Avoid_: pending event, temp event

**Reminder**:
A single cue attached to an Event that fires a fixed offset before an Occurrence's start, on one Channel. An Event carries zero or more, each with its own offset and Channel. Maps to one iCalendar `VALARM` (`TRIGGER` for the offset, `ACTION` for the Channel) so it round-trips through CalDAV unchanged. See ADR-0020.
_Avoid_: alert

**Channel**:
The delivery method of a Reminder — **Notification** (shown in-app) or **Email** (sent to the User). Corresponds to the iCalendar `VALARM` `ACTION` (`DISPLAY` / `EMAIL`).
_Avoid_: type, kind, action (in prose)

**Notification**:
The persistent in-app record the server creates when a Reminder on the Notification Channel fires. Lives in the Notification feed and, if a tab is open, is mirrored as a browser notification. A Notification-Channel Reminder is the rule; a Notification is the delivered instance it produces. Distinct from a toast, which is a transient client-only UI message (e.g. an error). See ADR-0021.
_Avoid_: alert, toast (for this), push

**Day cell**:
A single day's box in the Month view grid. Holds that day's Event chips and is the target of create-clicks and drag-to-move drops.
_Avoid_: day box, tile

**Event chip**:
The compact, single-line rendering of an Event inside a Day cell, drawn in its Calendar's color and showing start time and title. The Month-view counterpart to the grid's timed event block.
_Avoid_: pill, event bar

**Adjacent-month day**:
A Day cell belonging to the month before or after the one being displayed, shown to fill the fixed six-row grid. Rendered dimmed but fully live — its Events show, and it accepts creates, edits, and drops like any other Day cell.
_Avoid_: overflow day, padding day, out-of-month day

**Mini-month**:
A compact, read-only month shown in the Year view — day numbers only, no Event chips. One of twelve laid out in a grid.
_Avoid_: month thumbnail, small calendar

**Event-presence dot**:
The marker on a Mini-month day indicating it has at least one visible Event. A density hint only — it carries no Calendar color and no count.
_Avoid_: event indicator, badge

**Anchor zone**:
The IANA timezone an Event's wall-clock is defined in, stored as the `tzid` on the Event (the `TZID` a CalDAV client sends). Drives Recurrence rule expansion and DST — "every Mon 10:00 Europe/Berlin" fires at 10:00 Berlin wall-clock across a DST change. Belongs to the Event and is preserved when the Event is edited; only an explicit zone choice changes it. `Etc/UTC` means an absolute instant; absent means a Floating Event. See ADR-0019.
_Avoid_: event timezone, source zone, origin zone

**Viewer zone**:
The IANA timezone the current viewer sees times in — detected from the browser and applied only at render, shifting each Event's instant to local wall-clock (a 15:00 Berlin meeting shows as 09:00 to a New York viewer). Belongs to the session, not the Event, and follows the viewer as they travel. Distinct from the Anchor zone. See ADR-0019.
_Avoid_: local zone, display timezone, user zone

**Floating Event**:
A timed Event with no Anchor zone (`tzid` absent), whose wall-clock is interpreted in the Viewer zone rather than any fixed zone — it has no single absolute instant and expands recurrence in the viewer's wall-clock (iCalendar floating time, e.g. "take meds at 09:00 wherever I am"). The default for Events predating the timezone model. Distinct from an all-day Event, which is date-only. See ADR-0019.
_Avoid_: local event, zoneless event, naive event

**Theme preference**:
The user's chosen appearance — Light, Dark, or System — persisted locally and applied by toggling a `.dark` class on the document root. System defers to the OS setting; Light and Dark pin the appearance regardless of the OS. See ADR-0014.
_Avoid_: color scheme, mode, dark-mode toggle

## Authentication

**User**:
The account record a Session belongs to, validated by the backend. One per self-hosted instance today; see ADR-0010 for why the schema doesn't treat this as a singleton.
_Avoid_: account, profile

**Session**:
The logged-in state for one User, established by the backend after validating credentials. Present or absent — there is no partial/expired-but-visible session state in this app.
_Avoid_: login state, auth state

**Access token**:
The short-lived credential attached to authenticated requests, held in memory only on the client and re-obtained on page load via the Refresh token.
_Avoid_: auth token, bearer token

**Refresh token**:
The credential used to obtain a new Access token without re-entering credentials, via `refreshAccessToken`. Issued by the backend as an `httpOnly`, `Secure` cookie — see ADR-0009.
_Avoid_: renewal token

**App password**:
A per-User, revocable credential a native calendar client uses to authenticate over CalDAV (HTTP Basic), distinct from the login password and shown to the User only once when generated. Stored hashed, one row per generated credential, so a single device can be revoked without touching the account password. The successor to the abandoned feed token. See ADR-0024.
_Avoid_: app-specific password (in prose), device password, API key

## Sync

**Calendar object**:
The unit CalDAV addresses — one `.ics` resource per Event *series*, sharing a single iCalendar `UID` (the Master's id) and bundling the Master, all its Overrides, and its Exceptions in one file. The CalDAV-visible counterpart to the many rows a series occupies and to the Occurrences it expands to: one Calendar object, one series, many rows. Exposed at `{masterId}.ics`. See ADR-0025.
_Avoid_: calendar resource, ics file, vevent

## Deployment

**Data directory**:
The single directory (`DATA_DIR`, default `/data`) under which the backend stores all persistent state — currently the SQLite database, and any future runtime data (backups, attachments). The one path a self-hoster needs to mount as a volume.
_Avoid_: data dir (in prose), storage path
