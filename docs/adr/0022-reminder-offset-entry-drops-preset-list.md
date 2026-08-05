# Reminder offset entry drops the preset list

The Reminder row's offset control is a direct amount + unit entry (e.g. "15" / "minutes"), always editable — no preset dropdown, no separate "Custom…" step. This supersedes the UI portion of ADR-0020, which specified a fixed preset list plus a custom offset entry.

## Why

The preset-plus-custom design (mirroring the Custom recurrence dialog) didn't fit the Reminder row's narrower layout: a preset dropdown, channel dropdown, and — once "Custom…" was picked — an amount field and unit dropdown competed for space in the event modal, repeatedly breaking across screen sizes and preset label lengths. Google Calendar and Proton Calendar both skip the preset step entirely and let the amount/unit be edited directly; adopting that model removes the layout problem at its source rather than continuing to patch it, and cuts a click (no "Custom…" selection) for the common case of picking an offset that isn't one of a handful of presets.

## Decisions

- The Reminder row shows exactly three controls: Channel dropdown, offset amount (number input), offset unit dropdown (minutes/hours/days/weeks). No preset list, no "Custom…" option.
- `offsetMinutes` storage, normalization (`normalizeCustomOffset`), and display-splitting (`splitCustomOffset`) are unchanged from ADR-0020 — only the preset-list presentation layer is removed. `src/lib/reminderPresets.ts` is renamed to `src/lib/reminderOffset.ts` to drop the now-inaccurate name.
- The 09:00-anchor semantics for all-day Events' offsets (ADR-0020) are unaffected — that's an interpretation of the stored `offsetMinutes`, not a UI concern, so the Reminder row no longer needs an `allDay` prop at all.

## Consequences

- Simpler component: `ReminderRow` no longer tracks an `offsetChoice` / preset-vs-custom state machine, removing a category of bugs where that derived state could snap back to the wrong value.
- Reminder offsets that happen to equal a former preset (e.g. "10 minutes") no longer get a friendly label like "10 minutes before" — they display as amount 10 / unit minutes, same as any other value. Consistent with Google/Proton's presentation.
