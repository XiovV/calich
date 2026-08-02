# Type-based component folders

New components are organized by type (`components/layout`, `components/ui`) rather than by feature (e.g. an `app-shell/` folder). The app shell is cross-cutting layout infrastructure that every future feature sits inside, not a domain feature itself, so it doesn't warrant its own feature folder. `components/ui/` holds generic Base UI wrappers; `components/layout/` holds shell-specific composites.

**Feature folders are still used for genuinely separate concerns** — `calendar-grid/` (the Week/Day grid feature) and `auth/` (login, route protection, session) each get their own top-level folder rather than living under `components/layout/` or `components/ui/`, since neither is shell chrome or a generic wrapper. The rule of thumb: a real, self-contained domain feature gets a feature folder; cross-cutting chrome and generic primitives stay type-based.
