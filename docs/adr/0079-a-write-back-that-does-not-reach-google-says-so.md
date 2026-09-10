# A Write-back that does not reach Google says so, quickly and specifically

Status: accepted — amends ADR-0075 (Write-back's failure surface) and ADR-0060 (the outbox's statuses)

Three rules, one posture. A push that Google rejects **fails on the first response** rather than walking the backoff schedule; a push that was **never dispatched** is recorded as `skipped`, not `sent`; and a failed call **keeps Google's own explanation** instead of discarding it.

ADR-0075 built Write-back's failure surface on the premise that a push either lands or is visibly refused: *"a pending push needs no UI; it lands in seconds"*, with a per-Event marker and the Connection's needs-attention state for the permanent case. Every part of that was implemented. None of it fired, because all three rules above were the other way round, and each one independently hid the same fault.

## What the absence of these rules cost

Every Write-back this app had ever sent failed with **400**. The patch body carried an empty `date` beside a populated `dateTime` — a pair Google treats as mutually exclusive — because the encode path shared the decode struct, which has no `omitempty`. That is an ordinary bug, fixed in one struct, and it is not what this ADR is about.

What this ADR is about is that the bug survived in production while three separate mechanisms designed to surface it stayed silent:

- **The 400 was retried as though it were an outage.** Only 412 and a dead grant were classified; everything else fell through to 10s / 30s / 2m / 10m. A request that is guaranteed to be rejected identically five times consumed thirteen minutes before its Event could show a marker — and a server restart mid-schedule left the row `pending` forever, where the marker never arrived at all.
- **A push that was never dispatched was recorded as `sent`.** The "nothing left to push" paths returned a bare `nil`, which the Worker cannot distinguish from delivery. The outbox therefore contained rows claiming Google had accepted an edit it had never been shown — beside a stale `last_error` from an earlier attempt, which read as a contradiction and was in fact two different truths about two different attempts.
- **Google's explanation was thrown away.** Every call drained its failed response into `io.Discard` beneath a comment asserting *"the response carries nothing we need on failure"*. For a 4xx that is precisely backwards: the body names the offending field. `last_error` said `status 400` and nothing more, so the one artefact that should have ended the investigation instead started it.

None of these is severe alone. Together they made a total, permanent, every-single-write failure look exactly like nothing happening.

## The four statuses

| status | meaning |
| --- | --- |
| `pending` | queued, or waiting out a backoff |
| `sent` | dispatched, and Google accepted it |
| `skipped` | terminal; never dispatched, with the reason in `last_error` |
| `failed` | dispatched, and permanently rejected |

`skipped` is terminal like `sent` — the Worker does not retry it, because the state that produced it does not resolve on its own — and, unlike `failed`, raises no per-Event marker of its own. Most skips are races the User caused: they deleted the Event, they disconnected the account, they let a Refresh flip the Calendar read-only. The two that do leave the User's intent unfulfilled (an undeliverable DELETE or CANCEL_INSTANCE) already raise the Source's needs-attention state before skipping, exactly as ADR-0077 required; the status change does not weaken that, it stops the row from also claiming delivery.

`sent_at` stays NULL on a skip. A timestamp there would reintroduce the ambiguity the status exists to remove.

**The alternative was to keep `sent` and write the reason into `last_error`.** It needed no migration and would have fixed the misleading *row*. It was declined because it does not fix the misleading *query*: "did my edits reach Google?" is answered by `status`, and every tool, every future reader, and every one of these investigations starts there. ADR-0048 makes the schema cost near zero while this project is pre-release, which is the moment to spend it.

## Failing fast on 4xx

400, 401, 403 and 404 are marked permanent on the first response, via an `ErrPermanent` sentinel the Sender wraps and the Worker checks before its retry arithmetic. Three statuses stay on the retry path because they describe a moment rather than the request: **408**, **429** (where waiting is exactly the remedy), and **412**, which SendWriteBack's own bounded refetch loop owns and which must not be reclassified underneath it. 5xx and transport errors are untouched — those are the outage backoff was built for.

**The cost is stated plainly, because it is how this trade goes wrong.** A 403 raised by an ACL change that is reverted a minute later now fails permanently where it might once have retried into a success. Editing the Event re-enqueues the push (ADR-0077), which is the recovery. That is a better default than every genuinely hopeless push holding its Event's marker back for thirteen minutes, because the hopeless case is the common one and the transient-403 case is the rare one.

## Keeping the Provider's reason

A failed response's body is read to a bounded prefix (512 bytes), collapsed to a single line, and carried on the error — at **every** Google call site, not only the one that bit us. The bound is not about Google, whose error JSON is small; it is about a proxy or captive portal answering with an HTML page that would otherwise flood `last_error` and a grid tooltip.

Logging the body instead of storing it was the alternative, and was declined for the reason this whole ADR exists: the outbox row is where someone actually looks, and a row that cannot explain itself sends them to the code.

## Consequences

- **The bug class this closes is "a mechanism that reports nothing is indistinguishable from a system with nothing to report."** All three rules are instances of it. It is worth naming, because the next integration will have its own.
- **Tests must assert what is sent, not only what is not sent.** ADR-0075's suite is a thorough set of *negative* assertions — no attendees, no `conferenceData`, no visibility, no unexpected top-level key — and it was airtight. It never descended into `start`/`end`, so a body that was well-formed at the Go type level and invalid on the wire passed everything. Golden marshalled bodies, one per encoding shape, now pin the bytes; the negative assertions remain, because they guard a different thing.
- **A type serving both encode and decode will eventually satisfy neither.** The decode side wants every field present and bare; the encode side must send exactly one of a mutually-exclusive pair. That disagreement is not visible in the struct, only on the wire. The two are now separate types, and the encode one enforces its invariant in `MarshalJSON` so no call site can opt out — the same standard ADR-0075 already set for the field-scoped patch.
- **Historical rows are not rewritten.** A past `sent` that was really a skip stays `sent`; inventing a reason for it would be worse than leaving it. The distinction begins here.
