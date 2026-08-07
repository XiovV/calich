# Calendar color gains a per-User override

Status: accepted — amends ADR-0029

ADR-0029 established that a Calendar's color is an arbitrary sRGB value "shared by every client connected to the instance rather than being each client's private preference: whoever writes last wins." That was correct for one User. It collides with sharing.

ADR-0034 makes recolouring an Owner-only operation, because it is Calendar management rather than Event editing. But ADR-0028 and ADR-0029 accept `calendar-color` via PROPPATCH from native clients, and every calendar app on earth lets you pick a colour for a calendar someone shared with you. Owner-only means Bob's PROPPATCH must be refused — so his client shows green until the next sync and then silently snaps back to Alice's colour. That is precisely the failure ADR-0028 exists to prevent, and ADR-0029 rejected "making the server authoritative" for exactly this reason.

**The Calendar keeps its colour, set by the Owner. Any User with Access may set a personal override.**

```
calendars.color               -- the Owner's; the default everyone starts from
calendar_user_colors
  user_id, calendar_id, color
  PRIMARY KEY (calendar_id, user_id)

DisplayColor(user, calendar) = override if one exists, else calendar.color
```

PROPPATCH of `calendar-color` now succeeds for **every** principal: the Owner's writes `calendars.color`, anyone else's writes their own override row. No principal is ever told no, so no client's colour picker ever lies.

## Why this is cheap

`calendar-color` is a *collection* property, not object bytes. It can differ per principal without touching ETag or CTag — ADR-0035 already has `current-user-privilege-set` differing per principal for the same reason. ADR-0029's analysis carries over unchanged: colour changes do not bump `change_seq`, so they still have no defined convergence time and no conflict detection, and that remains CalDAV's ceiling rather than a gap here.

An unshared Calendar behaves exactly as it does today. This amends ADR-0029 only in the shared case.

## Considered Options

- **Per-User override over the Owner's colour (chosen).** One small table; PROPPATCH always succeeds; existing behaviour preserved for unshared Calendars.
- **Owner-only; colour stays purely a Calendar property.** Fully consistent with ADR-0034's Editor rule and needs no new table. Rejected because a `403` on Bob's PROPPATCH reopens the macOS warning wound ADR-0028 closed.
- **Colour becomes per-User entirely, dropping `calendars.color`.** Most uniform — no owner/override asymmetry to reason about. Rejected: it needs a real migration of existing colours, and it destroys the Owner's ability to state "Family is the green one" as a shared fact that new Shares inherit.

## Consequences

- **ADR-0032's shadow-column mechanism interacts with overrides.** A Subscribed Calendar's colour follows the publisher until the User touches it, tracked by comparing against a shadow column. That mechanism governs `calendars.color` only; a per-User override sits above it and wins for that User regardless of what the publisher sends. A User who has overridden the colour of a Subscribed Calendar stops tracking the publisher's colour — for themselves alone.
- **The web app must resolve colour per viewer**, not per Calendar, everywhere an Occurrence is drawn. The luminance-based text-colour computation from ADR-0029 runs against the resolved colour, unchanged.
- **Revoking a Share leaves an orphaned override row.** Harmless — it simply applies again if the Share is restored — but it should be cleaned up on revoke rather than accumulating, since it is per-User state about a Calendar the User can no longer see.
