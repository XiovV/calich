---
name: Bug report
about: Something is broken. Filed raw — /triage labels it.
title: "<area>: <what's broken>"
labels: ""
assignees: ""
---

<!--
Use CONTEXT.md's exact terms (Calendar, Access, Occurrence, Override, Master,
Exception, Workspace, Member, Group, Share). Triage searches the codebase by
domain concept, not by wording — the right noun points it at the right code.

Don't propose a fix, and don't add labels. Both are triage's job.
-->

## Preconditions

<!-- The state the world must be in first: Workspace/Membership, Calendar
ownership, Access level, Group membership, whether the Event recurs.
Delete this section if a fresh account reproduces it. -->

## Steps to reproduce

1. <!-- From a known start state: "open Week view on 2026-08-10" -->
2.
3.

## Expected

<!-- One sentence. If user-visible, say what the screen should render. -->

## Actual

<!-- What happened instead. Exact error text / status code if there is one. -->

## Environment

- Client: <!-- web + browser version, or CalDAV client + version -->
- Timezone: <!-- yours, and the Event's if different -->
- Frequency: <!-- every time / intermittent — N of M attempts -->
- First seen: <!-- version, commit, or "unknown" -->
