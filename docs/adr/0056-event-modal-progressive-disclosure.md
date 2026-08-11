# The Event modal splits into primary and secondary fields, auto-expanding the secondary ones only when populated

Status: accepted

The Event modal renders eleven fields as one scrolling column — Title, Location, Description, All day, Start/End, Calendar, Color, Repeat, Reminders, Attachments, Attendees — each a labelled block stacked on the next. It scrolls on most screens before anything has been typed. Almost every Event is a title and a time; the modal asked for all eleven every time, so the common case paid the full cost of the rare one. This is the decision that fixes what counts as a primary field versus a secondary one — every field added to this modal from now on has to answer it.

## Primary and secondary

Primary: Title, All day, Start/End, Calendar (with its colour swatch). Secondary: Location, Description, Repeat, Reminders, Attachments, Attendees. Only the primary set renders by default; a "More options" action reveals the rest, one-way for that modal session — a "Fewer options" control that re-hides fields the User may have just filled would cost more than the row it saves.

All day sits in the primary set despite being a checkbox rather than a headline field, because it changes what "when" even means — an all-day Event has to stay quick to create, not get buried behind a click.

## The rule: auto-expand if populated

```
isExpanded =
  mode == create  ?  false
  mode == edit    ?  hasSecondaryEventFields(occurrence, master)
```

`hasSecondaryEventFields` is true when any of Location, Description, Repeat, Reminders, Attachments, or Attendees is non-empty. It is a pure function of the Event being opened, recomputed once per open via a lazy `useState` initializer — never written to `shellStore`, `localStorage`, or a Preference. Two people looking at the same Event see the same modal, and there is no hidden variable to explain when they don't.

Create always starts collapsed: its initial form state returns empty strings and `reminders: []` on every secondary field by construction, so `hasSecondaryEventFields` is trivially false and there is nothing to special-case. Edit starts collapsed only when every secondary field is empty, and expanded when any has content.

## The server change this rule needed

`Event` carried nothing about Attendees; `EventAttendeesSection` fetched them in a `useEffect` after mount. At the moment the modal decides collapsed-or-expanded, it did not know whether the Event had Attendees — breaking the rule on the most common shape of Event that has any: a meeting with a title, a time, six people invited, and nothing else set. It would open collapsed with its people hidden, exactly the failure the rule exists to prevent.

`Event` (and `repository.Event`) now carries `attendeeCount`/`AttendeeCount`, populated by `EventService.attachAttendeeCounts`, batched the same way `attachCreatedByNames` and `attachCalendarMeta` already are, and wired into the same `List`/`Get`/`Create`/`Update` call sites those two are. This is the same kind of field `CreatedByName`, `Attachments`, and `CalendarColor` already are: not a column, attached by the service layer so a single read carries everything the modal needs at open, with no late reflow once a fetch lands 100–300ms after the cursor has already moved toward a control. It pays for itself twice — the Attendees row renders its own summary immediately too, without waiting on `EventAttendeesSection`'s own round-trip.

Unlike `Attachments`, `AttendeeCount` is not restricted to Masters — an Override can carry its own Attendees, and the count must be accurate for whichever Event row the disclosure rule is handed.

## Read-only Events follow the same rule

Subscribed feeds, Viewer access, and Attendee-only Events use the identical disclosure path as editable ones — one code path, one mental model, rather than a second set of rules for a viewer. Since an Attendee-only Event has Attendees by definition, it always auto-expands, and because the Attendees row renders its content inline rather than as a click-through summary, RSVP stays reachable without an extra step.

The accepted cost: "More options" on a read-only Event reveals rows the User cannot edit. This was chosen over a dedicated read-only viewer, which is what the reference calendar apps do and which has cleaner semantics, because splitting the component into an editor and a viewer is a materially larger change than this one. If it grates in practice, that split is the fix — and this decision is what forecloses it as an easy follow-up, since a viewer/editor split would need to re-derive this same rule twice.

## Rejected alternatives

- **Edit always expanded.** Simpler — one fewer state to reason about — but abandons the calm in the mode a User spends most of their time in: a title-only Event would still show six empty rows every time.
- **Always collapsed with a badge** (e.g. "3 more fields ›"). Hides real data behind a click — the User has to click to read their own meeting's location — which is progressive disclosure's one unforgivable failure: making a User work to see something they already told the app.
- **Per-field disclosure**, closest to Fantastical and genuinely tempting: each field reveals independently rather than all six together. Rejected because it turns the layout into one of sixty-four shapes instead of two, and needs its own "once revealed, stays revealed" rule so that clearing a field's content doesn't make its row vanish out from under the cursor mid-edit.

## Consequences

- `repository.Event` gains `AttendeeCount int`; `Event` (frontend) gains `attendeeCount?: number`; the wire response gains `attendeeCount`, `omitempty`, mirroring `calendarColor`'s treatment exactly.
- `EventService.attachAttendeeCounts` runs one batched `AttendeeRepository.ListUserIDsByEventIDs` query per `List`/`Get`/`Create`/`Update` call, the same cost `attachCreatedByNames` already pays.
- The Event modal owns one boolean, set once at mount and never reset for the life of that mounted instance — closing and reopening the modal (a fresh mount, keyed by draft/occurrence in `AppShell`) re-derives it from scratch.
- Every field this ADR calls secondary must, from now on, justify staying primary rather than defaulting to visible — the default for a new field on this modal is "behind More options" unless it's as load-bearing as All day.
