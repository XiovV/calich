# The public namespace is the User; the private one is the Workspace

Status: accepted — introduces the first unauthenticated surface in the app beyond the auth pages, and the first identifier a stranger is ever shown; scoped after ADR-0082 and ADR-0083, addressed against ADR-0044's Workspaces

A Booking Link is stored per `(User, Workspace)` like a Task List, but is published under a per-User **Handle** at the URL root: `/:handle` is that User's index of Public links and `/:handle/:slug` is one link. The index unions across every Workspace its owner belongs to. There is no stored index page.

## Why a Handle exists at all

Nothing in the model could address a User publicly. **Email** is unique but is a login credential, and putting it in a URL hands every visitor a valid username for the login form and for CalDAV Basic auth (ADR-0024). **Name** is explicitly not unique, by ADR-0047, so it cannot address anything. A public identifier therefore had to be invented; the only question was what it names.

It is **nullable and claimed on first use**, suggested from the Email's local part and deduplicated with a numeric suffix. Seeding one for every existing User at migration time was rejected: it hands a public name to people who will never publish anything, squats the namespace with strings nobody chose, and makes the first thing many self-hosters see about the feature an ugly identifier they did not ask for.

## Why the User and not the Workspace

The alternative was a Workspace Handle — `/acme/intro-call` — which is what a product selling to teams would build, and which has a real advantage: it makes the page an organizational surface an Admin can govern.

It was refused because it inverts who the page is about. The mockup's page is Damir's: his name, his avatar, his timezone, his thirty-minute call. A Workspace-addressed link cannot express "book me" without also implying "on behalf of Acme", and a User in two Workspaces then has two unrelated public identities with no page tying them together. It also forces every published link to be workspace-public, which removes the personal-side-project case entirely.

The hybrid, `/:workspaceHandle/:userHandle/:slug`, is unambiguous and collision-free and was rejected on ergonomics alone: it is the only option where a User cannot say their own booking URL out loud.

## Why the row is still Workspace-scoped

A Booking Link writes an Event into a Calendar, and every Calendar belongs to exactly one Workspace (ADR-0045). A row that floated free of Workspaces would have a Book-into Calendar in a Workspace the row itself did not name, and the sidebar — which is workspace-scoped chrome throughout — would have to show links pointing at Calendars not currently visible.

So the two scopes differ deliberately, and each is right for its own side:

- **Private side, Workspace-scoped.** The sidebar lists the Active Workspace's links. Book-into is restricted to Calendars in that Workspace. Losing the membership takes the links with it, exactly as it takes the Task Lists.
- **Public side, User-scoped and unioned.** A visitor has no Session and therefore no Active Workspace; there is nothing to scope the page by. `/damir` lists every Public link Damir has anywhere.

The consequence is explicit and accepted: a stranger may see one page carrying a link into Acme beside a link into a personal Workspace, and cannot tell them apart. Nobody is exposed who did not choose to be — Visibility is per link — and there is no instance-wide authority who could govern it anyway, ADR-0044 having retired that role.

The rejected third option was a per-User "which Workspace is public" setting. It removes the mixing, and it costs a setting whose failure mode is a User publishing a link that silently never appears.

## Decision

- **`handle` on `users`** — nullable, unique, case-insensitive on the same terms as Email (ADR-0047), claimed at first Booking Link creation, editable in Settings → Account.
- **`slug` on booking links, unique per User**, not instance-wide. Two people may both publish `intro-call`.
- **Root routing with a reserved-word list.** `/:handle` and `/:handle/:slug` sit in the same namespace as the app's own routes, so registration refuses `login`, `register`, `settings`, `api`, `book`, `assets`, `static`, `accept-workspace-invite`, and a small future-proofing set (`admin`, `help`, `about`, `new`). Every future top-level route must be added to that list. This is a permanent maintenance tax, taken knowingly in exchange for the URL shape.
- **No redirects on rename.** Changing a Handle or a Slug breaks every URL published under the old one. The UI warns; nothing forwards. Preserving old URLs means a slug-history table that must be kept forever and consulted on every public request, to rescue a link the User themselves chose to move.
- **Booking links are scoped `(user_id, workspace_id)`** and cascade on both, as an append-only migration per ADR-0048.
- **The index page has no row.** It is derived from the owner's Public links, in the sense ADR-0083 derives Task buckets.

## Considered Options

- **A per-User Handle at the URL root, with Workspace-scoped rows (chosen).**
- **A Workspace Handle.** Right for a team product; cannot express a personal booking page, and gives a User in two Workspaces two unrelated identities.
- **Both handles in the path.** Collision-proof, and the only option nobody can say aloud.
- **No handle — `/book/:opaqueId` only.** Costs nothing and collides with nothing, but there is then no index worth having, which collapses the Public/Private distinction into nothing.
- **A `/u/` or `/book/` prefix instead of the root.** Structurally immune to route collisions and needs no reserved list. Rejected for the URL shape alone; it remains the cheap escape if the reserved list ever becomes a burden.
- **Seeding a Handle for every User at migration.** Rejected: assigns public names to people who never asked for one.

## Consequences

- **The app now has an unauthenticated read surface that touches calendar data.** Every public endpoint must resolve Visibility, Access and the SMTP gate before revealing that a Handle even exists — including returning the same response for "no such handle" and "that link is Private", or the namespace becomes a User enumerator.
- **A reserved-word list is now a standing obligation.** Adding a top-level route without adding it to the list will shadow somebody's published page, and the failure is silent.
- **Losing a Workspace membership silently unpublishes links.** They behave as Paused rather than 404ing, so a stranger holding the URL is told it is not accepting bookings, not that it never existed.
- **Handle changes are unrecoverable by design.** There is no way to answer "who had this handle last week", which is also why a released Handle can be claimed by someone else immediately.
