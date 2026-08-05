# Calendar color is an arbitrary hex, not an enum; displayname joins it as settable

Status: accepted — supersedes ADR-0028

ADR-0028 exposed `calendar-color` over CalDAV but kept this app's fixed 8-value color enum as the domain type, snapping any client-supplied hex to the nearest enum member by RGB distance. It named the cost and deferred it: "Revisit if a client's exact color choice ever needs to survive unchanged."

It needs to now. Snapping means picking a custom color in macOS Calendar makes the calendar visibly change color on the next sync — the app quietly overrules a choice the user made on a device. The enum was never a product decision; it was the shape Tailwind utility classes (`bg-calendar-*`) pushed us into. Every other participant in a CalDAV mesh — Apple, DAVx⁵, Thunderbird — treats calendar color as a free value, which made this app the only one that couldn't hear what the others said.

**A Calendar's color is now an arbitrary sRGB value.** The eight colors survive as Swatches: quick picks in the web app's picker, not a constraint on the type.

## The value space

Canonical form is uppercase `#RRGGBBAA`. `#RGB` and `#RRGGBB` widen with `FF` alpha; anything else is rejected with `409` for that property, as before.

Alpha is **carried but not honoured**. Apple's `calendar-color` is documented as `#RRGGBBAA` and macOS sends eight digits, so storing eight means macOS's exact string round-trips byte-identical — the default in a sync mesh should be to preserve what a peer sent, and here preservation costs two characters. It is deliberately ignored at render: a translucent event block blends into the surface and would undo the contrast calculation below. `<input type="color">` has no alpha channel, so a web-authored color is always `…FF`; non-`FF` alpha can only ever arrive from a native client, and the web app can display an alpha it cannot author.

## Color changes do not touch the sync protocol

CalDAV has no mechanism for "a collection property changed" — CTag and `sync-collection` both track *objects* (ADR-0025). So a color set in the web app reaches a native client whenever that client next PROPFINDs collection properties, on its own schedule. Verified in practice: macOS Calendar picks up a web-side change on refresh, and its polling floor is one minute.

We deliberately do **not** bump `change_seq` to force this. CTag means "the objects in this collection changed"; inflating it for a cosmetic property would make every client refetch every `.ics` in the calendar to discover that nothing did. The accepted consequence is that Calendar color has no defined convergence time and no conflict detection — WebDAV properties carry no ETag — so two clients may disagree indefinitely and the last writer wins. That is CalDAV's ceiling, not a gap in this design.

## Rendering an arbitrary color

`EventVisual` used to fill a block with the calendar color under a hardcoded `text-ink-inverse`, which worked only because all eight enum colors were dark enough to carry white text. An arbitrary color is not: macOS's palette includes a bright yellow, and nothing stops a user picking white.

The fill is still rendered exactly as stored; the *text* color is computed per block from WCAG relative luminance, plus a low-alpha border so a pale fill keeps its edge against `bg-surface` in light theme. Two limits are accepted rather than engineered away: a mid-tone color (`#808080`) contrasts poorly with both black and white and lands under WCAG AA either way — auto-text picks the better of two mediocre options — and enforcing AA would mean rendering a color other than the one chosen, which is the ADR-0028 lie in a new costume.

## Four synchronised palettes become two independent lists

The enum lived in four places that had to agree, two of them hand-synced across languages. With the server no longer validating against a palette, only two lists remain and **they are allowed to drift**: `calendarColors.ts` holds the eight Swatch hexes, and `service.go`'s `defaultCalendars` holds three seed hexes. A mismatch used to be a bug; it is now just a seed value.

Deleted: `caldavserver/color.go` entirely (`nearestColor`, `squaredDistance`, `hexForColor`, `calendarColorHex`, `calendarColorOrder`), `service.CalendarColors`, and `index.css`'s `--color-calendar-*` custom properties with their `bg-calendar-*` classes — arbitrary hex can't be a Tailwind class, so every fill becomes an inline style and those classes lose their last consumer. `IsValidColor` becomes hex validation. The `graphite` fallback for an Event whose Calendar can't be resolved becomes a literal hex constant.

The REST API's `color` field changes from `"peacock"` to `"#12809CFF"`, migrated in place. A hard cutover with no name-accepting compat shim, on the strength of ADR-0010's single-user self-hosted instance: the web app is the only consumer.

## `displayname` becomes settable, and PROPPATCH becomes atomic

macOS Calendar can rename a synced calendar, which PROPPATCHes `displayname` — and got the same `403` that started this whole thread one property over. It is now settable. There is no lossiness question: `Calendar.name` is already a free string and `caldav.Calendar.Name` already feeds `displayname` on PROPFIND. Bundled here rather than ticketed separately because it is one case in the switch this ADR was already rewriting, and leaving a known `403` beside the one just fixed invites a second round of client warnings.

A second settable property creates a case that could not exist before: macOS sends both properties in one request, so a valid rename can arrive alongside an unparseable color. Per RFC 4918 §9.2, PROPPATCH instructions are processed atomically — all staged first, and if any staged result is not `2xx`, **nothing is applied** and the would-be-`200`s are reported `424 Failed Dependency`. `applyPropPatch`'s existing stage-then-write-once structure already provided the write atomicity; this extends it to the reported statuses. Applying the properties independently would let a client end up with a renamed calendar it believes was not renamed.

Unchanged from ADR-0028: a `<remove>` of either property is `403` (both columns are `NOT NULL`, so there is no real "unset"), and any other property name is `403` rather than a silent `200`.

## Rejected

- **Per-Event color.** RFC 7986 §5.9 does define `COLOR` on `VEVENT`, but its value is a CSS3 color *keyword*, not a hex — a different value space from Apple's `calendar-color`. Supporting both would mean either two color types or a keyword↔hex snapping layer, reintroducing exactly the lossiness this ADR deletes, one level down. Client support is also thin (DAVx⁵ opt-in; Thunderbird does not render it; no evidence macOS Calendar reads or writes it), and no web-app feature asks for it. Per-Event `COLOR` therefore keeps being normalized away by ADR-0026. Revisit as a *product* decision about color-coding within one Calendar, not as a sync decision.
- **Bumping CTag so color changes propagate promptly.** Lying to the sync protocol to speed up a cosmetic property; see above.
- **Rejecting `calendar-color` PROPPATCH to make the server authoritative.** Reopens the wound ADR-0028 closed — macOS PROPPATCHes on discovery and a rejection is what produced the persistent per-calendar warning in the first place.
- **Storing 6-digit `#RRGGBB` and dropping alpha.** Simpler, one less thing the renderer must ignore, but never round-trips macOS's exact string.
- **Clamping the fill's lightness into a legible band (OKLCH) instead of computing text color.** Guarantees contrast by rendering a color other than the one the user chose.
- **Swatches-only in the web picker.** A color set from a native client matches no Swatch, so the picker would have to render a state it cannot produce — or silently reset the calendar the moment the dialog is touched, destroying the user's device-side choice from the web app.
