# Recurrence via iCalendar RRULE, master + expand-on-read, expanded on the frontend

Status: accepted

Recurring Events are modeled as an iCalendar **`RRULE`** string carried on a single **master** Event row. Occurrences are never stored — they are computed by expanding the rule over the visible date window. The **web frontend** owns expansion (via `rrule.js`); the backend stays a thin CRUD store that treats `rrule` as opaque text.

## Why RRULE

CalDAV is a committed direction (ADR-0013), and `RRULE` is the format native calendar clients speak. Storing anything else would force a lossy two-way translation at the CalDAV boundary. The Google-style creation UI maps onto a small subset (`FREQ`, `INTERVAL`, `BYDAY`, `BYMONTHDAY`, `UNTIL`, `COUNT`); we build the UI over that subset but persist the full string, so the model is never the limiting factor.

Trade-off accepted: `rrule` is opaque in SQL (can't query "all weekly events") and needs a battle-tested expander rather than hand-rolled date math.

## Master + expand-on-read (not materialized rows)

Materializing one row per Occurrence is impossible for unbounded ("never ends") series and bloats bounded ones. One master row carries the first Occurrence's start/end (defining the duration every Occurrence inherits) plus the `rrule`. This is the iCalendar/CalDAV representation.

Consequence: an Event is no longer 1:1 with a thing drawn on the grid. The grid's unit is the **Occurrence**, identified by `(eventId, occurrenceStart)` — its iCalendar `RECURRENCE-ID`.

## Frontend expansion (not backend)

The backend returns a flat `Event[]` with the `rrule` and `exdates` on masters; the frontend expands over the currently visible window with a pure `expandOccurrences()` function. This fits the existing fetch-then-render model, makes navigation round-trip-free, and is cheap at single-user personal scale. Infinite series are safe because expansion is always bounded to the view.

The CalDAV server will still need its own server-side RRULE expansion for `calendar-query REPORT` — that is separate protocol work under ADR-0013 and does not block the web app. A Go RRULE parser is deferred to that work; until then the backend does only light sanity validation and trusts the frontend-authored rule (acceptable for single-user, self-authored data).

## Storage: relational overrides and exceptions

The single `events` table becomes self-referential to mirror iCalendar exactly:

- **Master** — `rrule` set, `parent_id` null. (A non-recurring Event is a master with `rrule` null.)
- **Override** — `parent_id` + `recurrence_id` set, `rrule` null; carries its own full title/start/end/calendar. A complete standalone instance, not a diff, so it reuses the normal Event render/edit path. Produced by editing "this event".
- **Exception** — a row in `event_exceptions (parent_id, occurrence_start)`; a cancelled Occurrence (`EXDATE`). Produced by deleting "this event".

`ON DELETE CASCADE` on `parent_id` and the exceptions table means deleting a series auto-cleans its overrides and exceptions.

Expansion = expand master `rrule` over the window → drop starts matching an exception → for each remaining start, render the override with that `recurrence_id` if one exists, else the shifted master.

## Editing semantics (Tier 3, full parity)

> **Interim (as shipped in #34):** the scope picker below does not exist yet. Until Overrides/Exceptions and the This-event/This-and-following options land (#35, #36), editing, deleting, or dragging a recurring Occurrence always operates on the whole series — i.e. the **All events** path only. Dragging additionally re-anchors the rule to the new start (a "Weekly on Tuesday" series dragged to a Wednesday becomes "Weekly on Wednesday") so the master's rule never falls out of sync with its start.

Edit/delete/drag on a recurring Occurrence prompt **This event / This and following / All events** (default *This event*):

- **This event** → write an override (edit) or exception (delete).
- **This and following** → truncate the old master with `UNTIL` at the last Occurrence before the split, create a new master anchored at the split carrying the remainder, and reparent overrides/exceptions at-or-after the split.
- **All events** → rewrite the master; overrides are left untouched (they keep their own fields).

Internally we prefer `UNTIL` over `COUNT` (the UI may still say "after N occurrences", translated to the Nth Occurrence's date at save) so splits never do count arithmetic. Changing or removing the rule itself is forced to **All events** and discards existing overrides/exceptions after a warning, because their `recurrence_id`s may no longer be generated.

Expansion uses **local wall-clock semantics** (via `rrule.js`) so a 09:00 series stays 09:00 across DST; per-event timezone is deferred to CalDAV.

## An Override is emitted from its own start, not only from the slot it replaces

Amended by #227. The one-line definition above — *"expand master `rrule` over the window → drop starts matching an exception → for each remaining start, render the override with that `recurrence_id` if one exists, else the shifted master"* — makes every Occurrence rule-produced, and quietly makes the **window** the arbiter of whether an Override exists at all. An Override moved onto a different day than the one it replaces has two dates: the slot it replaces (its `recurrence_id`) and the one it now sits on (its own start). The rule generates only the first. So a window holding the second but not the first generated no slot to substitute into, and the Event vanished — invisible in Day view on both days, and, moved across a week boundary, invisible in *every* week. Changing the week-start Preference made it reappear, which is the tell: same data, same view, a different window, a different answer.

**An Override is emitted from its own `[start, end)` whenever the substitution above didn't already emit it.** The two paths are exclusive by construction — the substitution records what it emitted — so an Override whose both dates land in one window still renders exactly once.

The rest of the rule stands unchanged, and deliberately:

- **The slot it replaces stays suppressed.** An Override outside the window still consumes its Master's generated start, so a moved Occurrence never renders on both its old and its new date. That is the same line of code as before; it is what makes the second emission safe rather than duplicative.
- **An Exception still wins.** A cancelled slot is cancelled wherever its Override was moved to, so the window remains irrelevant to the outcome — the property whose absence was the bug.
- **An Override still isn't a standalone Event.** One whose Master isn't in the expanded set — a deleted series, an unchecked Calendar — is not emitted. It is a member of a series that isn't being drawn.

This is a completion of the expansion rule rather than a new direction: the ADR always intended the Occurrence to be drawn once, on the date it actually falls on. Stating expansion purely as a walk over rule-produced starts is what lost the Overrides that had left theirs.
