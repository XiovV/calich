# Tasks are their own VTODO-shaped entity, on two independent date axes

Status: accepted — the first entity in this repo that is not an Event or a Calendar; adopts ADR-0082's private-outright model and deliberately does not extend ADR-0034 (Access), ADR-0052 (Source) or ADR-0025 (CalDAV) to it

A Task is a first-class entity modelled on iCalendar's `VTODO`, belonging to a Task List that is deliberately **not** a Calendar, carrying a Deadline and a Time block on two independent axes whose presence alone decides where it renders. Both Tasks and Task Lists are private to one User outright, on ADR-0082's terms. Nothing ships over CalDAV or to a Provider in this cut.

The two cheap designs this rejects are the two a reader will assume were overlooked, so both are recorded below.

## Why a Task is not an Event

The cheapest design is `events` plus `is_task` and `completed_at`: it inherits recurrence expansion (ADR-0016), the all-day lane (ADR-0017, ADR-0069), overlap layout (ADR-0004), drag-to-move, Reminders, Access and Write-back, all for nothing.

It was refused because the two entities disagree about their defining property. CONTEXT.md defines an Event as "a titled time block… Its start/end define the first occurrence and the duration every Occurrence inherits." A Task's defining property is that **it need not have a time at all** — the `No date` bucket is a Task with neither Deadline nor Time block. Putting that on `events` makes `start`/`end` nullable, and every consumer of an Event's time — recurrence expansion, the Anchor zone (ADR-0019), multi-day all-day rendering (ADR-0069), the Save plan (ADR-0066) — acquires an "unless it's a task" branch. That is cheap in week one and unremovable in month six.

Shaping the table after `VTODO` instead costs nothing now and makes eventual CalDAV exposure a codec-and-backend job rather than a migration. It is a shape, not a promise: the codec is VEVENT-only today and stays that way here.

## Why a Task List is not a Calendar

The other cheap design is a component kind on `calendars` (`events` | `tasks`) — which is not an invention but CalDAV's own `supported-calendar-component-set`, and is exactly how Apple Reminders lists are modelled. It would give Task Lists Access, Share, Workspace scoping, colour and Exposure for free.

The blast radius was measured before rejecting it and is genuinely modest: 16 query sites across 3 files touch `calendars` directly. **The mechanical cost is not the objection.**

The objection is that every concept currently defined over Calendar silently acquires a second meaning. `Access` would resolve for a thing with no Events. A `Source` — Subscription or Connection — becomes representable on a list of Tasks, and ADR-0052's "a Calendar has zero or one Source" would have to say what subscribing to a task list means. ICS import could target one. `calendarPickerEmptyReason` would have to exclude them. CONTEXT.md's own opening definition — "a named, independently-toggleable collection that groups events" — would become "groups Events *or* Tasks, never both", and a definition with an internal disjunction is usually two concepts sharing a word.

This decision **turns entirely on Tasks being private**, and would be wrong if they were not. Retrofitting sharing onto a parallel entity is the expensive path; doing it via a component kind is the cheap one. The component kind is therefore the right answer to a different question, and the question to re-ask before reopening this is not "was a parallel table a mistake" but "**do Task Lists need to be shared now?**" If that answer ever becomes yes, this ADR is the one to supersede.

## Why time-blocking does not create an Event

Motion and Reclaim both create a real calendar event for a scheduled task, so this is a live alternative rather than a strawman. It dies on privacy: an Event belongs to a Calendar, a Calendar is shared with a Role, so time-blocking a private Task onto one would publish it to everyone with Access. Avoiding that needs a hidden per-User Calendar — a new grouping noun that punches through ADR-0034's calendar-scoped ownership — or it needs Tasks to stop being private.

It also creates a two-row sync problem with no good answers: delete the Event, does the Task un-block? Complete the Task, does the Event vanish? Drag the Event to another Calendar, what does that mean for a Task that has no Calendar? Every one of those is a rule to invent and then keep true forever.

## The two date axes, and why they are independent

`VTODO`'s `DUE` is a **deadline**, and RFC 5545 makes `DUE` and `DURATION` mutually exclusive. So the Deadline and the Time block are separate fields and neither is derived from the other:

- **Deadline** (`DUE`) — when it is due. Decides the Task bucket.
- **Time block** (`DTSTART` + duration) — when it is being worked on. Decides where it draws on the grid.

"Due Friday, blocked Tuesday" is the ordinary case for time-blocking, not a conflict to reconcile. Collapsing the two — storing a Time block's end in `DUE` — was the first model drafted and it silently destroys the deadline the moment you schedule work against it, which removes the feature's main use case. **Rescheduling never touches the Deadline.**

## Decision

