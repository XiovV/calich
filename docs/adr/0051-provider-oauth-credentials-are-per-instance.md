# Provider OAuth credentials are supplied by the self-hoster, per instance

Status: accepted

Connecting a Google account requires an OAuth client ID and secret. This app **ships neither**. Each self-hoster registers their own Google Cloud project and supplies the credentials as environment variables; where they are unset, the Provider is simply absent from the UI, exactly as Email-Channel Reminders are absent without SMTP.

## Why

The alternative is to register one Google Cloud project, complete Google's verification, and embed those credentials in the binary. Every instance would then share one clean, verified consent screen and self-hosters would configure nothing. That is a materially better first-run experience and it is not available to us.

**A shipped client secret is not secret.** OAuth's confidential-client model assumes the secret lives on a server the developer controls. Here the "server" is a container on somebody else's machine, so the secret is readable by every self-hoster and, through them, by anyone. Google's terms treat that as compromised, and a compromised client is one report away from being disabled — taking down every instance in the world simultaneously.

**Redirect URIs are the harder blocker.** Google matches redirect URIs exactly against a registered allow-list. Self-hosted instances live at arbitrary hostnames: `calendar.example.com`, a Tailscale magic-DNS name, `localhost:5173`, a LAN IP. There is no wildcard that covers them, and the workaround — routing every instance's callback through a redirect broker we host — turns a self-hosted app into one with a mandatory dependency on our infrastructure, and puts a service we operate in the path of every user's authorization code. That is a different product.

## What the self-hoster actually gets

The tiers are worth writing down because the folklore about them is wrong:

| Publishing status | Refresh token | Cap | Consent screen |
| --- | --- | --- | --- |
| Testing | **expires after 7 days** | 100 test users | warning |
| In production, unverified | does not expire on a timer | 100 new users, lifetime | "unverified app" warning |
| In production, verified | does not expire | none | clean |

The seven-day refresh-token expiry is tied to the **Testing** publishing status specifically, *not* to being unverified. A self-hoster who flips their own project to "In production" and clicks past the unverified-app warning gets permanent tokens and a 100-user lifetime cap — orders of magnitude above what any self-hosted instance needs.

**The setup documentation must say this explicitly and prominently.** A self-hoster who leaves their project in Testing gets a Connection that dies every seven days, and will report it as a bug in this app. That is the single most likely support burden this feature creates, and the fix is one dropdown in a console they own.

## Considered Options

- **Per-instance credentials via env (chosen).** No verification, no review, permanent tokens, no shared blast radius. Costs a tedious setup guide and an unverified-app warning for every user.
- **One shipped verified client.** Clean consent screen, zero setup. Fails on the redirect-URI problem before the secret-distribution problem even gets a hearing.
- **Per-instance now, shipped client for a hosted offering later.** Effectively what this is: credentials are per-instance configuration, so a future hosted Calich can inject its own verified client with no schema change and no code change.

## Consequences

- **Every user of every self-hosted instance sees "Google hasn't verified this app".** This is not a defect to be fixed later; it is the permanent state of the feature under this decision. UI copy at the connect step should explain *why* — that the instance owner registered the app themselves — because a warning the user has been prepared for is a very different thing from one that ambushes them.
- **Whether Calendar scopes are classified *sensitive* or *restricted* remains unresolved, and does not matter yet.** It becomes decisive only if a hosted Calich ever ships a shared verified client: sensitive requires a free review and a demo video, whereas restricted adds an **annual paid third-party security assessment**. Resolve it before committing to that path, not before merging this.
- **`ENABLE_SIGNUPS` has a sibling.** The set of features that exist only when configured is now SMTP, and Providers. Whatever pattern the UI uses to hide an unconfigured capability should be shared rather than reinvented per feature.
