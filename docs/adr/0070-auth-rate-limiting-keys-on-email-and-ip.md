# Failed authentication is throttled per Email and per IP, Register per IP alone

Status: accepted — extends ADR-0047, ADR-0024, ADR-0044, ADR-0058

Three public surfaces accept unlimited credential guesses today: `POST /api/auth/login`, CalDAV Basic auth on `/dav/*` and `/.well-known/caldav` (App passwords, ADR-0024), and `POST /api/auth/register`. Nothing throttles how often any of them may be called. CalDAV is the sharper case: it's the surface a self-hoster is most likely to expose to the public internet, since exposing it is the entire point of CalDAV, and `AppPasswordService.Authenticate`/`AuthService.Login` bcrypt-compare on every request — a flood of wrong passwords is a CPU-exhaustion vector, not just a guessing one.

## The key is both Email and IP for a guessed credential, IP alone for Register

Login and CalDAV Basic auth are the same problem wearing two transports: both compare a submitted password against one account's stored hash. They share one rate limiter (`AuthRateLimiter`, `internal/service/rate_limit.go`), scoped `"auth"`, keyed on **both** the submitted Email and the caller's IP, each against its own ceiling:

- **Email alone** is defeated by rotating source IPs — a distributed guess against one account never trips a per-IP ceiling.
- **IP alone** is defeated by rotating target emails from one address, and separately lets an attacker behind a shared IP (an office NAT, a VPN exit) exhaust everyone sharing it with a single account's worth of guessing.
- **Both**, independently: a request is refused the moment *either* bucket is at its ceiling. This is the "most implementations track both" shape, chosen over either alone for the reasons above rather than because it's conventional.

Register has no existing credential to key on — the submitted email may not belong to anyone, and typically doesn't on the attempt an attacker cares about (probing which addresses are already taken, or just spamming account creation). It gets its own IP-only ceiling, and — unlike Login/CalDAV, which only charge a *failed* attempt — Register charges the ceiling on every call, success or failure. What's being bounded is call frequency, not a wrong-guess count; a script probing many candidate emails should not get to make each one "free" just because some later turn out registrable.

The check runs **before** any password comparison, against a plain `COUNT(*)` — the cheap half of the two costs this ADR addresses. Once a bucket is at its ceiling, a request is refused for the price of one indexed SQL read, never a bcrypt round.

## The IP is `RemoteAddr`, not `X-Forwarded-For`

`clientip.From` (`internal/clientip`) reads only `r.RemoteAddr`. It does not read `X-Forwarded-For` or `X-Real-IP`.

Both of those headers are attacker-controlled on a request that reaches this server directly — trusting either without a corresponding "which proxies are mine" allowlist would let an attacker set a fresh header value per request and walk straight through the per-IP ceiling, which is worse than the header not existing. Building that allowlist (a `TRUSTED_PROXIES` setting, validating the header's nearest hop only) is real scope this issue doesn't cover.

The documented consequence: **behind a reverse proxy, every request's IP collapses to the proxy's own address.** The per-IP bucket then bounds *the proxy's total* login/register traffic, not any individual client behind it — a coarser ceiling than intended, but not an absent one, and the per-Email bucket (Login/CalDAV) is unaffected by this since it doesn't depend on IP at all. A self-hoster terminating TLS at a reverse proxy in front of this instance should size `AUTH_RATE_LIMIT_PER_IP`/`REGISTER_RATE_LIMIT_PER_IP` with that in mind, or accept the coarser bound.

## A 429 looks identical whether the account exists

`AuthRateLimiter.CheckAuth`/`CheckRegister` return the same `ErrAuthRateLimitExceeded` regardless of which bucket tripped, rendered as the same 429 `rate_limited` body regardless of whether the submitted Email names a real User. This matters because Login's own failure path already collapses "no such account" and "wrong password" into one `ErrInvalidCredentials` (ADR-0047's enumeration posture) — a rate limiter that behaved differently for a real account (say, refusing sooner because *that* account's bucket already had rows from someone else's earlier attempt) would reopen exactly the oracle ADR-0047 closed. Both the check and the failure recording below run identically for an existing and a nonexistent Email.

## A success clears the Email bucket, not the IP bucket

On successful Login or CalDAV auth, `AuthRateLimiter.ClearAuthFailures` deletes the accumulated rows for that Email — a User who mistypes a few times and then gets it right is not left refused by their own earlier mistakes (`ClearAuthFailures` is Email-only; there is no ceiling-decay concern on the success path itself). The IP bucket is deliberately **not** cleared on this success: doing so would let an attacker mix one correct login (their own account, from the same IP as a script guessing at *other* accounts) into an otherwise-throttled run and reset their own cover. The IP bucket only decays passively, by its rows aging out of the rolling window.

Register has no analogous clear: every call charges its IP bucket regardless of outcome, by design (above), so there is nothing to reset on success.

## Ceilings are configurable, windows are not

Mirroring ADR-0058's invite limiter: the rolling window is a fixed constant per scope (15 minutes for Login/CalDAV, 1 hour for Register — `authRateLimitWindow`/`registerRateLimitWindow`), and only the ceiling is env-configurable:

