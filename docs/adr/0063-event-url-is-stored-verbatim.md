# The Event URL is stored verbatim and clickable only when it is http(s)

Status: accepted

An Event gains a `URL` — one optional link to more information about it: a ticket, a document, a video call someone pasted by hand. It is RFC 5545's `URL` property, the field Apple Calendar exposes as "URL" and Thunderbird as "Link", and the reason it needs a decision rather than following `location` and `description` into the free-text column next door is that it is the first Event field this app turns into an `href`.

## Verbatim in, gated at render

Two forces pull opposite ways.

Losslessness pulls toward accepting anything. CalDAV clients put non-web schemes in `URL` routinely — Apple Calendar writes `message://<id>` to link an event back to the mail it came from, and feeds carry `webcal:`, `mailto:`, and vendor `x-` schemes. Rejecting those on write means a native client's event silently loses data on every sync, or its `PUT` fails outright.

Safety pulls toward validating. A stored string rendered as `<a href={url}>` accepts `javascript:`, which on a shared Calendar is a stored XSS an Editor writes and every Viewer clicks.

**Both are satisfied by putting the check in exactly one place, and it is not the boundary.** The value is stored exactly as received — by the API, by CalDAV, by import, by Refresh — in a nullable `TEXT` column no different from `location`. The web app parses it at render and offers a click-through only when the scheme is `http` or `https`. Anything else renders as plain text: an Apple `message://` link survives a round-trip through this instance intact, and is simply not a link you can follow from a browser, which is the truth about it.

This is what makes "we never emit a dangerous `href`" true by construction. Validating at the boundary instead would make it true only for as long as every current *and future* write path remembers to validate — and this Event field already has four of them.

The accepted cost: the database will accumulate values that are not URLs at all, including whatever a user typed and abandoned. This is the same cost `location` already pays, and the field is displayed to humans rather than dereferenced by the server, so nothing downstream has to trust it.

## Following the link

The modal renders an `<Input>` plus a trailing open-in-new-tab button (`target="_blank"`, `rel="noopener noreferrer"`), present only when the render gate above says the value is followable — so a `message://` value shows no button rather than a button that does nothing.

The button stays **enabled when the Event is read-only**. Secondary fields on a read-only Event render as `disabled` inputs, and a disabled input cannot reliably be selected or copied in most browsers; without this, a Subscribed feed's event or one visible only through an Attendee invite — precisely the events whose link the viewer did not write and most needs — would display a URL with no way to reach it. "Cannot edit" must not collapse into "cannot open".

Under ADR-0056 the field is **secondary**, rendered directly under Location, and is a signal in `hasSecondaryEventFields`. Without that last part an Event whose only extra content is a link opens collapsed and hides it, which is the failure ADR-0056 exists to prevent.

## What it deliberately is not

- **Not a conference link.** RFC 7986's `CONFERENCE` — repeatable, carrying `FEATURE=VIDEO` and a label, and what a client renders as a Join button — is a different property for a different job. This app has no conferencing integration to populate it. A user may of course paste a meeting link here; that is them using a general-purpose field, not this app modelling a call.
- **Not a way to attach a file.** ADR-0040's Attachment is bytes this instance stores. A URL pointing at a document elsewhere is now representable, but it is a link and nothing more: no download, no size, no export inlining, no `ATTACH` property. `CONTEXT.md`'s Attachment entry is amended to say so, because it previously claimed a link to a file elsewhere was not modelled at all.
- **Not material for iMIP.** Under ADR-0059 a re-issued `REQUEST` bumps `SEQUENCE` only for start, end, `RRULE`, and `all_day`. Editing a URL joins title, description and colour in re-sending without a bump: pasting the correct link an hour before the call must not reset six people's Accepted back to Needs-Action. The known consequence is that many mail clients ignore an unbumped re-send, so an Attendee may keep the stale link — the right trade against wiping a meeting's responses over a corrected paste.

## Inbound: the source link, not the permalink

`URL` is decoded wherever iCalendar is parsed — CalDAV `PUT`, `.ics` import (ADR-0030), and Subscription Refresh — and joins `Title`/`Description`/`Location` in `reconcile`'s change detection, which would otherwise treat a feed's changed link as unchanged.

When Connections land (ADR-0050, ADR-0052), a Provider's **user-set source link** maps here and its **permalink does not**: for Google, `source.url` — documented as the page, mail, or document the event was created from, and writable — and never `htmlLink`, the read-only link into Google Calendar's own web UI.

They look interchangeable and are not. Every Google event has an `htmlLink`, so mapping it would put a URL on 100% of Linked Calendar events: the field stops distinguishing "someone attached a link" from "this event exists", every mirrored event auto-expands its modal by the disclosure rule above, and the link points at a UI the viewer may not be signed into. It is also close to unwithdrawable once people have started clicking it. `hangoutLink`/`conferenceData` are `CONFERENCE`'s territory and are likewise not mapped here.

Getting back to the event at its Provider is a real want; it belongs to a per-Linked-Calendar "Open in <Provider>" affordance, decided when Connections are built, and does not need to cost this field its meaning. The exact Google field names are worth re-confirming against the API at that point — the shape of the decision holds regardless.

## Rejected alternatives

- **Validate at the API boundary, http/https only.** One rule, everything stored is clickable, no render-time parsing. Rejected because it breaks native-client round-trips on values Apple Calendar writes by default, and because the guarantee it offers is per-write-path rather than structural.
- **Validate in the web form only, accept anything over CalDAV.** Round-trips survive, but the renderer still faces arbitrary schemes and needs the gate anyway — so this is a nicety layered on the chosen design, not an alternative to it. Form-level normalization (prefixing `https://` onto a bare host) remains available later on exactly that footing.
- **A list of URLs.** RFC 5545 forbids more than one `URL` per `VEVENT`, so a list would need `ATTACH;VALUE=URI` or a repeating `CONFERENCE` and would not round-trip as what it is. Description remains where several links go.

## Consequences

- `events` gains a nullable `url TEXT`; `repository.Event`, the service write structs, the wire payloads, and the frontend `Event` each gain the field alongside `Description`/`Location`, following those two's existing `omitempty` treatment exactly.
- `buildVEvent` emits `URL` when non-empty; `ical_decode` parses it into `ParsedEvent`; `reconcile`'s two comparison sites gain one term.
- `EventDisclosureSignals` gains a seventh member and `hasSecondaryEventFields` a seventh clause.
- The Reminder email is deliberately untouched. A link ten minutes before a call is genuinely the most useful thing that mail could carry, but "the Reminder email carries the Event's details" is a decision about that email, to be taken for `location` and `url` together rather than smuggled in behind this change.
