# Reminders are personal, with per-Calendar defaults

Status: accepted — supersedes ADR-0036, amends ADR-0020, ADR-0025, ADR-0026, ADR-0027 and ADR-0056

ADR-0036 made a Reminder a property of the Event that fans out to everyone with Access, and gave each User a *modifier* over it — one offset, one Channel, one muted flag. **A Reminder is now a property of a (User, Event) pair.** The Event has none of its own; Users do. Alice creating an Event on a shared Calendar reminds Alice, and nobody else, until they say otherwise.

The modifier is what forced the question. Expressing "remind me at a different time" as a substitution into somebody else's list produced a UI of three checkboxes over a concept nobody has — *mute for me*, *remind me at a different time*, *notify me on a different channel* — and a firing rule (ADR-0036's collapse-to-one-trigger, and the #110 bug before it) that exists only to reconcile one person's single modifier against another person's list of Reminders. A User cannot add a second Reminder, cannot retune two of them differently, and cannot remove one. The thing they actually want is the control the create modal already has: **Add reminder.**

This is the same move ADR-0038 made for Calendar color — per-User state where a shared value used to be — but it goes further, and the difference matters. Color has no absent state: a Calendar must render as *something*, so the Owner's value stays as the default everyone starts from. A Reminder's absence is meaningful and common, so there is no shared value left underneath. `event_reminders` gains `user_id`; `user_event_reminders` is deleted outright, and with it `offset_minutes`, `channel`, `muted`, and `representativeReminder`.

## What a shared Calendar loses, and what replaces it

ADR-0036's case was that "bin day on the Family calendar should nag everyone it belongs to — a shared calendar that only notifies one person is a display surface, not a coordination tool." That case is still good. What it got wrong is *who states the intent*: fan-out has the author of an Event deciding, once, on behalf of everyone who will ever have Access to that Calendar, including people invited to it years later.

**A default reminder — per User, per Calendar — states it from the other side.** "Everything on Family reminds me 30 minutes before." Set once by each person, on the Calendar they chose to keep, applying to every Event on it including the twelve someone else added last week. Bin day still nags everyone; each person opted in themselves, and each can retune or silence a single Event without touching anyone else's.

Defaults ship *with* this change rather than after it. Personal Reminders alone would mean opening every Event by hand to hear anything at all, which is a worse calendar than the one being replaced.

### Defaults resolve, they do not materialize

A default applies to every Event on the Calendar where that User has set no Reminders of their own. Nothing is copied at creation time, at share time, or on first view — resolution happens where the Reminders are read.

Materializing (copying the default into rows when an Event appears) would remove the resolution order entirely and make every Reminder that fires a row someone can see, which is genuinely attractive. It was rejected because it is stale by construction: changing a default would not reach existing Events, and Events predating the default would get nothing. That is the same "open twelve Events by hand" problem the feature exists to solve, arriving a week later.

Resolution costs exactly one bit, named here rather than smuggled in: with absence meaning *use my default*, "no Reminders on this one Event" needs a marker. One boolean per (User, Event), written whenever a User saves their list for an Event — including when they save it empty. Its entire meaning is "my list here is explicit". It carries no offset, no Channel, no mute, and it has no UI: deleting your last Reminder row on an Event is what sets it.

Two defaults per (User, Calendar), not one: an all-day Reminder's offset counts back from 09:00 on the date rather than midnight (ADR-0020), so a single "30 minutes before" resolves to 08:30 on bin day itself, after the bins have gone. Timed and all-day are separately useless to each other.

They are set in the Calendar's own edit modal, in each User's own view of it, beside their color override — the two are the same kind of state and the Calendar is the only place with the context to pick a value. Not a Settings Section, which would need a Calendar picker to say the same thing.

**Create mode pre-fills nothing.** A new Event opens with an empty Reminders section and gets the creator's default by resolution like any other Event, so retuning the default later moves their own Events too. Pre-filling would write explicit rows on save and silently opt every creator out of their own default, forever.

## CalDAV serves each principal their own VALARMs

ADR-0036 considered projecting overrides into CalDAV per principal and rejected it: *"a change to the sync core rather than an addition beside it — ETag and CTag would become per-(User, Calendar) … ADR-0025 and ADR-0026 would both need reopening."*

That rejection assumed a shared truth to serve instead. There no longer is one, so the option stops being an addition and becomes the only coherent answer: **a principal's `.ics` carries that principal's resolved Reminder set, and a PUT carrying `VALARM`s writes the PUTting principal's Reminders and touches nobody else's.** The alternatives are worse than what they replace — serving nobody's kills ADR-0020's founding premise that a Reminder round-trips as a `VALARM`, and serving the creator's to everyone means a phone firing alarms for an Event the web app says you have no Reminders on.

It is also cheaper than ADR-0036 feared:

