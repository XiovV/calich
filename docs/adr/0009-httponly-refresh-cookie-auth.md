# httpOnly refresh-token cookie now that a backend exists

Status: supersedes ADR-0008

The refresh token is now issued and validated by the Go backend as an `httpOnly`, `Secure` cookie, not stored in `localStorage`. The access token is kept in memory only (not persisted client-side); a page refresh re-authenticates silently against the refresh cookie via `refreshAccessToken`. ADR-0008 explicitly named this cookie pattern as the intended long-term design, blocked only on a backend existing to issue it — that backend now exists. `httpOnly` cookies aren't readable by JS, closing the XSS-based token theft risk that came with `localStorage`, which matters more here since this app is meant to run in self-hosted environments with varying levels of hardening.
