# Migrations are squashed until the first release, append-only after it

Status: accepted — governs `server/internal/db/migrations/`

Eight days of development produced 44 migrations. A large share of them exist only to undo each other: `00024_drop_events_user_id` removes a column `00004` created, `00041_drop_admin_and_invite_from_users` retires everything `00025` and `00033` added, and `00027`, `00038` and `00044` each rebuild a table wholesale because SQLite can't alter a constraint in place. The chain records how the schema was arrived at, which is history, not behaviour.

A migration chain earns its keep by moving a database *that already exists and holds data someone needs* from one schema to the next. No such database exists for this project: there are no tags, no releases, and no instance anywhere but a disposable local dev database. Every intermediate state in that chain is a state no database has ever been in or will ever be in again.

**Before the first release, migrations are squashed periodically. From the first release onwards, they are strictly append-only.**

## The trigger

The line is **the first instance whose data isn't mine** — practically, the first tagged release. Not the first deploy: a homelab instance running my own data is still disposable, and I'd know it. What makes a squash destructive is somebody else's database needing an upgrade path, and a release tag is the cheapest observable proxy for that moment.

Once that line is crossed, squashing a migration someone may have already applied silently destroys their upgrade path — their `goose_db_version` records versions that no longer exist, and no future migration can reason about what state they're in. There is no partial version of this rule: after the first release, every schema change is a new file, forever.

## How a squash is done

The danger in a squash is a schema that *looks* right. The procedure is built so that correctness is proved rather than reviewed:

1. Apply the existing chain to an empty database. This — not the dev database, which may carry drift from an edited migration or a manual `ALTER` — is the reference. Drift in a dev database would otherwise be baked into the new canonical schema permanently and invisibly.
2. Write the replacement `00001_init.sql` by hand, preserving each table's column order exactly as the reference produced it, and carrying forward the ADR/issue citations from the migrations being folded in.
3. Apply the replacement to a second empty database.
4. Diff the two: normalized `sqlite_master` SQL, plus a structural comparison over `pragma_table_info` / `pragma_foreign_key_list` / `pragma_index_list` / `pragma_index_info` (columns, types, nullability, defaults, primary keys, foreign keys with their `ON DELETE`/`ON UPDATE` actions, and every index's uniqueness, partiality and column list).
5. Compare seeded rows separately. **A schema dump does not capture data.** `change_sequence`'s singleton row is inserted by a migration and is not optional — the CalDAV sync path reads and bumps that row rather than creating it, so a database without it cannot serve sync at all. Any squash that drops an `INSERT` from a folded-in migration is broken in a way no schema diff will reveal.

A later squash is much cheaper than the first: it folds the handful of migrations added since into the existing `00001_init.sql`, rather than re-reading the whole chain. The verification procedure is unchanged and costs the same either way.

## Consequences

- Migration-behaviour tests die with the migrations they cover. `TestMigration00038_BackfillsWorkspaceIDFromOwnersWorkspace` and `TestMigration00044_FailsLoudlyOnDuplicateEmail`/`_OnNullEmail` asserted one-time transitions between states no database can now be in. They were deleted rather than rewritten: a rewritten version would only assert that SQLite enforces its own `UNIQUE` constraint. The live invariants they touched remain covered at the repository and service layers (`ErrEmailTaken`).
- Reasoning that lived only in a migration comment must move to an ADR before the squash. ADR-0047 already held the fail-loud rationale for 00044, so nothing was lost there — but a migration comment is not a durable home for a decision, precisely because the migration is expected to be deleted.
- Squashing invalidates any existing `goose_db_version`. The local dev database is deleted and recreated rather than repaired; leaving it in place is the worst option, since goose would see version 1 as already applied and start silently, with its recorded version 1 meaning something entirely different from the file now named version 1.
- `db.go` and `testdb.go` are untouched by a squash. Goose stays in place throughout: its version table is what makes the switch to append-only enforceable at the release boundary, and reintroducing it later would be strictly harder than never removing it.

## Considered and rejected

- **Drop goose entirely and apply a `schema.sql` at boot.** More honest about the pre-1.0 situation — there is no migration, only "create the schema" — but it saves one already-working dependency and charges a fiddly reintroduction at exactly the moment a release is being cut. Goose costs nothing per day to keep.
- **Dump the schema from the local dev database.** Convenient and wrong: it cannot distinguish the schema the migration chain produces from drift accumulated while debugging, and once squashed there is nothing left to compare against.
- **Hand-write the squashed schema from reading the 44 files.** Produces the most readable result and, with near-certainty, silently drops an index or a `DEFAULT` somewhere in a 44-file merge. The dump-then-tidy-then-diff procedure above gets the same readability with a proof attached.
- **Squash on a fixed cadence (say, every 20 migrations).** A count is not the thing that matters. The only event that changes what a squash costs is a database existing that someone else depends on.