- `AUTH_RATE_LIMIT_PER_EMAIL` (default 10 per 15m)
- `AUTH_RATE_LIMIT_PER_IP` (default 30 per 15m)
- `REGISTER_RATE_LIMIT_PER_IP` (default 20 per hour)

The Login/CalDAV defaults are picked to never trip an ordinary User mistyping their password a handful of times, while still bounding a script to single-digit guesses per minute. The IP ceiling sits above the Email one on the assumption that a shared address (NAT, VPN) legitimately produces more attempts than one person alone — a coarser bound is the accepted cost of not being able to tell "many people, one address" from "one attacker, one address" apart from the request itself.

## Storage: one table, not the outbox's actor-keyed shape

A new `auth_rate_limit_attempts` table (`scope`, `key_type`, `key_value`, `created_at`) plays the role `outbox.actor_user_id` plus `CountByActorSince` played for Invitations (ADR-0058) — a rolling-window count, in SQLite, checked before the write it guards. It can't reuse that shape directly: `actor_user_id` keys on an authenticated actor's User id, and by definition a failed Login has no authenticated actor to key on yet. `key_type`/`key_value` generalizes the key to "Email" or "IP", neither of which is a foreign key into `users`.

`Record` prunes every row in its own scope (any key) older than the window on every insert, rather than relying on a background sweep — a repeat offender's bucket never grows past what one window can hold, and there's no second worker to run, stop, and reason about at shutdown. Pruning is scope-wide rather than same-key-only: a same-key prune would never fire for a key written exactly once, and a script that rotates the submitted Email (or IP, behind a shared address it can't rotate) on every request would otherwise leave one permanent row behind per distinct value forever. It stays scoped to the caller's own scope rather than going unconditional, since Login/CalDAV's 15-minute window and Register's 1-hour window are different cutoffs — pruning across scopes with one call's own cutoff would delete the other scope's still-live rows.

## Checked, not transactional

ADR-0058's `chargeInviteRateLimit` runs its count-then-write inside the same DB transaction as the Invitation it guards, closing a TOCTOU gap that matters there: two concurrent invites racing past the ceiling means real mail sent past a configured limit. Here, the check (`CheckAuth`/`CheckRegister`) and the write that follows (`RecordAuthFailure`/`RecordRegisterAttempt`) are two separate statements, not one transaction. A race lets a burst land one or two attempts past the ceiling in the worst case — a bound worth having is not the same as a bound worth serializing every login through a transaction for, and unlike a sent Invitation, a slightly-late 429 costs nothing once it lands.

## Enforced at the HTTP layer, not inside AuthService/AppPasswordService

The check-record-clear sequence lives in `AuthHandler.Login`/`Register` and `httpauth.RequireCalDAVAuth`, calling `AuthRateLimiter` directly — not threaded through `AuthService.Login`/`Register` or `AppPasswordService.Authenticate` as an extra parameter.

Both of those methods are called directly (not through the HTTP layer) from a few dozen existing test fixtures across `internal/service` and `internal/handlers`, which exist to mint a session for an otherwise-unrelated test and have no reason to know about rate limiting at all. Widening their signatures for this would be a mechanical, blast-radius-only change across every one of those call sites for a concern the service layer doesn't otherwise need to know about: `AuthService.Login` still does exactly what it did before this ADR — look up a User and compare a password — and remains callable that way from a test. The HTTP layer is where the caller's IP is naturally available (`*http.Request.RemoteAddr`) and where every one of the three throttled surfaces already terminates, so it's also where the three independent call sites (`Login`, `Register`, CalDAV) converge into one shared `AuthRateLimiter` without a fourth party (the service layer) needing to hold it too.

## Considered and rejected

- **An in-memory limiter (e.g. a token-bucket map).** Cheaper per-check, but resets on every restart (a self-hoster's `docker compose restart` un-throttles an in-progress attack) and doesn't work if this server ever runs as more than one process. SQLite is already the durable store everything else in this app uses, including ADR-0058's identically-shaped invite limiter.
- **One shared ceiling for Login, CalDAV, and Register.** Register isn't the same threat: it has no existing credential to protect, and unlike a wrong Login guess, an registration attempt with a taken email is informative to the caller regardless of any rate limit (ADR-0044's `ErrEmailTaken` already tells them). Folding it into the same scope would either loosen Login/CalDAV's ceiling to accommodate Register's different usage pattern, or throttle Register down to a guessing-resistant ceiling it doesn't need.
- **Trusting `X-Forwarded-For` by default.** Rejected above — worse than RemoteAddr alone without a trusted-proxy allowlist this issue doesn't build.
- **Locking the account outright after N failures (an `is_locked` flag on `users`).** A durable per-account lock is a stronger, more visible User-enumeration oracle than a time-boxed 429 (its state persists and is checkable indefinitely, not just within a rolling window), and turns "attacker knows my email" into "attacker can lock me out of my own account indefinitely" rather than "for 15 minutes." The rolling window already bounds the damage without that persistence.
