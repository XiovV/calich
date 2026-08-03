# All-day Events as a half-open date range, serialized without timezone conversion

Status: accepted

An Event may be marked **all-day** (`all_day` flag). All-day Events are modeled as *dates*, not instants: `start` is the date and `end` is the exclusive next day (iCalendar's half-open `DATE` convention). Only single-day all-day Events are in scope now; multi-day (spanning-bar) all-day Events are deferred.

## Why dates, not instants

An all-day event has no time-of-day and no timezone — "August 4, all day" is the same day everywhere. The existing Event model stores `start`/`end` as UTC instants and serializes them with `toISOString()`. Running an all-day date through that conversion makes a viewer east or west of UTC see it land on the previous or next day — the classic all-day off-by-one bug.

So all-day Events bypass timezone conversion entirely:

- **Storage**: `all_day` boolean on `events`; `start` = the date, `end` = the next day (a single-day all-day Event is `start = Aug 4, end = Aug 5`). The half-open range keeps the "duration every Occurrence inherits" idea intact (1 day) and generalizes to multi-day later with no migration. The existing `end > start` validation still holds; the modal's time-range check is skipped when all-day.
- **Wire format**: all-day `start`/`end` are **date-only strings** (`"2026-08-04"`), never `toISOString()`. The frontend parses them as **local midnight** `Date`s and never UTC-converts them back. Timed Events are unchanged (full ISO instants).

Trade-off accepted: the serialization layer must branch on `allDay` (date-string vs instant). This is unavoidable — it is the price of correct all-day handling.

## Rendering

- **Day/Week view**: a new **all-day lane** pinned above the scrollable hourly grid holds all-day Occurrences as full-width chips.
- **Month view**: a single-day all-day Occurrence is an Event chip distinguished from timed chips (filled bar, no start-time prefix), flowing through the existing overflow logic.
- **Drag-to-move**: Month Day cells, and the Week all-day lane across day columns (changes the date). No resize (stretching to multiple days would create a multi-day Event, out of scope). A recurring all-day Occurrence dragged fires the same scope picker as any other edit.

## Scope boundary

Deferred: multi-day all-day Events (horizontal spanning bars, cross-week continuation, bar packing) — a layout problem independent of recurrence. Recurrence composes with all-day for free: an all-day Recurrence rule is expanded over dates like any other.
