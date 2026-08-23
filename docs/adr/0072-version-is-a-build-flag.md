# The Version is linked into the binary, and served publicly

Status: accepted

Nothing in this app says which build it is. `/api/version` (#256) answers that with a single opaque label, shown beside the wordmark in the top bar and readable by anyone who can reach the instance.

## Not a `config.go` env var

Every other knob in this system is an environment variable read by `config.Load` — SMTP, IMAP, rate-limit ceilings, `COOKIE_SECURE`. The Version is deliberately not one. It lives in `internal/version` as a package-level `var Version = "dev"` and is written at link time:

```
-ldflags "-X github.com/XiovV/calich/server/internal/version.Version=v1.2.3"
```

The distinction is what the value describes. Every setting in `config.go` describes a **deployment** — this instance's mail server, this instance's limits — and belongs to whoever runs it. The Version describes the **binary**, and the only useful property it has is that it cannot be wrong. An env var can be set to anything at `docker run` time, which means an operator (or a compromised compose file) can make an image claim to be a version it isn't, and the label is worth exactly as much as its trustworthiness. A linked-in symbol travels with the artifact and cannot be separated from the code it names.

The cost is that `make dev-backend`, which is `go run`, cannot set it — so local development always reports `dev`. That's the intended reading: a `go run` binary genuinely has no release identity.

`internal/version` is a separate leaf package rather than a var in `internal/handlers` so anything may read it without an import cycle. Nothing else reads it today; `icalendar.ProdID` is the obvious future caller, and this ADR does not propose that.

## Public and unauthenticated

`/api/version` is registered beside `/api/health`, outside every auth middleware. The disclosure argument against this is real — a version string tells an internet-facing scanner which CVEs apply, and self-hosted instances do sit on the open internet.

It was rejected on the grounds that the concealment would be **fictional**. The frontend bundle is served from content-hashed filenames (`index-Bfg857c1.js`), so any release can be fingerprinted exactly by hashing the assets a public instance already hands out unauthenticated. Authenticating the endpoint would withhold from an operator what an attacker can trivially compute, which is the wrong side of that trade.

What the public endpoint buys is the case where it matters most: `curl`-ing an instance that won't finish booting, or triaging a bug report, neither of which can rely on a working session. This is also why the badge itself is only in the authenticated shell and not on the login page — the unauthenticated path to this information is the endpoint, not a screen.

## The label is opaque

It is never parsed, validated, compared, or `v`-prefixed — not by the server, not by the client. Whoever writes the release tag decides what the badge says, the same posture ADR-0063 takes toward the Event URL.

Nothing in this app compares versions or checks compatibility, so there is nothing for parsing to serve, and each of the alternatives introduces a failure mode: normalising the `v` renders the uninjected default as `vdev` and a `v1.2.3` tag as `vv1.2.3` — a bug reachable only in a real release. Validating semver forces a choice between rejecting a mistyped tag (the endpoint fails) and falling back (the badge silently lies).

## Its own route, not a field on `/health`

The two answer different questions with different lifetimes. `/health` answers "is this process serving requests right now": volatile by design, polled by a liveness probe forever. `/version` answers "what code is this": constant for the process's whole life and permanently cacheable by a client.

Folding them would drag an immutable string through every liveness poll, and — the part that actually bites — would couple them, so that a `/health` which one day reports degradation by failing takes the version badge down with it. A single JSON field is the whole response body; a commit SHA or build timestamp can be added later without breaking any client, and deliberately isn't there now, because nothing currently injects one.

## Known gap: a misconfigured release reports `dev`

`dev` is the package default, so a release build whose ldflag silently failed to fire is indistinguishable from a laptop. Choosing `""` or `"unknown"` as the default does not fix this — it relocates the ambiguity. The only real fix is the release pipeline asserting the flag is set before publishing, which is out of scope for #256 and belongs with the pipeline work.

`dev` was chosen over an empty string for a different reason: an empty label hides the badge entirely, which means the badge is invisible during exactly the development in which its wiring would be noticed as broken.

## Considered and rejected

- **Baking the label into the frontend bundle with Vite `define`.** Zero runtime cost and no endpoint. Rejected because the value that survives is the one a machine can query — a bundle constant answers nothing for an operator holding only a URL.
- **Injecting at both build stages.** Would avoid the client's round trip, but the Docker image builds the frontend and the backend in separate stages, so it creates two injection points that drift the moment a pipeline sets one and forgets the other. One source of truth, fetched once per page load, is worth more than one saved request for a string.
- **A `VERSION` file in the repo read at runtime.** Same mutability problem as the env var, plus a file to ship.
- **Linking the badge to the GitHub release.** Useful once tags exist; today it would 404 for every value the build can produce (`dev` is not a tag, and no tags exist), and it hardcodes a github.com URL into an app that forks are expected to rebuild.
