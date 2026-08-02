# Separate calendars store

Calendars move from a static `mockCalendars.ts` array into a new `useCalendarsStore` (Zustand), rather than folding calendar creation into `useShellStore` or `useEventsStore`. This continues the pattern from ADR-0003: each kind of durable domain content (events, calendars) gets its own store, keeping `useShellStore` scoped to ephemeral shell-chrome state. The store is in-memory only for now — a planned backend will later persist calendars the same way it eventually will events.
