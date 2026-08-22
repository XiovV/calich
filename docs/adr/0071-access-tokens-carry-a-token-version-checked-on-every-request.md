# Access tokens carry a token_version, checked against the database on every request

Status: accepted — extends ADR-0037, ADR-0009

`AuthService.Authenticate` used to validate a bearer access token's signature and expiry alone, never touching the database. That made the "Change password" form's promise — **"Signs you out of every other device"** — false for up to `accessTokenTTL` (15 minutes): `ChangePassword` deletes every Session (killing refresh tokens), but an already-issued access token kept authenticating full read/write access until it expired on its own (#242). Change password is the case where this bites hardest — a user changes it *because* they believe someone else has the old one — so the gap is closed there, not qualified away in the UI copy.

## A counter, not a timestamp

`users.token_version` is an integer, bumped by `UpdatePassword`. Every access token embeds the value it was minted against in a `tv` claim; `Authenticate` rejects a token whose `tv` doesn't match the account's current `token_version`.

A `password_changed_at` timestamp compared against the JWT's `IssuedAt` was tried first and rejected: `jwt.NumericDate` round-trips through JSON at whole-second precision, and SQLite's `CURRENT_TIMESTAMP` is also second-grained. A password change landing in the same second as the token it's meant to outdate makes "issued before changed" ambiguous — and it isn't a rare edge case, it's exactly what happens when `ChangePassword` re-issues a session in the same request that revoked the old one (#123). An integer counter has no such tie: the freshly minted token always carries the *new* value outright, never a value that merely happens to compare equal.

## Every authenticated request now costs one indexed read

This is the trade ADR-0037 explicitly declined to make for Disable ("a poor trade for 15 minutes... a database read on every authenticated request"). It's taken here because the two gaps aren't the same size: Disable's blast radius is bounded to one account's own data becoming reachable again, self-inflicted and reversible by re-disabling; a stale access token surviving a password change hands continued read/write to whoever the change was meant to lock out, for the exact scenario the feature exists to cover.

It's also cheaper than it sounds against this codebase's existing shape. `RequireActiveUser` and `RequireEnabledUser` already each run a full `GetByID` per request on every route that has them — `Authenticate`'s new `GetTokenVersion` is a third indexed read alongside two that already happen, not the first. `GetTokenVersion` selects one column rather than the full row `GetByID` returns, keeping its own marginal cost small.

`Refresh` picks up the same read for a different reason: it mints a fresh access token directly (not through `issueSession`), so it needs `token_version` to embed. A live Session already proves no password change has intervened since it was created (`ChangePassword` deletes every Session for the user), so the value it reads back is guaranteed to be the one already in force — the read exists to get the number, not to make a decision with it.

## What this does not close

**Disable's gap is untouched.** `Authenticate` still says nothing about `is_disabled` — that stays exactly where ADR-0037 left it, behind `httpauth.RequireEnabledUser`'s own per-request read, with the same up-to-15-minute window on a Disabled account's stolen access token. Folding `is_disabled` into the same check `Authenticate` now makes for `token_version` was considered and deliberately left out of this change: it would widen scope this issue didn't ask for, and CalDAV Basic auth's own per-request bcrypt path (`AppPasswordService.Authenticate`) already re-checks credentials, so a Disabled user's *native client* traffic is unaffected either way, unlike web sessions.

**Every other account mutation is unaffected.** Renaming, changing email, or flipping preferences doesn't bump `token_version` — none of them are the "someone else might now have my old credential" event a password change is. Widening the bump to more mutations is a one-line change in each repository method if a future issue names one, not a structural one.

## Considered and rejected

- **Session/refresh-token-scoped revocation instead of a global counter.** Would let a targeted "sign out this one device" land later without touching every session, but `ChangePassword` already invalidates every Session unconditionally (#123) — there's no per-session distinction anywhere else in this codebase to hang a scoped check off, and inventing one is speculative scope this issue doesn't need.
- **Caching `token_version` in memory with a short TTL to avoid a DB read on the hot path.** Shrinks the window from instant to the cache's TTL rather than closing it, and buys that shrink with invalidation logic and a multi-process staleness question that doesn't otherwise exist in this codebase (SQLite, one process). Rejected in favor of paying the read outright, consistent with `RequireActiveUser`/`RequireEnabledUser` already doing so.
- **Qualifying the UI copy instead of fixing the behavior.** Accurate and free, but leaves the actual write-access window open — the option is documented in #242, not chosen.
