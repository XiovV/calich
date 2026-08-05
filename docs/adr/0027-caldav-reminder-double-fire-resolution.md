# Resolving double reminders once devices sync: channel split + opt-in toggle

Status: accepted

Closes the double-notification question ADR-0020 and ADR-0021 both deferred to "the CalDAV grilling". Once a device syncs an Event over CalDAV, it fires its own `DISPLAY` alarm from the synced `VALARM` — while the server-side scheduler (ADR-0021) also fires a Notification for the same reminder, notifying the user twice.

## The asymmetry that shapes the fix

`EMAIL` alarms are effectively never fired by client devices (iOS/macOS/Android ignore `ACTION:EMAIL` from arbitrary servers), so the server must always own the Email channel — no conflict there. The double-fire risk is **only** on the Notification (`DISPLAY`) channel, and **only** for users who have actually connected a device.

## Decision

- **Email channel: always server-fired.** Unchanged from ADR-0021; no device competes.
- **Notification channel: gated by an explicit per-User toggle** — "Let my synced devices show reminder pop-ups (disable in-app reminder notifications)." Default **off**, so web-only users are unaffected. The settings UI nudges the user toward enabling it when they create their first app password (ADR-0024).
- **No per-fire auto-dedup.**

## Why explicit over automatic

Auto-suppressing based on "has an app password" is fragile — a user might create an app password for a read-only or import client that never fires alarms, and silently lose every reminder. Per-fire dedup is impossible: the server has no reliable signal that a given device fired a given alarm. An honest user-controlled toggle beats a clever guess that fails silently.
