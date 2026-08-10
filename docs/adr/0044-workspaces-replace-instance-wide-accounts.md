# Workspaces replace the instance as the account boundary; instance-wide Admin is retired

Status: accepted — supersedes ADR-0010's single-instance framing, ADR-0037's Admin role, and ADR-0042's Invite mechanism; amended by ADR-0047 (the bootstrapped "name" this document refers to is a display Name — Email is the identifier)

ADR-0010 modelled a self-hosted instance as one shared pool of Users with one Admin (ADR-0037) governing all of them. That stops fitting once an instance can host people who have nothing to do with each other: `ENABLE_SIGNUPS` (default `false`) now lets anyone reach the instance and start their own **Workspace**, so "the instance's accounts" is no longer one list any single Admin could meaningfully own. The first account on an instance always bootstraps (name, email, password — replacing ADR-0010's fixed `admin`/`admin` default) regardless of the flag, and becomes the Owner of its own Workspace.

**A Workspace, not the instance, is now the account-management boundary.** A User belongs to zero or more Workspaces via `WorkspaceMember` rows, each carrying a Role: **Owner**, **Admin**, or **Member**. A User can be a Member of one Workspace and the Owner of another — there is no longer a single "this instance's Admin" answer, only "this Workspace's Owner/Admins."

## Role authority

- **Owner-only:** manage billing, delete the Workspace, grant or revoke the Admin role.
- **Admin:** everything Owner can do except the three powers above. In practice: invite and remove Members (never Owner, never another Admin), create and manage Groups (ADR-0045), create workspace-facing shared Calendars, configure workspace settings (default share privacy, workspace name).
- **Member:** no membership-management powers; participates via whatever Shares or Group Shares they hold, same as anyone else.

**Role grants no data access.** This is ADR-0037's central rule, carried down a level unchanged: *"Admin manages accounts. Admin has no data access whatsoever."* Owner and Admin resolve Calendar Access exactly like a Member does — through ownership or a Share (direct or via Group) — never through role. There is no `or role IN (owner, admin)` branch anywhere in Access resolution. The reasoning ADR-0037 gave for instance-wide Admin (a second, untested authorization path; a privacy surprise) applies identically here.

## Removing a Member

Removing a Member ends only that one `WorkspaceMember` row — their User account and any other Workspace memberships are untouched. But they may own Calendars *inside* this Workspace (ADR-0045: even workspace-facing Calendars stay User-owned), so removal requires the same explicit disposition ADR-0037 defined for account deletion — transfer each such Calendar to another Member, or delete it — scoped to this Workspace's Calendars only.

## Invites are workspace-scoped, not account-scoped

ADR-0042's Invite let an Admin create a Pending account and hand out a password-setting link, instance-wide. That mechanism is retired along with instance-wide Admin. A Workspace invite instead targets an email:

- If no User exists with that email, accepting the invite prompts account creation (name, email, password) and the new User is added as a Member of the inviting Workspace in one step.
- If a User already exists with that email, accepting just adds a `WorkspaceMember` row to their existing account — no new account, no password step.

There is no more "direct creation with an Admin-set temporary password" path (ADR-0042's fallback) — with no instance Admin, nobody is left to create an account on someone else's behalf outside of this invite flow. `Pending` as an account state (ADR-0042) is retired with it: a User invited by email either doesn't exist yet (and creates their own account, choosing their own password from the start) or already does (and needs no state change at all).

## Account lifecycle becomes self-service

With no Admin left to act on someone else's account, ADR-0037's Disable/Delete pair is re-scoped to the account holder alone:

- **Active** — normal.
- **Disabled** — self-chosen and reversible. A User may disable their own account and re-activate it later themselves; nobody else can disable it for them.
- **Deleted** — irreversible, still gated by ADR-0037's transfer-or-delete disposition for owned Calendars, now evaluated per-Workspace (a User may own Calendars in several Workspaces at once).

Both Disable and Delete are blocked while the User is the sole Owner of any Workspace that still has other Members in it — the Workspace-scoped analogue of ADR-0037's last-Admin guard. They must transfer Ownership, or reduce the Workspace to just themselves, first.

## Considered Options

- **One Workspace per self-hosted instance (rejected).** Simpler, and closer to today's shape, but self-registration plus multi-tenancy on the hosted product means the same account/Calendar model has to support many Workspaces regardless — running two separate models (self-hosted: one Workspace; hosted: many) for no benefit a self-hoster asked for was rejected in favor of one model everywhere.
- **Keep instance-wide Admin as a superadmin over all Workspaces (rejected).** Useful for support/debugging on your own box, but reintroduces exactly the "second authorization path" problem ADR-0037 rejected, now one level worse — it would need to see across Workspace boundaries that otherwise don't know about each other. `ENABLE_SIGNUPS` and per-Workspace Owners cover the actual levers a self-hosted operator needs.
- **Multi-workspace membership as a later addition (rejected).** Considered scoping a User to exactly one Workspace at first (simpler data model, no switcher UI). Rejected because the hosted product's pitch — one person invited into a colleague's Workspace while keeping their own — is a first-class use case, not an edge case to bolt on later.

## Consequences

- `users` loses `is_admin`, `invite_token_hash`, `invite_expires_at`, and its Pending/Disabled boolean; a `workspace_members` table gains `role` and carries what those columns used to encode, per-Workspace.
- The Admin account-list UI (ADR-0042) is replaced by a per-Workspace member-management UI scoped to that Workspace's Owner/Admins.
- Billing is deferred: a Workspace carries an opaque subscription status the Owner manages; seat-based vs. flat pricing is a product decision, not a domain one, and is out of scope here.
