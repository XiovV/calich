# A pending Write-back is invisible to the reconciler's absence rules

Status: accepted — amends ADR-0053

A series with a queued Write-back is **subtracted from the tombstone candidates** before a Full Refresh computes absence, and a series with a queued *delete* **discards any incoming update** for it. Neither rule is a guard the reconciler checks and could forget: the pending set is removed from the input, so the destructive branch never sees those rows.

## The interleaving

ADR-0075 queues the push so that an edit succeeds locally whether or not the Provider is reachable. That creates a window — usually seconds, occasionally a week (ADR-0075's broken-Connection case) — in which this app holds an Event the Provider has never heard of.

A **Full Refresh** landing inside that window is a data-loss event. Full mode reconciles a complete listing, so absence genuinely means deletion (ADR-0053), and a locally-created Event is absent for the most innocent possible reason: it has not been sent yet. **It is tombstoned before it was ever pushed**, and the user watches an Event they just created disappear on its own.

This is not a remote corner. ADR-0053 records that Full mode is entered not only on first sync but on every **410** cursor expiry, that Google invalidates sync tokens on **ACL changes**, and that calendars shared into the connecting account — the flagship use case — are exactly the ones whose ACLs change. Full Refresh is a routine occurrence, not a rare recovery.

The mirror case is quieter and just as wrong: a queued *delete* whose series arrives in the next Delta Refresh as an ordinary update. Reconciled naively, the Provider's copy is upserted back and the Event the user deleted **returns**.

## Why the pending set is subtracted rather than checked

ADR-0053 already established the shape of this argument for Delta mode: tombstone-by-absence had to be "*structurally unreachable*, not merely unused", because "a guard that could be forgotten is not adequate for a rule whose failure mode is silent mass deletion."

The same standard applies here for the same reason. A reconciler that computes tombstones and *then* filters out pending rows is one refactor away from filtering in the wrong order. Removing them from the candidate set before the computation means the destructive path has no way to reach them.

## Consequences

- **This needs its own table test, because nothing else will find it.** It reproduces only when a Full Refresh interleaves with a queued push — never in manual QA, never in a normal cycle, and not in any test that exercises Refresh and Write-back separately. The assertion is: one locally-created Event with a pending push, one Full Refresh over a listing that does not contain it, **zero** tombstones. That test is the executable form of this document, exactly as ADR-0053's delta-mode test is of that one.
- **"Pending" must mean pending, and outlive a restart.** The set is derived from the `outbox` rows themselves rather than a flag on the Event, so a push that is queued, retrying, or mid-backoff all count, and a crash between the local commit and the enqueue is the only hole — which is why both happen in one transaction (ADR-0018).
- **A push that permanently fails stops protecting its row.** Once ADR-0075 marks the Event needs-attention and abandons the push, the row rejoins the ordinary reconciliation and may be tombstoned by a later Full Refresh. That is correct — it never reached the Provider and never will — but it means the per-Event failure marker is the user's only chance to act, and the reason it must be visible in the grid rather than buried in Settings.
