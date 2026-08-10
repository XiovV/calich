# CalDAV authenticates with per-User app passwords over HTTP Basic

Status: accepted — amended by ADR-0047 (the Basic-auth identifier is Email, not username)

Native calendar clients speak HTTP Basic (or Digest), not the web app's JWT-access-token + refresh-cookie flow. CalDAV requests authenticate with **HTTP Basic over TLS**, validated against per-User **app passwords** (app-specific passwords) stored as hashes in a new `app_passwords` table — never the account's real login password. A CalDAV Basic-auth middleware sits in front of the `/dav/` routes (ADR-0023), parallel to the web app's `RequireAuth`, and resolves Basic credentials to a `userID` that feeds the same downstream services.

## Why

- **Basic, not Digest.** Digest exists to avoid sending the secret in cleartext, but the server is TLS-only, so Basic suffices and is far simpler. Basic also lets us store only a bcrypt/argon2 **hash**; Digest needs a reversible HA1, which is worse.
- **App passwords, not the login password.** Native clients cache the credential on every synced device. We don't want the real account password sprayed across devices, and we want per-device revocation. A `app_passwords (id, user_id, label, hash, created_at, last_used_at)` table: the user generates one in web settings, it is shown once, they paste it into the client. This is the natural successor to ADR-0012's discarded feed token — a credential separate from login, independently revocable — in the form native clients expect.

## Considered and rejected

- **Reuse the web login password directly.** Sprays the real password across device keychains and can't be revoked per device without changing the account password. Rejected.
- **Digest auth.** No cleartext benefit over Basic on TLS, and forces a worse-than-hash credential store. Rejected.
