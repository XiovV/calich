# Type-based component folders

New components are organized by type (`components/layout`, `components/ui`) rather than by feature (e.g. an `app-shell/` folder). The app shell is cross-cutting layout infrastructure that every future feature sits inside, not a domain feature itself, so it doesn't warrant its own feature folder. `components/ui/` holds generic Base UI wrappers; `components/layout/` holds shell-specific composites.
