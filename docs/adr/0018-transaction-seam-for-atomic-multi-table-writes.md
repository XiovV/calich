# Transaction seam for atomic multi-table writes

Status: accepted

Some service-layer writes touch more than one table and must not partially apply — e.g. the Series "this and following" split reparents rows in both `events` and `event_exceptions` (ADR-0016). This ADR records the seam that makes such writes atomic, and its boundaries.

## The seam

A `DBTX` interface (`ExecContext`, `QueryContext`, `QueryRowContext`) is satisfied by both `*sql.DB` and `*sql.Tx`. Each repository holds a `DBTX` instead of a `*sql.DB`, so its queries run against a plain connection or a transaction interchangeably. Constructors (`NewEventRepository`, `NewEventExceptionRepository`) are unchanged — they still take `*sql.DB` — so every existing call site and method signature is untouched.

A repository gains a `WithTx(tx *sql.Tx) *Repository` method that returns a copy of itself bound to that transaction, leaving the original (pool-bound) instance alone.

`repository.WithTx(ctx, db, fn)` owns the transaction lifecycle: begin, then commit if `fn` returns nil, or rollback if `fn` returns an error. `EventService` holds the `*sql.DB` and exposes a private `withTx` helper that opens a transaction and hands `fn` transaction-bound clones of the repositories it needs — the service still calls repository methods exactly as before, just through clones scoped to the transaction.

## Convention

- **Reads and validation happen outside the transaction**, against the pool-bound repositories, before `withTx` is called. The transaction wraps only the writes that must land together.
- **Single-write operations are not wrapped.** Deleting a Master, for instance, relies on `ON DELETE CASCADE` to remove its Overrides and Exceptions — one statement, one table touched directly, no seam needed.
- Multi-table writes elsewhere (e.g. CalDAV ingest, ADR-0013) should follow this same pattern once they exist.

## Non-decisions

No `UnitOfWork` type and no `Series` aggregate. Both would generalize a pattern that, right now, has exactly one repository pair (Event, EventException) needing it. Introduce them when a third table needs to join a transaction — earned complexity, not anticipated complexity.

## Landed with no behavior change

This ADR is prefactoring: it introduces the seam and the convention but wires nothing to it yet. It unblocks the two atomic Series operations that follow (split and delete-scope).
