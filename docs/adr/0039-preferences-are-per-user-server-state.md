# Preferences are per-User server state, decoupled from `WKST`

Status: accepted

The app has display settings it never let anyone set. `startOfWeek()` is called with no options in three places, so the week starts on Sunday because that is date-fns' default. `TimeAxis` renders the hour gutter through `toLocaleTimeString`, so it follows the browser's locale, while `EventVisual`, `MonthDayCell` and `MonthEventDragPreview` hardcode `h:mm a`, so they are always 12-hour — the same app disagreeing with itself. `activeView` is initialised to `"week"` at module load and is not persisted at all, so a reload throws away where you were.

**Four Preferences — Week start, Default view, Time format, Working hours — are stored on the User, on the server.**

```
users
  week_start           INTEGER NOT NULL DEFAULT 1   -- date-fns weekStartsOn, 0..6
  default_view         TEXT    NOT NULL DEFAULT 'week'
  time_format          TEXT    NOT NULL DEFAULT '24h'   -- '12h' | '24h'
  working_hours_start  INTEGER NULL                 -- minutes since midnight, 0..1439; NULL = no shading
  working_hours_end    INTEGER NULL                 -- minutes since midnight, 0..1439; NULL = no shading
```

They ride along on `GET /api/auth/me` and are written by a partial `PATCH /api/auth/preferences`, which returns the same `me` payload.

## Why the server, when ADR-0014 put Theme in localStorage

Theme is genuinely per-device: the machine you are sitting at is what has a dark mode, and the pre-paint script in `index.html` has to resolve it before React exists. None of that is true here. Week start is a fact about the person, not the laptop, and a self-hoster who logs in from a phone and a desktop should not have to answer the same question twice. The server storage also buys the one thing localStorage cannot: `reminder/dispatch.go` renders the Email a Reminder sends, and it can only honour a Time format the server can see.

So the mechanisms differ on purpose, and `CONTEXT.md` says so — Theme preference is not a Preference despite the name.

## Week start does not feed `WKST`

The tempting move is to write the author's Week start as `WKST` on the rules the app generates, so "every 2 weeks" lines up with the weeks they were looking at. **Rejected.**

`WKST` is not decoration: for `FREQ=WEEKLY;INTERVAL>1` it decides *which dates the rule generates*, and `customRecurrence.ts` can emit exactly that. Feeding a viewer's display Preference into it would mean two Users with Access to one shared Calendar (ADR-0034) expand the same Event into different Occurrences, a native CalDAV client expands it into a third, and changing a Preference silently rewrites the meaning of series that already exist. Occurrence identity is `(eventId, occurrenceStart)`; nothing that shifts `occurrenceStart` may be per-viewer.

Expansion keeps RFC 5545's default `WKST=MO` regardless of Week start. This is the decision most likely to be "fixed" by someone who has not thought it through, which is why it is written down.

## Working hours shade; they never clamp

Working hours tint the rows outside the range in Day and Week view. Clamping the grid to 08:00–18:00 is denser and was considered, and rejected: an Event at 06:00 — arriving from a shared Calendar, an import, a Subscription Refresh, or a native client — would vanish from the grid entirely, and the escape hatch that fixes it (auto-expand when Occurrences fall outside) is more machinery than the feature is worth. `TimeGrid`'s existing scroll-to-now is left alone for the same reason: on today, the current time is where you want to be.

Absent by default, so no existing grid changes appearance until a User opts in.

## Consequences

- **Monday and 24-hour change what existing Users see, once, on upgrade.** Both current behaviours arrived by accident rather than by decision — date-fns' default and a hardcoded format string — so the migration corrects them rather than preserving them. This is deliberate and is the only place these defaults will ever move.
- **Default view seeds; it is never written back.** `authStore` applies it to `shellStore` when `/me` resolves, on bootstrap and on login. `ProtectedRoute` already renders nothing until then, so there is no flash and no localStorage mirror. Navigating to Settings and back does not re-seed, so a mid-session view switch survives.
- **The native time input stays inconsistent with everything else.** `<input type="time">` renders per the browser's locale and its value is always `HH:mm`; no Preference can change that. A 12-hour User on a 24-hour browser will see a 24-hour picker in the Event modal. Accepted — the alternative is a custom time picker.
- **A per-recipient value enters the Reminder fan-out.** Fan-out is already per-User (ADR-0036), so Time format joins onto the due-reminder query rather than costing a query per Email.
- **`PATCH` needs pointer-typed decoding** to tell an absent field from a zero one — `week_start: 0` is Sunday, not "unset".
