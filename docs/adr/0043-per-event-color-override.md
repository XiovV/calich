# An Event may override its Calendar's color

Status: accepted — reverses ADR-0029's "Rejected: Per-Event color"

ADR-0029 rejected per-Event color, but not on product grounds — it named the exact reopening condition: "no web-app feature asks for it... revisit as a product decision about color-coding within one Calendar, not as a sync decision." This is that revisit. What ADR-0029 actually objected to was CalDAV sync fidelity — RFC 7986's `COLOR` on `VEVENT` is a CSS3 color *keyword*, not a hex value, a different value space from `calendar-color`'s arbitrary sRGB. That objection is still true. This ADR accepts the cost anyway, scoped to Events only, rather than deferring the product need further.

## Event color

An Event's Master or Override may carry a `color` — same value space and picker as Calendar color (ADR-0029: arbitrary hex, Swatches as quick-picks, canonical `#RRGGBBAA`) — nullable, defaulting to absent.

```
ResolveColor(user, occurrence) =
  occurrence.color if set,
  else DisplayColor(user, occurrence.calendar)   -- ADR-0038, unchanged
```

Event color is checked first and, if present, wins outright; `DisplayColor` is not even consulted. This composes with ADR-0038 without touching it: a Calendar's Owner-set color and a viewer's personal Calendar-color override still resolve exactly as before for every Event that has no color of its own.

## Shared, not personal

Two per-User customization mechanisms already exist in this codebase — Calendar color override (ADR-0038) and Reminder override (ADR-0036) — and both are personal: invisible to everyone but the User who set them. Event color is neither. It is Editor-settable Event data (same Role rule as title or time, ADR-0034) and every User with Access sees the same value. This is not a stylistic choice — it is required by the CalDAV goal below: a `VEVENT` has exactly one `COLOR`, so a per-viewer model would force an arbitrary "whose color gets exported" decision that a shared value doesn't need to make.

## Scoped like any other Event field

Color needed no new mechanism. Master and Override already exist to let "This event / This and following / All events" scope a change to one Occurrence, a split point onward, or the whole series (the Series operation compiler, ADR-0016). Color is simply one more field that compilation can carry — recoloring "just this Tuesday" produces an Override exactly as retiming it would.

## CalDAV round-trip, and the snapping it reintroduces

Stored Event color stays exact hex, always — nothing about this app's own read or write path is lossy. Lossiness appears only at the CalDAV boundary, one direction at a time:

- **Export** (building a `VEVENT` for a Calendar object, ADR-0025): the stored hex is snapped to the nearest CSS3 keyword by RGB distance — `nearestColor`/`squaredDistance`-style matching, deleted at the Calendar level by ADR-0029, now reintroduced scoped to Events only.
- **Import**: lossless. A `COLOR` keyword has an exact defined RGB per the CSS3 spec, so decoding it back to hex loses nothing.

The asymmetry is confined to what a native client happens to render, never to what this app stores or shows in the web UI — the same ceiling ADR-0029 already accepted for Calendar color's own PROPFIND-timing gap and lack of conflict detection. A user who sets `#FF6B35` in the web app will see `#FF6B35` everywhere in this app; a native client showing that Event will show whatever CSS3 keyword sits closest, same as ADR-0028's original enum-snapping problem, now scoped one level down and accepted deliberately rather than by accident.

## UI

A color swatch in the Event edit modal, next to title/time/Calendar, showing the resolved color (inherited unless already overridden) and opening the same Swatches-plus-hex picker Calendar color uses. Picking a color *is* setting the override — no separate enable toggle. A "Reset to Calendar color" action, shown only once a color is set, clears it back to `null`. Event chips and timed Event blocks render `ResolveColor`'s result; the WCAG-luminance text-contrast computation from ADR-0029 runs against it unchanged. The Mini-month's Event-presence dot is untouched — it never carried Calendar color either, by design, and an Event override doesn't reach it.

## Rejected

- **Series-level only, no per-Occurrence override.** The Master/Override mechanism already exists and already does exactly this for every other Event field; refusing to let color use it would be a special case with no justification.
- **Personal per-viewer color shadow**, mirroring ADR-0038. Rejected because a `VEVENT`'s `COLOR` is one value, not per-viewer — a personal shadow would force an arbitrary choice of whose color exports, undercutting the CalDAV goal this ADR exists to satisfy.
- **Restrict the web picker to the CSS3 keyword set**, guaranteeing lossless round-trips by construction. Rejected to keep one consistent picker behavior (arbitrary hex + Swatches) across both Calendar color and Event color, rather than two pickers that behave differently depending on what they're coloring.
- **Always-set color, defaulted at creation.** Rejected because it makes "inherit" inexpressible once set, and because an Event would silently stop following its Calendar's color the moment the Owner recolors it — even though nobody asked for that Event to become independent.

## Consequences

- `events` (or the equivalent Master/Override row) gains a nullable `color` column, alongside the existing title/start/end/calendar fields an Override already replaces wholesale.
- CalDAV export needs Event-scoped nearest-keyword matching — new code, distinct from the Calendar-level matching ADR-0029 deleted, since the two operate in different directions (Calendar color never left hex; Event color must still translate to a keyword on the way out).
- Every place an Occurrence is rendered (Event chip, timed Event block) resolves color via `ResolveColor` instead of reading the Calendar's color directly.