- `tasks` and `task_lists`, both scoped `(user_id, workspace_id)` and cascading on both, as a new append-only migration per ADR-0048.
- `tasks` columns named for what `VTODO` holds: title (`SUMMARY`), notes (`DESCRIPTION`), `due` (`DUE`), `start` + duration (`DTSTART`), `priority` (`PRIORITY`, 0–9), `completed_at` (`COMPLETED`).
- **Rendering is a strict precedence**, so a Task appears exactly once: Time block → hourly grid; else Deadline → all-day lane; else the Tasks panel only. No discriminator column — presence of the fields *is* the state, as with a Floating Event's absent `tzid` and "All calendars" being the absence of a Set.
- **Task buckets are derived at render, never stored** — Overdue / Today / Upcoming / No date, from the Deadline against today in the Viewer zone, falling back to the Time block's start. Storing them would require a job moving every Task across buckets at midnight in every User's timezone. Four buckets, not three: an undated Task is not overdue, not today, and not upcoming.
- **Completion is `completed_at`, nullable, not a status enum.** `IN-PROCESS`, `CANCELLED` and `PERCENT-COMPLETE` are unmodelled because nothing here can produce one — no CalDAV in, no Provider in, one radio button. Deliberately unlike Response (ADR-0046), which models all four `PARTSTAT` values precisely because a mail client can send any of them; ADR-0026 already establishes lossy round-trips as an accepted position.
- **Exactly one Task List per `(User, Workspace)` is the default** — a row with a movable flag, auto-created as "Inbox", renameable and recolorable like any other. Not a nullable `list_id` meaning Inbox: Inbox has every property a Task List has (name, colour, a row in the Lists filter), and the absence model would make each of those a hardcoded constant plus a sentinel id, and Inbox the one Task List that cannot be renamed.
- **Deleting a Task List reparents its Tasks to the default**; the default cannot be deleted while it holds the flag.
- **Private outright, with no sharing mechanism and no Role**, exactly as ADR-0082 specifies for a Calendar Set. There is no owner-vs-grantee axis, so "can B see A's Task?" is not a question the code can ask.
- **No effect below the UI.** No CalDAV exposure, no Provider Write-back, no Refresh, no `VALARM`, no participation in ICS import or export — the codec stays VEVENT-only. A Task is a thing one User has; nothing that runs without a tab open consults one.

## Considered Options

- **A `VTODO`-shaped entity of its own, private, on two independent date axes (chosen).**
- **An Event with `is_task` + `completed_at`.** Free machinery, at the cost of nullable start/end and a task-branch in every time-consuming code path.
- **A Task List as a Calendar with a component kind.** CalDAV-native and only ~16 query sites, but doubles the meaning of Access, Source, import and the Calendar definition itself. The right answer if Task Lists ever need sharing.
- **Time-blocking creates a linked Event.** What Motion and Reclaim do; incompatible with Tasks being private, and a permanent two-row sync problem.
- **A single date field, with a flag for "is this a deadline or a block".** Simpler schema, but cannot express "due Friday, working Tuesday", which is the entire point of time-blocking.

## Consequences

- **The Event machinery is not inherited and must be paralleled.** Overlap layout (ADR-0004) has to accept a mixed population of Occurrences and Time blocks or they will draw over each other, and `useOccurrenceDragCommit` needs a Task sibling. This is the real, accepted price of not building on `events`.
- **A Deadline notifies nobody.** With no Reminder on a Task (a Reminder belongs to one User on one Event, resolved server-side per-principal — ADR-0020, ADR-0064), a deadline passes in silence. This is the design's weakest seam, accepted knowingly: the alternative is a second reminder path that would later have to be merged into the first.
- **A drop on the all-day lane clears the Time block.** It must, or precedence would keep drawing the Task on the hourly grid and the drag would visibly snap back after having written something. It is the one gesture in the feature that destroys data rather than adding it.
- **Month view can move a Time block but never create one.** A drop there preserves time-of-day if there is one and otherwise sets a Deadline, because inventing an hour from a gesture that named no hour is worse than the asymmetry with Week view. Year view renders no Tasks at all; its Mini-months stay day-numbers-and-density-dots, and "Show tasks on calendar" is inert there.
- **`tasks` is not fetched by date window.** A no-date Task has no window to be in and the panel must always show it, so the store is shaped after `calendarsStore` rather than `eventsStore` — which is only safe because Tasks are private (small cardinality) and have no recurrence to expand. Completed Tasks are the bounded exception.
- **Manual ordering is unavailable while grouping is derived.** A stored position would be a position in a set that dissolves at midnight and re-forms with different members. It becomes coherent only under Group-by-Task-List, where the rows are a stable list, and can be added there without disturbing the derived order.
- **"Group" is now overloaded in conversation and must not be in code.** A Workspace Group is a set of Members; the panel's headings are Task buckets. The UI's "Group by" label is copy, not a term.
