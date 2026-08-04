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
A named, colored, independently-toggleable collection that groups events (e.g. "Work", "Personal"). Not the app itself, and not the view/grid.
_Avoid_: calendar list item, source

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
A minutes-before-start offset attached to an Event, chosen from a fixed preset list (not free-form).
_Avoid_: notification, alert

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

## Deployment

**Data directory**:
The single directory (`DATA_DIR`, default `/data`) under which the backend stores all persistent state — currently the SQLite database, and any future runtime data (backups, attachments). The one path a self-hoster needs to mount as a volume.
_Avoid_: data dir (in prose), storage path
