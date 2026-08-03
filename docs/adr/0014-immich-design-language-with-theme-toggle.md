# Adopt Immich's design language with a class-based theme toggle

We reskinned the app to match [Immich](https://github.com/immich-app/immich), whose look we wanted: an indigo (`#4250af`) accent, flat `neutral` grays (dropping our blue-tinted grays), and full 50–950 oklch color scales for `primary/success/danger/warning/info` imported verbatim from Immich's `@immich/ui` `default.css`. Our existing semantic tokens (`surface`, `ink`, `accent`, `border`, …) keep their names and are re-pointed onto Immich's scales. Immich is a Svelte app, so nothing runs as Immich code — we translated its design tokens, and later its `Button` and `Select` component styling (see below), into React/Tailwind.

The first pass was a pure token swap (no component markup changed). Later passes ported primitives to match Immich's components:

- `components/ui/Button.tsx` — a translation of Immich's `internal/Button.svelte` (`filled`/`outline`/`ghost` variants, `primary`/`secondary`/`danger`+ colors, pill `round` shape by default), with its class logic in `components/ui/buttonClasses.ts` so base-ui `Dialog.Close`/`AlertDialog.Close` triggers can reuse it.
- `components/ui/Select.tsx` — restyled to Immich's dropdown (pill trigger, indigo-tinted popup in dark mode, checked-item indicator).
- `components/ui/Input.tsx` — a port of Immich's `Input.svelte`: label + optional description over a **filled, borderless container** (`bg-surface-sunken`) with a `ring` that turns accent on focus, replacing the previous outlined `border` inputs. Shared field styling lives in `components/ui/fieldStyles.ts` (`fieldLabelClass`, `fieldContainerClasses`), reused by `Select`'s optional `label`.
- `components/ui/IconButton.tsx` (+ `iconButtonClasses.ts`) — a port of Immich's square, round, ghost-secondary `IconButton`, used across the top bar and menu triggers.

The app shell was flattened to match Immich's chrome: the top bar dropped its shadow (border only), the sidebar moved from a grey `surface-sunken` fill to the base `surface` (Immich's `bg-light`), and the calendar rows became Immich's flush-left **`rounded-e-full`** hover pills. The sidebar Create action is now a filled-primary pill `Button`.

All inline action buttons and form fields (which had used base-ui `Field`) were migrated to these primitives; inline error text moved from the `calendar-tomato` event colour to the semantic `danger` colour.

We switched dark mode from `prefers-color-scheme` (automatic, OS-only) to Immich's **`.dark` class** on `<html>`, driven by a new theme store with a `light | dark | system` preference (default `system`, persisted to `localStorage`) and a standalone toggle in the top bar. This is a real feature beyond a reskin, chosen deliberately: the class mechanism is what lets a user override their OS setting, which `prefers-color-scheme` alone cannot do. A small blocking script in `index.html` applies the class before first paint to avoid a flash.

## Considered options

- **Keep `prefers-color-scheme`** — rejected: it can't offer a user-facing override, and Immich's dark palette is authored against a `.dark` selector with inverted scales, which maps cleanly onto a class toggle.
- **Restructure every component to Immich's `primary-600`-style class names** — rejected: aliasing our semantic tokens onto Immich's scales gets the same visual result with zero component churn.

## Consequences

- **The accent inverts in dark mode.** Immich's `primary` scale flips, so filled accent surfaces (primary buttons, checked checkboxes) render as a *light lavender-blue with dark text* in dark mode rather than a saturated indigo. This is intentional and faithful to Immich; if a saturated dark-mode accent is ever wanted, pin `--color-accent` to a fixed shade instead of letting it flip.
- **`ink-muted` deviates from Immich for accessibility.** Immich's own `muted` gray (71% lightness) fails WCAG AA on white, so `ink-muted` maps to `neutral-500` (still Immich's neutral family) instead of `muted`.
- The per-Calendar event swatch colors are domain data and were left unchanged.
