# CalDAV calendar-color: XML-patched PROPFIND/PROPPATCH, mapped onto the fixed color enum

Status: superseded by ADR-0029

The XML-patching mechanism below (PROPFIND injection, the hand-rolled PROPPATCH path) stands. What ADR-0029 replaces is the color model layered on it: the fixed enum, the nearest-color snapping, and `color.go`.

ADR-0023 already flagged this gap and deferred it: "Calendar color is not exposed over CalDAV yet... emitting color would need a small XML-patching layer... deferred until a client actually needs it." A client needs it now — macOS Calendar sends `PROPPATCH` to every synced calendar right after discovery (almost certainly trying to write back its own choice of `calendar-color`, since we don't expose one either), gets a bare `405`, and shows a persistent warning on every calendar as a result.

This closes the gap: a Calendar's color is exposed over `PROPFIND` and accepted (best-effort) over `PROPPATCH`.

## go-webdav gives no hook for either direction

`caldav.Calendar` (the library's struct) has no color field and no extensibility point, the same gap ctag.go already works around for `getctag`. Worse for writing: `caldav.Backend` has no `PropPatch` method at all, and the library's private wrapper hardcodes `PropPatch` to always return `501 Not Implemented`, unconditionally, unreachable from any `Backend` implementation. So both directions are handled entirely outside go-webdav, at the raw HTTP layer:

- **Reading** reuses (and generalizes) ctag.go's existing technique: run the request through the library once via a recorder, then regex-patch its empty/404 `<calendar-color/>` fragment into a real 200 propstat. `propertyinject.go` extracts this into a property-agnostic `injectProperty`, shared by both getctag and calendar-color, dispatched by `propfind.go`'s single-pass `servePropfind` — needed because a real client asks for both properties in one request, and each patch must not independently re-run the request or write the response.
- **Writing** is handled by `proppatch.go`, entirely bypassing go-webdav: `chi.RegisterMethod("PROPPATCH")` (chi 405s any unregistered method before it ever reaches our dispatch) plus a hand-rolled `<propertyupdate>` body parser (go-webdav's own parsing types live in an unexported package of a different module and aren't importable here).

## Mapping an arbitrary color onto the fixed enum

This app's `Calendar.color` is a closed 8-value enum, each mapped to one fixed hex in `src/index.css`. Apple's `calendar-color` is an arbitrary hex+alpha a user can freely pick on a device. Losslessly storing that would mean widening the DB column, `CalendarService`'s validation, the frontend color picker, and the CSS palette — a much bigger change than this ticket, and not attempted here.

Instead, extending ADR-0026's "columns are the sole source of truth, serve the normalized reconstruction" stance from the calendar-object resource up to the Calendar-collection resource:

- **Reading** always serves the canonical hex for the calendar's stored enum color, from a small Go-side table (`color.go`) mirroring `src/index.css`'s 8 values — the same kind of cross-language duplication `service.CalendarColors` already has against `calendarColors.ts`.
- **Writing** snaps any parseable hex to its *nearest* enum color by squared RGB distance (ties broken by the enum's declared order) and persists that — never rejecting just because a color isn't an exact match. It always echoes back the matched color's canonical hex on the next `GET`/`PROPFIND`, never the client's raw input, so a client is never stuck seeing its just-set value as "changed" again (the same re-sync-loop guard ADR-0026 established for ETags).
- Only a genuinely unparseable value (not `#RGB`/`#RRGGBB`/`#RRGGBBAA`) is rejected, with `409 Conflict` for that property.
- A `<remove>` of `calendar-color`, and any `<set>`/`<remove>` of any other property name, is reported `403 Forbidden` rather than silently ignored — the `color` column is `NOT NULL` (no real "unset"), and this backend supports no other settable collection property.

## Rejected

- **Lossless round-trip of the client's exact color.** Would require the schema/validation/frontend widening described above. Revisit if a client's exact color choice ever needs to survive unchanged — nothing here blocks that later.
- **Silently ignoring unsupported PROPPATCH properties (200 OK for everything).** Rejected in favor of an honest `403` per property, so a client can tell its update didn't fully apply, consistent with the generic (non-CalDAV) `webdav` package's own `PropPatch` precedent in the vendored library (which rejects with `403` rather than pretending success).
