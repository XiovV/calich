# The Calendar is the unit of ownership and sharing

Status: accepted — supersedes ADR-0010's single-account rule

ADR-0010 modelled a `User` as its own record so that multi-user support would need "only the 'single account, no invites' business rule lifted, not a data-model migration." That held up better than expected: `calendars`, `events`, `sessions`, `notifications` and `app_passwords` all already carry `user_id`, and every method on `CalendarService` and `EventService` already takes one. There was no single-user data to migrate and no enforcement to remove — the constraint was absent code, not present code.

But the promise held only for **isolated** users. This app is for households, and a household wants a Family calendar that two people can both write. Sharing is where ADR-0010's schema stops being sufficient, because it makes `events.user_id` ambiguous for the first time.

**A Calendar has exactly one Owner and zero or more Shares. An Event has neither — it inherits both from its Calendar.** `events.user_id` is dropped.

## Why the column had to go

`events.user_id` was doing four unrelated jobs at once, and they agreed only because there was one user:

| Job | Where | Correct answer under sharing |
|---|---|---|
| Authorization | 19 `WHERE user_id = ?` sites in `repository/event.go` | the Calendar's Access |
| Reminder recipient | `reminder/due.go` | everyone with Access (ADR-0036) |
| Cascade delete | `ON DELETE CASCADE`, migration 4 | the Calendar's Owner |
| CalDAV home-set | `caldavserver/backend.go` | the accessing principal (ADR-0035) |

The moment Bob writes an Event into Alice's Family calendar, no value of `user_id` satisfies all four. Dropping it forces each job to be answered separately and explicitly, which is what sharing actually requires. The cascade chain survives untouched: `events.calendar_id` already has `ON DELETE CASCADE`, so deleting a User still removes their Calendars and therefore their Events.

Attribution, if wanted, is a *separate* nullable `created_by` with `ON DELETE SET NULL` — deliberately not an access concept, so it can never be mistaken for one. A departing household member's Events must not vanish from a calendar they did not own.

## The model

```
calendar_shares
  calendar_id  TEXT     -> calendars(id)  ON DELETE CASCADE
  user_id      INTEGER  -> users(id)      ON DELETE CASCADE
  role         TEXT      'viewer' | 'editor'
  PRIMARY KEY (calendar_id, user_id)

Access(user, calendar):
  base = Owner        if calendar.user_id = user
         share.role   if a share row exists
         None         otherwise

  if calendar.source_url IS NOT NULL:
      base = min(base, Viewer)      -- ADR-0032

  return base
```

**Access is computed, never stored.** It replaces `user_id = ?` as the single question every permission check asks.

**Ownership is implicit and singular.** The Owner has no Share row; `calendars.user_id` is the whole answer. One accountable User per Calendar, always.

**Role covers Events only.** Editor means full read/write on the Calendar's Events and their Reminders — nothing more. Rename, delete, re-share, revoke, and attaching a source URL are Owner-only. A person trusted with the shopping list should not be able to delete the calendar it lives on, or grant access to someone the Owner never chose. This also maps cleanly onto CalDAV, where those operations are largely not expressible anyway.

**ADR-0032's read-only rule clamps at resolution, not at grant time.** A Subscribed Calendar is read-only to Owner, Editor and Viewer alike — which is already true of the Owner today, so this extends an existing rule rather than inventing one. Clamping at resolution rather than validating at grant is load-bearing: `PATCH /api/calendars/{id}` can attach a source URL to an *already-shared* Calendar (`handlers/calendar.go`), so a grant that was valid when made can be invalidated later. The stored Share keeps saying Editor; it resolves to read-only for as long as the source URL is attached, and comes back if it is removed.

## Vocabulary

**Share**, **Role**, **Owner**, **Access** — defined in `CONTEXT.md`.

There is deliberately **no term for "a Calendar someone shared with me."** "Shared Calendar" points both ways — Alice's Family is shared *out*, Bob's Family is shared *in* — and it would sit directly beside **Subscribed Calendar**, which is also a Calendar you can see and may not write. The glossary already banned "feed" for precisely this defect. We say "a Calendar you have Editor Access to" instead. UI copy may still say "Shared with me"; that is a label, not a domain term.

## Considered Options

- **Drop `events.user_id` (chosen).** No column that can lie, and no chance of a query filtering the wrong one. Costs a mechanical rewrite of 19 query sites and rebuilding `idx_events_user_start` (ADR-0018) around `calendar_id`. At household scale in SQLite the performance delta is noise, and removing the field from the struct makes the compiler find every site.
- **Keep it, meaning the Calendar's Owner.** Every existing query keeps working and the index survives. But `GetByID(bob, dentistEvent)` returns not-found for an Event Bob is explicitly permitted to edit, so each of the 19 sites needs a second, sharing-aware variant beside it — two parallel query paths forever, with the wrong one always available and always compiling.
- **Keep it, meaning the author.** Cheapest insert, and silently wrong everywhere: Alice opening Family would not see an Event Bob created, because the grid filters `user_id = alice`. Nothing errors; the Event is simply missing. Deleting Bob's account would cascade away Events living in Alice's Calendar.
- **Instance-wide "shared: yes/no" flag instead of per-Calendar grants.** No join table, trivial UI, and a real fit for "it's just my family." Rejected because it cannot express *shared with some but not all* — two parents sharing a Finances calendar the teenager cannot see — and retrofitting granularity onto a boolean is a data migration plus a UI rewrite.
- **Grants without Roles.** A Share means see-and-write, full trust within the household. Cheaper now, and a role column could be added later with a default. Rejected because "the kids can see Family but not edit it" is the second thing anyone will ask for.

## Consequences

- **Authorization moves out of SQL and into the service layer.** This is a relocation, not an invention: `requireWritableCalendar` (`service/event.go`) already resolves a Calendar and refuses the write if it is Subscribed — a per-Calendar permission check with nothing to do with `user_id`. Access resolution is that same seam with a second dimension. ADR-0032's reasoning applies unchanged: both the REST handlers and the CalDAV backend funnel through `EventService`, so one guard covers both, and the next entry point added is guarded by default.
- **`EventService.CalendarCTag` is the one method that never took a `userID`.** Harmless while Calendar ids were unguessable UUIDs belonging to the only user; it must take Access now.
- **Every existing row is already correct.** Existing Calendars keep their `user_id` as Owner, existing Events lose a redundant column, and no backfill is needed.
- **Scope: this is Calendar sharing, not event attendees.** No invitations, no RSVP, no free/busy lookup, no iTIP/iMIP scheduling (RFC 6638). Those are a different feature that will also want the word "share," and naming them as non-goals now keeps this ADR from being read as covering them.
