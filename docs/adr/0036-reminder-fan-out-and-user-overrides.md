# Reminders fan out to everyone with Access; per-User overrides are a delivery preference

Status: accepted

A Reminder on an Event in a shared Calendar fires for **every User with Access to that Calendar** (ADR-0034), not just the Owner or the author. Bin day on the Family calendar should nag everyone it belongs to — a shared calendar that only notifies one person is a display surface, not a coordination tool.

Any User may then **override that Reminder for themselves**: retune the offset, change the Channel, or mute it. The override is a *server-side delivery preference*. It does not change the Event.

## The split that makes this work

`event_reminders` remains the Event's shared Reminders — what the author set, what ADR-0020 maps one-to-one onto `VALARM`, and **what CalDAV serves to every principal, byte-identical**. A per-User override layers on top at fire time and is never projected into iCalendar.

```
event_reminders          -- shared; the Event's VALARMs, unchanged
user_event_reminders     -- per-User override
  user_id, event_id
  offset_minutes, channel, muted

fired_reminders
  UNIQUE (reminder_id, user_id, occurrence_start)   -- user_id is new
```

`fired_reminders` must gain `user_id` in its unique key: once Bob fires at a different offset than Alice, one ledger row per `(reminder, occurrence)` can no longer represent what happened. `notifications` already carries `user_id`, so the delivery fan-out itself needs no migration — one firing inserts N rows.

Per-User gating that already exists composes naturally. ADR-0027's `synced_device_reminders_enabled` is per-User, so one firing may create a Notification for Bob while skipping it for Alice; a per-User ledger key makes that expressible rather than awkward.

## An offset override replaces the Event's Reminders, it does not retune each of them

An Event may carry more than one Reminder (a day before by Email, 30 minutes before by Notification). A User's offset override is a statement about the Event — "remind me two hours before this" — not a transformation applied to each of the Owner's Reminders in turn. So a User with an offset override gets exactly **one** DueReminder per Occurrence, at their own offset, regardless of how many Reminders the Event carries; the Event's other Reminders are not separately evaluated for that User. Firing every one of the Event's Reminders with the override's offset substituted in was the original implementation, and it was a bug (#110): every Reminder collapses onto the same instant and Channel, so the User gets N identical alerts instead of one.

A Channel-only override carries no such statement about timing, so it does not collapse anything: every Reminder still fires at its own offset, re-channelled. A muted override still drops the User from every Reminder, as before.

The ledger key for an offset-overridden User's single trigger is one of the Event's own Reminder ids (the lowest, arbitrarily but consistently), not a synthetic id — `fired_reminders` has no row type of its own for "the override's trigger," and inventing one is unnecessary just to name a ledger entry.

## The accepted conflict with ADR-0027

Once a User connects a device, that device fires its own `DISPLAY` alarm from the synced `VALARM` — the server is not in that loop (ADR-0027). Because CalDAV serves the *shared* VALARMs, a User whose device fires alarms gets the **Event's** timing on that device, not their override.

This is accepted rather than solved, and it is narrower than it first sounds: ADR-0027's toggle already means "my device owns my pop-ups." A User who has enabled it has delegated Notification timing to their device; their override still governs the Email channel, which no device ever fires. A User who has *not* enabled it — the default, and every web-only User — gets their override honoured on every channel.

## Considered Options

- **Fan out to everyone with Access, plus per-User overrides (chosen).**
- **Owner and Editors only, never Viewers.** Role carries the intent — Editor means "this Calendar is mine too," Viewer means "I just want visibility" — and it needs no new table. Rejected once overrides were on the table: with a mute available, the blunt heuristic stops being necessary, and defaulting Viewers to silence would hide reminders someone deliberately wants (a parent watching a child's school calendar).
- **Only the Owner and the Event's `created_by`.** Nobody is ever surprised, and Bin day silently fails to nag anyone else.
- **Project overrides into CalDAV per principal.** Bob's `.ics` carries Bob's VALARMs, so his devices honour his override everywhere — correct in the strongest sense. Rejected as a change to the sync *core* rather than an addition beside it: ETag and CTag would become per-`(User, Calendar)`, sync tokens would have to be tracked per User rather than per Calendar, and ADR-0025 and ADR-0026 would both need reopening. Revisit only if device-side override fidelity becomes a real complaint.
- **Mute/unmute only, no retuning.** No CalDAV conflict at all, and much cheaper. Rejected as narrower than what a household actually wants — leaving the house 2 hours before a shared appointment is a different need from leaving 30 minutes before.

## Consequences

- **A read-only Share is now a notification source.** Sharing a busy Calendar as Viewer will push Reminders to the recipient until they mute it, which is a discoverability problem the settings UI has to solve rather than a model problem.
- **Reminder delivery cost scales with the size of the Share set.** At household scale this is irrelevant; it is worth knowing before anyone shares a Calendar with a large group.
- **An override is scoped to the Event, not the Occurrence.** Overriding "Bin day" retunes every future Bin day for that User. Per-Occurrence overrides are not modelled, matching how Reminders themselves work.
- **Overrides survive Event edits only if the Event survives.** ADR-0020 wholesale-replaces `event_reminders` rows on Event update, which resets fired history by design. A `user_event_reminders` row keyed on `event_id` outlives that replacement; one keyed on `reminder_id` would not, which is why it is not.
- **An offset override collapses a multi-stage Reminder to one stage.** A User overriding an Event with a deliberate two-Reminder design (a day before *and* half an hour before) loses the second one — their override states their own single timing, and it wins outright rather than blending with the Owner's.
