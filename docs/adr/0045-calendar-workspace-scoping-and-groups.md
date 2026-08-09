# Calendars are workspace-scoped; Groups add dynamic, membership-based Shares

Status: accepted — amends ADR-0034

ADR-0034 made a Calendar the unit of ownership and sharing, with exactly one User Owner and zero or more per-User Shares. ADR-0044 adds Workspace as a boundary above User. This ADR settles where Calendar sits relative to it, and introduces Groups as a second Share target.

## A Calendar belongs to exactly one Workspace

`calendars` gains a required `workspace_id`. A Calendar is created inside the Workspace that is active at creation time, and stays there — there is no cross-Workspace Calendar. Switching the active Workspace in the UI changes the entire visible Calendar list, not just a share-picker's contact pool: the Workspace is the container a person's calendar life lives in, matching the billing model (ADR-0044) where the Workspace, not the User, is what's paid for.

**Calendar ownership is unchanged from ADR-0034 — a Calendar has exactly one User Owner, always.** This holds even for Calendars an Admin creates on the Workspace's behalf: a Workspace Admin who creates "Company Holidays" and shares it with the whole Workspace owns that Calendar personally, the same as any other Calendar they create. There is no Workspace-owned Calendar. Tying it to whichever Admin happened to create it, rather than inventing ownership-by-Workspace, keeps ADR-0034's Owner invariant — and its transfer-or-delete disposition on removal (ADR-0044) — as the one rule that governs every Calendar without exception.

## Groups

A **Group** (e.g. "Tech team", "Design team") is scoped to a Workspace, created and membership-managed by that Workspace's Owner or Admin. A User may belong to any number of Groups within a Workspace they're a Member of.

Share (ADR-0034) extends to optionally target a Group instead of a User:

```
calendar_shares         -- unchanged: (calendar_id, user_id, role)
calendar_group_shares   -- new: (calendar_id, group_id, role)

Access(user, calendar):
  base = Owner                                     if calendar.user_id = user
         max(direct share role, any group share role
             for a group `user` currently belongs to)
         None                                       otherwise
  ...clamped as ADR-0034 already defines
```

**Group membership is resolved dynamically, not expanded at share time.** Sharing "Company Holidays" with the Tech team means adding someone to Tech team later grants them Access immediately, with no one touching the Calendar again; removing them revokes it the same way. This is the entire point of reaching for a Group rather than a per-user Share — a Group that only reflects membership *at the moment of sharing* would just be a slower way to multi-select people, and the stated need (an office manager not wanting to re-share every time someone joins) is dynamic by nature. The cost is that removing someone from a Group is now understood as an Access-affecting action, not merely an org-chart edit.

Both a Share (User or Group target) and an Event Attendee invite are restricted to members of the Calendar's own Workspace — see ADR-0046 for the Attendee side of this rule.

## Considered Options

- **Groups as a share-time convenience, expanded to a snapshot of current members (rejected).** Cheaper — no new Access-resolution join, no "does this person still have access" question. Rejected because it defeats the actual use case: a Group whose Access doesn't track membership changes needs re-sharing on every hire, which is the exact manual step Groups exist to remove.
- **Workspace-owned Calendars for the "shared with everyone" case (rejected).** Would avoid tying "Company Holidays" to a departing Admin's account. Rejected in favor of keeping Calendar ownership singular and User-scoped everywhere, no exceptions — see ADR-0044's removal-disposition rule, which already handles an Owner leaving without inventing a second kind of ownership.
- **Calendar-agnostic Workspace (Workspace as a people-grouping only, Calendars stay unscoped) (rejected).** Considered and rejected during design — see ADR-0044's framing; a Workspace switcher that doesn't change what Calendars are visible has no real reason to exist.

## Consequences

- Access resolution (ADR-0034) grows a second join through `calendar_group_shares` and current Group membership; every Access check pays that cost, which is negligible at Workspace scale.
- The share picker UI now offers both individual Users and Groups as targets, scoped to the Calendar's own Workspace's member/group lists.
- CalDAV's accessor-home-set model (ADR-0035) is unaffected: a Calendar still resolves to exactly one Access value per accessing principal, however that value was reached.
