# Multi-day all-day Events render, as a repeated per-day chip

Status: accepted — amends ADR-0017

ADR-0017 deferred multi-day all-day Events, describing the deferred work as "horizontal spanning bars, cross-week continuation, bar packing." Import and the API never enforced that deferral, though: an imported `DTSTART;VALUE=DATE`/`DTEND;VALUE=DATE` pair spanning several dates round-trips through storage untouched. Only rendering stopped at the boundary — the all-day lane filtered Occurrences with `isSameDay(occurrence.start, day)` and Month view's chip list had the equivalent blind spot, so a 14–17 September Event showed a chip on the 14th and nothing on the 15th or 16th. The app was silently misrepresenting data it already held (#232).

**Multi-day all-day Events now render, everywhere an all-day Occurrence is shown.** The all-day lane in Day/Week view and Month view's Day cells both switch their day filter from same-day equality to `occurrenceIntersectsDay` (`src/lib/occurrenceSegments.ts`) — the same half-open `[start, end)`-vs-`[dayStart, dayEnd)` overlap test #230 introduced for a timed Occurrence crossing midnight. An all-day Occurrence's `start`/`end` are already whole dates under the half-open convention (ADR-0017), so the test needed no change, only reuse: the same function now backs three call sites (Day/Week's timed grid, the all-day lane, and Month view).

## What this deliberately does not do

ADR-0017's deferred list also named "spanning bars" and "bar packing" — a single visual element stretching across the day columns it covers, with rounded caps only at the true start and true end. This ADR does not build that. Each day column still renders its own independent chip for the Occurrence, same as every other day's Occurrence in that lane; a multi-day one simply now gets one such chip per day it touches instead of one chip only on its first day. Visually contiguous days read as a continuous run because nothing is missing between them, not because a single DOM element spans them. Packing multiple overlapping multi-day bars into shared rows (the way Google Calendar or Outlook lay out a busy week) is still deferred — the all-day lane has no overlap-resolution layout today, single-day or multi-day, and this ADR does not add one.

Creating a multi-day all-day Event from the Event modal is also still out of reach — the modal's all-day date range stays single-day (see the "All-day is unaffected" note in #231's commit). Only import and direct API calls produce a multi-day all-day Event today. That gap is a separate, future ticket.

## Why not defer further

The rendering gap was a correctness bug, not a scope question: the data was already stored and already round-tripped correctly (per #232's investigation), so refusing to render it was strictly worse than the alternative of showing it. The other path considered — having import refuse a multi-day all-day Event and say so in the Import summary — would have meant rejecting data other CalDAV clients and feeds routinely produce, for a rendering limitation the app could just fix. Fixing the renderer was less work than building and explaining a rejection path, and it removes a case where the app's own storage and its own display permanently disagreed.
