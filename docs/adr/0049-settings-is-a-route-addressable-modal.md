# Settings is a modal that keeps its routes

Status: accepted

Settings was a whole page. Clicking the gear in `TopBar` navigated to `/settings`, which unmounted `AppShell` entirely and replaced the calendar with a back-arrow, a nav rail and one Section — for changing a week start or copying an app password. Going there cost the User their grid, and coming back rebuilt it from scratch.

**Settings becomes a modal rendered over the calendar, and keeps every route it had.** `AppShell` is promoted from the leaf of `/` to the layout parent that renders an `<Outlet />`; the Settings routes become its children. `/settings/account` therefore paints the calendar underneath and a dialog on top, and `getSettingsSections()` stays the single source of truth that both the route table and the rail read.

## Why not drop the routes

The obvious modal is a piece of client state — `const [settings, setSettings] = useState(null)` — and it is what the reference UI this was modelled on appears to do. **Rejected**, twice over. A reload or a shared link would land on the calendar with the modal gone, and browser Back would either do nothing or throw the User out of the app entirely. Keeping the routes costs one `<Outlet />` and buys deep links, refresh-survival, and Back-closes-the-modal for free. A `?settings=account` query param was also considered and rejected: it is a second routing mechanism running alongside the router, for no gain over child routes.

The history handling is the part that will look fussy without this note. Opening **pushes**, so browser Back closes the modal. Switching Section **replaces**, so Back doesn't walk the User back through all seven Sections one at a time. Closing **replaces** with `/`, so a tab opened directly on `/settings/account` closes to the calendar instead of exiting the app.

## Settings sits on a lower z-rung than its own dialogs

Every dialog in this app hardcodes backdrop `z-40` / popup `z-50`. Eight of them — `DeleteAccountDialog`, `ImportPreviewDialog`, `GroupMembersDialog`, `RemoveMemberDialog`, `DeleteGroupDialog`, `CancelInviteDialog`, `ReissueInviteDialog`, `ExportSummaryDialog` — are opened from inside Settings and now open on top of it. At equal `z`, their backdrops would land *under* the Settings popup: a "Delete your account?" confirmation floating over a fully-lit Settings.

So the Settings modal takes the rung below (`z-30` backdrop, `z-35` popup) rather than the eight being bumped to `z-60`/`z-70`. It is the one-file change instead of the nine-file change, and it leaves every dialog outside Settings — `CalendarModal`, `EventModal`, `SubscribeCalendarModal` — untouched. Base UI's Dialog already handles the rest of nesting (`nestedOpenDialogCount`, Escape order, focus return), because those dialogs render inside the Settings dialog's React tree.

This does mean the codebase now has three stacking rungs held in hardcoded Tailwind classes and no named scale. Replacing the ad-hoc pairs app-wide with layer tokens was considered and deliberately deferred — it touches every dialog, popover and the Toaster, which is a wider change than this feature justifies.

## Consequences

- **The rail groups its Sections: Personal and Workspace.** `getSettingsSections()` gains a `group` field. The split follows how the code is already scoped — Workspace and Groups are per-Workspace (ADR-0045); Preferences (ADR-0039), Account, Reminder delivery, App passwords and Import & export are per-User. A visible "Settings" heading above the groups is the dialog's accessible name, so no Section's own `<h2>` has to double as the dialog title.
- **Outside-click closes it, losing unsaved text.** `AccountSection` has explicit Save buttons for name and email. Dismissing on outside press is what every other dialog here does, and the same exposure `CalendarModal` already accepts; a dirty-state guard was considered and rejected as machinery nothing else in the codebase has.
- **`AppShell` no longer unmounts when Settings opens.** Its fetch effects don't re-run, the grid keeps its scroll position, and a Preference changed in the modal is reflected in the calendar behind it immediately. ADR-0039's "navigating to Settings and back does not re-seed Default view" holds a fortiori.
- **Closing mid-import is safe.** `ImportExportSection` awaits `importApi.commit` and then `fetchCalendars()`/`fetchEvents()` — store actions, not component state — so unmounting the Section mid-flight still refreshes the stores and still toasts.
- **Sections lose horizontal room.** They render in a fixed modal (~56rem wide, capped height) rather than a full-height page, which the Groups and Workspace member tables feel most.
