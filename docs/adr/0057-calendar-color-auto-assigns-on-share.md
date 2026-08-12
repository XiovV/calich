# Calendar colour auto-assigns per-User on share; the Owner-tracking state is retired

Status: accepted — amends ADR-0038; touches ADR-0029

ADR-0038 gave every User with Access to a Calendar the ability to shadow its colour with a personal override, "applies to them alone." That fixed CalDAV's PROPPATCH problem, but the override stayed opt-in: until a User manually picks one, `DisplayColor(user, calendar)` falls back to the Owner's colour, so two Calendars shared to the same household member can render identically until someone notices and intervenes by hand. Colour is a personal thing — sharing should hand the recipient one of their own immediately, not wait for them to discover the picker.

**Sharing now always assigns a personal colour, computed lazily on first view.**

Rather than writing an override the moment `Share`/`ShareWithGroup` inserts a `calendar_shares` row — which can't reach a Group Share's future members — `resolveDisplayColor` (`service/calendar.go`) is extended: when it resolves a Calendar owned by someone else and finds no `calendar_user_colors` row for the viewer, it computes one and upserts it before returning, so the same request that first renders a shared Calendar is also the one that fixes its colour for good.

```
resolveDisplayColor(user, calendar):
  if override exists for (user, calendar): return override
  if user owns calendar:                   return calendar.color
  color = pickFreeColor(usedColorsFor(user, calendar.workspace))
  colorOverrides.Upsert(user, calendar, color)
  return color
```

One mechanism covers a direct Share, a Group Share's current members, and anyone who joins that Group later — all three just look like "no row yet" the next time they load the calendar list, so nothing has to notice a Group membership change separately.

## Picking a free colour

`usedColorsFor(user, workspace)` is the *resolved* `DisplayColor` of every Calendar the User can currently see in that Workspace — owned, shared-in, and Subscribed/Linked — not each Calendar's raw `calendars.color`. Comparing raw owner colours would let two Calendars both auto-assign to the same Swatch for one viewer whenever their Owners happened to pick the same colour independently, which is exactly the collision this feature exists to prevent.

`pickFreeColor` tries the 8 Swatches in fixed order — the same shape as the frontend's existing `getNextUnusedColor` (`calendarColors.ts:53`), reimplemented server-side since this decision lives in `resolveDisplayColor` — and returns the first one absent from the used set. Once every Swatch in a Workspace is taken (a real ceiling once a household has more than 8 visible Calendars), it falls back to a random hex outside the Swatch set: hue randomised, but saturation and lightness held in the same range the Swatches occupy, rather than raw random RGB that could land on something washed-out or muddy. When a batch of Calendars in one response all lack an override at once, they're assigned in Calendar-id order — a stable tiebreak so results don't depend on Go's randomised map iteration.

## The "track the Owner's colour" state is retired

Under ADR-0038, an absent override row meant "this User is deliberately following the Owner's colour, and will keep doing so as the Owner changes it." That state no longer exists: the moment a shared Calendar is first viewed, it gets a real override and never goes back. `CalendarModal`'s "Reset to the owner's colour" button (and the `ClearColorOverride` call behind it) is removed for non-Owners — clearing the row would just cause the very next list fetch to reassign a colour anyway, so the button's premise is gone. No replacement "reassign" affordance is added: the same modal already has a full Swatch picker one click away, and a User who dislikes their auto-pick just picks another Swatch by hand. Owners are unaffected — their own colour picker still writes `calendars.color` directly, unchanged.

## Considered Options

- **Assign at Share-creation time.** Simpler to reason about for a direct Share, but has no answer for a Group Share's future members — either a background job has to notice new Group joins, or they'd need a second, lazy fallback anyway. Rejected: lazy-on-first-view covers both cases with one code path.
- **Keep "track the Owner" as an explicit state, override only on collision.** Preserves ADR-0038's opt-in spirit and only intervenes when colours actually clash. Rejected because it reintroduces the asymmetry this ADR removes — some shared Calendars would track the Owner and others wouldn't, for reasons invisible to the User.
- **Fall back to `SWATCHES[0]` once the palette is exhausted**, matching the frontend's current behaviour. Rejected: it guarantees a collision every time a Workspace has more than 8 visible Calendars, which is the exact failure this feature exists to fix.

## Consequences

- No schema change — `calendar_user_colors` (ADR-0038) already has the shape this needs; only its write path grows a new caller.
- Existing Shares get colours the next time each recipient's calendar list loads; no backfill migration needed.
- `resolveDisplayColor`, previously a pure read, now performs a write on a Calendar's first resolution per viewer. Accepted as an idempotent upsert of derived data rather than a mutation of User intent — consistent with ADR-0029/0038's already-accepted stance that colour has no conflict detection and no defined convergence time.
- CalDAV clients get the same behaviour for free: a native client's first PROPFIND of a newly shared collection resolves through the same `resolveDisplayColor`, so it discovers an already-personal colour rather than the Owner's.
- `CONTEXT.md`'s **Calendar color** entry is updated: "may shadow" becomes "is shadowed" — the override is no longer optional.
