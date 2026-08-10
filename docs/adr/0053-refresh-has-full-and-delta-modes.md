# Refresh has a Full mode and a Delta mode; in Delta, absence never means deletion

Status: accepted — amends ADR-0033

**Refresh** widens to cover any Source. Its definition loses "whole-document"; what still separates it from Sync is that it is one-way and poller-driven, whereas Sync means CalDAV's two-way, change-tracked exchange. The reconciler gains two modes, and the rule that distinguishes them is the one thing in this feature that can destroy a User's data.

## The trap

ADR-0033's reconciler has three buckets: *parsed* → upsert, *absent from the feed* → **tombstone**, *present but unparseable* → leave alone and count as skipped. The middle bucket is sound only because an ICS feed is a whole document — everything the publisher still lists is present, so absence really does mean deletion.

A Provider's incremental response is a **delta**. Absence means *unchanged*, which is the overwhelming majority of every response. Point ADR-0033's reconciler at delta input and it tombstones the entire Calendar on the first incremental cycle, then re-creates it on the next full sync with fresh row ids — which CalDAV clients correctly read as every event being deleted and a different set created. That is not a subtle bug: it is the exact failure ADR-0033 was written to prevent, reintroduced through the front door by reusing the code ADR-0033 produced.

Deletions do not need to be inferred, because Google returns them explicitly: a deleted event appears in the incremental response with `status: cancelled`, precisely so clients can remove it from storage.

## The two modes

- **Full** — the initial sync for a newly Linked Calendar, and recovery after an expired cursor. The complete list is fetched and ADR-0033's three buckets apply unchanged, including tombstone-by-absence. Matching is on the Provider's event id.
- **Delta** — a cursor is presented and only changes come back. Upserts are applied; deletions are applied **only** when the Provider says so explicitly. Tombstone-by-absence must be *structurally unreachable* here, not merely unused — the mode carries the incoming set, and the function that computes tombstones-by-absence is not in scope for it. A guard that could be forgotten is not adequate for a rule whose failure mode is silent mass deletion.

The third bucket survives in both modes and keeps ADR-0033's meaning: an item present but unmappable is skipped and counted, never treated as absent. Unmappable is not absent, in either mode.

## Cursor expiry is where this gets decided

An expired `syncToken` returns **410 GONE**. Google's documented advice is "a full wipe of the client's store and a new full sync".

**We do not take that advice.** A wipe re-mints every row id, bumps `change_seq` on everything, and makes every CalDAV client re-download the whole collection plus a tombstone per old object — for a Calendar in which, typically, nothing has changed at all. Instead, 410 triggers a **Full mode Refresh**: fetch the complete list and reconcile it against the rows already stored, matching on Provider event id, preserving row ids. Unchanged series are left untouched and no client notices anything happened.

This matters more than it sounds, because 410 is not rare. Google invalidates sync tokens on **ACL changes**, and the flagship use case for this feature is calendars shared into the connecting account — exactly the calendars whose ACLs change.

Two smaller consequences of the cursor contract: the query parameters on every incremental request must be **identical** to those on the initial full sync, so any window or filter chosen at connect time is locked in for the life of the cursor and re-applied on every 410 recovery. And a cursor that fails to store is a correctness problem rather than a performance one — the next cycle silently becomes a Full Refresh, which is safe, so the failure is invisible and must be logged.

## What is reused unchanged from ADR-0033

The poller, its stagger, its exponential backoff to a ~24h ceiling, its reset on success, its two error classes (*needs attention* for 401/404 — a human must act; *retrying* for timeouts, 5xx and DNS), the rule that failure preserves the last good state and never disables the Source, and the rule that a manual refresh bypasses both the backoff and the unchanged short-circuit. Connections get their own default cadence of roughly fifteen minutes rather than the feed default of an hour, because a delta Refresh against an unchanged calendar costs one request returning an empty list — far cheaper than a conditional GET over a whole ICS document.

## Push notifications are deliberately out of scope

Google can push, and it would cut latency from minutes to seconds. It is excluded, and the exclusion costs nothing structurally: **push notifications carry no body.** They are a bare "something changed" signal, so the handler's entire job is to call the same Delta Refresh sooner. Adding push later touches the trigger and nothing in the reconciler.

What it would cost now is disproportionate. The receiving URL must be publicly reachable HTTPS with a CA-signed certificate — self-signed is explicitly rejected — which excludes most self-hosted instances outright (LAN, Tailscale, plain HTTP behind a reverse proxy). Channels expire with no automatic renewal, so keeping them alive is its own scheduled job. That is a second delivery path, a webhook endpoint to harden against forgery, and a renewal scheduler, for a capability that silently does nothing for the majority of this app's users.

## Consequences

- **The mode is the reconciler's most important input and must be explicit at every call site.** It is not inferred from whether a cursor happens to be present, because "cursor missing due to a storage failure" and "cursor deliberately absent" would then be indistinguishable, and one of those must not silently enable tombstone-by-absence.
- **Delta-mode reconciliation is table-tested against the specific regression this ADR exists to prevent:** a delta response containing one changed series, against a stored Calendar of many, must produce exactly one upsert and **zero** tombstones. That test is the executable form of this document.
- **Freshness improves from up-to-an-hour to roughly a coffee break,** which for the "my Google calendar, in this app" use case is close to the difference between trusting it and not.
