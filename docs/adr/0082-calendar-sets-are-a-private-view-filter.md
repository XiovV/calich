# A Calendar Set is a private per-User view filter, not a bulk toggle

Status: accepted — #300

A Calendar Set is a named, private selection of one Workspace's Calendars belonging to one User (`calendar_sets` + `calendar_set_members`, shaped after `groups` + `group_members`). Selecting one narrows **what exists** in the UI — a Calendar outside the Active Calendar Set is absent from the sidebar and from the Event modal's Calendar picker — while leaving Calendar toggle alone as one global answer per Calendar. The grid renders what is both in the Active Calendar Set and toggled on. "All calendars" is the absence of a Set, not a row.

The obvious cheaper design is the one this rejects: have selecting a Set write `checkedCalendarIds`. That needs no schema at read time, no new filter in `CalendarList`, and no fourth `CalendarPickerEmptyReason` — which is exactly why a future reader will assume it was overlooked rather than refused.

## Why not drive the toggles

Because it creates a dirty state with no honest rendering. Select "Work", then toggle one Calendar: the visible set no longer matches the named Set, and the switcher must either lie (still say "Work"), invent a modified state ("Work\*") with rules about when it resolves, or silently rewrite the Set from a gesture the User made about *today's view*. Every one of those is worse than the thing it saves.

Keeping the two orthogonal removes the question entirely. The Set answers "which Calendars exist right now"; the toggle answers "of those, which are showing". Neither can put the other into a state that needs explaining, `shellStore` gains no new state at all, and switching Sets is navigation rather than mutation — re-entering a Set never wipes a deliberate uncheck.

## The Event picker narrows too, and that is the load-bearing part

`src/lib/calendar.ts` already restricts the Event modal's Calendar picker to *checked* Calendars, and `CalendarPickerEmptyReason`'s `"hidden"` case exists solely to say "you own a Calendar but it's unchecked — go check it". The invariant underneath is **you can only create an Event on a Calendar you can currently see.**

Because the Active Calendar Set is a second way of not seeing a Calendar, it must narrow the picker too, or that invariant acquires an exception nobody can derive from first principles. `CalendarPickerEmptyReason` therefore gains `"outOfSet"`, which fires only when the Active Calendar Set is itself empty; inside a non-empty Set the existing `hidden` / `unwritable` precedence applies unchanged, over the Set's members instead of over every Calendar. Its remedy is "switch to All calendars", exactly parallel to `hidden`'s "check it".

Leaving the picker unfiltered was considered and is genuinely cheaper — it sources from `checkedCalendarIds`, which stays global, so it would need no code at all. It was refused because it would offer Calendars the sidebar is deliberately hiding, and produce an Event that vanishes the instant it is saved.

## Decision

- `calendar_sets(id, user_id, workspace_id, name, created_at)` and `calendar_set_members(set_id, calendar_id)` (#300), mirroring `groups`/`group_members` verbatim — no name-uniqueness constraint, `ORDER BY created_at, id`. A new append-only migration, per ADR-0048's rule that the squash is an occasional deliberate act rather than something every schema change performs.
- **Scoped to `(user_id, workspace_id)`.** A Calendar belongs to exactly one Workspace — a Linked Calendar included, since `ConnectionService.ImportCalendars` creates it *into* one, and the same Provider calendar imported into two Workspaces is two Calendar rows. A Set spanning Workspaces would surface Calendars from a Workspace the User is not currently in, contradicting ADR-0044's Workspace-as-visibility-boundary.
- **Private, with no sharing mechanism and no Role.** Not "private for now": there is no owner-vs-grantee axis to resolve, so the read path is `WHERE user_id = ?` and the question "can B see A's Set?" does not exist. Sitting in Settings' **Personal** group is the position that says so, alongside Preferences and Reminder delivery rather than beside Admin-managed Members and Groups.
- **Membership cascades on `calendars`.** A revoked Share, an unsubscribe, or a disconnected Connection silently shrinks any Set the Calendar was in, with no reconciliation code and no notification. An emptied Set survives; it does not self-delete.
- **An empty Set is legal**, whether created empty or emptied by cascade. Forbidding the first would not prevent the second, so the empty state has to exist regardless and the restriction would buy only an inconsistency.
- **Session state, with no Preference behind it.** The Active Calendar Set resets to "All calendars" on every load and on every Workspace switch. Deliberately unlike Default view (ADR-0039), whose per-User column would here point at a Set belonging to one Workspace and be meaningless in the others; the per-`(User, Workspace)` default this would need — an `is_default` flag, or a column on `workspace_members` — was designed and then dropped as unearned for a first cut.
- **Membership is curated by hand.** Creating a Calendar, subscribing to a feed, or bulk-importing from a Connection does not auto-join the Active Calendar Set. The three creation dialogs instead carry an **unticked** "Add to *N*" checkbox, present only while a Set is active.
- **No effect below the UI.** CalDAV home-set membership stays Exposure's (ADR-0080); Reminders and Notifications stay personal and server-resolved; Write-back is untouched. A Set is a property of what one browser tab is looking through, and nothing that runs without a tab open may consult it.
- **Sidebar grouping survives, filtered.** The My calendars / Subscribed / per-Connection headings stay, each showing only its in-Set Calendars, with an emptied heading hidden — one filter on `CalendarList`'s input rather than a second layout.
- Surfaces: a Settings Section mirroring `GroupsSection` (inline create, rename-in-place, `DeleteCalendarSetDialog`, `CalendarSetCalendarsDialog`); a top-bar switcher, always visible even at zero Sets, built on `Menu` rather than `WorkspaceSwitcher`'s `Select` so it can carry a divider and a "Manage sets…" deep link (ADR-0049 makes Settings route-addressable); and an "Add to set" submenu on each sidebar row's existing action menu.

## Considered Options

- **A private per-User saved filter that narrows what exists (chosen).**
- **Selecting a Set writes `checkedCalendarIds`.** Cheapest by a wide margin and needs no schema on the read path, but produces the dirty state above and makes re-entering a Set destroy a deliberate uncheck.
- **A Set as a container — each Calendar in exactly one, plus an implicit "Ungrouped".** Folder-shaped and familiar, but the motivating use case is a Calendar that belongs to both "Work" and "Personal", which a partition cannot express.
- **Workspace-owned, Admin-managed Sets, shaped like Group.** Would let a Workspace ship a curated set to its Members, but the same Set renders differently for every Member depending on their Access and may render empty, and the feature asked for was a personal one.

## Consequences

- **`shellStore` gains no state.** `checkedCalendarIds` and `knownCalendarIds` keep their exact meaning and their #116 reconcile behaviour; the Active Calendar Set is a separate, simpler value that never writes to them.
- **A Calendar created while a Set is active does not appear**, and the unticked checkbox is the only thing that explains why. This is the design's weakest seam, accepted knowingly: the alternative — auto-joining, by analogy with #175's auto-check — makes the Set's membership a side effect of unrelated actions, which is the property the hand-curation rule exists to prevent.
- **`calendarPickerEmptyReason` becomes Set-aware**, so its callers must pass the Active Calendar Set's members, not the full Calendar list. Its three existing cases keep their exact meanings, now evaluated over a narrower input.
- **Calendar toggle's glossary entry gains a boundary clause**, because "hidden" and "not in the Active Calendar Set" are now two different ways a Calendar fails to appear and the sidebar renders them differently — one as an unfilled ring, the other as no row.
- **Settings' Personal group acquires its first workspace-dependent Section.** Every other Personal Section's contents are stable across a Workspace switch; this one re-renders. Accepted because privacy, not workspace-independence, is what the Personal grouping communicates.
