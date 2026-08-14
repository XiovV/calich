# The Event save decision is compiled to a plan

Status: accepted

Pressing Save on the Event modal has twelve outcomes. Six are writes — three shapes of create, a non-recurring update, a scoped series edit, and a Reminders-only write for an Event the caller may not otherwise touch. The other six are conversation: bail because nothing actually changed, force "All events" because the rule changed, warn first when that would discard Overrides, skip the warning when there are none, open the scope picker, and resume once it is answered.

All twelve lived in `EventModal.tsx`'s `handleSave` and its two confirm handlers, reachable only by rendering a 1,547-line component that no test renders. **The decision now compiles to a value.** `planEventSave` is a pure function returning a `SavePlan` — one of ten cases — and the modal switches on it.

This is the same move `seriesOperation.ts` made one tier down (ADR-0016): a scoped edit compiles to an ordered `SeriesOp[]` rather than being performed inline. A Save plan sits above it and decides *which* write happens; a Series operation decides what a scoped edit becomes once the scope is settled. The pure predicates that were already extracted for testability — `hasFieldChanges`, `shouldDiscardChildren`, `remindersEqual` — are what the planner calls. They were never the problem: each was tested in isolation while the decision that called them was not.

## The plan describes the conversation, not just the writes

The tempting smaller version covers only the six writes and leaves the modal to decide when to open `ScopePicker` and `DiscardRecurrenceWarning`. It was rejected because the conversational half is where the subtlety actually is.

The no-op check (#141) exists so that a save where only an Attachment changed — which applies to the Master immediately and independently of Save, ADR-0040 — does not prompt for a scope that governs nothing. The forced-"all" path exists because a rule change invalidates existing Overrides' recurrence ids. The warning is raised only when the Master has children, so an untouched series changes its rule with no ceremony. Each is a decision about a dialog, and a plan that models only writes would leave all three exactly where they are.

`askScope` and `confirmDiscard` are therefore plan cases like any other, and the dialogs are what the modal renders in response.

## There is deliberately no executor

`seriesOperation` pairs its planner with two realizations, `applySeriesOps` and `dispatchSeriesOps`, and that pairing is the point there: the local cache and the API must never drift, so both read the same op list. Nothing analogous is true here. A `SavePlan` has one consumer.

Its effects are also half store writes and half component bookkeeping — setting `createdEventId`, starting XHR uploads against the row the create just minted, showing the Attendees section's own error banner, closing the dialog. An `executeSavePlan(plan, deps)` would need every one of those injected, producing a wide interface across a seam nothing varies over. One adapter is a claim, not a seam. The planner returns data; the modal performs effects.

## Dialogs resume by re-planning, not by replaying

The scope picker previously froze `{ changes, reminders }` into `pendingEditChanges` when Save was pressed, and the confirm handler applied that payload. Instead, confirming re-derives the draft from live form state and calls `planEventSave` again with `resolvedScope` or `discardConfirmed` set.

Two things follow. `pendingEditChanges` disappears as a state variable, and — more usefully — both confirm paths converge on the same `editSeries` case rather than each building its own `editOccurrence` call. The two handlers had already drifted into near-duplicates differing only in a hardcoded `"all"`.

Staleness is not a risk: both dialogs are modal over a modal, so nothing can edit the form between raising one and answering it.

## Create is three cases, not one case with flags

The three create shapes differ in control flow, not data. Nothing staged is optimistic and fire-and-forget. Staged Attendees must be awaited before anything is painted on the grid, so a rejected explicit target never shows an Event whose invites silently didn't go out (#187, ADR-0055), and its rejection is an inline banner rather than a toast. Staged Attachments must be awaited because an upload needs a row to reference (#132, ADR-0040), and the dialog then stays open so per-file progress and retry have somewhere to live.

A single `create` case carrying `awaitBeforePaint`, `failureMode` and `closeOnSuccess` would hand that branching straight back to the modal's switch arm and leave "which combinations are legal" unstated. Three named cases keep the choice in the planner, where each is one assertion.

Closing follows the same rule: it is implied by the case rather than carried as a flag. Every terminal case closes except the two that stay open for upload progress.

## Considered Options

- **Extract `handleSave` into a hook.** Rejected: a hook is still only reachable by rendering, so the twelve outcomes stay as untestable as they were. It would move the code without moving the seam.
- **Introduce React Testing Library and test the modal directly.** Rejected for now, and separately: there is no component test infrastructure in the repo at all, and the first such test should not land on its most complex component. Worth revisiting on its own terms — it is the only thing that will ever cover the switch arms.
- **Fold this into ADR-0016.** Rejected: that ADR is about recurrence semantics and the op-list seam beneath this one. Recording a different seam there blurs what it is for.
- **Have the planner own the form's own derivations** — time strings to Dates, the Repeat preset to an RRULE. Rejected: `"09:15"` is an `<input type="time">` value, not domain data. The planner takes the normalized `MasterFieldChanges` the rest of the recurrence code already speaks.

## Consequences

- **The Save button's disabled state derives from the plan.** `canSave` was one expression driving the button and a second, re-checked at the top of `handleSave`; both are now `plan.kind === "blocked"`, so they cannot disagree.
- **`blocked` carries a `reason` nothing renders.** Four distinct refusals — invalid fields, a read-only Event with untouched Reminders, a create already made, a create in flight — would otherwise be one indistinguishable assertion. The reason is a test-surface field, not speculative UI. Which reason wins when two apply is fixed but arbitrary; the behaviour is identical either way.
- **Pressing Enter on a read-only Event with untouched Reminders no longer closes the dialog.** It previously fell through to `onClose()`; it is now `blocked`, consistent with the Save button already being disabled in that state. Escape and Close are unaffected.
- **`StagedAttendeeTarget` is declared in `lib/savePlan.ts`.** The picker that produces one lives in `calendar-grid/EventAttendeesSection`, but a lib module may not depend on a component. That component's identical declaration should re-point here when it is next touched.
- **Delete and export are untouched.** Delete is two branches whose hard part (`planDeleteOccurrence`) is already behind the store seam and tested; export is a read-path pre-flight whose trigger is currently not even rendered. Neither concentrates complexity by being moved.
- **A non-recurring save still writes unconditionally.** The no-op check applies only to the recurring path, exactly as before — there is no scope to prompt for, so there is nothing for it to spare the User.