- **ETag falls out for free.** ADR-0026 already requires deriving it from the normalized reconstruction, and `currentObjectETag` already takes a `userID`. Per-principal bytes yield a per-principal ETag with no new mechanism.
- **CTag is the real cost, and it is waste rather than divergence.** `change_seq` is one global counter stamped per row, so Bob editing his own Reminders bumps the Calendar's CTag for everyone. Alice's client re-runs sync-collection, refetches, and gets byte-identical content under an unchanged ETag. Changing a *default* re-serializes every object in that Calendar for that User, which is the same waste once per settings change. Making `change_seq` per-(User, Calendar) is the fix if this ever bites, and is the ADR-0025 reopening still being avoided.
- **ADR-0027's conflict is resolved rather than inherited.** Every device now fires its own User's alarms, so `synced_device_reminders_enabled` becomes a coherent per-User statement — "my device owns my pop-ups" — instead of a shrug about whose timing a shared `VALARM` carries. ADR-0036's "accepted conflict with ADR-0027" section no longer describes anything.

## Considered Options

- **Personal Reminders plus per-Calendar defaults (chosen).**
- **Keep the fan-out, widen the override into a full personal list.** The Event's Reminders stay as the default everyone starts from; a personal list, when set, replaces them wholesale. Precisely ADR-0038's shape, keeps bin day nagging with no new concept, and kills the collapse bug just as well. Rejected because the author still decides for everyone by default, and because "I have no personal list" and "my personal list is empty" must then be different states on the *Event* — the tri-state this design pays only once, at the Calendar, where a default is a thing a User deliberately set rather than a thing they inherited.
- **Seed a copy of the creator's Reminders to everyone with Access at creation time.** Bin day nags everyone and every copy is independently editable. Rejected: a fan-out write per create, no answer for Users who gain Access later, and the creator fixing a typo'd offset reaches nobody.
- **A single global per-User default** in Preferences (ADR-0039), instead of per-Calendar. Cheaper by a table. Rejected on granularity: any default strong enough to be useful on Family is noise on a subscribed holidays feed or on a colleague's Calendar kept visible for scheduling, and the remedy is to turn it off.
- **Both, global overridable per Calendar.** A third resolution layer for a self-hosted household app.

## Consequences

- **Every Reminder that exists today stops reaching anyone but its author.** Acceptable only because ADR-0048 places this pre-release, with no instance holding data that isn't the author's. The schema change edits `00001_init.sql` in place; there is no migration.
- **An invited-only Event has no default.** A User-backed Attendee with no Access to the Event's Calendar (ADR-0046, ADR-0058) has no per-(User, Calendar) row that could hold one, and no Calendar settings surface to set it from — so an accepted meeting reminds them of nothing until they add a Reminder by hand. Left open deliberately: the fix is one Preference ("Events I'm invited to"), purely additive, and adding a second defaults surface with a different shape than the one just chosen is not worth doing speculatively.
- **Setting a Reminder is not an Event write.** Every User in the old fan-out set may set their own — Owner, Editor, Viewer, and Attendees with no Access at all. It therefore bypasses both the Viewer restriction and the read-only clamp a Source puts on a Calendar, exactly as `calendar_user_colors` does (ADR-0038). The Reminders section stays live on an Event whose every other field is disabled.
- **ADR-0020's wholesale-replace narrows to the acting User's rows.** Alice editing a title can no longer wipe Bob's Reminders. Its other rule — an Override copies the Master's Reminder set — now copies *every* User's rows, not just the editor's: creating an Override must not silently drop other people's Reminders for that Occurrence, and it creates nothing for anyone who had none.
- **A Reminder change bumps `change_seq` but never `SEQUENCE`.** It is not a material change under ADR-0059, so it must not re-send Invitations.
- **A Source's alarms belong to the Calendar's Owner.** A Subscription with `KeepAlarms` on, or a Linked Calendar, writes the Owner's rows — they are the one who asked for the feed's alarms — and `ClearSubscribedCalendarReminders` clears only those. Everyone else with Access to that Calendar gets their own default, or silence. The Owner hand-editing a Reminder on a subscribed Event will lose it the next time the feed changes *that series*, since `ReconcileSubscribedSeries` rewrites what it reconciles.
- **`fired_reminders`' `user_id` is now redundant in the unique key.** A Reminder id implies its User, which is what ADR-0036 needed the extra column to express.
- **ADR-0056's disclosure guarantee weakens from "two people see the same modal" to "disclosure is never remembered".** `reminderCount` is a per-viewer signal now, so Bob's modal auto-expands on Bin day and Alice's does not. Kept as a signal deliberately: your own Reminder is the field you are most likely to have set and most likely to want to see without hunting for it.
- **`ReminderOverrideControl.tsx`, `reminderOverrideApi` and the `/reminder-override` endpoints are deleted.** Personal Reminders still need their own write path, since a Viewer cannot PUT the Event they are attached to.
