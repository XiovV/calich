# Invitations are iMIP, and the reply comes back over IMAP

Status: accepted — reopens ADR-0046's "no iTIP/iMIP wire protocol" non-goal; ends ADR-0055's deferral of an `ORGANIZER` property in the codec

ADR-0046 kept the Attendee model inside this app and declared the wire protocol out of scope. Once an Attendee can be a bare address (ADR-0058), that position collapses: an address is only reachable by mail, and a mail that isn't an invitation is a notice about a meeting rather than the meeting itself. Every mainstream product — Google, Proton, Outlook — sends `METHOD:REQUEST`, and the mail clients on the receiving end are built to render it.

## What is sent

Every Attendee receives an `Invitation`: a `text/calendar; method=REQUEST` part their client renders as an invite card with its own Accept / Decline / Tentative.

**Everyone, Member or not.** The Member who most needs an Invitation is the one who doesn't have the app open, which is most people most of the time — an in-app Notification is a surface you see when you next look, not delivery. And after ADR-0058, whether that surface exists at all depends on Workspace membership, so emailing only outsiders would make "was I told about this meeting?" turn on invisible account state. This is the first mail this app sends that a User cannot switch off anywhere; that is right for the one message where not receiving it means missing a meeting.

**Without the Event's `VALARM`s.** The recipient's client applies its own default alarm, as it does for every other invitation in their life. A Reminder here is closer to a delivery preference than to a property of the meeting — ADR-0036 built a whole per-User override mechanism on that reading — so pushing the Organizer's timings onto someone else's phone inverts it. Concretely, an Email-Channel Reminder is `ACTION:EMAIL`, and exporting it asks a foreign client to email its owner about your meeting on a trigger they never chose.

**The full lifecycle, not just the first message.** A re-issued `REQUEST` with an incremented `SEQUENCE` when the Event materially changes — start, end, `RRULE`, `all_day` — which is what makes a recipient's client reset their `PARTSTAT` and re-ask them (RFC 5546). A `METHOD:CANCEL` when the Attendee is removed or the Event deleted. Title, description, URL (ADR-0063), colour and Attendee-list changes re-send without a bump, so nobody's Response is reset because someone fixed a typo. Sending `REQUEST` alone would put a copy of the meeting in a calendar we don't control and then never speak to it again, which is precisely the failure iMIP exists to prevent.

## `ORGANIZER` differs by serialization target

- **In the Invitation**: `ORGANIZER;CN=<Name>:mailto:<SMTP_FROM>`, with the message's `Reply-To:` set to the organizing User's own address.
- **Over CalDAV and in a Calendar file**: `ORGANIZER;CN=<Name>:mailto:<the User's own email>`.

This split is what makes inbound possible at all. An iMIP `REPLY` is addressed to the `ORGANIZER`, and this instance cannot read a mailbox at gmail.com — the mainstream products can only intercept replies because they *are* the mail provider. Putting an address this instance owns on the outgoing `REQUEST` is the only version of that available to a self-hosted calendar.

It is not applied uniformly because the cost over CalDAV would be severe: native clients, Apple Calendar especially, decide whether *you* may edit an event by comparing `ORGANIZER` against your own account address. Stamping `calendar@example.com` on every Event over CalDAV would make every User a non-organizer of their own meetings in their own client. ADR-0041 already made the codec take a serialization target; this is exactly the kind of divergence that seam exists for.

The accepted cost: a recipient's stored copy names the instance address as organizer, so a guest who later joins the Workspace and syncs over CalDAV sees the human address on the same meeting. Two artifacts, two purposes.

## The reply comes back over IMAP

A `METHOD:REPLY` arrives as ordinary mail in the `SMTP_FROM` mailbox. A background poller — the same shape as the Reminder scheduler and the Refresh poller — fetches unseen mail, parses `text/calendar` parts, and matches on `UID` plus the replying `ATTENDEE` address, which is what RFC 5546 requires a REPLY to carry. New config: `IMAP_HOST`, `IMAP_PORT`, `IMAP_USER`, `IMAP_PASS`.

Sub-addressed organizer addresses (`calendar+<opaque>@`) would make matching robust against clients that mangle `UID`, at the cost of requiring `+` addressing or a catch-all domain. Deferred until matching proves unreliable in practice.

## There is no RSVP link

The Invitation carries no Accept/Decline/Tentative URLs and this app exposes no unauthenticated RSVP endpoint. The invite card is the whole surface, exactly as Proton and Google present it.

Two consequences are accepted rather than mitigated: a recipient whose client renders no invite card has no way to respond that reaches us — their only recourse is a human reply to `Reply-To` — and every emailed response lands one poller-cycle late rather than instantly. In exchange there is no public token to scope, expire, leak, or forward.

## Consequences

- **Configuration is two-tier.** SMTP decides whether an Invitation can be sent; IMAP decides whether a Response can come back. With SMTP and no IMAP, external invites are still offered — the recipient gets a real invitation and the meeting lands in their calendar, which is most of the value — and those Attendees simply stay `needs-action` forever. Disclosed once in Settings beside the SMTP configuration, not as a warning on every invite. With no SMTP, ADR-0058 removes the affordance entirely.
- **An unmatched reply is a new failure class.** A `METHOD:REPLY` naming a `UID` we don't have, or an address that isn't an Attendee of it, has nowhere to go and must be logged and dropped rather than guessed at.
- **The codec gains `ORGANIZER`**, ending the deferral ADR-0055 recorded, and with it `ATTENDEE` — see ADR-0062 for what that means for CalDAV in both directions.
- **`Channel` no longer explains where mail comes from.** It is a property of a Reminder; an Invitation has none and is governed by nothing the User can set.

## Considered Options

- **`METHOD:PUBLISH` attachment plus our own RSVP links (rejected).** Inert `.ics` — no client offers to reply to it — so the add-to-calendar value survives with no inbound pipeline needed, and links reach the database synchronously and work in every client including a forwarded message. Rejected because it is visibly not how invitations work: the recipient sees a link where every other invitation in their inbox shows buttons, and matching the platform convention was the explicit goal.
- **`METHOD:REQUEST` with no inbound path (rejected as a destination, acceptable as a staging point).** The invitation looks right and the copy lands in their calendar; native-button responses are silently lost. Reachable as an intermediate step, but shipping it as the end state means the buttons the mail client puts in front of every recipient are decoration.
- **Inbound webhook from a mail provider's parse API (rejected).** Less code than IMAP, but it inserts a hosted third party into the middle of a self-hosted calendar and adds a second thing to configure that most self-hosters don't have.
